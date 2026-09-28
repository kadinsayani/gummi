package web

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/morphis/gummi/internal/atomicfile"
)

// How a browser earns the right to the board, in two steps.
//
// Step one is a **pairing code**: six digits gummi prints in the terminal
// running the server, live for three minutes and dead after three wrong
// guesses. It is deliberately short, because it is typed on a phone, and
// deliberately short-lived, because a six-digit secret is only safe while
// a guesser gets a handful of tries at it.
//
// Step two is a **device token**: 256 bits of entropy the server hands
// back once a code is redeemed, kept by the browser as an HttpOnly cookie
// and by gummi as a SHA-256 hash. Hashing is the difference between a
// leaked devices.json being an inconvenience and being a key — gummi
// never needs the token back, only the ability to recognize it.
const (
	// codeTTL is how long a minted pairing code stays redeemable.
	codeTTL = 3 * time.Minute
	// codeAttempts is how many wrong guesses a code survives.
	codeAttempts = 3
	// deviceTTL expires a paired device that has not been seen. It is the
	// cookie's Max-Age too, so the two sides forget on the same schedule.
	deviceTTL = 90 * 24 * time.Hour
	// lastSeenResolution throttles last-seen writes: a device in active
	// use would otherwise rewrite the file on every request, and the only
	// question the timestamp answers is "within the last 90 days".
	lastSeenResolution = time.Hour

	// wrongGuessBudget is how many wrong guesses pairing takes, across
	// every code and every caller, inside wrongGuessWindow before it locks
	// for pairingLockout. A code's own three guesses bound one code; this
	// bounds a guesser who burns codes and asks for new ones, from as many
	// addresses as they like — the per-address rate limits cannot, since
	// addresses are cheap (an IPv6 /64 is billions of them).
	wrongGuessBudget = 10
	wrongGuessWindow = 15 * time.Minute
	pairingLockout   = 15 * time.Minute
)

// Pairing errors the HTTP layer turns into status codes. Redeeming
// reports which of them happened so the page can say something true —
// "two tries left" and "that code expired" are different problems.
var (
	// ErrNoCode is returned when no code has been minted (or the last one
	// was already redeemed).
	ErrNoCode = errors.New("no pairing code is live; run `gummi web pair`")
	// ErrCodeExpired is returned for a code past its three minutes.
	ErrCodeExpired = errors.New("that pairing code expired")
	// ErrCodeBurned is returned once the guess budget is spent.
	ErrCodeBurned = errors.New("that pairing code is dead — too many wrong guesses")
	// ErrCodeLive is returned by Request while a code is live: it is
	// already in the terminal, and minting another would only reset its
	// guesses.
	ErrCodeLive = errors.New("a pairing code is already showing in the terminal running `gummi web`; use that one")
)

// LockedError is pairing refusing everything after too many wrong guesses
// across codes, until Until (or until `gummi web pair` mints a code).
type LockedError struct{ Until time.Time }

func (e *LockedError) Error() string {
	return "pairing is locked after too many wrong guesses; try again later, or run `gummi web pair` on the machine hosting the board"
}

// WrongCodeError is a wrong guess against a live code, carrying what is
// left of its budget so the page can count down honestly.
type WrongCodeError struct{ Remaining int }

func (e *WrongCodeError) Error() string {
	return fmt.Sprintf("wrong pairing code (%s left)", tries(e.Remaining))
}

// tries counts a guess budget in words, so the page can say "1 try left"
// rather than "1 tries left".
func tries(n int) string {
	if n == 1 {
		return "1 try"
	}
	return fmt.Sprintf("%d tries", n)
}

// Pairing holds the one live pairing code. One at a time is the whole
// policy: minting a second code invalidates the first, so a code read off
// a terminal is either the current one or no longer a key.
//
// Only the operator mints over a live code (Mint, MintFor: the server's
// start and `gummi web pair`). A browser asking for a code (Request) gets
// one only when none is live, so asking cannot reset a code's guesses or
// kill a code printed for somebody by name. Wrong guesses are also counted
// across codes, and too many lock pairing for a while (LockedError).
type Pairing struct {
	now func() time.Time

	mu       sync.Mutex
	code     string
	expires  time.Time
	attempts int
	// person is who the live code was minted for, empty when the browser
	// redeeming it says who it is.
	person string
	// wrong are the recent wrong guesses, oldest first; lockedUntil is
	// when a lockout they caused ends.
	wrong       []time.Time
	lockedUntil time.Time
}

