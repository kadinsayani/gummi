package web

import (
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/morphis/gummi/internal/agent"
	"github.com/morphis/gummi/internal/webapi"
)

// commitIn makes one commit touching name in the card's worktree.
func (b *docsBoard) commitIn(name, msg string) {
	b.t.Helper()
	writeFile(b.t, filepath.Join(b.wt, name), msg+"\n")
	gitIn(b.t, b.wt, "add", "-A")
	gitIn(b.t, b.wt, "commit", "-q", "-m", msg)
}

// The log lists the card's own commits oldest first, and a plan is a dry
// run that moves nothing until the rewrite is sent.
func TestLogReadPlanAndRewrite(t *testing.T) {
	b := newDocsBoard(t, agent.NewFake("ok"))
	b.commitIn("a.txt", "FD-001: implement checkpoint")
	b.commitIn("b.txt", "FD-001: implement checkpoint")

	var log webapi.Log
	if st := b.get("/api/cards/FD-001/log", &log); st != http.StatusOK {
		t.Fatalf("log = %d", st)
	}
	if len(log.Commits) != 3 || log.Commits[0].Subject != "work" || !log.Commits[1].Checkpoint || log.Commits[0].Checkpoint {
		t.Fatalf("commits = %+v", log.Commits)
	}
	if !log.Rewritable || log.Why != "" || log.Head != log.Commits[2].SHA || log.Base != "main" {
		t.Errorf("log = %+v", log)
	}
	var one webapi.CommitDiff
	if st := b.get("/api/cards/FD-001/log/"+log.Commits[1].Short, &one); st != http.StatusOK || len(one.Files) != 1 || one.Files[0].Path != "a.txt" || one.Files[0].Add != 1 {
		t.Errorf("commit diff = %d %+v", st, one)
	}
	if st := b.get("/api/cards/FD-001/log/--output=x", nil); st != http.StatusConflict {
		t.Errorf("a name that is no commit of the card = %d, want 409", st)
	}
	tree := gitIn(t, b.wt, "rev-parse", "HEAD^{tree}")
	c := log.Commits
	req := webapi.RewriteRequest{Head: log.Head, Groups: []webapi.RewriteGroup{
		{Commits: []string{c[0].SHA}, Message: "feat(ui): dark mode"},
		{Commits: []string{c[1].SHA, c[2].SHA}, Message: "test(ui): cover it"},
	}}

	var prev webapi.RewritePreview
	if st := b.send(http.MethodPost, "/api/cards/FD-001/log/plan", pl(req), &prev); st != http.StatusOK {
		t.Fatalf("plan = %d", st)
	}
	if len(prev.Commits) != 2 || prev.Changed != 2 || prev.Pushed || prev.Noop || prev.Commits[1].Subject != "test(ui): cover it" {
		t.Errorf("preview = %+v", prev)
	}
	if got := gitIn(t, b.wt, "rev-parse", "HEAD"); got != log.Head {
		t.Fatal("a dry run moved the branch")
	}

	var res webapi.RewriteResult
	if st := b.send(http.MethodPost, "/api/cards/FD-001/log/rewrite", pl(req), &res); st != http.StatusOK {
		t.Fatalf("rewrite = %d", st)
	}
	if len(res.Log.Commits) != 2 || res.Log.Commits[0].Subject != "feat(ui): dark mode" || res.Head != res.Log.Head {
		t.Errorf("result = %+v", res)
	}
	if got := gitIn(t, b.wt, "rev-parse", "HEAD^{tree}"); got != tree {
		t.Errorf("tree changed: %s want %s", got, tree)
	}

	// the plan was made against commits that have moved
	if st := b.send(http.MethodPost, "/api/cards/FD-001/log/rewrite", pl(req), nil); st != http.StatusConflict {
		t.Errorf("stale plan = %d, want 409", st)
	}
}

func TestLogRewriteRefusals(t *testing.T) {
	b := newDocsBoard(t, agent.NewFake("ok"))
	b.commitIn("a.txt", "second")
	var log webapi.Log
	b.get("/api/cards/FD-001/log", &log)
	all := func(msg string) string {
		return pl(webapi.RewriteRequest{Head: log.Head, Groups: []webapi.RewriteGroup{{Commits: []string{log.Commits[0].SHA, log.Commits[1].SHA}, Message: msg}}})
	}
	if st := b.send(http.MethodPost, "/api/cards/FD-001/log/rewrite", all("feat: x\n\nCo-Authored-By: Claude <noreply@anthropic.com>"), nil); st != http.StatusBadRequest {
		t.Errorf("attribution = %d, want 400", st)
	}
	if st := b.send(http.MethodPost, "/api/cards/FD-001/log/rewrite", `{"groups":[]}`, nil); st != http.StatusBadRequest {
		t.Errorf("empty plan = %d, want 400", st)
	}
	writeFile(t, filepath.Join(b.wt, "stray.txt"), "x\n")
	st := b.send(http.MethodPost, "/api/cards/FD-001/log/rewrite", all("feat: x"), nil)
	if st != http.StatusConflict {
		t.Errorf("dirty worktree = %d, want 409", st)
	}
	// a card nobody started has no branch to read
	var none webapi.Log
	if st := b.get("/api/cards/FD-002/log", &none); st != http.StatusOK || none.Rewritable || !strings.Contains(none.Why, "no branch") || len(none.Commits) != 0 {
		t.Errorf("todo card: %d %+v", st, none)
	}
}
