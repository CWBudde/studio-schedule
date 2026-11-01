package storetest

import (
	"context"
	"testing"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/store"
)

func testLevels(t *testing.T, newStore func(*testing.T) store.Store) {
	t.Helper()

	testCRUD(t, newStore, crud[domain.LevelID, domain.Level]{
		id: func(l domain.Level) domain.LevelID { return l.ID },
		get: func(ctx context.Context, tx store.Tx, id domain.LevelID) (domain.Level, error) {
			return tx.Levels().Get(ctx, id)
		},
		create: func(ctx context.Context, tx store.Tx, l domain.Level) error { return tx.Levels().Create(ctx, l) },
		update: func(ctx context.Context, tx store.Tx, l domain.Level) error { return tx.Levels().Update(ctx, l) },
		remove: func(ctx context.Context, tx store.Tx, id domain.LevelID) error { return tx.Levels().Delete(ctx, id) },
		fixtures: func(e *env) (domain.Level, domain.Level) {
			// Update never writes the position, so both share it.
			minimal := domain.Level{ID: newID[domain.LevelID](e), Name: "Beginner", Position: 2}
			full := minimal
			full.Name = "Beginner (adults)"
			full.Code = "B"
			valid(e.t, minimal)
			valid(e.t, full)

			return minimal, full
		},
	})

	cases(t, newStore, map[string]func(e *env){
		"DuplicateName": func(e *env) {
			beginner, advanced := e.level("Beginner", "", 1), e.level("Advanced", "", 2)

			dup := domain.Level{ID: newID[domain.LevelID](e), Name: "beginner", Position: 3}
			err := e.write(func(ctx context.Context, tx store.Tx) error { return tx.Levels().Create(ctx, dup) })
			wantErr(e.t, err, domain.ErrDuplicate)

			advanced.Name = "BEGINNER"
			err = e.write(func(ctx context.Context, tx store.Tx) error { return tx.Levels().Update(ctx, advanced) })
			wantErr(e.t, err, domain.ErrDuplicate)

			// A level keeps its own name in another case.
			beginner.Name = "BEGINNER"
			e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Levels().Update(ctx, beginner) })
			wantSlice(e.t, e.levels(), []domain.Level{beginner, e.getLevel(advanced.ID)})
		},
		"DuplicateCode": func(e *env) {
			a, b := e.level("Advanced", "a", 1), e.level("Beginner", "B", 2)

			dup := domain.Level{ID: newID[domain.LevelID](e), Name: "Expert", Code: "A", Position: 3}
			err := e.write(func(ctx context.Context, tx store.Tx) error { return tx.Levels().Create(ctx, dup) })
			wantErr(e.t, err, domain.ErrDuplicate)

			b.Code = "A"
			err = e.write(func(ctx context.Context, tx store.Tx) error { return tx.Levels().Update(ctx, b) })
			wantErr(e.t, err, domain.ErrDuplicate)

			// A level keeps its own code in another case.
			a.Code = "A"
			e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Levels().Update(ctx, a) })
			wantEqual(e.t, e.getLevel(a.ID), a)
		},
		"WithoutCode": func(e *env) {
			// Any number of levels may go without a code.
			one, two := e.level("Open", "", 1), e.level("Taster", "", 2)
			three := e.level("Mixed", "M", 3)

			three.Code = ""
			e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Levels().Update(ctx, three) })
			wantSlice(e.t, e.levels(), []domain.Level{one, two, three})
		},
		"UpdateKeepsPosition": func(e *env) {
			l := e.level("Intermediate", "I", 3)

			moved := l
			moved.Name, moved.Position = "Intermediate II", 1
			e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Levels().Update(ctx, moved) })

			moved.Position = 3
			wantEqual(e.t, e.getLevel(l.ID), moved)
		},
		"ListOrder": func(e *env) {
			// Created in another order than the list, so that neither the
			// insertion order nor the name alone gives the expected one.
			mid, zeta, alpha := e.level("Mid", "", 2), e.level("Zeta", "", 1), e.level("Alpha", "", 2)
			wantSlice(e.t, e.levels(), []domain.Level{zeta, alpha, mid})
		},
		"SetOrder": func(e *env) {
			a, b, c := e.level("Beginner", "B", 1), e.level("Intermediate", "I", 2), e.level("Advanced", "A", 3)

			e.mustWrite(func(ctx context.Context, tx store.Tx) error {
				return tx.Levels().SetOrder(ctx, []domain.LevelID{c.ID, b.ID, a.ID})
			})

			a.Position, b.Position, c.Position = 3, 2, 1
			wantSlice(e.t, e.levels(), []domain.Level{c, b, a})
		},
		"SetOrderRefusals": func(e *env) {
			a, b, c := e.level("Beginner", "B", 1), e.level("Intermediate", "I", 2), e.level("Advanced", "A", 3)
			before := e.levels()

			for name, order := range map[string][]domain.LevelID{
				"missing level":             {c.ID, a.ID},
				"repeated level":            {c.ID, a.ID, a.ID},
				"repeated level, one extra": {c.ID, b.ID, a.ID, a.ID},
				"unknown level":             {c.ID, b.ID, newID[domain.LevelID](e)},
				"unknown level, extra":      {c.ID, b.ID, a.ID, newID[domain.LevelID](e)},
				"empty":                     nil,
			} {
				err := e.write(func(ctx context.Context, tx store.Tx) error { return tx.Levels().SetOrder(ctx, order) })
				if !errorsIs(err, domain.ErrValidation) {
					e.t.Errorf("%s: want ErrValidation, got %v", name, err)
				}
			}

			// A valid order in a read-only transaction.
			err := e.read(func(ctx context.Context, tx store.Tx) error {
				return tx.Levels().SetOrder(ctx, []domain.LevelID{c.ID, b.ID, a.ID})
			})
			wantErr(e.t, err, store.ErrReadOnly)

			wantSlice(e.t, e.levels(), before)
		},
		"LockScale": func(e *env) {
			err := e.read(func(ctx context.Context, tx store.Tx) error { return tx.Levels().LockScale(ctx) })
			wantErr(e.t, err, store.ErrReadOnly)
			e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Levels().LockScale(ctx) })
		},
		"SetOrderWithoutLevels": func(e *env) {
			for _, order := range [][]domain.LevelID{nil, {}} {
				e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Levels().SetOrder(ctx, order) })
			}

			err := e.write(func(ctx context.Context, tx store.Tx) error {
				return tx.Levels().SetOrder(ctx, []domain.LevelID{newID[domain.LevelID](e)})
			})
			wantErr(e.t, err, domain.ErrValidation)
			wantSlice(e.t, e.levels(), nil)
		},
	})
}

// level creates a level; code may be empty.
func (e *env) level(name, code string, position int) domain.Level {
	e.t.Helper()

	l := domain.Level{ID: newID[domain.LevelID](e), Name: name, Code: code, Position: position}
	valid(e.t, l)
	e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Levels().Create(ctx, l) })

	return l
}

func (e *env) getLevel(id domain.LevelID) domain.Level {
	e.t.Helper()

	return mustQuery(e, func(ctx context.Context, tx store.Tx) (domain.Level, error) { return tx.Levels().Get(ctx, id) })
}

func (e *env) levels() []domain.Level {
	e.t.Helper()

	return mustQuery(e, func(ctx context.Context, tx store.Tx) ([]domain.Level, error) { return tx.Levels().List(ctx) })
}
