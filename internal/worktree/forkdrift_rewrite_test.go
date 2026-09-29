package worktree

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/morphis/gummi/internal/domain"
)

// baseRepo is a repo whose cards fork from a non-trunk branch: main plus
// dev, where dev carries one commit of its own (D1, touching shared.txt).
// The manager resolves every card's base to dev, as a card created with
// that base would.
func baseRepo(t *testing.T) (string, *Manager, *memForkStore) {
	t.Helper()
	root := newRepo(t)
	fs := &memForkStore{}
	m, err := NewManager(ctx, root, root, fs)
	if err != nil {
		t.Fatal(err)
	}
	m.SetBaseLookup(func(context.Context, *domain.Feature) (string, error) { return "simon/dev", nil })
	mustGit(t, root, "checkout", "-q", "-b", "simon/dev")
	writeFile(t, root, "shared.txt", "dev v1\n")
	mustGit(t, root, "add", ".")
	mustGit(t, root, "commit", "-q", "-m", "D1")
	return root, m, fs
}

// cardWithCommit creates a card's worktree and commits one file of its own.
func cardWithCommit(t *testing.T, m *Manager, n int, file string) (*domain.Feature, string) {
	t.Helper()
	f := feature(n, "card")
	p, err := m.Create(ctx, f)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, p, file, "card work\n")
	mustGit(t, p, "add", ".")
	mustGit(t, p, "commit", "-q", "-m", "card work "+file)
	return f, p
}

// amendBase rewrites dev's tip in place, the way `git commit --amend` on
// the base branch does.
func amendBase(t *testing.T, root string) {
	t.Helper()
	writeFile(t, root, "shared.txt", "dev v1 amended\n")
	mustGit(t, root, "add", ".")
	mustGit(t, root, "commit", "-q", "--amend", "-m", "D1'")
}

// subjects lists the commit subjects on branch that main does not have,
// newest first.
func subjects(t *testing.T, root, branch string) []string {
	t.Helper()
	out := mustGit(t, root, "log", "--format=%s", "main.."+branch)
	if out == "" {
		return nil
	}
	return strings.Split(out, "\n")
}

// TestRebaseReplaysOnlyTheCardsCommitsWhenItsBaseWasRewritten is the
// FD-025 shape: a card based on another branch, whose base was amended
// under it. The plain rebase replayed the base's OLD commit onto its own
// rewrite and stopped on conflicts the card never authored — handing them
// to an agent to "resolve". The recovery must replay the card's own work
// only, then re-anchor, and leave nothing of the old base behind.
func TestRebaseReplaysOnlyTheCardsCommitsWhenItsBaseWasRewritten(t *testing.T) {
	root, m, fs := baseRepo(t)
	f, p := cardWithCommit(t, m, 25, "feat.txt")
	amendBase(t, root)

	drift, err := m.Drift(ctx, f)
	if err != nil || drift == nil {
		t.Fatalf("Drift = %v, %v; want drift after the base was amended", drift, err)
	}
	if drift.ForkedFrom != "" {
		t.Fatalf("ForkedFrom = %q: the base itself was rewritten, nothing else was forked from", drift.ForkedFrom)
	}
	cmd, err := m.RebaseCommand(ctx, f)
	if err != nil {
		t.Fatal(err)
	}
	recorded, _ := fs.ForkPoint(ctx, f.ID)
	if !strings.Contains(cmd, "--onto") || !strings.HasSuffix(cmd, " "+recorded) {
		t.Fatalf("RebaseCommand = %q, want an --onto from the recorded fork %s", cmd, recorded)
	}

	if err := m.RebaseOnMain(ctx, f); err != nil {
		t.Fatalf("RebaseOnMain on a rewritten base: %v", err)
	}
	if err := m.ReanchorOnMain(ctx, f); err != nil {
		t.Fatalf("ReanchorOnMain: %v", err)
	}
	if d, err := m.Drift(ctx, f); err != nil || d != nil {
		t.Fatalf("still drifted after the rebase: %v, %v", d, err)
	}
	if got := subjects(t, root, f.BranchName()); len(got) != 2 || got[0] != "card work feat.txt" || got[1] != "D1'" {
		t.Fatalf("branch = %q, want the card's commit on top of the amended base alone", got)
	}
	if b, _ := os.ReadFile(filepath.Join(p, "shared.txt")); string(b) != "dev v1 amended\n" {
		t.Fatalf("shared.txt = %q: the base's old version came back", b)
	}
}

