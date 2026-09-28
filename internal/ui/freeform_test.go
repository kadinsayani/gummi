package ui

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/morphis/gummi/internal/cardrun"
	"github.com/morphis/gummi/internal/domain"
	"github.com/morphis/gummi/internal/state"
	"github.com/morphis/gummi/internal/ui/theme"
)

// freeformRow builds a freeform card's row, at the one stage such a card
// ever holds.
func freeformRow(num int, title string, wt bool) featureRow {
	id, _ := domain.NewID(domain.KindFreeform, num)
	slug, _ := domain.Slugify(title)
	f := domain.Feature{
		ID: id, Num: num, Kind: domain.KindFreeform, Title: title, Slug: slug,
		Stage: domain.StageOpen, BranchScheme: domain.BranchSchemeKind,
		Budget:  domain.Budget{Envelope: 400},
		Profile: "thrifty", CreatedAt: fixedTime, UpdatedAt: fixedTime,
	}
	return featureRow{F: f, HasWorktree: wt}
}

// freeformShell is a detached board whose selected card is a freeform one.
func freeformShell(t *testing.T, w, h int, wt bool) *Shell {
	t.Helper()
	m := NewShell(theme.GummiDark(), "v0.1.0-test")
	m.now = func() time.Time { return fixedTime }
	m.rows = []featureRow{
		row(42, "dark mode", domain.StageImplement, "thrifty", true),
		freeformRow(12, "drop the leaked pty fd", wt),
	}
	m.sel = 1
	model, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return model.(*Shell)
}

// TestAFreeformCardsStripNamesItsBranch: the five stages faint with none
// of them lit would say the card is somewhere in the workflow, which is
// the one thing that is not true of it. Its branch is what a reader of
// that row actually wants.
func TestAFreeformCardsStripNamesItsBranch(t *testing.T) {
	m := freeformShell(t, 120, 34, true)
	out := ansi.Strip(m.threadView(120, 34))
	if !strings.Contains(out, "freeform") {
		t.Errorf("the masthead never says the card is freeform:\n%s", out)
	}
	if !strings.Contains(out, "ff/drop-the-leaked-pty-fd") {
		t.Errorf("the masthead never names the card's branch:\n%s", out)
	}
	for _, stage := range []string{"plan", "implement", "verify"} {
		if strings.Contains(out, stage) {
			t.Errorf("the freeform card's page mentions the workflow stage %q:\n%s", stage, out)
		}
	}
}

// TestAFreeformCardSaysHowToStartIt: with no session and nothing on its
// branch, the thread must still say what to do — a blank page under a
// card that claims to be in progress teaches nothing.
func TestAFreeformCardSaysHowToStartIt(t *testing.T) {
	m := freeformShell(t, 120, 34, false)
	out := ansi.Strip(m.threadView(120, 34))
	if !strings.Contains(out, "type below to start") {
		t.Errorf("an unstarted freeform card does not say how to start it:\n%s", out)
	}
}

// TestAStartedFreeformCardPointsAtItsDiff is the other half: after a
// restart the conversation is gone (a freeform session is not persisted)
// but the work is on the branch, and saying nothing there is what would
// read as "my card is gone".
func TestAStartedFreeformCardPointsAtItsDiff(t *testing.T) {
	m := freeformShell(t, 120, 34, true)
	out := ansi.Strip(m.threadView(120, 34))
	if !strings.Contains(out, "alt+d") {
		t.Errorf("a worked-on freeform card does not point at its diff:\n%s", out)
	}
	if !strings.Contains(out, "as long as the board does") {
		t.Errorf("nothing explains why the conversation is not here:\n%s", out)
	}
}

// TestAFreeformCardOffersNoWorkflowActions: "not shown" and "not
// available" must not diverge, so every workflow-shaped row is withheld
// from a card that has no workflow — and the two endings it does have are
// offered.
func TestAFreeformCardOffersNoWorkflowActions(t *testing.T) {
	in := nextInput{stage: domain.StageOpen, kind: domain.KindFreeform, hasWorktree: true}
	r := freeformRow(12, "drop the leaked pty fd", true)
	acts := cardActionsFor(in, r)

	ids := map[string]bool{}
	for _, a := range acts {
		ids[a.id] = true
	}
	for _, withheld := range []string{"advance", "spec", "verify", "bounce", "gate", "ask"} {
		if ids[withheld] {
			t.Errorf("a freeform card offers %q, which it has no workflow for", withheld)
		}
	}
	for _, want := range []string{"diff", "merge", "handoff"} {
		if !ids[want] {
			t.Errorf("a freeform card does not offer %q", want)
		}
	}
}

