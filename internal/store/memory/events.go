package memory

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// eventImage is the stored image of an event (a row of event_images) with
// its digest, computed when it is written.
type eventImage struct {
	EventID domain.EventID
	Image   domain.Background
	Digest  string
}

// ---------------------------------------------------------------- events

type eventRepo struct{ tx *tx }

func (r eventRepo) Get(_ context.Context, id domain.EventID) (domain.Event, error) {
	e, ok := r.tx.state.events.get(id)
	if !ok {
		return domain.Event{}, notFound("event")
	}

	return e, nil
}

func (r eventRepo) BySlug(_ context.Context, slug string) (domain.Event, error) {
	found := r.tx.state.events.filter(func(e domain.Event) bool { return e.Slug == slug })
	if len(found) == 0 {
		return domain.Event{}, notFound("event")
	}

	return found[0], nil
}

func (r eventRepo) Create(_ context.Context, e domain.Event) error {
	if err := r.tx.checkWritable(); err != nil {
		return err
	}

	if r.tx.state.events.has(e.ID) {
		return conflict("event %s exists", e.ID)
	}

	return r.put(e)
}

func (r eventRepo) Update(_ context.Context, e domain.Event) error {
	if err := r.tx.checkWritable(); err != nil {
		return err
	}

	if !r.tx.state.events.has(e.ID) {
		return notFound("event")
	}

	return r.put(e)
}

func (r eventRepo) put(e domain.Event) error {
	s := &r.tx.state
	e.SignupOpensAt, e.SignupClosesAt = instant(e.SignupOpensAt), instant(e.SignupClosesAt)
	e.CancelUntil, e.CreatedAt, e.UpdatedAt = instant(e.CancelUntil), instant(e.CreatedAt), instant(e.UpdatedAt)

	switch {
	case !s.courseTypes.has(e.CourseTypeID) || (!e.CreatedBy.IsZero() && !s.accounts.has(e.CreatedBy)):
		return conflict("event references a missing record")
	case s.events.exists(func(o domain.Event) bool { return o.ID != e.ID && o.Slug == e.Slug }):
		return fmt.Errorf("the event slug %q is taken: %w", e.Slug, domain.ErrDuplicate)
	}

	s.events.put(r.tx.gen, e.ID, e)

	return nil
}

func (r eventRepo) Delete(_ context.Context, id domain.EventID) error {
	s := &r.tx.state
	if err := r.tx.checkWritable(); err != nil {
		return err
	}

	switch {
	case !s.events.has(id):
		return notFound("event")
	case s.lessons.exists(func(l domain.Lesson) bool { return l.EventID == id }),
		s.eventRegistrations.exists(func(reg domain.EventRegistration) bool { return reg.EventID == id }):
		return fmt.Errorf("event %s has sessions or registrations: %w", id, domain.ErrInUse)
	}

	s.eventImages.delete(r.tx.gen, id)
	s.events.delete(r.tx.gen, id)

	return nil
}

func (r eventRepo) List(context.Context) ([]domain.Event, error) {
	s := &r.tx.state

	// The start of each event's first session, canceled ones included.
	first := map[domain.EventID]time.Time{}

	for _, l := range s.lessons.rows {
		if l.EventID.IsZero() {
			continue
		}

		if at, ok := first[l.EventID]; !ok || l.StartsAt.Before(at) {
			first[l.EventID] = l.StartsAt
		}
	}

	return sorted(s.events.filter(all), func(a, b domain.Event) int {
		at, aok := first[a.ID]
		bt, bok := first[b.ID]

		switch {
		case aok && bok:
			return cmp.Or(at.Compare(bt), compareID(a.ID, b.ID))
		case aok != bok:
			// Events without a session come last.
			if aok {
				return -1
			}

			return 1
		}

		return compareID(a.ID, b.ID)
	}), nil
}

// Lock needs no lock of its own: the store mutex already serialises every
// transaction.
func (r eventRepo) Lock(ctx context.Context, id domain.EventID) (domain.Event, error) {
	if err := r.tx.checkWritable(); err != nil {
		return domain.Event{}, err
	}

	return r.Get(ctx, id)
}

