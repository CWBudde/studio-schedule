package service_test

import (
	"strings"
	"testing"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/mail"
	"github.com/cwbudde/studio-schedule/internal/service"
)

func TestAccountLocale(t *testing.T) {
	t.Parallel()

	register := func(f *authFixture, email, locale string) {
		t.Helper()

		err := f.svc.Register(t.Context(), service.Registration{
			GivenName: "Ada", FamilyName: "Lovelace", Email: email, Password: "correct horse", Locale: locale,
		})
		if err != nil {
			t.Fatalf("Register(%s, %s): %v", email, locale, err)
		}
	}

	t.Run("registration stores the matching template locale", func(t *testing.T) {
		t.Parallel()

		f := newAuthFixture(t)

		for _, tt := range []struct{ email, locale, want string }{
			{"a@example.org", "de-AT", "de"},
			{"b@example.org", "fr", ""}, // no template: the school's default applies
			{"c@example.org", "", ""},
		} {
			register(f, tt.email, tt.locale)

			if got := f.account(tt.email).Locale; got != tt.want {
				t.Errorf("registered with %q: locale %q, want %q", tt.locale, got, tt.want)
			}
		}
	})

	t.Run("a repeated registration replaces it", func(t *testing.T) {
		t.Parallel()

		f := newAuthFixture(t)
		register(f, "ada@example.org", "de")
		register(f, "ada@example.org", "en")

		if got := f.account("ada@example.org").Locale; got != "en" {
			t.Errorf("locale %q, want en", got)
		}

		register(f, "ada@example.org", "") // no language: keep it

		if got := f.account("ada@example.org").Locale; got != "en" {
			t.Errorf("locale %q after a registration without one, want en", got)
		}
	})

	t.Run("SetLocale", func(t *testing.T) {
		t.Parallel()

		f := newAuthFixture(t)
		s := f.signedUp("ada@example.org", domain.RoleMember)

		for _, tt := range []struct{ in, want string }{{"DE", "de"}, {"en-GB", "en"}, {"", ""}} {
			if err := f.svc.SetLocale(t.Context(), s.Principal, tt.in); err != nil {
				t.Fatalf("SetLocale(%q): %v", tt.in, err)
			}

			if got := f.account("ada@example.org").Locale; got != tt.want {
				t.Errorf("SetLocale(%q): locale %q, want %q", tt.in, got, tt.want)
			}
		}

		wantIs(t, f.svc.SetLocale(t.Context(), s.Principal, "fr"), domain.ErrValidation)
	})

	t.Run("mails to the account use it", func(t *testing.T) {
		t.Parallel()

		f := newAuthFixture(t)
		s := f.signedUp("ada@example.org", domain.RoleMember)

		if err := f.svc.SetLocale(t.Context(), s.Principal, "de"); err != nil {
			t.Fatal(err)
		}

		if err := f.svc.RequestPasswordReset(t.Context(), "ada@example.org", "en"); err != nil {
			t.Fatal(err)
		}

		if m := f.mails()[0]; m.Template != mail.TemplateResetPassword || !strings.Contains(m.Subject, "Passwort") {
			t.Errorf("reset mail %s %q, want it in German", m.Template, m.Subject)
		}
	})
}
