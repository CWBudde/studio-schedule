package service_test

import (
	"errors"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/service"
)

// operatorVia is the channel of a command run by the OS user operator.
var operatorVia = domain.AuditVia{Channel: domain.ChannelCLI, OSUser: "operator"}

// TestActor: the command line's principal (ADR 0020) is the account of the
// email, whatever its case and surrounding spaces, with its role, person
// and early access, no session, and the channel it was asked for. An
// unknown account, one not confirmed yet and a disabled one cannot act;
// all three are not found.
func TestActor(t *testing.T) {
	t.Parallel()

	f := newAuthFixture(t)
	ctx := t.Context()
	f.signedUp("olga@example.org", domain.RoleOffice)
	f.update("olga@example.org", func(a *domain.Account) { a.VisibleDays = 30 })
	acc := f.account("olga@example.org")

	p, err := f.svc.Actor(ctx, " OLGA@example.ORG\t", operatorVia)
	if err != nil {
		t.Fatal(err)
	}

	// No session and no impersonator: the zero values.
	want := service.Principal{
		AccountID: acc.ID, PersonID: acc.PersonID, Email: "olga@example.org", Role: domain.RoleOffice,
		VisibleDays: 30, Via: operatorVia,
	}
	if p != want {
		t.Errorf("Actor = %+v, want %+v", p, want)
	}

	f.register("pending@example.org", "correct horse")
	f.signedUp("gone@example.org", domain.RoleMember)
	f.update("gone@example.org", func(a *domain.Account) { a.DisabledAt = f.clock.Now().Add(-time.Minute) })

	for email, want := range map[string]error{
		"nobody@example.org":  service.ErrNoActor,
		"pending@example.org": service.ErrActorInactive,
		"gone@example.org":    service.ErrActorInactive,
	} {
		_, err := f.svc.Actor(ctx, email, operatorVia)
		if !errors.Is(err, want) || !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("Actor(%s) = %v, want %v (not found)", email, err, want)
		}
	}
}

// TestActorStoreFaults: a store failure while looking up the actor is
// returned as itself, not as an unknown account.
func TestActorStoreFaults(t *testing.T) {
	t.Parallel()

	sweepAuth(t, func(t *testing.T, f *authFixture) func(*service.AuthService) error {
		f.signedUp("olga@example.org", domain.RoleOffice)

		return func(svc *service.AuthService) error {
			_, err := svc.Actor(t.Context(), "olga@example.org", operatorVia)
			if errors.Is(err, domain.ErrNotFound) {
				return errors.New("a store failure reported as an unknown actor")
			}

			return err
		}
	})
}
