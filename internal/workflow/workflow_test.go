package workflow

import (
	"testing"

	"github.com/morphis/gummi/internal/domain"
)

// edge is one from→to pair, for the exhaustive table below.
type edge struct{ from, to domain.Stage }

// legal is every transition the one workflow allows. Three tables became
// one when three graphs did: they were the same shape wearing three sets
// of names, so a table per kind was three copies of this.
var legal = map[edge]bool{
	{domain.StageTodo, domain.StagePlan}:        true,
	{domain.StagePlan, domain.StageImplement}:   true,
	{domain.StageImplement, domain.StageVerify}: true,
	{domain.StageVerify, domain.StageDone}:      true,
	{domain.StageImplement, domain.StagePlan}:   true, // the plan was wrong
	{domain.StageVerify, domain.StageImplement}: true, // checks failed
}

// TestTransitionTableExhaustive checks every from×to pair against the
// table, so an edge added or lost anywhere shows up here rather than in
// whichever loop happened to walk it.
func TestTransitionTableExhaustive(t *testing.T) {
	for _, from := range domain.Stages {
		for _, to := range domain.Stages {
			err := CanTransition(from, to)
			if legal[edge{from, to}] {
				if err != nil {
					t.Errorf("%s → %s should be legal, got %v", from, to, err)
				}
				continue
			}
			if err == nil {
				t.Errorf("%s → %s should be illegal, got nil", from, to)
			}
		}
	}
}

// TestVerifyNeverSkippable: the only way into Done is Verify, the only
// way into Verify is Implement, and the only way out of Implement is
// Verify or back to Plan. Nothing may jump the quality floor — and with
// no skip flags left there is nothing that could try.
func TestVerifyNeverSkippable(t *testing.T) {
	for _, from := range domain.Stages {
		if from != domain.StageVerify {
			if err := CanTransition(from, domain.StageDone); err == nil {
				t.Errorf("%s → done must be illegal", from)
			}
		}
		if from != domain.StageImplement {
			if err := CanTransition(from, domain.StageVerify); err == nil {
				t.Errorf("%s → verify must be illegal", from)
			}
		}
	}
	for _, to := range domain.Stages {
		if to == domain.StageVerify || to == domain.StagePlan {
			continue
		}
		if err := CanTransition(domain.StageImplement, to); err == nil {
			t.Errorf("implement → %s must be illegal", to)
		}
	}
}

func TestUnknownStages(t *testing.T) {
	if err := CanTransition("nope", domain.StagePlan); err == nil {
		t.Error("an unknown from-stage was accepted")
	}
	if err := CanTransition(domain.StagePlan, "nope"); err == nil {
		t.Error("an unknown to-stage was accepted")
	}
}

// TestNext walks the forward path and the two backward edges. Next lists
// in table order, so the forward edge comes first everywhere; the rerun
// edges follow it.
func TestNext(t *testing.T) {
	for _, tc := range []struct {
		from domain.Stage
		want []domain.Stage
	}{
		{domain.StageTodo, []domain.Stage{domain.StagePlan}},
		{domain.StagePlan, []domain.Stage{domain.StageImplement}},
		{domain.StageImplement, []domain.Stage{domain.StageVerify, domain.StagePlan}},
		{domain.StageVerify, []domain.Stage{domain.StageDone, domain.StageImplement}},
		{domain.StageDone, nil},
	} {
		got := Next(tc.from)
		if len(got) != len(tc.want) {
			t.Errorf("Next(%s) = %v, want %v", tc.from, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("Next(%s) = %v, want %v", tc.from, got, tc.want)
				break
			}
		}
	}
}

// TestRerunTarget: the rewind surfaces (the TUI's bounce action, the
// headless --bounce) take their target from here, so exactly the two
// backward edges have one, and each names the stage that produced it.
func TestRerunTarget(t *testing.T) {
	for _, tc := range []struct {
		from domain.Stage
		want domain.Stage
		ok   bool
	}{
		{domain.StageImplement, domain.StagePlan, true},
		{domain.StageVerify, domain.StageImplement, true},
		{domain.StageTodo, "", false},
		{domain.StagePlan, "", false},
		{domain.StageDone, "", false},
	} {
		got, ok := RerunTarget(tc.from)
		if ok != tc.ok || got != tc.want {
			t.Errorf("RerunTarget(%s) = (%q, %v), want (%q, %v)", tc.from, got, ok, tc.want, tc.ok)
		}
	}
}

// TestInitialAndTerminal: every card starts at todo and ends at done, and
// neither answer depends on the kind any more — which is the merge.
func TestInitialAndTerminal(t *testing.T) {
	if Initial() != domain.StageTodo {
		t.Errorf("Initial() = %s, want todo", Initial())
	}
	if !Terminal(domain.StageDone) {
		t.Error("done should be terminal")
	}
	for _, s := range domain.Stages {
		if s == domain.StageDone {
			continue
		}
		if Terminal(s) {
			t.Errorf("%s should not be terminal", s)
		}
	}
}

// TestAFreeformCardTakesNoTransition: domain.StageOpen is in no edge of
// the table, in either direction, so the state machine can neither move a
// freeform card nor let a card in the graph arrive at one. That — and not
// a per-kind branch inside CanTransition — is the whole of "a freeform
// card has no workflow" (DESIGN §19).
func TestAFreeformCardTakesNoTransition(t *testing.T) {
	for _, to := range append([]domain.Stage{domain.StageOpen}, domain.Stages...) {
		if err := CanTransition(domain.StageOpen, to); err == nil {
			t.Errorf("open → %s is legal; a freeform card must have no moves", to)
		}
	}
	for _, from := range domain.Stages {
		if err := CanTransition(from, domain.StageOpen); err == nil {
			t.Errorf("%s → open is legal; nothing in the graph may arrive at a freeform card's stage", from)
		}
	}
	if next := Next(domain.StageOpen); len(next) != 0 {
		t.Errorf("Next(open) = %v, want nothing: no advance action, no next-stage chip", next)
	}
	if _, ok := RerunTarget(domain.StageOpen); ok {
		t.Error("open has a rerun target; there is no stage to bounce back to")
	}
	// Terminal is true for it, which is what makes the headless driver
	// report it unresumable rather than drivable.
	if !Terminal(domain.StageOpen) {
		t.Error("open is not terminal; the driver would try to advance it")
	}
}

// TestInitialForPutsAFreeformCardOutsideTheGraph: the one kind-aware thing
// about a freeform card's place in the state machine is where it starts.
func TestInitialForPutsAFreeformCardOutsideTheGraph(t *testing.T) {
	if got := InitialFor(domain.KindFreeform); got != domain.StageOpen {
		t.Errorf("InitialFor(freeform) = %q, want %q", got, domain.StageOpen)
	}
	for _, k := range []domain.Kind{domain.KindFeature, domain.KindBug, domain.KindResearch, domain.KindGoal, domain.Kind("")} {
		if got := InitialFor(k); got != Initial() {
			t.Errorf("InitialFor(%q) = %q, want the graph's start %q", k, got, Initial())
		}
	}
}
