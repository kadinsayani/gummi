package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/morphis/gummi/internal/atomicfile"
)

// One board has one interactive host at a time (DESIGN §20.2): the TUI or
// `gummi web`, whichever took the workspace's instance lock first. The
// lock alone could only say "something has it"; the holder file beside it
// says what, so the second host to start can tell a person where the board
// already is instead of printing a bare lock error.

// The two kinds of board host.
const (
	HostTUI = "tui"
	HostWeb = "web"
)

// InstanceHolder is who holds a workspace's instance lock.
type InstanceHolder struct {
	// Host is HostTUI or HostWeb.
	Host     string `json:"host"`
	PID      int    `json:"pid"`
	Hostname string `json:"hostname"`
	// URL is where a web host serves the board; empty for the TUI.
	URL   string    `json:"url,omitempty"`
	Since time.Time `json:"since"`
}

// InstanceFile is the holder record written beside LockFile while the lock
// is held. It lives under the gitignored state/ dir like the lock itself.
func (w Workspace) InstanceFile() string { return filepath.Join(w.StateDir(), "instance.json") }

// InstanceHeldError is AcquireInstance's refusal: the lock is held, and
// Holder says by whom when the holder recorded itself (nil when it did not
// — an older gummi, or a holder caught between taking the lock and writing
// the file).
type InstanceHeldError struct {
	Holder *InstanceHolder
	// now is the clock the message's "since" is read against.
	now func() time.Time
}

// Error names the holder and how to reach it.
func (e *InstanceHeldError) Error() string {
	h := e.Holder
	if h == nil {
		return "this board is already open in another gummi process. Close it there, or stop it."
	}
	now := time.Now
	if e.now != nil {
		now = e.now
	}
	when := fmt.Sprintf("since %s, pid %d", sinceLabel(h.Since, now()), h.PID)
	switch {
	case h.Host == HostWeb && h.URL != "":
		return fmt.Sprintf("this board is served by gummi web at %s (%s). Open it there, or stop it.", h.URL, when)
	case h.Host == HostWeb:
		return fmt.Sprintf("this board is served by gummi web on %s (%s). Open it there, or stop it.", h.Hostname, when)
	default:
		return fmt.Sprintf("this board is open in the TUI on %s (%s). Use it there, or close it.", h.Hostname, when)
	}
}

// Unwrap makes the refusal an ErrLocked to errors.Is.
func (e *InstanceHeldError) Unwrap() error { return ErrLocked }

// sinceLabel is "09:12" for a start today and "Sep 26 09:12" for one
// before, in local time.
func sinceLabel(since, now time.Time) string {
	since, now = since.Local(), now.Local()
	y, m, d := since.Date()
	ny, nm, nd := now.Date()
	if y == ny && m == nm && d == nd {
		return since.Format("15:04")
	}
	return since.Format("Jan 2 15:04")
}

// AcquireInstance takes the workspace's instance lock for a board host and
// records h beside it (0600). On success the release func removes the
// record and then drops the lock. When the lock is held it returns an
// *InstanceHeldError naming the holder.
//
// PID, Hostname and Since are filled in when left zero.
func AcquireInstance(ws Workspace, h InstanceHolder) (func(), error) {
	release, err := AcquireLock(ws.LockFile())
	if errors.Is(err, ErrLocked) {
		return nil, &InstanceHeldError{Holder: readHolder(ws)}
	}
	if err != nil {
		return nil, err
	}
	if h.PID == 0 {
		h.PID = os.Getpid()
	}
	if h.Hostname == "" {
		h.Hostname, _ = os.Hostname()
	}
	if h.Since.IsZero() {
		h.Since = time.Now()
	}
	path := ws.InstanceFile()
	b, err := json.MarshalIndent(h, "", "  ")
	if err == nil {
		err = atomicfile.Write(path, append(b, '\n'), 0o600)
	}
	if err != nil {
		release()
		return nil, fmt.Errorf("recording the board's host in %s: %w", path, err)
	}
	return func() {
		_ = os.Remove(path)
		release()
	}, nil
}

// SetInstanceURL rewrites the URL in the holder record, for a web host
// that learns where it serves only after it took the lock — a tailnet
// node, which may wait minutes for a person to log it in. Only the
// lock's holder may call it.
func SetInstanceURL(ws Workspace, url string) error {
	h, err := ReadInstanceHolder(ws)
	if err != nil {
		return fmt.Errorf("reading the board's host record: %w", err)
	}
	h.URL = url
	b, err := json.MarshalIndent(h, "", "  ")
	if err != nil {
		return err
	}
	if err := atomicfile.Write(ws.InstanceFile(), append(b, '\n'), 0o600); err != nil {
		return fmt.Errorf("recording the board's address in %s: %w", ws.InstanceFile(), err)
	}
	return nil
}

// ReadInstanceHolder reads the holder record, for a caller that wants it
// without contending for the lock.
func ReadInstanceHolder(ws Workspace) (InstanceHolder, error) {
	b, err := os.ReadFile(ws.InstanceFile())
	if err != nil {
		return InstanceHolder{}, err
	}
	var h InstanceHolder
	if err := json.Unmarshal(b, &h); err != nil {
		return InstanceHolder{}, fmt.Errorf("parsing %s: %w", ws.InstanceFile(), err)
	}
	return h, nil
}

func readHolder(ws Workspace) *InstanceHolder {
	h, err := ReadInstanceHolder(ws)
	if err != nil {
		return nil
	}
	return &h
}
