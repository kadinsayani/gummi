package engine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/morphis/gummi/internal/agent"
)

// TestAFreeformCardsLandingIsDraftedFromItsConversation is the regression
// for a freeform card whose landing dialog always read "draft unavailable:
// scribe could not read the spec for its digest: open : no such file or
// directory". A freeform card has no spec (DESIGN §19); its draft is made
// from the branch and what the person asked for.
func TestAFreeformCardsLandingIsDraftedFromItsConversation(t *testing.T) {
	var mu sync.Mutex
	var prompt string
	ag := &agent.Fake{Responder: func(opts agent.SessionOpts, msg string) []agent.Event {
		if opts.Role == agent.RoleScribe {
			mu.Lock()
			prompt = msg
			mu.Unlock()
			return []agent.Event{{Kind: agent.EventMessage, Text: "```gummi-commit\nfix(pty): close the leaked fd\n\n- a leaked fd kept the child alive\n```"}, {Kind: agent.EventIdle}}
		}
		if err := os.WriteFile(filepath.Join(opts.WorkDir, "pty.txt"), []byte("closed\n"), 0o600); err != nil {
			t.Error(err)
		}
		return []agent.Event{{Kind: agent.EventMessage, Text: "done"}, {Kind: agent.EventIdle}}
	}}
	ag.Caps = agent.Capabilities{UsageEvents: true, Interrupt: true}
	ws, store, wt := newRepo(t)
	e := New(Config{Agents: singleAgent(ag), Store: store, Worktrees: wt, Workspace: ws, Model: "m"})
	t.Cleanup(func() { e.Close() })
	ctx := context.Background()

	f := freeformCard(1, "poke at the pty leak")
	createFeature(t, store, f)
	ff, err := e.OpenFreeform(ctx, f)
	if err != nil {
		t.Fatal(err)
	}
	if err := ff.Send(ctx, "drop the leaked fd in the pty child"); err != nil {
		t.Fatal(err)
	}
	waitFreeformIdle(t, ff)

	draft, err := e.DraftCommitMessage(ctx, f)
	if err != nil {
		t.Fatalf("freeform draft failed: %v (dialog reason %q)", err, CommitDraftFailureReason(err))
	}
	if !strings.Contains(draft, "close the leaked fd") {
		t.Errorf("draft = %q", draft)
	}
	mu.Lock()
	defer mu.Unlock()
	for _, want := range []string{"What the person asked for", "poke at the pty leak", "drop the leaked fd in the pty child"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("draft prompt missing %q:\n%s", want, prompt)
		}
	}
	if strings.Contains(prompt, "Spec digest") || strings.Contains(prompt, "This branch is verified") {
		t.Errorf("a freeform draft prompt still speaks of a spec or a verify:\n%s", prompt)
	}
}

// TestCommitDraftFailureReasonIsShort: the dialog prints "no draft:
// <reason>", never a wrapped Go error.
func TestCommitDraftFailureReasonIsShort(t *testing.T) {
	raw := errors.New("open : no such file or directory")
	cases := map[string]error{
		"the card's spec could not be read": &CommitDraftUnavailable{Short: "the card's spec could not be read", Err: raw},
		"the scribe (claude-haiku-4.5 on claude) failed — There's an issue with the selected model": &ScribeFailure{
			Pass: "the landing draft", Model: "claude-haiku-4.5", Backend: "claude",
			Err: errors.New("There's an issue with the selected model\nmore stderr"),
		},
		"the scribe timed out":           context.DeadlineExceeded,
		"the scribe could not draft one": raw,
	}
	for want, err := range cases {
		if got := CommitDraftFailureReason(err); got != want {
			t.Errorf("CommitDraftFailureReason(%v) = %q, want %q", err, got, want)
		}
	}
}
