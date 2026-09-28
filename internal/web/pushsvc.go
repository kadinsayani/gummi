package web

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/morphis/gummi/internal/domain"
	"github.com/morphis/gummi/internal/web/push"
)

// The files Web Push keeps beside the paired devices, in .gummi/state/web.
// PushFile is exported for `gummi web unpair`, which drops a device's
// subscription without a server.
const (
	vapidFile = "vapid.json"
	PushFile  = "push.json"
)

// Push is the board's Web Push: the server's VAPID key, the devices
// subscribed to it, and the notifier that delivers to them. It is the
// board's attention notifier (ui.Shell.AddAttentionNotifier) — a card
// that starts needing a person is a notification on every subscribed
// device — beside or instead of the terminal bell.
type Push struct {
	Store    *push.Store
	Sender   *push.Sender
	Notifier *push.Notifier
}

// OpenPush loads (or, the first time, creates) the key and the
// subscriptions under dir.
func OpenPush(dir string) (*Push, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("preparing %s: %w", dir, err)
	}
	v, err := push.LoadOrCreateVAPID(filepath.Join(dir, vapidFile))
	if err != nil {
		return nil, err
	}
	store, err := push.OpenStore(filepath.Join(dir, PushFile), nil)
	if err != nil {
		return nil, err
	}
	sender := push.NewSender(v, "")
	return &Push{Store: store, Sender: sender, Notifier: push.NewNotifier(store, sender)}, nil
}

// CheckEndpoint refuses a subscription whose endpoint is, or resolves to,
// an address the host must not POST to (push/addr.go). The sender checks
// again when it dials.
func (p *Push) CheckEndpoint(ctx context.Context, endpoint string) error {
	return push.CheckEndpoint(ctx, endpoint, p.Sender.AllowPrivate)
}

// Key is the VAPID public key a browser subscribes with.
func (p *Push) Key() string { return p.Sender.VAPID().PublicKey() }

// NeedsYou posts "<ID> needs you" to every subscribed device, the
// question as its body; tapping it opens the card. It never blocks: the
// board calls it from its own loop.
func (p *Push) NeedsYou(id domain.FeatureID, text string) {
	p.Notifier.Post(push.Message{
		Title: string(id) + " needs you",
		Body:  text,
		URL:   "/#" + string(id),
		Tag:   string(id),
	})
}