func (r eventRepo) Image(_ context.Context, id domain.EventID) (domain.Background, error) {
	if !r.tx.state.events.has(id) {
		return domain.Background{}, notFound("event")
	}

	img, ok := r.tx.state.eventImages.get(id)
	if !ok {
		return domain.Background{}, nil
	}

	b := img.Image
	b.Data = slices.Clone(b.Data)

	return b, nil
}

func (r eventRepo) PutImage(_ context.Context, id domain.EventID, img domain.Background) error {
	s := &r.tx.state
	if err := r.tx.checkWritable(); err != nil {
		return err
	}

	if !s.events.has(id) {
		return notFound("event")
	}

	if img.IsZero() {
		s.eventImages.delete(r.tx.gen, id)

		return nil
	}

	img.Data = slices.Clone(img.Data)
	s.eventImages.put(r.tx.gen, id, eventImage{EventID: id, Image: img, Digest: img.Digest()})

	return nil
}

func (r eventRepo) ImageInfo(_ context.Context, id domain.EventID) (domain.BackgroundInfo, error) {
	if !r.tx.state.events.has(id) {
		return domain.BackgroundInfo{}, notFound("event")
	}

	img, ok := r.tx.state.eventImages.get(id)
	if !ok {
		return domain.BackgroundInfo{}, nil
	}

	return domain.BackgroundInfo{ContentType: img.Image.ContentType, Digest: img.Digest}, nil
}

// ---------------------------------------------------------------- event registrations

type eventRegistrationRepo struct{ tx *tx }

// byCreated orders registrations by CreatedAt and ID, the order of an
// event's waiting list.
func byCreated(a, b domain.EventRegistration) int {
	return cmp.Or(a.CreatedAt.Compare(b.CreatedAt), compareID(a.ID, b.ID))
}

func (r eventRegistrationRepo) Get(_ context.Context, id domain.EventRegistrationID) (domain.EventRegistration, error) {
	reg, ok := r.tx.state.eventRegistrations.get(id)
	if !ok {
		return domain.EventRegistration{}, notFound("event registration")
	}

	return reg, nil
}

func (r eventRegistrationRepo) first(pred func(domain.EventRegistration) bool) (domain.EventRegistration, error) {
	found := r.tx.state.eventRegistrations.filter(pred)
	if len(found) == 0 {
		return domain.EventRegistration{}, notFound("event registration")
	}

	return found[0], nil
}

func (r eventRegistrationRepo) GetByInviteTokenHash(
	_ context.Context, hash [32]byte,
) (domain.EventRegistration, error) {
	return r.first(func(reg domain.EventRegistration) bool {
		return hash != [32]byte{} && reg.InviteTokenHash == hash
	})
}

func (r eventRegistrationRepo) GetByReceipt(
	_ context.Context, receipt domain.ReceiptID,
) (domain.EventRegistration, error) {
	return r.first(func(reg domain.EventRegistration) bool {
		return !receipt.IsZero() && reg.ReceiptID == receipt
	})
}

func (r eventRegistrationRepo) Create(_ context.Context, reg domain.EventRegistration) error {
	if err := r.tx.checkWritable(); err != nil {
		return err
	}

	if r.tx.state.eventRegistrations.has(reg.ID) {
		return conflict("event registration %s exists", reg.ID)
	}

	return r.put(reg)
}

func (r eventRegistrationRepo) Update(_ context.Context, reg domain.EventRegistration) error {
	if err := r.tx.checkWritable(); err != nil {
		return err
	}

	if !r.tx.state.eventRegistrations.has(reg.ID) {
		return notFound("event registration")
	}

	return r.put(reg)
}

