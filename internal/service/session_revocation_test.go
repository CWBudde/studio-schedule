package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/service"
)

func TestImpersonationRechecksCurrentAccountsAndSession(t *testing.T) {
	t.Parallel()

	t.Run("start after admin demotion", func(t *testing.T) {
		f := newAuthFixture(t)
		admin := f.signedUp("admin@example.test", domain.RoleAdmin)
		member := f.signedUp("member@example.test", domain.RoleMember)
		f.update("admin@example.test", func(a *domain.Account) { a.Role = domain.RoleOffice })

		_, err := f.svc.Impersonate(t.Context(), admin.Principal, member.Principal.AccountID)
		wantIs(t, err, service.ErrNoSession)
	})
	t.Run("start after session expiry", func(t *testing.T) {
		f := newAuthFixture(t)
		admin := f.signedUp("admin@example.test", domain.RoleAdmin)
		member := f.signedUp("member@example.test", domain.RoleMember)
		f.clock.advance(service.SessionIdleTimeout)

		_, err := f.svc.Impersonate(t.Context(), admin.Principal, member.Principal.AccountID)
		wantIs(t, err, service.ErrNoSession)
	})
	t.Run("stop after subject promotion", func(t *testing.T) {
		f := newAuthFixture(t)
		admin := f.signedUp("admin@example.test", domain.RoleAdmin)
		member := f.signedUp("member@example.test", domain.RoleMember)
		as, err := f.svc.Impersonate(t.Context(), admin.Principal, member.Principal.AccountID)
		if err != nil {
			t.Fatal(err)
		}
		f.update("member@example.test", func(a *domain.Account) { a.Role = domain.RoleAdmin })

		_, err = f.svc.StopImpersonation(t.Context(), as.Principal)
		wantIs(t, err, service.ErrNoSession)
	})
}

type idlePushSender struct{}

func (idlePushSender) PublicKey() string { return "" }
func (idlePushSender) Send(context.Context, domain.PushSubscription, []byte, time.Duration) error {
	return nil
}

func TestPushChecksStoredSessionAfterRequestAuthentication(t *testing.T) {
	t.Parallel()

	f := newAuthFixture(t)
	member := f.signedUp("member@example.test", domain.RoleMember)
	push := service.NewPushService(f.st, f.clock, f.seq, idlePushSender{})
	if err := f.svc.Logout(t.Context(), member.Token); err != nil {
		t.Fatal(err)
	}

	err := push.Subscribe(t.Context(), member.Principal, service.PushSubscriptionInput{
		Endpoint: "https://push.example.test/fixture",
		P256DH:   "BCVxsr7N_eNgVRqvHtD0zTZsEc6-VV-JvLexhqUzORcxaOzi6-AYWXvTBHm4bjyPjs7Vd8pZGH6SRpkNtoIAiw4",
		Auth:     "BTBZMqHH6r4Tts7J_aSIgg",
	})
	wantIs(t, err, service.ErrNoSession)
}
