package service_test

import (
	"context"
	"errors"
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

// settingsNow is a Wednesday in the demo term, 12:00 in Berlin.
var settingsNow = time.Date(2026, time.October, 21, 10, 0, 0, 0, time.UTC)

// newSettings returns a store with the demo settings only (nothing that
// locks the time zone or the currency) and its SettingsService.
func newSettings(t *testing.T) (*memory.Store, *service.SettingsService) {
	t.Helper()

	st := memory.New()

	err := st.InTx(t.Context(), store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		return tx.Settings().Put(ctx, seed.DemoSettings())
	})
	if err != nil {
		t.Fatal(err)
	}

	return st, service.NewSettingsService(st, clock.NewFixed(settingsNow), ids.V7{})
}

// newDemoSettings returns the SettingsService of the demo school, whose
// lessons, prices and receipts lock the time zone and the currency.
func newDemoSettings(t *testing.T) (*memory.Store, *service.SettingsService) {
	t.Helper()

	st := memory.New()
	if err := seed.LoadDemo(t.Context(), st); err != nil {
		t.Fatal(err)
	}

	return st, service.NewSettingsService(st, clock.NewFixed(settingsNow), ids.V7{})
}

func TestSettingsPublic(t *testing.T) {
	t.Parallel()

	st := memory.New()
	svc := service.NewSettingsService(st, clock.NewFixed(settingsNow), ids.V7{})

	if _, err := svc.Public(t.Context()); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("empty store: want ErrNotFound, got %v", err)
	}

	want := seed.DemoSettings()

	err := st.InTx(t.Context(), store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		return tx.Settings().Put(ctx, want)
	})
	if err != nil {
		t.Fatal(err)
	}

	got, err := svc.Public(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	if got.Settings != want || !got.Logo.IsZero() {
		t.Fatalf("Public() = %+v, want %+v without a logo", got, want)
	}

	logo := domain.Logo{ContentType: domain.LogoPNG, Data: []byte("\x89PNG\r\n\x1a\nlogo")}

	err = st.InTx(t.Context(), store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		return tx.Settings().PutLogo(ctx, logo)
	})
	if err != nil {
		t.Fatal(err)
	}

	if got, err = svc.Public(t.Context()); err != nil ||
		got.Logo != (domain.LogoInfo{ContentType: domain.LogoPNG, Digest: logo.Digest()}) {
		t.Fatalf("Public() with a logo = %+v, %v", got, err)
	}
}

func TestSettingsLogo(t *testing.T) {
	t.Parallel()

	st, svc := newSettings(t)
	asked := ""
	hasNone := func(digest string) bool { asked = digest; return false }

	if l, err := svc.Logo(t.Context(), hasNone); err != nil || l.Digest != "" || !l.Logo.IsZero() {
		t.Fatalf("without a logo: %+v, %v", l, err)
	}

	logo := domain.Logo{ContentType: domain.LogoPNG, Data: []byte("\x89PNG\r\n\x1a\nlogo")}

	err := st.InTx(t.Context(), store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		return tx.Settings().PutLogo(ctx, logo)
	})
	if err != nil {
		t.Fatal(err)
	}

	l, err := svc.Logo(t.Context(), hasNone)
	if err != nil || l.Digest != logo.Digest() || asked != logo.Digest() || string(l.Logo.Data) != string(logo.Data) {
		t.Fatalf("a new client: %+v, %v (asked about %q)", l, err, asked)
	}

	// A client with this logo gets its digest only.
	l, err = svc.Logo(t.Context(), func(digest string) bool { return digest == logo.Digest() })
	if err != nil || l.Digest != logo.Digest() || !l.Logo.IsZero() {
		t.Fatalf("a client with the logo: %+v, %v", l, err)
	}
}

// Settings stored before ADR 0013 may name any language tag. Each card
// still saves, and the default language becomes its locale.
func TestSettingsLegacyLocale(t *testing.T) {
	t.Parallel()

	for stored, want := range map[string]string{"de-DE": "de", "en_GB": "en", "fr": "en"} {
		st, svc := newSettings(t)

		err := st.InTx(t.Context(), store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
			s, err := tx.Settings().Get(ctx)
			s.DefaultLocale = stored

			return errors.Join(err, tx.Settings().Put(ctx, s))
		})
		if err != nil {
			t.Fatal(err)
		}

		if p, err := svc.Public(t.Context()); err != nil || p.DefaultLocale != want {
			t.Errorf("%s: public locale %q, %v; want %q", stored, p.DefaultLocale, err, want)
		}

		if v, err := svc.Regional(t.Context()); err != nil || v.DefaultLocale != want {
			t.Errorf("%s: regional locale %q, %v; want %q", stored, v.DefaultLocale, err, want)
		}

		if _, err := svc.SetSchool(t.Context(), service.SchoolProfile{Name: "Fictional Ballet Academy"}); err != nil {
			t.Fatalf("%s: saving the school profile: %v", stored, err)
		}

		var got string

		err = st.InTx(t.Context(), store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
			s, err := tx.Settings().Get(ctx)
			got = s.DefaultLocale

			return err
		})
		if err != nil || got != want {
			t.Errorf("%s: stored %q, %v; want %q", stored, got, err, want)
		}
	}
}

