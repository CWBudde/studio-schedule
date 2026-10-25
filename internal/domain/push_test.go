package domain_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
)

func TestPushValidate(t *testing.T) {
	t.Parallel()

	t.Run("Subscription", func(t *testing.T) {
		t.Parallel()
		checkValidate(t, func() domain.PushSubscription {
			s := domain.PushSubscription{
				ID: newID[domain.PushSubscriptionID](), AccountID: newID[domain.AccountID](),
				Endpoint: "https://push.example.net/push/abc", DeviceLabel: "Firefox on Linux",
				CreatedAt: t0, LastUsedAt: t0.Add(time.Hour),
			}
			s.P256DH[0], s.Auth[0] = 4, 1

			return s
		}, map[string]func(*domain.PushSubscription){
			"zero id":           func(s *domain.PushSubscription) { s.ID = domain.PushSubscriptionID{} },
			"zero account":      func(s *domain.PushSubscription) { s.AccountID = domain.AccountID{} },
			"plain http":        func(s *domain.PushSubscription) { s.Endpoint = "http://push.example.net/push/abc" },
			"upper-case scheme": func(s *domain.PushSubscription) { s.Endpoint = "HTTPS://push.example.net/push/abc" },
			"no host":           func(s *domain.PushSubscription) { s.Endpoint = "https:///push" },
			"credentials":       func(s *domain.PushSubscription) { s.Endpoint = "https://u:p@push.example.net/x" },
			"long endpoint":     func(s *domain.PushSubscription) { s.Endpoint += strings.Repeat("x", 2048) },
			"compressed key":    func(s *domain.PushSubscription) { s.P256DH[0] = 2 },
			"no auth":           func(s *domain.PushSubscription) { s.Auth = [16]byte{} },
			"long device label": func(s *domain.PushSubscription) { s.DeviceLabel = strings.Repeat("é", 101) },
			"no created at":     func(s *domain.PushSubscription) { s.CreatedAt = time.Time{} },
			"no last used":      func(s *domain.PushSubscription) { s.LastUsedAt = time.Time{} },
			"used before":       func(s *domain.PushSubscription) { s.LastUsedAt = t0.Add(-time.Second) },
		})
	})

	t.Run("Message", func(t *testing.T) {
		t.Parallel()
		checkValidate(t, func() domain.PushMessage {
			return domain.PushMessage{
				ID: newID[domain.PushMessageID](), SubscriptionID: newID[domain.PushSubscriptionID](),
				Payload: `{"title":"A place is free"}`, CreatedAt: t0, ExpiresAt: t0.Add(time.Hour), NextAttemptAt: t0,
			}
		}, map[string]func(*domain.PushMessage){
			"zero id":            func(m *domain.PushMessage) { m.ID = domain.PushMessageID{} },
			"zero subscription":  func(m *domain.PushMessage) { m.SubscriptionID = domain.PushSubscriptionID{} },
			"no payload":         func(m *domain.PushMessage) { m.Payload = "" },
			"not json":           func(m *domain.PushMessage) { m.Payload = "{" },
			"not an object":      func(m *domain.PushMessage) { m.Payload = `"text"` },
			"payload too long":   func(m *domain.PushMessage) { m.Payload = `{"t":"` + strings.Repeat("x", 3000) + `"}` },
			"no created at":      func(m *domain.PushMessage) { m.CreatedAt = time.Time{} },
			"expired at once":    func(m *domain.PushMessage) { m.ExpiresAt = t0 },
			"negative attempts":  func(m *domain.PushMessage) { m.Attempts = -1 },
			"pending without":    func(m *domain.PushMessage) { m.NextAttemptAt = time.Time{} },
			"sent with next":     func(m *domain.PushMessage) { m.SentAt = t0 },
			"sent and given up":  func(m *domain.PushMessage) { m.SentAt, m.FailedAt, m.NextAttemptAt = t0, t0, time.Time{} },
			"sent before create": func(m *domain.PushMessage) { m.SentAt, m.NextAttemptAt = t0.Add(-time.Second), time.Time{} },
		})
	})
}

func TestParsePushKeys(t *testing.T) {
	t.Parallel()

	// The keys of RFC 8291's example.
	const p256dh = "BCVxsr7N_eNgVRqvHtD0zTZsEc6-VV-JvLexhqUzORcxaOzi6-AYWXvTBHm4bjyPjs7Vd8pZGH6SRpkNtoIAiw4"
	const auth = "BTBZMqHH6r4Tts7J_aSIgg"

	pub, secret, err := domain.ParsePushKeys(p256dh, auth+"==") // padding is accepted
	if err != nil || pub[0] != 4 || secret[0] != 0x05 {
		t.Fatalf("ParsePushKeys: %x %x %v", pub[:2], secret[:2], err)
	}

	notOnCurve := "BA" + strings.Repeat("A", 85) // 0x04 and zeros: not a point

	for name, tc := range map[string]struct{ p256dh, auth, field string }{
		"short p256dh":     {"BCVx", auth, "keys.p256dh"},
		"not on the curve": {notOnCurve, auth, "keys.p256dh"},
		"short auth":       {p256dh, "BTBZ", "keys.auth"},
		"not base64":       {p256dh, "!!!", "keys.auth"},
	} {
		_, _, err := domain.ParsePushKeys(tc.p256dh, tc.auth)

		var fe *domain.FieldError
		if !errors.As(err, &fe) || fe.Field != tc.field || !errors.Is(err, domain.ErrValidation) {
			t.Errorf("%s: want a field error on %s, got %v", name, tc.field, err)
		}
	}
}
