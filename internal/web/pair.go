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
	"strings"
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
	// the codes a browser asks for, for pairingLockout — doubled at every
	// lockout that follows within lockoutMemory, up to maxLockout. A
	// code's own three guesses bound one code; this bounds a guesser who
	// burns codes and asks for new ones, from as many addresses as they
	// like — the per-address limits cannot, since addresses are cheap (an
	// IPv6 /64 is billions of them). Doubling is what makes that budget a
	// bound: ten guesses a quarter-hour is a thousand a day, and ten a
	// lockout that doubles is about seventy.
	//
	// It never locks a code the operator minted — the one printed when the
	// server starts, or by `gummi web pair` — so a guesser cannot keep the
	// operator from pairing: such a code is bounded by its own three
	// guesses, and there is only ever one when the operator asked for it.
	wrongGuessBudget = 10
	wrongGuessWindow = 15 * time.Minute
	pairingLockout   = 15 * time.Minute
	// sourceGuessBudget wrong guesses from one address (a /64 for IPv6)
	// inside wrongGuessWindow lock that address out of the codes a
	// browser asks for, for sourceLockout, doubling the same way.
	sourceGuessBudget = codeAttempts
	sourceLockout     = time.Minute
	// maxLockout caps a doubled lockout; lockoutMemory is how long without
	// a lockout before the doubling starts over.
	maxLockout    = 24 * time.Hour
	lockoutMemory = 24 * time.Hour
	// maxSources bounds how many addresses' guesses are remembered.
	maxSources = 4096
)

// CodeOrigin says who minted a pairing code, which is who a pairing that
// redeems it is announced as having come through.
type CodeOrigin string

// The code origins. Only a code a browser asked for (OriginBrowser) is
// subject to the lockouts: the other two are the operator's.
const (
	// OriginTerminal: printed in the terminal running `gummi web` when it
	// started with no device paired.
	OriginTerminal CodeOrigin = "terminal"
	// OriginCLI: minted by `gummi web pair` through the admin route.
	OriginCLI CodeOrigin = "cli"
	// OriginBrowser: asked for from the pairing form, and printed in the
	// terminal running `gummi web`.
	OriginBrowser CodeOrigin = "browser"
)

// Via is how a pairing that redeemed a code from o is announced.
func (o CodeOrigin) Via() string {
	switch o {
	case OriginCLI:
		return "via the local CLI (`gummi web pair`)"
	case OriginBrowser:
		return "with a code a browser asked for"
	}
	return "with the code printed when `gummi web` started"
}

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

// LockedError is pairing refusing a browser's code after too many wrong
// guesses, until Until: from one address (Source), or from everywhere.
// A code the operator mints (`gummi web pair`) is never locked. Started
// marks the guess that caused the lockout.
type LockedError struct {
	Until   time.Time
	Source  bool
	Started bool
}

func (e *LockedError) Error() string {
	if e.Source {
		return "this address is locked out of pairing after too many wrong guesses; try again later, or run `gummi web pair` on the machine hosting the board"
	}
	return "pairing is locked after too many wrong guesses; try again later, or run `gummi web pair` on the machine hosting the board"
}

// Redeemed is what a correct guess redeemed: the person the code was
// minted for (empty for an unnamed code), and who minted it.
type Redeemed struct {
	Person string
	Origin CodeOrigin
}

// strikes are the wrong guesses one scope (an address, or everybody) has
// made, and the lockouts they caused.
type strikes struct {
	wrong       []time.Time
	lockouts    int
	lockedUntil time.Time
	lastLock    time.Time
}

func (s *strikes) locked(now time.Time) bool { return now.Before(s.lockedUntil) }

// hit records a wrong guess at now, and reports whether it just locked
// the scope: budget guesses inside window lock it for base, doubled for
// every lockout before it within lockoutMemory, up to maxLockout.
func (s *strikes) hit(now time.Time, budget int, window, base time.Duration) bool {
	if s.lockouts > 0 && now.Sub(s.lastLock) >= lockoutMemory {
		s.lockouts = 0
	}
	cut := 0
	for cut < len(s.wrong) && now.Sub(s.wrong[cut]) >= window {
		cut++
	}
	s.wrong = append(s.wrong[cut:], now)
	if len(s.wrong) < budget {
		return false
	}
	d := maxLockout
	if s.lockouts < 16 {
		d = min(base<<s.lockouts, maxLockout)
	}
	s.wrong, s.lockedUntil, s.lastLock = nil, now.Add(d), now
	s.lockouts++
	return true
}

