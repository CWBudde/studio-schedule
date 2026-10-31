package service

import (
	"errors"
	"testing"

	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/domain"
)

// TestAssignStoreFaults: a store failure while a pass is given away is
// returned and stores no pass.
func TestAssignStoreFaults(t *testing.T) {
	t.Parallel()

	SweepFaults(t, FaultSweep{Setup: func(t *testing.T) (*FaultStore, func() error) {
		f := newPassFixture(t, domain.RedeemOnAttendance)
		fs, svc, _ := f.faulty(t)

		return fs, func() error {
			_, err := svc.Assign(t.Context(), f.office, f.mia.ID, PassAssignment{PassTypeID: f.month.ID, Note: "Prize"})

			return err
		}
	}})
}

// TestVoidStoreFaults: a store failure while a pass is voided is returned
// and changes nothing: the pass stays active, the bookings ahead stay paid
// by it, and the single visit that would take over one keeps its visit.
func TestVoidStoreFaults(t *testing.T) {
	t.Parallel()

	SweepFaults(t, FaultSweep{Setup: func(t *testing.T) (*FaultStore, func() error) {
		f := newPassFixture(t, domain.RedeemOnBooking)
		pass := f.sell(t, f.mia, f.tenVisits, "")
		f.sell(t, f.mia, f.single, "")
		f.book(t, bookingNow, f.regular(t, staffLesson), f.mia) // started when voided
		f.book(t, bookingNow, f.regular(t, "2026-10-23T16:00:00Z"), f.mia)
		f.book(t, bookingNow, f.regular(t, "2026-10-24T16:00:00Z"), f.mia)

		fs := NewFaultStore(f.st)
		svc := NewPassService(fs, clock.NewFixed(mustInstant(t, staffStarted)), f.seq, nil)

		return fs, func() error {
			_, err := svc.Void(t.Context(), pass.ID)

			return err
		}
	}})
}

// TestPassReadStoreFaults: the pass reads return a store failure.
func TestPassReadStoreFaults(t *testing.T) {
	t.Parallel()

	for name, read := range map[string]func(t *testing.T, f *passFixture, svc *PassService) error{
		"PersonPasses": func(t *testing.T, f *passFixture, svc *PassService) error {
			_, err := svc.PersonPasses(t.Context(), f.mia.ID)

			return err
		},
		"Mine": func(t *testing.T, f *passFixture, svc *PassService) error {
			_, err := svc.Mine(t.Context(), f.principal(t, f.anna))

			return err
		},
		"PassTypes": func(t *testing.T, _ *passFixture, svc *PassService) error {
			_, err := svc.PassTypes(t.Context())

			return err
		},
		"RedemptionMode": func(t *testing.T, _ *passFixture, svc *PassService) error {
			_, err := svc.RedemptionMode(t.Context())

			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			SweepFaults(t, FaultSweep{Setup: func(t *testing.T) (*FaultStore, func() error) {
				// Mia's pass paid for a lesson: the views read its uses.
				f := newPassFixture(t, domain.RedeemOnBooking)
				f.sell(t, f.mia, f.tenVisits, "")
				f.book(t, bookingNow, f.regular(t, "2026-10-23T16:00:00Z"), f.mia)
				fs, svc, _ := f.faulty(t)

				return fs, func() error { return read(t, f, svc) }
			}})
		})
	}
}

// TestPassTypeWriteStoreFaults: a store failure while the office edits the
// pass catalog or the redemption mode is returned and changes nothing.
func TestPassTypeWriteStoreFaults(t *testing.T) {
	t.Parallel()

	data := PassTypeData{Name: "5-visit pass", Kind: domain.PassVisits, Visits: 5, ValidityDays: 90, Price: 6500, Active: true}

	for name, write := range map[string]func(t *testing.T, f *passFixture, svc *PassService) error{
		"CreatePassType": func(t *testing.T, _ *passFixture, svc *PassService) error {
			_, err := svc.CreatePassType(t.Context(), data)

			return err
		},
		"UpdatePassType": func(t *testing.T, f *passFixture, svc *PassService) error {
			_, err := svc.UpdatePassType(t.Context(), f.single.ID, data)

			return err
		},
		"RetirePassType": func(t *testing.T, f *passFixture, svc *PassService) error {
			_, err := svc.RetirePassType(t.Context(), f.single.ID)

			return err
		},
		"DeletePassType": func(t *testing.T, f *passFixture, svc *PassService) error {
			return svc.DeletePassType(t.Context(), f.single.ID)
		},
		"SetRedemptionMode": func(t *testing.T, _ *passFixture, svc *PassService) error {
			return svc.SetRedemptionMode(t.Context(), domain.RedeemOnBooking)
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			SweepFaults(t, FaultSweep{Setup: func(t *testing.T) (*FaultStore, func() error) {
				f := newPassFixture(t, domain.RedeemOnAttendance)
				fs, svc, _ := f.faulty(t)

				return fs, func() error { return write(t, f, svc) }
			}})
		})
	}
}

