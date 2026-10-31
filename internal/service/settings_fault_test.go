package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/seed"
	"github.com/cwbudde/studio-schedule/internal/service"
	"github.com/cwbudde/studio-schedule/internal/store"
	"github.com/cwbudde/studio-schedule/internal/store/memory"
)

// faultySettings returns the SettingsService on a FaultStore over a store
// with the demo settings, a logo and a background, so that every card and
// image read reaches the store.
func faultySettings(t *testing.T) (*service.FaultStore, *service.SettingsService) {
	t.Helper()

	st := memory.New()

	err := st.InTx(t.Context(), store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		if err := tx.Settings().Put(ctx, seed.DemoSettings()); err != nil {
			return err
		}

		if err := tx.Settings().PutLogo(ctx, domain.Logo{ContentType: domain.LogoPNG, Data: []byte("\x89PNG\r\n\x1a\nlogo")}); err != nil {
			return err
		}

		return tx.Settings().PutBackground(ctx, domain.Background{
			ContentType: domain.BackgroundPNG, Data: []byte("\x89PNG\r\n\x1a\nbackground"),
		})
	})
	if err != nil {
		t.Fatal(err)
	}

	fs := service.NewFaultStore(st)

	return fs, service.NewSettingsService(fs, clock.NewFixed(settingsNow), ids.NewSequence())
}

// TestSettingsStoreFaults: every settings read and write returns a store
// failure, and a refused write changes nothing.
func TestSettingsStoreFaults(t *testing.T) {
	t.Parallel()

	hasNone := func(string) bool { return false }
	demo := seed.DemoSettings()

	for name, op := range map[string]func(t *testing.T, svc *service.SettingsService) error{
		"Public": func(t *testing.T, svc *service.SettingsService) error {
			_, err := svc.Public(t.Context())

			return err
		},
		"Logo": func(t *testing.T, svc *service.SettingsService) error {
			_, err := svc.Logo(t.Context(), hasNone)

			return err
		},
		"Background": func(t *testing.T, svc *service.SettingsService) error {
			_, err := svc.Background(t.Context(), hasNone)

			return err
		},
		"Branding": func(t *testing.T, svc *service.SettingsService) error {
			_, err := svc.Branding(t.Context())

			return err
		},
		"Regional": func(t *testing.T, svc *service.SettingsService) error {
			_, err := svc.Regional(t.Context())

			return err
		},
		"BookingRules": func(t *testing.T, svc *service.SettingsService) error {
			_, err := svc.BookingRules(t.Context())

			return err
		},
		"PreviewRelease": func(t *testing.T, svc *service.SettingsService) error {
			_, err := svc.PreviewRelease(t.Context(), service.ReleaseRule{
				Weekday: time.Sunday, Time: domain.TimeOfDay{Hour: 18}, WeeksAhead: 1,
			})

			return err
		},
		"PutBackground": func(t *testing.T, svc *service.SettingsService) error {
			return svc.PutBackground(t.Context(), domain.Background{})
		},
		"SetSchool": func(t *testing.T, svc *service.SettingsService) error {
			_, err := svc.SetSchool(t.Context(), service.SchoolProfile{Name: "Studio Elsewhere"})

			return err
		},
		"SetBranding": func(t *testing.T, svc *service.SettingsService) error {
			_, err := svc.SetBranding(t.Context(), service.Branding{})

			return err
		},
		"SetRegional": func(t *testing.T, svc *service.SettingsService) error {
			_, err := svc.SetRegional(t.Context(), service.Regional{
				Timezone: "Europe/Vienna", DefaultLocale: "de", Currency: demo.Currency,
			})

			return err
		},
		// Narrower: nothing to generate, so one transaction (a failing
		// generation is no error, TestSettingsSetBookingRulesGenerationFails).
		"SetBookingRules": func(t *testing.T, svc *service.SettingsService) error {
			_, err := svc.SetBookingRules(t.Context(), service.BookingRules{
				ReleaseWeekday: demo.ReleaseWeekday, ReleaseTime: demo.ReleaseTime, ReleaseWeeksAhead: demo.ReleaseWeeksAhead,
				BookingCutoff: 30 * time.Minute, CancellationCutoff: demo.CancellationCutoff,
				WaitlistInviteExpiry: demo.WaitlistInviteExpiry, DefaultVisibleDays: demo.DefaultVisibleDays,
			})

			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			service.SweepFaults(t, service.FaultSweep{Setup: func(t *testing.T) (*service.FaultStore, func() error) {
				fs, svc := faultySettings(t)

				return fs, func() error { return op(t, svc) }
			}})
		})
	}
}
