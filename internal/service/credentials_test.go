package service_test

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/mail"
	"github.com/cwbudde/studio-schedule/internal/service"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// link returns the token and recipient of the newest mail of template,
// whose link opens route.
func (f *authFixture) link(template, route string) (token, to string) {
	f.t.Helper()

	re := regexp.MustCompile(regexp.QuoteMeta(appURL+route) + `\?token=([A-Za-z0-9_-]+)`)

	for _, m := range f.mails() {
		if m.Template != template {
			continue
		}

		match := re.FindStringSubmatch(m.Text)
		if match == nil {
			f.t.Fatalf("no %s link in %q", route, m.Text)
		}

		return match[1], m.To
	}

	f.t.Fatalf("no %s mail in the outbox", template)

	return "", ""
}

// count returns how many mails of template are in the outbox.
func (f *authFixture) count(template string) int {
	f.t.Helper()

	n := 0

	for _, m := range f.mails() {
		if m.Template == template {
			n++
		}
	}

	return n
}

func (f *authFixture) login(email, pw string) service.SignedIn {
	f.t.Helper()

	s, err := f.svc.Login(f.t.Context(), email, pw)
	if err != nil {
		f.t.Fatalf("Login(%s): %v", email, err)
	}

	return s
}

func (f *authFixture) wantSignedIn(s service.SignedIn, want bool) {
	f.t.Helper()

	if _, _, err := f.svc.Authenticate(f.t.Context(), s.Token); (err == nil) != want {
		f.t.Errorf("session authenticates: %v, want %v", err == nil, want)
	}
}

func (f *authFixture) wantLogin(email, pw string, want bool) {
	f.t.Helper()

	if _, err := f.svc.Login(f.t.Context(), email, pw); (err == nil) != want {
		f.t.Errorf("Login(%s, %s) = %v, want success %v", email, pw, err, want)
	}
}

func TestPasswordReset(t *testing.T) {
	t.Parallel()

	f := newAuthFixture(t)
	first := f.signedUp("ada@example.org", domain.RoleMember)
	second := f.login("ada@example.org", "correct horse")
	f.update("ada@example.org", func(a *domain.Account) { a.Locale = "" }) // the request's language applies

	if err := f.svc.RequestPasswordReset(t.Context(), " ADA@example.org ", "de"); err != nil {
		t.Fatal(err)
	}

	token, to := f.link(mail.TemplateResetPassword, "reset-password")
	if to != "ada@example.org" {
		t.Errorf("reset mail to %q", to)
	}

	if m := f.mails()[0]; !strings.Contains(m.Subject, "Passwort") {
		t.Errorf("subject %q is not German", m.Subject)
	}

	wantIs(t, f.svc.ResetPassword(t.Context(), token, "short"), domain.ErrValidation)

	if err := f.svc.ResetPassword(t.Context(), token, "battery staple"); err != nil {
		t.Fatalf("ResetPassword: %v", err)
	}

	f.wantSignedIn(first, false)
	f.wantSignedIn(second, false)
	f.wantLogin("ada@example.org", "correct horse", false)
	f.wantLogin("ada@example.org", "battery staple", true)

	wantIs(t, f.svc.ResetPassword(t.Context(), token, "another password"), service.ErrInvalidToken)
}

func TestPasswordResetIsSilent(t *testing.T) {
	t.Parallel()

	f := newAuthFixture(t)
	f.register("pending@example.org", "correct horse") // not confirmed
	f.signedUp("off@example.org", domain.RoleMember)
	f.update("off@example.org", func(a *domain.Account) { a.DisabledAt = f.clock.Now() })

	for _, email := range []string{"nobody@example.org", "pending@example.org", "off@example.org", ""} {
		if err := f.svc.RequestPasswordReset(t.Context(), email, ""); err != nil {
			t.Errorf("RequestPasswordReset(%q) = %v, want nil", email, err)
		}
	}

	if n := f.count(mail.TemplateResetPassword); n != 0 {
		t.Errorf("%d reset mails sent, want none", n)
	}
}

