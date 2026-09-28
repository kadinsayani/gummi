package ui

import (
	"context"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/morphis/gummi/internal/agent"
	"github.com/morphis/gummi/internal/domain"
	"github.com/morphis/gummi/internal/reentry"
	"github.com/morphis/gummi/internal/webapi"
)

// The TUI's chip never spends on enter (§9.2, chip.go goOnEnter): a
// RerunInPlace reading needs y. The web chip marks that "go" as danger and
// asks before it goes.
func TestASpendingChipAsksBeforeItGoes(t *testing.T) {
	b, _, eng, f, _ := headlessBoard(t, agent.NewFake("ok"))
	ctx := context.Background()
	waitBoard(t, b, func(bd webapi.Board) bool { return len(bd.Rows) == 1 })
	set := func(out reentry.Outcome) {
		if err := b.Do(ctx, func(m *Shell) tea.Cmd {
			m.setChip(&reentryReading{id: f.ID, line: "tighten the plan", out: out, goOnEnter: goOnEnter(out)})
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	set(reentry.Outcome{Action: reentry.Rewind, Target: domain.StagePlan, Path: []domain.Stage{domain.StagePlan}, Note: "tighten the plan"})
	rew, err := b.Card(ctx, "FD-001")
	if err != nil {
		t.Fatal(err)
	}
	spend := reentry.Outcome{Action: reentry.RerunInPlace, Target: domain.StagePlan, Note: "tighten the plan"}
	if goOnEnter(spend) {
		t.Fatal("precondition: a rerun is a spend the TUI keeps off enter")
	}
	set(spend)
	c, err := b.Card(ctx, "FD-001")
	if err != nil {
		t.Fatal(err)
	}
	d := c.Decision
	t.Logf("rewind chip options: %+v", rew.Decision.Options)
	t.Logf("spend  chip options: %+v", d.Options)
	if d.Options[0].ID == webOptionGo && !d.Options[0].Danger {
		t.Errorf("spending chip serves go at index 0 (the page's default highlight, what a bare enter answers) with no danger marker; nothing tells the page enter must not take it")
	}
	if _, err := b.Answer(ctx, "FD-001", webapi.AnswerRequest{Ref: d.Ref, Option: webOptionGo, Against: d.Against.Token}, "Simon"); err != nil {
		t.Logf("answer go refused: %v", err)
		return
	}
	deadline := time.Now().Add(5 * time.Second)
	for eng.Get(f.ID) == nil && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if eng.Get(f.ID) != nil {
		t.Errorf("Answer(go) on a spending chip started a %s run with no confirm (TUI: enter does nothing, only y)", f.Stage)
	}
}
