package engine

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// Restack is the walk `stack restack` asks for: it settles the whole chain
// in one call and names the push each moved branch needs.
func TestRestackSettlesTheStackAndNamesThePushes(t *testing.T) {
	f := newStackFixture(t)
	ctx := context.Background()
	a := f.card(1, "parser", "chain", 0)
	b := f.card(2, "eval", "chain", 1)
	c := f.card(3, "cli", "chain", 2)
	f.cut(a, "a.txt", "a\n")
	f.cut(b, "b.txt", "b\n")
	f.cut(c, "c.txt", "c\n")

	aTree := filepath.Join(f.root, ".gummi", "worktrees", string(a.ID))
	if err := os.WriteFile(filepath.Join(aTree, "a.txt"), []byte("a fixed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	f.gitIn(aTree, "commit", "-q", "-a", "--amend", "-m", "A: work, fixed")

	res, err := f.eng.Restack(ctx, "chain")
	if err != nil {
		t.Fatal(err)
	}
	if res.Conflict != nil || res.WaitingOn != "" {
		t.Fatalf("restack stopped short: %+v", res)
	}
	if len(res.Replayed) != 2 || res.Replayed[0] != b.ID || res.Replayed[1] != c.ID {
		t.Fatalf("replayed = %v, want B then C", res.Replayed)
	}
	want := []string{PushCommand(b.BranchName()), PushCommand(c.BranchName())}
	if len(res.Push) != 2 || res.Push[0] != want[0] || res.Push[1] != want[1] {
		t.Fatalf("push = %v, want %v", res.Push, want)
	}
	again, err := f.eng.Restack(ctx, "chain")
	if err != nil {
		t.Fatal(err)
	}
	if len(again.Replayed) != 0 || len(again.Push) != 0 {
		t.Fatalf("a settled stack replayed again: %+v", again)
	}
}
