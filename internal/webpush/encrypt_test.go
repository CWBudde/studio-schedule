package webpush

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"testing"
)

// The example of RFC 8291, section 5 and appendix A.
const (
	rfcPlaintext  = "When I grow up, I want to be a watermelon"
	rfcAuth       = "BTBZMqHH6r4Tts7J_aSIgg"
	rfcUAPrivate  = "q1dXpw3UpT5VOmu_cf_v6ih07Aems3njxI-JWgLcM94"
	rfcUAPublic   = "BCVxsr7N_eNgVRqvHtD0zTZsEc6-VV-JvLexhqUzORcxaOzi6-AYWXvTBHm4bjyPjs7Vd8pZGH6SRpkNtoIAiw4"
	rfcASPrivate  = "yfWPiYE-n46HLnH0KqZOF1fJJU3MYrct3AELtAQ-oRw"
	rfcASPublic   = "BP4z9KsN6nGRTbVYI_c7VJSPQTBtkgcy27mlmlMoZIIgDll6e3vCYLocInmYWAmS6TlzAC8wEqKK6PBru3jl7A8"
	rfcSalt       = "DGv6ra1nlYgDCS1FRnbzlw"
	rfcCEK        = "oIhVW04MRdy2XN9CiKLxTg"
	rfcNonce      = "4h_95klXJ5E_qnoN"
	rfcHeader     = "DGv6ra1nlYgDCS1FRnbzlwAAEABBBP4z9KsN6nGRTbVYI_c7VJSPQTBtkgcy27mlmlMoZIIgDll6e3vCYLocInmYWAmS6TlzAC8wEqKK6PBru3jl7A8"
	rfcCiphertext = "8pfeW0KbunFT06SuDKoJH9Ql87S1QUrdirN6GcG7sFz1y1sqLgVi1VhjVkHsUoEsbI_0LpXMuGvnzQ"
	rfcBody       = "DGv6ra1nlYgDCS1FRnbzlwAAEABBBP4z9KsN6nGRTbVYI_c7VJSPQTBtkgcy27mlmlMoZIIgDll6e3vCYLocInmYWAmS6TlzAC8wEqKK6PBru3jl7A_yl95bQpu6cVPTpK4Mqgkf1CXztLVBSt2Ks3oZwbuwXPXLWyouBWLVWGNWQexSgSxsj_Qulcy4a-fN"
)

func b64(t *testing.T, s string) []byte {
	t.Helper()

	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		t.Fatalf("decode %q: %v", s, err)
	}

	return b
}

func rfcSubscription(t *testing.T) Subscription {
	t.Helper()

	return Subscription{
		Endpoint: "https://push.example.net/push/JzLQ3raZJfFBR0aqvOMsLrt54w4rJUsV",
		P256DH:   [PublicKeyLen]byte(b64(t, rfcUAPublic)), Auth: [AuthLen]byte(b64(t, rfcAuth)),
	}
}

func rfcSender(t *testing.T) *ecdh.PrivateKey {
	t.Helper()

	k, err := ecdh.P256().NewPrivateKey(b64(t, rfcASPrivate))
	if err != nil {
		t.Fatal(err)
	}

	if got := base64.RawURLEncoding.EncodeToString(k.PublicKey().Bytes()); got != rfcASPublic {
		t.Fatalf("sender public key %s, want %s", got, rfcASPublic)
	}

	return k
}

func TestEncryptReproducesRFC8291Example(t *testing.T) {
	t.Parallel()

	sub := rfcSubscription(t)
	as := rfcSender(t)

	cek, nonce, err := deriveKeys(sub, as, b64(t, rfcSalt))
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(cek, b64(t, rfcCEK)) {
		t.Errorf("CEK %x, want %x", cek, b64(t, rfcCEK))
	}

	if !bytes.Equal(nonce, b64(t, rfcNonce)) {
		t.Errorf("nonce %x, want %x", nonce, b64(t, rfcNonce))
	}

	body, err := encrypt(sub, []byte(rfcPlaintext), as, b64(t, rfcSalt))
	if err != nil {
		t.Fatal(err)
	}

	if got := base64.RawURLEncoding.EncodeToString(body[:headerLen]); got != rfcHeader {
		t.Errorf("header\n got %s\nwant %s", got, rfcHeader)
	}

	if got := base64.RawURLEncoding.EncodeToString(body[headerLen:]); got != rfcCiphertext {
		t.Errorf("ciphertext\n got %s\nwant %s", got, rfcCiphertext)
	}

	if got := base64.RawURLEncoding.EncodeToString(body); got != rfcBody {
		t.Errorf("body\n got %s\nwant %s", got, rfcBody)
	}
}

