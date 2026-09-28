package ui

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/morphis/gummi/internal/agent"
	"github.com/morphis/gummi/internal/domain"
	"github.com/morphis/gummi/internal/engine"
	"github.com/morphis/gummi/internal/webapi"
)

// A run that spends its envelope behind its question stops on its budget
// with the question still up: two open decisions, and the question comes
// first. Answering the question settles the question and nothing else —
// the card is still stopped on its budget, and says so. The board used
// to drop its one needs-you item for the card on the answer, so the
// budget stop vanished until the next restart and the card offered a
// plain run of the stage in its place, past a budget nobody raised.
func TestAnsweringAQuestionLeavesTheBudgetStopBehindIt(t *testing.T) {
	args, _ := json.Marshal(map[string]any{
		"question": "Persist where?", "changes_section": "Problem",
		"options": []map[string]string{{"label": "per-device"}, {"label": "synced"}},
	})
	ag := &agent.Fake{Caps: agent.Capabilities{ClientTools: true, Interrupt: true, UsageEvents: true}}
	ag.Responder = func(_ agent.SessionOpts, _ string) []agent.Event {
		return []agent.Event{
			{Kind: agent.EventClientToolCall, ToolCall: &agent.ToolCall{ID: "call-1", Name: "ask_user", Args: args}},
			{Kind: agent.EventUsage, Usage: agent.Usage{Credits: 500}},
			{Kind: agent.EventIdle},
		}
	}
	f := domain.Feature{ID: "FD-001", Num: 1, Title: "Dark mode", Slug: "dark-mode", Stage: domain.StagePlan}
	b, _, eng, _ := headlessBoardWith(t, ag, []domain.Feature{f}, func(c *engine.Config) { c.StageBudget = 100 })
	ctx := context.Background()
	waitBoard(t, b, func(bd webapi.Board) bool { return len(bd.Rows) == 1 })
	if err := eng.Run(f); err != nil {
		t.Fatal(err)
	}
	card := waitCard(t, b, "FD-001", "the question over the budget stop", func(c webapi.Card) bool {
		return c.Decision != nil && c.Decision.Kind == webapi.DecisionAsk && c.Status == webapi.StatusNeeds &&
			c.Needs != nil && c.Needs.Kind == "budget"
	})
	if _, err := b.Answer(ctx, "FD-001", webapi.AnswerRequest{
		Ref: card.Decision.Ref, Option: "1", Against: card.Decision.Against.Token,
	}, "alice"); err != nil {
		t.Fatalf("answer: %v", err)
	}
	waitCard(t, b, "FD-001", "the budget stop after the answer", func(c webapi.Card) bool {
		return c.Decision != nil && c.Decision.Kind == webapi.DecisionBudget
	})
}
