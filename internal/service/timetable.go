package service

import (
	"context"
	"fmt"

	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// TimetableService provides the terms and their weekly classes, and the
// office's writes to the classes (class_admin.go).
type TimetableService struct {
	store store.Store
	clock clock.Clock
	ids   ids.Source
	gen   *LessonGenerator
}

// NewTimetableService returns a TimetableService backed by st.
func NewTimetableService(st store.Store, clk clock.Clock, src ids.Source) *TimetableService {
	return &TimetableService{store: st, clock: clk, ids: src, gen: NewLessonGenerator(st, clk, src)}
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

// TimetableClass is a weekly class with its staff, so the timetable can
// name them even after they were deactivated (ADR 0007). Accompanist is zero
// when there is none.
type TimetableClass struct {
	domain.Class

	Instructor, Accompanist domain.Staff
}

// Classes returns the weekly classes of a term with their staff, ordered by
// weekday (Sunday first) and start time, without retired ones. An unknown
// term is domain.ErrNotFound.
func (s *TimetableService) Classes(ctx context.Context, term domain.TermID) ([]TimetableClass, error) {
	return s.classes(ctx, term, false)
}

// classes is Classes, with the retired classes if withRetired is set.
func (s *TimetableService) classes(ctx context.Context, term domain.TermID, withRetired bool) ([]TimetableClass, error) {
	var classes []TimetableClass

	err := s.store.InTx(ctx, store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		// The class list cannot tell an unknown term from one without
		// classes.
		if _, err := tx.Terms().Get(ctx, term); err != nil {
			return err
		}

		list, err := tx.Classes().ListByTerm(ctx, term)
		if err != nil {
			return err
		}

		// One list of all staff, former ones included, instead of a lookup
		// per class.
		all, err := tx.Staff().List(ctx)
		if err != nil {
			return err
		}

		staff := make(map[domain.StaffID]domain.Staff, len(all))
		for _, m := range all {
			staff[m.ID] = m
		}

		classes = make([]TimetableClass, 0, len(list))
		for _, c := range list {
			if c.Retired && !withRetired {
				continue
			}

			classes = append(classes, TimetableClass{
				Class: c, Instructor: staff[c.InstructorID], Accompanist: staff[c.AccompanistID],
			})
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("list classes of term %s: %w", term, err)
	}

	return classes, nil
}
