package service

import (
	"context"
	"fmt"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// TimetableService provides read access to the terms and their weekly
// classes.
type TimetableService struct {
	store store.Store
}

// NewTimetableService returns a TimetableService backed by st.
func NewTimetableService(st store.Store) *TimetableService {
	return &TimetableService{store: st}
}

// Terms returns all terms ordered by start date.
func (s *TimetableService) Terms(ctx context.Context) ([]domain.Term, error) {
	var terms []domain.Term

	err := s.store.InTx(ctx, store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		var err error
		terms, err = tx.Terms().List(ctx)

		return err
	})
	if err != nil {
		return nil, fmt.Errorf("list terms: %w", err)
	}

	return terms, nil
}

// Classes returns the weekly classes of a term ordered by weekday (Sunday
// first) and start time. An unknown term is domain.ErrNotFound.
func (s *TimetableService) Classes(ctx context.Context, term domain.TermID) ([]domain.Class, error) {
	var classes []domain.Class

	err := s.store.InTx(ctx, store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		// The class list cannot tell an unknown term from one without
		// classes.
		if _, err := tx.Terms().Get(ctx, term); err != nil {
			return err
		}

		var err error
		classes, err = tx.Classes().ListByTerm(ctx, term)

		return err
	})
	if err != nil {
		return nil, fmt.Errorf("list classes of term %s: %w", term, err)
	}

	return classes, nil
}
