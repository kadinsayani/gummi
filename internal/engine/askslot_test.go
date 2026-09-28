package engine

import (
	"context"
	"testing"
	"time"

	"github.com/morphis/gummi/internal/agent"
	"github.com/morphis/gummi/internal/domain"
)

// TestAQuestionGivesTheAttendedLaneBack: with the one attended lane, a
// card waiting on a person's answer used to hold it for as long as the
// question sat there, and every other attended card queued behind it. A
// blocked session frees its slot (DESIGN §4.2): the queued card starts,
// and the answered card takes a slot back at once rather than queuing
// behind the card that started meanwhile.
func TestAQuestionGivesTheAttendedLaneBack(t *testing.T) {
	release := make(chan struct{})
	args := []byte(`{"changes_section":"Problem","question":"Persist where?","options":[{"label":"per-device","detail":"localStorage"},{"label":"synced","detail":"account"}]}`)
	ag := &agent.Fake{Caps: agent.Capabilities{ClientTools: true, Interrupt: true}, Responder: func(opts agent.SessionOpts, msg string) []agent.Event {
		if opts.FeatureID == "FD-001" {
			return []agent.Event{{Kind: agent.EventClientToolCall, ToolCall: &agent.ToolCall{ID: "call-1", Name: "ask_user", Args: args}}}
		}
		<-release
		return []agent.Event{{Kind: agent.EventIdle}}
	}}
	ws, store, wt := newRepo(t)
	e := New(Config{
		Agents: singleAgent(ag), Store: store, Worktrees: wt, Workspace: ws,
		Model: "m", MaxActive: 1, AutopilotLanes: 2,
	})
	t.Cleanup(func() {
		close(release)
		e.Close()
	})

	asker := attendedFeature(1, "asker", domain.StagePlan)
	other := attendedFeature(2, "other", domain.StageImplement)
	for _, f := range []domain.Feature{asker, other} {
		if err := store.CreateFeature(context.Background(), &f); err != nil {
			t.Fatal(err)
		}
		withWorktree(t, wt, f)
	}
	if err := e.Run(asker); err != nil {
		t.Fatal(err)
	}
	waitState(t, e, "FD-001", StateRunning)
	if err := e.Run(other); err != nil {
		t.Fatal(err)
	}

	// the question opens: the lane goes to the card queued behind it
	waitUntil(t, func() bool { return e.Get("FD-001").Snapshot().PendingAsk != nil })
	waitState(t, e, "FD-002", StateRunning)
	if lc := e.LaneCounts(); lc.AttendedRunning != 1 {
		t.Errorf("attended running = %d while FD-001 waits on its question, want 1 (FD-002's)", lc.AttendedRunning)
	}

	// the answer takes a slot back at once: FD-001 runs on beside FD-002
	if err := e.AnswerAs(context.Background(), "FD-001", "per-device", "user"); err != nil {
		t.Fatal(err)
	}
	if st := e.Get("FD-001").State(); st != StateRunning {
		t.Errorf("answered card is %s, want running", st)
	}
	if lc := e.LaneCounts(); lc.AttendedRunning != 2 {
		t.Errorf("attended running after the answer = %d, want 2 (the answered card and the one that started meanwhile)", lc.AttendedRunning)
	}
}

// waitUntil polls cond until it holds or the test's patience runs out.
func waitUntil(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition never held")
		}
		time.Sleep(5 * time.Millisecond)
	}
}