// TestDriftMessageNamesTheCardsBase: the remedy used to say "rebase onto
// main" and "if main was accidentally rewound" to a card based on another
// branch. It names the base the card is actually on.
func TestDriftMessageNamesTheCardsBase(t *testing.T) {
	root, m, _ := baseRepo(t)
	f, _ := cardWithCommit(t, m, 25, "feat.txt")
	amendBase(t, root)
	err := m.AssertNoForkDrift(ctx, f)
	if err == nil {
		t.Fatal("want drift")
	}
	msg := err.Error()
	for _, want := range []string{"onto simon/dev", "if simon/dev was accidentally rewound", "press r"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message %q does not say %q", msg, want)
		}
	}
	if strings.Contains(msg, "main") {
		t.Errorf("message %q names main, which this card never forked from", msg)
	}
}

// TestSiblingCardsOnARewrittenBaseAreNotEachOthersFork: two cards cut from
// the same base before it was rewritten both still carry the old commit.
// Neither forked from the other, so the message must not tell a reader to
// land the sibling first — and each rebase must replay only its own work.
func TestSiblingCardsOnARewrittenBaseAreNotEachOthersFork(t *testing.T) {
	root, m, _ := baseRepo(t)
	a, _ := cardWithCommit(t, m, 1, "a.txt")
	b, _ := cardWithCommit(t, m, 2, "b.txt")
	amendBase(t, root)

	for _, f := range []*domain.Feature{a, b} {
		d, err := m.Drift(ctx, f)
		if err != nil || d == nil {
			t.Fatalf("%s: Drift = %v, %v", f.ID, d, err)
		}
		if d.ForkedFrom != "" {
			t.Fatalf("%s: ForkedFrom = %q — a sibling cut from the same old base", f.ID, d.ForkedFrom)
		}
		if strings.Contains(d.Error(), "Land ") {
			t.Fatalf("%s: message sends the reader to land a sibling: %q", f.ID, d.Error())
		}
		if err := m.RebaseOnMain(ctx, f); err != nil {
			t.Fatalf("%s: %v", f.ID, err)
		}
		if err := m.ReanchorOnMain(ctx, f); err != nil {
			t.Fatalf("%s: %v", f.ID, err)
		}
		if got := subjects(t, root, f.BranchName()); len(got) != 2 || got[1] != "D1'" {
			t.Fatalf("%s: branch = %q", f.ID, got)
		}
	}
}

// TestAForkOnAnUnlandedBranchKeepsThePlainRebase: when the fork lives on
// another branch the base never carried (a goal's, not yet landed), the
// base was not rewritten and --onto would strip the work the card was
// built on. That case keeps the rebase it always had.
func TestAForkOnAnUnlandedBranchKeepsThePlainRebase(t *testing.T) {
	root := newRepo(t)
	m := newManager(t, root)
	// the card is cut while goal/x is checked out, then the checkout goes
	// back to main: the card's fork is a goal commit main never had
	mustGit(t, root, "checkout", "-q", "-b", "goal/x")
	writeFile(t, root, "goal.txt", "goal\n")
	mustGit(t, root, "add", ".")
	mustGit(t, root, "commit", "-q", "-m", "G1")
	f, _ := cardWithCommit(t, m, 4, "feat.txt")
	mustGit(t, root, "checkout", "-q", "main")

	d, err := m.Drift(ctx, f)
	if err != nil || d == nil {
		t.Fatalf("Drift = %v, %v", d, err)
	}
	if d.ForkedFrom != "goal/x" {
		t.Fatalf("ForkedFrom = %q, want goal/x", d.ForkedFrom)
	}
	cmd, err := m.RebaseCommand(ctx, f)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(cmd, "--onto") {
		t.Fatalf("RebaseCommand = %q: the goal's commits are the card's foundation, not a rewrite", cmd)
	}
}

// TestDriftAsksWithoutStamping: the board asks Drift on every load of a
// stopped card, and the asking must not backfill a fork the way the
// refusing check does — a card that never had one stays without one.
func TestDriftAsksWithoutStamping(t *testing.T) {
	root := newRepo(t)
	fs := &memForkStore{}
	m, err := NewManager(ctx, root, root, fs)
	if err != nil {
		t.Fatal(err)
	}
	f := feature(1, "Unstamped")
	mustGit(t, root, "worktree", "add", "-q", "-b", f.BranchName(), "--", filepath.Join(root, f.WorktreePath()))
	if d, err := m.Drift(ctx, f); err != nil || d != nil {
		t.Fatalf("Drift = %v, %v; want none for an unstamped card", d, err)
	}
	if got, _ := fs.ForkPoint(ctx, f.ID); got != "" {
		t.Fatalf("Drift stamped a fork point %s", got)
	}
}

