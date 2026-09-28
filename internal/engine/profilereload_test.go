package engine

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/morphis/gummi/internal/agent"
	"github.com/morphis/gummi/internal/config"
)

// writeProfiles writes profiles.yaml and stamps it with a distinct mtime,
// so a rewrite within the filesystem's timestamp granularity still reads
// as a new version.
func writeProfiles(t *testing.T, path, body string, at time.Time) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, at, at); err != nil {
		t.Fatal(err)
	}
}

func scribeProfiles(model string) string {
	return "default: std\nprofiles:\n  std:\n    scribe:\n      model: " + model + "\n"
}

// TestProfileEditsReachNewSessions is the regression for a board whose
// profiles.yaml was fixed while it ran: the doctor re-read the file and
// said Ready, and the engine kept resolving the scribe to the old,
// unservable model for as long as the process lived. A validated edit is
// now what the next session resolves; a broken one is refused and the
// last good profiles stay in force.
func TestProfileEditsReachNewSessions(t *testing.T) {
	ws, store, wt := newRepo(t)
	path := ws.ProfilesFile()
	t0 := time.Now().Add(-time.Hour)
	writeProfiles(t, path, scribeProfiles("claude-haiku-4.5"), t0)
	loaded, err := config.LoadProfiles(path)
	if err != nil {
		t.Fatal(err)
	}
	e := New(Config{Agents: singleAgent(agent.NewFake("ok")), Store: store, Worktrees: wt, Workspace: ws, Model: "m", Profiles: loaded})
	t.Cleanup(func() { e.Close() })

	if rc, _ := e.resolveRole("std", agent.RoleScribe); rc.Model != "claude-haiku-4.5" {
		t.Fatalf("scribe model = %q before the edit", rc.Model)
	}

	writeProfiles(t, path, scribeProfiles("claude-haiku-4-5"), t0.Add(time.Minute))
	if rc, _ := e.resolveRole("std", agent.RoleScribe); rc.Model != "claude-haiku-4-5" {
		t.Fatalf("scribe model = %q after the edit, want the edited claude-haiku-4-5", rc.Model)
	}
	if st := e.ProfilesState(); st.Reloads != 1 || st.Refused != "" {
		t.Errorf("ProfilesState = %+v, want one reload and nothing refused", st)
	}

	// a file that does not parse is refused, loudly, and changes nothing
	writeProfiles(t, path, "profiles: [not: a map\n", t0.Add(2*time.Minute))
	if rc, _ := e.resolveRole("std", agent.RoleScribe); rc.Model != "claude-haiku-4-5" {
		t.Fatalf("a broken edit replaced the profiles: scribe model = %q", rc.Model)
	}
	if st := e.ProfilesState(); st.Refused == "" {
		t.Error("a broken profiles.yaml was not reported as refused")
	}

	// a role routed to a backend this process never started is refused too
	writeProfiles(t, path, "default: std\nprofiles:\n  std:\n    scribe:\n      backend: opencode\n      model: x\n", t0.Add(3*time.Minute))
	if rc, _ := e.resolveRole("std", agent.RoleScribe); rc.Model != "claude-haiku-4-5" {
		t.Fatalf("an edit naming an unstarted backend was applied: scribe model = %q", rc.Model)
	}
	if st := e.ProfilesState(); !strings.Contains(st.Refused, "opencode") || !strings.Contains(st.Refused, "restart") {
		t.Errorf("refusal = %q, want it to name the backend and the restart", st.Refused)
	}
}
