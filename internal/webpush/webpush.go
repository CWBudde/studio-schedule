// Package webpush sends Web Push messages (ADR 0017) with the standard
// library only: the payload is encrypted for the browser per RFC 8291
// (aes128gcm, RFC 8188), and the request carries a VAPID token per RFC 8292
// (an ES256 JWT signed with the application server's key).
//
// The package is server only: the web demo shows its notifications through
// a sender of its own (internal/wasmbridge), and scripts/check-wasm-deps.sh
// keeps this package out of the WebAssembly build.
package webpush

import (
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
)

// Lengths of the subscription keys.
const (
	// PublicKeyLen is the length of an uncompressed P-256 point: 0x04, x, y.
	PublicKeyLen = 65
	// AuthLen is the length of a subscription's authentication secret.
	AuthLen = 16
)

// Subscription is a browser's push subscription (PushSubscription.toJSON()
// in the browser): where to send, and the keys to encrypt for.
type Subscription struct {
	// Endpoint is the push service's URL of the subscription.
	Endpoint string
	// P256DH is the browser's ECDH public key, an uncompressed P-256 point.
	P256DH [PublicKeyLen]byte
	// Auth is the browser's authentication secret.
	Auth [AuthLen]byte
}

// ErrInvalidKey is wrapped by the errors of keys that do not parse.
var ErrInvalidKey = errors.New("webpush: invalid key")

// Keys is the application server's VAPID key pair (RFC 8292).
type Keys struct {
	private *ecdsa.PrivateKey
	public  []byte // uncompressed point
}

// GenerateKeys returns a new random VAPID key pair.
func GenerateKeys() (Keys, error) {
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return Keys{}, fmt.Errorf("webpush: generate key: %w", err)
	}

	return keysOf(k)
}

// ParseKeys parses a VAPID key pair in the form web push libraries share:
// the public key as the base64url of its uncompressed point (65 bytes, the
// applicationServerKey browsers subscribe with), the private key as the
// base64url of its 32-byte scalar. Padding is optional. It fails unless the
// public key belongs to the private one.
func ParseKeys(public, private string) (Keys, error) {
	d, err := decodeBase64URL(private)
	if err != nil {
		return Keys{}, fmt.Errorf("%w: private key is not base64url", ErrInvalidKey)
	}

	k, err := ecdsa.ParseRawPrivateKey(elliptic.P256(), d)
	if err != nil {
		return Keys{}, fmt.Errorf("%w: private key is not a P-256 scalar", ErrInvalidKey)
	}

	keys, err := keysOf(k)
	if err != nil {
		return Keys{}, err
	}

	pub, err := decodeBase64URL(public)
	if err != nil {
		return Keys{}, fmt.Errorf("%w: public key is not base64url", ErrInvalidKey)
	}

	if string(pub) != string(keys.public) {
		return Keys{}, fmt.Errorf("%w: the public key does not belong to the private key", ErrInvalidKey)
	}

	return keys, nil
}

func keysOf(k *ecdsa.PrivateKey) (Keys, error) {
	pub, err := k.PublicKey.Bytes()
	if err != nil {
		return Keys{}, fmt.Errorf("%w: %w", ErrInvalidKey, err)
	}

	return Keys{private: k, public: pub}, nil
}

// PublicKey returns the base64url (without padding) of the public key: the
// applicationServerKey browsers subscribe with.
func (k Keys) PublicKey() string { return encodeBase64URL(k.public) }

// PrivateKey returns the base64url (without padding) of the private
// scalar. It is a secret.
func (k Keys) PrivateKey() string {
	b, err := k.private.Bytes()
	if err != nil {
		return "" // unreachable: a P-256 key always encodes
	}

	return encodeBase64URL(b)
}

// IsZero reports whether k holds no key.
func (k Keys) IsZero() bool { return k.private == nil }

// ParseSubscriptionKeys decodes the keys of a browser subscription
// (PushSubscription.toJSON().keys, base64url): the ECDH public key must be
// a point on P-256, the authentication secret 16 bytes.
func ParseSubscriptionKeys(p256dh, auth string) (pub [PublicKeyLen]byte, secret [AuthLen]byte, err error) {
	p, err := decodeBase64URL(p256dh)
	if err != nil || len(p) != PublicKeyLen {
		return pub, secret, fmt.Errorf("%w: p256dh must be the base64url of a 65-byte point", ErrInvalidKey)
	}

	if _, err := ecdh.P256().NewPublicKey(p); err != nil {
		return pub, secret, fmt.Errorf("%w: p256dh is not a P-256 point", ErrInvalidKey)
	}

	a, err := decodeBase64URL(auth)
	if err != nil || len(a) != AuthLen {
		return pub, secret, fmt.Errorf("%w: auth must be the base64url of 16 bytes", ErrInvalidKey)
	}

	return [PublicKeyLen]byte(p), [AuthLen]byte(a), nil
}

// decodeBase64URL accepts base64url with or without padding.
func decodeBase64URL(s string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(strings.TrimRight(strings.TrimSpace(s), "="))
}

func encodeBase64URL(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }
