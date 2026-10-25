package memory

import (
	"cmp"
	"context"
	"fmt"
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
)

// ---------------------------------------------------------------- waitlist

type waitlistRepo struct{ tx *tx }

// byJoined orders entries by JoinedAt and ID, the order of the list.
func byJoined(a, b domain.WaitlistEntry) int {
	return cmp.Or(a.JoinedAt.Compare(b.JoinedAt), compareID(a.ID, b.ID))
}

func (r waitlistRepo) Get(_ context.Context, id domain.WaitlistEntryID) (domain.WaitlistEntry, error) {
	e, ok := r.tx.state.waitlist.get(id)
	if !ok {
		return domain.WaitlistEntry{}, notFound("waitlist entry")
	}

	return e, nil
}

func (r waitlistRepo) GetByTokenHash(_ context.Context, hash [32]byte) (domain.WaitlistEntry, error) {
	found := r.tx.state.waitlist.filter(func(e domain.WaitlistEntry) bool {
		return e.InviteTokenHash != [32]byte{} && e.InviteTokenHash == hash
	})
	if len(found) == 0 {
		return domain.WaitlistEntry{}, notFound("waitlist entry")
	}

	return found[0], nil
}

func (r waitlistRepo) Create(_ context.Context, e domain.WaitlistEntry) error {
	if err := r.tx.checkWritable(); err != nil {
		return err
	}

	if r.tx.state.waitlist.has(e.ID) {
		return conflict("waitlist entry %s exists", e.ID)
	}

	return r.put(e)
}

func (r waitlistRepo) Update(_ context.Context, e domain.WaitlistEntry) error {
	if err := r.tx.checkWritable(); err != nil {
		return err
	}

	if !r.tx.state.waitlist.has(e.ID) {
		return notFound("waitlist entry")
	}

	return r.put(e)
}

func (r waitlistRepo) put(e domain.WaitlistEntry) error {
	s := &r.tx.state
	e.JoinedAt, e.InvitedAt, e.ExpiresAt = instant(e.JoinedAt), instant(e.InvitedAt), instant(e.ExpiresAt)
	hasToken := e.InviteTokenHash != [32]byte{}

	switch {
	case !s.lessons.has(e.LessonID) || !s.persons.has(e.PersonID) ||
		(!e.JoinedBy.IsZero() && !s.accounts.has(e.JoinedBy)):
		return conflict("waitlist entry references a missing record")
	case hasToken && s.waitlist.exists(func(o domain.WaitlistEntry) bool {
		return o.ID != e.ID && o.InviteTokenHash == e.InviteTokenHash
	}):
		return conflict("invite token hash is taken")
	case e.Open() && s.waitlist.exists(func(o domain.WaitlistEntry) bool {
		return o.ID != e.ID && o.LessonID == e.LessonID && o.PersonID == e.PersonID && o.Open()
	}):
		return fmt.Errorf("person is on the waiting list of the lesson already: %w", domain.ErrDuplicate)
	}

	s.waitlist.put(r.tx.gen, e.ID, e)

	return nil
}

func (r waitlistRepo) ListByLesson(_ context.Context, lesson domain.LessonID) ([]domain.WaitlistEntry, error) {
	return sorted(r.tx.state.waitlist.filter(func(e domain.WaitlistEntry) bool { return e.LessonID == lesson }), byJoined), nil
}

func (r waitlistRepo) ListOpenByLessons(_ context.Context, lessons []domain.LessonID) ([]domain.WaitlistEntry, error) {
	wanted := setOf(lessons)

	return sorted(r.tx.state.waitlist.filter(func(e domain.WaitlistEntry) bool {
		_, ok := wanted[e.LessonID]

		return ok && e.Open()
	}), byJoined), nil
}

func (r waitlistRepo) CountByLessons(_ context.Context, lessons []domain.LessonID) (map[domain.LessonID]int, error) {
	wanted := setOf(lessons)
	out := map[domain.LessonID]int{}

	for _, e := range r.tx.state.waitlist.filter(func(e domain.WaitlistEntry) bool {
		_, ok := wanted[e.LessonID]

		return ok
	}) {
		out[e.LessonID]++
	}

	return out, nil
}

func (r waitlistRepo) ListOpenByPersons(_ context.Context, persons []domain.PersonID) ([]domain.WaitlistEntry, error) {
	wanted := setOf(persons)

	return sorted(r.tx.state.waitlist.filter(func(e domain.WaitlistEntry) bool {
		_, ok := wanted[e.PersonID]

		return ok && e.Open()
	}), byJoined), nil
}

func (r waitlistRepo) ListDueLessons(_ context.Context, now time.Time) ([]domain.LessonID, error) {
	now = instant(now)
	due := map[domain.LessonID]struct{}{}

	for _, e := range r.tx.state.waitlist.filter(domain.WaitlistEntry.Open) {
		l, ok := r.tx.state.lessons.get(e.LessonID)

		switch {
		case e.Status == domain.WaitlistInvited && !e.ExpiresAt.After(now):
			due[e.LessonID] = struct{}{}
		case ok && !l.StartsAt.After(now):
			due[e.LessonID] = struct{}{}
		}
	}

	out := make([]domain.LessonID, 0, len(due))
	for id := range due {
		out = append(out, id)
	}

	return sorted(out, compareID[domain.LessonID]), nil
}