func TestPasswordResetLinks(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// spoil makes the reset link unusable, or returns another token
		// to try instead.
		spoil func(f *authFixture) (other string)
	}{
		{"expired", func(f *authFixture) string { f.clock.advance(service.ResetTokenTTL); return "" }},
		{"replaced by a newer link", func(f *authFixture) string {
			if err := f.svc.RequestPasswordReset(f.t.Context(), "ada@example.org", ""); err != nil {
				f.t.Fatal(err)
			}

			return ""
		}},
		{"account disabled", func(f *authFixture) string {
			f.update("ada@example.org", func(a *domain.Account) { a.DisabledAt = f.clock.Now() })

			return ""
		}},
		{"password changed", func(f *authFixture) string {
			s := f.login("ada@example.org", "correct horse")
			if err := f.svc.ChangePassword(f.t.Context(), s.Principal, "correct horse", "battery staple"); err != nil {
				f.t.Fatal(err)
			}

			return ""
		}},
		{"address changed", func(f *authFixture) string {
			// e.g. by an admin; the link went to the old address
			f.update("ada@example.org", func(a *domain.Account) { a.Email = "ada@example.net" })

			return ""
		}},
		{"a confirmation link", func(f *authFixture) string {
			f.register("bob@example.org", "correct horse")

			return f.lastToken()
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := newAuthFixture(t)
			f.signedUp("ada@example.org", domain.RoleMember)

			if err := f.svc.RequestPasswordReset(t.Context(), "ada@example.org", ""); err != nil {
				t.Fatal(err)
			}

			token, _ := f.link(mail.TemplateResetPassword, "reset-password")
			if other := tt.spoil(f); other != "" {
				token = other
			}

			wantIs(t, f.svc.ResetPassword(t.Context(), token, "new password!"), service.ErrInvalidToken)
		})
	}
}

func TestPasswordResetEndsImpersonation(t *testing.T) {
	t.Parallel()

	f := newAuthFixture(t)
	admin := f.signedUp("admin@example.org", domain.RoleAdmin)
	f.signedUp("ada@example.org", domain.RoleMember)

	as, err := f.svc.Impersonate(t.Context(), admin.Principal, f.account("ada@example.org").ID)
	if err != nil {
		t.Fatal(err)
	}

	if err := f.svc.RequestPasswordReset(t.Context(), "ada@example.org", ""); err != nil {
		t.Fatal(err)
	}

	token, _ := f.link(mail.TemplateResetPassword, "reset-password")
	if err := f.svc.ResetPassword(t.Context(), token, "battery staple"); err != nil {
		t.Fatal(err)
	}

	f.wantSignedIn(as, false)
}

func TestChangePassword(t *testing.T) {
	t.Parallel()

	f := newAuthFixture(t)
	current := f.signedUp("ada@example.org", domain.RoleMember)
	other := f.login("ada@example.org", "correct horse")
	p := current.Principal

	wantIs(t, f.svc.ChangePassword(t.Context(), p, "wrong password", "battery staple"), service.ErrWrongPassword)
	wantIs(t, f.svc.ChangePassword(t.Context(), p, "correct horse", "short"), domain.ErrValidation)
	f.wantSignedIn(other, true)

	if err := f.svc.ChangePassword(t.Context(), p, "correct horse", "battery staple"); err != nil {
		t.Fatalf("ChangePassword: %v", err)
	}

	f.wantSignedIn(current, true)
	f.wantSignedIn(other, false)
	f.wantLogin("ada@example.org", "battery staple", true)
}

func TestChangePasswordEndsImpersonationsOfTheAdmin(t *testing.T) {
	t.Parallel()

	f := newAuthFixture(t)
	admin := f.signedUp("admin@example.org", domain.RoleAdmin)
	f.signedUp("ada@example.org", domain.RoleMember)

	// A second session of the admin impersonates; the first changes the
	// password.
	second := f.login("admin@example.org", "correct horse")

	as, err := f.svc.Impersonate(t.Context(), second.Principal, f.account("ada@example.org").ID)
	if err != nil {
		t.Fatal(err)
	}

	if err := f.svc.ChangePassword(t.Context(), admin.Principal, "correct horse", "battery staple"); err != nil {
		t.Fatal(err)
	}

	f.wantSignedIn(admin, true)
	f.wantSignedIn(as, false)
}

