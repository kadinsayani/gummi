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

// The board replays a stack one tick at a time, and a walk it finished on
// its own needs its pushes said as much as a forced restack does (§18.5:
// gummi prints the push it needs). The tick that finds the stack settled
// hands back the walk it just ended, once, and the walk stays readable
// afterwards so a surface opened later still has the lines.
func TestAnAutomaticReplayHandsBackItsPushes(t *testing.T) {
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

	want := []string{PushCommand(b.BranchName()), PushCommand(c.BranchName())}
	var settled []*StackReplay
	for i := 0; i < 16; i++ {
		res, err := f.eng.StackTick(ctx, "chain")
		if err != nil {
			t.Fatalf("tick %d: %v", i, err)
		}
		if res.Settled != nil {
			settled = append(settled, res.Settled)
		}
		if !res.Again {
			break
		}
	}
	if len(settled) != 1 {
		t.Fatalf("the walk was handed back %d times, want once", len(settled))
	}
	got := settled[0]
	if len(got.Cards) != 2 || got.Cards[0] != b.ID || got.Cards[1] != c.ID ||
		len(got.Push) != 2 || got.Push[0] != want[0] || got.Push[1] != want[1] {
		t.Fatalf("settled walk = %+v, want B and C with their pushes %v", got, want)
	}
	last, ok := f.eng.LastReplay("chain")
	if !ok || len(last.Push) != 2 {
		t.Fatalf("the last replay reads %+v %v, want the walk's pushes", last, ok)
	}
	// a settled stack's next tick hands back nothing more
	if res, err := f.eng.StackTick(ctx, "chain"); err != nil || res.Settled != nil {
		t.Fatalf("a quiet tick = %+v %v, want nothing handed back", res, err)
	}
}

// A restack that arrives in the middle of a walk the board's ticks began
// finishes it, and its answer names every replay of that walk — the ones
// the board made first included. Before, the board's replay was in
// neither answer: the restack found it done and said nothing to replay,
// and the board's own settling tick found nothing left to hand back.
func TestARestackAnswersForTheWalkTheBoardBegan(t *testing.T) {
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

	// the board's tick replays B, and the restack comes in before its next
	if res, err := f.eng.StackTick(ctx, "chain"); err != nil || res.Restacked != b.ID {
		t.Fatalf("board tick = %+v %v, want B replayed", res, err)
	}
	res, err := f.eng.Restack(ctx, "chain")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{PushCommand(b.BranchName()), PushCommand(c.BranchName())}
	if len(res.Replayed) != 2 || res.Replayed[0] != b.ID || len(res.Push) != 2 || res.Push[0] != want[0] || res.Push[1] != want[1] {
		t.Fatalf("restack = replayed %v push %v, want B then C with both pushes", res.Replayed, res.Push)
	}
}
