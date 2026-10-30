// Package service holds the application use cases. Services run their work
// inside store transactions and return domain errors; they know nothing
// about HTTP.
package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/store"
)

var (
	// ErrTimezoneLocked refuses a new time zone once lessons exist: their
	// instants were computed in the old one (ADR 0013).
	ErrTimezoneLocked = fmt.Errorf("%w: the time zone cannot change once lessons exist", domain.ErrConflict)
	// ErrCurrencyLocked refuses a new currency once a price was set or a
	// receipt issued: their amounts are in the old one (ADR 0013).
	ErrCurrencyLocked = fmt.Errorf(
		"%w: the currency cannot change once prices or receipts exist", domain.ErrConflict,
	)
)

// SettingsService reads and changes the school settings, one area (card)
// at a time (ADR 0013). The pass and receipt settings live with their
// services.
type SettingsService struct {
	store store.Store
	clock clock.Clock
	gen   *LessonGenerator
}

// NewSettingsService returns a SettingsService backed by st.
func NewSettingsService(st store.Store, clk clock.Clock, src ids.Source) *SettingsService {
	return &SettingsService{store: st, clock: clk, gen: NewLessonGenerator(st, clk, src)}
}

// PublicSettings are the settings anonymous visitors may see, with the
// content type and digest of the logo and of the background (each zero
// without one).
type PublicSettings struct {
	domain.Settings

	Logo       domain.LogoInfo
	Background domain.BackgroundInfo
}

// Public returns the settings that may be shown to anonymous visitors.
func (s *SettingsService) Public(ctx context.Context) (PublicSettings, error) {
	var out PublicSettings

	err := s.store.InTx(ctx, store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		var err error
		if out.Settings, err = tx.Settings().Get(ctx); err != nil {
			return err
		}

		out.DefaultLocale = domain.SchoolLocale(out.DefaultLocale)
		if out.Logo, err = tx.Settings().LogoInfo(ctx); err != nil {
			return err
		}

		out.Background, err = tx.Settings().BackgroundInfo(ctx)

		return err
	})
	if err != nil {
		return PublicSettings{}, fmt.Errorf("public settings: %w", err)
	}

	return out, nil
}

// CachedLogo is the school's logo for a client that may have it already.
type CachedLogo struct {
	// Digest identifies the logo (domain.Logo.Digest); "" without one.
	Digest string
	// Logo is the logo; the zero Logo when the client has it already.
	Logo domain.Logo
}

// Logo returns the school's logo (the receipts' logo, ADR 0013): the zero
// CachedLogo without one, only its digest when has reports that the client
// has that one, and else the logo with its digest. A client that has the
// logo costs no read of the image.
func (s *SettingsService) Logo(ctx context.Context, has func(digest string) bool) (CachedLogo, error) {
	var out CachedLogo

	err := s.store.InTx(ctx, store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		info, err := tx.Settings().LogoInfo(ctx)
		if err != nil || info.IsZero() {
			return err
		}

		if has(info.Digest) {
			out = CachedLogo{Digest: info.Digest}

			return nil
		}

		// Read apart from the digest, the logo may have changed since:
		// its own digest describes what is sent.
		l, err := tx.Settings().Logo(ctx)
		out = CachedLogo{Digest: l.Digest(), Logo: l}

		return err
	})
	if err != nil {
		return CachedLogo{}, fmt.Errorf("logo: %w", err)
	}

	return out, nil
}

// CachedBackground is the school's background image for a client that may
// have it already.
type CachedBackground struct {
	// Digest identifies the background (domain.Background.Digest); ""
	// without one.
	Digest string
	// Background is the background; the zero Background when the client
	// has it already.
	Background domain.Background
}

// Background returns the school's background image (ADR 0018) as Logo
// does the logo: the zero CachedBackground without one, only its digest
// when has reports that the client has that one, and else the background
// with its digest. A client that has the background costs no read of the
// image.
func (s *SettingsService) Background(ctx context.Context, has func(digest string) bool) (CachedBackground, error) {
	var out CachedBackground

	err := s.store.InTx(ctx, store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		info, err := tx.Settings().BackgroundInfo(ctx)
		if err != nil || info.IsZero() {
			return err
		}

		if has(info.Digest) {
			out = CachedBackground{Digest: info.Digest}

			return nil
		}

		// Read apart from the digest, the background may have changed
		// since: its own digest describes what is sent.
		b, err := tx.Settings().Background(ctx)
		out = CachedBackground{Digest: b.Digest(), Background: b}

		return err
	})
	if err != nil {
		return CachedBackground{}, fmt.Errorf("background: %w", err)
	}

	return out, nil
}

