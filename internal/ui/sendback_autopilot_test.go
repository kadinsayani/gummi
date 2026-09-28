package ui

import (
	"context"
	"strings"
	"sync"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/morphis/gummi/internal/agent"
	"github.com/morphis/gummi/internal/domain"
	"github.com/morphis/gummi/internal/reentry"
	"github.com/morphis/gummi/internal/threadfold"
)

// kickoffRecorder classifies every line as intent and records what each
// implementer session was told when it started.
type kickoffRecorder struct {
	mu         sync.Mutex
	implKicked []string
}

func (k *kickoffRecorder) agent(intent string) *agent.Fake {
	return &agent.Fake{Responder: func(opts agent.SessionOpts, msg string) []agent.Event {
		if strings.Contains(msg, "INTENT: <one of the words above>") {
			return []agent.Event{{Kind: agent.EventMessage, Text: "INTENT: " + intent}, {Kind: agent.EventIdle}}
		}
		if opts.Role == agent.RoleImplementer {
			k.mu.Lock()
			k.implKicked = append(k.implKicked, msg)
			k.mu.Unlock()
		}
		return []agent.Event{{Kind: agent.EventMessage, Text: "ok"}, {Kind: agent.EventIdle}}
	}}
}

func (k *kickoffRecorder) kicked() []string {
	k.mu.Lock()
	defer k.mu.Unlock()
	return append([]string(nil), k.implKicked...)
}

// TestSendingAnAutopilotCardBackRunsTheStageItLandsOn: a send-back to
// implement on a card handed to autopilot starts implement, with the
// person's line in its kickoff. It used to only walk the card back and
// leave it idle at implement, under a chip promising it would run.
func TestSendingAnAutopilotCardBackRunsTheStageItLandsOn(t *testing.T) {
	rec := &kickoffRecorder{}
	m := verifyCard(t, rec.agent("implementation_wrong"))
	if err := m.store.SetGateApproval(context.Background(), "FD-001", domain.GateAutopilot); err != nil {
		t.Fatal(err)
	}
	m = pump(t, m, m.loadRows)
	before := len(rec.kicked())

	const line = "the greeting must end with a full stop"
	m = openCardPage(t, m)
	m = pump(t, m, m.routeReentry(m.rows[0], "bounce", line))
	p := m.pendingChip()
	if p == nil || p.out.Action != reentry.Rewind || p.out.Target != domain.StageImplement {
		t.Fatalf("no rewind chip to implement: %+v", p)
	}
	details := strings.Join(chipDetails(m.rows[0], p), " ")
	if strings.Contains(details, "design gate") {
		t.Errorf("a rewind to implement passes no design gate, yet the chip says: %q", details)
	}
	if !strings.Contains(details, "implement runs straight away") {
		t.Errorf("the chip does not say implement runs: %q", details)
	}
	key := tea.KeyPressMsg{Code: 'y', Text: "y"}
	if p.goOnEnter {
		key = tea.KeyPressMsg{Code: tea.KeyEnter}
	}
	m = press(t, m, key)
	m = drainEngineLoop(t, m)

	kicked := rec.kicked()
	if len(kicked) <= before {
		t.Fatal("nothing ran after sending an autopilot card back to implement")
	}
	if !strings.Contains(strings.Join(kicked[before:], "\n"), line) {
		t.Errorf("the line did not ride implement's kickoff: %q", kicked[before:])
	}

	// the receipt reads as the send-back it was
	var receipt string
	for _, g := range gateEventsFor(t, m, "FD-001", domain.StageVerify) {
		if g.To == string(domain.StageImplement) {
			receipt = threadfold.GateLine(g)
		}
	}
	if !strings.Contains(receipt, "sent it back") || strings.Contains(receipt, "advanced") {
		t.Errorf("send-back receipt = %q, want it to read as a send-back", receipt)
	}
}

// TestAnAttendedSendBackRunsNothing: the other half — an attended card
// walks back and waits, and its chip says so rather than promising a
// design gate a rewind to implement never meets.
func TestAnAttendedSendBackRunsNothing(t *testing.T) {
	rec := &kickoffRecorder{}
	m := verifyCard(t, rec.agent("implementation_wrong"))
	if err := m.store.SetGateApproval(context.Background(), "FD-001", domain.GateAttended); err != nil {
		t.Fatal(err)
	}
	m = pump(t, m, m.loadRows)
	before := len(rec.kicked())
	m = openCardPage(t, m)
	m = pump(t, m, m.routeReentry(m.rows[0], "bounce", "wrong"))
	p := m.pendingChip()
	if p == nil {
		t.Fatal("no chip")
	}
	if got := strings.Join(chipDetails(m.rows[0], p), " "); !strings.Contains(got, "Nothing runs until you start implement") {
		t.Errorf("attended chip = %q", got)
	}
	key := tea.KeyPressMsg{Code: 'y', Text: "y"}
	if p.goOnEnter {
		key = tea.KeyPressMsg{Code: tea.KeyEnter}
	}
	m = press(t, m, key)
	m = drainEngineLoop(t, m)
	if n := len(rec.kicked()); n != before {
		t.Errorf("an attended send-back started implement on its own")
	}
	got, _ := m.store.GetFeature(context.Background(), "FD-001")
	if got.Stage != domain.StageImplement {
		t.Errorf("stage = %s, want implement", got.Stage)
	}
}
