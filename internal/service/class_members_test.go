package service

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/seed"
	"github.com/cwbudde/studio-schedule/internal/store"
)

func TestClassMembers(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newGenFixture(t, seed.DemoSettings())
	svc := f.timetable(t, "2026-10-19T10:00:00Z")

	c, err := svc.CreateClass(ctx, f.term.ID, f.classData())
	if err != nil {
		t.Fatal(err)
	}

	zoe := domain.Person{ID: ids.New[domain.PersonID](f.seq), GivenName: "Zoe", FamilyName: "Example"}
	ann := domain.Person{ID: ids.New[domain.PersonID](f.seq), GivenName: "Ann", FamilyName: "Example"}
	f.write(t, func(ctx context.Context, tx store.Tx) error {
		return errors.Join(tx.Persons().Create(ctx, zoe), tx.Persons().Create(ctx, ann))
	})

	members := func(t *testing.T) []ClassMemberEntry {
		t.Helper()

		ms, err := svc.ClassMembers(ctx, c.ID)
		if err != nil {
			t.Fatal(err)
		}

		return ms
	}

	t.Run("add", func(t *testing.T) {
		since := mustDate(t, "2026-09-07")

		m, err := svc.AddClassMember(ctx, Principal{}, domain.ClassMember{ClassID: c.ID, PersonID: zoe.ID, Since: since})
		if err != nil {
			t.Fatal(err)
		}

		if m.Person != zoe || m.Since != since || m.ClassID != c.ID {
			t.Fatalf("added %+v", m)
		}

		if _, err := svc.AddClassMember(ctx, Principal{}, domain.ClassMember{ClassID: c.ID, PersonID: ann.ID}); err != nil {
			t.Fatal(err)
		}

		got := members(t)
		if len(got) != 2 || got[0].Person != ann || got[1].Person != zoe || got[1].Since != since {
			t.Fatalf("members %+v, want Ann, then Zoe since %s", got, since)
		}
	})

	t.Run("twice", func(t *testing.T) {
		_, err := svc.AddClassMember(ctx, Principal{}, domain.ClassMember{ClassID: c.ID, PersonID: zoe.ID})
		if !errors.Is(err, domain.ErrDuplicate) {
			t.Fatalf("want ErrDuplicate, got %v", err)
		}
	})

	t.Run("unknown person", func(t *testing.T) {
		_, err := svc.AddClassMember(ctx, Principal{}, domain.ClassMember{ClassID: c.ID, PersonID: ids.New[domain.PersonID](f.seq)})

		var fe *domain.FieldError
		if !errors.As(err, &fe) || fe.Field != "personId" {
			t.Fatalf("want a validation error on personId, got %v", err)
		}
	})

	t.Run("invalid since", func(t *testing.T) {
		_, err := svc.AddClassMember(ctx, Principal{}, domain.ClassMember{
			ClassID: c.ID, PersonID: ann.ID, Since: domain.Date{Year: 2026, Month: 2, Day: 31},
		})

		var fe *domain.FieldError
		if !errors.As(err, &fe) || fe.Field != "since" {
			t.Fatalf("want a validation error on since, got %v", err)
		}
	})

	t.Run("unknown class", func(t *testing.T) {
		unknown := ids.New[domain.ClassID](f.seq)

		if _, err := svc.ClassMembers(ctx, unknown); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("list: want ErrNotFound, got %v", err)
		}

		_, err := svc.AddClassMember(ctx, Principal{}, domain.ClassMember{ClassID: unknown, PersonID: zoe.ID})
		if !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("add: want ErrNotFound, got %v", err)
		}
	})

	t.Run("a class with members is in use", func(t *testing.T) {
		if err := svc.DeleteClass(ctx, c.ID); !errors.Is(err, domain.ErrInUse) {
			t.Fatalf("want ErrInUse, got %v", err)
		}
	})

	t.Run("remove", func(t *testing.T) {
		if err := svc.RemoveClassMember(ctx, Principal{}, c.ID, zoe.ID); err != nil {
			t.Fatal(err)
		}

		if got := members(t); len(got) != 1 || got[0].Person != ann {
			t.Fatalf("members %+v, want Ann", got)
		}

		if err := svc.RemoveClassMember(ctx, Principal{}, c.ID, zoe.ID); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("remove twice: want ErrNotFound, got %v", err)
		}
	})
}

// racingMembers is a store whose AddMember fails with err, as when a
// concurrent transaction wins a race after the service checked.
type racingMembers struct {
	store.Store

	err error
}

func (s racingMembers) InTx(ctx context.Context, mode store.TxMode, fn func(context.Context, store.Tx) error) error {
	return s.Store.InTx(ctx, mode, func(ctx context.Context, tx store.Tx) error {
		return fn(ctx, racingMembersTx{Tx: tx, err: s.err})
	})
}

type racingMembersTx struct {
	store.Tx

	err error
}

func (t racingMembersTx) Classes() store.ClassRepo { return racingClassRepo{t.Tx.Classes(), t.err} }

type racingClassRepo struct {
	store.ClassRepo

	err error
}

func (r racingClassRepo) AddMember(context.Context, domain.ClassMember) error { return r.err }

func TestAddClassMemberRaces(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newGenFixture(t, seed.DemoSettings())

	c, err := f.timetable(t, "2026-10-19T10:00:00Z").CreateClass(ctx, f.term.ID, f.classData())
	if err != nil {
		t.Fatal(err)
	}

	zoe := domain.Person{ID: ids.New[domain.PersonID](f.seq), GivenName: "Zoe", FamilyName: "Example"}
	f.write(t, func(ctx context.Context, tx store.Tx) error { return tx.Persons().Create(ctx, zoe) })

	add := func(storeErr error) error {
		svc := NewTimetableService(racingMembers{Store: f.st, err: storeErr}, clock.NewFixed(mustInstant(t, "2026-10-19T10:00:00Z")), f.seq, Mails{})
		_, err := svc.AddClassMember(ctx, Principal{}, domain.ClassMember{ClassID: c.ID, PersonID: zoe.ID})

		return err
	}

	// A class or person deleted meanwhile is gone, not a duplicate.
	err = add(fmt.Errorf("foreign key violated: %w", domain.ErrConflict))
	if !errors.Is(err, domain.ErrNotFound) || errors.Is(err, domain.ErrDuplicate) {
		t.Errorf("reference deleted meanwhile: %v, want ErrNotFound and no ErrDuplicate", err)
	}
}

// TestUpdateClassMemberInvalidDate refuses an impossible pause date as a
// field error and keeps the membership and its bookings.
func TestUpdateClassMemberInvalidDate(t *testing.T) {
	t.Parallel()

	f := newBookingFixture(t)
	c := f.memberClass(t)

	_, err := f.timetable(t, bookingNow).UpdateClassMember(t.Context(), f.principal(t, f.olga), domain.ClassMember{
		ClassID: c.ID, PersonID: f.mia.ID, PausedUntil: domain.Date{Year: 2026, Month: 2, Day: 30},
	})
	wantFieldError(t, err, "pausedUntil")

	if m := f.memberOf(t, c.ID, f.mia); !m.PausedUntil.IsZero() {
		t.Errorf("membership %+v, want no pause", m)
	}

	for _, l := range f.classLessons(t, c.ID) {
		if b := f.bookingOf(t, l, f.mia); len(b) != 1 || b[0].Status != domain.BookingBooked {
			t.Errorf("%s: bookings %+v, want Mia's kept", l.SlotDate, b)
		}
	}
}
