package push

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/morphis/gummi/internal/atomicfile"
)

// Subscription is one browser's push subscription, held for the paired
// device that made it.
type Subscription struct {
	Endpoint  string    `json:"endpoint"`
	Keys      Keys      `json:"keys"`
	Device    string    `json:"device"`
	Person    string    `json:"person,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// Validate checks what a browser handed us before the host ever POSTs to
// it: an https endpoint (the host sends to whatever URL a paired device
// names, so nothing else is accepted), keys that decode to a P-256 point
// and a 16-octet secret, and a device to key it by.
func (s Subscription) Validate() error {
	if s.Device == "" {
		return errors.New("push: subscription has no device")
	}
	u, err := url.Parse(s.Endpoint)
	if err != nil {
		return fmt.Errorf("push: endpoint: %w", err)
	}
	if u.Scheme != "https" || u.Host == "" || u.User != nil {
		return fmt.Errorf("push: endpoint must be an https URL, got %q", s.Endpoint)
	}
	_, _, err = s.Keys.decode()
	return err
}

type storeFile struct {
	Version       int            `json:"version"`
	Subscriptions []Subscription `json:"subscriptions"`
}

const storeVersion = 1

// Store holds the subscriptions, one per paired device, in a JSON file
// (0600, rewritten atomically). Like the paired-device store, the file is
// the source of truth and is re-read whenever it changes on disk, so
// unpairing a device from another terminal can drop its subscription with
// a file edit.
type Store struct {
	path string
	now  func() time.Time

	mu    sync.Mutex
	subs  []Subscription
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

// OpenStore loads the store at path. Neither the file nor its directory is
// created until the first subscription is added.
func OpenStore(path string, now func() time.Time) (*Store, error) {
	if now == nil {
		now = time.Now
	}
	s := &Store{path: path, now: now}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadLocked(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) loadLocked() error {
	b, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		s.subs, s.stamp = nil, fileStamp{}
		return nil
	}
	if err != nil {
		return fmt.Errorf("reading %s: %w", s.path, err)
	}
	var f storeFile
	if err := json.Unmarshal(b, &f); err != nil {
		return fmt.Errorf("parsing %s: %w", s.path, err)
	}
	if f.Version != storeVersion {
		return fmt.Errorf("%s has format version %d, which this gummi does not understand", s.path, f.Version)
	}
	s.subs = f.Subscriptions
	s.stamp, _ = statStamp(s.path)
	return nil
}

// refreshLocked re-reads the file when it changed underneath us. A file
// somebody deleted means no subscriptions; one that no longer parses keeps
// what we had rather than dropping every device's notifications.
func (s *Store) refreshLocked() {
	st, err := statStamp(s.path)
	if errors.Is(err, os.ErrNotExist) {
		s.subs, s.stamp = nil, fileStamp{}
		return
	}
	if err != nil || st == s.stamp {
		return
	}
	_ = s.loadLocked()
}

func (s *Store) saveLocked() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("preparing %s: %w", filepath.Dir(s.path), err)
	}
	subs := s.subs
	if subs == nil {
		subs = []Subscription{}
	}
	b, err := json.MarshalIndent(storeFile{Version: storeVersion, Subscriptions: subs}, "", "  ")
	if err != nil {
		return err
	}
	if err := atomicfile.Write(s.path, append(b, '\n'), 0o600); err != nil {
		return fmt.Errorf("writing %s: %w", s.path, err)
	}
	s.stamp, _ = statStamp(s.path)
	return nil
}

// Add validates sub and records it as its device's subscription,
// replacing any earlier one for that device and any other device's entry
// for the same endpoint (one browser, re-paired, is one subscription).
// CreatedAt is stamped when zero.
func (s *Store) Add(sub Subscription) error {
	if err := sub.Validate(); err != nil {
		return err
	}
	if sub.CreatedAt.IsZero() {
		sub.CreatedAt = s.now().UTC()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refreshLocked()
	s.subs = slices.DeleteFunc(s.subs, func(x Subscription) bool {
		return x.Device == sub.Device || x.Endpoint == sub.Endpoint
	})
	s.subs = append(s.subs, sub)
	return s.saveLocked()
}

// Remove drops device's subscription. Removing one that is not there is
// not an error.
func (s *Store) Remove(device string) error {
	return s.removeWhere(func(x Subscription) bool { return x.Device == device })
}

// RemoveDevices drops every listed device's subscription — what
// unpairing them calls for, so a revoked phone stops hearing about the
// board.
func (s *Store) RemoveDevices(devices ...string) error {
	if len(devices) == 0 {
		return nil
	}
	return s.removeWhere(func(x Subscription) bool { return slices.Contains(devices, x.Device) })
}

// RemoveEndpoint drops every subscription for endpoint — what a push
// service's 404/410 calls for.
func (s *Store) RemoveEndpoint(endpoint string) error {
	return s.removeWhere(func(x Subscription) bool { return x.Endpoint == endpoint })
}

func (s *Store) removeWhere(match func(Subscription) bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refreshLocked()
	n := len(s.subs)
	s.subs = slices.DeleteFunc(s.subs, match)
	if len(s.subs) == n {
		return nil
	}
	return s.saveLocked()
}

// Get returns device's subscription, if it has one.
func (s *Store) Get(device string) (Subscription, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refreshLocked()
	for _, x := range s.subs {
		if x.Device == device {
			return x, true
		}
	}
	return Subscription{}, false
}

// List returns a copy of every subscription, oldest first.
func (s *Store) List() []Subscription {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refreshLocked()
	return slices.Clone(s.subs)
}