// PutBackground replaces the school's background image (ADR 0018); the
// zero Background removes it. One that is not a JPEG, PNG or WebP image of
// at most domain.MaxBackgroundBytes is a validation error on contentType
// or data. Like the logo (ReceiptService.SetLogo), the change is not
// audited.
func (s *SettingsService) PutBackground(ctx context.Context, b domain.Background) error {
	if !b.IsZero() {
		if err := b.Validate(); err != nil {
			return err
		}
	}

	err := s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		return tx.Settings().PutBackground(ctx, b)
	})
	if err != nil {
		return fmt.Errorf("set background: %w", err)
	}

	return nil
}

// SettingsView is the settings as the office sees them, with what the card
// at hand shows besides (viewParts): the logo and the background, what
// fixes the time zone and the currency, or the release window now.
type SettingsView struct {
	domain.Settings

	// Logo is the logo's type and digest (viewLogo).
	Logo domain.LogoInfo
	// Background is the background's type and digest (viewBackground).
	Background domain.BackgroundInfo
	// Usage is what fixes the time zone and the currency (viewUsage).
	Usage store.SettingsUsage
	// Now is the instant the window was computed at (viewWindow).
	Now    time.Time
	Window ReleaseWindow
	// Generated is how many lessons a change of the booking rules created
	// right away; zero for every other read and write.
	Generated int
	// GenerateErr is why those lessons could not be generated at once. The
	// booking rules are stored all the same, and the hourly job generates
	// the lessons (ADR 0013).
	GenerateErr error
}

// viewParts is what a SettingsView shows besides the settings; each card
// reads only what it shows.
type viewParts uint8

const (
	viewLogo viewParts = 1 << iota
	viewBackground
	viewUsage
	viewWindow
)

// TimezoneLocked reports whether the time zone is fixed: lessons exist.
func (v SettingsView) TimezoneLocked() bool { return v.Usage.Lessons }

// CurrencyLocked reports whether the currency is fixed: prices or receipts
// exist.
func (v SettingsView) CurrencyLocked() bool { return v.Usage.Prices || v.Usage.Receipts }

// AccentDark is the dark theme's variant of the accent color, "" without
// an accent.
func (v SettingsView) AccentDark() string { return AccentDark(v.AccentColor) }

// AccentDark is the dark theme's variant of an accent color ("#rrggbb"),
// "" for none (or an invalid one).
func AccentDark(accent string) string {
	c, ok := domain.ParseColor(accent)
	if !ok {
		return ""
	}

	return domain.DarkAccent(c).Hex()
}

// School returns the settings of the school profile card.
func (s *SettingsService) School(ctx context.Context) (SettingsView, error) {
	return s.get(ctx, "school profile", 0)
}

// Branding returns the settings of the branding card, with the logo and
// the background.
func (s *SettingsService) Branding(ctx context.Context) (SettingsView, error) {
	return s.get(ctx, "branding", viewLogo|viewBackground)
}

// Regional returns the settings of the time and language card, with what
// fixes the time zone and the currency.
func (s *SettingsService) Regional(ctx context.Context) (SettingsView, error) {
	return s.get(ctx, "time and language", viewUsage)
}

// BookingRules returns the settings of the booking rules card, with the
// release window now.
func (s *SettingsService) BookingRules(ctx context.Context) (SettingsView, error) {
	return s.get(ctx, "booking rules", viewWindow)
}

func (s *SettingsService) get(ctx context.Context, what string, parts viewParts) (SettingsView, error) {
	var out SettingsView

	err := s.store.InTx(ctx, store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		settings, _, err := loadSchool(ctx, tx)
		if err != nil {
			return err
		}

		out, err = s.view(ctx, tx, parts, settings, nil)

		return err
	})
	if err != nil {
		return SettingsView{}, fmt.Errorf("%s settings: %w", what, err)
	}

	return out, nil
}

