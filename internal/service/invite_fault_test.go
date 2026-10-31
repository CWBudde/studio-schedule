package service_test

import (
	"testing"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/mail"
	"github.com/cwbudde/studio-schedule/internal/service"
	"github.com/cwbudde/studio-schedule/internal/store/memory"
)

// faultyAuth returns an AuthService of f on a FaultStore around f's memory
// store.
func (f *authFixture) faultyAuth() (*service.FaultStore, *service.AuthService) {
	f.t.Helper()

	mem, ok := f.st.(*memory.Store)
	if !ok {
		f.t.Fatalf("the auth fixture's store is a %T", f.st)
	}

	renderer, err := mail.NewRenderer()
	if err != nil {
		f.t.Fatal(err)
	}

	fs := service.NewFaultStore(mem)

	return fs, service.NewAuthService(fs, f.clock, ids.NewSequence(), nil, renderer, appURL)
}

// TestCreateAccountStoreFaults: a store failure while the office creates an
// account is returned, and neither the account, its token nor the invite
// mail stays.
func TestCreateAccountStoreFaults(t *testing.T) {
	t.Parallel()

	service.SweepFaults(t, service.FaultSweep{Setup: func(t *testing.T) (*service.FaultStore, func() error) {
		f := newAuthFixture(t)
		kim := f.newPerson("Kim")
		fs, svc := f.faultyAuth()

		return fs, func() error {
			_, err := svc.CreateAccount(t.Context(), officePrincipal, kim, service.NewAccount{
				Email: "kim@example.org", Role: domain.RoleMember,
			})

			return err
		}
	}})
}

// TestInvitedResetCredentialsFault: whether an invited account may ask for
// a new link depends on it having no password yet; a store failure reading
// its credentials fails the request, and no link is mailed.
func TestInvitedResetCredentialsFault(t *testing.T) {
	t.Parallel()

	f := newAuthFixture(t)

	_, err := f.svc.CreateAccount(t.Context(), officePrincipal, f.newPerson("Kim"), service.NewAccount{
		Email: "kim@example.org", Role: domain.RoleMember,
	})
	if err != nil {
		t.Fatal(err)
	}

	fs, svc := f.faultyAuth()
	before := fs.Snapshot(t)
	fs.Fail("Credentials.Get", 1)

	wantIs(t, svc.RequestPasswordReset(t.Context(), "kim@example.org", ""), service.ErrInjected)

	if changed := before.Changed(fs.Snapshot(t)); len(changed) > 0 {
		t.Errorf("the failed request changed %v", changed)
	}
}
