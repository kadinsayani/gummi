package main

import (
	"testing"
)

// TestResumeDoneRSDispatchesDecompose confirms --approve and
// --request-changes round-trip into ResumeInput exactly as they do for any
// other card — the FD-081 decompose checkpoint on a done research card
// needs no new CLI plumbing, only the driver-side dispatch on kind+stage
// (internal/driver/decompose_test.go covers that dispatch end-to-end).
func TestResumeDoneRSDispatchesDecompose(t *testing.T) {
	in, err := resumeInput(parsedFlags(t, "resume", "--approve"))
	if err != nil {
		t.Fatalf("--approve: %v", err)
	}
	if !in.Approve {
		t.Errorf("ResumeInput.Approve = false, want true")
	}

	note := "tighten the second slice's scope"
	in, err = resumeInput(parsedFlags(t, "resume", "--request-changes", note))
	if err != nil {
		t.Fatalf("--request-changes: %v", err)
	}
	if in.RequestChanges == nil || *in.RequestChanges != note {
		t.Errorf("ResumeInput.RequestChanges = %v, want %q", in.RequestChanges, note)
	}
}

// An explicitly empty --answer is a decision (an empty answer the driver
// can reject cleanly), not an unset flag that silently re-runs the stage.
// The distinction rides on pflag's Changed.
func TestResumeEmptyAnswerIsStillADecision(t *testing.T) {
	in, err := resumeInput(parsedFlags(t, "resume", "--answer", ""))
	if err != nil {
		t.Fatalf("--answer \"\": %v", err)
	}
	if in.Answer == nil {
		t.Fatal(`--answer "" produced no answer at all; an explicitly empty answer is a decision`)
	}
	bare, err := resumeInput(parsedFlags(t, "resume"))
	if err != nil {
		t.Fatalf("bare resume: %v", err)
	}
	if bare.Answer != nil {
		t.Fatal("a bare resume carried an answer")
	}
}

// --note only composes with --bounce; alone it is a usage error rather
// than a silent no-op.
func TestResumeNoteRequiresBounce(t *testing.T) {
	if _, err := resumeInput(parsedFlags(t, "resume", "--note", "why")); err == nil {
		t.Fatal("--note without --bounce must be refused")
	}
	in, err := resumeInput(parsedFlags(t, "resume", "--bounce", "--note", "why"))
	if err != nil || in.Bounce == nil || *in.Bounce != "why" {
		t.Fatalf("--bounce --note: %+v %v", in, err)
	}
}