func TestSettingsGetAndPreview(t *testing.T) {
	t.Parallel()

	_, svc := newDemoSettings(t)

	v, err := svc.BookingRules(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	berlin, _ := time.LoadLocation("Europe/Berlin")
	// Sunday 18:00 releases one week ahead: the release of Sunday the 18th
	// opens everything that starts before Monday the 26th.
	if want := time.Date(2026, time.October, 18, 18, 0, 0, 0, berlin); !v.Window.LastRelease.Equal(want) {
		t.Errorf("last release %v, want %v", v.Window.LastRelease, want)
	}

	if want := time.Date(2026, time.October, 26, 0, 0, 0, 0, berlin); !v.Window.ReleasedUntil.Equal(want) {
		t.Errorf("released until %v, want %v", v.Window.ReleasedUntil, want)
	}

	if !v.Now.Equal(settingsNow) {
		t.Errorf("window computed at %v, want %v", v.Now, settingsNow)
	}

	if r, err := svc.Regional(t.Context()); err != nil || !r.TimezoneLocked() || !r.CurrencyLocked() {
		t.Errorf("regional = %+v, %v; want both locked", r, err)
	}

	// Each card reads only what it shows.
	if s, err := svc.School(t.Context()); err != nil || s.Usage != (store.SettingsUsage{}) || !s.Now.IsZero() {
		t.Errorf("school = %+v, %v; want neither usage nor window", s, err)
	}

	p, err := svc.PreviewRelease(t.Context(), time.Friday, domain.TimeOfDay{Hour: 9}, 3)
	if err != nil {
		t.Fatal(err)
	}

	if want := time.Date(2026, time.October, 16, 9, 0, 0, 0, berlin); !p.Window.LastRelease.Equal(want) {
		t.Errorf("preview: last release %v, want %v", p.Window.LastRelease, want)
	}

	if want := time.Date(2026, time.November, 7, 0, 0, 0, 0, berlin); !p.Window.ReleasedUntil.Equal(want) {
		t.Errorf("preview: released until %v, want %v", p.Window.ReleasedUntil, want)
	}

	// The preview stores nothing.
	if again, _ := svc.BookingRules(t.Context()); again.ReleaseWeekday != time.Sunday {
		t.Errorf("the preview changed the settings: %+v", again.Settings)
	}

	_, err = svc.PreviewRelease(t.Context(), time.Friday, domain.TimeOfDay{Hour: 9}, 53)
	wantField(t, err, "releaseWeeksAhead")

	_, err = svc.PreviewRelease(t.Context(), time.Weekday(7), domain.TimeOfDay{Hour: 9}, 2)
	wantField(t, err, "releaseWeekday")
}

// The preview checks the rule only: a stored setting that fails today's
// rules does not break it.
func TestSettingsPreviewIgnoresOtherSettings(t *testing.T) {
	t.Parallel()

	st, svc := newSettings(t)

	err := st.InTx(t.Context(), store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		s, err := tx.Settings().Get(ctx)
		s.AccentColor = "#ff0000" // too light, refused since ADR 0013

		return errors.Join(err, tx.Settings().Put(ctx, s))
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := svc.PreviewRelease(t.Context(), time.Friday, domain.TimeOfDay{Hour: 9}, 3); err != nil {
		t.Fatalf("preview with a stored invalid accent: %v", err)
	}
}

func TestSettingsSetSchoolAndBranding(t *testing.T) {
	t.Parallel()

	_, svc := newSettings(t)

	v, err := svc.SetSchool(t.Context(), service.SchoolProfile{
		Name: "  Fictional Ballet Academy ", LegalNotice: " Fictional Ballet Academy · Example Road 1 ",
	})
	if err != nil {
		t.Fatal(err)
	}

	if v.SchoolName != "Fictional Ballet Academy" || v.LegalNotice != "Fictional Ballet Academy · Example Road 1" {
		t.Fatalf("got %q, %q", v.SchoolName, v.LegalNotice)
	}

	_, err = svc.SetSchool(t.Context(), service.SchoolProfile{Name: " "})
	wantField(t, err, "schoolName")

	if v, err = svc.SetBranding(t.Context(), "#1D4ED8"); err != nil {
		t.Fatal(err)
	}

	if v.AccentColor != "#1d4ed8" || v.AccentDark() != "#97b7f8" {
		t.Fatalf("accent %q, dark %q", v.AccentColor, v.AccentDark())
	}

	// Too light to carry the light text on it.
	for _, bad := range []string{"#ff0000", "#808080", "blue", "#fff"} {
		_, err = svc.SetBranding(t.Context(), bad)
		wantField(t, err, "accentColor")
	}

	if v, err = svc.SetBranding(t.Context(), ""); err != nil || v.AccentColor != "" || v.AccentDark() != "" {
		t.Fatalf("removing the accent: %q, %v", v.AccentColor, err)
	}

	// The other cards' settings stay.
	if v.SchoolName != "Fictional Ballet Academy" {
		t.Fatalf("school name lost: %q", v.SchoolName)
	}
}

func TestSettingsSetRegional(t *testing.T) {
	t.Parallel()

	t.Run("BeforeLessonsAndPrices", func(t *testing.T) {
		t.Parallel()

		st, svc := newSettings(t)

		v, err := svc.SetRegional(t.Context(), service.Regional{
			Timezone: "America/New_York", DefaultLocale: "de", Currency: "USD",
		})
		if err != nil {
			t.Fatal(err)
		}

		if v.Timezone != "America/New_York" || v.DefaultLocale != "de" || v.Currency != "USD" {
			t.Fatalf("got %+v", v.Settings)
		}

		// A language tag is stored as its locale.
		if v, err = svc.SetRegional(t.Context(), service.Regional{
			Timezone: "America/New_York", DefaultLocale: "en-US", Currency: "USD",
		}); err != nil || v.DefaultLocale != "en" {
			t.Fatalf("en-US: got %q, %v", v.DefaultLocale, err)
		}

		for field, r := range map[string]service.Regional{
			"timezone":      {Timezone: "Mars/Olympus", DefaultLocale: "de", Currency: "USD"},
			"defaultLocale": {Timezone: "America/New_York", DefaultLocale: "fr", Currency: "USD"},
			"currency":      {Timezone: "America/New_York", DefaultLocale: "de", Currency: "XYZ"},
		} {
			_, err := svc.SetRegional(t.Context(), r)
			wantField(t, err, field)
		}

		// A priced pass type fixes the currency, but not the time zone.
		err = st.InTx(t.Context(), store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
			return tx.PassTypes().Create(ctx, domain.PassType{
				ID: ids.New[domain.PassTypeID](ids.V7{}), Name: "Single visit", Kind: domain.PassVisits, Visits: 1,
				Price: 1500, Active: true, CreatedAt: settingsNow,
			})
		})
		if err != nil {
			t.Fatal(err)
		}

		_, err = svc.SetRegional(t.Context(), service.Regional{
			Timezone: "America/New_York", DefaultLocale: "de", Currency: "EUR",
		})
		if !errors.Is(err, service.ErrCurrencyLocked) {
			t.Fatalf("want ErrCurrencyLocked, got %v", err)
		}

		if v, err = svc.SetRegional(t.Context(), service.Regional{
			Timezone: "Europe/Vienna", DefaultLocale: "en", Currency: "USD",
		}); err != nil || v.Timezone != "Europe/Vienna" || !v.CurrencyLocked() || v.TimezoneLocked() {
			t.Fatalf("got %+v, %v", v, err)
		}
	})

	t.Run("AfterLessons", func(t *testing.T) {
		t.Parallel()

		_, svc := newDemoSettings(t)

		_, err := svc.SetRegional(t.Context(), service.Regional{
			Timezone: "Europe/Vienna", DefaultLocale: "en", Currency: "EUR",
		})
		if !errors.Is(err, service.ErrTimezoneLocked) {
			t.Fatalf("want ErrTimezoneLocked, got %v", err)
		}

		// The language may change; the locked values may be sent as they are.
		v, err := svc.SetRegional(t.Context(), service.Regional{
			Timezone: "Europe/Berlin", DefaultLocale: "de", Currency: "EUR",
		})
		if err != nil || v.DefaultLocale != "de" {
			t.Fatalf("got %+v, %v", v.Settings, err)
		}
	})
}

func TestSettingsSetBookingRules(t *testing.T) {
	t.Parallel()

	st, svc := newDemoSettings(t)
	ctx := t.Context()

	if _, err := service.NewLessonGenerator(st, clock.NewFixed(settingsNow), ids.V7{}).GenerateAhead(ctx); err != nil {
		t.Fatal(err)
	}

	rules := service.BookingRules{
		ReleaseWeekday: time.Friday, ReleaseTime: domain.TimeOfDay{Hour: 9}, ReleaseWeeksAhead: 1,
		BookingCutoff: 30 * time.Minute, CancellationCutoff: 24 * time.Hour,
		WaitlistInviteExpiry: 6 * time.Hour, DefaultVisibleDays: 14,
	}

	// The same window: nothing to generate.
	v, err := svc.SetBookingRules(ctx, rules)
	if err != nil {
		t.Fatal(err)
	}

	if v.Generated != 0 || v.ReleaseWeekday != time.Friday || v.CancellationCutoff != 24*time.Hour ||
		v.WaitlistInviteExpiry != 6*time.Hour || v.BookingCutoff != 30*time.Minute {
		t.Fatalf("got %+v", v)
	}

	// Ten weeks ahead reach beyond the generated horizon.
	rules.ReleaseWeeksAhead = 10

	if v, err = svc.SetBookingRules(ctx, rules); err != nil {
		t.Fatal(err)
	}

	if v.Generated == 0 {
		t.Fatal("a wider window generated no lessons")
	}

	berlin, _ := time.LoadLocation("Europe/Berlin")
	if want := time.Date(2026, time.December, 26, 0, 0, 0, 0, berlin); !v.Window.ReleasedUntil.Equal(want) {
		t.Errorf("released until %v, want %v", v.Window.ReleasedUntil, want)
	}

	// Every lesson the window reaches exists.
	var count int

	err = st.InTx(ctx, store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		lessons, err := tx.Lessons().ListRange(ctx, v.Window.ReleasedUntil.AddDate(0, 0, -7), v.Window.ReleasedUntil)
		count = len(lessons)

		return err
	})
	if err != nil || count == 0 {
		t.Fatalf("no lessons in the last released week: %v", err)
	}

	// Narrower again: nothing is generated, nothing removed.
	rules.ReleaseWeeksAhead = 2
	if v, err = svc.SetBookingRules(ctx, rules); err != nil || v.Generated != 0 {
		t.Fatalf("narrower: generated %d, %v", v.Generated, err)
	}

	rules.WaitlistInviteExpiry = 0
	_, err = svc.SetBookingRules(ctx, rules)
	wantField(t, err, "waitlistInviteExpiry")
}

