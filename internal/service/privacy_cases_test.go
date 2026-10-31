package service

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// TestAnonymizeStoreFaults: a store failure anywhere in anonymizing Paula
// is returned and leaves her as she was: nothing is half-anonymized, no
// booking canceled, no visit given back, no link, session or account gone,
// no audit event and no mail.
func TestAnonymizeStoreFaults(t *testing.T) {
	t.Parallel()

	SweepFaults(t, FaultSweep{Setup: func(t *testing.T) (*FaultStore, func() error) {
		f := newPrivacyFaults(t)
		fs := NewFaultStore(f.st)
		svc := f.persons(t, fs)

		return fs, func() error {
			_, err := svc.Anonymize(t.Context(), f.office, f.paula.ID)

			return err
		}
	}})
}

// TestAnonymizeAdminStoreFaults: an admin anonymizes the person of another
// admin, which reads every admin under the locks of their persons.
func TestAnonymizeAdminStoreFaults(t *testing.T) {
	t.Parallel()

	SweepFaults(t, FaultSweep{Setup: func(t *testing.T) (*FaultStore, func() error) {
		f := newPrivacyFaults(t)
		fs := NewFaultStore(f.st)
		svc := f.persons(t, fs)

		return fs, func() error {
			sum, err := svc.Anonymize(t.Context(), f.admin, f.abeAcc.PersonID)
			if err == nil && !sum.AccountDeleted {
				return errors.New("the admin's account stayed")
			}

			return err
		}
	}})
}