// TestAFreeformCardsNextStepsAreItsReviewLoop: it has no gate to teach a
// reader what comes next, so the answer set has to say it — read the
// diff, land it, or keep the branch and close.
func TestAFreeformCardsNextStepsAreItsReviewLoop(t *testing.T) {
	in := nextInput{stage: domain.StageOpen, kind: domain.KindFreeform, hasWorktree: true}
	var ids []string
	for _, a := range nextActions(in) {
		ids = append(ids, a.id)
	}
	want := []string{"diff", "merge", "handoff"}
	if strings.Join(ids, ",") != strings.Join(want, ",") {
		t.Errorf("next steps = %v, want %v", ids, want)
	}

	// Before anything has run there is nothing to read and nothing to
	// land: a row offering either would be a row that refuses.
	if acts := nextActions(nextInput{stage: domain.StageOpen, kind: domain.KindFreeform}); len(acts) != 0 {
		t.Errorf("an unstarted freeform card recommends %v, want nothing", acts)
	}
}

// TestAFreeformCardsKeysDropTheWorkflowOnes: the status bar and the ?
// overlay render from one slice, so a key that refuses on this card must
// not appear in it.
func TestAFreeformCardsKeysDropTheWorkflowOnes(t *testing.T) {
	m := freeformShell(t, 120, 34, true)
	keys := map[string]bool{}
	for _, b := range m.boardBindings() {
		keys[b.key] = true
	}
	for _, withheld := range []string{"g", "s", "b", "v", "A"} {
		if keys[withheld] {
			t.Errorf("the key list offers %q on a freeform card", withheld)
		}
	}
	// What it keeps: the diff, the branch verbs and its endings.
	for _, want := range []string{"d", "m", "h", "r", "c"} {
		if !keys[want] {
			t.Errorf("the key list drops %q, which works on a freeform card", want)
		}
	}
}

// TestTheComposerOnAFreeformCardNamesWhatItCanDo: the placeholder must not
// advertise approve/send-back/gate, which is what this surface is built
// around and exactly what such a card does not have.
func TestTheComposerOnAFreeformCardNamesWhatItCanDo(t *testing.T) {
	got := composerPlaceholder(domain.KindFreeform)
	if got == placeholderText {
		t.Fatal("a freeform card gets the workflow composer's placeholder")
	}
	for _, want := range []string{"branch", "alt+d", "land"} {
		if !strings.Contains(got, want) {
			t.Errorf("the freeform placeholder never mentions %q: %q", want, got)
		}
	}
}

// TestCreatingAFreeformCardMintsItOutsideTheGraph drives the real creation
// dialog and checks what lands in the store: a card at the stage that has
// no edges, on its own branch, with no draft artifact anywhere.
func TestCreatingAFreeformCardMintsItOutsideTheGraph(t *testing.T) {
	m, root := newWorkspace(t)
	m.SetCopilotHint(false)
	m = pump(t, m, m.Init())
	m.Overlay.Push(m.openCardForm(domain.CardType{Kind: domain.KindFreeform}))
	m = typeString(t, m, "Drop the leaked pty fd")
	m = press(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})

	fs, err := m.store.ListFeatures(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(fs) != 1 {
		t.Fatalf("minted %d cards, want 1", len(fs))
	}
	f := fs[0]
	if f.Kind != domain.KindFreeform {
		t.Errorf("kind = %q, want freeform", f.Kind)
	}
	if f.Stage != domain.StageOpen {
		t.Errorf("stage = %q, want %q — a freeform card starts outside the graph", f.Stage, domain.StageOpen)
	}
	if got := f.BranchName(); got != "ff/drop-the-leaked-pty-fd" {
		t.Errorf("branch = %q", got)
	}
	// No artifact was seeded: its thread is the record.
	if entries, _ := os.ReadDir(filepath.Join(root, ".gummi", "state", "drafts")); len(entries) != 0 {
		t.Errorf("a freeform card left %d draft(s) behind", len(entries))
	}
}

