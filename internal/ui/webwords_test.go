package ui

import (
	"context"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/morphis/gummi/internal/agent"
	"github.com/morphis/gummi/internal/domain"
	"github.com/morphis/gummi/internal/webapi"
)

// The words carried with an answer are prose for that answer: a "/verb"
// among them is refused, never routed by the parser — "/approve" sent as
// the words of "start the architect" must not cross the gate.
func TestAnAnswersWordsAreNeverACommand(t *testing.T) {
	b, _, _, f, _ := headlessBoard(t, agent.NewFake("ok"))
	ctx := context.Background()
	waitBoard(t, b, func(bd webapi.Board) bool { return len(bd.Rows) == 1 })
	c, err := b.Card(ctx, "FD-001")
	if err != nil || c.Decision == nil {
		t.Fatalf("card %+v err %v", c, err)
	}
	var words string
	for _, o := range c.Decision.Options {
		t.Logf("option %s %q words=%v", o.ID, o.Label, o.Words)
		if o.Words {
			words = o.ID
		}
	}
	if words == "" {
		t.Skip("no word-eating option at this stop")
	}
	_, err = b.Answer(ctx, "FD-001", webapi.AnswerRequest{Ref: c.Decision.Ref, Option: words, Words: "/approve", Against: c.Decision.Against.Token}, "Simon")
	t.Logf("answer err: %v", err)
	time.Sleep(200 * time.Millisecond)
	var stage domain.Stage
	_ = b.Do(ctx, func(m *Shell) tea.Cmd {
		if r, ok := m.rowByID(f.ID); ok {
			stage = r.F.Stage
		}
		return nil
	})
	if stage != domain.StagePlan {
		t.Errorf("words %q on option %q moved the card to %s", "/approve", words, stage)
	}
	if err == nil {
		t.Errorf("a /verb line was accepted as the words of %q", words)
	}
}
