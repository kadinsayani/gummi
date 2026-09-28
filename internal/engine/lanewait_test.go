package engine

import (
	"reflect"
	"testing"

	"github.com/morphis/gummi/internal/agent"
	"github.com/morphis/gummi/internal/domain"
)

// A queued run can say what it waits for: the pool it waits in, its cap,
// whose runs hold that pool's slots, and how many runs are ahead of it. A
// card that sat "queued" for minutes behind a question nobody had
// answered said none of it.
func TestLaneWaitNamesWhatAQueuedRunWaitsFor(t *testing.T) {
	release := make(chan struct{})
	ag := &agent.Fake{Responder: func(opts agent.SessionOpts, msg string) []agent.Event {
		<-release
		return []agent.Event{{Kind: agent.EventIdle}}
	}}
	ws, store, wt := newRepo(t)
	e := New(Config{Agents: singleAgent(ag), Store: store, Worktrees: wt, Workspace: ws, Model: "m", AutopilotLanes: 1})
	t.Cleanup(func() {
		close(release)
		e.Close()
	})
	f1, f2, f3 := autopilotFeature(1, "one"), autopilotFeature(2, "two"), autopilotFeature(3, "three")
	for _, f := range []domain.Feature{f1, f2, f3} {
		withWorktree(t, wt, f)
	}
	if err := e.Run(f1); err != nil {
		t.Fatal(err)
	}
	waitState(t, e, "FD-001", StateRunning)
	if _, ok := e.LaneWait("FD-001"); ok {
		t.Error("a running card reports a queue wait")
	}
	for _, f := range []domain.Feature{f2, f3} {
		if err := e.Run(f); err != nil {
			t.Fatal(err)
		}
		waitState(t, e, f.ID, StateQueued)
	}
	w, ok := e.LaneWait("FD-003")
	if !ok {
		t.Fatal("a queued card reports no wait")
	}
	want := LaneWait{Autopilot: true, Max: 1, Holders: []domain.FeatureID{"FD-001"}, Ahead: 1}
	if !reflect.DeepEqual(w, want) {
		t.Errorf("LaneWait = %+v, want %+v", w, want)
	}
}
