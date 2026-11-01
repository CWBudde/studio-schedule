package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// ReceiptSettingsUpdate is what the receipt settings card writes: the
// receipt settings and the receipt number prefix (ReceiptService.SetSettings).
type ReceiptSettingsUpdate struct {
	Settings domain.ReceiptSettings
	Prefix   string
}

// SettingsImport is a change of several areas (cards) at once, for
// `settings import`: every area that is not nil is written as its setter
// would write it. The logo and the background image are not part of it.
type SettingsImport struct {
	School         *SchoolProfile
	Branding       *Branding
	Regional       *Regional
	BookingRules   *BookingRules
	Receipts       *ReceiptSettingsUpdate
	RedemptionMode *domain.RedemptionMode
}

// SettingsImported is the result of an import: the settings before and
// after it (the same after a dry run as after the real one), and, for a
// real one whose booking rules widen the window, the lessons generated at
// once, or why they could not be (as SettingsView.Generated and
// GenerateErr).
type SettingsImported struct {
	Before, After domain.Settings
	Generated     int
	GenerateErr   error
}

// Export returns the school's settings, every area at once, for
// `settings export`. A database without settings is ErrNoSchool.
func (s *SettingsService) Export(ctx context.Context) (domain.Settings, error) {
	var out domain.Settings

	err := s.store.InTx(ctx, store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		var err error

		out, _, err = loadSchool(ctx, tx)

		return err
	})
	if err != nil {
		return domain.Settings{}, fmt.Errorf("export settings: %w", err)
	}

	return out, nil
}

// Import writes the areas of in, all of them in one transaction or none:
// each area is changed as its setter changes it (SetSchool, SetBranding,
// SetRegional, SetBookingRules, ReceiptService.SetSettings,
// PassService.SetRedemptionMode), so the same rules apply. A new time zone
// or currency that the data fixes is ErrTimezoneLocked or ErrCurrencyLocked,
// invalid values are FieldErrors on the settings' fields. With dryRun it
// checks everything and writes nothing. Booking rules that widen the window
// generate the lessons it reaches after the change is stored, as
// SetBookingRules does.
func (s *SettingsService) Import(ctx context.Context, in SettingsImport, dryRun bool) (SettingsImported, error) {
	var (
		out     SettingsImported
		widened bool
	)

	mode := store.ReadWrite
	if dryRun {
		mode = store.ReadOnly
	}

	err := s.store.InTx(ctx, mode, func(ctx context.Context, tx store.Tx) error {
		settings, _, err := loadSchool(ctx, tx)
		if err != nil {
			return err
		}

		usage, err := tx.Settings().Usage(ctx)
		if err != nil {
			return err
		}

		out.Before = settings

		if widened, err = in.apply(&settings, usage, s.clock.Now()); err != nil {
			return err
		}

		if err := settings.Validate(); err != nil {
			return err
		}

		out.After = settings

		if dryRun {
			return nil
		}

		return tx.Settings().Put(ctx, settings)
	})
	if err != nil {
		return SettingsImported{}, fmt.Errorf("import settings: %w", err)
	}

	if widened && !dryRun {
		out.Generated, out.GenerateErr = s.generateAhead(ctx)
	}

	return out, nil
}

// apply writes the areas of in into settings, the regional settings (the
// time zone the booking rules are read in) first, and reports whether the
// booking rules widen the window.
func (in SettingsImport) apply(settings *domain.Settings, usage store.SettingsUsage, now time.Time) (bool, error) {
	if in.Regional != nil {
		if err := in.Regional.apply(settings, usage); err != nil {
			return false, err
		}
	}

	if in.School != nil {
		in.School.apply(settings)
	}

	if in.Branding != nil {
		in.Branding.apply(settings)
	}

	if in.Receipts != nil {
		in.Receipts.apply(settings)
	}

	if in.RedemptionMode != nil {
		settings.RedemptionMode = *in.RedemptionMode
	}

	if in.BookingRules == nil {
		return false, nil
	}

	return in.BookingRules.apply(settings, now)
}

// apply writes the receipt settings into settings, trimmed as
// ReceiptService.SetSettings trims them.
func (r ReceiptSettingsUpdate) apply(settings *domain.Settings) {
	rs := r.Settings
	rs.Issuer.Name, rs.Issuer.Address = strings.TrimSpace(rs.Issuer.Name), trimLines(rs.Issuer.Address)
	rs.Issuer.TaxID, rs.Issuer.Email = strings.TrimSpace(rs.Issuer.TaxID), strings.TrimSpace(rs.Issuer.Email)
	rs.Issuer.Phone, rs.TaxNote = strings.TrimSpace(rs.Issuer.Phone), strings.TrimSpace(rs.TaxNote)
	rs.Footer = trimLines(rs.Footer)

	settings.Receipts, settings.ReceiptPrefix = rs, strings.TrimSpace(r.Prefix)
}
