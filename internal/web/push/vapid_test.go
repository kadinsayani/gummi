package push

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"encoding/json"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestVAPIDIsCreatedOnceAndPersisted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "web", "vapid.json")
	v1, err := LoadOrCreateVAPID(path)
	if err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("vapid.json mode %v, want 0600", fi.Mode().Perm())
	}
	v2, err := LoadOrCreateVAPID(path)
	if err != nil {
		t.Fatal(err)
	}
	if v1.PublicKey() != v2.PublicKey() {
		t.Fatal("reloading minted a different key")
	}
	raw, err := decodeB64(v1.PublicKey())
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) != 65 || raw[0] != 0x04 {
		t.Fatalf("public key is %d octets starting %#x; want an uncompressed point", len(raw), raw[0])
	}
}

func TestVAPIDRefusesAFileItCannotRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vapid.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadOrCreateVAPID(path); err == nil {
		t.Fatal("a corrupt key file was replaced instead of refused")
	}
	b, _ := os.ReadFile(path)
	if string(b) != "{not json" {
		t.Fatal("the corrupt file was overwritten")
	}

	other, _ := GenerateVAPID()
	v, _ := GenerateVAPID()
	raw, _ := v.priv.Bytes()
	mismatched, _ := json.Marshal(vapidFile{Version: 1, PrivateKey: b64.EncodeToString(raw), PublicKey: other.PublicKey()})
	if err := os.WriteFile(path, mismatched, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadOrCreateVAPID(path); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("mismatched pair: err = %v", err)
	}
}

// verifyJWT checks an ES256 JWT against a base64url uncompressed public
// key and returns its claims.
func verifyJWT(t *testing.T, token, pubB64 string) map[string]any {
	t.Helper()
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("JWT has %d parts", len(parts))
	}
	var header map[string]any
	hb, _ := decodeB64(parts[0])
	if err := json.Unmarshal(hb, &header); err != nil {
		t.Fatal(err)
	}
	if header["alg"] != "ES256" || header["typ"] != "JWT" {
		t.Fatalf("header = %v", header)
	}
	raw, err := decodeB64(pubB64)
	if err != nil {
		t.Fatal(err)
	}
	pub, err := ecdsa.ParseUncompressedPublicKey(elliptic.P256(), raw)
	if err != nil {
		t.Fatal(err)
	}
	sig, err := decodeB64(parts[2])
	if err != nil || len(sig) != 64 {
		t.Fatalf("signature: %d octets, err %v", len(sig), err)
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	r, s := new(big.Int).SetBytes(sig[:32]), new(big.Int).SetBytes(sig[32:])
	if !ecdsa.Verify(pub, digest[:], r, s) {
		t.Fatal("JWT signature does not verify with the public key")
	}
	var claims map[string]any
	cb, _ := decodeB64(parts[1])
	if err := json.Unmarshal(cb, &claims); err != nil {
		t.Fatal(err)
	}
	return claims
}

func TestVAPIDTokenVerifiesWithThePublicKey(t *testing.T) {
	v, err := GenerateVAPID()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_800_000_000, 0)
	auth, err := v.Authorization("https://push.example.net:8443/wpush/v2/abc?x=1", "", now)
	if err != nil {
		t.Fatal(err)
	}
	tok, key, ok := strings.Cut(strings.TrimPrefix(auth, "vapid t="), ", k=")
	if !ok || !strings.HasPrefix(auth, "vapid t=") {
		t.Fatalf("Authorization = %q", auth)
	}
	if key != v.PublicKey() {
		t.Fatal("k= is not the public key")
	}
	claims := verifyJWT(t, tok, key)
	if claims["aud"] != "https://push.example.net:8443" {
		t.Fatalf("aud = %v", claims["aud"])
	}
	if claims["sub"] != DefaultSubject {
		t.Fatalf("sub = %v", claims["sub"])
	}
	exp := int64(claims["exp"].(float64))
	if d := time.Unix(exp, 0).Sub(now); d <= 0 || d > 24*time.Hour {
		t.Fatalf("exp is %v from now", d)
	}

	if _, err := v.Token("https://x", "", now, now.Add(25*time.Hour)); err == nil {
		t.Fatal("a 25h token was issued")
	}
	tok, _ = v.Token("https://x", "https://gummi.example/contact", now, now.Add(time.Hour))
	if c := verifyJWT(t, tok, v.PublicKey()); c["sub"] != "https://gummi.example/contact" {
		t.Fatalf("sub = %v", c["sub"])
	}
}
