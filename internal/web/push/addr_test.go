package push

import (
	"context"
	"errors"
	"net/netip"
	"path/filepath"
	"testing"
)

func TestOnlyPublicAddressesArePushable(t *testing.T) {
	for addr, want := range map[string]bool{
		"8.8.8.8":         true,
		"2606:4700::1111": true,
		"127.0.0.1":       false,
		"::1":             false,
		"10.1.2.3":        false,
		"192.168.1.5":     false,
		"172.16.0.1":      false,
		"169.254.169.254": false,
		"fe80::1":         false,
		"fd00::1":         false,
		"0.0.0.0":         false,
		"::":              false,
		"224.0.0.1":       false,
		"100.101.102.103": false, // a tailnet address
		"::ffff:10.0.0.1": false,
	} {
		if got := publicAddr(netip.MustParseAddr(addr)); got != want {
			t.Errorf("publicAddr(%s) = %v, want %v", addr, got, want)
		}
	}
}

// An endpoint is refused at subscribe time when it names an address, or a
// host that resolves inward; the test-only escape lets everything through.
func TestCheckEndpointRefusesInternalHosts(t *testing.T) {
	ctx := context.Background()
	for _, ep := range []string{
		"https://127.0.0.1/x",
		"https://[::1]:6379/x",
		"https://169.254.169.254/latest",
		"https://8.8.8.8/x", // an address, however public, is not a push service
		"https://localhost:8443/x",
	} {
		if err := CheckEndpoint(ctx, ep, false); !errors.Is(err, ErrPrivateEndpoint) {
			t.Errorf("CheckEndpoint(%s) = %v, want a refusal", ep, err)
		}
		if err := CheckEndpoint(ctx, ep, true); err != nil {
			t.Errorf("CheckEndpoint(%s, allowPrivate) = %v", ep, err)
		}
	}
	// a name that does not resolve is left to the dial-time check
	if err := CheckEndpoint(ctx, "https://push.example.invalid/x", false); err != nil {
		t.Errorf("an unresolvable name = %v, want it let through", err)
	}
}

// The default client refuses to connect to an internal address whatever
// the name said, and AllowPrivate is the only way past that.
func TestSenderRefusesToDialAnInternalAddress(t *testing.T) {
	p := newPushService(t)
	v, err := GenerateVAPID()
	if err != nil {
		t.Fatal(err)
	}
	s := NewSender(v, "")
	sub := p.subscribe(t, "d1", "/wpush/d1", 0)
	err = s.Send(context.Background(), sub, []byte("hello"), Options{})
	if !errors.Is(err, ErrPrivateEndpoint) {
		t.Fatalf("send to %s = %v, want a refusal to dial", sub.Endpoint, err)
	}
	if len(p.received()) != 0 {
		t.Fatal("the stand-in push service was reached")
	}
	// allowed through, the dial happens (and fails on the test server's
	// self-signed certificate, which the default client does not trust)
	s.AllowPrivate = true
	if err := s.Send(context.Background(), sub, []byte("hello"), Options{}); err == nil || errors.Is(err, ErrPrivateEndpoint) {
		t.Fatalf("send with AllowPrivate = %v, want a TLS failure past the address check", err)
	}
}

// A subscription whose device is no longer paired is dropped, not sent to.
func TestNotifierDropsUnpairedDevices(t *testing.T) {
	p := newPushService(t)
	store, err := OpenStore(filepath.Join(t.TempDir(), "push.json"), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, sub := range []Subscription{p.subscribe(t, "phone", "/phone", 0), p.subscribe(t, "revoked", "/revoked", 0)} {
		if err := store.Add(sub); err != nil {
			t.Fatal(err)
		}
	}
	n := NewNotifier(store, newTestSender(t, p))
	n.Paired = func(device string) bool { return device == "phone" }
	if res := n.Notify(context.Background(), Message{Title: "x"}); res.Sent != 1 {
		t.Fatalf("result = %+v, want one delivery", res)
	}
	if _, ok := store.Get("revoked"); ok {
		t.Error("the unpaired device's subscription is still stored")
	}
	for _, r := range p.received() {
		if r.path == "/revoked" {
			t.Error("the unpaired device was notified")
		}
	}
}

func TestRemoveDevices(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "push.json"), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{"a", "b", "c"} {
		if err := store.Add(testSub(t, d, "https://push.example/"+d)); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.RemoveDevices("a", "c", "nope"); err != nil {
		t.Fatal(err)
	}
	if subs := store.List(); len(subs) != 1 || subs[0].Device != "b" {
		t.Errorf("left %+v, want only b", subs)
	}
}
