package web

import (
	"errors"
	"fmt"
	"sort"
	"time"
)

// Approval: a new device is let in by a device already at the board.
//
// A pairing code proves only that whoever typed it could read it. The
// code printed when `gummi web` starts crosses nothing but the terminal
// running it; the other two do not stay there. `gummi web pair` prints its
// code for whoever ran it, and anything running as the operator — an
// agent's shell included, which nothing confines (DESIGN §4.4) — can run
// it, or read the admin token it uses. A code a browser asks for is
// printed in that same terminal, which an agent that can read a tmux pane
// or started the server itself can read too. Such a pairing would give an
// agent the board's landing and gate-crossing, which the board-level tools
// deliberately withhold (§16).
//
// So once any device has the board, a device paired with either of those
// codes is only waiting (StatusPending): it may load the page and learn
// where it stands, and nothing else, until a person on a device already
// at the board lets it in or turns it away. The first device — none has
// the board, so nobody could be asked — and one paired with the code
// printed at start are let in at once.
//
// Letting a device in is done only from a paired page. A command for it
// would be run as easily by an agent as by the operator, which is the
// hole this closes; `gummi web unpair` withdraws a request, which opens
// nothing.
const (
	// StatusApproved is a device with the board (the empty status every
	// device paired before approval existed has).
	StatusApproved = ""
	// StatusPending is a device waiting to be let in.
	StatusPending = "pending"
	// StatusRejected is a device a person turned away.
	StatusRejected = "rejected"
	// StatusExpired is a device nobody answered in time.
	StatusExpired = "expired"
	// statusForged is a row the running server did not write and does not
	// honour (Pin). It is never stored.
	statusForged = "unrecognized"

	// pendingTTL is how long a device waits to be let in before the
	// request lapses.
	pendingTTL = 10 * time.Minute
	// maxPending requests may wait at once, and pendingBudget may be made
	// inside pendingWindow: every request is a banner on every open page
	// and a notification on every subscribed device, and asking must not
	// be a way to bury the one that matters under a dozen that do not.
	maxPending    = 3
	pendingBudget = 5
	pendingWindow = 10 * time.Minute
	// decidedKeep is how long a turned-away or lapsed request stays in
	// the file, for `gummi web devices` to show, before it is dropped.
	decidedKeep = 24 * time.Hour
	// maxUserAgent bounds the User-Agent kept for a request.
	maxUserAgent = 256
)

var (
	// ErrTooManyPending refuses a request to be let in while too many
	// wait, or too many were made lately.
	ErrTooManyPending = errors.New("too many devices are already waiting to be let in; " +
		"approve or reject them on a paired device, or wait for them to expire")
	// ErrNotPending is an answer to a request that is no longer waiting.
	ErrNotPending = errors.New("that device is no longer waiting to be let in")
	// ErrNoDevice is an answer about a device the store does not hold.
	ErrNoDevice = errors.New("no such device")
)

// Arrival is what the server knows about a browser that just redeemed a
// code: where it paired (Device.Origin), which code it used, its address
// and its User-Agent.
type Arrival struct {
	Origin    string
	Via       CodeOrigin
	Source    string
	UserAgent string
}

// statusRank orders statuses by how little they allow.
func statusRank(s string) int {
	switch s {
	case StatusApproved:
		return 0
	case StatusPending:
		return 1
	default:
		return 2
	}
}

// statusLocked is the status that applies to dev now: a wait past
// pendingTTL has lapsed, and on a pinned store a row is never kinder than
// what this process remembers of it, nor honoured when it remembers none.
func (d *Devices) statusLocked(dev Device, now time.Time) string {
	st := dev.Status
	if d.known != nil {
		mem, ok := d.known[dev.TokenSHA256]
		if !ok {
			if !d.warned[dev.TokenSHA256] {
				d.warned[dev.TokenSHA256] = true
				if d.onForged != nil {
					d.onForged(dev)
				}
			}
			return statusForged
		}
		if statusRank(mem) > statusRank(st) {
			st = mem
		}
	}
	if st == StatusPending && !now.Before(dev.PairedAt.Add(pendingTTL)) {
		return StatusExpired
	}
	return st
}

// approvedLocked counts the devices with the board.
func (d *Devices) approvedLocked(now time.Time) int {
	n := 0
	for _, dev := range d.devices {
		if d.statusLocked(dev, now) == StatusApproved {
			n++
		}
	}
	return n
}

// sweepLocked writes a lapsed wait down as expired, and drops a request
// answered (or lapsed) more than decidedKeep ago. It runs on every
// refresh, so the next save writes down what is true without a write
// of its own.
func (d *Devices) sweepLocked(now time.Time) {
	kept := d.devices[:0]
	for _, dev := range d.devices {
		if dev.Status == StatusPending && !now.Before(dev.PairedAt.Add(pendingTTL)) {
			dev.Status, dev.DecidedAt = StatusExpired, dev.PairedAt.Add(pendingTTL)
			if d.known != nil {
				if _, ok := d.known[dev.TokenSHA256]; ok {
					d.known[dev.TokenSHA256] = StatusExpired
				}
			}
		}
		if (dev.Status == StatusRejected || dev.Status == StatusExpired) && now.Sub(dev.DecidedAt) > decidedKeep {
			continue
		}
		kept = append(kept, dev)
	}
	d.devices = kept
}

