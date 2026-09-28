package ui

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/morphis/gummi/internal/agent"
	"github.com/morphis/gummi/internal/domain"
	"github.com/morphis/gummi/internal/webapi"
)

// A research card runs in a scratch tree and never gets a branch, so its
// head names none — the page used to show "research/research-…" and
// "onto main", a checkout that does not exist and a landing that never
// happens.
func TestAResearchHeadNamesNoBranch(t *testing.T) {
	b, _, _, _, _ := headlessBoardFor(t, agent.NewFake("ok"), domain.Feature{
		ID: "RS-001", Num: 1, Kind: domain.KindResearch, Title: "Compare greeting libraries",
		Slug: "compare-greeting-libraries", Stage: domain.StagePlan,
	})
	waitBoard(t, b, func(bd webapi.Board) bool { return len(bd.Rows) == 1 })
	c, err := b.Card(context.Background(), "RS-001")
	if err != nil {
		t.Fatal(err)
	}
	if c.Branch != "" || c.Base != "" || !c.Scratch {
		t.Errorf("research head: branch %q onto %q (scratch %v); want no branch, no base, a scratch tree", c.Branch, c.Base, c.Scratch)
	}
}

// A research card's stops are about its research document, and are
// raised on it: its verify decision anchors the spec tab and names the
// document's revision, never a branch it does not have.
func TestAResearchVerifyIsAboutItsDocument(t *testing.T) {
	b, _, _, f, _ := headlessBoardFor(t, agent.NewFake("ok"), domain.Feature{
		ID: "RS-001", Num: 1, Kind: domain.KindResearch, Title: "Compare greeting libraries",
		Slug: "compare-greeting-libraries", Stage: domain.StageVerify,
	})
	waitBoard(t, b, func(bd webapi.Board) bool { return len(bd.Rows) == 1 })
	ctx := context.Background()
	if err := b.Do(ctx, func(m *Shell) tea.Cmd {
		m.inbox.add(f.ID, attnGate, "verification passed — decide whether the research is done")
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	c, err := b.Card(ctx, "RS-001")
	if err != nil {
		t.Fatal(err)
	}
	if c.Decision == nil || c.Decision.Kind != webapi.DecisionVerify {
		t.Fatalf("decision = %+v, want the verify stop", c.Decision)
	}
	if c.Decision.Anchor != webapi.AnchorSpec {
		t.Errorf("anchor = %q, want the research document (spec)", c.Decision.Anchor)
	}
	if strings.Contains(c.Decision.Against.Label, "branch") || !strings.Contains(c.Decision.Against.Label, "research document") {
		t.Errorf("raised on %q, want the research document's revision", c.Decision.Against.Label)
	}
}
