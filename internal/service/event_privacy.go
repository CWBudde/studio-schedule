package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/store"
)

var errRegistrationEventsChanged = fmt.Errorf("%w: the person's event registrations changed", domain.ErrConflict)

// A registration may have committed between reading the affected events
// and locking the persons. Retry from the event locks in a new transaction.
func personEventsTx(ctx context.Context, st store.Store, write func(context.Context, store.Tx) error) error {
	for range 5 {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := st.InTx(ctx, store.ReadWrite, write)
		if !errors.Is(err, errRegistrationEventsChanged) {
			return err
		}
	}

	return errRegistrationEventsChanged
}

func lockPersonEvents(
	ctx context.Context, tx store.Tx, includeCanceled bool, ids ...domain.PersonID,
) (map[domain.EventID]domain.Event, error) {
	regs, err := tx.EventRegistrations().ListByPersons(ctx, ids)
	if err != nil {
		return nil, err
	}

	events := make(map[domain.EventID]domain.Event)
	for _, reg := range regs {
		if includeCanceled || reg.Open() {
			events[reg.EventID] = domain.Event{}
		}
	}
	for _, id := range sortedEventIDs(events) {
		events[id], err = tx.Events().Lock(ctx, id)
		if err != nil {
			return nil, err
		}
	}

	return events, nil
}

func sortedEventIDs[V any](events map[domain.EventID]V) []domain.EventID {
	return slices.SortedFunc(maps.Keys(events), func(a, b domain.EventID) int { return bytes.Compare(a[:], b[:]) })
}

func lockedPersonRegistrations(
	ctx context.Context, tx store.Tx, events map[domain.EventID]domain.Event, includeCanceled bool, ids ...domain.PersonID,
) ([]domain.EventRegistration, error) {
	regs, err := tx.EventRegistrations().ListByPersons(ctx, ids)
	if err != nil {
		return nil, err
	}
	if !includeCanceled {
		regs = slices.DeleteFunc(regs, func(r domain.EventRegistration) bool { return !r.Open() })
	}

	for _, reg := range regs {
		if _, locked := events[reg.EventID]; !locked {
			return nil, errRegistrationEventsChanged
		}
	}

	return regs, nil
}

func (s *PersonService) cancelFutureEventRegistrations(
	ctx context.Context, tx store.Tx, events map[domain.EventID]domain.Event, regs []domain.EventRegistration, now time.Time,
) error {
	if len(regs) == 0 {
		return nil
	}
	sessions, err := tx.Lessons().ListByEvents(ctx, sortedEventIDs(events))
	if err != nil {
		return err
	}
	started := make(map[domain.EventID]bool)
	for _, session := range sessions {
		if session.Status == domain.LessonScheduled && !session.StartsAt.After(now) {
			started[session.EventID] = true
		}
	}

	freed := make(map[domain.EventID]domain.Event)
	for _, reg := range regs {
		if started[reg.EventID] {
			continue
		}

		freesPlace := reg.Status == domain.EventRegistered || reg.Status == domain.EventInvited
		if err := cancelEventRegistration(ctx, tx, &reg, now); err != nil {
			return err
		}
		if freesPlace {
			freed[reg.EventID] = events[reg.EventID]
		}
	}

	return s.offerFreedEventPlaces(ctx, tx, freed, now)
}

func (s *PersonService) mergeEventRegistrations(
	ctx context.Context, tx store.Tx, events map[domain.EventID]domain.Event, regs []domain.EventRegistration,
	survivor, merged domain.PersonID, now time.Time,
) error {
	survivorOpen := make(map[domain.EventID]bool)
	for _, reg := range regs {
		if reg.PersonID == survivor && reg.Open() {
			survivorOpen[reg.EventID] = true
		}
	}

	freed := make(map[domain.EventID]domain.Event)
	for _, reg := range regs {
		if reg.PersonID != merged {
			continue
		}
		reg.PersonID, reg.UpdatedAt = survivor, now
		if reg.Open() && survivorOpen[reg.EventID] {
			if reg.Status == domain.EventRegistered || reg.Status == domain.EventInvited {
				freed[reg.EventID] = events[reg.EventID]
			}
			if err := cancelEventRegistration(ctx, tx, &reg, now); err != nil {
				return err
			}
		} else if err := tx.EventRegistrations().Update(ctx, reg); err != nil {
			return err
		}
	}

	return s.offerFreedEventPlaces(ctx, tx, freed, now)
}

func (s *PersonService) offerFreedEventPlaces(
	ctx context.Context, tx store.Tx, freed map[domain.EventID]domain.Event, now time.Time,
) error {
	waitlist := EventService{ids: s.ids, mails: s.w.mails}
	for _, id := range sortedEventIDs(freed) {
		if err := waitlist.offerEventPlaces(ctx, tx, freed[id], now); err != nil {
			return err
		}
	}

	return nil
}
