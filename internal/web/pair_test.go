package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// clock is an injectable time source: every deadline in this package
// (code TTL, device TTL, last-seen resolution) is checked by moving it,
// never by sleeping.
type clock struct{ t time.Time }

func newClock() *clock { return &clock{t: time.Date(2026, 9, 19, 10, 0, 0, 0, time.UTC)} }

func (c *clock) now() time.Time      { return c.t }
func (c *clock) add(d time.Duration) { c.t = c.t.Add(d) }

func TestPairingCodeRedeemsOnce(t *testing.T) {
	c := newClock()
	p := NewPairing(c.now)
	code, expires, err := p.Mint()
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}
	if len(code) != 6 {
		t.Fatalf("code %q is not six digits", code)
	}
	if got := expires.Sub(c.now()); got != codeTTL {
		t.Errorf("code lives %v, want %v", got, codeTTL)
	}
	if err := p.Redeem(code); err != nil {
		t.Fatalf("Redeem(correct): %v", err)
	}
	// A code is a one-device key: the same digits must not work twice.
	if err := p.Redeem(code); !errors.Is(err, ErrNoCode) {
		t.Errorf("second Redeem = %v, want ErrNoCode", err)
	}
}

func TestPairingCodeExpires(t *testing.T) {
	c := newClock()
	p := NewPairing(c.now)
	code, _, err := p.Mint()
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}
	c.add(codeTTL + time.Second)
	if p.Live() {
		t.Error("Live() is true past the TTL")
	}
	if err := p.Redeem(code); !errors.Is(err, ErrCodeExpired) {
		t.Errorf("Redeem(expired) = %v, want ErrCodeExpired", err)
	}
}

func TestPairingCodeBurnsAfterThreeWrongGuesses(t *testing.T) {
	c := newClock()
	p := NewPairing(c.now)
	code, _, err := p.Mint()
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}
	wrong := "000000"
	if wrong == code {
		wrong = "111111"
	}
	for want := codeAttempts - 1; want >= 1; want-- {
		var wc *WrongCodeError
		err := p.Redeem(wrong)
		if !errors.As(err, &wc) {
			t.Fatalf("Redeem(wrong) = %v, want *WrongCodeError", err)
		}
		if wc.Remaining != want {
			t.Errorf("remaining = %d, want %d", wc.Remaining, want)
		}
	}
	if err := p.Redeem(wrong); !errors.Is(err, ErrCodeBurned) {
		t.Fatalf("last wrong guess = %v, want ErrCodeBurned", err)
	}
	// And the real code is dead too — that is the point of burning it.
	if err := p.Redeem(code); err == nil {
		t.Error("the correct code still redeemed after the guess budget was spent")
	}
}

func TestMintReplacesTheLiveCode(t *testing.T) {
	c := newClock()
	p := NewPairing(c.now)
	first, _, err := p.Mint()
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}
	second, _, err := p.Mint()
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}
	if first == second {
		t.Skip("two mints drew the same six digits; nothing to prove here")
	}
	var wc *WrongCodeError
	if err := p.Redeem(first); !errors.As(err, &wc) {
		t.Errorf("the replaced code redeemed as %v, want a wrong-code error", err)
	}
}

func devicesPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "web", "devices.json")
}

func TestPairAndVerify(t *testing.T) {
	c := newClock()
	d, err := OpenDevices(devicesPath(t), c.now)
	if err != nil {
		t.Fatalf("OpenDevices: %v", err)
	}
	if d.Count() != 0 {
		t.Fatalf("a fresh store has %d devices, want 0", d.Count())
	}
	token, dev, err := d.Pair("Simon", "iPhone")
	if err != nil {
		t.Fatalf("Pair: %v", err)
	}
	got, ok := d.Verify(token)
	if !ok {
		t.Fatal("Verify rejected the token it just issued")
	}
	if got.ID != dev.ID || got.Name != "iPhone" || got.Person != "Simon" {
		t.Errorf("Verify returned %+v, want the paired device %+v", got, dev)
	}
	if _, ok := d.Verify(token + "x"); ok {
		t.Error("Verify accepted a token that is not stored")
	}
	if _, ok := d.Verify(""); ok {
		t.Error("Verify accepted the empty token")
	}
}