// TestUpdatePassTypeInvalid: an invalid edit of a pass type is a validation
// error on its field and keeps the type as it was.
func TestUpdatePassTypeInvalid(t *testing.T) {
	t.Parallel()

	f := newPassFixture(t, domain.RedeemOnAttendance)
	svc := f.passService(t, bookingNow)

	var fe *domain.FieldError

	_, err := svc.UpdatePassType(t.Context(), f.single.ID, PassTypeData{Name: "  ", Kind: domain.PassVisits, Visits: 1, Price: 1500, Active: true})
	if !errors.As(err, &fe) || fe.Field != "name" {
		t.Fatalf("UpdatePassType = %v, want a validation error on name", err)
	}

	types, err := svc.PassTypes(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	for _, pt := range types {
		if pt.ID == f.single.ID && pt != f.single {
			t.Fatalf("type %+v after a refused edit, want %+v", pt, f.single)
		}
	}
}

// TestPassInvalidFirstDay: a pass from a date that does not exist is a
// validation error on validFrom, for a sale before it takes a receipt
// number and for a gift, and stores nothing.
func TestPassInvalidFirstDay(t *testing.T) {
	t.Parallel()

	f := newPassFixture(t, domain.RedeemOnAttendance)
	svc := f.passService(t, bookingNow)
	feb30 := domain.Date{Year: 2026, Month: 2, Day: 30}

	var fe *domain.FieldError

	_, err := svc.Assign(t.Context(), f.office, f.mia.ID, PassAssignment{PassTypeID: f.month.ID, ValidFrom: feb30})
	if !errors.As(err, &fe) || fe.Field != "validFrom" {
		t.Errorf("Assign = %v, want a validation error on validFrom", err)
	}

	_, err = svc.Sell(t.Context(), f.office, f.mia.ID, PassSale{PassTypeID: f.month.ID, ValidFrom: feb30})
	if !errors.As(err, &fe) || fe.Field != "validFrom" {
		t.Errorf("Sell = %v, want a validation error on validFrom", err)
	}

	if passes, err := svc.PersonPasses(t.Context(), f.mia.ID); err != nil || len(passes) != 0 || f.receiptCount(t) != 0 {
		t.Fatalf("passes %+v (%v), %d receipts after refused passes", passes, err, f.receiptCount(t))
	}

	if r, _ := f.receipt(t, f.sell(t, f.mia, f.single, "").ReceiptID); r.Number != "SD-2026-0001" {
		t.Fatalf("number %s after a refused sale, want SD-2026-0001", r.Number)
	}
}

// TestVoidBeforeTheSale: a clock behind the sale (a server's clock that
// went back) cannot void a pass, neither directly nor by a Storno: the
// void would predate the sale. Nothing changes.
func TestVoidBeforeTheSale(t *testing.T) {
	t.Parallel()

	f := newPassFixture(t, domain.RedeemOnAttendance)
	pass := f.sell(t, f.mia, f.tenVisits, "")
	earlier := "2026-10-21T09:00:00Z"

	if _, err := f.passService(t, earlier).Void(t.Context(), pass.ID); !errors.Is(err, domain.ErrValidation) {
		t.Errorf("Void = %v, want a validation error", err)
	}

	if _, err := f.receiptService(t, earlier).Cancel(t.Context(), f.office, pass.ReceiptID, ""); !errors.Is(err, domain.ErrValidation) {
		t.Errorf("Cancel = %v, want a validation error", err)
	}

	if got := f.pass(t, pass.ID); got != pass || f.receiptCount(t) != 1 {
		t.Fatalf("pass %+v with %d receipts, want it as sold with its receipt only", got, f.receiptCount(t))
	}
}
