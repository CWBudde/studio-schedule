package memory

import (
	"cmp"
	"context"
	"fmt"
	"strings"

	"github.com/cwbudde/studio-schedule/internal/domain"
)

type levelRepo struct{ tx *tx }

func (r levelRepo) Get(_ context.Context, id domain.LevelID) (domain.Level, error) {
	l, ok := r.tx.state.levels.get(id)
	if !ok {
		return domain.Level{}, notFound("level")
	}

	return l, nil
}

func (r levelRepo) Create(_ context.Context, l domain.Level) error {
	if err := r.tx.checkWritable(); err != nil {
		return err
	}

	if r.tx.state.levels.has(l.ID) {
		return conflict("level %s exists", l.ID)
	}

	return r.put(l)
}

func (r levelRepo) Update(_ context.Context, l domain.Level) error {
	if err := r.tx.checkWritable(); err != nil {
		return err
	}

	old, ok := r.tx.state.levels.get(l.ID)
	if !ok {
		return notFound("level")
	}

	// Only SetOrder writes the position.
	l.Position = old.Position

	return r.put(l)
}

func (r levelRepo) put(l domain.Level) error {
	s := &r.tx.state
	if s.levels.exists(func(o domain.Level) bool { return o.ID != l.ID && sameName(o.Name, l.Name) }) {
		return fmt.Errorf("level name is taken: %w", domain.ErrDuplicate)
	}

	if l.Code != "" &&
		s.levels.exists(func(o domain.Level) bool { return o.ID != l.ID && sameName(o.Code, l.Code) }) {
		return fmt.Errorf("level code is taken: %w", domain.ErrDuplicate)
	}

	s.levels.put(r.tx.gen, l.ID, l)

	return nil
}

func (r levelRepo) Delete(_ context.Context, id domain.LevelID) error {
	s := &r.tx.state
	if err := r.tx.checkWritable(); err != nil {
		return err
	}

	switch {
	case !s.levels.has(id):
		return notFound("level")
	case s.classes.exists(func(c domain.Class) bool { return c.Levels.From == id || c.Levels.To == id }):
		return conflict("level %s is referenced", id)
	}

	s.levels.delete(r.tx.gen, id)

	return nil
}

func (r levelRepo) List(context.Context) ([]domain.Level, error) {
	return sorted(r.tx.state.levels.filter(all), func(a, b domain.Level) int {
		return cmp.Or(cmp.Compare(a.Position, b.Position), strings.Compare(a.Name, b.Name), compareID(a.ID, b.ID))
	}), nil
}

func (r levelRepo) SetOrder(_ context.Context, order []domain.LevelID) error {
	s := &r.tx.state
	if err := r.tx.checkWritable(); err != nil {
		return err
	}

	open := make(map[domain.LevelID]bool, len(s.levels.rows))
	for id := range s.levels.rows {
		open[id] = true
	}

	for _, id := range order {
		if !open[id] {
			return fmt.Errorf("level order: %s is not a level or repeated: %w", id, domain.ErrValidation)
		}

		delete(open, id)
	}

	if len(open) > 0 {
		return fmt.Errorf("level order: %d levels are missing: %w", len(open), domain.ErrValidation)
	}

	for i, id := range order {
		l, _ := s.levels.get(id)
		l.Position = i + 1
		s.levels.put(r.tx.gen, id, l)
	}

	return nil
}

// LockScale needs no lock of its own: the memory store serializes all
// transactions.
func (r levelRepo) LockScale(context.Context) error { return r.tx.checkWritable() }
