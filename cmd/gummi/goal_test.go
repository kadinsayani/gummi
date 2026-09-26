package main

import (
	"strings"
	"testing"

	"github.com/morphis/gummi/internal/driver"
)

// A goal's envelope is its whole budget: missing, it fails before any
// workspace work, the same way a run does.
func TestGoalRequiresEnvelope(t *testing.T) {
	t.Setenv("GUMMI_ENVELOPE", "")
	err := runCLI("goal", "export works offline")
	if err == nil || !strings.Contains(err.Error(), "envelope is required") {
		t.Fatalf("err = %v, want an envelope-required failure", err)
	}
}

func TestGoalValidatesItsArguments(t *testing.T) {
	t.Setenv("GUMMI_ENVELOPE", "1000")
	if err := runCLI("goal"); err == nil || !strings.Contains(err.Error(), "exactly one objective") {
		t.Fatalf("err = %v", err)
	}
	if err := runCLI("goal", "--until", "implement", "x"); err == nil || !strings.Contains(err.Error(), "not a valid stop") {
		t.Fatalf("err = %v", err)
	}
	if err := runCLI("goal", "--plan-file", "/nonexistent/goal.md", "x"); err == nil || !strings.Contains(err.Error(), "--plan-file") {
		t.Fatalf("a missing plan file names the flag: %v", err)
	}
}

func TestGoalResumeInput(t *testing.T) {
	goalFlags := func(argv ...string) cliFlags { return parsedFlags(t, "resume", argv...) }

	in, err := goalResumeInput(goalFlags("--goal-note", "also Windows"), driver.ResumeInput{})
	if err != nil || in.Note == nil || *in.Note != "also Windows" {
		t.Fatalf("--goal-note: %+v %v", in, err)
	}
	why := "tables read better"
	in, err = goalResumeInput(goalFlags("--reverse", "D-2"), driver.ResumeInput{RequestChanges: &why})
	if err != nil || in.Reverse == nil || *in.Reverse != "D-2" || in.RequestChanges == nil {
		t.Fatalf("--reverse with a reason: %+v %v", in, err)
	}
	if in, err = goalResumeInput(goalFlags("--wrap-up"), driver.ResumeInput{}); err != nil || !in.WrapUp {
		t.Fatalf("--wrap-up: %+v %v", in, err)
	}
	if _, err := goalResumeInput(goalFlags("--goal-note", "n", "--reverse", "D-1"), driver.ResumeInput{}); err == nil {
		t.Fatal("two goal flags at once must be refused")
	}
	if _, err := goalResumeInput(goalFlags("--wrap-up"), driver.ResumeInput{Approve: true}); err == nil {
		t.Fatal("a goal flag with another decision must be refused")
	}
	if in, err := goalResumeInput(goalFlags(), driver.ResumeInput{Approve: true}); err != nil || !in.Approve {
		t.Fatalf("no goal flag passes the input through: %+v %v", in, err)
	}
}

func TestGoalCmdRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"goal"})
	if err != nil || cmd != goalCmd {
		t.Fatalf("goal is not registered: %v %v", cmd, err)
	}
}
