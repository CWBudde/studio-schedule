package service

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/mail"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// The waiting-list fault tests run on the waiting-list lesson (Friday
// 2026-10-23, 18:00 in Berlin) at bookingNow. Mia's invite there, unless it
// ran out, is valid until 2026-10-21T22:00:00Z.

const (
	inviteValid   = "2026-10-21T22:00:00Z"
	inviteRanOut  = "2026-10-21T09:00:00Z"
	miasInviteKey = "mias-invite-token"
)

// storeEntry stores e with a new ID, joined the day before bookingNow
// unless it says otherwise, and returns it.
func (f *bookingFixture) storeEntry(t *testing.T, e domain.WaitlistEntry) domain.WaitlistEntry {
	t.Helper()

	e.ID = ids.New[domain.WaitlistEntryID](f.seq)
	if e.JoinedAt.IsZero() {
		e.JoinedAt = mustInstant(t, "2026-10-20T09:00:00Z")
	}

	f.write(t, func(ctx context.Context, tx store.Tx) error { return tx.Waitlist().Create(ctx, e) })

	return e
}

// linkedInvite stores an invite of person in l, sent the day before
// bookingNow with the link token and valid until expires.
func (f *bookingFixture) linkedInvite(
	t *testing.T, l domain.Lesson, person domain.Person, token, expires string, stay bool,
) domain.WaitlistEntry {
	t.Helper()

	return f.storeEntry(t, domain.WaitlistEntry{
		LessonID: l.ID, PersonID: person.ID, Status: domain.WaitlistInvited, StayOnList: stay,
		InviteTokenHash: domain.HashToken(token), InvitedAt: mustInstant(t, "2026-10-20T10:00:00Z"),
		ExpiresAt: mustInstant(t, expires),
	})
}

// waitlistOn returns a waiting-list service on fs at now.
func (f *bookingFixture) waitlistOn(t *testing.T, fs *FaultStore, now string) *WaitlistService {
	t.Helper()

	return NewWaitlistService(fs, clock.NewFixed(mustInstant(t, now)), f.seq, testMails(newTestRenderer(t)))
}

// TestWaitlistJoinStoreFaults joins behind an invite that ran out: Tom goes
// back to the end, Olga gets the place, and Mia joins. A store failure
// anywhere rolls all of it back, the mails included.
func TestWaitlistJoinStoreFaults(t *testing.T) {
	t.Parallel()

	SweepFaults(t, FaultSweep{Setup: func(t *testing.T) (*FaultStore, func() error) {
		f := newBookingFixture(t)
		anna := f.principal(t, f.anna)
		l := f.fullLesson(t, 2)
		f.cancelStella(t, "2026-10-20T08:00:00Z", l) // one place free
		f.linkedInvite(t, l, f.tom, "toms-token", inviteRanOut, true)
		olga := f.waitingEntry(t, l, f.olga, "2026-10-20T11:00:00Z")
		fs := NewFaultStore(f.st)
		svc := f.waitlistOn(t, fs, bookingNow)

		return fs, func() error {
			place, err := svc.Join(t.Context(), anna, l.ID, f.mia.ID, false)
			if err == nil {
				wantStatus(t, f.entry(t, olga.ID), domain.WaitlistInvited)

				// Invited Olga does not count; Tom waits ahead.
				if place.Position != 2 {
					t.Errorf("Mia's position %d, want 2 (behind Tom)", place.Position)
				}
			}

			return err
		}
	}})
}

// TestWaitlistLeaveStoreFaults withdraws Mia's invite, whose place goes to
// Olga.
func TestWaitlistLeaveStoreFaults(t *testing.T) {
	t.Parallel()

	SweepFaults(t, FaultSweep{Setup: func(t *testing.T) (*FaultStore, func() error) {
		f := newBookingFixture(t)
		anna := f.principal(t, f.anna)
		l := f.fullLesson(t, 1)
		f.cancelStella(t, "2026-10-20T08:00:00Z", l)
		mia := f.linkedInvite(t, l, f.mia, miasInviteKey, inviteValid, false)
		olga := f.waitingEntry(t, l, f.olga, "2026-10-20T11:00:00Z")
		fs := NewFaultStore(f.st)
		svc := f.waitlistOn(t, fs, bookingNow)

		return fs, func() error {
			err := svc.Leave(t.Context(), anna, mia.ID)
			if err == nil {
				wantStatus(t, f.entry(t, olga.ID), domain.WaitlistInvited)
			}

			return err
		}
	}})
}

