package ui

import (
	"testing"
	"time"

	"github.com/morphis/gummi/internal/fleetrun"
	"github.com/morphis/gummi/internal/webapi"
)

// The fleet projection carries what the fold computed rather than a
// subset of it: each lane's tokens and diagnosis, the busiest stretch,
// and — on an all-history window, whose fold window starts at the zero
// time — the real start of the history (the fold's first activity), so
// the page never has to reconstruct it from the lanes.
func TestWebFleetReportCarriesTheFold(t *testing.T) {
	to := time.Date(2026, 9, 15, 18, 0, 0, 0, time.UTC)
	first := to.Add(-5 * time.Hour)
	rep := fleetrun.Report{
		Window:       fleetrun.Window{To: to},
		RateSpan:     5 * time.Hour,
		PeakLanes:    2,
		Busiest:      first.Add(time.Hour),
		BusiestLen:   time.Hour,
		BusiestAgent: 90 * time.Minute,
		Tokens:       fleetrun.Tokens{Input: 10, Cached: 20, Output: 5},
		Lanes: []fleetrun.Lane{{
			ID: "FD-001", Title: "one", Kind: "feature",
			Tokens: fleetrun.Tokens{Input: 10, Cached: 20, Output: 5},
			Note:   "sent back once after a verdict",
			Blocks: []fleetrun.Block{{From: first, To: first.Add(time.Hour), Stage: "implement"}},
		}},
	}
	got := WebFleetReport(rep)
	if !got.From.Equal(first) {
		t.Errorf("from = %v, want the history's first activity %v", got.From, first)
	}
	if got.Busiest == nil || !got.Busiest.From.Equal(rep.Busiest) || got.Busiest.LenMs != time.Hour.Milliseconds() || got.Busiest.AgentMs != (90*time.Minute).Milliseconds() {
		t.Errorf("busiest = %+v, want the fold's busiest stretch", got.Busiest)
	}
	if got.Tokens != (webapi.Tokens{Input: 10, Cached: 20, Output: 5}) {
		t.Errorf("window tokens = %+v", got.Tokens)
	}
	l := got.Lanes[0]
	if l.Note != "sent back once after a verdict" || l.Tokens.Input != 10 || l.Tokens.Cached != 20 || l.Tokens.Output != 5 {
		t.Errorf("lane = %+v, want its tokens and note", l)
	}

	// nothing ever ran: the window is empty rather than 0001-01-01
	if e := WebFleetReport(fleetrun.Report{Window: fleetrun.Window{To: to}}); !e.From.Equal(to) || e.Busiest != nil {
		t.Errorf("empty all-history report = from %v busiest %+v, want from = to and no busiest", e.From, e.Busiest)
	}
}