// TestAnonymizeEndsEverything checks what the sweep's fixture loses: the
// running impersonation of the account is audited as ended, the staff
// record, the duplicate dismissal and the push subscription no longer name
// the person, and a lesson that starts right now keeps its booking as
// history.
func TestAnonymizeEndsEverything(t *testing.T) {
	t.Parallel()

	f := newPrivacyFaults(t)
	now := f.lessonAt(t, f.now)
	nowBooking := f.booking(t, now, domain.BookingBooked, f.now.Add(-time.Hour))

	sum, err := f.persons(t, f.st).Anonymize(t.Context(), f.office, f.paula.ID)
	if err != nil {
		t.Fatal(err)
	}

	want := AnonymizeSummary{
		Person: f.paula.Anonymize(f.now), BookingsCanceled: 1, WaitlistWithdrawn: 1, MembershipsEnded: 1,
		LinksRemoved: 2, StaffUnlinked: 1, AccountDeleted: true,
	}
	if sum != want {
		t.Errorf("summary %+v, want %+v", sum, want)
	}

	err = f.st.InTx(t.Context(), store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		st, err := tx.Staff().Get(ctx, f.staff.ID)
		if err != nil || !st.PersonID.IsZero() || st.DisplayName != "Paula P." {
			t.Errorf("staff %+v, %v; want it without the person", st, err)
		}

		if d, err := tx.DuplicateDismissals().ListByPerson(ctx, f.paula.ID); err != nil || len(d) != 0 {
			t.Errorf("duplicate dismissals %+v, %v", d, err)
		}

		if subs, err := tx.PushSubscriptions().ListByAccount(ctx, f.paulaAcc.ID); err != nil || len(subs) != 0 {
			t.Errorf("push subscriptions %+v, %v", subs, err)
		}

		if b, err := tx.Bookings().Get(ctx, nowBooking.ID); err != nil || b.Status != domain.BookingBooked {
			t.Errorf("the booking of the lesson that starts now %+v, %v; want it kept", b, err)
		}

		if b, err := tx.Bookings().Get(ctx, f.futureBooking.ID); err != nil || b.Status != domain.BookingCanceled {
			t.Errorf("future booking %+v, %v", b, err)
		}

		if p, err := tx.Passes().Get(ctx, f.pass.ID); err != nil || p.VisitsRemaining != 9 {
			t.Errorf("pass %+v, %v; want the future visit back", p, err)
		}

		events, err := tx.Audit().List(ctx, 10)
		if err != nil {
			return err
		}

		// Deleting the account points the events that named it at its
		// person.
		ended := slices.ContainsFunc(events, func(e domain.AuditEvent) bool {
			return e.Action == domain.AuditImpersonationEnded && e.ActorID == f.adaAcc.ID &&
				e.TargetKind == domain.AuditTargetPerson && e.TargetPersonID == f.paula.ID
		})
		if !ended || events[0].Action != domain.AuditPersonAnonymized {
			t.Errorf("audit %+v; want the impersonation ended and the person anonymized", events)
		}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestExportStoreFaults: a store failure anywhere in an export is returned
// (an export only reads).
func TestExportStoreFaults(t *testing.T) {
	t.Parallel()

	for name, export := range map[string]func(t *testing.T, f *privacyFaults, st store.Store) error{
		"office": func(t *testing.T, f *privacyFaults, st store.Store) error {
			_, err := f.persons(t, st).Export(t.Context(), f.paula.ID)

			return err
		},
		"own": func(t *testing.T, f *privacyFaults, st store.Store) error {
			p := Principal{AccountID: f.paulaAcc.ID, PersonID: f.paula.ID, Role: domain.RoleMember}
			_, err := NewFamilyService(st, clock.NewFixed(f.now), f.seq, Mails{}).Export(t.Context(), p, f.paula.ID)

			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			SweepFaults(t, FaultSweep{Setup: func(t *testing.T) (*FaultStore, func() error) {
				f := newPrivacyFaults(t)
				fs := NewFaultStore(f.st)

				return fs, func() error { return export(t, f, fs) }
			}})
		})
	}
}

// TestExportAccountData: Paula's own export has her push subscription, the
// announcement she dismissed but not the one she did not, and her receipts
// with the canceled one marked; Piet's receipt is not hers. The office's
// export of her is the same, account included.
func TestExportAccountData(t *testing.T) {
	t.Parallel()

	f := newPrivacyFaults(t)
	p := Principal{AccountID: f.paulaAcc.ID, PersonID: f.paula.ID, Role: domain.RoleMember}

	x, err := NewFamilyService(f.st, clock.NewFixed(f.now), f.seq, Mails{}).Export(t.Context(), p, f.paula.ID)
	if err != nil {
		t.Fatal(err)
	}

	if x.Account == nil || x.Account.ID != f.paulaAcc.ID || len(x.PushSubscriptions) != 1 ||
		x.PushSubscriptions[0].Endpoint != "https://push.example.org/paula" {
		t.Errorf("account %+v, push subscriptions %+v", x.Account, x.PushSubscriptions)
	}

	if len(x.Dismissed) != 1 || x.Dismissed[0].ID != f.dismissed.ID {
		t.Errorf("dismissed %+v, want only %q", x.Dismissed, f.dismissed.Title)
	}

	canceled := map[domain.ReceiptID]bool{}
	for _, r := range x.Receipts {
		canceled[r.Receipt.ID] = r.Canceled
	}

	if len(canceled) != 2 || !canceled[f.receipt.ID] || canceled[f.cancellation.ID] {
		t.Errorf("receipts (canceled) %v, want the sale canceled and its cancellation, not Piet's %s",
			canceled, f.unpaid.ID)
	}

	if len(x.Passes) != 1 || x.Passes[0].Type.Name != "10-visit pass" || len(x.Passes[0].Redemptions) != 2 {
		t.Errorf("passes %+v", x.Passes)
	}

	office, err := f.persons(t, f.st).Export(t.Context(), f.paula.ID)
	if err != nil || !reflect.DeepEqual(office, x) {
		t.Errorf("the office's export %+v, %v; want her own", office, err)
	}
}

// TestRetentionStoreFaults: a store failure in the purge is returned and
// rolls back what the purge deleted before it.
func TestRetentionStoreFaults(t *testing.T) {
	t.Parallel()

	SweepFaults(t, FaultSweep{Setup: func(t *testing.T) (*FaultStore, func() error) {
		f := newPrivacyFaults(t)
		fs := NewFaultStore(f.st)
		svc := NewRetentionService(fs, clock.NewFixed(f.now.AddDate(4, 0, 0)))

		return fs, func() error {
			got, err := svc.Purge(t.Context())
			if err == nil && got.PushSubscriptions != 1 {
				return errors.New("the unused push subscription stayed")
			}

			return err
		}
	}})
}
