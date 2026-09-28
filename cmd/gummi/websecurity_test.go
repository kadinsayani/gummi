package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/morphis/gummi/internal/state"
	"github.com/morphis/gummi/internal/web"
	"github.com/morphis/gummi/internal/web/push"
	"github.com/morphis/gummi/internal/webapi"
)

// A second `gummi web` on the address the first holds is refused by the
// lock, naming the holder and where it serves — not by the kernel's
// "address already in use".
func TestASecondWebOnTheSameAddressNamesTheHolder(t *testing.T) {
	root := boardRepo(t)
	ws, err := ensureWorkspace(root, root)
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	addr := ln.Addr().String()
	release, err := state.AcquireInstance(ws, state.InstanceHolder{Host: state.HostWeb, URL: "http://" + addr})
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	err = runCLI("web", "--addr", addr)
	if err == nil || !strings.Contains(err.Error(), "served by gummi web at http://"+addr) || strings.Contains(err.Error(), "in use") {
		t.Errorf("a second web on %s = %v, want the lock naming the holder", addr, err)
	}
}

// SIGHUP — the terminal closed, the tmux pane killed — unwinds `gummi web`
// like Ctrl-C: the instance record and server.json go with it.
func TestWebCleansUpOnHangup(t *testing.T) {
	root := boardRepo(t)
	ws, err := ensureWorkspace(root, root)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- runCLI("web", "--addr", "127.0.0.1:0") }()
	serverJSON := filepath.Join(ws.WebDir(), serverFile)
	deadline := time.Now().Add(20 * time.Second)
	for {
		if _, err := os.Stat(serverJSON); err == nil {
			break
		}
		select {
		case err := <-done:
			t.Fatalf("gummi web stopped before serving: %v", err)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("gummi web never wrote server.json")
		}
		time.Sleep(20 * time.Millisecond)
	}
	holder, err := state.ReadInstanceHolder(ws)
	if err != nil || !strings.HasPrefix(holder.URL, "http://127.0.0.1:") {
		t.Fatalf("instance record = %+v, %v; want the listener's URL", holder, err)
	}
	if err := syscall.Kill(os.Getpid(), syscall.SIGHUP); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("gummi web on SIGHUP = %v", err)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("gummi web did not stop on SIGHUP")
	}
	for _, f := range []string{ws.InstanceFile(), serverJSON} {
		if _, err := os.Stat(f); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s outlived the hangup (%v)", f, err)
		}
	}
}

// The TUI's half: SIGHUP quits the program, so runBoard's deferred Close
// runs.
func TestTheBoardQuitsOnHangup(t *testing.T) {
	p := tea.NewProgram(idleModel{}, tea.WithInput(nil), tea.WithOutput(io.Discard))
	stop := quitOnHangup(p)
	defer stop()
	done := make(chan error, 1)
	go func() { _, err := p.Run(); done <- err }()
	time.Sleep(50 * time.Millisecond)
	if err := syscall.Kill(os.Getpid(), syscall.SIGHUP); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the program did not quit on SIGHUP")
	}
}

type idleModel struct{}

func (idleModel) Init() tea.Cmd                         { return nil }
func (m idleModel) Update(tea.Msg) (tea.Model, tea.Cmd) { return m, nil }
func (idleModel) View() tea.View                        { return tea.NewView("") }

func TestAdminTarget(t *testing.T) {
	for addr, want := range map[string]string{
		"127.0.0.1:7878": "127.0.0.1:7878",
		"[::1]:7878":     "[::1]:7878",
		"0.0.0.0:7878":   "127.0.0.1:7878",
		"[::]:7878":      "127.0.0.1:7878",
	} {
		if got, err := adminTarget(addr); err != nil || got != want {
			t.Errorf("adminTarget(%s) = %q, %v; want %q", addr, got, err, want)
		}
	}
	if _, err := adminTarget("192.168.1.5:7878"); err == nil || !strings.Contains(err.Error(), "listens only on 192.168.1.5:7878") {
		t.Errorf("adminTarget on a LAN address = %v, want an explanation", err)
	}
}

// `gummi web pair` reaches a --tls-cert server over HTTPS.
func TestWebPairSpeaksTLS(t *testing.T) {
	root := boardRepo(t)
	ws, err := ensureWorkspace(root, root)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/admin/pair" || r.Header.Get("Authorization") != "Bearer tok" {
			http.Error(w, "no", http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(webapi.AdminPairResponse{Code: "123456", ExpiresInSecs: 180})
	}))
	defer srv.Close()
	if err := os.MkdirAll(ws.WebDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := writeServerFile(ws, strings.TrimPrefix(srv.URL, "https://"), "tok", true); err != nil {
		t.Fatal(err)
	}
	out := captureStdout(t, func() { mustCLI(t, "web", "pair") })
	if !strings.Contains(out, "pairing code 123456") {
		t.Errorf("web pair printed %q", out)
	}
	// and a refusal says why
	if err := writeServerFile(ws, strings.TrimPrefix(srv.URL, "https://"), "wrong", true); err != nil {
		t.Fatal(err)
	}
	if err := runCLI("web", "pair"); err == nil || !strings.Contains(err.Error(), "401") {
		t.Errorf("web pair with a bad token = %v", err)
	}
}

