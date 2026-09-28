package ui

import (
	"testing"

	"github.com/morphis/gummi/internal/domain"
	"github.com/morphis/gummi/internal/verify"
)

// TestAFollowOnErrorDoesNotRefuseTheAct: an approval that crossed its
// gate starts the check baseline, and a baseline that finds a check
// already failing is an error notice. The web intent that approved used
// to read that notice as its own refusal, so the page was told the
// approval failed (409) while the card had in fact moved on — and the
// next answer the person gave was made against a card they believed
// had not moved. A notice about follow-on work is passed on, never
// taken as the act refused; a refusal of the act itself still is one.
func TestAFollowOnErrorDoesNotRefuseTheAct(t *testing.T) {
	m := &Shell{baselining: map[domain.FeatureID]bool{"FD-001": true}}
	_, _ = m.Update(baselineDoneMsg{id: "FD-001", results: []verify.Result{{Name: "build", OK: false, ExitCode: 1, Status: verify.StatusFail}}})
	if !m.notice.isErr || !m.notice.aside {
		t.Fatalf("a failing baseline should be an error notice marked aside, got %+v", m.notice)
	}

	it := &webIntent{}
	it.noticed(m, noticeMsg{})
	if it.out.refused != "" {
		t.Fatalf("a failing baseline after an approval was read as the approval refused: %q", it.out.refused)
	}
	if len(it.out.notices) != 1 {
		t.Fatalf("the baseline's notice should still be passed on, got %v", it.out.notices)
	}

	m.notice = noticeMsg{text: "FD-001: blocked by unmet dependency FD-002@plan", isErr: true}
	it = &webIntent{}
	it.noticed(m, noticeMsg{})
	if it.out.refused == "" {
		t.Fatal("an error about the act itself must still refuse it")
	}
}
