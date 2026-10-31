package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/mail"
	"github.com/cwbudde/studio-schedule/internal/service"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// The store-failure sweeps of the account services (auth, credentials,
// impersonation, account administration, audit log): a failure of any store
// call is returned, and nothing the operation wrote stays — no half-changed
// password, no session, no link, no audit event and no mail.

// authOp prepares the fixture f for an operation and returns it, to run on
// a service on a FaultStore.
type authOp func(t *testing.T, f *authFixture) func(svc *service.AuthService) error

// sweepAuth sweeps the store faults of the operation prepare returns.
func sweepAuth(t *testing.T, prepare authOp) {
	t.Helper()

	service.SweepFaults(t, service.FaultSweep{Setup: func(t *testing.T) (*service.FaultStore, func() error) {
		f := newAuthFixture(t)
		op := prepare(t, f)
		fs, svc := f.faulty()

		return fs, func() error { return op(svc) }
	}})
}

// impersonating signs Ada (admin) in as Bea (member) and returns both
// sessions; Ada's own session ended with the start.
func (f *authFixture) impersonating() (admin, imp service.SignedIn) {
	f.t.Helper()

	admin = f.signedUp("ada@example.org", domain.RoleAdmin)
	bea := f.signedUp("bea@example.org", domain.RoleMember)

	imp, err := f.svc.Impersonate(f.t.Context(), admin.Principal, bea.Principal.AccountID)
	if err != nil {
		f.t.Fatal(err)
	}

	return admin, imp
}

// alwaysRehash is a hasher whose stored hashes are all outdated.
type alwaysRehash struct{ service.PasswordHasher }

func (alwaysRehash) NeedsRehash(string) bool { return true }

