package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/morphis/gummi/internal/state"
)

// boardRepo makes a one-commit git repo, chdirs into it, and keeps any
// agent backend from starting so a board built there stays static.
func boardRepo(t *testing.T) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	git := func(args ...string) {
		t.Helper()
		if out, err := exec.CommandContext(context.Background(), "git", append([]string{"-C", root}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q", "-b", "main")
	git("config", "user.name", "t")
	git("config", "user.email", "t@e.invalid")
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	git("add", ".")
	git("commit", "-q", "-m", "init")
	t.Chdir(root)
	// No backend on PATH and none named: buildEngine leaves the board
	// static rather than starting a real agent.
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", filepath.Dir(gitPath))
	t.Setenv("GUMMI_AGENT", "")
	t.Setenv("GUMMI_NOTIFY", "off")
	return root
}

// The TUI refuses to open over a web host, and says where the board is.
func TestRunBoardNamesTheWebHostHoldingTheLock(t *testing.T) {
	root := boardRepo(t)
	ws, err := ensureWorkspace(root, root)
	if err != nil {
		t.Fatal(err)
	}
	release, err := state.AcquireInstance(ws, state.InstanceHolder{Host: state.HostWeb, URL: "http://127.0.0.1:7878", PID: 4411})
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	err = runBoard()
	if err == nil {
		t.Fatal("runBoard opened a second board over a web host")
	}
	want := "this board is served by gummi web at http://127.0.0.1:7878 ("
	if !strings.HasPrefix(err.Error(), want) || !strings.Contains(err.Error(), "pid 4411). Open it there, or stop it.") {
		t.Errorf("runBoard err = %q, want it to name the web host", err)
	}
}

// openBoard holds the lock and the holder record for as long as the host
// is open, and Close gives both back.
func TestOpenBoardHoldsTheInstanceUntilClosed(t *testing.T) {
	root := boardRepo(t)
	h, err := openBoard(boardOpts{holder: state.InstanceHolder{Host: state.HostWeb, URL: "http://x"}})
	if err != nil {
		t.Fatal(err)
	}
	if h.shell == nil || h.store == nil || h.pool == nil {
		t.Fatalf("host missing parts: %+v", h)
	}
	if h.ws.Root != root {
		t.Errorf("workspace root = %q, want %q", h.ws.Root, root)
	}
	got, err := state.ReadInstanceHolder(h.ws)
	if err != nil || got.Host != state.HostWeb || got.URL != "http://x" {
		t.Errorf("holder = %+v, %v; want the web host recorded", got, err)
	}
	if _, err := openBoard(boardOpts{holder: state.InstanceHolder{Host: state.HostTUI}}); err == nil ||
		!strings.Contains(err.Error(), "served by gummi web at http://x") {
		t.Errorf("second openBoard err = %v, want a refusal naming the first", err)
	}
	h.Close()
	if _, err := os.Stat(h.ws.InstanceFile()); !os.IsNotExist(err) {
		t.Errorf("holder record survives Close: %v", err)
	}
	h2, err := openBoard(boardOpts{holder: state.InstanceHolder{Host: state.HostTUI}})
	if err != nil {
		t.Fatalf("reopen after Close: %v", err)
	}
	h2.Close()
}
