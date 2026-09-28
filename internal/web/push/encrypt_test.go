package push

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// unb64 decodes an RFC's base64url, which wraps with whitespace.
func unb64(t *testing.T, s string) []byte {
	t.Helper()
	b, err := decodeB64(strings.Join(strings.Fields(s), ""))
	if err != nil {
		t.Fatalf("decoding %q: %v", s, err)
	}
	return b
}

// TestEncryptMatchesRFC8291AppendixA reproduces the RFC's worked example
// byte for byte: same keys, same salt, same message.
func TestEncryptMatchesRFC8291AppendixA(t *testing.T) {
	plaintext := unb64(t, "V2hlbiBJIGdyb3cgdXAsIEkgd2FudCB0byBiZSBhIHdhdGVybWVsb24")
	if string(plaintext) != "When I grow up, I want to be a watermelon" {
		t.Fatalf("plaintext decoded to %q", plaintext)
	}
	asPrivate := unb64(t, "yfWPiYE-n46HLnH0KqZOF1fJJU3MYrct3AELtAQ-oRw")
	asPublic := unb64(t, `BP4z9KsN6nGRTbVYI_c7VJSPQTBtkgcy27mlmlMoZIIg
		Dll6e3vCYLocInmYWAmS6TlzAC8wEqKK6PBru3jl7A8`)
	keys := Keys{
		P256dh: `BCVxsr7N_eNgVRqvHtD0zTZsEc6-VV-JvLexhqUzORcxaOzi6-AYWXvTBHm4bjyPjs7Vd8pZGH6SRpkNtoIAiw4`,
		Auth:   "BTBZMqHH6r4Tts7J_aSIgg",
	}
	salt := unb64(t, "DGv6ra1nlYgDCS1FRnbzlw")

	as, err := ecdh.P256().NewPrivateKey(asPrivate)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(as.PublicKey().Bytes(), asPublic) {
		t.Fatal("as_private does not yield the RFC's as_public")
	}

	got, err := encrypt(keys, plaintext, as, salt)
	if err != nil {
		t.Fatal(err)
	}
	header := unb64(t, `DGv6ra1nlYgDCS1FRnbzlwAAEABBBP4z 9KsN6nGRTbVYI_c7VJSPQTBtkgcy27ml
		mlMoZIIgDll6e3vCYLocInmYWAmS6Tlz AC8wEqKK6PBru3jl7A8`)
	ciphertext := unb64(t, `8pfeW0KbunFT06SuDKoJH9Ql87S1QUrd irN6GcG7sFz1y1sqLgVi1VhjVkHsUoEs
		bI_0LpXMuGvnzQ`)
	if len(header) != 86 {
		t.Fatalf("RFC header decoded to %d octets, want 86", len(header))
	}
	want := append(append([]byte{}, header...), ciphertext...)
	// Section 5's message is the same bytes in one line.
	section5 := unb64(t, `DGv6ra1nlYgDCS1FRnbzlwAAEABBBP4z9KsN6nGRTbVYI_c7VJSPQTBtkgcy27ml
		mlMoZIIgDll6e3vCYLocInmYWAmS6TlzAC8wEqKK6PBru3jl7A_yl95bQpu6cVPT
		pK4Mqgkf1CXztLVBSt2Ks3oZwbuwXPXLWyouBWLVWGNWQexSgSxsj_Qulcy4a-fN`)
	if !bytes.Equal(want, section5) {
		t.Fatal("Appendix A header+ciphertext differ from Section 5's message; the vector is mistyped")
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("encrypt mismatch\n got %s\nwant %s", b64.EncodeToString(got), b64.EncodeToString(want))
	}

	// And the receiving side, with only ua_private, reads it back.
	ua, err := ecdh.P256().NewPrivateKey(unb64(t, "q1dXpw3UpT5VOmu_cf_v6ih07Aems3njxI-JWgLcM94"))
	if err != nil {
		t.Fatal(err)
	}
	pt, err := decryptForTest(ua, unb64(t, keys.Auth), got)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(pt, plaintext) {
		t.Fatalf("decrypted %q", pt)
	}
}