func TestAuthStoreFaults(t *testing.T) {
	t.Parallel()

	const pw = "correct horse"

	register := func(t *testing.T, email, locale string) func(svc *service.AuthService) error {
		return func(svc *service.AuthService) error {
			return svc.Register(t.Context(), service.Registration{
				GivenName: "Bea", FamilyName: "Example", Email: email, Password: pw, Locale: locale,
			})
		}
	}

	for name, prepare := range map[string]authOp{
		"Register": func(t *testing.T, _ *authFixture) func(*service.AuthService) error {
			return register(t, "bea@example.org", "en")
		},
		// A second registration of a pending address renews its person,
		// language, password and link.
		"RegisterAgain": func(t *testing.T, f *authFixture) func(*service.AuthService) error {
			f.register("bea@example.org", pw)

			return register(t, "bea@example.org", "de")
		},
		"Confirm": func(t *testing.T, f *authFixture) func(*service.AuthService) error {
			f.register("bea@example.org", pw)
			token := f.lastToken()

			return func(svc *service.AuthService) error {
				_, err := svc.Confirm(t.Context(), token, pw)

				return err
			}
		},
		// A login with an outdated hash stores the new one with the session.
		"LoginRehash": func(t *testing.T, f *authFixture) func(*service.AuthService) error {
			f.signedUp("bea@example.org", domain.RoleMember)
			f.hasher = alwaysRehash{f.hasher}

			return func(svc *service.AuthService) error {
				_, err := svc.Login(t.Context(), "bea@example.org", pw)

				return err
			}
		},
		// A session last seen two days ago is extended.
		"AuthenticateExtends": func(t *testing.T, f *authFixture) func(*service.AuthService) error {
			s := f.signedUp("bea@example.org", domain.RoleMember)
			f.clock.advance(48 * time.Hour)

			return func(svc *service.AuthService) error {
				_, refreshed, err := svc.Authenticate(t.Context(), s.Token)
				if err == nil && refreshed.Token == "" {
					return errors.New("not extended")
				}

				return err
			}
		},
		"AuthenticateImpersonation": func(t *testing.T, f *authFixture) func(*service.AuthService) error {
			_, imp := f.impersonating()

			return func(svc *service.AuthService) error {
				_, _, err := svc.Authenticate(t.Context(), imp.Token)

				return err
			}
		},
		// Signing out of an impersonation audits its end.
		"LogoutImpersonation": func(t *testing.T, f *authFixture) func(*service.AuthService) error {
			_, imp := f.impersonating()

			return func(svc *service.AuthService) error { return svc.Logout(t.Context(), imp.Token) }
		},
		"PurgeExpired": func(t *testing.T, f *authFixture) func(*service.AuthService) error {
			f.signedUp("bea@example.org", domain.RoleMember)
			f.register("cleo@example.org", pw)
			f.clock.advance(service.SessionIdleTimeout)

			return func(svc *service.AuthService) error {
				n, err := svc.PurgeExpired(t.Context())
				if err == nil && n != 2 {
					return errors.New("purged too little")
				}

				return err
			}
		},
		// Me of an impersonated instructor reads her staff records and the
		// admin.
		"MeImpersonated": func(t *testing.T, f *authFixture) func(*service.AuthService) error {
			_, imp := f.impersonating()
			f.update("bea@example.org", func(a *domain.Account) { a.Role = domain.RoleInstructor })

			return func(svc *service.AuthService) error {
				_, err := svc.Me(t.Context(), imp.Principal)

				return err
			}
		},
		"SetLocale": func(t *testing.T, f *authFixture) func(*service.AuthService) error {
			s := f.signedUp("bea@example.org", domain.RoleMember)

			return func(svc *service.AuthService) error { return svc.SetLocale(t.Context(), s.Principal, "de") }
		},
		"RequestPasswordReset": func(t *testing.T, f *authFixture) func(*service.AuthService) error {
			f.signedUp("bea@example.org", domain.RoleMember)

			return func(svc *service.AuthService) error {
				return svc.RequestPasswordReset(t.Context(), "bea@example.org", "en")
			}
		},
		// A reset of a confirmed account with a password: the password, the
		// sessions, the links and the password-changed mail.
		"ResetPassword": func(t *testing.T, f *authFixture) func(*service.AuthService) error {
			f.signedUp("bea@example.org", domain.RoleMember)

			if err := f.svc.RequestPasswordReset(t.Context(), "bea@example.org", "en"); err != nil {
				t.Fatal(err)
			}

			token, _ := f.link(mail.TemplateResetPassword, "reset-password")

			return func(svc *service.AuthService) error {
				return svc.ResetPassword(t.Context(), token, "a new secret phrase")
			}
		},
		// The reset link of an invitation confirms the account as well.
		"ResetPasswordInvited": func(t *testing.T, f *authFixture) func(*service.AuthService) error {
			f.invited("cleo@example.org")

			if err := f.svc.RequestPasswordReset(t.Context(), "cleo@example.org", "en"); err != nil {
				t.Fatal(err)
			}

			token, _ := f.link(mail.TemplateResetPassword, "reset-password")

			return func(svc *service.AuthService) error {
				return svc.ResetPassword(t.Context(), token, "a new secret phrase")
			}
		},
		"ChangePassword": func(t *testing.T, f *authFixture) func(*service.AuthService) error {
			s := f.signedUp("bea@example.org", domain.RoleMember)

			return func(svc *service.AuthService) error {
				return svc.ChangePassword(t.Context(), s.Principal, pw, "a new secret phrase")
			}
		},
		"RequestEmailChange": func(t *testing.T, f *authFixture) func(*service.AuthService) error {
			s := f.signedUp("bea@example.org", domain.RoleMember)

			return func(svc *service.AuthService) error {
				return svc.RequestEmailChange(t.Context(), s.Principal, pw, "bea.new@example.org", "en")
			}
		},
		"ConfirmEmailChange": func(t *testing.T, f *authFixture) func(*service.AuthService) error {
			s := f.signedUp("bea@example.org", domain.RoleMember)

			err := f.svc.RequestEmailChange(t.Context(), s.Principal, pw, "bea.new@example.org", "en")
			if err != nil {
				t.Fatal(err)
			}

			token, _ := f.link(mail.TemplateChangeEmail, "confirm-email-change")

			return func(svc *service.AuthService) error {
				return svc.ConfirmEmailChange(t.Context(), token, "en")
			}
		},
		"Impersonate": func(t *testing.T, f *authFixture) func(*service.AuthService) error {
			admin := f.signedUp("ada@example.org", domain.RoleAdmin)
			bea := f.signedUp("bea@example.org", domain.RoleMember)

			return func(svc *service.AuthService) error {
				_, err := svc.Impersonate(t.Context(), admin.Principal, bea.Principal.AccountID)

				return err
			}
		},
		"StopImpersonation": func(t *testing.T, f *authFixture) func(*service.AuthService) error {
			_, imp := f.impersonating()

			return func(svc *service.AuthService) error {
				_, err := svc.StopImpersonation(t.Context(), imp.Principal)

				return err
			}
		},
		"Accounts": func(t *testing.T, f *authFixture) func(*service.AuthService) error {
			f.signedUp("bea@example.org", domain.RoleMember)

			return func(svc *service.AuthService) error {
				_, err := svc.Accounts(t.Context())

				return err
			}
		},
		// The log names accounts (the impersonation) and a person (the
		// deleted account).
		"AuditLog": func(t *testing.T, f *authFixture) func(*service.AuthService) error {
			_, imp := f.impersonating()

			if _, err := f.svc.StopImpersonation(t.Context(), imp.Principal); err != nil {
				t.Fatal(err)
			}

			admin := f.login("ada@example.org", pw)
			cleo := f.signedUp("cleo@example.org", domain.RoleMember)

			if err := f.svc.DeleteAccount(t.Context(), admin.Principal, cleo.Principal.AccountID); err != nil {
				t.Fatal(err)
			}

			return func(svc *service.AuthService) error {
				_, err := svc.AuditLog(t.Context())

				return err
			}
		},
		// Disabling an account ends its sessions and audits the
		// impersonation of it that was running.
		"DisableAccount": func(t *testing.T, f *authFixture) func(*service.AuthService) error {
			_, imp := f.impersonating()
			cleo := f.signedUp("cleo@example.org", domain.RoleAdmin)

			return func(svc *service.AuthService) error {
				_, err := svc.SetAccountDisabled(t.Context(), cleo.Principal, imp.Principal.AccountID, true)

				return err
			}
		},
		"EnableAccount": func(t *testing.T, f *authFixture) func(*service.AuthService) error {
			admin := f.signedUp("ada@example.org", domain.RoleAdmin)
			bea := f.signedUp("bea@example.org", domain.RoleMember)
			f.update("bea@example.org", func(a *domain.Account) { a.DisabledAt = f.clock.Now() })

			return func(svc *service.AuthService) error {
				_, err := svc.SetAccountDisabled(t.Context(), admin.Principal, bea.Principal.AccountID, false)

				return err
			}
		},
		// Demoting one of two admins reads the admins under their locks.
		"SetAccountRole": func(t *testing.T, f *authFixture) func(*service.AuthService) error {
			admin := f.signedUp("ada@example.org", domain.RoleAdmin)
			other := f.signedUp("bea@example.org", domain.RoleAdmin)

			return func(svc *service.AuthService) error {
				_, err := svc.SetAccountRole(t.Context(), admin.Principal, other.Principal.AccountID,
					domain.RoleOffice)

				return err
			}
		},
		"DeleteAccount": func(t *testing.T, f *authFixture) func(*service.AuthService) error {
			admin := f.signedUp("ada@example.org", domain.RoleAdmin)
			other := f.signedUp("bea@example.org", domain.RoleAdmin)

			return func(svc *service.AuthService) error {
				return svc.DeleteAccount(t.Context(), admin.Principal, other.Principal.AccountID)
			}
		},
		"DeleteOwnAccount": func(t *testing.T, f *authFixture) func(*service.AuthService) error {
			bea := f.signedUp("bea@example.org", domain.RoleMember)

			return func(svc *service.AuthService) error {
				return svc.DeleteOwnAccount(t.Context(), bea.Principal, pw, "en")
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			sweepAuth(t, prepare)
		})
	}
}

// invited stores an invited account of email: unconfirmed, without a
// password, as the office's invitation leaves it.
func (f *authFixture) invited(email string) domain.Account {
	f.t.Helper()

	p := domain.Person{ID: ids.New[domain.PersonID](f.seq), GivenName: "Cleo", FamilyName: "Example"}
	acc := domain.Account{ID: ids.New[domain.AccountID](f.seq), PersonID: p.ID, Email: email, Role: domain.RoleMember}

	err := f.st.InTx(f.t.Context(), store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		return errors.Join(tx.Persons().Create(ctx, p), tx.Accounts().Create(ctx, acc))
	})
	if err != nil {
		f.t.Fatal(err)
	}

	return acc
}