// TestRebaseOfABaseResetBackwardDropsWhatTheBaseDropped: the base was
// reset past a commit (D2) the card was cut on top of. That commit was
// thrown away on purpose or by accident; either way it is not the card's,
// and replaying it would put it in the card's diff as if the card wrote
// it. The rebase replays the card's own commit only.
func TestRebaseOfABaseResetBackwardDropsWhatTheBaseDropped(t *testing.T) {
	root, m, _ := baseRepo(t)
	writeFile(t, root, "d2.txt", "d2\n")
	mustGit(t, root, "add", ".")
	mustGit(t, root, "commit", "-q", "-m", "D2")
	f, p := cardWithCommit(t, m, 7, "feat.txt")
	mustGit(t, root, "reset", "-q", "--hard", "HEAD~1")

	if err := m.RebaseOnMain(ctx, f); err != nil {
		t.Fatal(err)
	}
	if err := m.ReanchorOnMain(ctx, f); err != nil {
		t.Fatal(err)
	}
	if got := subjects(t, root, f.BranchName()); len(got) != 2 || got[1] != "D1" {
		t.Fatalf("branch = %q, want the card's commit on D1 without D2", got)
	}
	if _, err := os.Stat(filepath.Join(p, "d2.txt")); !os.IsNotExist(err) {
		t.Fatalf("d2.txt survived the rebase (%v): the dropped commit is in the card's diff", err)
	}
}

// TestRewrittenBaseWithUncommittedWorkAutostashes: a drifted card with
// uncommitted edits takes the autostash rebase — and that rebase must
// replay from the fork too, or the dirty case alone keeps the conflict.
func TestRewrittenBaseWithUncommittedWorkAutostashes(t *testing.T) {
	root, m, _ := baseRepo(t)
	f, p := cardWithCommit(t, m, 9, "feat.txt")
	writeFile(t, p, "feat.txt", "uncommitted\n")
	amendBase(t, root)

	if err := m.RebaseOnMainAutostash(ctx, f); err != nil {
		t.Fatal(err)
	}
	if err := m.ReanchorOnMain(ctx, f); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(p, "feat.txt")); string(b) != "uncommitted\n" {
		t.Fatalf("uncommitted edit = %q after the autostash rebase", b)
	}
	if got := subjects(t, root, f.BranchName()); len(got) != 2 || got[1] != "D1'" {
		t.Fatalf("branch = %q", got)
	}
}

// TestAHandRebasedBranchFallsBackToThePlainRebase: someone rebased the
// card's branch by hand, so it no longer contains its recorded fork. An
// --onto from a commit outside the branch would replay the wrong range;
// the plain rebase is the one that still means something.
func TestAHandRebasedBranchFallsBackToThePlainRebase(t *testing.T) {
	root, m, _ := baseRepo(t)
	f, p := cardWithCommit(t, m, 11, "feat.txt")
	amendBase(t, root)
	mustGit(t, p, "rebase", "-q", "--onto", "main", "HEAD~1")

	cmd, err := m.RebaseCommand(ctx, f)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(cmd, "--onto") {
		t.Fatalf("RebaseCommand = %q for a branch without its recorded fork", cmd)
	}
	if err := m.RebaseOnMain(ctx, f); err != nil {
		t.Fatal(err)
	}
	if err := m.ReanchorOnMain(ctx, f); err != nil {
		t.Fatal(err)
	}
	if d, _ := m.Drift(ctx, f); d != nil {
		t.Fatalf("still drifted: %v", d)
	}
}

