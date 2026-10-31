package webpush

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
)

// RFC 8291 encryption (aes128gcm content coding, RFC 8188), one record.
const (
	// recordSize is the record size the header announces; the message is
	// one record, so it bounds the payload (MaxPayload).
	recordSize = 4096
	saltLen    = 16
	// headerLen is the content coding header: salt, record size, key ID
	// length and the key ID (the sender's public key).
	headerLen = saltLen + 4 + 1 + PublicKeyLen
	tagLen    = 16
	// MaxPayload is the longest payload one record takes: the record holds
	// the payload, the padding delimiter and the AEAD tag. Push services
	// accept 4096 bytes of body (RFC 8030), which the header leaves room in.
	MaxPayload = recordSize - headerLen - 1 - tagLen
)

// ErrPayloadTooLarge is returned for a payload longer than MaxPayload.
var ErrPayloadTooLarge = errors.New("webpush: payload too large")

// Encrypt encrypts payload for the subscription per RFC 8291 with a new
// ephemeral key and salt, and returns the message body.
func Encrypt(sub Subscription, payload []byte) ([]byte, error) {
	ephemeral, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("webpush: generate key: %w", err)
	}

	salt := make([]byte, saltLen)
	// crypto/rand.Read never returns an error (it crashes the program
	// instead, since Go 1.24).
	rand.Read(salt)

	return encrypt(sub, payload, ephemeral, salt)
}

// encrypt is Encrypt with the application server's ephemeral key and the
// salt given, so the RFC's example can be reproduced.
func encrypt(sub Subscription, payload []byte, as *ecdh.PrivateKey, salt []byte) ([]byte, error) {
	if len(payload) > MaxPayload {
		return nil, fmt.Errorf("%w: %d bytes, at most %d", ErrPayloadTooLarge, len(payload), MaxPayload)
	}

	cek, nonce, err := deriveKeys(sub, as, salt)
	if err != nil {
		return nil, err
	}

	block, err := aes.NewCipher(cek)
	if err != nil {
		return nil, fmt.Errorf("webpush: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("webpush: %w", err)
	}

	asPublic := as.PublicKey().Bytes()

	out := make([]byte, 0, headerLen+len(payload)+1+tagLen)
	out = append(out, salt...)
	out = binary.BigEndian.AppendUint32(out, recordSize)
	out = append(out, PublicKeyLen) // the key ID's length: an uncompressed point
	out = append(out, asPublic...)

	// The last (and only) record ends with the delimiter 0x02, unpadded.
	record := append(append(make([]byte, 0, len(payload)+1), payload...), 0x02)

	return gcm.Seal(out, nonce, record, nil), nil
}

// deriveKeys derives the content encryption key and the nonce (RFC 8291,
// section 3.4) from the ECDH secret of the sender's key and the browser's,
// the browser's authentication secret and the salt.
func deriveKeys(sub Subscription, as *ecdh.PrivateKey, salt []byte) (cek, nonce []byte, err error) {
	ua, err := ecdh.P256().NewPublicKey(sub.P256DH[:])
	if err != nil {
		return nil, nil, fmt.Errorf("%w: p256dh is not a P-256 point", ErrInvalidKey)
	}

	secret, err := as.ECDH(ua)
	if err != nil {
		return nil, nil, fmt.Errorf("webpush: ecdh: %w", err)
	}

	// key_info = "WebPush: info" || 0x00 || ua_public || as_public
	keyInfo := append([]byte("WebPush: info\x00"), sub.P256DH[:]...)
	keyInfo = append(keyInfo, as.PublicKey().Bytes()...)

	ikm, err := hkdf.Key(sha256.New, secret, sub.Auth[:], string(keyInfo), 32)
	if err != nil {
		return nil, nil, fmt.Errorf("webpush: hkdf: %w", err)
	}

	prk, err := hkdf.Extract(sha256.New, ikm, salt)
	if err != nil {
		return nil, nil, fmt.Errorf("webpush: hkdf: %w", err)
	}

	if cek, err = hkdf.Expand(sha256.New, prk, "Content-Encoding: aes128gcm\x00", 16); err != nil {
		return nil, nil, fmt.Errorf("webpush: hkdf: %w", err)
	}

	if nonce, err = hkdf.Expand(sha256.New, prk, "Content-Encoding: nonce\x00", 12); err != nil {
		return nil, nil, fmt.Errorf("webpush: hkdf: %w", err)
	}

	return cek, nonce, nil
}
