package service_test

import (
	"errors"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/seed"
	"github.com/cwbudde/studio-schedule/internal/service"
	"github.com/cwbudde/studio-schedule/internal/store/memory"
)

// importAll is an import of every area, each changed from the demo
// settings, with texts to trim.
func importAll() service.SettingsImport {
	theme, mode := domain.ThemeWerkstatt, domain.RedeemOnBooking

	return service.SettingsImport{
		School:   &service.SchoolProfile{Name: "  Studio Elsewhere ", LegalNotice: "Example Street 1"},
		Branding: &service.Branding{AccentColor: "#1F5FA8", Theme: &theme},
		Regional: &service.Regional{Timezone: "America/New_York", DefaultLocale: "de-AT", Currency: "USD"},
		BookingRules: &service.BookingRules{
			ReleaseWeekday: time.Monday, ReleaseTime: domain.TimeOfDay{Hour: 9}, ReleaseWeeksAhead: 1,
			CancellationCutoff: time.Hour, WaitlistInviteExpiry: 30 * time.Minute, DefaultVisibleDays: 7,
			ReleasedThrough: domain.Date{Year: 2026, Month: 11, Day: 30},
		},
		Receipts: &service.ReceiptSettingsUpdate{
			Settings: domain.ReceiptSettings{
				Issuer: domain.ReceiptIssuer{
					Name: " Studio Elsewhere ", Address: " Example Street 1 \n\n 12345 Exampletown ",
					TaxID: " 11/111/11111 ", Email: " office@elsewhere.example.org ", Phone: " +1 000 000 0000 ",
				},
				TaxMode: domain.TaxExempt, TaxNote: " Exempt by example law ", Footer: " Thank you \n",
			},
			Prefix: " SE ",
		},
		RedemptionMode: &mode,
	}
}

// TestSettingsImport writes every area in one go, as the setters would:
// trimmed, the accent in lower case, the language matched. The dry run
// returns the same result and writes nothing.
func TestSettingsImport(t *testing.T) {
	t.Parallel()

	st, svc := newSettings(t)
	demo := seed.DemoSettings()

	want := demo
	want.SchoolName, want.LegalNotice = "Studio Elsewhere", "Example Street 1"
	want.AccentColor, want.Theme = "#1f5fa8", domain.ThemeWerkstatt
	want.Timezone, want.DefaultLocale, want.Currency = "America/New_York", "de", "USD"
	want.ReleaseWeekday, want.ReleaseTime, want.ReleaseWeeksAhead = time.Monday, domain.TimeOfDay{Hour: 9}, 1
	want.BookingCutoff, want.CancellationCutoff, want.WaitlistInviteExpiry = 0, time.Hour, 30*time.Minute
	want.DefaultVisibleDays, want.ReleasedThrough = 7, domain.Date{Year: 2026, Month: 11, Day: 30}
	want.Receipts = domain.ReceiptSettings{
		Issuer: domain.ReceiptIssuer{
			Name: "Studio Elsewhere", Address: "Example Street 1\n12345 Exampletown", TaxID: "11/111/11111",
			Email: "office@elsewhere.example.org", Phone: "+1 000 000 0000",
		},
		TaxMode: domain.TaxExempt, TaxNote: "Exempt by example law", Footer: "Thank you",
	}
	want.ReceiptPrefix, want.RedemptionMode = "SE", domain.RedeemOnBooking

	dry, err := svc.Import(t.Context(), importAll(), true)
	if err != nil {
		t.Fatal(err)
	}

	if dry.Before != demo || dry.After != want || dry.Generated != 0 {
		t.Errorf("dry run = %+v, want after\n%+v", dry, want)
	}

	if got, _ := svc.Export(t.Context()); got != demo {
		t.Errorf("the dry run wrote: %+v", got)
	}

	res, err := svc.Import(t.Context(), importAll(), false)
	if err != nil || res.After != want || res.GenerateErr != nil {
		t.Fatalf("import = %+v, %v", res, err)
	}

	if got, _ := service.NewSettingsService(st, clock.NewFixed(settingsNow), ids.V7{}).Export(t.Context()); got != want {
		t.Errorf("stored\n%+v\nwant\n%+v", got, want)
	}

	// Nothing to import keeps everything.
	if res, err := svc.Import(t.Context(), service.SettingsImport{}, false); err != nil || res.After != want {
		t.Errorf("empty import = %+v, %v", res, err)
	}
}