// The names `gummi web` answers to: the --addr name, the certificate's,
// --allow-host's.
func TestWebHosts(t *testing.T) {
	dir := t.TempDir()
	cert, key := selfSigned(t, dir, []string{"board.example", "board"}, []net.IP{net.ParseIP("192.168.1.5")})
	hosts, err := webHosts("mybox.lan:7878", cert, key, "gummi.tail1234.ts.net, proxy.example")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"mybox.lan", "board.example", "board", "192.168.1.5", "gummi.tail1234.ts.net", "proxy.example"} {
		if !slices.Contains(hosts, want) {
			t.Errorf("hosts %v lack %s", hosts, want)
		}
	}
	if hosts, _ := webHosts("127.0.0.1:7878", "", "", ""); len(hosts) != 0 {
		t.Errorf("an address adds names %v", hosts)
	}
	if _, err := webHosts("127.0.0.1:0", filepath.Join(dir, "nope.pem"), key, ""); err == nil {
		t.Error("a missing certificate was accepted")
	}
}

func TestLoopbackAddr(t *testing.T) {
	for addr, want := range map[string]bool{
		"127.0.0.1:0": true, "[::1]:0": true, "localhost:7878": true, "box.lan:7878": true,
		"0.0.0.0:0": false, ":7878": false, "192.168.1.5:7878": false, "nonsense": false,
	} {
		if got := loopbackAddr(addr); got != want {
			t.Errorf("loopbackAddr(%q) = %v, want %v", addr, got, want)
		}
	}
}

// The tailnet key is read from TS_AUTHKEY too, which ps does not show, and
// --help says to prefer it.
func TestTailnetAuthKeyFromTheEnvironment(t *testing.T) {
	ws := state.Workspace{Root: t.TempDir()}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	t.Setenv("TS_AUTHKEY", "tskey-ts")
	if o := tailnetOptions(parsedFlags(t, "web", "--tailscale"), ws, ln); o.AuthKey != "tskey-ts" {
		t.Errorf("auth key = %q, want TS_AUTHKEY's", o.AuthKey)
	}
	if o := tailnetOptions(parsedFlags(t, "web", "--tailscale", "--ts-authkey", "tskey-flag"), ws, ln); o.AuthKey != "tskey-flag" {
		t.Errorf("auth key = %q, want the flag's", o.AuthKey)
	}
	out := captureStdout(t, func() { mustCLI(t, "web", "--help") })
	for _, want := range []string{"shows in ps", "--ts-authkey key", "--verbose", "--allow-host"} {
		if !strings.Contains(out, want) {
			t.Errorf("gummi web --help does not say %q", want)
		}
	}
}

// Unpairing from the CLI drops the device's notification subscription.
func TestWebUnpairDropsNotifications(t *testing.T) {
	root := boardRepo(t)
	ws, err := ensureWorkspace(root, root)
	if err != nil {
		t.Fatal(err)
	}
	devices, err := web.OpenDevices(filepath.Join(ws.WebDir(), devicesFile), nil)
	if err != nil {
		t.Fatal(err)
	}
	_, keep, err := devices.Pair("Ana", "iPhone")
	if err != nil {
		t.Fatal(err)
	}
	_, gone, err := devices.Pair("Simon", "Mac")
	if err != nil {
		t.Fatal(err)
	}
	pusher, err := web.OpenPush(ws.WebDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{keep.ID, gone.ID} {
		if err := pusher.Store.Add(testPushSub(t, id)); err != nil {
			t.Fatal(err)
		}
	}
	captureStdout(t, func() { mustCLI(t, "web", "unpair", gone.ID) })
	if _, ok := pusher.Store.Get(gone.ID); ok {
		t.Error("the unpaired device is still subscribed")
	}
	if _, ok := pusher.Store.Get(keep.ID); !ok {
		t.Error("the other device's subscription went too")
	}
	captureStdout(t, func() { mustCLI(t, "web", "unpair", "--all") })
	if subs := pusher.Store.List(); len(subs) != 0 {
		t.Errorf("after --all, subscriptions remain: %+v", subs)
	}
}

func testPushSub(t *testing.T, device string) push.Subscription {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pub, err := k.PublicKey.ECDH()
	if err != nil {
		t.Fatal(err)
	}
	auth := make([]byte, 16)
	_, _ = rand.Read(auth)
	return push.Subscription{
		Endpoint: "https://push.example/" + device,
		Keys: push.Keys{
			P256dh: base64.RawURLEncoding.EncodeToString(pub.Bytes()),
			Auth:   base64.RawURLEncoding.EncodeToString(auth),
		},
		Device: device,
	}
}

// selfSigned writes a certificate for names and ips, and its key.
func selfSigned(t *testing.T, dir string, names []string, ips []net.IP) (cert, key string) {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: names[0]},
		DNSNames:     names,
		IPAddresses:  ips,
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &k.PublicKey, k)
	if err != nil {
		t.Fatal(err)
	}
	kder, err := x509.MarshalECPrivateKey(k)
	if err != nil {
		t.Fatal(err)
	}
	cert, key = filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	if err := os.WriteFile(cert, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(key, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kder}), 0o600); err != nil {
		t.Fatal(err)
	}
	return cert, key
}
