package web

import (
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/morphis/gummi/internal/agent"
	"github.com/morphis/gummi/internal/domain"
	"github.com/morphis/gummi/internal/engine"
	"github.com/morphis/gummi/internal/webapi"
)

// namedFake is the fake agent under a real backend's name, so a session
// can name it the way a person names codex in the picker.
type namedFake struct {
	*agent.Fake
	name string
}

func (n namedFake) Name() string { return n.name }

// waitTranscript waits for a session's conversation to hold want.
func waitTranscript(t *testing.T, h *cardBoard, id, want string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if ff := h.eng.Freeform(domain.FeatureID(id)); ff != nil {
			for _, m := range ff.Snapshot().Transcript {
				if strings.Contains(m.Content, want) {
					return
				}
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s's conversation never held %q", id, want)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// A session is started from its draft: the agent and model picked in the
// composer, and the first message verbatim as its first turn. The card
// says what it runs on, and the picker offers that pair back as recent.
func TestASessionStartsOnTheModelItsDraftPicked(t *testing.T) {
	h := newCardBoard(t, namedFake{Fake: agent.NewFake("on it"), name: "codex"})

	var form webapi.Form
	if st := h.call(http.MethodGet, "/api/form", nil, &form); st != http.StatusOK {
		t.Fatalf("form = %d", st)
	}
	names := make([]string, 0, len(form.Sessions.Agents))
	for _, a := range form.Sessions.Agents {
		names = append(names, a.Name)
	}
	if !slices.Equal(names, engine.SessionBackends) {
		t.Errorf("the picker offers %v, want every session backend %v", names, engine.SessionBackends)
	}
	if i := slices.IndexFunc(form.Sessions.Agents, func(a webapi.SessionAgent) bool { return a.Name == "codex" }); i < 0 || !form.Sessions.Agents[i].Installed || !form.Sessions.Agents[i].NeedsModel {
		t.Errorf("codex, which this board runs, is not offered as installed and needing a model: %+v", form.Sessions.Agents)
	}

	opening := "The retry test flakes on CI.\n\nFind out why, and keep the fix small."
	c := h.create(webapi.CreateCardRequest{Kind: "freeform", Description: opening, Backend: "codex", Model: "gpt-5"})
	if c.Session == nil || c.Session.Backend != "codex" || c.Session.Model != "gpt-5" {
		t.Fatalf("the session runs on %+v, want codex/gpt-5", c.Session)
	}
	if c.Title != "The retry test flakes on CI." {
		t.Errorf("title = %q, want the opening's first line", c.Title)
	}
	waitTranscript(t, h, c.ID, "keep the fix small")

	if st := h.call(http.MethodGet, "/api/form", nil, &form); st != http.StatusOK {
		t.Fatalf("form = %d", st)
	}
	if !slices.Contains(form.Sessions.Recent, webapi.SessionModel{Backend: "codex", Model: "gpt-5"}) {
		t.Errorf("the pair a session runs on is not offered as recent: %+v", form.Sessions.Recent)
	}
}

// What a session may not be started on is refused before anything is
// minted: a model on a card in the workflow, a pair no agent could run,
// and an agent this host does not have.
func TestASessionDraftRefusesWhatCannotRun(t *testing.T) {
	h := newCardBoard(t, namedFake{Fake: agent.NewFake("ok"), name: "codex"})
	for _, req := range []webapi.CreateCardRequest{
		{Kind: "feature", Title: "Configurable retries", Backend: "codex", Model: "gpt-5"},
		{Kind: "freeform", Description: "anything", Backend: "claude", Model: "gpt-5"},
		{Kind: "freeform", Description: "anything", Backend: "opencode"},
		{Kind: "freeform", Description: "anything", Model: "gpt-5"},
	} {
		var e webapi.Error
		if st := h.call(http.MethodPost, "/api/cards", req, &e); st != http.StatusBadRequest {
			t.Errorf("%+v = %d %+v, want a refusal", req, st, e)
		}
	}
	rows, err := h.store.ListFeatures(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Errorf("a refused draft minted %d cards", len(rows))
	}
}

// A session's menu switches its model; the next turn runs on the new one,
// and a card in the workflow is offered no such switch.
func TestASessionSwitchesItsModelFromItsMenu(t *testing.T) {
	h := newCardBoard(t, namedFake{Fake: agent.NewFake("done"), name: "codex"})
	c := h.create(webapi.CreateCardRequest{Kind: "freeform", Description: "tidy the help text", Backend: "codex", Model: "gpt-5"})
	waitTranscript(t, h, c.ID, "done")
	for deadline := time.Now().Add(10 * time.Second); h.eng.Freeform(domain.FeatureID(c.ID)).Busy(); time.Sleep(20 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the opening turn never ended")
		}
	}

	if !slices.ContainsFunc(h.card(c.ID).Actions, func(a webapi.Action) bool { return a.ID == "model" && a.Needs == webapi.ActionNeedsModel }) {
		t.Fatalf("a session's menu offers no model switch: %+v", h.card(c.ID).Actions)
	}
	if st, _ := h.actionRaw(c.ID, "model", webapi.ActionRequest{Backend: "claude", Model: "gpt-5"}); st != http.StatusBadRequest {
		t.Errorf("switching claude to a gpt model = %d, want a refusal", st)
	}
	h.action(c.ID, "model", webapi.ActionRequest{Backend: "codex", Model: "gpt-5-codex"})
	got := h.waitCard(c.ID, "the new model", func(c webapi.Card) bool { return c.Session != nil && c.Session.Model == "gpt-5-codex" })
	if got.Session.Backend != "codex" {
		t.Errorf("after the switch the session runs on %+v", got.Session)
	}
	row, err := h.store.GetFeature(t.Context(), domain.FeatureID(c.ID))
	if err != nil {
		t.Fatal(err)
	}
	if row.SessionModel != "gpt-5-codex" {
		t.Errorf("the card holds model %q after the switch", row.SessionModel)
	}

	f := h.create(webapi.CreateCardRequest{Kind: "feature", Title: "Configurable retries"})
	if slices.ContainsFunc(h.card(f.ID).Actions, func(a webapi.Action) bool { return a.ID == "model" }) {
		t.Error("a card in the workflow is offered a model switch; its stages take their models from its profile")
	}
	if h.card(f.ID).Session != nil {
		t.Error("a card in the workflow reports a session model")
	}
}
