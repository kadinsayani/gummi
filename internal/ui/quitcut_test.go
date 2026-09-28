package ui

import (
	"strings"
	"testing"

	"github.com/morphis/gummi/internal/domain"
	"github.com/morphis/gummi/internal/engine"
)

// TestARunCutByAQuitSaysSo: a stage the last quit stopped mid-run (a host
// restart) comes back paused, and its decision says the quit cut it
// rather than "the run is paused", which read as something a person did.
func TestARunCutByAQuitSaysSo(t *testing.T) {
	in := nextInput{stage: domain.StageImplement, kind: domain.KindResearch, sess: engine.StatePaused, cutByQuit: true}
	acts := stageActions(in)
	if len(acts) == 0 || acts[0].id != "run" || !strings.Contains(acts[0].why, "gummi stopped while implement ran") {
		t.Fatalf("answers = %+v, want a resume row naming the quit", acts)
	}
	in.cutByQuit = false
	if acts := stageActions(in); !strings.Contains(acts[0].why, "the run is paused") {
		t.Errorf("a paused run reads %q", acts[0].why)
	}
}