// TestSettingsImportAllOrNothing: one refused area writes none: a locked
// time zone or currency is its conflict, an invalid value its field error.
func TestSettingsImportAllOrNothing(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		change func(*service.SettingsImport)
		want   error
		field  string
	}{
		"time zone": {
			change: func(in *service.SettingsImport) { in.Regional.Currency = "EUR" },
			want:   service.ErrTimezoneLocked,
		},
		"currency": {
			change: func(in *service.SettingsImport) { in.Regional.Timezone = "Europe/Berlin" },
			want:   service.ErrCurrencyLocked,
		},
		"cutoff": {
			change: func(in *service.SettingsImport) {
				in.Regional = nil
				in.BookingRules.BookingCutoff = 31 * 24 * time.Hour
			},
			want: domain.ErrValidation, field: "bookingCutoff",
		},
		"released through": {
			change: func(in *service.SettingsImport) {
				in.Regional = nil
				in.BookingRules.ReleasedThrough = domain.Date{Year: 2028, Month: 1, Day: 1}
			},
			want: domain.ErrValidation, field: "releasedThrough",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, svc := newDemoSettings(t)
			before, _ := svc.Export(t.Context())

			in := importAll()
			tc.change(&in)

			for _, dryRun := range []bool{true, false} {
				_, err := svc.Import(t.Context(), in, dryRun)

				var fe *domain.FieldError
				if !errors.Is(err, tc.want) || (tc.field != "" && (!errors.As(err, &fe) || fe.Field != tc.field)) {
					t.Errorf("dry run %t: %v, want %v on %q", dryRun, err, tc.want, tc.field)
				}
			}

			if after, _ := svc.Export(t.Context()); after != before {
				t.Errorf("a refused import wrote: %+v", after)
			}
		})
	}
}

// TestSettingsImportGenerates: booking rules that widen the window
// generate its lessons after the import, never in a dry run.
func TestSettingsImportGenerates(t *testing.T) {
	t.Parallel()

	_, svc := newDemoSettings(t)
	demo, _ := svc.Export(t.Context())
	rules := &service.BookingRules{
		ReleaseWeekday: demo.ReleaseWeekday, ReleaseTime: demo.ReleaseTime, ReleaseWeeksAhead: demo.ReleaseWeeksAhead + 2,
		BookingCutoff: demo.BookingCutoff, CancellationCutoff: demo.CancellationCutoff,
		WaitlistInviteExpiry: demo.WaitlistInviteExpiry, DefaultVisibleDays: demo.DefaultVisibleDays,
	}

	dry, err := svc.Import(t.Context(), service.SettingsImport{BookingRules: rules}, true)
	if err != nil || dry.Generated != 0 || dry.After.ReleaseWeeksAhead != demo.ReleaseWeeksAhead+2 {
		t.Errorf("dry run = %+v, %v", dry, err)
	}

	res, err := svc.Import(t.Context(), service.SettingsImport{BookingRules: rules}, false)
	if err != nil || res.Generated == 0 || res.GenerateErr != nil {
		t.Errorf("import = %d lessons, %v, %v; want lessons generated", res.Generated, res.GenerateErr, err)
	}
}

// TestSettingsExportWithoutSchool: no settings is ErrNoSchool for both.
func TestSettingsExportWithoutSchool(t *testing.T) {
	t.Parallel()

	svc := service.NewSettingsService(memory.New(), clock.NewFixed(settingsNow), ids.V7{})

	if _, err := svc.Export(t.Context()); !errors.Is(err, service.ErrNoSchool) {
		t.Errorf("Export: %v, want ErrNoSchool", err)
	}

	if _, err := svc.Import(t.Context(), importAll(), false); !errors.Is(err, service.ErrNoSchool) {
		t.Errorf("Import: %v, want ErrNoSchool", err)
	}
}

// TestSettingsImportStoreFaults: every store failure of Export and Import
// is returned, and a refused import changes nothing.
func TestSettingsImportStoreFaults(t *testing.T) {
	t.Parallel()

	for name, op := range map[string]func(t *testing.T, svc *service.SettingsService) error{
		"Export": func(t *testing.T, svc *service.SettingsService) error {
			_, err := svc.Export(t.Context())

			return err
		},
		// Without booking rules: nothing to generate, so one transaction.
		"Import": func(t *testing.T, svc *service.SettingsService) error {
			in := importAll()
			in.BookingRules = nil

			_, err := svc.Import(t.Context(), in, false)

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
