package threadfold

import (
	"testing"

	"github.com/morphis/gummi/internal/domain"
	"github.com/morphis/gummi/internal/state"
)

// A consult asked at todo is drawn where it was asked — before the plan
// stage that followed — not below everything else, and it belongs to no
// stage: it neither opens a segment nor counts as a turn of one.
func TestAConsultTurnSitsWhereItWasAsked(t *testing.T) {
	l := newLog()
	l.add(domain.StageTodo, state.EventConsult, "", MessagePayload{Author: "user", Content: "is this already done?", By: state.PersonActor("alice")}, "")
	l.add(domain.StageTodo, state.EventConsult, "", MessagePayload{Author: "assistant", Content: "No — nothing touches it yet."}, "")
	l.enter(domain.StagePlan, "architect", "stage").say(domain.StagePlan, "assistant", "planning")
	l.add(domain.StagePlan, state.EventConsult, "", MessagePayload{Author: "user", Content: "why that approach?"}, "")
	l.say(domain.StagePlan, "assistant", "plan written").exit(domain.StagePlan, "", 3)

	segs := Segments(l.evs)
	if len(segs) != 1 || segs[0].Turns() != 2 {
		t.Fatalf("segments = %d (turns %d), want one plan segment with its own two turns", len(segs), segs[0].Turns())
	}
	items := Items(l.evs, Options{})
	var got []string
	for _, it := range items {
		switch it.T {
		case ItemYou, ItemMessage:
			got = append(got, string(it.T)+":"+it.Via+":"+it.Text)
		case ItemStage:
			got = append(got, "stage:"+string(it.Stage))
		}
	}
	want := []string{
		"you:consult:is this already done?",
		"message:consult:No — nothing touches it yet.",
		"stage:plan",
		"message::planning",
		"you:consult:why that approach?",
		"message::plan written",
	}
	if len(got) != len(want) {
		t.Fatalf("items = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("item %d = %q, want %q (all: %q)", i, got[i], want[i], got)
		}
	}
	if items[0].By != "alice" || items[1].Author != "consult" {
		t.Errorf("question by %q, answer from %q; want alice and consult", items[0].By, items[1].Author)
	}
}
