package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// The office maintains the terms (ADR 0007). A term's new date range takes
// its classes' lessons along in the same transaction; see fitLessons. The
// HTTP layer admits only office and admin accounts to these methods.

// TermData are the fields the office edits on a term.
type TermData struct {
	Name     string
	StartsOn domain.Date
	EndsOn   domain.Date
}

// CreateTerm adds a term. It has no classes yet, so no lessons either.
func (s *TimetableService) CreateTerm(ctx context.Context, d TermData) (domain.Term, error) {
	t := d.term(ids.New[domain.TermID](s.ids))

	err := s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		if err := t.Validate(); err != nil {
			return err
		}

		return tx.Terms().Create(ctx, t)
	})
	if err != nil {
		return domain.Term{}, fmt.Errorf("create term: %w", err)
	}

	return t, nil
}

// UpdateTerm replaces a term's name and dates. The lessons of its classes
// that the new range leaves out are deleted while they lie ahead and have
// no bookings; a lesson left out that has started or is booked is a
// validation error on the date that leaves it out (fitLessons). A longer
// range gets the lessons of the rolling horizon that have not started.
func (s *TimetableService) UpdateTerm(ctx context.Context, id domain.TermID, d TermData) (domain.Term, error) {
	t := d.term(id)

	err := s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		// An unknown term is a 404 before anything about its fields. The
		// lock keeps class edits and lesson generation, which read the
		// term's dates, out until its lessons fit them.
		old, err := tx.Terms().Lock(ctx, id)
		if err != nil {
			return err
		}

		if err := t.Validate(); err != nil {
			return err
		}

		if err := tx.Terms().Update(ctx, t); err != nil {
			return err
		}

		if t.StartsOn == old.StartsOn && t.EndsOn == old.EndsOn {
			return nil // a new name changes no lesson
		}

		return s.fitLessons(ctx, tx, t)
	})
	if err != nil {
		return domain.Term{}, fmt.Errorf("update term %s: %w", id, err)
	}

	return t, nil
}

// DeleteTerm deletes a term without classes. A term with classes is
// domain.ErrInUse: its classes are deleted or retired one by one, and the
// term ends by its date (ADR 0007).
func (s *TimetableService) DeleteTerm(ctx context.Context, id domain.TermID) error {
	err := s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		return tx.Terms().Delete(ctx, id)
	})
	// The store refuses to delete a term that classes refer to.
	if errors.Is(err, domain.ErrConflict) {
		err = fmt.Errorf("%w (%w)", domain.ErrInUse, err)
	}

	if err != nil {
		return fmt.Errorf("delete term %s: %w", id, err)
	}

	return nil
}

// fitLessons brings the lessons of the classes of term t in line with its
// date range, then generates their missing lessons of the rolling horizon
// that have not started yet, as after a class edit. The caller has locked
// t.
//
// A lesson whose slot lies outside the range is deleted while it lies
// ahead and has no bookings, whether it follows its class, was edited one
// by one (detached) or is canceled: nobody is booked into it, so nobody
// misses it (ADR 0007). A lesson outside that has started or is booked
// refuses the new range with a validation error on startsOn or endsOn,
// whichever leaves it out: history never ends up outside its term, and the
// office handles the bookings first.
func (s *TimetableService) fitLessons(ctx context.Context, tx store.Tx, t domain.Term) error {
	settings, loc, err := loadSchool(ctx, tx)
	if errors.Is(err, ErrNoSchool) {
		return nil // without a school there are no lessons
	} else if err != nil {
		return err
	}

	now := s.clock.Now()

	from, to, err := horizon(ctx, tx, settings, domain.DateOf(now, loc))
	if err != nil {
		return err
	}

	classes, err := tx.Classes().ListByTerm(ctx, t.ID)
	if err != nil {
		return fmt.Errorf("list classes of term %s: %w", t.ID, err)
	}

	for _, c := range classes {
		// The lock keeps lesson generation and class edits out.
		if c, err = tx.Classes().Lock(ctx, c.ID); err != nil {
			return err
		}

		lessons, err := tx.Lessons().ListByClass(ctx, c.ID, domain.Date{})
		if err != nil {
			return fmt.Errorf("list lessons of class %s: %w", c.ID, err)
		}

		for _, l := range lessons {
			if field := outside(t, l.SlotDate); field != "" {
				if err := leave(ctx, tx, l.ID, field, now); err != nil {
					return err
				}
			}
		}

		if _, err := s.gen.fill(ctx, tx, c, t, from, to, loc, now); err != nil {
			return err
		}
	}

	return nil
}

// outside returns the date of term t that leaves the slot date out:
// "startsOn" or "endsOn", or "" for a date within the term.
func outside(t domain.Term, d domain.Date) string {
	switch {
	case d.Compare(t.StartsOn) < 0:
		return "startsOn"
	case d.Compare(t.EndsOn) > 0:
		return "endsOn"
	}

	return ""
}

// leave deletes lesson id, which the term's field leaves out, or refuses
// the term with a validation error on field; see fitLessons. It locks the
// lesson first, as every booking check must.
func leave(ctx context.Context, tx store.Tx, id domain.LessonID, field string, now time.Time) error {
	l, err := tx.Lessons().Lock(ctx, id)
	if err != nil {
		return err
	}

	booked, err := hasBookings(ctx, tx, l.ID)
	if err != nil {
		return err
	}

	var why string

	switch {
	case !l.StartsAt.After(now):
		why = "has started"
	case booked:
		why = "has bookings"
	default:
		return tx.Lessons().Delete(ctx, l.ID)
	}

	return &domain.FieldError{
		Field: field,
		Msg:   fmt.Sprintf("the lesson of %s would lie outside the term, but it %s", l.SlotDate, why),
	}
}

func (d TermData) term(id domain.TermID) domain.Term {
	return domain.Term{ID: id, Name: strings.TrimSpace(d.Name), StartsOn: d.StartsOn, EndsOn: d.EndsOn}
}
