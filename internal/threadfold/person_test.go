package threadfold

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/morphis/gummi/internal/state"
)

func payload(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// A person the web face names is a person everywhere the thread asks
// "did a human do this?": their gate crossing and their answer each take
// the card back from autopilot, exactly as the terminal's bare "user"
// does, and the receipts name them.
func TestANamedPersonIsAHuman(t *testing.T) {
	at := time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)
	simon := state.PersonActor("Simon")
	took := state.CardEvent{Kind: state.EventAutopilot, At: at, Payload: payload(t, state.AutopilotPayload{Event: state.AutopilotTookOver})}

	for name, closing := range map[string]state.CardEvent{
		"gate": {Kind: state.EventGate, At: at.Add(time.Minute), Payload: payload(t, state.GatePayload{From: "plan", To: "implement", Actor: simon})},
		"ask":  {Kind: state.EventAsk, At: at.Add(time.Minute), Payload: payload(t, state.AskPayload{Question: "q", Answer: "a", Actor: simon, By: simon})},
	} {
		got := Stretches([]state.CardEvent{took, closing})
		if len(got) != 1 || got[0].Closed != StretchTakenBack {
			t.Errorf("%s by %s: stretches = %+v, want one taken back", name, simon, got)
		}
	}
	if !HumanGateActor(simon) || HumanGateActor(state.ActorAutopilot) {
		t.Error("HumanGateActor misreads a named person or autopilot")
	}
	if got := GateCrosser(state.GatePayload{Actor: simon}); got != "Simon" {
		t.Errorf("GateCrosser = %q, want Simon", got)
	}
	if got := GateCrosser(state.GatePayload{Actor: state.ActorUser}); got != "you" {
		t.Errorf("GateCrosser(user) = %q, want you", got)
	}
	if got := AskAnswerer(state.AskPayload{Actor: simon, By: simon}); got != "Simon" {
		t.Errorf("AskAnswerer = %q, want Simon", got)
	}
	// a person's crossing is not a machine decision line
	gate := state.CardEvent{Kind: state.EventGate, Payload: payload(t, state.GatePayload{From: "plan", To: "implement", Actor: simon})}
	if line := DecisionLine(gate, false); line != "" {
		t.Errorf("DecisionLine for a person's crossing = %q, want none", line)
	}
}