// TestWaitlistAcceptStoreFaults accepts Mia's invite in the app; her pass
// pays for the booking.
func TestWaitlistAcceptStoreFaults(t *testing.T) {
	t.Parallel()

	SweepFaults(t, FaultSweep{Setup: func(t *testing.T) (*FaultStore, func() error) {
		f := newPassFixture(t, domain.RedeemOnBooking)
		anna := f.principal(t, f.anna)
		pass := f.sell(t, f.mia, f.tenVisits, "")
		l := f.fullLesson(t, 1)
		f.cancelStella(t, "2026-10-20T08:00:00Z", l)
		mia := f.linkedInvite(t, l, f.mia, miasInviteKey, inviteValid, false)
		fs := NewFaultStore(f.st)
		svc := f.waitlistOn(t, fs, bookingNow)

		return fs, func() error {
			_, err := svc.Accept(t.Context(), anna, mia.ID)
			if err == nil {
				f.wantVisits(t, pass, 9)
				wantStatus(t, f.entry(t, mia.ID), domain.WaitlistAccepted)
			}

			return err
		}
	}})
}

// TestWaitlistDeclineStoreFaults declines Mia's invite: she goes back to
// the end of the list, and Olga gets the place.
func TestWaitlistDeclineStoreFaults(t *testing.T) {
	t.Parallel()

	SweepFaults(t, FaultSweep{Setup: func(t *testing.T) (*FaultStore, func() error) {
		f := newBookingFixture(t)
		anna := f.principal(t, f.anna)
		l := f.fullLesson(t, 1)
		f.cancelStella(t, "2026-10-20T08:00:00Z", l)
		mia := f.linkedInvite(t, l, f.mia, miasInviteKey, inviteValid, true)
		olga := f.waitingEntry(t, l, f.olga, "2026-10-20T11:00:00Z")
		fs := NewFaultStore(f.st)
		svc := f.waitlistOn(t, fs, bookingNow)

		return fs, func() error {
			place, err := svc.Decline(t.Context(), anna, mia.ID)
			if err == nil {
				wantStatus(t, f.entry(t, olga.ID), domain.WaitlistInvited)

				// Invited Olga does not count: Mia is the first who waits.
				if place.Entry.Status != domain.WaitlistWaiting || place.Position != 1 {
					t.Errorf("Mia's place %+v, want waiting first", place)
				}
			}

			return err
		}
	}})
}

// TestWaitlistInviteStoreFaults shows the link of an entry that went back
// to waiting, with its position.
func TestWaitlistInviteStoreFaults(t *testing.T) {
	t.Parallel()

	SweepFaults(t, FaultSweep{Setup: func(t *testing.T) (*FaultStore, func() error) {
		f := newBookingFixture(t)
		l := f.fullLesson(t, 1)
		f.waitingEntry(t, l, f.olga, "2026-10-20T08:00:00Z")
		f.storeEntry(t, domain.WaitlistEntry{
			LessonID: l.ID, PersonID: f.mia.ID, Status: domain.WaitlistWaiting,
			InviteTokenHash: domain.HashToken(miasInviteKey),
		})
		fs := NewFaultStore(f.st)
		svc := f.waitlistOn(t, fs, bookingNow)

		return fs, func() error {
			v, err := svc.Invite(t.Context(), miasInviteKey)
			if err == nil && (v.State != InviteWaiting || v.Position != 2 || v.Person.ID != f.mia.ID) {
				t.Errorf("view %+v, want Mia waiting second", v)
			}

			return err
		}
	}})
}

// TestWaitlistAcceptInviteStoreFaults accepts Mia's invite through its
// link; her pass pays, and her guardian gets the confirmation.
func TestWaitlistAcceptInviteStoreFaults(t *testing.T) {
	t.Parallel()

	SweepFaults(t, FaultSweep{Setup: func(t *testing.T) (*FaultStore, func() error) {
		f := newPassFixture(t, domain.RedeemOnBooking)
		pass := f.sell(t, f.mia, f.tenVisits, "")
		l := f.fullLesson(t, 1)
		f.cancelStella(t, "2026-10-20T08:00:00Z", l)
		f.linkedInvite(t, l, f.mia, miasInviteKey, inviteValid, false)
		fs := NewFaultStore(f.st)
		svc := f.waitlistOn(t, fs, bookingNow)

		return fs, func() error {
			v, err := svc.AcceptInvite(t.Context(), miasInviteKey)
			if err == nil {
				f.wantVisits(t, pass, 9)

				if v.State != InviteAccepted || len(f.mailsOf(t, mail.TemplateBookingConfirmed)) != 1 {
					t.Errorf("view %+v; want accepted and one confirmation", v)
				}
			}

			return err
		}
	}})
}

