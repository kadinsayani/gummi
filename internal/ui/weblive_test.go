package ui

import (
	"testing"

	"github.com/morphis/gummi/internal/engine"
)

// A tool line whose backend never reported back is not running once the
// session has stopped: only the newest pending call of a session mid-turn
// is in flight.
func TestWebTranscriptOnlyTheCallInFlightRuns(t *testing.T) {
	tr := []engine.Message{
		{Author: engine.AuthorTool, Content: "read  a.go"},
		{Author: engine.AuthorAssistant, Content: "done"},
		{Author: engine.AuthorTool, Content: "edit  b.go"},
	}
	turns, _, tool := webTranscript(engine.Snapshot{Transcript: tr})
	if tool != nil {
		t.Fatalf("a stopped session has no call in flight, got %+v", tool)
	}
	for _, tn := range turns {
		if tn.Tool != nil && tn.Tool.Status != "" {
			t.Fatalf("%q: status %q, want none", tn.Tool.Label, tn.Tool.Status)
		}
	}

	turns, _, tool = webTranscript(engine.Snapshot{Transcript: tr, Busy: true})
	if tool == nil || tool.Status != "running" || tool.Label != "edit  b.go" {
		t.Fatalf("the newest call of a busy session is in flight, got %+v", tool)
	}
	if st := turns[0].Tool.Status; st != "" {
		t.Fatalf("an older pending call: status %q, want none", st)
	}
	if st := turns[2].Tool.Status; st != "running" {
		t.Fatalf("the call in flight: status %q, want running", st)
	}
}