func TestCredentialsRefusedWhileImpersonating(t *testing.T) {
	t.Parallel()

	f := newAuthFixture(t)
	admin := f.signedUp("admin@example.org", domain.RoleAdmin)
	f.signedUp("ada@example.org", domain.RoleMember)

	as, err := f.svc.Impersonate(t.Context(), admin.Principal, f.account("ada@example.org").ID)
	if err != nil {
		t.Fatal(err)
	}

	p := as.Principal
	wantIs(t, f.svc.ChangePassword(t.Context(), p, "correct horse", "battery staple"), service.ErrImpersonating)
	wantIs(t, f.svc.RequestEmailChange(t.Context(), p, "correct horse", "new@example.org", ""), service.ErrImpersonating)
}

func TestChangeEmail(t *testing.T) {
	t.Parallel()

	f := newAuthFixture(t)
	s := f.signedUp("ada@example.org", domain.RoleMember)

	if err := f.svc.RequestEmailChange(t.Context(), s.Principal, "correct horse", " ada@example.net ", "en"); err != nil {
		t.Fatal(err)
	}

	token, to := f.link(mail.TemplateChangeEmail, "confirm-email-change")
	if to != "ada@example.net" {
		t.Errorf("change mail to %q, want the new address", to)
	}

	if got := f.account("ada@example.org").Email; got != "ada@example.org" {
		t.Errorf("email changed before confirmation: %q", got)
	}

	// The account's language wins over the request's.
	if err := f.svc.SetLocale(t.Context(), s.Principal, "de"); err != nil {
		t.Fatal(err)
	}

	if err := f.svc.ConfirmEmailChange(t.Context(), token, "en"); err != nil {
		t.Fatalf("ConfirmEmailChange: %v", err)
	}

	notice := f.mails()[0]
	if notice.Template != mail.TemplateEmailChanged || notice.To != "ada@example.org" ||
		!strings.Contains(notice.Text, "ada@example.net") || !strings.Contains(notice.Subject, "geändert") {
		t.Errorf("notice = %s to %s: %q %q", notice.Template, notice.To, notice.Subject, notice.Text)
	}

	f.wantSignedIn(s, true)
	f.wantLogin("ada@example.net", "correct horse", true)
	f.wantLogin("ada@example.org", "correct horse", false)

	wantIs(t, f.svc.ConfirmEmailChange(t.Context(), token, ""), service.ErrInvalidToken)
}

func TestChangeEmailRejected(t *testing.T) {
	t.Parallel()

	f := newAuthFixture(t)
	p := f.signedUp("ada@example.org", domain.RoleMember).Principal

	wantIs(t, f.svc.RequestEmailChange(t.Context(), p, "wrong password", "ada@example.net", ""), service.ErrWrongPassword)
	wantIs(t, f.svc.RequestEmailChange(t.Context(), p, "correct horse", "ADA@example.org", ""), service.ErrSameEmail)
	// The request names the address newEmail; a 422 points there.
	wantField(t, f.svc.RequestEmailChange(t.Context(), p, "correct horse", "not an email", ""), "newEmail")
	wantField(t, f.svc.RequestEmailChange(t.Context(), p, "correct horse", "Ada <ada@example.net>", ""), "newEmail")

	if n := f.count(mail.TemplateChangeEmail); n != 0 {
		t.Errorf("%d change mails sent, want none", n)
	}
}

func TestChangeEmailToTakenAddressIsSilent(t *testing.T) {
	t.Parallel()

	f := newAuthFixture(t)
	s := f.signedUp("ada@example.org", domain.RoleMember)
	f.signedUp("bob@example.org", domain.RoleMember)

	if err := f.svc.RequestEmailChange(t.Context(), s.Principal, "correct horse", "BOB@example.org", ""); err != nil {
		t.Errorf("RequestEmailChange to a taken address = %v, want nil", err)
	}

	if n := f.count(mail.TemplateChangeEmail); n != 0 {
		t.Errorf("%d change mails sent, want none", n)
	}
}

