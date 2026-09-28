package push

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func testSub(t *testing.T, device, endpoint string) Subscription {
	t.Helper()
	_, keys := newTestKeys(t)
	return Subscription{Endpoint: endpoint, Keys: keys, Device: device, Person: "simon"}
}

func TestStoreKeepsOneSubscriptionPerDevice(t *testing.T) {
	path := filepath.Join(t.TempDir(), "web", "push.json")
	now := time.Date(2026, 9, 27, 9, 12, 0, 0, time.UTC)
	s, err := OpenStore(path, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	if len(s.List()) != 0 {
		t.Fatal("a fresh store is not empty")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("opening the store created its file")
	}

	if err := s.Add(testSub(t, "d1", "https://push.example/a")); err != nil {
		t.Fatal(err)
	}
	if err := s.Add(testSub(t, "d2", "https://push.example/b")); err != nil {
		t.Fatal(err)
	}
	// d1 resubscribes: its old endpoint goes.
	if err := s.Add(testSub(t, "d1", "https://push.example/c")); err != nil {
		t.Fatal(err)
	}
	// d3 is the browser that held /b, re-paired: /b moves to d3.
	if err := s.Add(testSub(t, "d3", "https://push.example/b")); err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, x := range s.List() {
		got[x.Device] = x.Endpoint
		if !x.CreatedAt.Equal(now) {
			t.Fatalf("CreatedAt = %v", x.CreatedAt)
		}
	}
	want := map[string]string{"d1": "https://push.example/c", "d3": "https://push.example/b"}
	if len(got) != len(want) || got["d1"] != want["d1"] || got["d3"] != want["d3"] {
		t.Fatalf("subscriptions = %v, want %v", got, want)
	}
	if sub, ok := s.Get("d3"); !ok || sub.Person != "simon" {
		t.Fatalf("Get(d3) = %v, %v", sub, ok)
	}

	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("push.json mode %v, want 0600", fi.Mode().Perm())
	}

	if err := s.Remove("d1"); err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveEndpoint("https://push.example/b"); err != nil {
		t.Fatal(err)
	}
	if err := s.Remove("nobody"); err != nil {
		t.Fatal(err)
	}
	if n := len(s.List()); n != 0 {
		t.Fatalf("%d subscriptions left", n)
	}

	reopened, err := OpenStore(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if n := len(reopened.List()); n != 0 {
		t.Fatalf("reopened store has %d subscriptions", n)
	}
}

func TestStoreRefusesWhatItWouldNotSendTo(t *testing.T) {
	s, err := OpenStore(filepath.Join(t.TempDir(), "push.json"), nil)
	if err != nil {
		t.Fatal(err)
	}
	for name, sub := range map[string]Subscription{
		"http endpoint":     testSub(t, "d", "http://push.example/a"),
		"relative endpoint": testSub(t, "d", "/a"),
		"userinfo":          testSub(t, "d", "https://u:p@push.example/a"),
		"no device":         testSub(t, "", "https://push.example/a"),
		"bad auth":          {Endpoint: "https://push.example/a", Device: "d", Keys: Keys{P256dh: testSub(t, "d", "x").Keys.P256dh, Auth: "AAAA"}},
	} {
		if err := s.Add(sub); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if len(s.List()) != 0 {
		t.Fatal("a refused subscription was stored")
	}
}

// TestStoreRereadsAnExternalWrite: another process (an unpair from a
// second terminal) edits the file; the running store sees it.
func TestStoreRereadsAnExternalWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "push.json")
	running, err := OpenStore(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := running.Add(testSub(t, "d1", "https://push.example/a")); err != nil {
		t.Fatal(err)
	}
	if err := running.Add(testSub(t, "d2", "https://push.example/b")); err != nil {
		t.Fatal(err)
	}

	other, err := OpenStore(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := other.Remove("d1"); err != nil {
		t.Fatal(err)
	}
	// Make the change visible even on a filesystem with coarse mtimes.
	later := time.Now().Add(time.Minute)
	if err := os.Chtimes(path, later, later); err != nil {
		t.Fatal(err)
	}
	subs := running.List()
	if len(subs) != 1 || subs[0].Device != "d2" {
		t.Fatalf("running store still sees %v", subs)
	}

	// A running Add builds on the external edit rather than resurrecting d1.
	if err := running.Add(testSub(t, "d3", "https://push.example/c")); err != nil {
		t.Fatal(err)
	}
	if n := len(other.List()); n != 2 {
		t.Fatalf("other store sees %d subscriptions, want 2", n)
	}

	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if n := len(running.List()); n != 0 {
		t.Fatalf("after the file was deleted the store still holds %d", n)
	}
}
