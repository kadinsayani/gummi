package ui

import (
	"context"
	"strings"
	"testing"

	"github.com/morphis/gummi/internal/domain"
	"github.com/morphis/gummi/internal/state"
)

// autopilotBehindDependency is FD-001 on autopilot at its design gate,
// its clean crossing refused because FD-002 (at implement) has not landed.
func autopilotBehindDependency(t *testing.T) *Shell {
	t.Helper()
	ctx := context.Background()
	m, eng := agentWorkspace(t, writingArchitect())
	dep := &domain.Feature{ID: "FD-002", Num: 2, Title: "dep", Slug: "dep", Stage: domain.StageImplement}
	if err := m.store.CreateFeature(ctx, dep); err != nil {
		t.Fatal(err)
	}
	if err := m.store.AddDependency(ctx, "FD-001", dep.ID); err != nil {
		t.Fatal(err)
	}
	if err := m.store.SetGateApproval(ctx, "FD-001", domain.GateAutopilot); err != nil {
		t.Fatal(err)
	}
	m = pump(t, m, m.loadRows)
	m = openAndAttach(t, m)
	settleChat(t, eng)
	m = drainEngineLoop(t, m)

	got, _ := m.store.GetFeature(ctx, "FD-001")
	if got.Stage != domain.StagePlan {
		t.Fatalf("fixture: FD-001 crossed with its dependency unmet (at %s)", got.Stage)
	}
	it, ok := m.inbox.get("FD-001")
	if !ok || !strings.Contains(it.Text, unmetDependencyClause) {
		t.Fatalf("fixture: FD-001 did not park on its dependency: %+v", it)
	}
	return m
}

func landDependency(t *testing.T, m *Shell, id domain.FeatureID) {
	t.Helper()
	for _, st := range []domain.Stage{domain.StageVerify, domain.StageDone} {
		if _, err := m.store.Transition(context.Background(), id, st, "test"); err != nil {
			t.Fatal(err)
		}
	}
}

// TestAutopilotCrossesOnceItsDependencyLands: a card handed to autopilot
// that parked at its design gate only because a dependency had not landed
// crosses the gate and runs on once the dependency is done. It used to
// sit there for good, under a gate reason still naming the blocker.
func TestAutopilotCrossesOnceItsDependencyLands(t *testing.T) {
	m := autopilotBehindDependency(t)
	landDependency(t, m, "FD-002")

	m = pump(t, m, m.loadRows)
	m = drainEngineLoop(t, m)

	got, _ := m.store.GetFeature(context.Background(), "FD-001")
	if got.Stage != domain.StageVerify {
		t.Fatalf("FD-001 at %s after its dependency landed, want it to run on to verify", got.Stage)
	}
	for _, g := range gateEventsFor(t, m, "FD-001", domain.StagePlan) {
		if g.To == string(domain.StageImplement) && g.Actor != state.ActorAutopilot {
			t.Errorf("plan→implement crossed by %q, want autopilot", g.Actor)
		}
	}
	open, err := m.store.OpenDecisions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range open["FD-001"] {
		if strings.Contains(d.Question, unmetDependencyClause) {
			t.Errorf("a decision still names the landed dependency: %q", d.Question)
		}
	}
}

// TestAnAttendedGateStopsNamingALandedDependency: a card taken back from
// autopilot while it waited keeps waiting for its person, but its gate no
// longer claims a dependency blocks it.
func TestAnAttendedGateStopsNamingALandedDependency(t *testing.T) {
	m := autopilotBehindDependency(t)
	if err := m.store.SetGateApproval(context.Background(), "FD-001", domain.GateAttended); err != nil {
		t.Fatal(err)
	}
	landDependency(t, m, "FD-002")
	m = pump(t, m, m.loadRows)

	got, _ := m.store.GetFeature(context.Background(), "FD-001")
	if got.Stage != domain.StagePlan {
		t.Fatalf("an attended card crossed its gate on its own (at %s)", got.Stage)
	}
	it, ok := m.inbox.get("FD-001")
	if !ok || it.Kind != attnGate {
		t.Fatalf("the gate left the queue: %+v", it)
	}
	if strings.Contains(it.Text, unmetDependencyClause) {
		t.Errorf("gate reason still names the landed dependency: %q", it.Text)
	}
}

// TestApprovingAnAutopilotCardRunsTheStageBehindTheGate: a person's own
// approval of a card they handed to autopilot leaves the stage behind the
// gate to autopilot, as autopilot's own crossing does — it does not stop
// idle at implement.
func TestApprovingAnAutopilotCardRunsTheStageBehindTheGate(t *testing.T) {
	m := autopilotBehindDependency(t)
	landDependency(t, m, "FD-002")
	// the card is taken back, the dependency lands, and then handed over
	// again while it sits at the gate: the person approves it themselves.
	if err := m.store.SetGateApproval(context.Background(), "FD-001", domain.GateAttended); err != nil {
		t.Fatal(err)
	}
	m = pump(t, m, m.loadRows)
	if err := m.store.SetGateApproval(context.Background(), "FD-001", domain.GateAutopilot); err != nil {
		t.Fatal(err)
	}
	m = pump(t, m, m.loadRows)
	m = pump(t, m, m.advanceStage("FD-001"))
	m = drainEngineLoop(t, m)

	got, _ := m.store.GetFeature(context.Background(), "FD-001")
	if got.Stage != domain.StageVerify {
		t.Fatalf("an approved autopilot card stopped at %s, want it to run on to verify", got.Stage)
	}
}
