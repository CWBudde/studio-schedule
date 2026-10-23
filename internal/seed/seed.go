// Package seed provides fictional demo data for the demo server, the
// WebAssembly build and tests. It must never contain real names or data.
package seed

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// DemoSettings returns the settings of the fictional demo school.
func DemoSettings() domain.Settings {
	return domain.Settings{
		SchoolName:         "Studio Demo — Ballet & Dance",
		Timezone:           "Europe/Berlin",
		DefaultLocale:      "en",
		Currency:           "EUR",
		ReceiptPrefix:      "SD",
		ReleaseWeekday:     time.Sunday,
		ReleaseTime:        domain.TimeOfDay{Hour: 18},
		ReleaseWeeksAhead:  1,
		BookingCutoff:      0,
		CancellationCutoff: 2 * time.Hour,
		DefaultVisibleDays: 14,
	}
}

// Dataset is a complete set of school data. Every entity is valid, and all
// references point to entities of the same Dataset.
type Dataset struct {
	Settings     domain.Settings
	Locations    []domain.Location
	Rooms        []domain.Room
	Staff        []domain.Staff
	CourseTypes  []domain.CourseType
	Terms        []domain.Term
	Classes      []domain.Class
	Persons      []domain.Person
	Guardians    []domain.Guardian
	Accounts     []domain.Account
	ClassMembers []domain.ClassMember
}

// LoadDemo writes the complete demo data set (Demo) into st in one
// transaction. It fails with domain.ErrConflict unless st is empty (has no
// settings and no persons).
func LoadDemo(ctx context.Context, st store.Store) error {
	d := Demo()

	err := st.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		return write(ctx, tx, d)
	})
	if err != nil {
		return fmt.Errorf("seed demo data: %w", err)
	}

	return nil
}

// write stores d through the repositories of tx, referenced records first.
// It has no side effects outside tx, so the store may retry it.
func write(ctx context.Context, tx store.Tx, d Dataset) error {
	if err := checkEmpty(ctx, tx); err != nil {
		return err
	}

	if err := tx.Settings().Put(ctx, d.Settings); err != nil {
		return fmt.Errorf("settings: %w", err)
	}

	for _, p := range d.Persons {
		if err := tx.Persons().Create(ctx, p); err != nil {
			return fmt.Errorf("person %v: %w", p.ID, err)
		}
	}

	for _, g := range d.Guardians {
		if err := tx.Persons().AddGuardian(ctx, g); err != nil {
			return fmt.Errorf("guardian %v of %v: %w", g.GuardianID, g.DependentID, err)
		}
	}

	for _, a := range d.Accounts {
		if err := tx.Accounts().Create(ctx, a); err != nil {
			return fmt.Errorf("account %v: %w", a.ID, err)
		}
	}

	for _, l := range d.Locations {
		if err := tx.Locations().Create(ctx, l); err != nil {
			return fmt.Errorf("location %v: %w", l.ID, err)
		}
	}

	for _, r := range d.Rooms {
		if err := tx.Rooms().Create(ctx, r); err != nil {
			return fmt.Errorf("room %v: %w", r.ID, err)
		}
	}

	for _, s := range d.Staff {
		if err := tx.Staff().Create(ctx, s); err != nil {
			return fmt.Errorf("staff %v: %w", s.ID, err)
		}
	}

	for _, c := range d.CourseTypes {
		if err := tx.CourseTypes().Create(ctx, c); err != nil {
			return fmt.Errorf("course type %v: %w", c.ID, err)
		}
	}

	for _, t := range d.Terms {
		if err := tx.Terms().Create(ctx, t); err != nil {
			return fmt.Errorf("term %v: %w", t.ID, err)
		}
	}

	for _, c := range d.Classes {
		if err := tx.Classes().Create(ctx, c); err != nil {
			return fmt.Errorf("class %v: %w", c.ID, err)
		}
	}

	for _, m := range d.ClassMembers {
		if err := tx.Classes().AddMember(ctx, m); err != nil {
			return fmt.Errorf("member %v of class %v: %w", m.PersonID, m.ClassID, err)
		}
	}

	return nil
}

// checkEmpty fails with domain.ErrConflict if the store has settings or
// persons: demo data is never mixed into a store that is in use.
func checkEmpty(ctx context.Context, tx store.Tx) error {
	switch _, err := tx.Settings().Get(ctx); {
	case err == nil:
		return fmt.Errorf("%w: the store is not empty (settings exist)", domain.ErrConflict)
	case !errors.Is(err, domain.ErrNotFound):
		return fmt.Errorf("settings: %w", err)
	}

	persons, err := tx.Persons().List(ctx)
	if err != nil {
		return fmt.Errorf("persons: %w", err)
	}

	if len(persons) > 0 {
		return fmt.Errorf("%w: the store is not empty (persons exist)", domain.ErrConflict)
	}

	return nil
}
