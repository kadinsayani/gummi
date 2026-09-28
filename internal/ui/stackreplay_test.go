package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/morphis/gummi/internal/domain"
	"github.com/morphis/gummi/internal/engine"
	"github.com/morphis/gummi/internal/webapi"
)

// A replay walk the board's own ticks finished says what moved and the
// push each moved branch needs (§18.5) — the lines only a manual restack
// used to print — and reloads the board, whose git columns it staled.
func TestAFinishedAutomaticReplaySaysItsPushes(t *testing.T) {
	m := &Shell{}
	w := &engine.StackReplay{
		Cards: []domain.FeatureID{"FD-002", "FD-003"},
		Push:  []string{engine.PushCommand("feat/eval"), engine.PushCommand("feat/cli")},
		At:    time.Now(),
	}
	cmd := m.onStackTick(stackTickMsg{res: engine.StackTickResult{Stack: "chain", Settled: w}})
	if cmd == nil {
		t.Fatal("a finished walk said nothing")
	}
	n, ok := cmd().(noticeMsg)
	if !ok || !n.reload || n.isErr {
		t.Fatalf("notice = %#v, want a reloading notice", n)
	}
	for _, want := range []string{"FD-002, FD-003", "git push --force-with-lease origin feat/eval", "git push --force-with-lease origin feat/cli"} {
		if !strings.Contains(n.text, want) {
			t.Errorf("notice %q lacks %q", n.text, want)
		}
	}

	// a single step of a walk still going stays silent: the board's
	// marker says it, and a notice per step is noise
	step := m.onStackTick(stackTickMsg{res: engine.StackTickResult{Stack: "chain", Restacked: "FD-002", Push: w.Push[0]}})
	if step != nil {
		if n, ok := step().(noticeMsg); ok && n.text != "" {
			t.Fatalf("a replay step spoke: %q", n.text)
		}
	}

	// the stack's projection carries the walk for a page opened later
	st := withStackReplay(webapi.Stack{ID: "chain"}, *w)
	if strings.Join(st.Replayed, ",") != "FD-002,FD-003" || len(st.Push) != 2 || !st.ReplayedAt.Equal(w.At) {
		t.Fatalf("stack projection = %+v, want the walk's cards, pushes and time", st)
	}
}

// What the notice says is only half of it: a status pill is one row, so a
// multi-line notice rendered as one showed "…push them yourself:" and
// never a push line, and its width pushed the key hints off the bar. The
// notice must reach the screen whole, in the band above the status bar,
// with the bar keeping its hints.
func TestAFinishedReplayShowsItsPushLinesOnScreen(t *testing.T) {
	m := populatedShell(120, 34)
	w := engine.StackReplay{
		Cards: []domain.FeatureID{"FD-002", "FD-003"},
		Push:  []string{engine.PushCommand("feat/eval"), engine.PushCommandTo("fork", "feat/cli", "cli")},
	}
	m.notice = noticeMsg{text: stackReplayNotice("chain", w), reload: true}
	screen := stripANSI(populatedShellView(m))
	for _, want := range []string{
		"replayed FD-002, FD-003 onto their new base",
		"  git push --force-with-lease origin feat/eval",
		"  git push --force-with-lease fork feat/cli:cli",
	} {
		if !strings.Contains(screen, want) {
			t.Errorf("screen lacks %q:\n%s", want, screen)
		}
	}
	lines := strings.Split(screen, "\n")
	bar := lines[len(lines)-1]
	for _, l := range lines {
		if strings.Contains(l, "gummi") && strings.Contains(l, "?") {
			bar = l
		}
	}
	if strings.Contains(bar, "replayed") || !strings.Contains(bar, "help") {
		t.Errorf("the status bar carries the notice or lost its key hints: %q", bar)
	}

	// one card is "it", not "them"
	one := stackReplayNotice("chain", engine.StackReplay{Cards: []domain.FeatureID{"FD-002"}, Push: []string{engine.PushCommand("feat/eval")}})
	if !strings.Contains(one, "replayed FD-002 onto its new base") || !strings.Contains(one, "push it yourself:") {
		t.Errorf("single-card notice = %q", one)
	}
}
