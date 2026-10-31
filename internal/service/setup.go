package service

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// ErrAlreadySetUp refuses to set up a school whose settings exist.
var ErrAlreadySetUp = fmt.Errorf("%w: the school is set up already", domain.ErrConflict)

// SetupService sets a new installation up: the school settings and its
// first admin (`studio-schedule init`, ADR 0013).
type SetupService struct {
	store  store.Store
	clock  clock.Clock
	ids    ids.Source
	appURL string
}

// NewSetupService returns a SetupService; appURL is the root of the web
// app that the setup link points to, ending in "/" (or "#/").
func NewSetupService(st store.Store, clk clock.Clock, src ids.Source, appURL string) *SetupService {
	return &SetupService{store: st, clock: clk, ids: src, appURL: appURL}
}

// SetupData is what a new school needs to start with; the rest of the
// settings start from DefaultSettings and are edited in the app.
type SetupData struct {
	SchoolName string
	Timezone   string
	Locale     string
	Currency   string
	// The first admin.
	AdminEmail      string
	AdminGivenName  string
	AdminFamilyName string
}

// SetupResult is the first admin and the one-time link that sets their
// password.
type SetupResult struct {
	Account   domain.Account
	Link      string
	ExpiresAt time.Time
}

// DefaultSettings are the settings of a new school apart from its name,
// time zone, language and currency: a weekly release on Sunday at 18:00 of
// the next two weeks, booking until the start, canceling until 2 hours
// before, waiting-list invites held for 12 hours, two weeks of early
// access, visits used on attendance, receipts numbered "R-…".
func DefaultSettings() domain.Settings {
	return domain.Settings{
		ReceiptPrefix:        "R",
		ReleaseWeekday:       time.Sunday,
		ReleaseTime:          domain.TimeOfDay{Hour: 18},
		ReleaseWeeksAhead:    2,
		CancellationCutoff:   2 * time.Hour,
		WaitlistInviteExpiry: 12 * time.Hour,
		DefaultVisibleDays:   14,
		RedemptionMode:       domain.RedeemOnAttendance,
	}
}

// Init stores the settings of a new school and its first admin, in one
// transaction. The admin's account starts like an invited one: unconfirmed
// and without a password. Instead of a mail (a new installation may have
// no mail server yet), the link that sets the password, and so confirms
// the account, is returned; it is valid for InviteTokenTTL, and after that
// "forgot password" sends a new one. Settings that exist already are
// ErrAlreadySetUp; invalid data is a validation error naming the field.
func (s *SetupService) Init(ctx context.Context, d SetupData) (SetupResult, error) {
	settings := DefaultSettings()
	settings.SchoolName = strings.TrimSpace(d.SchoolName)
	settings.Timezone = strings.TrimSpace(d.Timezone)
	settings.DefaultLocale = matchLocale(d.Locale)
	settings.Currency = strings.ToUpper(strings.TrimSpace(d.Currency))

	if err := settings.Validate(); err != nil {
		return SetupResult{}, err
	}

	person := domain.Person{
		ID: ids.New[domain.PersonID](s.ids), GivenName: strings.TrimSpace(d.AdminGivenName),
		FamilyName: strings.TrimSpace(d.AdminFamilyName), Email: strings.TrimSpace(d.AdminEmail),
	}
	acc := domain.Account{
		ID: ids.New[domain.AccountID](s.ids), PersonID: person.ID, Email: person.Email, Role: domain.RoleAdmin,
		Locale: settings.DefaultLocale,
	}

	if err := person.Validate(); err != nil {
		return SetupResult{}, err
	}

	if err := acc.Validate(); err != nil {
		return SetupResult{}, err
	}

	token := newToken()

	now := s.clock.Now()
	t := domain.AccountToken{
		Hash: domain.HashToken(token), AccountID: acc.ID, Purpose: domain.TokenResetPassword, Email: acc.Email,
		CreatedAt: now, ExpiresAt: now.Add(InviteTokenTTL),
	}

	err := s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		switch _, err := tx.Settings().Get(ctx); {
		case err == nil:
			return ErrAlreadySetUp
		case !errors.Is(err, domain.ErrNotFound):
			return err
		}

		if err := tx.Settings().Put(ctx, settings); err != nil {
			return err
		}

		if err := tx.Persons().Create(ctx, person); err != nil {
			return err
		}

		switch err := tx.Accounts().Create(ctx, acc); {
		case errors.Is(err, domain.ErrDuplicate):
			return ErrEmailInUse
		case err != nil:
			return err
		}

		return tx.AccountTokens().Create(ctx, t)
	})
	if err != nil {
		return SetupResult{}, fmt.Errorf("set up the school: %w", err)
	}

	return SetupResult{
		Account: acc, Link: s.appURL + "reset-password?token=" + url.QueryEscape(token), ExpiresAt: t.ExpiresAt,
	}, nil
}
