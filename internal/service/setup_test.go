package service_test

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"

	"github.com/cwbudde/studio-schedule/internal/auth/password"
	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/mail"
	"github.com/cwbudde/studio-schedule/internal/service"
	"github.com/cwbudde/studio-schedule/internal/store"
	"github.com/cwbudde/studio-schedule/internal/store/memory"
)

func setupData() service.SetupData {
	return service.SetupData{
		SchoolName: "Fictional Dance Hall", Timezone: "Europe/Vienna", Locale: "de", Currency: "eur",
		AdminEmail: "admin@fictional-dance.example", AdminGivenName: "Ada", AdminFamilyName: "Example",
	}
}

func TestSetupInit(t *testing.T) {
	t.Parallel()

	st := memory.New()
	clk := clock.NewFixed(settingsNow)
	svc := service.NewSetupService(st, clk, ids.NewSequence(), "https://schedule.example.test/")

	data := setupData()
	data.Locale = "de-AT" // stored as its locale

	res, err := svc.Init(t.Context(), data)
	if err != nil {
		t.Fatal(err)
	}

	if res.Account.Role != domain.RoleAdmin || !res.Account.ConfirmedAt.IsZero() || res.Account.Locale != "de" {
		t.Fatalf("account %+v: want an unconfirmed admin with the school's language", res.Account)
	}

	if !res.ExpiresAt.Equal(settingsNow.Add(service.InviteTokenTTL)) {
		t.Fatalf("expires %v, want 72 h after %v", res.ExpiresAt, settingsNow)
	}

	prefix := "https://schedule.example.test/reset-password?token="
	if !strings.HasPrefix(res.Link, prefix) {
		t.Fatalf("link %q", res.Link)
	}

	token, err := url.QueryUnescape(strings.TrimPrefix(res.Link, prefix))
	if err != nil {
		t.Fatal(err)
	}

	settings, err := service.NewSettingsService(st, clk, ids.V7{}).School(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	want := service.DefaultSettings()
	want.SchoolName, want.Timezone, want.DefaultLocale, want.Currency = "Fictional Dance Hall", "Europe/Vienna", "de", "EUR"

	if settings.Settings != want {
		t.Fatalf("settings %+v, want %+v", settings.Settings, want)
	}

	// No mail: the link is the only way in.
	err = st.InTx(t.Context(), store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		mails, err := tx.Outbox().List(ctx, store.OutboxFilter{Limit: 10})
		if err != nil || len(mails) != 0 {
			t.Errorf("outbox %v, %v; want empty", mails, err)
		}

		tok, err := tx.AccountTokens().Get(ctx, domain.HashToken(token))
		if err != nil || tok.AccountID != res.Account.ID || tok.Purpose != domain.TokenResetPassword {
			t.Errorf("token %+v, %v", tok, err)
		}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// The link sets the password, which confirms the account.
	hasher, err := password.NewWithParams(cheapParams, nil)
	if err != nil {
		t.Fatal(err)
	}

	renderer, err := mail.NewRenderer()
	if err != nil {
		t.Fatal(err)
	}

	auth := service.NewAuthService(st, clk, ids.V7{}, hasher, renderer, "https://schedule.example.test/")
	if err := auth.ResetPassword(t.Context(), token, "a long fictional password"); err != nil {
		t.Fatal(err)
	}

	if _, err := auth.Login(t.Context(), "admin@fictional-dance.example", "a long fictional password"); err != nil {
		t.Fatalf("login after setup: %v", err)
	}

	// Once is enough.
	if _, err := svc.Init(t.Context(), setupData()); !errors.Is(err, service.ErrAlreadySetUp) {
		t.Fatalf("second init: want ErrAlreadySetUp, got %v", err)
	}
}

func TestSetupInitInvalid(t *testing.T) {
	t.Parallel()

	for field, change := range map[string]func(*service.SetupData){
		"schoolName":    func(d *service.SetupData) { d.SchoolName = " " },
		"timezone":      func(d *service.SetupData) { d.Timezone = "Nowhere/Town" },
		"defaultLocale": func(d *service.SetupData) { d.Locale = "fr" },
		"currency":      func(d *service.SetupData) { d.Currency = "EURO" },
		"email":         func(d *service.SetupData) { d.AdminEmail = "not an address" },
		"givenName":     func(d *service.SetupData) { d.AdminGivenName = "" },
		"familyName":    func(d *service.SetupData) { d.AdminFamilyName = "" },
	} {
		t.Run(field, func(t *testing.T) {
			t.Parallel()

			st := memory.New()
			d := setupData()
			change(&d)

			_, err := service.NewSetupService(st, clock.NewFixed(settingsNow), ids.NewSequence(), "/").Init(t.Context(), d)
			wantField(t, err, field)

			// Nothing was stored.
			err = st.InTx(t.Context(), store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
				_, err := tx.Settings().Get(ctx)

				return err
			})
			if !errors.Is(err, domain.ErrNotFound) {
				t.Fatalf("settings stored: %v", err)
			}
		})
	}
}