func TestEncryptDecryptsWithTheBrowsersKey(t *testing.T) {
	t.Parallel()

	ua, err := ecdh.P256().NewPrivateKey(b64(t, rfcUAPrivate))
	if err != nil {
		t.Fatal(err)
	}

	sub := rfcSubscription(t)

	payload := []byte(`{"title":"A place is free"}`)

	body, err := Encrypt(sub, payload)
	if err != nil {
		t.Fatal(err)
	}

	if got := decrypt(t, ua, sub.Auth[:], body); !bytes.Equal(got, payload) {
		t.Errorf("decrypted %q, want %q", got, payload)
	}

	// A fresh key and salt every time.
	again, err := Encrypt(sub, payload)
	if err != nil {
		t.Fatal(err)
	}

	if bytes.Equal(body[:headerLen], again[:headerLen]) {
		t.Error("two messages share salt and key")
	}
}

func TestEncryptPayloadLimit(t *testing.T) {
	t.Parallel()

	sub := rfcSubscription(t)

	body, err := Encrypt(sub, bytes.Repeat([]byte("x"), MaxPayload))
	if err != nil {
		t.Fatal(err)
	}

	if len(body) != recordSize {
		t.Errorf("a full message is %d bytes, want the record size %d", len(body), recordSize)
	}

	_, err = Encrypt(sub, bytes.Repeat([]byte("x"), MaxPayload+1))
	if !errors.Is(err, ErrPayloadTooLarge) {
		t.Errorf("want ErrPayloadTooLarge, got %v", err)
	}
}

// decrypt is the browser's side of RFC 8291 for one record.
func decrypt(t *testing.T, ua *ecdh.PrivateKey, auth, body []byte) []byte {
	t.Helper()

	if len(body) < headerLen+tagLen {
		t.Fatalf("body of %d bytes", len(body))
	}

	salt := body[:saltLen]
	if rs := binary.BigEndian.Uint32(body[saltLen:]); rs != recordSize {
		t.Fatalf("record size %d", rs)
	}

	if idlen := body[saltLen+4]; idlen != PublicKeyLen {
		t.Fatalf("key id length %d", idlen)
	}

	asPublic := body[saltLen+5 : headerLen]

	as, err := ecdh.P256().NewPublicKey(asPublic)
	if err != nil {
		t.Fatal(err)
	}

	secret, err := ua.ECDH(as)
	if err != nil {
		t.Fatal(err)
	}

	keyInfo := append(append([]byte("WebPush: info\x00"), ua.PublicKey().Bytes()...), asPublic...)

	ikm, err := hkdf.Key(sha256.New, secret, auth, string(keyInfo), 32)
	if err != nil {
		t.Fatal(err)
	}

	prk, _ := hkdf.Extract(sha256.New, ikm, salt)
	cek, _ := hkdf.Expand(sha256.New, prk, "Content-Encoding: aes128gcm\x00", 16)
	nonce, _ := hkdf.Expand(sha256.New, prk, "Content-Encoding: nonce\x00", 12)

	block, err := aes.NewCipher(cek)
	if err != nil {
		t.Fatal(err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatal(err)
	}

	plain, err := gcm.Open(nil, nonce, body[headerLen:], nil)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	end := bytes.LastIndexByte(plain, 0x02)
	if end < 0 || bytes.ContainsFunc(plain[end+1:], func(r rune) bool { return r != 0 }) {
		t.Fatalf("no last-record delimiter in %x", plain)
	}

	return plain[:end]
}