func (r eventRegistrationRepo) put(reg domain.EventRegistration) error {
	s := &r.tx.state
	reg.InvitedAt, reg.ExpiresAt, reg.CanceledAt = instant(reg.InvitedAt), instant(reg.ExpiresAt), instant(reg.CanceledAt)
	reg.CreatedAt, reg.UpdatedAt = instant(reg.CreatedAt), instant(reg.UpdatedAt)

	// other reports whether another registration satisfies pred.
	other := func(pred func(o domain.EventRegistration) bool) bool {
		return s.eventRegistrations.exists(func(o domain.EventRegistration) bool { return o.ID != reg.ID && pred(o) })
	}

	switch {
	case !s.events.has(reg.EventID) ||
		(!reg.PersonID.IsZero() && !s.persons.has(reg.PersonID)) ||
		(!reg.ReceiptID.IsZero() && !s.receipts.has(reg.ReceiptID)) ||
		(!reg.CancellationReceiptID.IsZero() && !s.receipts.has(reg.CancellationReceiptID)) ||
		(!reg.RegisteredBy.IsZero() && !s.accounts.has(reg.RegisteredBy)):
		return conflict("event registration references a missing record")
	case reg.InviteTokenHash != [32]byte{} &&
		other(func(o domain.EventRegistration) bool { return o.InviteTokenHash == reg.InviteTokenHash }):
		return conflict("invite token hash is taken")
	case !reg.ReceiptID.IsZero() && other(func(o domain.EventRegistration) bool { return o.ReceiptID == reg.ReceiptID }):
		return conflict("receipt %s belongs to another registration", reg.ReceiptID)
	case !reg.CancellationReceiptID.IsZero() && other(func(o domain.EventRegistration) bool {
		return o.CancellationReceiptID == reg.CancellationReceiptID
	}):
		return conflict("Storno %s belongs to another registration", reg.CancellationReceiptID)
	case reg.Open() && !reg.PersonID.IsZero() && other(func(o domain.EventRegistration) bool {
		return o.EventID == reg.EventID && o.PersonID == reg.PersonID && o.Open()
	}):
		return fmt.Errorf("person is registered for the event already: %w", domain.ErrDuplicate)
	}

	s.eventRegistrations.put(r.tx.gen, reg.ID, reg)

	return nil
}

func (r eventRegistrationRepo) ListByEvent(
	_ context.Context, event domain.EventID,
) ([]domain.EventRegistration, error) {
	return sorted(r.tx.state.eventRegistrations.filter(func(reg domain.EventRegistration) bool {
		return reg.EventID == event
	}), byCreated), nil
}

func (r eventRegistrationRepo) ListByPersons(
	_ context.Context, persons []domain.PersonID,
) ([]domain.EventRegistration, error) {
	wanted := setOf(persons)

	return sorted(r.tx.state.eventRegistrations.filter(func(reg domain.EventRegistration) bool {
		_, ok := wanted[reg.PersonID]

		return ok && !reg.PersonID.IsZero()
	}), byCreated), nil
}

func (r eventRegistrationRepo) CountsByEvents(
	_ context.Context, events []domain.EventID, at time.Time,
) (map[domain.EventID]store.EventRegistrationCounts, error) {
	at = instant(at)
	wanted := setOf(events)
	out := map[domain.EventID]store.EventRegistrationCounts{}

	for _, reg := range r.tx.state.eventRegistrations.rows {
		if _, ok := wanted[reg.EventID]; !ok {
			continue
		}

		c := out[reg.EventID]

		switch {
		case reg.Status == domain.EventRegistered:
			c.Registered++
		case reg.Status == domain.EventInvited && reg.ExpiresAt.After(at):
			c.Invited++
		case reg.Status == domain.EventWaiting:
			c.Waiting++
		default:
			continue
		}

		out[reg.EventID] = c
	}

	return out, nil
}

func (r eventRegistrationRepo) AnonymizeGuests(_ context.Context, endedBefore time.Time) (int, error) {
	s := &r.tx.state
	if err := r.tx.checkWritable(); err != nil {
		return 0, err
	}

	endedBefore = instant(endedBefore)

	// The end of each event's last session, canceled ones included.
	last := map[domain.EventID]time.Time{}

	for _, l := range s.lessons.rows {
		if !l.EventID.IsZero() && l.EndsAt.After(last[l.EventID]) {
			last[l.EventID] = l.EndsAt
		}
	}

	guests := s.eventRegistrations.filter(func(reg domain.EventRegistration) bool {
		if !reg.PersonID.IsZero() || (reg.GuestName == domain.AnonymousGuestName && reg.GuestEmail == "") {
			return false
		}

		end, ok := last[reg.EventID]

		return ok && end.Before(endedBefore)
	})

	for _, reg := range guests {
		reg.GuestName, reg.GuestEmail = domain.AnonymousGuestName, ""
		s.eventRegistrations.put(r.tx.gen, reg.ID, reg)
	}

	return len(guests), nil
}