// NewPairing returns an empty pairing slot. now is injectable so tests
// can age a code without sleeping.
func NewPairing(now func() time.Time) *Pairing {
	if now == nil {
		now = time.Now
	}
	return &Pairing{now: now}
}

// Mint generates a fresh six-digit code, replacing any live one, and
// reports when it dies.
func (p *Pairing) Mint() (code string, expires time.Time, err error) {
	return p.MintFor("")
}

// MintFor is Mint for a named person: whoever redeems the code pairs as
// person, whatever name their browser gives. It is the operator's mint, so
// it also lifts a lockout: the person at the machine is asking to pair.
func (p *Pairing) MintFor(person string) (code string, expires time.Time, err error) {
	code, err = newCode()
	if err != nil {
		return "", time.Time{}, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.wrong, p.lockedUntil = nil, time.Time{}
	p.setLocked(code, person)
	return p.code, p.expires, nil
}

// Request is a browser asking for a code: a fresh, unnamed one, unless a
// code is live already (ErrCodeLive, with when it dies) or pairing is
// locked (*LockedError). It never replaces a live code.
func (p *Pairing) Request() (code string, expires time.Time, err error) {
	code, err = newCode()
	if err != nil {
		return "", time.Time{}, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	now := p.now()
	if now.Before(p.lockedUntil) {
		return "", time.Time{}, &LockedError{Until: p.lockedUntil}
	}
	if p.liveLocked() {
		return "", p.expires, ErrCodeLive
	}
	p.setLocked(code, "")
	return p.code, p.expires, nil
}

func newCode() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		return "", fmt.Errorf("generating a pairing code: %w", err)
	}
	return fmt.Sprintf("%06d", n.Int64()), nil
}

func (p *Pairing) setLocked(code, person string) {
	p.code = code
	p.expires = p.now().Add(codeTTL)
	p.attempts = codeAttempts
	p.person = person
}

func (p *Pairing) liveLocked() bool {
	return p.code != "" && p.attempts > 0 && p.now().Before(p.expires)
}

// lockedUntilTime is when the current lockout ends (zero when there has
// been none), so the caller can tell the moment a guess started one.
func (p *Pairing) lockedUntilTime() time.Time {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.lockedUntil
}

// Live reports whether a code is currently redeemable.
func (p *Pairing) Live() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.liveLocked() && !p.now().Before(p.lockedUntil)
}

// LiveFor reports the person the live code was minted for, empty when it
// was minted for nobody in particular or no code is live.
func (p *Pairing) LiveFor() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.code == "" || p.attempts <= 0 || !p.now().Before(p.expires) {
		return ""
	}
	return p.person
}

// Redeem checks a guess. A correct one consumes the code — a code is a
// one-device key, not a password.
func (p *Pairing) Redeem(guess string) error {
	_, err := p.RedeemFor(guess)
	return err
}

// RedeemFor is Redeem that also reports the person the code was minted
// for (empty for an unnamed code).
func (p *Pairing) RedeemFor(guess string) (person string, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := p.now()
	switch {
	case now.Before(p.lockedUntil):
		return "", &LockedError{Until: p.lockedUntil}
	case p.code == "":
		return "", ErrNoCode
	case p.attempts <= 0:
		return "", ErrCodeBurned
	case !now.Before(p.expires):
		p.code, p.attempts = "", 0
		return "", ErrCodeExpired
	}
	// Constant time, even though the code is short-lived and rate limited:
	// a timing oracle on a six-digit secret is exactly the kind of thing
	// that turns "1 in a million per window" into "a few hundred tries".
	if subtle.ConstantTimeCompare([]byte(p.code), []byte(guess)) != 1 {
		p.attempts--
		if p.countWrongLocked(now) {
			p.code, p.attempts, p.person = "", 0, ""
			return "", &LockedError{Until: p.lockedUntil}
		}
		if p.attempts <= 0 {
			p.code = ""
			return "", ErrCodeBurned
		}
		return "", &WrongCodeError{Remaining: p.attempts}
	}
	person = p.person
	p.code, p.attempts, p.person = "", 0, ""
	return person, nil
}