// idle reports a scope with nothing left to remember.
func (s *strikes) idle(now time.Time) bool {
	return !s.locked(now) && (s.lockouts == 0 || now.Sub(s.lastLock) >= lockoutMemory) &&
		(len(s.wrong) == 0 || now.Sub(s.wrong[len(s.wrong)-1]) >= wrongGuessWindow)
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
// per address and across codes, and too many lock the codes a browser asks
// for, for a while that doubles each time (LockedError) — never the
// operator's.
type Pairing struct {
	now func() time.Time

	mu       sync.Mutex
	code     string
	expires  time.Time
	attempts int
	// person is who the live code was minted for, empty when the browser
	// redeeming it says who it is; origin is who minted it.
	person string
	origin CodeOrigin
	// public are every caller's wrong guesses together; sources each
	// address's own.
	public  strikes
	sources map[string]*strikes
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
// reports when it dies: the code the terminal prints when the server
// starts.
func (p *Pairing) Mint() (code string, expires time.Time, err error) {
	return p.mint(OriginTerminal, "")
}

// MintFor is `gummi web pair`'s mint, for a named person or (empty) for
// whoever redeems it: a person named pairs as that person, whatever name
// their browser gives. It is the operator's code, which no lockout holds:
// the person at the machine is asking to pair.
func (p *Pairing) MintFor(person string) (code string, expires time.Time, err error) {
	return p.mint(OriginCLI, person)
}

func (p *Pairing) mint(origin CodeOrigin, person string) (string, time.Time, error) {
	code, err := newCode()
	if err != nil {
		return "", time.Time{}, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.setLocked(code, person, origin)
	return p.code, p.expires, nil
}

// Request is a browser asking for a code with no address to hold it to
// (RequestFrom).
func (p *Pairing) Request() (code string, expires time.Time, err error) {
	return p.RequestFrom("")
}

// RequestFrom is a browser at source asking for a code: a fresh, unnamed
// one, unless a code is live already (ErrCodeLive, with when it dies) or
// source or everybody is locked out (*LockedError). It never replaces a
// live code.
func (p *Pairing) RequestFrom(source string) (code string, expires time.Time, err error) {
	code, err = newCode()
	if err != nil {
		return "", time.Time{}, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	now := p.now()
	if s := p.sources[source]; s != nil && s.locked(now) {
		return "", time.Time{}, &LockedError{Until: s.lockedUntil, Source: true}
	}
	if p.public.locked(now) {
		return "", time.Time{}, &LockedError{Until: p.public.lockedUntil}
	}
	if p.liveLocked() {
		return "", p.expires, ErrCodeLive
	}
	p.setLocked(code, "", OriginBrowser)
	return p.code, p.expires, nil
}

func newCode() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		return "", fmt.Errorf("generating a pairing code: %w", err)
	}
	return fmt.Sprintf("%06d", n.Int64()), nil
}

func (p *Pairing) setLocked(code, person string, origin CodeOrigin) {
	p.code = code
	p.expires = p.now().Add(codeTTL)
	p.attempts = codeAttempts
	p.person = person
	p.origin = origin
}

func (p *Pairing) liveLocked() bool {
	return p.code != "" && p.attempts > 0 && p.now().Before(p.expires)
}

// source is the strikes kept for one address, made on first use.
func (p *Pairing) source(key string, now time.Time) *strikes {
	if p.sources == nil {
		p.sources = map[string]*strikes{}
	}
	s := p.sources[key]
	if s == nil {
		if len(p.sources) >= maxSources {
			for k, old := range p.sources {
				if old.idle(now) {
					delete(p.sources, k)
				}
			}
		}
		s = &strikes{}
		p.sources[key] = s
	}
	return s
}

// Live reports whether a code is currently redeemable by a browser that
// is not locked out of it itself.
func (p *Pairing) Live() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.liveLocked() && (p.origin != OriginBrowser || !p.public.locked(p.now()))
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
	r, err := p.RedeemFrom("", guess)
	return r.Person, err
}

// RedeemFrom checks a guess from source (an address, as sourceKey reads
// it). A code a browser asked for is refused outright to an address, or
// to everybody, locked out by earlier wrong guesses; the operator's code
// is refused to nobody but its own three wrong guesses. Every wrong guess
// counts against both scopes, whichever code it was made against.
func (p *Pairing) RedeemFrom(source, guess string) (Redeemed, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := p.now()
	src := p.source(source, now)
	operators := p.liveLocked() && p.origin != OriginBrowser
	if !operators {
		switch {
		case src.locked(now):
			return Redeemed{}, &LockedError{Until: src.lockedUntil, Source: true}
		case p.public.locked(now):
			return Redeemed{}, &LockedError{Until: p.public.lockedUntil}
		}
	}
	switch {
	case p.code == "":
		return Redeemed{}, ErrNoCode
	case p.attempts <= 0:
		return Redeemed{}, ErrCodeBurned
	case !now.Before(p.expires):
		p.code, p.attempts = "", 0
		return Redeemed{}, ErrCodeExpired
	}
	// Constant time, even though the code is short-lived and rate limited:
	// a timing oracle on a six-digit secret is exactly the kind of thing
	// that turns "1 in a million per window" into "a few hundred tries".
	if subtle.ConstantTimeCompare([]byte(p.code), []byte(guess)) != 1 {
		p.attempts--
		srcLocked := src.hit(now, sourceGuessBudget, wrongGuessWindow, sourceLockout)
		pubLocked := p.public.hit(now, wrongGuessBudget, wrongGuessWindow, pairingLockout)
		if !operators {
			switch {
			case pubLocked:
				p.code, p.attempts, p.person = "", 0, ""
				return Redeemed{}, &LockedError{Until: p.public.lockedUntil, Started: true}
			case srcLocked:
				if p.attempts <= 0 {
					p.code = ""
				}
				return Redeemed{}, &LockedError{Until: src.lockedUntil, Source: true, Started: true}
			}
		}
		if p.attempts <= 0 {
			p.code = ""
			return Redeemed{}, ErrCodeBurned
		}
		return Redeemed{}, &WrongCodeError{Remaining: p.attempts}
	}
	r := Redeemed{Person: p.person, Origin: p.origin}
	p.code, p.attempts, p.person = "", 0, ""
	return r, nil
}

// Device is one paired browser. The token itself is never stored.
//
// Person is the name given when pairing; devices paired under one name are
// one person (DESIGN §20.3), and it is what receipts, notes and the viewer
// list carry. Name is the device's own label, read off its User-Agent.
//
// Origin is the host (with its port) the device paired on: its
// token is honoured there and nowhere else (Devices.VerifyAt). A browser
// sends a cookie to every port of the host that set it, so a token can
// reach another server on this machine; bound to its origin, it is no key
// to this board through any other name or port. Via says how it paired
// (CodeOrigin.Via), for `gummi web devices`.
type Device struct {
	ID          string    `json:"id"`
	Person      string    `json:"person"`
	Name        string    `json:"name"`
	TokenSHA256 string    `json:"token_sha256"`
	PairedAt    time.Time `json:"paired_at"`
	LastSeen    time.Time `json:"last_seen"`
	Origin      string    `json:"origin,omitempty"`
	Via         string    `json:"via,omitempty"`
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
	return d.PairAt(person, name, "", "")
}

// PairAt is Pair for a device bound to origin (see Device), paired via
// the code origin named.
func (d *Devices) PairAt(person, name, origin string, via CodeOrigin) (token string, dev Device, err error) {
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
		Origin:      origin,
		Via:         string(via),
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
	return d.VerifyAt(token, "")
}

// VerifyAt is VerifyTouch for a token presented at origin (the host and
// port the request reached this server on): a device bound to another
// origin is not recognized. A device paired before devices were bound is
// bound to the first origin it is presented at. An empty origin checks
// nothing.
func (d *Devices) VerifyAt(token, origin string) (dev Device, ok, touched bool) {
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
		if origin != "" {
			switch bound := d.devices[i].Origin; {
			case bound == "":
				d.devices[i].Origin = origin
				_ = d.saveLocked()
			case !strings.EqualFold(bound, origin):
				return Device{}, false, false
			}
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