// TestADriftedRebaseThatConflictsStillAbortsClean: --onto does not make a
// real conflict go away — the card's own work against the base's new
// history — and that one must still come back typed, with the worktree
// clean and on its old tip.
func TestADriftedRebaseThatConflictsStillAbortsClean(t *testing.T) {
	root, m, _ := baseRepo(t)
	f, p := cardWithCommit(t, m, 13, "shared.txt")
	before := mustGit(t, p, "rev-parse", "HEAD")
	amendBase(t, root)

	err := m.RebaseOnMain(ctx, f)
	var ce *RebaseConflictError
	if !errors.As(err, &ce) {
		t.Fatalf("want *RebaseConflictError, got %T: %v", err, err)
	}
	if len(ce.Files) != 1 || ce.Files[0] != "shared.txt" {
		t.Fatalf("conflicts = %q", ce.Files)
	}
	if after := mustGit(t, p, "rev-parse", "HEAD"); after != before {
		t.Fatalf("tip moved %s → %s after an aborted rebase", before, after)
	}
	if st := mustGit(t, p, "status", "--porcelain"); st != "" {
		t.Fatalf("worktree dirty after abort: %q", st)
	}
}

// Uncommitted work a drifted card could not checkpoint can meet the same
// path arriving from its rewritten base: git refuses before the rebase
// starts, because an untracked file would be overwritten. That used to
// come back untyped ("did not start"), so the board never offered the
// agent. It is a typed stop now, naming the file and git's reason, with
// the worktree exactly as it was.
func TestAnUntrackedFileInTheWayIsATypedStop(t *testing.T) {
	root, m, _ := baseRepo(t)
	f, p := cardWithCommit(t, m, 15, "feat.txt")
	writeFile(t, p, "notes.txt", "card's unfinished notes\n")
	writeFile(t, p, "feat.txt", "uncommitted edit\n")
	writeFile(t, root, "notes.txt", "the base's notes\n")
	mustGit(t, root, "add", "notes.txt")
	mustGit(t, root, "commit", "-q", "--amend", "-m", "D1'")
	before := mustGit(t, p, "rev-parse", "HEAD")

	err := m.RebaseOnMainAutostash(ctx, f)
	var ce *RebaseConflictError
	if !errors.As(err, &ce) {
		t.Fatalf("want *RebaseConflictError, got %T: %v", err, err)
	}
	if len(ce.Files) != 1 || ce.Files[0] != "notes.txt" || !strings.Contains(ce.Reason, "untracked working tree files would be overwritten") {
		t.Fatalf("stop = %+v", ce)
	}
	if !strings.Contains(ce.Error(), "notes.txt") || strings.Contains(ce.Error(), "hit conflicts") {
		t.Errorf("message %q", ce.Error())
	}
	if after := mustGit(t, p, "rev-parse", "HEAD"); after != before {
		t.Fatal("tip moved")
	}
	for name, want := range map[string]string{"notes.txt": "card's unfinished notes\n", "feat.txt": "uncommitted edit\n"} {
		if b, _ := os.ReadFile(filepath.Join(p, name)); string(b) != want {
			t.Errorf("%s = %q after the stop", name, b)
		}
	}
}

// A rebase can stop mid-way with nothing unmerged — here the card's own
// history added a file, removed it again, and the worktree holds an
// untracked copy the first replayed commit would overwrite. It read "hit
// conflicts" with no file named; it names git's reason and the path now,
// and the worktree comes back as it was.
func TestAMidRebaseStopWithNothingUnmergedSaysWhy(t *testing.T) {
	root, m, _ := baseRepo(t)
	f := feature(17, "card")
	p, err := m.Create(ctx, f)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, p, "gen.txt", "generated\n")
	mustGit(t, p, "add", ".")
	mustGit(t, p, "commit", "-q", "-m", "add gen")
	mustGit(t, p, "rm", "-q", "gen.txt")
	mustGit(t, p, "commit", "-q", "-m", "drop gen")
	writeFile(t, p, "gen.txt", "regenerated, uncommitted\n")
	amendBase(t, root)

	err = m.RebaseOnMainAutostash(ctx, f)
	var ce *RebaseConflictError
	if !errors.As(err, &ce) {
		t.Fatalf("want *RebaseConflictError, got %T: %v", err, err)
	}
	if !strings.Contains(ce.Reason, "untracked working tree files would be overwritten by merge: gen.txt") || strings.Contains(ce.Error(), "hit conflicts") {
		t.Fatalf("stop with nothing unmerged says nothing: %q", ce.Error())
	}
	if m.rebaseInProgress(ctx, p) {
		t.Fatal("rebase left in flight")
	}
	if b, _ := os.ReadFile(filepath.Join(p, "gen.txt")); string(b) != "regenerated, uncommitted\n" {
		t.Errorf("gen.txt = %q", b)
	}
}