// countWrongLocked records a wrong guess against the budget shared by
// every code, and reports whether it just locked pairing.
func (p *Pairing) countWrongLocked(now time.Time) bool {
	cut := 0
	for cut < len(p.wrong) && now.Sub(p.wrong[cut]) >= wrongGuessWindow {
		cut++
	}
	p.wrong = append(p.wrong[cut:], now)
	if len(p.wrong) < wrongGuessBudget {
		return false
	}
	p.wrong, p.lockedUntil = nil, now.Add(pairingLockout)
	return true
}

// Device is one paired browser. The token itself is never stored.
//
// Person is the name given when pairing; devices paired under one name are
// one person (DESIGN §20.3), and it is what receipts, notes and the viewer
// list carry. Name is the device's own label, read off its User-Agent.
type Device struct {
	ID          string    `json:"id"`
	Person      string    `json:"person"`
	Name        string    `json:"name"`
	TokenSHA256 string    `json:"token_sha256"`
	PairedAt    time.Time `json:"paired_at"`
	LastSeen    time.Time `json:"last_seen"`
}

// devicesFile is the on-disk shape. Version exists so a later format can
// be recognized rather than misread.
type devicesFile struct {
	Version int      `json:"version"`
	Devices []Device `json:"devices"`
}

const devicesVersion = 1

// Devices is the paired-device store: a JSON file under the workspace's
// web dir, mode 0600, rewritten atomically.
//
// The file is the single source of truth, re-read whenever it changes on
// disk. That is what lets `gummi web unpair` be a file edit rather than
// an IPC call to a running server: revoking a device is a write, and the
// next request the server serves already knows.
type Devices struct {
	path string
	now  func() time.Time

	mu      sync.Mutex
	devices []Device
	// stamp is the file state we last read, so an external write (an
	// unpair from another terminal) is noticed and a self-write is not
	// re-read for nothing.
	stamp fileStamp
}

type fileStamp struct {
	mtime time.Time
	size  int64
}

func statStamp(path string) (fileStamp, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return fileStamp{}, err
	}
	return fileStamp{mtime: fi.ModTime(), size: fi.Size()}, nil
}

// OpenDevices loads the store at path, creating neither the file nor its
// directory until the first device is paired.
func OpenDevices(path string, now func() time.Time) (*Devices, error) {
	if now == nil {
		now = time.Now
	}
	d := &Devices{path: path, now: now}
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.loadLocked(); err != nil {
		return nil, err
	}
	return d, nil
}

func (d *Devices) loadLocked() error {
	b, err := os.ReadFile(d.path)
	if errors.Is(err, os.ErrNotExist) {
		d.devices, d.stamp = nil, fileStamp{}
		return nil
	}
	if err != nil {
		return fmt.Errorf("reading %s: %w", d.path, err)
	}
	var f devicesFile
	if err := json.Unmarshal(b, &f); err != nil {
		return fmt.Errorf("parsing %s: %w", d.path, err)
	}
	if f.Version != devicesVersion {
		return fmt.Errorf("%s has format version %d, which this gummi does not understand", d.path, f.Version)
	}
	d.devices = f.Devices
	d.stamp, _ = statStamp(d.path)
	return nil
}

// refreshLocked re-reads the file when it changed underneath us. A stat
// error (including a file somebody deleted) collapses to "no devices",
// which is the safe direction: a missing store pairs nobody in.
func (d *Devices) refreshLocked() {
	st, err := statStamp(d.path)
	if errors.Is(err, os.ErrNotExist) {
		d.devices, d.stamp = nil, fileStamp{}
		return
	}
	if err != nil || st == d.stamp {
		return
	}
	_ = d.loadLocked()
}

func (d *Devices) saveLocked() error {
	if err := os.MkdirAll(filepath.Dir(d.path), 0o700); err != nil {
		return fmt.Errorf("preparing %s: %w", filepath.Dir(d.path), err)
	}
	b, err := json.MarshalIndent(devicesFile{Version: devicesVersion, Devices: d.devices}, "", "  ")
	if err != nil {
		return err
	}
	if err := atomicfile.Write(d.path, append(b, '\n'), 0o600); err != nil {
		return fmt.Errorf("writing %s: %w", d.path, err)
	}
	d.stamp, _ = statStamp(d.path)
	return nil
}