// TestLandingAFreeformCardFromTheBoard is the ending, driven the way a
// person drives it: m on the card, then the commit message. It has no
// verify stamp and no stage to be at, and it still reaches main and closes
// — through the one store method that may take it out of StageOpen.
func TestLandingAFreeformCardFromTheBoard(t *testing.T) {
	m, root := newWorkspace(t)
	m.SetCopilotHint(false)
	m = pump(t, m, m.Init())
	m.Overlay.Push(m.openCardForm(domain.CardType{Kind: domain.KindFreeform}))
	m = typeString(t, m, "Drop the leaked pty fd")
	m = press(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})

	ctx := context.Background()
	fs, err := m.store.ListFeatures(ctx)
	if err != nil || len(fs) != 1 {
		t.Fatalf("features = %v, err = %v", fs, err)
	}
	f := fs[0]
	// Cut the tree and leave a commit on it, the way a turn would.
	if _, err := m.wt.Ensure(ctx, &f); err != nil {
		t.Fatal(err)
	}
	commitWork(t, root, string(f.ID))
	m = pump(t, m, m.loadRows)

	before := headSHA(t, root)
	// Creating a freeform card opens its page (work is about to happen
	// there), and on the card page every printable key belongs to the
	// composer — so the board is where m is a verb.
	m = press(t, m, tea.KeyPressMsg{Code: tea.KeyEsc})
	m.sel = 0
	m = press(t, m, tea.KeyPressMsg{Code: 'm', Text: "m"})
	if _, ok := m.Overlay.Top().(*commitMsgDialog); !ok {
		t.Fatalf("m did not open the commit-message dialog (notice %q)", m.notice.text)
	}
	typeMessage(t, m, "fix(copilot): drop the pty fd leaked on idle timeout")
	m = press(t, m, tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	if m.notice.isErr || !strings.Contains(m.notice.text, "squash-merged") {
		t.Fatalf("merge notice = %q (err=%v)", m.notice.text, m.notice.isErr)
	}
	m = pump(t, m, m.loadRows)

	if got := headSHA(t, root); got == before {
		t.Fatalf("main did not move: %s", got)
	}
	got, err := m.store.GetFeature(ctx, f.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Stage != domain.StageDone {
		t.Errorf("stage after landing = %q, want done", got.Stage)
	}
}

// headSHA is main's current commit in the managed checkout.
func headSHA(t *testing.T, root string) string {
	t.Helper()
	out, err := exec.CommandContext(context.Background(), "git", "-C", root, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}

// TestTheArtifactChordAnswersOnAFreeformCard: the tab bar does not draw an
// artifact tab for a card with no document, so the chord that names it owes
// an answer rather than mounting a view onto nothing.
func TestTheArtifactChordAnswersOnAFreeformCard(t *testing.T) {
	m := freeformShell(t, 120, 34, true)
	m.cardOpen = true
	if bar := ansi.Strip(m.cardTabBar(cardTabThread, 120)); strings.Contains(bar, "spec") {
		t.Errorf("the tab bar offers a document on a freeform card: %q", bar)
	}
	cmd, handled := m.cardTabKey("alt+s")
	if !handled {
		t.Fatal("alt+s was not answered on a freeform card")
	}
	if cmd != nil {
		t.Error("alt+s mounted something on a card with no document")
	}
	if !strings.Contains(m.notice.text, "no document") {
		t.Errorf("notice = %q, want it to say why", m.notice.text)
	}
}

// TestHandOffIsOfferedOnAFreeformCard: hand-off is stage-gated everywhere
// else because it is an ending and an unfinished card has nothing to end.
// On a freeform card only the person can say when the work is finished, so
// the stage refusal must not fire.
func TestHandOffIsOfferedOnAFreeformCard(t *testing.T) {
	m := freeformShell(t, 120, 34, true)
	m.handleKey(tea.KeyPressMsg{Code: 'h', Text: "h"})
	if strings.Contains(m.notice.text, "hand-off ends a verified card") {
		t.Errorf("hand-off was refused on a freeform card: %q", m.notice.text)
	}
}

// TestTheFreeformCommandIsInTheVocabulary: the kind row of `n` reaches a
// freeform card, but a reader who knows what they want types the word. The
// entry has no accelerator, so the menu and the composer's slash line are
// the only places it can be found.
func TestTheFreeformCommandIsInTheVocabulary(t *testing.T) {
	m := freeformShell(t, 120, 34, true)
	var found *command
	for _, c := range m.globalCommands() {
		if c.name == "freeform" {
			found = &c
			break
		}
	}
	if found == nil {
		t.Fatal("no freeform entry in the command vocabulary")
	}
	if found.key != "" {
		t.Errorf("the freeform command took the accelerator %q", found.key)
	}
	if cmd := m.runCommand(found.id); cmd != nil {
		t.Error("opening the creation dialog should need no command")
	}
	d, ok := m.Overlay.Top().(*cardForm)
	if !ok {
		t.Fatalf("the freeform command did not open the creation dialog")
	}
	if d.Kind() != domain.KindFreeform {
		t.Errorf("the dialog opened preset to %q, want freeform", d.Kind())
	}
}

// TestTheStatsTabReportsAFreeformCardsBill: a freeform card has no passes
// — nothing mirrors a stage enter for it — but it has a real bill, and the
// pty drive caught this surface saying "nothing has run on this card yet"
// in front of a card that had spent nine thousand credits.
func TestTheStatsTabReportsAFreeformCardsBill(t *testing.T) {
	f := freeformRow(12, "drop the leaked pty fd", true).F
	f.Spend = domain.Spend{Credits: 9036}
	run := cardrun.Report(cardrun.Input{
		Feature: f,
		Spend: []state.StageSpend{
			{Stage: domain.StageOpen, Role: "implementer", Model: "stand-in", Credits: 9036},
		},
	})
	out := ansi.Strip(strings.Join(statsLines(theme.New(theme.GummiDark()), run, 90), "\n"))
	if strings.Contains(out, "nothing has run on this card yet") {
		t.Errorf("the stats tab denies a spent card ran:\n%s", out)
	}
	for _, want := range []string{"WHERE IT WENT", "9036", "THE ENVELOPE"} {
		if !strings.Contains(out, want) {
			t.Errorf("the stats tab never reports %q:\n%s", want, out)
		}
	}

	// A card that genuinely has not run still says so.
	bare := cardrun.Report(cardrun.Input{Feature: freeformRow(13, "not started", false).F})
	if !strings.Contains(ansi.Strip(strings.Join(statsLines(theme.New(theme.GummiDark()), bare, 90), "\n")),
		"nothing has run on this card yet") {
		t.Error("an unstarted card no longer says nothing has run")
	}
}

// TestWhileAFreeformTurnRunsTheAnswerIsToStopIt: a turn in flight owns the
// screen, and stopping it is the only answer worth offering — nothing else
// on this board stops an interactive session, and the deps picker (which p
// otherwise opens) must not be what p means while one is running.
func TestWhileAFreeformTurnRunsTheAnswerIsToStopIt(t *testing.T) {
	busy := nextInput{stage: domain.StageOpen, kind: domain.KindFreeform, hasWorktree: true, freeformBusy: true}
	var ids []string
	for _, a := range nextActions(busy) {
		ids = append(ids, a.id)
	}
	if len(ids) != 1 || ids[0] != "pause" {
		t.Errorf("answers while a turn runs = %v, want just the stop", ids)
	}

	r := freeformRow(12, "drop the leaked pty fd", true)
	offered := map[string]bool{}
	for _, a := range cardActionsFor(busy, r) {
		offered[a.id] = true
	}
	if !offered["pause"] {
		t.Error("the inventory offers no way to stop a turn in flight")
	}
	if offered["deps"] {
		t.Error("the dependency picker is what p would open mid-turn")
	}

	// Between turns it is the review loop again, and p is the picker.
	idle := busy
	idle.freeformBusy = false
	offered = map[string]bool{}
	for _, a := range cardActionsFor(idle, r) {
		offered[a.id] = true
	}
	if offered["pause"] || !offered["deps"] {
		t.Error("between turns p should open the dependency picker, not a stop")
	}
}

// TestDeletingAFreeformCardLeavesTheWorkspaceStanding is the regression
// test for the worst bug this feature had: the delete path removed the
// card's artifact with os.RemoveAll over filepath.Join(root,
// ArtifactPath()), and a freeform card's artifact path is empty — so it
// deleted the repository. A pty drive caught it; nothing in the unit tests
// could have, because none of them deleted a card with no document.
func TestDeletingAFreeformCardLeavesTheWorkspaceStanding(t *testing.T) {
	m, root := newWorkspace(t)
	m.SetCopilotHint(false)
	m = pump(t, m, m.Init())
	m.Overlay.Push(m.openCardForm(domain.CardType{Kind: domain.KindFreeform}))
	m = typeString(t, m, "Drop the leaked pty fd")
	m = press(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})

	ctx := context.Background()
	fs, err := m.store.ListFeatures(ctx)
	if err != nil || len(fs) != 1 {
		t.Fatalf("features = %v, err = %v", fs, err)
	}
	f := fs[0]
	if _, err := m.wt.Ensure(ctx, &f); err != nil {
		t.Fatal(err)
	}
	m = pump(t, m, m.deleteFeature(f.ID))

	// The card is gone…
	if left, err := m.store.ListFeatures(ctx); err != nil || len(left) != 0 {
		t.Errorf("features after the delete = %v (err %v), want none", left, err)
	}
	// …and everything that is not the card is still there.
	for _, keep := range []string{"README.md", ".git", ".gummi"} {
		if _, err := os.Stat(filepath.Join(root, keep)); err != nil {
			t.Fatalf("deleting a freeform card removed %s from the workspace: %v", keep, err)
		}
	}
	if _, err := os.Stat(filepath.Join(root, ".gummi", "worktrees", string(f.ID))); !os.IsNotExist(err) {
		t.Errorf("the card's own worktree survived the delete (stat err = %v)", err)
	}
}