// The file must never carry the token itself: a devices.json that leaks
// should cost you a re-pair, not the board.
func TestDevicesFileStoresOnlyAHash(t *testing.T) {
	c := newClock()
	path := devicesPath(t)
	d, err := OpenDevices(path, c.now)
	if err != nil {
		t.Fatalf("OpenDevices: %v", err)
	}
	token, _, err := d.Pair("Simon", "laptop")
	if err != nil {
		t.Fatalf("Pair: %v", err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read devices file: %v", err)
	}
	if string(b) == "" {
		t.Fatal("devices file is empty")
	}
	if strings.Contains(string(b), token) {
		t.Error("devices.json contains the raw token")
	}
	if !strings.Contains(string(b), hashToken(token)) {
		t.Error("devices.json does not contain the token hash")
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Errorf("devices.json mode is %v, want 0600", perm)
	}
	var parsed devicesFile
	if err := json.Unmarshal(b, &parsed); err != nil {
		t.Fatalf("devices.json is not valid JSON: %v", err)
	}
	if parsed.Version != devicesVersion {
		t.Errorf("version = %d, want %d", parsed.Version, devicesVersion)
	}
}

func TestVerifyExpiresAnUnseenDevice(t *testing.T) {
	c := newClock()
	d, err := OpenDevices(devicesPath(t), c.now)
	if err != nil {
		t.Fatalf("OpenDevices: %v", err)
	}
	token, _, err := d.Pair("Simon", "old phone")
	if err != nil {
		t.Fatalf("Pair: %v", err)
	}
	c.add(deviceTTL + time.Hour)
	if _, ok := d.Verify(token); ok {
		t.Fatal("Verify accepted a device unseen for longer than the TTL")
	}
	if d.Count() != 0 {
		t.Errorf("the expired device is still in the store (%d left)", d.Count())
	}
}

func TestVerifyTouchesLastSeenAtMostHourly(t *testing.T) {
	c := newClock()
	path := devicesPath(t)
	d, err := OpenDevices(path, c.now)
	if err != nil {
		t.Fatalf("OpenDevices: %v", err)
	}
	token, _, err := d.Pair("Simon", "phone")
	if err != nil {
		t.Fatalf("Pair: %v", err)
	}
	before := d.List()[0].LastSeen

	c.add(time.Minute)
	if _, ok := d.Verify(token); !ok {
		t.Fatal("Verify rejected a live token")
	}
	if got := d.List()[0].LastSeen; !got.Equal(before) {
		t.Errorf("last_seen moved after a minute (%v → %v); it should be throttled", before, got)
	}

	c.add(lastSeenResolution)
	if _, ok := d.Verify(token); !ok {
		t.Fatal("Verify rejected a live token")
	}
	if got := d.List()[0].LastSeen; !got.Equal(c.now()) {
		t.Errorf("last_seen = %v after an hour, want %v", got, c.now())
	}
}

func TestForgetAndForgetAll(t *testing.T) {
	c := newClock()
	d, err := OpenDevices(devicesPath(t), c.now)
	if err != nil {
		t.Fatalf("OpenDevices: %v", err)
	}
	t1, dev1, err := d.Pair("Simon", "phone")
	if err != nil {
		t.Fatalf("Pair: %v", err)
	}
	c.add(time.Minute)
	t2, _, err := d.Pair("Simon", "tablet")
	if err != nil {
		t.Fatalf("Pair: %v", err)
	}
	if _, err := d.Forget(dev1.ID); err != nil {
		t.Fatalf("Forget: %v", err)
	}
	if _, ok := d.Verify(t1); ok {
		t.Error("a forgotten device still verifies")
	}
	if _, ok := d.Verify(t2); !ok {
		t.Error("forgetting one device revoked another")
	}
	if _, err := d.Forget("nope"); err == nil {
		t.Error("Forget on an unknown id returned no error")
	}
	n, err := d.ForgetAll()
	if err != nil {
		t.Fatalf("ForgetAll: %v", err)
	}
	if n != 1 {
		t.Errorf("ForgetAll reported %d, want 1", n)
	}
	if _, ok := d.Verify(t2); ok {
		t.Error("a token survived ForgetAll")
	}
}

func TestForgetAcceptsAUniquePrefix(t *testing.T) {
	c := newClock()
	d, err := OpenDevices(devicesPath(t), c.now)
	if err != nil {
		t.Fatalf("OpenDevices: %v", err)
	}
	_, dev, err := d.Pair("Simon", "phone")
	if err != nil {
		t.Fatalf("Pair: %v", err)
	}
	if _, err := d.Forget(dev.ID[:4]); err != nil {
		t.Fatalf("Forget(prefix): %v", err)
	}
	if d.Count() != 0 {
		t.Error("the device survived a prefix Forget")
	}
}

// `gummi web unpair` is a file edit, not an IPC call — a running server
// must notice the next time it checks a token.
func TestStoreRereadsAfterAnExternalEdit(t *testing.T) {
	c := newClock()
	path := devicesPath(t)
	server, err := OpenDevices(path, c.now)
	if err != nil {
		t.Fatalf("OpenDevices: %v", err)
	}
	token, _, err := server.Pair("Simon", "phone")
	if err != nil {
		t.Fatalf("Pair: %v", err)
	}
	if _, ok := server.Verify(token); !ok {
		t.Fatal("Verify rejected a live token")
	}

	// A second process (the `unpair` command) revokes it. Bump the clock
	// so the rewrite lands on a different mtime even on a coarse one.
	c.add(2 * time.Second)
	other, err := OpenDevices(path, c.now)
	if err != nil {
		t.Fatalf("OpenDevices (second handle): %v", err)
	}
	if _, err := other.ForgetAll(); err != nil {
		t.Fatalf("ForgetAll: %v", err)
	}
	if _, ok := server.Verify(token); ok {
		t.Error("the server still accepts a token another process revoked")
	}
}

func TestOpenDevicesRejectsAnUnknownVersion(t *testing.T) {
	path := devicesPath(t)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(`{"version":99,"devices":[]}`), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := OpenDevices(path, nil); err == nil {
		t.Error("OpenDevices accepted a devices.json from the future")
	}
}

func TestANamedCodeCarriesItsPerson(t *testing.T) {
	c := newClock()
	p := NewPairing(c.now)
	code, _, err := p.MintFor("Ana")
	if err != nil {
		t.Fatal(err)
	}
	if p.LiveFor() != "Ana" {
		t.Errorf("LiveFor = %q, want Ana", p.LiveFor())
	}
	person, err := p.RedeemFor(code)
	if err != nil || person != "Ana" {
		t.Errorf("RedeemFor = %q, %v; want Ana", person, err)
	}
	if p.LiveFor() != "" {
		t.Error("a redeemed code still names its person")
	}
	// an unnamed mint replaces the name too
	if _, _, err := p.MintFor("Ana"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := p.Mint(); err != nil {
		t.Fatal(err)
	}
	if p.LiveFor() != "" {
		t.Error("an unnamed code inherited the last one's person")
	}
}

// A browser asking for a code gets one only when none is live: asking
// cannot reset a code's guesses, or replace one printed for a person.
func TestRequestNeverReplacesALiveCode(t *testing.T) {
	c := newClock()
	p := NewPairing(c.now)
	code, _, err := p.MintFor("Ana")
	if err != nil {
		t.Fatal(err)
	}
	wrong := "000000"
	if wrong == code {
		wrong = "111111"
	}
	_ = p.Redeem(wrong)
	if _, _, err := p.Request(); !errors.Is(err, ErrCodeLive) {
		t.Fatalf("Request over a live code = %v, want ErrCodeLive", err)
	}
	var wc *WrongCodeError
	if err := p.Redeem(wrong); !errors.As(err, &wc) || wc.Remaining != codeAttempts-2 {
		t.Fatalf("after a Request the code has %v, want its guesses unchanged (%d left)", err, codeAttempts-2)
	}
	if p.LiveFor() != "Ana" {
		t.Error("a Request dropped the name the code was printed for")
	}
	// once the code has expired, asking mints a fresh, unnamed one
	c.add(codeTTL)
	fresh, _, err := p.Request()
	if err != nil || fresh == "" || p.LiveFor() != "" {
		t.Fatalf("Request after expiry = %q, %v (for %q)", fresh, err, p.LiveFor())
	}
}

// Wrong guesses count across codes and addresses: burning codes and
// asking for new ones from address after address runs into a lockout of
// the codes a browser asks for, which no browser can lift and which
// doubles each time it comes back — and which never holds the operator's
// own code (`gummi web pair`).
func TestWrongGuessesAcrossCodesLockPairing(t *testing.T) {
	c := newClock()
	p := NewPairing(c.now)
	var locked *LockedError
	addr := 0
	// burn asks for a code from a fresh address each time and guesses it
	// to death, the way a guesser from many addresses would, until
	// pairing locks.
	burn := func() {
		t.Helper()
		guesses := 0
		for guesses < wrongGuessBudget {
			addr++
			src := fmt.Sprintf("192.0.2.%d", addr)
			code, _, err := p.RequestFrom(src)
			if err != nil {
				t.Fatalf("Request after %d wrong guesses: %v", guesses, err)
			}
			wrong := "000000"
			if wrong == code {
				wrong = "111111"
			}
			for i := 0; i < codeAttempts && guesses < wrongGuessBudget; i++ {
				_, err = p.RedeemFrom(src, wrong)
				guesses++
				c.add(time.Second)
			}
			if guesses == wrongGuessBudget && (!errors.As(err, &locked) || locked.Source || !locked.Started) {
				t.Fatalf("guess %d = %v, want the lockout of everybody", guesses, err)
			}
		}
	}
	burn()
	if !locked.Until.Equal(c.now().Add(pairingLockout - time.Second)) {
		t.Errorf("locked until %v, want %v after the last guess", locked.Until, pairingLockout)
	}
	if _, _, err := p.RequestFrom("198.51.100.1"); !errors.As(err, &locked) {
		t.Fatalf("Request from a fresh address while locked = %v, want the lockout", err)
	}
	if p.Live() {
		t.Error("a browser's code is live while pairing is locked")
	}
	// the operator's code is redeemable while locked, from any address
	code, _, err := p.MintFor("")
	if err != nil {
		t.Fatal(err)
	}
	if !p.Live() {
		t.Error("the operator's code does not read live while pairing is locked")
	}
	if r, err := p.RedeemFrom("192.0.2.1", code); err != nil || r.Origin != OriginCLI {
		t.Errorf("the operator's code during a lockout = %+v %v", r, err)
	}
	// the lockout ends on its own
	c.add(pairingLockout)
	if _, _, err := p.RequestFrom("198.51.100.1"); err != nil {
		t.Fatalf("Request after the lockout = %v", err)
	}
	// and comes back twice as long
	c.add(codeTTL)
	burn()
	if !locked.Until.Equal(c.now().Add(2*pairingLockout - time.Second)) {
		t.Errorf("the second lockout ends %v, want %v after the last guess", locked.Until.Sub(c.now()), 2*pairingLockout)
	}
}

// One address that keeps guessing is locked out on its own — after one
// code's worth of wrong guesses, for a minute that doubles each time —
// while every other address can still ask for a code.
func TestOneAddressIsLockedOutOnItsOwn(t *testing.T) {
	c := newClock()
	p := NewPairing(c.now)
	const bad, good = "203.0.113.9", "198.51.100.7"
	var locked *LockedError
	for round, want := range []time.Duration{sourceLockout, 2 * sourceLockout, 4 * sourceLockout} {
		code, _, err := p.RequestFrom(bad)
		if err != nil {
			t.Fatalf("round %d: Request = %v", round, err)
		}
		wrong := "000000"
		if wrong == code {
			wrong = "111111"
		}
		for i := 0; i < sourceGuessBudget; i++ {
			_, err = p.RedeemFrom(bad, wrong)
		}
		if !errors.As(err, &locked) || !locked.Source || !locked.Started || !locked.Until.Equal(c.now().Add(want)) {
			t.Fatalf("round %d: the last guess = %v (%+v), want this address locked for %v", round, err, locked, want)
		}
		if _, _, err := p.RequestFrom(bad); !errors.As(err, &locked) || !locked.Source {
			t.Fatalf("round %d: the locked address asking = %v", round, err)
		}
		if _, _, err := p.RequestFrom(good); err != nil {
			t.Fatalf("round %d: another address asking = %v", round, err)
		}
		// that code expires unredeemed before the next round
		c.add(codeTTL + want)
	}
}

// Old wrong guesses leave the window: a person who mistypes now and then
// is never locked out.
func TestWrongGuessesAgeOut(t *testing.T) {
	c := newClock()
	p := NewPairing(c.now)
	for i := 0; i < 3*wrongGuessBudget; i++ {
		// a code asked for from the page, mistyped once, left to expire
		if _, _, err := p.Request(); err != nil {
			t.Fatalf("Request after %d spaced-out mistakes: %v", i, err)
		}
		var locked *LockedError
		if err := p.Redeem("not-a-code"); errors.As(err, &locked) {
			t.Fatalf("locked after %d spaced-out mistakes", i+1)
		}
		c.add(max(codeTTL, wrongGuessWindow/(sourceGuessBudget-1)))
	}
}

// A pairing is bound to the host and port it was made on: the same token
// presented through another is not recognized. A device paired before
// devices were bound takes the first origin it is used at.
func TestADeviceIsBoundToItsOrigin(t *testing.T) {
	d, err := OpenDevices(filepath.Join(t.TempDir(), "devices.json"), nil)
	if err != nil {
		t.Fatal(err)
	}
	token, _, err := d.PairAt("Simon", "Linux", "127.0.0.1:7878", OriginCLI)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := d.VerifyAt(token, "127.0.0.1:7878"); !ok {
		t.Error("refused at its own origin")
	}
	if _, ok, _ := d.VerifyAt(token, "127.0.0.1:9999"); ok {
		t.Error("recognized on another port")
	}
	legacy, _, err := d.Pair("Ana", "Phone")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := d.VerifyAt(legacy, "gummi.example"); !ok {
		t.Error("an unbound device was refused at its first origin")
	}
	if _, ok, _ := d.VerifyAt(legacy, "127.0.0.1:7878"); ok {
		t.Error("an unbound device was not bound by its first use")
	}
}

func TestVerifyTouchReportsASlide(t *testing.T) {
	c := newClock()
	d, err := OpenDevices(devicesPath(t), c.now)
	if err != nil {
		t.Fatal(err)
	}
	token, dev, err := d.Pair("Simon", "phone")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, touched := d.VerifyTouch(token); !ok || touched {
		t.Errorf("fresh device = %v touched %v, want ok and untouched", ok, touched)
	}
	c.add(lastSeenResolution)
	if _, ok, touched := d.VerifyTouch(token); !ok || !touched {
		t.Errorf("an hour on = %v touched %v, want ok and touched", ok, touched)
	}
	if !d.Has(dev.ID) {
		t.Error("Has is false for a paired device")
	}
	if _, err := d.Forget(dev.ID); err != nil {
		t.Fatal(err)
	}
	if d.Has(dev.ID) {
		t.Error("Has is true for an unpaired device")
	}
}
