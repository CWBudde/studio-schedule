package domain_test

import (
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