// Pair records a new device for person and returns its token — the only
// time the token exists outside the browser.
func (d *Devices) Pair(person, name string) (token string, dev Device, err error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", Device{}, fmt.Errorf("generating a device token: %w", err)
	}
	token = base64.RawURLEncoding.EncodeToString(raw)
	id := make([]byte, 4)
	if _, err := rand.Read(id); err != nil {
		return "", Device{}, fmt.Errorf("generating a device id: %w", err)
	}
	now := d.now()
	dev = Device{
		ID:          hex.EncodeToString(id),
		Person:      person,
		Name:        name,
		TokenSHA256: hashToken(token),
		PairedAt:    now,
		LastSeen:    now,
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	d.refreshLocked()
	d.devices = append(d.devices, dev)
	if err := d.saveLocked(); err != nil {
		return "", Device{}, err
	}
	return token, dev, nil
}

// Verify recognizes a token, expiring devices unseen for deviceTTL. It
// touches last-seen at most once an hour so an open board does not
// rewrite the file on every request.
func (d *Devices) Verify(token string) (Device, bool) {
	dev, ok, _ := d.VerifyTouch(token)
	return dev, ok
}

// VerifyTouch is Verify that also reports whether it moved the device's
// last-seen forward — the moment the cookie's own lifetime should slide
// with it, or an active device would be logged out at day 90 while the
// server still considered it fresh.
func (d *Devices) VerifyTouch(token string) (dev Device, ok, touched bool) {
	if token == "" {
		return Device{}, false, false
	}
	want := hashToken(token)

	d.mu.Lock()
	defer d.mu.Unlock()
	d.refreshLocked()
	now := d.now()
	for i := range d.devices {
		if subtle.ConstantTimeCompare([]byte(d.devices[i].TokenSHA256), []byte(want)) != 1 {
			continue
		}
		if now.Sub(d.devices[i].LastSeen) > deviceTTL {
			// Expired: drop it rather than leaving a dead row that would
			// come back to life the moment somebody replayed the cookie.
			d.devices = append(d.devices[:i], d.devices[i+1:]...)
			_ = d.saveLocked()
			return Device{}, false, false
		}
		if now.Sub(d.devices[i].LastSeen) >= lastSeenResolution {
			d.devices[i].LastSeen = now
			_ = d.saveLocked()
			touched = true
		}
		return d.devices[i], true, touched
	}
	return Device{}, false, false
}

// Has reports whether device id is still paired (and not expired). An open
// event stream asks on every heartbeat, so a device unpaired from another
// terminal stops hearing about the board.
func (d *Devices) Has(id string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.refreshLocked()
	now := d.now()
	for _, dev := range d.devices {
		if dev.ID == id {
			return now.Sub(dev.LastSeen) <= deviceTTL
		}
	}
	return false
}

// List returns the paired devices, newest pairing last.
func (d *Devices) List() []Device {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.refreshLocked()
	out := append([]Device(nil), d.devices...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].PairedAt.Before(out[j].PairedAt) })
	return out
}

// Forget removes one device by id (or by a unique id prefix, since that
// is what a person types off `gummi web devices`).
func (d *Devices) Forget(id string) (Device, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.refreshLocked()
	var (
		match = -1
		count int
	)
	for i := range d.devices {
		if d.devices[i].ID == id {
			match, count = i, 1
			break
		}
		if len(id) > 0 && len(id) < len(d.devices[i].ID) && d.devices[i].ID[:len(id)] == id {
			match = i
			count++
		}
	}
	switch {
	case count == 0:
		return Device{}, fmt.Errorf("no paired device with id %q", id)
	case count > 1:
		return Device{}, fmt.Errorf("%q matches more than one paired device; use the full id", id)
	}
	dev := d.devices[match]
	d.devices = append(d.devices[:match], d.devices[match+1:]...)
	if err := d.saveLocked(); err != nil {
		return Device{}, err
	}
	return dev, nil
}

// ForgetAll unpairs every device and reports how many went.
func (d *Devices) ForgetAll() (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.refreshLocked()
	n := len(d.devices)
	if n == 0 {
		return 0, nil
	}
	d.devices = nil
	if err := d.saveLocked(); err != nil {
		return 0, err
	}
	return n, nil
}

// Count reports how many devices are paired.
func (d *Devices) Count() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.refreshLocked()
	return len(d.devices)
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
