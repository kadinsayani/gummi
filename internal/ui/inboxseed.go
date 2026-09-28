package ui

import (
	"context"

	tea "charm.land/bubbletea/v2"

	"github.com/morphis/gummi/internal/decisions"
	"github.com/morphis/gummi/internal/domain"
	"github.com/morphis/gummi/internal/state"
)

// openDecisionsMsg carries the startup query's result: every card's still
// open decisions (DESIGN §10.18), keyed by feature, or the error from a
// failed read.
type openDecisionsMsg struct {
	decisions map[domain.FeatureID][]state.OpenDecision
	err       error
}

// fetchOpenDecisions runs Store.OpenDecisions once, at startup: a database
// read, so — per the no-IO-in-Update contract attachChat documents — it
// has to happen inside a dispatched command rather than inline in Init or
// Update.
func (m *Shell) fetchOpenDecisions() tea.Msg {
	open, err := m.store.OpenDecisions(context.Background())
	return openDecisionsMsg{decisions: open, err: err}
}

// seedInboxFromDecisions seeds the needs-attention queue from the durable
// decision_open records the startup query read — the primary source
// DESIGN §10.18 exists for, with reconstructInbox's session inference
// (called right after this, in the openDecisionsMsg handler) as the
// fallback for whatever it doesn't cover.
//
// Every add goes through inbox.seed, never put/add: a live engine event
// can raise a feature's item before this message lands (the query, and
// the round trip back to Update, both take time a running engine doesn't
// wait for), and that live item is truer than a snapshot taken before
// Init even asked.
func (m *Shell) seedInboxFromDecisions(open map[domain.FeatureID][]state.OpenDecision) {
	for id, decs := range open {
		// a goal card's stops are its goal's, after a restart as before it
		if m.goalOf(id) != "" {
			continue
		}
		dec, ok := decisions.Rank(decs)
		if !ok {
			continue
		}
		lane, escalated, ok := decisions.Attention(dec.Kind)
		if !ok {
			continue
		}
		kind := attnKind(lane)
		m.inbox.seed(attnItem{
			Feature: id, Kind: kind, Text: dec.Question,
			Escalated: escalated, At: dec.At,
		})
	}
}