// TestWaitlistExpireDueStoreFaults ends the invites that ran out in two
// lessons, each in its own transaction: a store failure undoes the lesson
// it hit, with its mail, and leaves the other settled.
func TestWaitlistExpireDueStoreFaults(t *testing.T) {
	t.Parallel()

	type world struct {
		f       *bookingFixture
		entries []domain.WaitlistEntry
	}

	worlds := map[*FaultStore]*world{} // the sweep's runs are sequential

	SweepFaults(t, FaultSweep{
		Setup: func(t *testing.T) (*FaultStore, func() error) {
			f := newBookingFixture(t)
			a, b := f.fullLesson(t, 1), f.fullLesson(t, 1)
			w := &world{f: f, entries: []domain.WaitlistEntry{
				f.linkedInvite(t, a, f.tom, "toms-token", inviteRanOut, false),
				f.linkedInvite(t, b, f.anna, "annas-token", inviteRanOut, false),
			}}
			fs := NewFaultStore(f.st)
			worlds[fs] = w
			svc := f.waitlistOn(t, fs, bookingNow)

			return fs, func() error {
				n, err := svc.ExpireDue(t.Context())
				if err == nil && n != 2 {
					t.Errorf("ExpireDue ended %d entries, want 2", n)
				}

				return err
			}
		},
		Check: func(t *testing.T, fs *FaultStore, _ StoreData, call FaultCall) {
			t.Helper()

			w := worlds[fs]
			calls := fs.Calls()
			tx := 0

			for _, c := range calls[:slices.Index(calls, call)+1] {
				if c.Method == "InTx" {
					tx++
				}
			}

			// The first transaction lists the due lessons: failing, nothing
			// happens. Each later one settles one lesson.
			wantOpen := 1
			if tx == 1 {
				wantOpen = 2
			}

			open := 0

			for _, e := range w.entries {
				if w.f.entry(t, e.ID).Status == domain.WaitlistInvited {
					open++
				}
			}

			mails := len(w.f.mailsOf(t, mail.TemplateWaitlistExpired))
			if open != wantOpen || mails != 2-open {
				t.Errorf("with %s failing (transaction %d): %d invites left, %d mails; want %d left and a mail per ended one",
					call, tx, open, mails, wantOpen)
			}
		},
	})
}

// TestWaitlistExpireDueDeletedLesson skips a due lesson deleted between
// listing and locking it, with its entries, and settles the others.
func TestWaitlistExpireDueDeletedLesson(t *testing.T) {
	t.Parallel()

	f := newBookingFixture(t)
	a, b := f.fullLesson(t, 1), f.fullLesson(t, 1)
	inA := f.linkedInvite(t, a, f.tom, "toms-token", inviteRanOut, false)
	inB := f.linkedInvite(t, b, f.anna, "annas-token", inviteRanOut, false)
	fs := NewFaultStore(f.st).FailWith("Lessons.Lock", 1, domain.ErrNotFound)

	n, err := f.waitlistOn(t, fs, bookingNow).ExpireDue(t.Context())
	if err != nil || n != 1 {
		t.Fatalf("ExpireDue = %d, %v; want 1 and no error", n, err)
	}

	// The lesson locked first (the lower ID) counts as deleted.
	first, second := inA, inB
	if bytes.Compare(b.ID[:], a.ID[:]) < 0 {
		first, second = inB, inA
	}

	wantStatus(t, f.entry(t, first.ID), domain.WaitlistInvited)
	wantStatus(t, f.entry(t, second.ID), domain.WaitlistExpired)
}