// Pin makes the store the running server's: from now on it honours a
// device only as this process knows it. Every row in the file now is
// known as it stands; a row the file gains later that this process did not
// write — devices.json edited behind the server's back, by anything that
// can write the operator's files — is not honoured at all (onForged hears
// of it once), and a status the file changes to one that allows more than
// the one remembered (a waiting device written in as let in) is ignored.
// What the file takes away — an unpaired device, a status that allows
// less — still counts at once, so `gummi web unpair` keeps working as a
// file edit.
//
// Only a restart trusts the file afresh, which is the limit of what a
// store in the operator's own files can promise against the operator's
// own processes.
func (d *Devices) Pin(onForged func(Device)) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.refreshLocked()
	d.known, d.warned, d.onForged = map[string]string{}, map[string]bool{}, onForged
	for _, dev := range d.devices {
		d.known[dev.TokenSHA256] = dev.Status
	}
}

// Request stores a device that just redeemed a code, and decides whether
// it has the board at once: when it used the code printed at start, or
// when no device has the board yet. Otherwise it waits (StatusPending) —
// unless too many already wait, or too many asked lately
// (ErrTooManyPending), in which case nothing is stored.
func (d *Devices) Request(person, name string, in Arrival) (token string, dev Device, err error) {
	token, dev, err = d.mint(person, name, in)
	if err != nil {
		return "", Device{}, err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.refreshLocked()
	now := d.now()
	if in.Via != OriginTerminal && d.approvedLocked(now) > 0 {
		if err := d.roomLocked(now); err != nil {
			return "", Device{}, err
		}
		dev.Status = StatusPending
		d.asked = append(d.asked, now)
	}
	if err := d.addLocked(dev); err != nil {
		return "", Device{}, err
	}
	return token, dev, nil
}

// Room reports whether a device may ask to be let in now, so a request
// that would be refused is refused before its code is spent.
func (d *Devices) Room() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.refreshLocked()
	now := d.now()
	if d.approvedLocked(now) == 0 {
		return nil
	}
	return d.roomLocked(now)
}

func (d *Devices) roomLocked(now time.Time) error {
	cut := 0
	for cut < len(d.asked) && now.Sub(d.asked[cut]) >= pendingWindow {
		cut++
	}
	d.asked = d.asked[cut:]
	waiting := 0
	for _, dev := range d.devices {
		if d.statusLocked(dev, now) == StatusPending {
			waiting++
		}
	}
	if waiting >= maxPending || len(d.asked) >= pendingBudget {
		return ErrTooManyPending
	}
	return nil
}

// Pending lists the devices waiting to be let in, oldest first.
func (d *Devices) Pending() []Device {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.refreshLocked()
	now := d.now()
	var out []Device
	for _, dev := range d.devices {
		if d.statusLocked(dev, now) == StatusPending {
			dev.Status = StatusPending
			out = append(out, dev)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].PairedAt.Before(out[j].PairedAt) })
	return out
}

// StatusOf is where device id stands now, and false when the store does
// not hold it (or does not honour it).
func (d *Devices) StatusOf(id string) (string, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.refreshLocked()
	now := d.now()
	for _, dev := range d.devices {
		if dev.ID == id {
			st := d.statusLocked(dev, now)
			return st, st != statusForged
		}
	}
	return "", false
}

// Approve lets a waiting device in; by names who did, for the record.
func (d *Devices) Approve(id, by string) (Device, error) {
	return d.decide(id, by, StatusApproved)
}

// Reject turns a waiting device away; by names who did. The row stays,
// turned away, for decidedKeep — its token honoured by nothing — so the
// page it was on can say what happened and `gummi web devices` can show
// it.
func (d *Devices) Reject(id, by string) (Device, error) {
	return d.decide(id, by, StatusRejected)
}

func (d *Devices) decide(id, by, to string) (Device, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.refreshLocked()
	now := d.now()
	for i := range d.devices {
		if d.devices[i].ID != id {
			continue
		}
		if d.statusLocked(d.devices[i], now) != StatusPending {
			return Device{}, ErrNotPending
		}
		prev := d.devices[i]
		d.devices[i].Status, d.devices[i].DecidedAt, d.devices[i].DecidedBy = to, now, by
		if d.known != nil {
			d.known[prev.TokenSHA256] = to
		}
		if err := d.saveLocked(); err != nil {
			d.devices[i] = prev
			if d.known != nil {
				d.known[prev.TokenSHA256] = StatusPending
			}
			return Device{}, err
		}
		return d.devices[i], nil
	}
	return Device{}, fmt.Errorf("%w %q", ErrNoDevice, id)
}

// clip cuts s to at most n bytes, on a rune boundary.
func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !isRuneStart(s[n]) {
		n--
	}
	return s[:n]
}

func isRuneStart(b byte) bool { return b&0xC0 != 0x80 }
