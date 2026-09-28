package main

import (
	"encoding/json"
	"fmt"
	"net"
	"path/filepath"
	"strings"
	"testing"

	"github.com/morphis/gummi/internal/state"
	"github.com/morphis/gummi/internal/web"
)

func TestWebRefusesNoPairingOffLoopback(t *testing.T) {
	boardRepo(t)
	err := runCLI("web", "--addr", "0.0.0.0:0", "--no-pairing")
	if err == nil || !strings.Contains(err.Error(), "only allowed on a loopback address") {
		t.Errorf("web --no-pairing on 0.0.0.0 = %v, want a refusal", err)
	}
}

// --tailscale is the only way off loopback that --no-pairing cannot follow,
// and a --ts-* flag without it would configure a node nobody starts.
func TestWebTailnetFlagRefusals(t *testing.T) {
	boardRepo(t)
	err := runCLI("web", "--addr", "127.0.0.1:0", "--tailscale", "--no-pairing")
	if err == nil || !strings.Contains(err.Error(), "drop --tailscale") {
		t.Errorf("web --tailscale --no-pairing = %v, want a refusal", err)
	}
	for _, argv := range [][]string{
		{"--ts-hostname", "board"},
		{"--ts-authkey", "tskey-auth-x"},
		{"--ts-tls"},
		{"--verbose"},
	} {
		err := runCLI(append([]string{"web", "--addr", "127.0.0.1:0"}, argv...)...)
		if err == nil || !strings.Contains(err.Error(), "it needs --tailscale") {
			t.Errorf("web %v without --tailscale = %v, want a refusal", argv, err)
		}
	}
}

// The node's configuration: state under the web dir, HTTP on the loopback
// port or HTTPS on 443, and an auth key from the flag before TS_AUTHKEY.
func TestTailnetOptionsFromFlags(t *testing.T) {
	ws := state.Workspace{Root: t.TempDir()}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	_, port, _ := net.SplitHostPort(ln.Addr().String())

	t.Setenv("TS_AUTHKEY", "tskey-from-env")
	o := tailnetOptions(parsedFlags(t, "web", "--tailscale"), ws, ln)
	if o.Hostname != "gummi" || o.TLS || fmt.Sprint(o.Port) != port || o.AuthKey != "tskey-from-env" || o.Verbose {
		t.Errorf("defaults = %+v, want gummi, plain HTTP on %s, the env key", o, port)
	}
	if want := filepath.Join(ws.WebDir(), "tsnet"); o.StateDir != want {
		t.Errorf("state dir = %q, want %q", o.StateDir, want)
	}

	o = tailnetOptions(parsedFlags(t, "web", "--tailscale", "--ts-hostname", "board", "--ts-tls", "--ts-authkey", "tskey-flag", "--verbose"), ws, ln)
	if o.Hostname != "board" || !o.TLS || o.Port != 0 || o.AuthKey != "tskey-flag" || !o.Verbose {
		t.Errorf("with flags = %+v, want board, TLS on the default port, the flag's key, verbose", o)
	}
}

func TestWebHelpListsTheTailnetFlags(t *testing.T) {
	out := captureStdout(t, func() { mustCLI(t, "web", "--help") })
	for _, flag := range []string{"--tailscale", "--ts-hostname", "--ts-authkey", "--ts-tls", "--verbose", "TS_AUTHKEY"} {
		if !strings.Contains(out, flag) {
			t.Errorf("gummi web --help does not mention %s:\n%s", flag, out)
		}
	}
}

func TestWebTLSFlagsGoTogether(t *testing.T) {
	boardRepo(t)
	if err := runCLI("web", "--tls-cert", "c.pem"); err == nil || !strings.Contains(err.Error(), "go together") {
		t.Errorf("web --tls-cert alone = %v, want a refusal", err)
	}
}

// The web host refuses to start over a TUI, naming it, and leaves the
// address it bound free again.
func TestWebNamesTheTUIHoldingTheLock(t *testing.T) {
	root := boardRepo(t)
	ws, err := ensureWorkspace(root, root)
	if err != nil {
		t.Fatal(err)
	}
	release, err := state.AcquireInstance(ws, state.InstanceHolder{Host: state.HostTUI, Hostname: "desk", PID: 99})
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	err = runCLI("web", "--addr", "127.0.0.1:0")
	if err == nil || !strings.Contains(err.Error(), "this board is open in the TUI on desk (") {
		t.Errorf("web over a TUI = %v, want a refusal naming the TUI", err)
	}
}

func TestWebDevicesAndUnpair(t *testing.T) {
	root := boardRepo(t)
	ws, err := ensureWorkspace(root, root)
	if err != nil {
		t.Fatal(err)
	}
	out := captureStdout(t, func() { mustCLI(t, "web", "devices", "--json") })
	if strings.TrimSpace(out) != "[]" {
		t.Errorf("devices --json on a fresh board = %q, want []", out)
	}
	devices, err := web.OpenDevices(filepath.Join(ws.WebDir(), devicesFile), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := devices.Pair("Ana", "iPhone"); err != nil {
		t.Fatal(err)
	}
	_, dev, err := devices.Pair("Simon", "Mac")
	if err != nil {
		t.Fatal(err)
	}
	var list []web.Device
	out = captureStdout(t, func() { mustCLI(t, "web", "devices", "--json") })
	if err := json.Unmarshal([]byte(out), &list); err != nil || len(list) != 2 || list[0].Person != "Ana" {
		t.Fatalf("devices --json = %q (%v)", out, err)
	}
	if text := captureStdout(t, func() { mustCLI(t, "web", "devices") }); !strings.Contains(text, "Simon") {
		t.Errorf("devices lists %q, want the person", text)
	}

	for _, argv := range [][]string{{"web", "unpair"}, {"web", "unpair", "a", "b"}, {"web", "unpair", "--all", dev.ID}} {
		if err := runCLI(argv...); err == nil {
			t.Errorf("%v succeeded, want a usage refusal", argv)
		}
	}
	captureStdout(t, func() { mustCLI(t, "web", "unpair", dev.ID) })
	if devices.Count() != 1 {
		t.Errorf("after unpairing one, %d devices remain, want 1", devices.Count())
	}
	captureStdout(t, func() { mustCLI(t, "web", "unpair", "--all") })
	if devices.Count() != 0 {
		t.Errorf("after --all, %d devices remain", devices.Count())
	}
}

func TestWebPairNeedsARunningServer(t *testing.T) {
	boardRepo(t)
	mustCLI(t, "init")
	if err := runCLI("web", "pair"); err == nil || !strings.Contains(err.Error(), "no `gummi web` is running") {
		t.Errorf("web pair with no server = %v", err)
	}
}

func TestLoopbackOnly(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	if !loopbackOnly(ln) {
		t.Error("127.0.0.1 is not loopback?")
	}
}
