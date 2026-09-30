package ui

import (
	"strings"
	"testing"

	"github.com/morphis/gummi/internal/domain"
)

// TestASpecsBriefSaysWhereItsWorkCameFrom: a spec written from a session is
// told which session it continues and that the work is already on its
// branch, and carries what the person asked, newest kept when the
// conversation is longer than the brief may be.
func TestASpecsBriefSaysWhereItsWorkCameFrom(t *testing.T) {
	id, _ := domain.NewID(domain.KindFreeform, 3)
	f := domain.Feature{ID: id, Num: 3, Kind: domain.KindFreeform, Title: "Fix the flaky retry", Slug: "fix-the-flaky-retry", BranchScheme: domain.BranchSchemeKind}
	brief := specBrief(f, "Configurable sync retries", "a3f9c21deadbeef", []string{"find why TestRetry flakes", "make the cap configurable"})
	for _, want := range []string{"Configurable sync retries\n", "FF-003", f.BranchName(), "a3f9c21", "- find why TestRetry flakes", "- make the cap configurable"} {
		if !strings.Contains(brief, want) {
			t.Errorf("the brief does not carry %q:\n%s", want, brief)
		}
	}
	if strings.Contains(brief, "a3f9c21d") {
		t.Error("the brief names the full commit id rather than its short form")
	}

	var many []string
	for i := range 400 {
		many = append(many, strings.Repeat("x", 40)+" request "+string(rune('a'+i%26)))
	}
	many = append(many, "the newest request")
	long := specBrief(f, "", "a3f9c21", many)
	if !strings.HasPrefix(long, f.Title+"\n") {
		t.Errorf("an empty title did not fall back to the session's:\n%s", long[:80])
	}
	if len(long) > specBriefMax+2000 {
		t.Errorf("the brief is %d bytes for a %d budget", len(long), specBriefMax)
	}
	if !strings.Contains(long, "the newest request") || !strings.Contains(long, "earlier requests left out") {
		t.Error("the bounded brief dropped the newest request, or did not say it left some out")
	}
}