func TestChangeEmailLinks(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		spoil   func(f *authFixture)
		wantErr error
	}{
		{"expired", func(f *authFixture) { f.clock.advance(service.ChangeEmailTokenTTL) }, service.ErrInvalidToken},
		{"address taken meanwhile", func(f *authFixture) {
			f.signedUp("ada@example.net", domain.RoleMember)
		}, service.ErrEmailTaken},
		{"account disabled", func(f *authFixture) {
			f.update("ada@example.org", func(a *domain.Account) { a.DisabledAt = f.clock.Now() })
		}, service.ErrInvalidToken},
		// Whoever requested the change may have known the old password.
		{"password reset", func(f *authFixture) {
			if err := f.svc.RequestPasswordReset(f.t.Context(), "ada@example.org", ""); err != nil {
				f.t.Fatal(err)
			}

			reset, _ := f.link(mail.TemplateResetPassword, "reset-password")
			if err := f.svc.ResetPassword(f.t.Context(), reset, "battery staple"); err != nil {
				f.t.Fatal(err)
			}
		}, service.ErrInvalidToken},
		{"password changed", func(f *authFixture) {
			p := f.login("ada@example.org", "correct horse").Principal
			if err := f.svc.ChangePassword(f.t.Context(), p, "correct horse", "battery staple"); err != nil {
				f.t.Fatal(err)
			}
		}, service.ErrInvalidToken},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := newAuthFixture(t)
			s := f.signedUp("ada@example.org", domain.RoleMember)

			if err := f.svc.RequestEmailChange(t.Context(), s.Principal, "correct horse", "ada@example.net", ""); err != nil {
				t.Fatal(err)
			}

			token, _ := f.link(mail.TemplateChangeEmail, "confirm-email-change")
			tt.spoil(f)
			wantIs(t, f.svc.ConfirmEmailChange(t.Context(), token, ""), tt.wantErr)

			if got := f.account("ada@example.org").Email; got != "ada@example.org" {
				t.Errorf("email = %q, want it unchanged", got)
			}
		})
	}
}

func TestChangeEmailEndsLinksToTheOldAddress(t *testing.T) {
	t.Parallel()

	f := newAuthFixture(t)
	s := f.signedUp("ada@example.org", domain.RoleMember)

	if err := f.svc.RequestPasswordReset(t.Context(), "ada@example.org", ""); err != nil {
		t.Fatal(err)
	}

	reset, _ := f.link(mail.TemplateResetPassword, "reset-password")

	if err := f.svc.RequestEmailChange(t.Context(), s.Principal, "correct horse", "ada@example.net", ""); err != nil {
		t.Fatal(err)
	}

	token, _ := f.link(mail.TemplateChangeEmail, "confirm-email-change")
	if err := f.svc.ConfirmEmailChange(t.Context(), token, ""); err != nil {
		t.Fatal(err)
	}

	wantIs(t, f.svc.ResetPassword(t.Context(), reset, "battery staple"), service.ErrInvalidToken)
}

// TestConfirmEmailChangeInvalidAddress checks that an unusable address in a
// link is not reported on a field: the confirmation request has none.
func TestConfirmEmailChangeInvalidAddress(t *testing.T) {
	t.Parallel()

	f := newAuthFixture(t)
	f.signedUp("ada@example.org", domain.RoleMember)
	acc := f.account("ada@example.org")
	now := f.clock.Now()

	err := f.st.InTx(t.Context(), store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		return tx.AccountTokens().Create(ctx, domain.AccountToken{
			Hash: domain.HashToken("bad-address"), AccountID: acc.ID, Purpose: domain.TokenChangeEmail,
			Email: "not an email", CreatedAt: now, ExpiresAt: now.Add(time.Hour),
		})
	})
	if err != nil {
		t.Fatal(err)
	}

	err = f.svc.ConfirmEmailChange(t.Context(), "bad-address", "")
	wantIs(t, err, domain.ErrValidation)

	var fe *domain.FieldError
	if errors.As(err, &fe) {
		t.Errorf("error on field %q, want none", fe.Field)
	}

	if got := f.account("ada@example.org").Email; got != "ada@example.org" {
		t.Errorf("email = %q, want it unchanged", got)
	}
}
