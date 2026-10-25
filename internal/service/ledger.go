package service

import (
	"context"
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// The ledger is the one way to write a booking, and a lesson that may have
// bookings: it writes through the store and settles the passes of what
// changed in the same transaction (ADR 0012). The caller holds the lesson
// lock (or has just created the lesson), so passes are locked after it.
// TestLedgerIsTheOnlyWriter keeps the rest of the package from writing
// bookings or updating lessons directly.

// chargesIn returns the redemption rules of the school.
func chargesIn(ctx context.Context, tx store.Tx) (charges, error) {
	settings, loc, err := loadSchool(ctx, tx)
	if err != nil {
		return charges{}, err
	}

	return chargesOf(settings, loc), nil
}

// createBooking stores the new booking b of lesson l and settles its pass:
// a booking due when it is made uses one. A store error of the create is
// returned unchanged, for the caller to map.
func createBooking(ctx context.Context, tx store.Tx, c charges, now time.Time, l domain.Lesson, b domain.Booking) error {
	if err := tx.Bookings().Create(ctx, b); err != nil {
		return err
	}

	return c.settle(ctx, tx, now, l, l, bookingChange{after: b})
}

// updateBookings stores the changed bookings of lesson l and settles their
// passes together (one lock of the persons' passes).
func updateBookings(
	ctx context.Context, tx store.Tx, c charges, now time.Time, l domain.Lesson, changes ...bookingChange,
) error {
	for _, ch := range changes {
		if err := tx.Bookings().Update(ctx, ch.after); err != nil {
			return err
		}
	}

	return c.settle(ctx, tx, now, l, l, changes...)
}

// updateLesson stores lesson l, which was old, and settles its bookings: a
// change of its status settles every booking (a lesson the school canceled
// costs nothing), and a move to another school date keeps its bookings paid
// only by passes valid on the new date (charges.recheck).
func updateLesson(ctx context.Context, tx store.Tx, c charges, now time.Time, old, l domain.Lesson) error {
	if err := tx.Lessons().Update(ctx, l); err != nil {
		return err
	}

	statusChanged := old.Status != l.Status
	moved := domain.DateOf(old.StartsAt, c.loc) != domain.DateOf(l.StartsAt, c.loc)

	if !statusChanged && !moved {
		return nil
	}

	bookings, err := tx.Bookings().ListByLesson(ctx, l.ID)
	if err != nil || len(bookings) == 0 {
		return err
	}

	if statusChanged {
		changes := make([]bookingChange, len(bookings))
		for i, b := range bookings {
			changes[i] = bookingChange{before: b, after: b}
		}

		if err := c.settle(ctx, tx, now, old, l, changes...); err != nil {
			return err
		}
	}

	if moved {
		return c.recheck(ctx, tx, now, l, bookings)
	}

	return nil
}
