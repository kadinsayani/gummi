package ui

import (
	"context"
	"os/exec"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/morphis/gummi/internal/agent"
	"github.com/morphis/gummi/internal/webapi"
)

// A board served for days must offer a branch cut after it started: the
// branches were read once, at attach, and a branch worth adopting —
// usually cut outside gummi — was refused as "cannot be adopted".
func TestWebFormOffersABranchCutAfterLaunch(t *testing.T) {
	b, _, _, _, _ := headlessBoard(t, agent.NewFake("ok"))
	ctx := context.Background()
	var repo string
	if err := b.Do(ctx, func(m *Shell) tea.Cmd { repo = m.ws.RepoRoot; return nil }); err != nil {
		t.Fatal(err)
	}
	// a branch with a commit of its own, cut without touching the checkout
	sha, err := exec.CommandContext(ctx, "git", "-C", repo, "commit-tree", "HEAD^{tree}", "-p", "HEAD", "-m", "late work").Output()
	if err != nil {
		t.Fatalf("git commit-tree: %v", err)
	}
	if out, err := exec.CommandContext(ctx, "git", "-C", repo, "branch", "topic/late", strings.TrimSpace(string(sha))).CombinedOutput(); err != nil {
		t.Fatalf("git branch: %v\n%s", err, out)
	}
	c, err := b.CreateCard(ctx, webapi.CreateCardRequest{Kind: "feature", Title: "Pick up the late branch", Adopt: "topic/late"}, "Simon")
	if err != nil {
		t.Fatalf("a branch cut after launch could not be adopted: %v", err)
	}
	if c.Branch != "topic/late" {
		t.Errorf("the card works on %q, want the adopted topic/late", c.Branch)
	}
}

// The same refusal twice running is said twice: the second create that
// hit it used to answer only "the card was not created".
func TestWebCreateSaysARepeatedRefusalAgain(t *testing.T) {
	b, _, _, _, _ := headlessBoard(t, agent.NewFake("ok"))
	ctx := context.Background()
	var base string
	if err := b.Do(ctx, func(m *Shell) tea.Cmd { base = m.baseBranches[""]; return nil }); err != nil {
		t.Fatal(err)
	}
	for i := range 2 {
		_, err := b.CreateCard(ctx, webapi.CreateCardRequest{Kind: "feature", Title: "Adopt the trunk", Adopt: base}, "Simon")
		if err == nil || !strings.Contains(err.Error(), "would land on") {
			t.Fatalf("attempt %d: refused with %v, want the reason", i+1, err)
		}
	}
}
