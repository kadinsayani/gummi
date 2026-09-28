package ui

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/morphis/gummi/internal/agent"
	"github.com/morphis/gummi/internal/domain"
	"github.com/morphis/gummi/internal/spec"
	"github.com/morphis/gummi/internal/state"
)

// writingArchitect is a scripted agent whose architect actually drafts the
// sections a feature's design gate demands, and whose reviewer passes
// everything — a stand-in for a clean run that needs no fixture stepping
// in between the plan finishing and its gate being judged.
func writingArchitect() *agent.Fake {
	return &agent.Fake{Responder: func(opts agent.SessionOpts, _ string) []agent.Event {
		if opts.Role == agent.RoleReviewer {
			return []agent.Event{{Kind: agent.EventMessage, Text: "Nothing blocking.\nVERDICT: pass"}, {Kind: agent.EventIdle}}
		}
		if opts.Role == agent.RoleArchitect && opts.ArtifactPath != "" {
			fillSections(opts.ArtifactPath, "Chosen approach", "Implementation notes")
		}
		return []agent.Event{{Kind: agent.EventMessage, Text: "written"}, {Kind: agent.EventIdle}}
	}}
}

// fillSections appends a line to each named section of the artifact at
// path that is still blank.
func fillSections(path string, names ...string) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return
	}
	content := string(raw)
	for _, name := range spec.UndraftedSections(content, names) {
		body, ok := spec.ViewSection(content, name)
		if !ok {
			continue
		}
		if next, _, err := spec.ReplaceSection(content, name, body+"drafted by the agent.\n\n"); err == nil {
			content = next
		}
	}
	_ = os.WriteFile(path, []byte(content), 0o600)
}

// TestHandingATodoCardToAutopilotCrossesItsCleanDesignGate: the menu's
// hand-over on a todo card writes the mode and starts plan in one command,
// and a fast stage finishes before the board's rows reload. The crossing
// used to read the row — still todo, still attended — so autopilot parked
// at a clean design gate it had been handed, and filed the park and its
// decision under "todo". The card must run on to its verify gate, and
// every row it writes must carry the stage it was really in.
func TestHandingATodoCardToAutopilotCrossesItsCleanDesignGate(t *testing.T) {
	ctx := context.Background()
	m, eng := agentWorkspace(t, writingArchitect())
	f := domain.Feature{ID: "FD-002", Num: 2, Title: "Clap", Slug: "clap", Stage: domain.StageTodo}
	if err := m.store.CreateFeature(ctx, &f); err != nil {
		t.Fatal(err)
	}
	m = pump(t, m, m.loadRows)
	_ = eng

	// run the hand-over's command, and deliberately drop its reply: the
	// reload it asks for is exactly what has not landed yet when the
	// stage ends.
	_ = m.startAutopilot(f, domain.GateAutopilot, m.planAutopilot(f))()
	if st := m.stageOf("FD-002"); st != domain.StageTodo {
		t.Fatalf("fixture: the row should still be stale at todo, reads %s", st)
	}
	m = drainEngineLoop(t, m)

	got, err := m.store.GetFeature(ctx, "FD-002")
	if err != nil {
		t.Fatal(err)
	}
	if got.Stage != domain.StageVerify {
		t.Fatalf("a card handed to autopilot at todo stopped at %s, want it to run on to verify", got.Stage)
	}
	evs, err := m.store.Events(ctx, "FD-002")
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range evs {
		switch ev.Kind {
		case state.EventPark, state.EventDecisionOpen:
			if ev.Stage == domain.StageTodo || strings.Contains(ev.Payload, ":todo:") {
				t.Errorf("%s filed under todo after the card left it: %+v", ev.Kind, ev)
			}
		}
	}
	for _, g := range gateEventsFor(t, m, "FD-002", domain.StagePlan) {
		if g.To == string(domain.StageImplement) && g.Actor != state.ActorAutopilot {
			t.Errorf("plan→implement crossed by %q, want autopilot", g.Actor)
		}
	}
}
