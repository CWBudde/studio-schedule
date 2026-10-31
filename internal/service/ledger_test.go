package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// TestLedgerIsTheOnlyWriter keeps every write of a booking, and every
// update of a lesson, in ledger.go, which settles their passes (ADR 0012):
// a new write path that wrote through the store directly would skip it.
func TestLedgerIsTheOnlyWriter(t *testing.T) {
	t.Parallel()

	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}

	writes := []string{"Bookings().Create(", "Bookings().Update(", "Lessons().Update("}

	for _, name := range files {
		if name == "ledger.go" || strings.HasSuffix(name, "_test.go") {
			continue
		}

		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}

		for _, w := range writes {
			if strings.Contains(string(src), w) {
				t.Errorf("%s calls %s directly; write through the ledger (ledger.go)", name, w)
			}
		}
	}
}

// TestRedemptionModeChange: changing the redemption mode is not retroactive
// (ADR 0012); a booking redeemed on booking settles by the new mode on its
// next transition only.
func TestRedemptionModeChange(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newPassFixture(t, domain.RedeemOnBooking)
	pass := f.sell(t, f.mia, f.tenVisits, "")
	thursday := f.regular(t, "2026-10-22T16:00:00Z")
	friday := f.regular(t, "2026-10-23T16:00:00Z")
	attended := f.book(t, bookingNow, thursday, f.mia)
	canceled := f.book(t, bookingNow, friday, f.mia)
	f.wantVisits(t, pass, 8)

	f.setMode(t, domain.RedeemOnAttendance)

	staff := f.participants(t, "2026-10-22T16:30:00Z")

	// Attended becomes due under the new mode, but the booking is paid
	// already: no second visit.
	if _, err := staff.SetAttendance(ctx, f.office, attended.ID, domain.BookingAttended); err != nil {
		t.Fatal(err)
	}

	f.wantVisits(t, pass, 8)
	f.wantPaidBy(t, attended.ID, pass.ID)

	// Back to booked stops being due: the visit redeemed on booking comes
	// back.
	if _, err := staff.SetAttendance(ctx, f.office, attended.ID, domain.BookingBooked); err != nil {
		t.Fatal(err)
	}

	f.wantVisits(t, pass, 9)
	f.wantPaidBy(t, attended.ID, domain.PassID{})

	// A timely cancellation is no transition under the new mode (the booking
	// was not due before it and is not after it), so the visit redeemed on
	// booking stays spent and the canceled booking stays paid.
	if _, err := f.bookingService(t, "2026-10-22T10:00:00Z").Cancel(ctx, f.principal(t, f.anna), canceled.ID); err != nil {
		t.Fatal(err)
	}

	f.wantVisits(t, pass, 9)
	f.wantPaidBy(t, canceled.ID, pass.ID)
}

// ledgerTx runs fn on the locked lesson l in a transaction of fs, with the
// school's charges.
func ledgerTx(
	t *testing.T, fs *FaultStore, l domain.Lesson, fn func(ctx context.Context, tx store.Tx, c charges, l domain.Lesson) error,
) error {
	t.Helper()

	return fs.InTx(t.Context(), store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		c, err := chargesIn(ctx, tx)
		if err != nil {
			return err
		}

		locked, err := tx.Lessons().Lock(ctx, l.ID)
		if err != nil {
			return err
		}

		return fn(ctx, tx, c, locked)
	})
}

// TestUpdateLessonStoreFaults: a store failure while the ledger writes a
// lesson and settles its bookings is returned, and no lesson, redemption or
// visit changes.
func TestUpdateLessonStoreFaults(t *testing.T) {
	t.Parallel()

	now := mustInstant(t, bookingNow)

	t.Run("canceled", func(t *testing.T) {
		t.Parallel()

		// The school cancels the lesson: Mia's visit comes back.
		SweepFaults(t, FaultSweep{Setup: func(t *testing.T) (*FaultStore, func() error) {
			f := newPassFixture(t, domain.RedeemOnBooking)
			pass := f.sell(t, f.mia, f.tenVisits, "")
			l := f.regular(t, "2026-10-22T16:00:00Z")
			b := f.book(t, bookingNow, l, f.mia)
			fs := NewFaultStore(f.st)

			return fs, func() error {
				err := ledgerTx(t, fs, l, func(ctx context.Context, tx store.Tx, c charges, old domain.Lesson) error {
					l := old
					l.Status = domain.LessonCanceled

					return updateLesson(ctx, tx, c, now, old, l)
				})
				if err == nil {
					f.wantVisits(t, pass, 10)
					f.wantPaidBy(t, b.ID, domain.PassID{})
				}

				return err
			}
		}})
	})

	t.Run("moved", func(t *testing.T) {
		t.Parallel()

		// The lesson moves past the end of Mia's 30-day pass, which paid
		// for it: her 10-visit pass, valid from 2026-11-20, pays instead.
		SweepFaults(t, FaultSweep{Setup: func(t *testing.T) (*FaultStore, func() error) {
			f := newPassFixture(t, domain.RedeemOnBooking)
			month := f.sell(t, f.mia, f.month, "")
			visits := f.sell(t, f.mia, f.tenVisits, "2026-11-20")
			l := f.regular(t, "2026-10-22T16:00:00Z")
			b := f.book(t, bookingNow, l, f.mia)
			f.wantPaidBy(t, b.ID, month.ID)
			fs := NewFaultStore(f.st)

			return fs, func() error {
				err := ledgerTx(t, fs, l, func(ctx context.Context, tx store.Tx, c charges, old domain.Lesson) error {
					l := old
					l.StartsAt = mustInstant(t, "2026-11-25T16:00:00Z")
					l.EndsAt = l.StartsAt.Add(time.Hour)

					return updateLesson(ctx, tx, c, now, old, l)
				})
				if err == nil {
					f.wantVisits(t, visits, 9)
					f.wantPaidBy(t, b.ID, visits.ID)
				}

				return err
			}
		}})
	})
}

// TestPaymentsStoreFaults: a store failure while reading how bookings are
// paid is returned.
func TestPaymentsStoreFaults(t *testing.T) {
	t.Parallel()

	SweepFaults(t, FaultSweep{Setup: func(t *testing.T) (*FaultStore, func() error) {
		f := newPassFixture(t, domain.RedeemOnBooking)
		pass := f.sell(t, f.mia, f.tenVisits, "")
		l := f.regular(t, "2026-10-22T16:00:00Z")
		paid := f.book(t, bookingNow, l, f.mia)
		f.bookFor(t, l, domain.Person{}) // a guest, who owns no passes
		bookings := f.bookingsOf(t, l)
		fs := NewFaultStore(f.st)

		return fs, func() error {
			return fs.InTx(t.Context(), store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
				c := charges{mode: domain.RedeemOnBooking, loc: berlin(t)}

				pays, err := c.payments(ctx, tx, map[domain.LessonID]domain.Lesson{l.ID: l}, bookings)
				if err == nil {
					if got := pays[paid.ID]; got.Status != PaymentPaid || got.Pass.ID != pass.ID || got.PassType.ID != f.tenVisits.ID {
						t.Errorf("Mia's payment %+v, want paid by her pass", got)
					}

					for _, b := range bookings {
						if b.PersonID.IsZero() && pays[b.ID].Status != PaymentUnpaid {
							t.Errorf("the guest's payment %+v, want unpaid", pays[b.ID])
						}
					}
				}

				return err
			})
		}
	}})
}
