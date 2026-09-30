package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/morphis/gummi/internal/webapi"
)

// `gummi log` lists the card's own three commits, and `gummi rewrite`
// squashes them by a plan built from its JSON, content unchanged.
func TestLogAndRewriteCommands(t *testing.T) {
	_, f := squashCLIRepo(t)
	wt := filepath.Join(".gummi", "worktrees", string(f.ID))
	tree := cliGit(t, wt, "rev-parse", "HEAD^{tree}")

	var runErr error
	text := captureStdout(t, func() { runErr = runCLI("log", string(f.ID)) })
	if runErr != nil {
		t.Fatalf("log: %v", runErr)
	}
	if strings.Count(text, "checkpoint") != 3 {
		t.Errorf("log text = %q", text)
	}
	raw := captureStdout(t, func() { runErr = runCLI("log", string(f.ID), "--json") })
	if runErr != nil {
		t.Fatalf("log --json: %v", runErr)
	}
	var l webapi.Log
	if err := json.Unmarshal([]byte(raw), &l); err != nil {
		t.Fatalf("log --json is not a log: %v\n%s", err, raw)
	}
	if len(l.Commits) != 3 || !l.Rewritable || l.Head == "" {
		t.Fatalf("log = %+v", l)
	}

	var shas []string
	for _, c := range l.Commits {
		shas = append(shas, c.Short)
	}
	plan := filepath.Join(t.TempDir(), "plan.json")
	body, _ := json.Marshal(webapi.RewriteRequest{Head: l.Head, Groups: []webapi.RewriteGroup{{Commits: shas, Message: "feat(export): json"}}})
	if err := os.WriteFile(plan, body, 0o600); err != nil {
		t.Fatal(err)
	}

	dry := captureStdout(t, func() { runErr = runCLI("rewrite", string(f.ID), "--plan", plan, "--dry-run") })
	if runErr != nil || !strings.Contains(dry, "content unchanged") {
		t.Fatalf("dry run: %v %q", runErr, dry)
	}
	if n := cliGit(t, wt, "rev-list", "--count", "origin/main..HEAD"); n != "3" {
		t.Fatalf("a dry run rewrote the branch: %s commits", n)
	}

	out := captureStdout(t, func() { runErr = runCLI("rewrite", string(f.ID), "--plan", plan) })
	if runErr != nil || !strings.Contains(out, "history rewritten") {
		t.Fatalf("rewrite: %v %q", runErr, out)
	}
	if n := cliGit(t, wt, "rev-list", "--count", "origin/main..HEAD"); n != "1" {
		t.Errorf("commits = %s, want 1", n)
	}
	if got := cliGit(t, wt, "rev-parse", "HEAD^{tree}"); got != tree {
		t.Errorf("content changed")
	}
	// the same plan again names commits that are gone
	if err := runCLI("rewrite", string(f.ID), "--plan", plan); err == nil {
		t.Error("a stale plan was accepted")
	}
}

func TestRewriteNeedsAPlan(t *testing.T) {
	_, f := squashCLIRepo(t)
	if err := runCLI("rewrite", string(f.ID)); err == nil || !strings.Contains(err.Error(), "--plan") {
		t.Errorf("err = %v", err)
	}
}
