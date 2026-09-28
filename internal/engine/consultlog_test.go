package engine

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/morphis/gummi/internal/agent"
	"github.com/morphis/gummi/internal/domain"
	"github.com/morphis/gummi/internal/state"
)

// A consult's question and answer are written to the card's log, where
// they were asked: the conversation used to live in its session alone,
// drawn below everything else on the page and gone after a restart.
func TestConsultTurnsAreRecordedOnTheCard(t *testing.T) {
	ag := &agent.Fake{Responder: func(_ agent.SessionOpts, msg string) []agent.Event {
		return []agent.Event{
			{Kind: agent.EventTextDelta, Text: "Not yet — "},
			{Kind: agent.EventTextDelta, Text: "nothing touches it."},
			{Kind: agent.EventMessage},
			{Kind: agent.EventIdle},
		}
	}}
	ws, store, wt := newRepo(t)
	e := New(Config{Agents: singleAgent(ag), Store: store, Worktrees: wt, Workspace: ws, Model: "m", Persist: true})
	t.Cleanup(func() { e.Close() })
	ctx := context.Background()
	f := feature(4, "asked at todo", domain.StageTodo)
	createFeature(t, store, f)

	c, err := e.OpenConsult(ctx, f)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Send(WithActor(ctx, state.PersonActor("alice")), "is this already done?"); err != nil {
		t.Fatal(err)
	}
	waitConsultIdle(t, c)

	evs, err := store.Events(ctx, f.ID)
	if err != nil {
		t.Fatal(err)
	}
	type turn struct{ Author, Content, By string }
	var got []turn
	for _, ev := range evs {
		if ev.Kind != state.EventConsult {
			continue
		}
		var p turn
		if err := json.Unmarshal([]byte(ev.Payload), &p); err != nil {
			t.Fatal(err)
		}
		if ev.Stage != domain.StageTodo {
			t.Errorf("consult turn recorded at %q, want the card's stage (todo)", ev.Stage)
		}
		got = append(got, p)
	}
	want := []turn{
		{Author: "user", Content: "is this already done?", By: state.PersonActor("alice")},
		{Author: "assistant", Content: "Not yet — nothing touches it."},
	}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("recorded consult turns = %+v, want %+v", got, want)
	}
}
