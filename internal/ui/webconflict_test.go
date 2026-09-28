package ui

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/morphis/gummi/internal/agent"
	"github.com/morphis/gummi/internal/webapi"
)

// A stale answer to a stop, on a card that now pins nothing at the same
// stage (an interactive session mid-turn, a conducted card, one driven
// elsewhere), is a 409 "moved" — not a nil dereference that drops the
// connection.
func TestAStaleAnswerOnACardThatPinsNothingIsMoved(t *testing.T) {
	b, _, _, _, _ := headlessBoard(t, agent.NewFake("ok"))
	ctx := context.Background()
	waitBoard(t, b, func(bd webapi.Board) bool { return len(bd.Rows) == 1 })
	c, err := b.Card(ctx, "FD-001")
	if err != nil || c.Decision == nil {
		t.Fatalf("card %+v err %v", c, err)
	}
	now := c
	now.Decision = nil // what Card serves while the architect is mid-turn
	var got error
	func() {
		defer func() {
			if p := recover(); p != nil {
				t.Fatalf("answeredOrMoved panicked on a card that pins nothing: %v", p)
			}
		}()
		got = b.answeredOrMoved(ctx, "FD-001", c.Decision.Ref, c.Decision.Against.Token, now)
	}()
	we, ok := IsWebError(got)
	if !ok || we.Code != WebConflict || we.Reason != webapi.ConflictMoved {
		t.Fatalf("got %v, want a 409 moved", got)
	}
	if !strings.Contains(we.Text, "nothing is waiting on you") {
		t.Errorf("moved text %q does not say nothing waits", we.Text)
	}
}

// Two people answer the same stop: the second is told who answered it and
// what they chose — even when the first answer left the card on its stage
// with the same decision ref ("stop here" at a stop parks the run, and the
// same stop stays pinned with another answer set), where the log has no
// crossing to name the answerer from.
func TestTheSecondAnswerToAStopNamesTheFirst(t *testing.T) {
	b, _, _, _, _ := headlessBoard(t, agent.NewFake("ok"))
	ctx := context.Background()
	waitBoard(t, b, func(bd webapi.Board) bool { return len(bd.Rows) == 1 })
	answer := func(who, opt string) (webapi.Decision, error) {
		t.Helper()
		c, err := b.Card(ctx, "FD-001")
		if err != nil || c.Decision == nil {
			t.Fatalf("card %+v err %v", c, err)
		}
		d := *c.Decision
		if !slices.ContainsFunc(d.Options, func(o webapi.Option) bool { return o.ID == opt }) {
			t.Fatalf("no %q on %+v", opt, d.Options)
		}
		_, err = b.Answer(ctx, "FD-001", webapi.AnswerRequest{Ref: d.Ref, Option: opt, Against: d.Against.Token}, who)
		return d, err
	}
	// start the architect: the run ends at the design gate, which offers
	// "stop here"
	if _, err := answer("carol", "run"); err != nil {
		t.Fatal(err)
	}
	for i := 0; ; i++ {
		if c, _ := b.Card(ctx, "FD-001"); c.Decision != nil && c.Decision.Kind == webapi.DecisionGate {
			break
		}
		if i == 100 {
			t.Fatal("the design gate never came up")
		}
		time.Sleep(20 * time.Millisecond)
	}
	seen, err := answer("alice", "pause")
	if err != nil {
		t.Fatalf("alice's stop here: %v", err)
	}
	var now webapi.Card
	for range 50 {
		if now, _ = b.Card(ctx, "FD-001"); now.Decision == nil || now.Decision.Against.Token != seen.Against.Token {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if now.Decision == nil || now.Decision.Ref != seen.Ref || now.Decision.Against.Token == seen.Against.Token {
		t.Fatalf("stop here left %+v; the race needs the same stop with another revision", now.Decision)
	}
	_, err = b.Answer(ctx, "FD-001", webapi.AnswerRequest{Ref: seen.Ref, Option: "advance", Against: seen.Against.Token}, "bob")
	we, ok := IsWebError(err)
	if !ok || we.Reason != webapi.ConflictAnswered {
		t.Fatalf("bob got %v (%+v), want 409 answered by alice", err, we)
	}
	if we.By != "alice" || we.Text != "answered by alice — stop here" {
		t.Errorf("answered by %q (%q), want alice — stop here", we.By, we.Text)
	}
}

// A confirmation's yes is the token it was asked with, and it answers
// that one question, once: a second confirmation the same flow reaches on
// the same card — even one asking the very same words — is asked on its
// own, and a token for a question that now reads differently answers
// nothing.
func TestAConfirmTokenAnswersOneQuestionOnce(t *testing.T) {
	b, _, _, _, _ := headlessBoard(t, agent.NewFake("ok"))
	ctx := context.Background()
	waitBoard(t, b, func(bd webapi.Board) bool { return len(bd.Rows) == 1 })
	run := func(confirm, detail string) ([]string, webOutcome) {
		t.Helper()
		var fired []string
		out, err := b.intent(ctx, "FD-001", webInput{confirm: confirm}, webWait, func(m *Shell, r featureRow) (tea.Cmd, error) {
			// two questions on the stack at once, word for word the same
			for _, which := range []string{"first", "second"} {
				m.Overlay.Push(&confirmDialog{id: "confirm-delete", card: r.F.ID, question: "delete FD-001?", detail: detail, onConfirm: func() tea.Cmd {
					fired = append(fired, which)
					return nil
				}})
			}
			return nil, nil
		})
		if err != nil {
			t.Fatal(err)
		}
		return fired, out
	}
	fired, out := run("", "Dark mode")
	if len(fired) != 0 || out.needs != webapi.ActionNeedsConfirm || out.confirm == "" || out.question != "delete FD-001?\nDark mode" {
		t.Fatalf("unconfirmed: fired %v, outcome %+v; want the question whole with its token, and nothing run", fired, out)
	}
	tok := out.confirm
	fired, out = run(tok, "Dark mode")
	if len(fired) != 1 || fired[0] != "first" || out.needs != webapi.ActionNeedsConfirm {
		t.Errorf("one token: fired %v, outcome %+v; want the first question answered and the second asked", fired, out)
	}
	// the question now says more (a goal that grew a card): the old
	// token answers nothing
	fired, out = run(tok, "Dark mode — and the same for its 2 cards")
	if len(fired) != 0 || out.needs != webapi.ActionNeedsConfirm || out.confirm == tok {
		t.Errorf("a token for another wording: fired %v, outcome %+v", fired, out)
	}
}
