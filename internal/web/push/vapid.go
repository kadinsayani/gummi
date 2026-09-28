// Package push delivers Web Push notifications from the gummi web host to
// the paired browsers that asked for them (DESIGN §20.4), with the
// standard library alone and no third-party relay.
//
// Four pieces, each its own file:
//
//   - vapid.go: the host's application server identity (RFC 8292). A
//     P-256 keypair persisted 0600 under the workspace, the public half
//     handed to PushManager.subscribe, the private half signing a short
//     ES256 JWT on every request.
//   - encrypt.go: message encryption (RFC 8291) in the aes128gcm content
//     coding (RFC 8188). The push service only ever sees ciphertext.
//   - store.go: the subscriptions, one per paired device, in a JSON file
//     re-read whenever it changes on disk.
//   - send.go / notifier.go: one POST to one push service, and the fan-out
//     of a notification to every subscription, dropping the ones the push
//     service reports gone.
//
// The package derives nothing about the workflow: what is worth a
// notification is the caller's decision.
package push

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/morphis/gummi/internal/atomicfile"
)

// DefaultSubject is the contact the JWT names when none is configured. RFC
// 8292 asks for a mailto: or https: URI a push service operator could use
// to reach whoever runs the application server.
const DefaultSubject = "mailto:gummi@localhost"

// maxTokenLifetime is RFC 8292 §2's ceiling on a VAPID JWT's exp.
const maxTokenLifetime = 24 * time.Hour

// tokenLifetime is what Token issues: well inside the ceiling, so a push
// service whose clock runs a little fast still accepts it.
const tokenLifetime = 12 * time.Hour

// VAPID is the application server keypair.
type VAPID struct {
	priv *ecdsa.PrivateKey
	pub  []byte // uncompressed SEC1 point, 65 octets
}

type vapidFile struct {
	Version    int       `json:"version"`
	PrivateKey string    `json:"private_key"` // base64url raw scalar
	PublicKey  string    `json:"public_key"`  // base64url uncompressed point
	CreatedAt  time.Time `json:"created_at"`
}

const vapidVersion = 1

// LoadOrCreateVAPID reads the keypair at path, generating and persisting a
// fresh one (0600, atomically, its directory 0700) when none exists yet.
// The key is the host's identity to every push service a browser
// subscribed through: replacing it silently invalidates every
// subscription, so a file that exists but cannot be read is an error, not
// a reason to mint a new one.
func LoadOrCreateVAPID(path string) (*VAPID, error) {
	b, err := os.ReadFile(path)
	if err == nil {
		return parseVAPID(path, b)
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	v, err := GenerateVAPID()
	if err != nil {
		return nil, err
	}
	raw, err := v.priv.Bytes()
	if err != nil {
		return nil, err
	}
	out, err := json.MarshalIndent(vapidFile{
		Version:    vapidVersion,
		PrivateKey: b64.EncodeToString(raw),
		PublicKey:  v.PublicKey(),
		CreatedAt:  time.Now().UTC(),
	}, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("preparing %s: %w", filepath.Dir(path), err)
	}
	if err := atomicfile.Write(path, append(out, '\n'), 0o600); err != nil {
		return nil, fmt.Errorf("writing %s: %w", path, err)
	}
	return v, nil
}

// GenerateVAPID makes a fresh keypair without persisting it.
func GenerateVAPID() (*VAPID, error) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generating a VAPID key: %w", err)
	}
	return newVAPID(priv)
}

func newVAPID(priv *ecdsa.PrivateKey) (*VAPID, error) {
	pub, err := priv.PublicKey.Bytes()
	if err != nil {
		return nil, err
	}
	return &VAPID{priv: priv, pub: pub}, nil
}

func parseVAPID(path string, b []byte) (*VAPID, error) {
	var f vapidFile
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	if f.Version != vapidVersion {
		return nil, fmt.Errorf("%s has format version %d, which this gummi does not understand", path, f.Version)
	}
	raw, err := decodeB64(f.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("%s: private key: %w", path, err)
	}
	priv, err := ecdsa.ParseRawPrivateKey(elliptic.P256(), raw)
	if err != nil {
		return nil, fmt.Errorf("%s: private key: %w", path, err)
	}
	v, err := newVAPID(priv)
	if err != nil {
		return nil, err
	}
	if f.PublicKey != "" && f.PublicKey != v.PublicKey() {
		return nil, fmt.Errorf("%s: public key does not match the private key", path)
	}
	return v, nil
}

// PublicKey is the application server key in the form
// PushManager.subscribe({applicationServerKey}) takes: the uncompressed
// P-256 point, base64url without padding.
func (v *VAPID) PublicKey() string { return b64.EncodeToString(v.pub) }

// Token signs an RFC 8292 JWT (ES256) for aud — the origin of a push
// endpoint — naming subject as the contact, expiring at exp. exp more than
// 24 hours past now is refused, as a push service would refuse it.
func (v *VAPID) Token(aud, subject string, now, exp time.Time) (string, error) {
	if exp.Sub(now) > maxTokenLifetime {
		return "", fmt.Errorf("vapid: exp %s is more than 24h away", exp.Sub(now))
	}
	if subject == "" {
		subject = DefaultSubject
	}
	header := `{"typ":"JWT","alg":"ES256"}`
	claims, err := json.Marshal(struct {
		Aud string `json:"aud"`
		Exp int64  `json:"exp"`
		Sub string `json:"sub"`
	}{aud, exp.Unix(), subject})
	if err != nil {
		return "", err
	}
	signing := b64.EncodeToString([]byte(header)) + "." + b64.EncodeToString(claims)
	digest := sha256.Sum256([]byte(signing))
	r, s, err := ecdsa.Sign(rand.Reader, v.priv, digest[:])
	if err != nil {
		return "", fmt.Errorf("vapid: signing: %w", err)
	}
	// JWS ES256 is the fixed-width r||s, not the ASN.1 form.
	sig := make([]byte, 64)
	r.FillBytes(sig[:32])
	s.FillBytes(sig[32:])
	return signing + "." + b64.EncodeToString(sig), nil
}

// Authorization is the header value for a request to endpoint:
// `vapid t=<jwt>, k=<public key>` (RFC 8292 §3).
func (v *VAPID) Authorization(endpoint, subject string, now time.Time) (string, error) {
	aud, err := Audience(endpoint)
	if err != nil {
		return "", err
	}
	t, err := v.Token(aud, subject, now, now.Add(tokenLifetime))
	if err != nil {
		return "", err
	}
	return "vapid t=" + t + ", k=" + v.PublicKey(), nil
}

// Audience is the origin of a push endpoint, the JWT's aud.
func Audience(endpoint string) (string, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return "", fmt.Errorf("push endpoint: %w", err)
	}
	if u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("push endpoint %q is not an absolute URL", endpoint)
	}
	return u.Scheme + "://" + u.Host, nil
}

var b64 = base64.RawURLEncoding

// decodeB64 accepts base64url with or without padding: browsers hand out
// subscription keys unpadded, but not every client strips it.
func decodeB64(s string) ([]byte, error) {
	return b64.DecodeString(strings.TrimRight(s, "="))
}