// view returns the view of settings with parts; usage, if not nil, is
// what was read already.
func (s *SettingsService) view(
	ctx context.Context, tx store.Tx, parts viewParts, settings domain.Settings, usage *store.SettingsUsage,
) (SettingsView, error) {
	out := SettingsView{Settings: settings}

	var err error

	if parts&viewLogo != 0 {
		if out.Logo, err = tx.Settings().LogoInfo(ctx); err != nil {
			return SettingsView{}, err
		}
	}

	if parts&viewBackground != 0 {
		if out.Background, err = tx.Settings().BackgroundInfo(ctx); err != nil {
			return SettingsView{}, err
		}
	}

	switch {
	case parts&viewUsage == 0:
	case usage != nil:
		out.Usage = *usage
	default:
		if out.Usage, err = tx.Settings().Usage(ctx); err != nil {
			return SettingsView{}, err
		}
	}

	if parts&viewWindow != 0 {
		// Valid settings name a known zone.
		loc, err := time.LoadLocation(settings.Timezone)
		if err != nil {
			return SettingsView{}, fmt.Errorf("school time zone %q: %w", settings.Timezone, err)
		}

		out.Now = s.clock.Now()
		out.Window = ComputeReleaseWindow(settings, loc, out.Now)
	}

	return out, nil
}

// update changes the settings with change, validates them and stores them,
// all in one transaction, and returns the view of the result with parts.
// change may refuse with an error, seeing what uses the settings: the usage
// is read for viewUsage only, and is zero otherwise.
func (s *SettingsService) update(
	ctx context.Context, what string, parts viewParts, change func(*domain.Settings, store.SettingsUsage) error,
) (SettingsView, error) {
	var out SettingsView

	err := s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		settings, _, err := loadSchool(ctx, tx)
		if err != nil {
			return err
		}

		var usage *store.SettingsUsage

		if parts&viewUsage != 0 {
			u, err := tx.Settings().Usage(ctx)
			if err != nil {
				return err
			}

			usage = &u
		}

		if err := change(&settings, deref(usage)); err != nil {
			return err
		}

		if err := settings.Validate(); err != nil {
			return err
		}

		if err := tx.Settings().Put(ctx, settings); err != nil {
			return err
		}

		out, err = s.view(ctx, tx, parts, settings, usage)

		return err
	})
	if err != nil {
		return SettingsView{}, fmt.Errorf("set %s: %w", what, err)
	}

	return out, nil
}

func deref[T any](p *T) T {
	if p == nil {
		var zero T

		return zero
	}

	return *p
}

// SchoolProfile is the school's name and its legal notice (imprint).
type SchoolProfile struct {
	Name        string
	LegalNotice string
}

// SetSchool changes the school's name and legal notice; both are trimmed.
func (s *SettingsService) SetSchool(ctx context.Context, p SchoolProfile) (SettingsView, error) {
	return s.update(ctx, "school profile", 0, func(settings *domain.Settings, _ store.SettingsUsage) error {
		settings.SchoolName = strings.TrimSpace(p.Name)
		settings.LegalNotice = strings.TrimSpace(p.LegalNotice)

		return nil
	})
}

// Branding is what the branding card edits. The logo is the receipts'
// (ReceiptService.SetLogo), the background is PutBackground's.
type Branding struct {
	// AccentColor is the accent color, "" for the app's own.
	AccentColor string
	// Theme is the school's theme (ADR 0018); nil keeps the stored one, so
	// a client that does not know themes cannot reset it.
	Theme *domain.Theme
}

// SetBranding changes the accent color and, if b names one, the theme. The
// accent is stored in lower case; one that does not stand out against the
// text on it is a validation error on accentColor, an unknown theme one on
// theme.
func (s *SettingsService) SetBranding(ctx context.Context, b Branding) (SettingsView, error) {
	return s.update(ctx, "branding", viewLogo|viewBackground, func(settings *domain.Settings, _ store.SettingsUsage) error {
		settings.AccentColor = strings.ToLower(strings.TrimSpace(b.AccentColor))
		if b.Theme != nil {
			settings.Theme = *b.Theme
		}

		return nil
	})
}

// Regional are the time zone, the default language and the currency.
type Regional struct {
	Timezone      string
	DefaultLocale string
	Currency      string
}

