package push

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"slices"
	"sync"
	"time"
)

// Message is what a notification says. It travels as the JSON payload
// the service worker reads: {"title","body","url","tag"}.
type Message struct {
	Title string `json:"title"`
	Body  string `json:"body,omitempty"`
	// URL is what tapping the notification opens, e.g. "/#FD-012".
	URL string `json:"url,omitempty"`
	// Tag groups notifications about one thing (a card): the browser
	// replaces an older one with the same tag, the push service replaces
	// an undelivered one (see TopicFor), and the Notifier debounces it.
	Tag string `json:"tag,omitempty"`
}

// TopicFor maps a tag to a Topic header value: the push service allows
// only 32 base64url characters, and a tag is free text.
func TopicFor(tag string) string {
	if tag == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(tag))
	return b64.EncodeToString(sum[:24])
}

// DefaultDebounce is how long a tag stays quiet after it was sent.
const DefaultDebounce = 5 * time.Second

// DefaultOptions are what a Notifier sends with: kept half a day, so a
// phone that was off overnight still hears that a card is waiting.
var DefaultOptions = Options{TTL: 12 * time.Hour, Urgency: UrgencyNormal}

// Notifier fans a Message out to every stored subscription.
type Notifier struct {
	store  *Store
	sender *Sender

	// Options are the delivery headers; Topic is derived from the tag.
	Options Options
	// Debounce suppresses a second message with the same tag inside this
	// window, so one card stopping is one notification even when two
	// paths report it. Zero means DefaultDebounce; negative disables it.
	Debounce time.Duration
	// OnError, when set, hears each failed delivery other than a gone
	// subscription (which is removed, not reported).
	OnError func(Subscription, error)
	// Paired, when set, is asked whether a subscription's device is still
	// paired. One that is not is removed rather than sent to: unpairing
	// drops the subscription itself, and this catches one it missed (an
	// unpair by an older gummi, a write that failed).
	Paired func(device string) bool

	now func() time.Time

	mu   sync.Mutex
	sent map[string]time.Time
}

// NewNotifier builds a Notifier over store, delivering through sender.
func NewNotifier(store *Store, sender *Sender) *Notifier {
	return &Notifier{
		store:   store,
		sender:  sender,
		Options: DefaultOptions,
		now:     time.Now,
		sent:    map[string]time.Time{},
	}
}

// Result is what one Notify did.
type Result struct {
	// Debounced is true when the message was not sent at all because its
	// tag was sent within the debounce window.
	Debounced bool
	Sent      int
	// Gone are the subscriptions the push service no longer knows; they
	// have been removed from the store.
	Gone []Subscription
	// Failed are the other failures, by device.
	Failed map[string]error
}

// Notify sends msg to every subscription concurrently and waits for all of
// them. Subscriptions reported gone are removed from the store.
func (n *Notifier) Notify(ctx context.Context, msg Message) Result {
	if n.debounced(msg.Tag) {
		return Result{Debounced: true}
	}
	payload, err := json.Marshal(msg)
	if err != nil {
		return Result{Failed: map[string]error{"": err}}
	}
	opts := n.Options
	opts.Topic = TopicFor(msg.Tag)

	subs := n.store.List()
	if n.Paired != nil {
		var revoked []string
		subs = slices.DeleteFunc(subs, func(sub Subscription) bool {
			if n.Paired(sub.Device) {
				return false
			}
			revoked = append(revoked, sub.Device)
			return true
		})
		_ = n.store.RemoveDevices(revoked...)
	}
	errs := make([]error, len(subs))
	var wg sync.WaitGroup
	for i, sub := range subs {
		wg.Go(func() {
			errs[i] = n.sender.Send(ctx, sub, payload, opts)
		})
	}
	wg.Wait()

	var res Result
	for i, sub := range subs {
		switch err := errs[i]; {
		case err == nil:
			res.Sent++
		case errors.Is(err, ErrGone):
			res.Gone = append(res.Gone, sub)
			_ = n.store.RemoveEndpoint(sub.Endpoint)
		default:
			if res.Failed == nil {
				res.Failed = map[string]error{}
			}
			res.Failed[sub.Device] = err
			if n.OnError != nil {
				n.OnError(sub, err)
			}
		}
	}
	return res
}

// Post is Notify in the background, for a caller (the board's attention
// hook) that must not wait on the network.
func (n *Notifier) Post(msg Message) {
	go n.Notify(context.Background(), msg)
}

// debounced records tag as sent now unless it was sent within the window,
// in which case it reports true and records nothing.
func (n *Notifier) debounced(tag string) bool {
	window := n.Debounce
	if window == 0 {
		window = DefaultDebounce
	}
	if tag == "" || window < 0 {
		return false
	}
	now := n.now()
	n.mu.Lock()
	defer n.mu.Unlock()
	for t, at := range n.sent {
		if now.Sub(at) >= window {
			delete(n.sent, t)
		}
	}
	if _, ok := n.sent[tag]; ok {
		return true
	}
	n.sent[tag] = now
	return false
}
