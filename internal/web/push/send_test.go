package push

import (
	"context"
	"crypto/ecdh"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// pushService is a stand-in push service: every endpoint it hands out
// belongs to one browser whose private key it holds, so it can decrypt
// what the host sends the way the browser would.
type pushService struct {
	t   *testing.T
	srv *httptest.Server

	mu       sync.Mutex
	browsers map[string]browser // by path
	status   map[string]int     // by path; 0 means 201
	got      []received
}

type browser struct {
	priv *ecdh.PrivateKey
	auth []byte
}

type received struct {
	path    string
	header  http.Header
	payload []byte
}

func newPushService(t *testing.T) *pushService {
	p := &pushService{t: t, browsers: map[string]browser{}, status: map[string]int{}}
	p.srv = httptest.NewTLSServer(http.HandlerFunc(p.serve))
	t.Cleanup(p.srv.Close)
	return p
}

// subscribe mints a browser at path and the subscription it would post.
func (p *pushService) subscribe(t *testing.T, device, path string, status int) Subscription {
	t.Helper()
	priv, keys := newTestKeys(t)
	p.mu.Lock()
	p.browsers[path] = browser{priv: priv, auth: mustAuth(t, keys)}
	p.status[path] = status
	p.mu.Unlock()
	return Subscription{Endpoint: p.srv.URL + path, Keys: keys, Device: device, Person: "simon"}
}

func (p *pushService) serve(w http.ResponseWriter, r *http.Request) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if st := p.status[r.URL.Path]; st != 0 {
		http.Error(w, "nope", st)
		return
	}
	b, ok := p.browsers[r.URL.Path]
	body, _ := io.ReadAll(r.Body)
	if !ok || r.Method != http.MethodPost {
		http.Error(w, "unknown", http.StatusBadRequest)
		return
	}
	pt, err := decryptForTest(b.priv, b.auth, body)
	if err != nil {
		p.t.Errorf("%s: decrypting: %v", r.URL.Path, err)
		http.Error(w, "undecryptable", http.StatusBadRequest)
		return
	}
	p.got = append(p.got, received{path: r.URL.Path, header: r.Header.Clone(), payload: pt})
	w.WriteHeader(http.StatusCreated)
}

func (p *pushService) received() []received {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]received(nil), p.got...)
}

func newTestSender(t *testing.T, p *pushService) *Sender {
	t.Helper()
	v, err := GenerateVAPID()
	if err != nil {
		t.Fatal(err)
	}
	s := NewSender(v, "")
	s.Client = p.srv.Client()
	return s
}

func TestSendDeliversAnEncryptedSignedMessage(t *testing.T) {
	p := newPushService(t)
	s := newTestSender(t, p)
	sub := p.subscribe(t, "d1", "/wpush/d1", 0)

	err := s.Send(context.Background(), sub, []byte("hello"), Options{
		TTL: 90 * time.Minute, Urgency: UrgencyHigh, Topic: TopicFor("FD-012"),
	})
	if err != nil {
		t.Fatal(err)
	}
	got := p.received()
	if len(got) != 1 {
		t.Fatalf("service received %d messages", len(got))
	}
	r := got[0]
	if string(r.payload) != "hello" {
		t.Fatalf("payload = %q", r.payload)
	}
	for k, want := range map[string]string{
		"Content-Encoding": "aes128gcm",
		"Content-Type":     "application/octet-stream",
		"Ttl":              "5400",
		"Urgency":          "high",
		"Topic":            TopicFor("FD-012"),
	} {
		if v := r.header.Get(k); v != want {
			t.Errorf("%s = %q, want %q", k, v, want)
		}
	}
	auth := r.header.Get("Authorization")
	tok, key, ok := strings.Cut(strings.TrimPrefix(auth, "vapid t="), ", k=")
	if !ok || key != s.VAPID().PublicKey() {
		t.Fatalf("Authorization = %q", auth)
	}
	if c := verifyJWT(t, tok, key); c["aud"] != p.srv.URL {
		t.Fatalf("aud = %v, want %s", c["aud"], p.srv.URL)
	}
}

func TestSendReportsGoneAndOtherFailures(t *testing.T) {
	p := newPushService(t)
	s := newTestSender(t, p)
	for _, st := range []int{http.StatusGone, http.StatusNotFound} {
		err := s.Send(context.Background(), p.subscribe(t, "d", "/gone", st), []byte("x"), Options{})
		if !errors.Is(err, ErrGone) {
			t.Fatalf("%d: err = %v, want ErrGone", st, err)
		}
	}
	err := s.Send(context.Background(), p.subscribe(t, "d", "/boom", http.StatusTooManyRequests), []byte("x"), Options{})
	var se *StatusError
	if !errors.As(err, &se) || se.Code != http.StatusTooManyRequests || errors.Is(err, ErrGone) {
		t.Fatalf("err = %v, want a 429 StatusError", err)
	}
	if err := s.Send(context.Background(), p.subscribe(t, "d", "/t", 0), []byte("x"), Options{Topic: "not a topic!"}); err == nil {
		t.Fatal("an invalid topic was sent")
	}
}

func TestSendIsBoundedByItsTimeout(t *testing.T) {
	stall := make(chan struct{})
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-stall
	}))
	defer srv.Close()
	defer close(stall)
	v, _ := GenerateVAPID()
	s := NewSender(v, "")
	s.Client = srv.Client()
	s.Timeout = 50 * time.Millisecond
	_, keys := newTestKeys(t)
	start := time.Now()
	err := s.Send(context.Background(), Subscription{Endpoint: srv.URL + "/x", Keys: keys, Device: "d"}, []byte("x"), Options{})
	if err == nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want a deadline", err)
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Fatalf("Send took %v", d)
	}
}
