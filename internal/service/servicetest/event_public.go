package servicetest

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/service"
)

// memberOf gives p a member account with the email and returns its
// principal.
func (w *world) memberOf(p domain.Person, email string) service.Principal {
	w.t.Helper()

	acc := w.account(p, email)

	return service.Principal{AccountID: acc.ID, PersonID: p.ID, Email: acc.Email, Role: acc.Role}
}

// publicEvent creates an event of capacity places with a session on Friday
// 2026-10-30 at the hour, published unless draft.
func (w *world) publicEvent(svc *service.EventService, title string, capacity, hour int, draft bool) domain.Event {
	w.t.Helper()

	ctx := w.t.Context()

	e, err := svc.Create(ctx, w.office, service.EventData{
		Title: title, CourseTypeID: w.courseType, Capacity: capacity,
	})
	if err != nil {
		w.t.Fatal(err)
	}

	if _, err := svc.AddSession(ctx, e.Event.ID, service.SessionData{
		Date: domain.Date{Year: 2026, Month: time.October, Day: 30}, StartTime: domain.TimeOfDay{Hour: hour},
		Duration: time.Hour, RoomID: w.room, InstructorID: w.instructor,
	}); err != nil {
		w.t.Fatal(err)
	}

	if draft {
		return e.Event
	}

	got, err := svc.Publish(ctx, e.Event.ID)
	if err != nil {
		w.t.Fatal(err)
	}

	return got.Event
}

// eventMembersLastPlace (review focus 1): two members of two families
// register their own persons for the last place of an event at once:
// exactly one gets it, the other is told the event is full, and the event
// has one registration.
func eventMembersLastPlace(t *testing.T, w *world) {
	ctx := t.Context()
	svc := w.events()
	e := w.publicEvent(svc, "Imaginary Intensive", 1, 10, false)

	ada, ben := w.person("Ada", "Dancer", ""), w.person("Ben", "Jumper", "")
	members := map[domain.PersonID]service.Principal{
		ada.ID: w.memberOf(ada, "ada@example.org"),
		ben.ID: w.memberOf(ben, "ben@example.org"),
	}

	var (
		mu   sync.Mutex
		errs []error
		wg   sync.WaitGroup
	)

	for person, p := range members {
		wg.Go(func() {
			_, err := svc.Register(ctx, p, e.ID, person)

			mu.Lock()
			defer mu.Unlock()

			errs = append(errs, err)
		})
	}

	wg.Wait()

	won, full := 0, 0

	for _, err := range errs {
		switch {
		case err == nil:
			won++
		case errors.Is(err, service.ErrEventFull):
			full++
		default:
			t.Errorf("registration: %v", err)
		}
	}

	if won != 1 || full != 1 {
		t.Errorf("%d registered and %d refused, want one each", won, full)
	}

	regs, err := svc.ListRegistrations(ctx, e.ID)
	if err != nil || len(regs) != 1 {
		t.Errorf("registrations %+v, %v", regs, err)
	}

	got, err := svc.BySlug(ctx, e.Slug)
	if err != nil || got.FreePlaces != 0 || !got.SignupOpen {
		t.Errorf("event %+v, %v", got, err)
	}
}

// eventPrivacy (review focus 2): a draft is absent from the published
// events and its slug and image are not found; a canceled event is absent
// too, its page says canceled and it takes no registration.
func eventPrivacy(t *testing.T, w *world) {
	ctx := t.Context()
	svc := w.events()
	draft := w.publicEvent(svc, "Imaginary Draft", 5, 10, true)
	canceled := w.publicEvent(svc, "Imaginary Canceled", 5, 12, false)
	open := w.publicEvent(svc, "Imaginary Open", 5, 14, false)

	if _, err := svc.Cancel(ctx, w.office, canceled.ID, ""); err != nil {
		t.Fatal(err)
	}

	list, err := svc.Published(ctx)
	if err != nil || len(list) != 1 || list[0].Event.ID != open.ID {
		t.Errorf("published %+v, %v", list, err)
	}

	if _, err := svc.BySlug(ctx, draft.Slug); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("draft BySlug: %v, want ErrNotFound", err)
	}

	if _, err := svc.PublicImage(ctx, draft.Slug); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("draft PublicImage: %v, want ErrNotFound", err)
	}

	if _, err := svc.PublicEventID(ctx, draft.Slug); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("draft PublicEventID: %v, want ErrNotFound", err)
	}

	got, err := svc.BySlug(ctx, canceled.Slug)
	if err != nil || got.Event.Status != domain.EventCanceled || got.SignupOpen {
		t.Errorf("canceled %+v, %v", got, err)
	}

	ada := w.person("Ada", "Dancer", "")
	member := w.memberOf(ada, "ada@example.org")

	if _, err := svc.Register(ctx, member, canceled.ID, ada.ID); !errors.Is(err, service.ErrEventClosed) {
		t.Errorf("register for the canceled event: %v, want ErrEventClosed", err)
	}

	if _, err := svc.Register(ctx, member, draft.ID, ada.ID); !errors.Is(err, service.ErrEventClosed) {
		t.Errorf("register for the draft: %v, want ErrEventClosed", err)
	}

	mine, err := svc.Register(ctx, member, open.ID, ada.ID)
	if err != nil || mine.Event.FreePlaces != 4 || !mine.Cancelable {
		t.Errorf("register for the open event %+v, %v", mine, err)
	}
}
