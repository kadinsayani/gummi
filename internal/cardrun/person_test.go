package cardrun

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/morphis/gummi/internal/domain"
	"github.com/morphis/gummi/internal/state"
)

// A gate and an answer a named person gave through the web face are
// counted as the person's, the same as the terminal's "user".
func TestANamedPersonAnswersAsAPerson(t *testing.T) {
	at := time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)
	simon := state.PersonActor("Simon")
	gate, _ := json.Marshal(state.GatePayload{From: "plan", To: "implement", Actor: simon})
	ask, _ := json.Marshal(state.AskPayload{Question: "q", Answer: "a", Actor: simon, By: simon})
	run := Report(Input{
		Feature: domain.Feature{ID: "FD-001", Stage: domain.StageImplement},
		Events: []state.CardEvent{
			{Seq: 1, Kind: state.EventAsk, At: at, Stage: domain.StagePlan, Payload: string(ask)},
			{Seq: 2, Kind: state.EventGate, At: at.Add(time.Minute), Stage: domain.StagePlan, Payload: string(gate)},
		},
	})
	if run.Judgment.Gates.ByYou != 1 || run.Judgment.Gates.ByMachine != 0 {
		t.Errorf("gates = %+v, want one by you", run.Judgment.Gates)
	}
	if run.Judgment.Asks.ByYou != 1 || run.Judgment.Asks.ByMachine != 0 {
		t.Errorf("asks = %+v, want one by you", run.Judgment.Asks)
	}
}