// TestWaitlistWriteConflicts maps what the store reports for a write that
// lost a race the lesson lock does not cover: a second entry or booking of
// the person (the unique index) and a person deleted meanwhile. Nothing
// stays written.
func TestWaitlistWriteConflicts(t *testing.T) {
	t.Parallel()

	join := func(t *testing.T, f *bookingFixture, svc *WaitlistService, l domain.Lesson) error {
		_, err := svc.Join(t.Context(), f.principal(t, f.tom), l.ID, f.tom.ID, false)

		return err
	}
	accept := func(t *testing.T, _ *bookingFixture, svc *WaitlistService, _ domain.Lesson) error {
		_, err := svc.AcceptInvite(t.Context(), miasInviteKey)

		return err
	}

	for name, tt := range map[string]struct {
		method string
		err    error
		op     func(t *testing.T, f *bookingFixture, svc *WaitlistService, l domain.Lesson) error
		want   error
	}{
		"join, entered meanwhile":  {"Waitlist.Create", domain.ErrDuplicate, join, ErrAlreadyWaiting},
		"join, person deleted":     {"Waitlist.Create", domain.ErrConflict, join, domain.ErrNotFound},
		"accept, booked meanwhile": {"Bookings.Create", domain.ErrDuplicate, accept, ErrAlreadyBooked},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := newBookingFixture(t)
			l := f.fullLesson(t, 1)
			f.waitingEntry(t, l, f.olga, "2026-10-20T08:00:00Z")
			f.cancelStella(t, "2026-10-21T09:30:00Z", l) // Olga is invited
			f.linkedInvite(t, l, f.mia, miasInviteKey, inviteValid, false)

			fs := NewFaultStore(f.st).FailWith(tt.method, 1, tt.err)
			before := fs.Snapshot(t)

			if err := tt.op(t, f, f.waitlistOn(t, fs, bookingNow), l); !errors.Is(err, tt.want) {
				t.Fatalf("want %v, got %v", tt.want, err)
			}

			if changed := before.Changed(fs.Snapshot(t)); len(changed) > 0 {
				t.Errorf("the store changed: %v", changed)
			}
		})
	}
}

// waitlisterTx runs fn with a waitlister in a transaction of fs.
func (f *bookingFixture) waitlisterTx(
	t *testing.T, fs *FaultStore, fn func(ctx context.Context, tx store.Tx, w waitlister) error,
) error {
	t.Helper()

	w := waitlister{ids: f.seq, mails: testMails(newTestRenderer(t))}

	return fs.InTx(t.Context(), store.ReadWrite, func(ctx context.Context, tx store.Tx) error { return fn(ctx, tx, w) })
}

// TestWaitlisterVoidStoreFaults sends the invites of a canceled lesson back
// to waiting; a store failure keeps every invite.
func TestWaitlisterVoidStoreFaults(t *testing.T) {
	t.Parallel()

	SweepFaults(t, FaultSweep{Setup: func(t *testing.T) (*FaultStore, func() error) {
		f := newBookingFixture(t)
		l := f.fullLesson(t, 3)
		f.cancelStella(t, "2026-10-20T08:00:00Z", l)
		mia := f.linkedInvite(t, l, f.mia, miasInviteKey, inviteValid, false)
		tom := f.linkedInvite(t, l, f.tom, "toms-token", inviteValid, false)
		waiting := f.waitingEntry(t, l, f.anna, "2026-10-20T11:00:00Z")
		fs := NewFaultStore(f.st)

		return fs, func() error {
			err := f.waitlisterTx(t, fs, func(ctx context.Context, tx store.Tx, w waitlister) error {
				return w.void(ctx, tx, l.ID)
			})
			if err == nil {
				for _, e := range []domain.WaitlistEntry{mia, tom, waiting} {
					if got := f.entry(t, e.ID); got.Status != domain.WaitlistWaiting || !got.ExpiresAt.IsZero() ||
						!got.JoinedAt.Equal(e.JoinedAt) {
						t.Errorf("entry %+v, want waiting at its place", got)
					}
				}
			}

			return err
		}
	}})
}

// TestWaitlisterWithdrawPersonStoreFaults withdraws Tom's entries of two
// lessons; the place his invite held goes to Olga. A store failure keeps
// his entries and invites nobody.
func TestWaitlisterWithdrawPersonStoreFaults(t *testing.T) {
	t.Parallel()

	SweepFaults(t, FaultSweep{Setup: func(t *testing.T) (*FaultStore, func() error) {
		f := newBookingFixture(t)
		a, b := f.fullLesson(t, 1), f.fullLesson(t, 1)
		f.cancelStella(t, "2026-10-20T08:00:00Z", a)
		invited := f.linkedInvite(t, a, f.tom, "toms-token", inviteValid, false)
		olga := f.waitingEntry(t, a, f.olga, "2026-10-20T11:00:00Z")
		waiting := f.waitingEntry(t, b, f.tom, "2026-10-20T11:00:00Z")
		fs := NewFaultStore(f.st)

		return fs, func() error {
			err := f.waitlisterTx(t, fs, func(ctx context.Context, tx store.Tx, w waitlister) error {
				return w.withdrawPerson(ctx, tx, mustInstant(t, bookingNow), f.tom.ID)
			})
			if err == nil {
				wantStatus(t, f.entry(t, invited.ID), domain.WaitlistWithdrawn)
				wantStatus(t, f.entry(t, waiting.ID), domain.WaitlistWithdrawn)
				wantStatus(t, f.entry(t, olga.ID), domain.WaitlistInvited)
			}

			return err
		}
	}})
}