func TestEncryptRoundTripsWithFreshKeys(t *testing.T) {
	ua, keys := newTestKeys(t)
	msg := []byte(`{"title":"FD-012 needs you"}`)
	a, err := Encrypt(keys, msg)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Encrypt(keys, msg)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(a, b) {
		t.Fatal("two encryptions of one message are identical: the key or salt is not fresh")
	}
	pt, err := decryptForTest(ua, mustAuth(t, keys), a)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(pt, msg) {
		t.Fatalf("decrypted %q", pt)
	}
}

func TestEncryptRefusesOversizeAndBadKeys(t *testing.T) {
	_, keys := newTestKeys(t)
	if _, err := Encrypt(keys, make([]byte, MaxPayload)); err != nil {
		t.Fatalf("a MaxPayload message was refused: %v", err)
	}
	if _, err := Encrypt(keys, make([]byte, MaxPayload+1)); !errors.Is(err, ErrPayloadTooLarge) {
		t.Fatalf("oversize: err = %v", err)
	}
	bad := keys
	bad.Auth = b64.EncodeToString([]byte("short"))
	if _, err := Encrypt(bad, []byte("x")); err == nil {
		t.Fatal("a 5-octet auth secret was accepted")
	}
	bad = keys
	bad.P256dh = b64.EncodeToString(make([]byte, 65))
	if _, err := Encrypt(bad, []byte("x")); err == nil {
		t.Fatal("a p256dh that is not a curve point was accepted")
	}
}

// newTestKeys makes a browser's side of a subscription: its private key,
// and the Keys it would hand the host.
func newTestKeys(t *testing.T) (*ecdh.PrivateKey, Keys) {
	t.Helper()
	ua, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	auth := make([]byte, 16)
	_, _ = rand.Read(auth)
	return ua, Keys{P256dh: b64.EncodeToString(ua.PublicKey().Bytes()), Auth: b64.EncodeToString(auth)}
}

func mustAuth(t *testing.T, k Keys) []byte {
	t.Helper()
	b, err := decodeB64(k.Auth)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// decryptForTest is the user agent's half of RFC 8291, written apart from
// encrypt so a shared mistake cannot pass for agreement.
func decryptForTest(ua *ecdh.PrivateKey, auth, body []byte) ([]byte, error) {
	if len(body) < 21 {
		return nil, errors.New("body shorter than a header")
	}
	salt := body[:16]
	rs := binary.BigEndian.Uint32(body[16:20])
	idlen := int(body[20])
	if len(body) < 21+idlen {
		return nil, errors.New("truncated key id")
	}
	asPublic := body[21 : 21+idlen]
	record := body[21+idlen:]
	if rs != 4096 {
		return nil, fmt.Errorf("record size %d", rs)
	}
	if len(record) > int(rs) {
		return nil, fmt.Errorf("record of %d octets exceeds rs", len(record))
	}
	as, err := ecdh.P256().NewPublicKey(asPublic)
	if err != nil {
		return nil, err
	}
	secret, err := ua.ECDH(as)
	if err != nil {
		return nil, err
	}
	info := "WebPush: info\x00" + string(ua.PublicKey().Bytes()) + string(asPublic)
	ikm, err := hkdf.Key(sha256.New, secret, auth, info, 32)
	if err != nil {
		return nil, err
	}
	cek, err := hkdf.Key(sha256.New, ikm, salt, "Content-Encoding: aes128gcm\x00", 16)
	if err != nil {
		return nil, err
	}
	nonce, err := hkdf.Key(sha256.New, ikm, salt, "Content-Encoding: nonce\x00", 12)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(cek)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	pt, err := gcm.Open(nil, nonce, record, nil)
	if err != nil {
		return nil, err
	}
	i := bytes.LastIndexFunc(pt, func(r rune) bool { return r != 0 })
	if i < 0 || pt[i] != 0x02 {
		return nil, errors.New("missing last-record delimiter")
	}
	return pt[:i], nil
}
