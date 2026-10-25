package domain

import (
	"crypto/ecdh"
	"encoding/base64"
	"encoding/json"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"
)

// Limits of web push (ADR 0017).
const (
	// MaxPushEndpointLen bounds a subscription's endpoint URL.
	MaxPushEndpointLen = 2048
	// MaxDeviceLabelLen bounds a subscription's device label, in
	// characters.
	MaxDeviceLabelLen = 100
	// MaxPushPayloadLen bounds a message's payload in bytes; push services
	// take about 4 KB once encrypted.
	MaxPushPayloadLen = 3000
)

// PushSubscription is a browser's web push subscription of an account (ADR
// 0017): where the browser vendor's push service takes messages for one
// device, and the keys to encrypt them for it. An endpoint belongs to one
// subscription.
type PushSubscription struct {
	ID        PushSubscriptionID
	AccountID AccountID
	// Endpoint is the push service's https URL of the subscription.
	Endpoint string
	// P256DH is the browser's ECDH public key, an uncompressed P-256 point;
	// Auth its authentication secret (RFC 8291).
	P256DH [65]byte
	Auth   [16]byte
	// DeviceLabel names the device for its holder, e.g. "Firefox on
	// Linux"; optional.
	DeviceLabel string
	CreatedAt   time.Time
	// LastUsedAt is when the device last saved the subscription or a
	// message last reached the push service for it; it starts at
	// CreatedAt.
	LastUsedAt time.Time
}

// Validate checks the subscription for consistency. All errors wrap
// ErrValidation.
func (s PushSubscription) Validate() error {
	return validate(
		rule{!s.ID.IsZero(), "push subscription id must be set"},
		rule{!s.AccountID.IsZero(), "push subscription account must be set"},
		on("endpoint", validPushEndpoint(s.Endpoint), "the endpoint must be an https URL"),
		on("keys.p256dh", s.P256DH[0] == 4, "p256dh must be an uncompressed P-256 point"),
		on("keys.auth", s.Auth != [16]byte{}, "auth must be set"),
		on("deviceLabel", utf8.RuneCountInString(s.DeviceLabel) <= MaxDeviceLabelLen && utf8.ValidString(s.DeviceLabel),
			"the device label is too long"),
		rule{!s.CreatedAt.IsZero() && !s.LastUsedAt.IsZero(), "push subscription times must be set"},
		rule{notBefore(s.LastUsedAt, s.CreatedAt), "push subscription cannot be used before it was created"},
	)
}

// validPushEndpoint reports whether s is an absolute https URL with a host
// and without credentials, not too long. It starts with a literal,
// lowercase "https://", as the database's CHECK has it.
func validPushEndpoint(s string) bool {
	u, err := url.Parse(s)

	return err == nil && strings.HasPrefix(s, "https://") && len(s) <= MaxPushEndpointLen && u.Hostname() != "" &&
		u.User == nil
}

// ParsePushKeys decodes the keys of a browser's subscription
// (PushSubscription.toJSON().keys, base64url with or without padding):
// p256dh must be a point on P-256 (RFC 8291), auth 16 bytes. A bad key is a
// *FieldError on keys.p256dh or keys.auth.
func ParsePushKeys(p256dh, auth string) (pub [65]byte, secret [16]byte, err error) {
	p, err := decodeBase64URL(p256dh)
	if err != nil || len(p) != len(pub) {
		return pub, secret, &FieldError{Field: "keys.p256dh", Msg: "p256dh must be the base64url of a P-256 public key"}
	}

	if _, err := ecdh.P256().NewPublicKey(p); err != nil {
		return pub, secret, &FieldError{Field: "keys.p256dh", Msg: "p256dh must be the base64url of a P-256 public key"}
	}

	a, err := decodeBase64URL(auth)
	if err != nil || len(a) != len(secret) {
		return pub, secret, &FieldError{Field: "keys.auth", Msg: "auth must be the base64url of 16 bytes"}
	}

	return [65]byte(p), [16]byte(a), nil
}

// decodeBase64URL accepts base64url with or without padding.
func decodeBase64URL(s string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(strings.TrimRight(strings.TrimSpace(s), "="))
}

// PushMessage is a web push message waiting to be sent, or one that was
// (ADR 0017). Like mails, messages are written in the transaction that
// causes them and sent later by a background job.
type PushMessage struct {
	ID             PushMessageID
	SubscriptionID PushSubscriptionID
	// Payload is the JSON object the service worker shows as a
	// notification.
	Payload   string
	CreatedAt time.Time
	// ExpiresAt is when the message stops being worth sending (an invite
	// that ran out); the push service keeps it no longer either.
	ExpiresAt time.Time
	// Attempts counts the delivery attempts so far.
	Attempts int
	// NextAttemptAt is when the next attempt is due; zero once the message
	// is sent or given up.
	NextAttemptAt time.Time
	SentAt        time.Time // optional
	FailedAt      time.Time // optional: when delivery was given up
	LastError     string    // optional, the error of the last failed attempt
}

// Pending reports whether the message is neither sent nor given up.
func (m PushMessage) Pending() bool { return m.SentAt.IsZero() && m.FailedAt.IsZero() }

// Validate checks the message for consistency. All errors wrap
// ErrValidation.
func (m PushMessage) Validate() error {
	return validate(
		rule{!m.ID.IsZero(), "push message id must be set"},
		rule{!m.SubscriptionID.IsZero(), "push message subscription must be set"},
		rule{
			len(m.Payload) <= MaxPushPayloadLen && json.Valid([]byte(m.Payload)) && m.Payload[0] == '{',
			"push message payload must be a JSON object of at most 3000 bytes",
		},
		rule{!m.CreatedAt.IsZero(), "push message creation time must be set"},
		rule{m.ExpiresAt.After(m.CreatedAt), "push message must expire after it is created"},
		rule{m.Attempts >= 0, "push message attempts must not be negative"},
		rule{m.SentAt.IsZero() || m.FailedAt.IsZero(), "a push message is not both sent and given up"},
		rule{m.Pending() != m.NextAttemptAt.IsZero(), "a pending push message, and only one, has a next attempt"},
		rule{
			notBefore(m.SentAt, m.CreatedAt) && notBefore(m.FailedAt, m.CreatedAt),
			"push message cannot be done before it is created",
		},
	)
}
