package ui

import (
	"testing"

	"github.com/morphis/gummi/internal/domain"
)

// The bug list says who opened each issue: gh returns the author, and a
// proposal carries it to the page.
func TestWebBugProposalCarriesItsAuthor(t *testing.T) {
	got := webBugProposal(domain.BugProposal{Title: "Login loops", ExternalRef: "https://x/42", Number: 42, Author: "octo"})
	if got.Author != "octo" {
		t.Fatalf("author = %q, want octo", got.Author)
	}
}
