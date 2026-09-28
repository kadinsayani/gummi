package state

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

// The holder record exists exactly while the lock is held, mode 0600, and
// a second host is refused with a message naming the first.
func TestAcquireInstanceRecordsTheHolder(t *testing.T) {
	ws := Workspace{Root: t.TempDir()}
	release, err := AcquireInstance(ws, InstanceHolder{Host: HostWeb, URL: "http://127.0.0.1:7878"})
	if err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(ws.InstanceFile())
	if err != nil {
		t.Fatalf("holder record missing while held: %v", err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("holder record mode = %v, want 0600", fi.Mode().Perm())
	}
	h, err := ReadInstanceHolder(ws)
	if err != nil {
		t.Fatal(err)
	}
	if h.Host != HostWeb || h.PID != os.Getpid() || h.URL != "http://127.0.0.1:7878" || h.Since.IsZero() || h.Hostname == "" {
		t.Errorf("holder = %+v, want web/this pid/url/since/hostname filled", h)
	}

	_, err = AcquireInstance(ws, InstanceHolder{Host: HostTUI})
	var held *InstanceHeldError
	if !errors.As(err, &held) || held.Holder == nil || held.Holder.Host != HostWeb {
		t.Fatalf("second acquire err = %v, want an InstanceHeldError naming the web host", err)
	}
	if !errors.Is(err, ErrLocked) {
		t.Error("InstanceHeldError should be an ErrLocked")
	}

	release()
	if _, err := os.Stat(ws.InstanceFile()); !os.IsNotExist(err) {
		t.Errorf("holder record survives release: %v", err)
	}
	release2, err := AcquireInstance(ws, InstanceHolder{Host: HostTUI})
	if err != nil {
		t.Fatalf("acquire after release: %v", err)
	}
	release2()
}

// A tailnet URL is known only after the node logs in, well after the lock
// was taken; the second host's refusal names it once it is recorded.
func TestSetInstanceURLNamesTheLaterAddress(t *testing.T) {
	ws := Workspace{Root: t.TempDir()}
	release, err := AcquireInstance(ws, InstanceHolder{Host: HostWeb, URL: "http://127.0.0.1:7878"})
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	before, err := ReadInstanceHolder(ws)
	if err != nil {
		t.Fatal(err)
	}
	if err := SetInstanceURL(ws, "https://gummi.tail1234.ts.net"); err != nil {
		t.Fatal(err)
	}
	after, err := ReadInstanceHolder(ws)
	if err != nil {
		t.Fatal(err)
	}
	if after.URL != "https://gummi.tail1234.ts.net" || after.PID != before.PID || !after.Since.Equal(before.Since) || after.Host != HostWeb {
		t.Errorf("holder after SetInstanceURL = %+v, want only the URL changed from %+v", after, before)
	}
	if fi, err := os.Stat(ws.InstanceFile()); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("holder record after rewrite: %v %v, want mode 0600", fi, err)
	}
	_, err = AcquireInstance(ws, InstanceHolder{Host: HostTUI})
	if err == nil || !strings.Contains(err.Error(), "https://gummi.tail1234.ts.net") {
		t.Errorf("second acquire err = %v, want it to name the tailnet URL", err)
	}
}

func TestInstanceHeldMessage(t *testing.T) {
	now := time.Date(2026, 9, 27, 15, 0, 0, 0, time.Local)
	since := time.Date(2026, 9, 27, 9, 12, 0, 0, time.Local)
	for _, tc := range []struct {
		name string
		h    *InstanceHolder
		want string
	}{
		{
			"web", &InstanceHolder{Host: HostWeb, PID: 4411, Hostname: "box", URL: "http://127.0.0.1:7878", Since: since},
			"this board is served by gummi web at http://127.0.0.1:7878 (since 09:12, pid 4411). Open it there, or stop it.",
		},
		{
			"tui", &InstanceHolder{Host: HostTUI, PID: 4411, Hostname: "box", Since: since},
			"this board is open in the TUI on box (since 09:12, pid 4411). Use it there, or close it.",
		},
		{
			"yesterday", &InstanceHolder{Host: HostTUI, PID: 7, Hostname: "box", Since: since.AddDate(0, 0, -1)},
			"this board is open in the TUI on box (since Sep 26 09:12, pid 7). Use it there, or close it.",
		},
		{
			"unrecorded", nil,
			"this board is already open in another gummi process. Close it there, or stop it.",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := &InstanceHeldError{Holder: tc.h, now: func() time.Time { return now }}
			if got := err.Error(); got != tc.want {
				t.Errorf("got  %q\nwant %q", got, tc.want)
			}
		})
	}
}