// failingWrites fails every read-write transaction after the first n.
type failingWrites struct {
	store.Store

	n int
}

var errWriteFailed = errors.New("write failed")

func (f *failingWrites) InTx(ctx context.Context, mode store.TxMode, fn func(context.Context, store.Tx) error) error {
	if mode == store.ReadWrite {
		if f.n == 0 {
			return errWriteFailed
		}

		f.n--
	}

	return f.Store.InTx(ctx, mode, fn)
}

// A wider window whose lessons cannot be generated at once is still saved:
// the answer shows the stored rules, and the hourly job generates the
// lessons.
func TestSettingsSetBookingRulesGenerationFails(t *testing.T) {
	t.Parallel()

	st, _ := newDemoSettings(t)
	svc := service.NewSettingsService(&failingWrites{Store: st, n: 1}, clock.NewFixed(settingsNow), ids.V7{})

	v, err := svc.SetBookingRules(t.Context(), service.BookingRules{
		ReleaseWeekday: time.Sunday, ReleaseTime: domain.TimeOfDay{Hour: 18}, ReleaseWeeksAhead: 10,
		WaitlistInviteExpiry: time.Hour, DefaultVisibleDays: 14,
	})
	if err != nil {
		t.Fatalf("the change failed: %v", err)
	}

	if v.ReleaseWeeksAhead != 10 || v.Generated != 0 || !errors.Is(v.GenerateErr, errWriteFailed) {
		t.Fatalf("got weeks %d, generated %d, %v", v.ReleaseWeeksAhead, v.Generated, v.GenerateErr)
	}

	if stored, err := service.NewSettingsService(st, clock.NewFixed(settingsNow), ids.V7{}).BookingRules(t.Context()); err != nil ||
		stored.ReleaseWeeksAhead != 10 {
		t.Fatalf("stored weeks %d, %v", stored.ReleaseWeeksAhead, err)
	}
}

func TestSettingsWithoutSchool(t *testing.T) {
	t.Parallel()

	svc := service.NewSettingsService(memory.New(), clock.NewFixed(settingsNow), ids.V7{})

	if _, err := svc.School(t.Context()); !errors.Is(err, service.ErrNoSchool) {
		t.Fatalf("School: want ErrNoSchool, got %v", err)
	}

	if _, err := svc.SetBranding(t.Context(), ""); !errors.Is(err, service.ErrNoSchool) {
		t.Fatalf("SetBranding: want ErrNoSchool, got %v", err)
	}
}
