package service

import (
	"context"
	"errors"
	"testing"

	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// passLockRace is a store whose next Passes().LockByPersons first runs
// meanwhile in its transaction: what a concurrent transaction committed
// between a pass's first read and its lock. PostgreSQL runs at READ
// COMMITTED, so the lock sees that write, and a row it moved to another
// owner no longer matches the lock's persons.
type passLockRace struct {
	store.Store

	meanwhile func(ctx context.Context, tx store.Tx) error
}

func (s *passLockRace) InTx(ctx context.Context, mode store.TxMode, fn func(context.Context, store.Tx) error) error {
	return s.Store.InTx(ctx, mode, func(ctx context.Context, tx store.Tx) error {
		return fn(ctx, passLockRaceTx{Tx: tx, s: s})
	})
}

type passLockRaceTx struct {
	store.Tx

	s *passLockRace
}

func (t passLockRaceTx) Passes() store.PassRepo {
	return passLockRacePasses{PassRepo: t.Tx.Passes(), tx: t.Tx, s: t.s}
}

type passLockRacePasses struct {
	store.PassRepo

	tx store.Tx
	s  *passLockRace
}

func (p passLockRacePasses) LockByPersons(ctx context.Context, persons ...domain.PersonID) ([]domain.Pass, error) {
	if m := p.s.meanwhile; m != nil {
		p.s.meanwhile = nil
		if err := m(ctx, p.tx); err != nil {
			return nil, err
		}
	}

	return p.PassRepo.LockByPersons(ctx, persons...)
}

// changePass returns a meanwhile that changes the pass id with change.
func changePass(id domain.PassID, change func(*domain.Pass)) func(context.Context, store.Tx) error {
	return func(ctx context.Context, tx store.Tx) error {
		p, err := tx.Passes().Get(ctx, id)
		if err != nil {
			return err
		}

		change(&p)

		return tx.Passes().Update(ctx, p)
	}
}

// TestVoidPassChangedBeforeTheLock: a pass voided or moved to another owner
// (a merge, ADR 0015) between its first read and its lock is not voided
// again or written back with the owner it had.
func TestVoidPassChangedBeforeTheLock(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		name   string
		change func(f *passFixture, p *domain.Pass)
		want   error
	}{
		{"voided", func(_ *passFixture, p *domain.Pass) {
			p.Status, p.VoidedAt = domain.PassVoided, mustInstant(t, bookingNow)
		}, ErrPassVoided},
		{"moved", func(f *passFixture, p *domain.Pass) { p.PersonID = f.anna.ID }, domain.ErrNotFound},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			f := newPassFixture(t, domain.RedeemOnAttendance)
			pass := f.sell(t, f.mia, f.tenVisits, "")
			race := &passLockRace{Store: f.st}
			race.meanwhile = changePass(pass.ID, func(p *domain.Pass) { c.change(f, p) })

			_, err := NewPassService(race, clock.NewFixed(mustInstant(t, staffStarted)), f.seq, nil).Void(t.Context(), pass.ID)
			if !errors.Is(err, c.want) {
				t.Fatalf("Void = %v, want %v", err, c.want)
			}

			// The refused void rolled the concurrent write back too: the
			// pass is as sold.
			if got := f.pass(t, pass.ID); got != pass {
				t.Fatalf("pass %+v, want %+v", got, pass)
			}
		})
	}
}

// TestCancelReceiptPassMovedBeforeTheLock: a Storno whose pass a merge
// moved to the survivor (ADR 0015) between its first read and its lock
// must not write the pass back with the tombstone as its owner.
func TestCancelReceiptPassMovedBeforeTheLock(t *testing.T) {
	t.Parallel()

	f := newPassFixture(t, domain.RedeemOnAttendance)
	dup := f.person(t, "Anna", "", "", "")
	pass := f.sell(t, dup, f.tenVisits, "")

	moved := false
	race := &passLockRace{Store: f.st}
	race.meanwhile = func(ctx context.Context, tx store.Tx) error {
		moved = true

		return changePass(pass.ID, func(p *domain.Pass) { p.PersonID = f.anna.ID })(ctx, tx)
	}

	rs := NewReceiptService(race, clock.NewFixed(mustInstant(t, bookingNow)), f.seq, Mails{}, f.pdf)

	_, err := rs.Cancel(t.Context(), f.office, pass.ReceiptID, "")
	if !moved {
		t.Fatal("the Storno did not lock the passes")
	}

	if err != nil {
		// Refused: everything rolled back, the concurrent move included.
		if !errors.Is(err, domain.ErrConflict) {
			t.Fatalf("Cancel = %v, want a conflict or a Storno of the moved pass", err)
		}

		if got := f.pass(t, pass.ID); got != pass {
			t.Fatalf("pass %+v after a refused Storno, want %+v", got, pass)
		}

		return
	}

	if got := f.pass(t, pass.ID); got.PersonID != f.anna.ID || got.Status != domain.PassVoided {
		t.Fatalf("pass owned by %v (%s) after the Storno, want the survivor %v's, voided", got.PersonID, got.Status, f.anna.ID)
	}
}