// SetRegional changes the time zone, the default language and the
// currency. The language is one of domain.Locales or a tag that matches one
// ("de-AT" is stored as "de"), and may always change; the time zone only while no
// lesson exists (ErrTimezoneLocked) and the currency only while no price
// and no receipt exists (ErrCurrencyLocked). Sending the current value of a
// locked setting is no change.
func (s *SettingsService) SetRegional(ctx context.Context, r Regional) (SettingsView, error) {
	return s.update(ctx, "time and language", viewUsage, func(settings *domain.Settings, usage store.SettingsUsage) error {
		tz, currency := strings.TrimSpace(r.Timezone), strings.TrimSpace(r.Currency)

		switch {
		case tz != settings.Timezone && usage.Lessons:
			return ErrTimezoneLocked
		case currency != settings.Currency && (usage.Prices || usage.Receipts):
			return ErrCurrencyLocked
		}

		settings.Timezone, settings.DefaultLocale, settings.Currency = tz, matchLocale(r.DefaultLocale), currency

		return nil
	})
}

// BookingRules are the release rule, the cutoffs, how long a waiting-list
// invite holds its place and the members' early access (ADR 0003, 0009,
// 0010).
type BookingRules struct {
	ReleaseWeekday       time.Weekday
	ReleaseTime          domain.TimeOfDay
	ReleaseWeeksAhead    int
	BookingCutoff        time.Duration
	CancellationCutoff   time.Duration
	WaitlistInviteExpiry time.Duration
	DefaultVisibleDays   int
}

// SetBookingRules changes the booking rules from now on. Released lessons
// stay released, but a narrower window holds back the ones it no longer
// reaches. A wider window (more weeks ahead or more days of early access)
// generates the lessons it reaches right away, after the change is stored,
// rather than at the next hourly run; SettingsView.Generated counts them.
// The change is stored either way: a failure to generate is no error but
// SettingsView.GenerateErr, and the hourly job generates the lessons.
func (s *SettingsService) SetBookingRules(ctx context.Context, r BookingRules) (SettingsView, error) {
	var widened bool

	out, err := s.update(ctx, "booking rules", viewWindow, func(settings *domain.Settings, _ store.SettingsUsage) error {
		widened = r.ReleaseWeeksAhead > settings.ReleaseWeeksAhead || r.DefaultVisibleDays > settings.DefaultVisibleDays
		settings.ReleaseWeekday, settings.ReleaseTime, settings.ReleaseWeeksAhead = r.ReleaseWeekday, r.ReleaseTime,
			r.ReleaseWeeksAhead
		settings.BookingCutoff, settings.CancellationCutoff = r.BookingCutoff, r.CancellationCutoff
		settings.WaitlistInviteExpiry, settings.DefaultVisibleDays = r.WaitlistInviteExpiry, r.DefaultVisibleDays

		return nil
	})
	if err != nil || !widened {
		return out, err
	}

	// Its own transaction: InTx never nests.
	if out.Generated, err = s.gen.GenerateAhead(ctx); err != nil {
		out.Generated, out.GenerateErr = 0, fmt.Errorf("generate lessons for the wider window: %w", err)
	}

	return out, nil
}

// ReleasePreview is the release window a release rule would have now.
type ReleasePreview struct {
	Now    time.Time
	Window ReleaseWindow
}

// PreviewRelease computes the release window of a release rule at the
// current instant in the school's time zone, without storing anything, so
// the form shows what a change would release. Only the rule is checked
// (domain.ValidateReleaseRule): out-of-range values are FieldErrors on
// releaseWeekday, releaseTime and releaseWeeksAhead.
func (s *SettingsService) PreviewRelease(
	ctx context.Context, weekday time.Weekday, at domain.TimeOfDay, weeksAhead int,
) (ReleasePreview, error) {
	if err := domain.ValidateReleaseRule(weekday, at, weeksAhead); err != nil {
		return ReleasePreview{}, err
	}

	var out ReleasePreview

	err := s.store.InTx(ctx, store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		settings, loc, err := loadSchool(ctx, tx)
		if err != nil {
			return err
		}

		settings.ReleaseWeekday, settings.ReleaseTime, settings.ReleaseWeeksAhead = weekday, at, weeksAhead
		out.Now = s.clock.Now()
		out.Window = ComputeReleaseWindow(settings, loc, out.Now)

		return nil
	})
	if err != nil {
		return ReleasePreview{}, fmt.Errorf("preview release: %w", err)
	}

	return out, nil
}

// matchLocale returns the locale of domain.Locales a language tag asks for,
// or the trimmed tag, which Settings.Validate then refuses.
func matchLocale(tag string) string {
	if l, ok := domain.MatchLocale(tag); ok {
		return l
	}

	return strings.TrimSpace(tag)
}
