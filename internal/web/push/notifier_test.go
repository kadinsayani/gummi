package push

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestNotifierFansOutAndForgetsGoneSubscriptions(t *testing.T) {
	p := newPushService(t)
	store, err := OpenStore(filepath.Join(t.TempDir(), "push.json"), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, sub := range []Subscription{
		p.subscribe(t, "phone", "/phone", 0),
		p.subscribe(t, "laptop", "/laptop", 0),
		p.subscribe(t, "old", "/old", http.StatusGone),
		p.subscribe(t, "flaky", "/flaky", http.StatusServiceUnavailable),
	} {
		if err := store.Add(sub); err != nil {
			t.Fatal(err)
		}
	}
	n := NewNotifier(store, newTestSender(t, p))
	var reported []string
	var mu sync.Mutex
	n.OnError = func(s Subscription, _ error) { mu.Lock(); reported = append(reported, s.Device); mu.Unlock() }

	msg := Message{Title: "FD-012 needs you", Body: "Approve the design?", URL: "/#FD-012", Tag: "FD-012"}
	res := n.Notify(context.Background(), msg)
	if res.Sent != 2 || len(res.Gone) != 1 || res.Gone[0].Device != "old" || len(res.Failed) != 1 || res.Failed["flaky"] == nil {
		t.Fatalf("result = %+v", res)
	}
	if len(reported) != 1 || reported[0] != "flaky" {
		t.Fatalf("OnError heard %v", reported)
	}
	if _, ok := store.Get("old"); ok {
		t.Fatal("the gone subscription is still stored")
	}
	if _, ok := store.Get("flaky"); !ok {
		t.Fatal("a transient failure removed the subscription")
	}

	for _, r := range p.received() {
		var got Message
		if err := json.Unmarshal(r.payload, &got); err != nil {
			t.Fatal(err)
		}
		if got != msg {
			t.Fatalf("%s got %+v", r.path, got)
		}
		if r.header.Get("Topic") != TopicFor("FD-012") || r.header.Get("Ttl") != "43200" {
			t.Fatalf("%s headers %v", r.path, r.header)
		}
	}
}

func TestNotifierDebouncesATag(t *testing.T) {
	p := newPushService(t)
	store, _ := OpenStore(filepath.Join(t.TempDir(), "push.json"), nil)
	if err := store.Add(p.subscribe(t, "phone", "/phone", 0)); err != nil {
		t.Fatal(err)
	}
	n := NewNotifier(store, newTestSender(t, p))
	clock := time.Date(2026, 9, 27, 9, 0, 0, 0, time.UTC)
	n.now = func() time.Time { return clock }

	send := func(tag string) Result {
		return n.Notify(context.Background(), Message{Title: "x", Tag: tag})
	}
	if r := send("FD-1"); r.Debounced || r.Sent != 1 {
		t.Fatalf("first: %+v", r)
	}
	clock = clock.Add(2 * time.Second)
	if r := send("FD-1"); !r.Debounced {
		t.Fatalf("a repeat 2s later was sent: %+v", r)
	}
	if r := send("FD-2"); r.Debounced {
		t.Fatal("another card's notification was debounced")
	}
	if r := send(""); r.Debounced {
		t.Fatal("an untagged notification was debounced")
	}
	clock = clock.Add(DefaultDebounce)
	if r := send("FD-1"); r.Debounced || r.Sent != 1 {
		t.Fatalf("after the window: %+v", r)
	}
	if got := len(p.received()); got != 4 {
		t.Fatalf("service received %d, want 4", got)
	}
}

func TestTopicForFitsTheHeader(t *testing.T) {
	for _, tag := range []string{"FD-012", strings.Repeat("ü", 100)} {
		tp := TopicFor(tag)
		if len(tp) != 32 || validTopic(tp) != nil {
			t.Fatalf("TopicFor(%q) = %q", tag, tp)
		}
	}
	if TopicFor("") != "" {
		t.Fatal("an empty tag produced a topic")
	}
}
