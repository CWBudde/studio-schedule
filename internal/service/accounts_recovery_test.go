package service_test

import (
	"testing"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/service"
)

// TestAccountsRecoveryStoreFaults sweeps the store failures of the command
// line's account administration (accounts_recovery.go): a failure of any
// store call is returned, and no link, mail, ended session or audit event
// stays.
func TestAccountsRecoveryStoreFaults(t *testing.T) {
	t.Parallel()

	for name, prepare := range map[string]authOp{
		"AccountByRefEmail": func(t *testing.T, f *authFixture) func(*service.AuthService) error {
			f.signedUp("bea@example.org", domain.RoleMember)

			return func(svc *service.AuthService) error {
				_, err := svc.AccountByRef(t.Context(), "bea@example.org")

				return err
			}
		},
		"AccountByRefID": func(t *testing.T, f *authFixture) func(*service.AuthService) error {
			bea := f.signedUp("bea@example.org", domain.RoleMember)

			return func(svc *service.AuthService) error {
				_, err := svc.AccountByRef(t.Context(), bea.Principal.AccountID.String())

				return err
			}
		},
		"IssuePasswordReset": func(t *testing.T, f *authFixture) func(*service.AuthService) error {
			ada := f.signedUp("ada@example.org", domain.RoleAdmin)
			bea := f.signedUp("bea@example.org", domain.RoleMember)

			return func(svc *service.AuthService) error {
				_, err := svc.IssuePasswordReset(t.Context(), ada.Principal, bea.Principal.AccountID, false)

				return err
			}
		},
		"MailPasswordReset": func(t *testing.T, f *authFixture) func(*service.AuthService) error {
			ada := f.signedUp("ada@example.org", domain.RoleAdmin)
			bea := f.signedUp("bea@example.org", domain.RoleMember)

			return func(svc *service.AuthService) error {
				_, err := svc.IssuePasswordReset(t.Context(), ada.Principal, bea.Principal.AccountID, true)

				return err
			}
		},
		// An invited account may set a password only without one: the
		// credentials are read.
		"IssuePasswordResetInvited": func(t *testing.T, f *authFixture) func(*service.AuthService) error {
			ada := f.signedUp("ada@example.org", domain.RoleAdmin)
			acc, err := f.svc.CreateAccount(t.Context(), officePrincipal, f.newPerson("Kim"), service.NewAccount{
				Email: "kim@example.org", Role: domain.RoleMember,
			})
			if err != nil {
				t.Fatal(err)
			}

			return func(svc *service.AuthService) error {
				_, err := svc.IssuePasswordReset(t.Context(), ada.Principal, acc.ID, false)

				return err
			}
		},
		// Ending the sessions of the impersonated account audits the
		// running impersonation.
		"RevokeSessions": func(t *testing.T, f *authFixture) func(*service.AuthService) error {
			admin, imp := f.impersonating()

			return func(svc *service.AuthService) error {
				_, err := svc.RevokeSessions(t.Context(), admin.Principal, imp.Principal.AccountID)

				return err
			}
		},
		"RevokeAllSessions": func(t *testing.T, f *authFixture) func(*service.AuthService) error {
			admin, _ := f.impersonating()

			return func(svc *service.AuthService) error {
				_, _, err := svc.RevokeAllSessions(t.Context(), admin.Principal)

				return err
			}
		},
		// No admin can sign in: the admin set is read and locked, then the
		// invited account and its credentials.
		"IssueSetupLink": func(t *testing.T, f *authFixture) func(*service.AuthService) error {
			_, err := f.svc.CreateAccount(t.Context(), adminPrincipal, f.newPerson("Kim"), service.NewAccount{
				Email: "kim@example.org", Role: domain.RoleAdmin,
			})
			if err != nil {
				t.Fatal(err)
			}

			return func(svc *service.AuthService) error {
				_, err := svc.IssueSetupLink(t.Context(), "kim@example.org", domain.AuditVia{
					Channel: domain.ChannelCLI, OSUser: "operator",
				})

				return err
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			sweepAuth(t, prepare)
		})
	}
}
