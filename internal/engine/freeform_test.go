package engine

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/morphis/gummi/internal/agent"
	"github.com/morphis/gummi/internal/domain"
	"github.com/morphis/gummi/internal/state"
)

// freeformCard builds a freeform work item (FF-NNN) at the one stage such
// a card ever holds, with an envelope, since the envelope is the single
// floor a freeform card keeps.
func freeformCard(num int, title string) domain.Feature {
	id, _ := domain.NewID(domain.KindFreeform, num)
	slug, _ := domain.Slugify(title)
	now := time.Now()
	return domain.Feature{
		ID: id, Num: num, Kind: domain.KindFreeform, Title: title, Slug: slug,
		Stage: domain.StageOpen, Budget: domain.Budget{Envelope: 400},
		BranchScheme: domain.BranchSchemeKind,
		CreatedAt:    now, UpdatedAt: now,
	}
}

func waitFreeformIdle(t *testing.T, ff *FreeformSession) {
	t.Helper()
	deadline := time.After(testWaitTimeout)
	for {
		if !ff.Snapshot().Busy {
			return
		}
		select {
		case <-deadline:
			t.Fatal("freeform session never went idle")
		case <-time.After(5 * time.Millisecond):
		}
	}
}

// TestAFreeformTurnCheckpointsItsWorktree is the load-bearing property of
// the whole kind: a freeform card has no stage completion to settle and no
// gate to hold its work, so the commit at the end of each turn is the only
// thing between what the agent wrote and the branch. A turn that ended
// uncommitted would be a turn whose work exists only in a working tree.
func TestAFreeformTurnCheckpointsItsWorktree(t *testing.T) {
	ag := &agent.Fake{Responder: func(opts agent.SessionOpts, _ string) []agent.Event {
		// write into the session's own cwd, which must be the card's
		// worktree — the freeform card's "gets its worktree/branch" half
		if err := os.WriteFile(filepath.Join(opts.WorkDir, "sketch.txt"), []byte("hand-rolled\n"), 0o600); err != nil {
			t.Error(err)
		}
		return []agent.Event{{Kind: agent.EventMessage, Text: "wrote it"}, {Kind: agent.EventIdle}}
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
	if err := ff.Send(ctx, "drop the leaked fd"); err != nil {
		t.Fatal(err)
	}
	waitFreeformIdle(t, ff)

	tree := filepath.Join(ws.Root, f.WorktreePath())
	if _, err := os.Stat(filepath.Join(tree, "sketch.txt")); err != nil {
		t.Fatalf("the turn's file is not in the card's worktree: %v", err)
	}
	// The commit exists, on the card's own branch, and names a turn rather
	// than the stage the card is parked at.
	log := gitOut(t, tree, "log", "--oneline", "--no-decorate")
	if !strings.Contains(log, string(f.ID)+": turn checkpoint") {
		t.Errorf("no turn checkpoint on the branch:\n%s", log)
	}
	if got := strings.TrimSpace(gitOut(t, tree, "rev-parse", "--abbrev-ref", "HEAD")); got != f.BranchName() {
		t.Errorf("worktree is on %s, want the card's branch %s", got, f.BranchName())
	}
	if files := gitOut(t, tree, "show", "--name-only", "--format=", "HEAD"); !strings.Contains(files, "sketch.txt") {
		t.Errorf("the checkpoint did not commit the turn's file:\n%s", files)
	}
	// Nothing left dirty: the point of checkpointing every turn is that the
	// tree is not where the work lives.
	if out := gitOut(t, tree, "status", "--porcelain"); strings.TrimSpace(out) != "" {
		t.Errorf("worktree still dirty after the turn:\n%s", out)
	}
}

// TestAFreeformSessionGetsNoArtifact: a freeform card has no document, so
// the session must not be handed one — a backend told where its artifact
// is would go and look for a file that does not exist.
func TestAFreeformSessionGetsNoArtifact(t *testing.T) {
	r := recordingAgent()
	ws, store, wt := newRepo(t)
	e := New(Config{Agents: singleAgent(r), Store: store, Worktrees: wt, Workspace: ws, Model: "m"})
	t.Cleanup(func() { e.Close() })
	ctx := context.Background()

	f := freeformCard(3, "no document here")
	createFeature(t, store, f)
	if _, err := e.OpenFreeform(ctx, f); err != nil {
		t.Fatal(err)
	}
	opts := r.opts()
	if opts.ArtifactPath != "" {
		t.Errorf("freeform session was given an artifact at %q", opts.ArtifactPath)
	}
	if want := filepath.Join(ws.Root, f.WorktreePath()); opts.WorkDir != want {
		t.Errorf("WorkDir = %q, want the card's worktree %q", opts.WorkDir, want)
	}
	// The envelope is the one floor it keeps, so the cap must be real.
	if opts.MaxCredits <= 0 {
		t.Errorf("MaxCredits = %v, want the card's envelope enforced", opts.MaxCredits)
	}
	// No hint may name an artifact path: there is no file to name, and a
	// session pointed at one would go looking for it.
	for _, h := range opts.SystemHints {
		if strings.Contains(h, ".gummi/specs") || strings.Contains(h, f.SpecPath()) {
			t.Errorf("a freeform hint points at an artifact:\n%s", h)
		}
	}
}

// TestTheFreeformContractPromisesNoCeremony pins what the card's own hint
// says, and mostly what it does not: a session told to emit a verdict, fill
// a section or wait at a gate would invent the ceremony this kind exists to
// do without.
func TestTheFreeformContractPromisesNoCeremony(t *testing.T) {
	f := freeformCard(11, "no ceremony")
	hint := freeformContractHint(f, "/tmp/wt")
	// The ceremony is named only to be denied — an agent that has run
	// gummi's stages before will assume a document and a verdict unless it
	// is told otherwise, so silence is not the same as absence here.
	for _, want := range []string{"no design document", "no plan to write", "no verdict to emit"} {
		if !strings.Contains(hint, want) {
			t.Errorf("the freeform contract never says %q:\n%s", want, hint)
		}
	}
	// What it must never carry is the verdict grammar itself: a session
	// shown "VERDICT: pass" will emit one, and nothing here parses it.
	if strings.Contains(hint, "VERDICT:") {
		t.Errorf("the freeform contract shows the verdict grammar:\n%s", hint)
	}
	for _, want := range []string{string(f.ID), f.BranchName(), "/tmp/wt", "committed"} {
		if !strings.Contains(hint, want) {
			t.Errorf("the freeform contract never names %q — the session has no other way to learn it:\n%s", want, hint)
		}
	}
}

// TestOpenFreeformIsIdempotentPerCard: one session per card, so a second
// caller (the diff surface sending comments while the card page is open)
// reaches the session that holds the worktree rather than spawning a
// second writer into it.
func TestOpenFreeformIsIdempotentPerCard(t *testing.T) {
	r := recordingAgent()
	ws, store, wt := newRepo(t)
	e := New(Config{Agents: singleAgent(r), Store: store, Worktrees: wt, Workspace: ws, Model: "m"})
	t.Cleanup(func() { e.Close() })
	ctx := context.Background()

	f := freeformCard(4, "one session only")
	createFeature(t, store, f)
	a, err := e.OpenFreeform(ctx, f)
	if err != nil {
		t.Fatal(err)
	}
	b, err := e.OpenFreeform(ctx, f)
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Fatal("OpenFreeform returned a second session for the same card")
	}
	if r.count() != 1 {
		t.Errorf("backend spawned %d times, want 1", r.count())
	}
	if got := e.Freeform(f.ID); got != a {
		t.Errorf("Freeform(id) = %p, want %p", got, a)
	}
}

// TestAFreeformSessionRefusesACardInTheWorkflow: the session has no
// stage, no verdict and no gate, so opening one on a feature would be a
// second way to run that card which skips all three.
func TestAFreeformSessionRefusesACardInTheWorkflow(t *testing.T) {
	r := recordingAgent()
	ws, store, wt := newRepo(t)
	e := New(Config{Agents: singleAgent(r), Store: store, Worktrees: wt, Workspace: ws, Model: "m"})
	t.Cleanup(func() { e.Close() })

	f := feature(9, "a real feature", domain.StageImplement)
	createFeature(t, store, f)
	if _, err := e.OpenFreeform(context.Background(), f); err == nil {
		t.Fatal("OpenFreeform accepted a feature card")
	}
	if r.count() != 0 {
		t.Errorf("a backend was spawned for a refused card (%d sessions)", r.count())
	}
}

// TestAFreeformCardHoldsItsCardLock: the hold spans the conversation, not
// one turn — between two turns the worktree holds uncommitted work and the
// branch holds commits nothing has reviewed, and a landing arriving in
// that window is what the lock excludes. Dropping it is Close's job.
func TestAFreeformCardHoldsItsCardLock(t *testing.T) {
	r := recordingAgent()
	ws, store, wt := newRepo(t)
	e := New(Config{
		Agents: singleAgent(r), Store: store, Worktrees: wt, Workspace: ws, Model: "m",
		CardLocks: state.NewCardLocks(ws),
	})
	t.Cleanup(func() { e.Close() })
	ctx := context.Background()

	f := freeformCard(5, "locked while open")
	createFeature(t, store, f)
	ff, err := e.OpenFreeform(ctx, f)
	if err != nil {
		t.Fatal(err)
	}
	// A second holder outside this process's refcounted pool is excluded
	// while the session lives.
	if release, err := state.AcquireLock(ws.CardLockFile(f.ID)); err == nil {
		release()
		t.Fatal("the card lock was free while its freeform session was open")
	}
	if err := ff.Close(); err != nil {
		t.Fatal(err)
	}
	release, err := state.AcquireLock(ws.CardLockFile(f.ID))
	if err != nil {
		t.Fatalf("the card lock was still held after Close: %v", err)
	}
	release()
}

// TestAFreeformCardsToolSurfaceIsResolveAnnotationOnly pins what a
// freeform session may reach for: the review loop's resolve tool, and
// nothing else. No spec tools (there is no artifact) and no ask_user (the
// person is in the thread, so a reply is the answer).
func TestAFreeformCardsToolSurfaceIsResolveAnnotationOnly(t *testing.T) {
	tools := stageTools(domain.StageOpen, flavorStage, nil)
	if len(tools) != 1 || tools[0].Name != resolveToolName {
		var names []string
		for _, td := range tools {
			names = append(names, td.Name)
		}
		t.Fatalf("freeform tools = %v, want just %s", names, resolveToolName)
	}
}

// TestAFreeformCardResolvesItsDiffComments is the review loop the whole
// kind leans on: the reader's comments arrive as a turn, the agent marks
// each addressed through the same resolve_annotation tool a stage uses, and
// the open count burns down live. A freeform card is the simplest consumer
// of that machinery — one session, always the writer — and this asserts it
// is genuinely the same machinery rather than a second copy.
func TestAFreeformCardResolvesItsDiffComments(t *testing.T) {
	var resolved atomic.Int64
	ag := &agent.Fake{Responder: func(_ agent.SessionOpts, msg string) []agent.Event {
		// the compiled turn carries each comment's [id]; answer the first
		if id := resolved.Add(1); id == 1 {
			return []agent.Event{{Kind: agent.EventClientToolCall, ToolCall: &agent.ToolCall{
				ID: "call-1", Name: resolveToolName, Args: json.RawMessage(`{"id": 1}`),
			}}, {Kind: agent.EventMessage, Text: "fixed the error path"}, {Kind: agent.EventIdle}}
		}
		return []agent.Event{{Kind: agent.EventMessage, Text: "nothing else"}, {Kind: agent.EventIdle}}
	}}
	ag.Caps = agent.Capabilities{ClientTools: true, UsageEvents: true, Interrupt: true}
	ws, store, wt := newRepo(t)
	e := New(Config{Agents: singleAgent(ag), Store: store, Worktrees: wt, Workspace: ws, Model: "m"})
	t.Cleanup(func() { e.Close() })
	ctx := context.Background()

	f := freeformCard(6, "review me")
	createFeature(t, store, f)
	annID, err := store.AddDiffAnnotation(ctx, domain.DiffAnnotation{
		Feature: f.ID, File: "sketch.txt", Anchor: "a1",
		Excerpt: "hand-rolled", Comment: "this leaks on the error path too",
	}, time.Now())
	if err != nil {
		t.Fatal(err)
	}

	ff, err := e.OpenFreeform(ctx, f)
	if err != nil {
		t.Fatal(err)
	}
	anns, err := store.ListDiffAnnotations(ctx, f.ID)
	if err != nil {
		t.Fatal(err)
	}
	turn := CompileDiffComments(anns, true)
	if !strings.Contains(turn, "this leaks on the error path too") {
		t.Fatalf("the compiled turn does not carry the comment:\n%s", turn)
	}
	if err := ff.Send(ctx, turn); err != nil {
		t.Fatal(err)
	}
	waitFreeformIdle(t, ff)

	got, err := store.ListDiffAnnotations(ctx, f.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range got {
		if a.ID == annID && !a.Resolved {
			t.Errorf("comment [%d] is still open after the agent resolved it", a.ID)
		}
	}
}

// TestAFreeformSessionInheritsOpenComments: a conversation that idled out
// between "request changes" and the fix must not lose the request. The
// comments are in the store, so every backend this card spawns reads them
// in its opening hints — the same path a cold stage takes.
func TestAFreeformSessionInheritsOpenComments(t *testing.T) {
	r := recordingAgent()
	ws, store, wt := newRepo(t)
	e := New(Config{Agents: singleAgent(r), Store: store, Worktrees: wt, Workspace: ws, Model: "m"})
	t.Cleanup(func() { e.Close() })
	ctx := context.Background()

	f := freeformCard(7, "inherit my comments")
	createFeature(t, store, f)
	if _, err := store.AddDiffAnnotation(ctx, domain.DiffAnnotation{
		Feature: f.ID, File: "sketch.txt", Anchor: "a1",
		Excerpt: "hand-rolled", Comment: "name this something else",
	}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := e.OpenFreeform(ctx, f); err != nil {
		t.Fatal(err)
	}
	hints := strings.Join(r.opts().SystemHints, "\n")
	if !strings.Contains(hints, "name this something else") {
		t.Errorf("a fresh freeform backend did not inherit the open comment:\n%s", hints)
	}
}

// TestAFreeformCardCarriesOnAfterATopUp: the envelope is the one floor a
// freeform card keeps, so running into it must be a stop rather than an
// ending. The exhaustion latch lives on the backend, so a topped-up card
// gets a fresh one — otherwise "raise the envelope to carry on" would be a
// sentence with nothing behind it.
func TestAFreeformCardCarriesOnAfterATopUp(t *testing.T) {
	ag := agent.NewFake("ok")
	ag.Caps = agent.Capabilities{UsageEvents: true, Interrupt: true}
	ws, store, wt := newRepo(t)
	e := New(Config{Agents: singleAgent(ag), Store: store, Worktrees: wt, Workspace: ws, Model: "m"})
	t.Cleanup(func() { e.Close() })
	ctx := context.Background()

	f := freeformCard(8, "spend it all")
	createFeature(t, store, f)
	ff, err := e.OpenFreeform(ctx, f)
	if err != nil {
		t.Fatal(err)
	}
	// Stand in for the spend: latch the backend exhausted the way an
	// over-budget usage event would.
	ff.mu.Lock()
	spent := ff.sess
	ff.mu.Unlock()
	if !spent.markExhausted() {
		t.Fatal("precondition: the session was already exhausted")
	}

	// With nothing left, the turn is refused and says what would unblock it.
	if err := store.AddSpend(ctx, f.ID, 500, 0, 0, 0); err != nil {
		t.Fatal(err)
	}
	setEnvelope(t, store, f.ID, 100)
	err = ff.Send(ctx, "carry on")
	if err == nil {
		t.Fatal("an exhausted freeform card took another turn with nothing left")
	}
	if !strings.Contains(err.Error(), "raise it") {
		t.Errorf("the refusal does not say what would unblock it: %v", err)
	}

	// Topped up, the same card carries on — on a fresh backend that keeps
	// the conversation.
	setEnvelope(t, store, f.ID, 4000)
	if err := ff.Send(ctx, "carry on"); err != nil {
		t.Fatalf("a topped-up freeform card still refuses turns: %v", err)
	}
	waitFreeformIdle(t, ff)
	ff.mu.Lock()
	fresh := ff.sess
	ff.mu.Unlock()
	if fresh == spent {
		t.Error("the exhausted backend was reused; its cap is still the spent one")
	}
	var saw bool
	for _, m := range fresh.Snapshot().Transcript {
		if strings.Contains(m.Content, "carry on") {
			saw = true
		}
	}
	if !saw {
		t.Error("the respawned backend did not carry the conversation")
	}
}

// setEnvelope writes a card's envelope straight to the store, standing in
// for a top-up at the board without RaiseEnvelope's floor arithmetic (the
// point here is a spent envelope, which that floor exists to prevent).
func setEnvelope(t *testing.T, store *state.Store, id domain.FeatureID, to int) {
	t.Helper()
	ctx := context.Background()
	f, err := store.GetFeature(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	f.Budget.Envelope = to
	if err := store.UpdateFeature(ctx, &f); err != nil {
		t.Fatal(err)
	}
}

// TestAFreeformSessionIsNotToldTheWorkflowGovernsIt guards a contradiction
// the pty drive found: the repo-precedence hint every stage session gets
// asserts that gummi governs "the stage, the gates" and that "the workflow
// wins", and a freeform card has none of those — so on such a card that
// paragraph contradicts, in the same prompt, its own contract saying there
// is no gate and no verdict. What must survive is the half that is true of
// every card: never land it yourself, never run a second gummi.
func TestAFreeformSessionIsNotToldTheWorkflowGovernsIt(t *testing.T) {
	r := recordingAgent()
	ws, store, wt := newRepo(t)
	e := New(Config{Agents: singleAgent(r), Store: store, Worktrees: wt, Workspace: ws, Model: "m"})
	t.Cleanup(func() { e.Close() })

	f := freeformCard(9, "no contradictions")
	createFeature(t, store, f)
	if _, err := e.OpenFreeform(context.Background(), f); err != nil {
		t.Fatal(err)
	}
	hints := strings.Join(r.opts().SystemHints, "\n")
	for _, forbidden := range []string{"the workflow wins", "the stage, the gates", "skip review for small changes"} {
		if strings.Contains(hints, forbidden) {
			t.Errorf("a freeform session is told %q, which is not true of it:\n%s", forbidden, hints)
		}
	}
	for _, want := range []string{"never merge or land this branch yourself", "never run another\ngummi"} {
		if !strings.Contains(hints, want) {
			t.Errorf("the freeform precedence hint drops %q, which holds on every card", want)
		}
	}
}

// TestInterruptingAFreeformTurnCommitsWhatItWrote: a turn a reader stops
// has still written whatever it wrote before being stopped, and on a
// freeform card nothing else will commit it — the idle that normally does
// never arrives for a turn the backend abandoned. The pty drive found the
// stop unreachable at all; this pins both halves of it.
func TestInterruptingAFreeformTurnCommitsWhatItWrote(t *testing.T) {
	var interrupted atomic.Bool
	ag := &agent.Fake{Responder: func(opts agent.SessionOpts, _ string) []agent.Event {
		if err := os.WriteFile(filepath.Join(opts.WorkDir, "half.txt"), []byte("half\n"), 0o600); err != nil {
			t.Error(err)
		}
		// No idle: the turn is still in flight when the reader stops it.
		return []agent.Event{{Kind: agent.EventTextDelta, Text: "working"}}
	}}
	ag.Caps = agent.Capabilities{UsageEvents: true, Interrupt: true}
	ag.OnInterrupt = func() { interrupted.Store(true) }
	ws, store, wt := newRepo(t)
	e := New(Config{Agents: singleAgent(ag), Store: store, Worktrees: wt, Workspace: ws, Model: "m"})
	t.Cleanup(func() { e.Close() })
	ctx := context.Background()

	f := freeformCard(10, "stop me")
	createFeature(t, store, f)
	ff, err := e.OpenFreeform(ctx, f)
	if err != nil {
		t.Fatal(err)
	}
	if err := ff.Send(ctx, "go slowly"); err != nil {
		t.Fatal(err)
	}
	if !ff.Snapshot().Busy {
		t.Fatal("precondition: the session is not busy")
	}
	// The stand-in writes on its own goroutine, so wait for the turn to
	// have actually written something before stopping it — otherwise the
	// test asserts the checkpoint committed a file that did not exist yet,
	// and only loses that race under load.
	tree := filepath.Join(ws.Root, f.WorktreePath())
	waitForFile(t, filepath.Join(tree, "half.txt"))
	if err := e.InterruptFreeform(ctx, f.ID); err != nil {
		t.Fatal(err)
	}
	if !interrupted.Load() {
		t.Error("the backend was never interrupted")
	}
	if ff.Snapshot().Busy {
		t.Error("the session still reports itself busy after the stop")
	}
	if files := gitOut(t, tree, "show", "--name-only", "--format=", "HEAD"); !strings.Contains(files, "half.txt") {
		t.Errorf("the interrupted turn's work was not committed:\n%s", files)
	}
	// And the conversation survives the stop: the backend is kept, so the
	// next turn continues rather than starting a new session.
	if err := ff.Send(ctx, "carry on"); err != nil {
		t.Errorf("the session refuses turns after a stop: %v", err)
	}
}

// TestAFreeformCardsWorktreeIsSettledOnShutdown: every other card's work
// reaches its branch through a stage that ends; a freeform card's reaches
// it through the commit at the end of each turn, so a board that quits
// mid-turn is the one moment its work could be stranded. The pty drive
// found exactly that — an untracked file left behind by a quit.
func TestAFreeformCardsWorktreeIsSettledOnShutdown(t *testing.T) {
	ag := &agent.Fake{Responder: func(opts agent.SessionOpts, _ string) []agent.Event {
		if err := os.WriteFile(filepath.Join(opts.WorkDir, "inflight.txt"), []byte("x\n"), 0o600); err != nil {
			t.Error(err)
		}
		return []agent.Event{{Kind: agent.EventTextDelta, Text: "still going"}}
	}}
	ag.Caps = agent.Capabilities{UsageEvents: true, Interrupt: true}
	ws, store, wt := newRepo(t)
	e := New(Config{Agents: singleAgent(ag), Store: store, Worktrees: wt, Workspace: ws, Model: "m"})
	ctx := context.Background()

	f := freeformCard(14, "quit on me")
	createFeature(t, store, f)
	ff, err := e.OpenFreeform(ctx, f)
	if err != nil {
		t.Fatal(err)
	}
	if err := ff.Send(ctx, "go slowly"); err != nil {
		t.Fatal(err)
	}
	tree := filepath.Join(ws.Root, f.WorktreePath())
	waitForFile(t, filepath.Join(tree, "inflight.txt"))
	if err := e.Close(); err != nil {
		t.Fatal(err)
	}
	if out := gitOut(t, tree, "status", "--porcelain"); strings.TrimSpace(out) != "" {
		t.Errorf("the shutdown stranded work in the worktree:\n%s", out)
	}
	if files := gitOut(t, tree, "show", "--name-only", "--format=", "HEAD"); !strings.Contains(files, "inflight.txt") {
		t.Errorf("the in-flight turn's work never reached the branch:\n%s", files)
	}
}

// waitForFile polls until path exists. The stand-in agents write on their
// own goroutines, so a test that asserts what a turn left behind has to
// wait for the turn to have left it — a check that only fails under load
// is worse than no check.
func waitForFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.After(testWaitTimeout)
	for {
		if _, err := os.Stat(path); err == nil {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("%s never appeared", path)
		case <-time.After(5 * time.Millisecond):
		}
	}
}
