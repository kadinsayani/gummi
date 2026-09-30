package ui

import (
	"context"
	"os"
	"os/exec"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/morphis/gummi/internal/agentcli"
	"github.com/morphis/gummi/internal/domain"
	"github.com/morphis/gummi/internal/engine"
	"github.com/morphis/gummi/internal/webapi"
)

// A session is a freeform card that runs on the agent and model its person
// picked (DESIGN §19.8). This file is the Shell's half: what the web face's
// model picker offers, which pair a card's session runs on, and the switch.

// sessionRecentMax bounds the picker's Recent list: it is a shortcut to the
// few pairs in use, not a history.
const sessionRecentMax = 5

// agentInstalled reports whether this host can start the agent named: its
// CLI resolves on PATH, or — headless having no CLI of its own — a command
// line is configured for it. A var so a test can decide without a PATH.
var agentInstalled = func(name string) bool {
	if bin, ok := agentcli.Binary(name); ok {
		_, err := exec.LookPath(bin)
		return err == nil
	}
	if name == "headless" {
		return strings.TrimSpace(os.Getenv("GUMMI_AGENT_CMD")) != ""
	}
	return false
}

// webSessionModels is what a session's model picker offers: every agent a
// session can run on with the models the profiles already run there, the
// pairs sessions on this board run on now, and what a new session gets
// when nothing is picked.
func (m *Shell) webSessionModels() webapi.SessionModels {
	out := webapi.SessionModels{Agents: []webapi.SessionAgent{}, Recent: []webapi.SessionModel{}}
	var suggest map[string][]string
	if m.engine != nil {
		suggest = m.engine.SessionSuggestions()
		b, model := m.engine.SessionModel(domain.Feature{Kind: domain.KindFreeform, Profile: m.defaultProfile()})
		out.Default = webapi.SessionModel{Backend: b, Model: model}
	}
	for _, name := range engine.SessionBackends {
		needs, hint := engine.SessionModelRule(name)
		out.Agents = append(out.Agents, webapi.SessionAgent{
			Name:       name,
			Installed:  (m.engine != nil && m.engine.HasAgent(name)) || agentInstalled(name),
			Models:     append([]string{}, suggest[name]...),
			NeedsModel: needs, Hint: hint,
		})
	}
	var named []domain.Feature
	for _, r := range m.rows {
		if r.F.IsFreeform() && (r.F.SessionBackend != "" || r.F.SessionModel != "") {
			named = append(named, r.F)
		}
	}
	slices.SortStableFunc(named, func(a, b domain.Feature) int { return b.UpdatedAt.Compare(a.UpdatedAt) })
	for _, f := range named {
		p := webapi.SessionModel{Backend: f.SessionBackend, Model: f.SessionModel}
		if !slices.Contains(out.Recent, p) {
			out.Recent = append(out.Recent, p)
		}
		if len(out.Recent) == sessionRecentMax {
			break
		}
	}
	return out
}

// defaultProfile is the profile a card minted with none resolves under:
// the declared default, which is also what the new-card form preselects.
func (m *Shell) defaultProfile() string {
	if len(m.profileNames) > 0 {
		return m.profileNames[0]
	}
	return ""
}

// webSessionOf is the agent and model a card's session runs on, as its next
// turn will resolve them; nil for a card in the workflow.
func (m *Shell) webSessionOf(f domain.Feature) *webapi.SessionModel {
	if !f.IsFreeform() || m.engine == nil {
		return nil
	}
	b, model := m.engine.SessionModel(f)
	return &webapi.SessionModel{Backend: b, Model: model}
}

// checkSessionPick refuses a backend/model pair before anything is minted
// or switched: a pair the engine refuses, and an agent this host cannot
// start. Empty is no pick at all, which is always fine.
func (m *Shell) checkSessionPick(backend, model string) string {
	if backend == "" && strings.TrimSpace(model) == "" {
		return ""
	}
	if backend == "" {
		return "say which agent runs " + strings.TrimSpace(model)
	}
	if err := engine.CheckSessionModel(backend, model); err != nil {
		return err.Error()
	}
	if !(m.engine != nil && m.engine.HasAgent(backend)) && !agentInstalled(backend) {
		return backend + " is not installed on this host"
	}
	return ""
}

// switchSessionModel moves a card's session to another agent and model,
// off the loop: stopping a live backend commits the worktree first.
func (m *Shell) switchSessionModel(id domain.FeatureID, backend, model string) tea.Cmd {
	eng := m.engine
	return func() tea.Msg {
		if err := eng.SwitchSessionModel(context.Background(), id, backend, model); err != nil {
			return noticeMsg{text: sanitize(err.Error()), isErr: true, id: id}
		}
		label := strings.TrimSpace(model)
		if label == "" {
			label = backend + "'s default model"
		} else {
			label += " · " + backend
		}
		return sessionSwitchedMsg{id: id, text: string(id) + " now runs on " + label}
	}
}

// sessionSwitchedMsg settles a model switch: the rows reload, since the
// card row is what carries the pair, and the notice says what runs now.
type sessionSwitchedMsg struct {
	id   domain.FeatureID
	text string
}
