package servicetest

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/service"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// Events runs the event scenarios (ADR 0019) on stores from newStore: the
// event's lock serialises the capacity checks of its registrations.
func Events(t *testing.T, newStore func(t *testing.T) store.Store) {
	t.Helper()

	scenarios := map[string]func(t *testing.T, w *world){
		"LastPlaceOnce": eventLastPlace,
		"Lifecycle":     eventLifecycle,
	}

	for name, run := range scenarios {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			run(t, newWorld(t, newStore(t)))
		})
	}
}

// events returns the event service of the world at now.
func (w *world) events() *service.EventService {
	mails := service.Mails{Renderer: w.renderer(), AppURL: "https://school.example.org/"}
	clk := clock.NewFixed(now)

	return service.NewEventService(w.st, clk, w.seq, mails, service.NewReceiptService(w.st, clk, w.seq, mails, stubPDF{}))
}

// eventLifecycle runs the office's event administration through the
// store: two events of one title get two slugs; an event gets a session,
// an image, is published, takes a person and a guest, and their fees (the
// guest's receipt has no payer); a new capacity reaches the session; a
// refunded registration and the receipt views name the event; canceling
// the event refunds the rest, cancels the session and audits it; a draft
// with a session is deleted with it.
func eventLifecycle(t *testing.T, w *world) {
	ctx := t.Context()
	svc := w.events()
	data := service.EventData{
		Title: "Imaginary Intensive", CourseTypeID: w.courseType, Capacity: 2, Price: 4500, VATRate: 1900,
	}

	e, err := svc.Create(ctx, w.office, data)
	if err != nil {
		t.Fatal(err)
	}

	draft, err := svc.Create(ctx, w.office, data)
	if err != nil || draft.Event.Slug != "imaginary-intensive-2" {
		t.Fatalf("second event %+v, %v", draft.Event, err)
	}

	id := e.Event.ID
	session := service.SessionData{
		Date: domain.Date{Year: 2026, Month: time.October, Day: 30}, StartTime: domain.TimeOfDay{Hour: 10},
		Duration: time.Hour, RoomID: w.room, InstructorID: w.instructor,
	}

	sl, err := svc.AddSession(ctx, id, session)
	if err != nil {
		t.Fatal(err)
	}

	img := domain.Background{ContentType: domain.BackgroundPNG, Data: []byte("\x89PNG\r\n\x1a\nimage")}
	if err := svc.PutImage(ctx, id, img); err != nil {
		t.Fatal(err)
	}

	if _, err := svc.Publish(ctx, id); err != nil {
		t.Fatal(err)
	}

	ada := w.person("Ada", "Dancer", "ada@example.org")

	person, err := svc.AddRegistration(ctx, w.office, id, service.NewRegistration{PersonID: ada.ID})
	if err != nil {
		t.Fatal(err)
	}

	guest, err := svc.AddRegistration(ctx, w.office, id, service.NewRegistration{
		GuestName: "Gina Guest", GuestEmail: "gina@example.org",
	})
	if err != nil {
		t.Fatal(err)
	}

	for _, r := range []service.RegistrationView{person, guest} {
		if _, err := svc.MarkPaid(ctx, w.office, r.Registration.ID, service.EventPayment{}); err != nil {
			t.Fatal(err)
		}
	}

	data.Capacity = 3
	if _, err := svc.Update(ctx, id, data); err != nil {
		t.Fatal(err)
	}

	if l := read(w, func(ctx context.Context, tx store.Tx) (domain.Lesson, error) {
		return tx.Lessons().Get(ctx, sl.Lesson.ID)
	}); l.Capacity != 3 {
		t.Errorf("session capacity %d, want 3", l.Capacity)
	}

	refunded, err := svc.CancelRegistration(ctx, w.office, person.Registration.ID, true)
	if err != nil || refunded.Registration.Payment != domain.EventRefunded {
		t.Fatalf("refunded %+v, %v", refunded, err)
	}

	receipts := service.NewReceiptService(w.st, clock.NewFixed(now), w.seq, service.Mails{}, stubPDF{})

	storno, err := receipts.Get(ctx, refunded.Registration.CancellationReceiptID)
	if err != nil || storno.EventID != id || storno.EventTitle != "Imaginary Intensive" || storno.EventStarted {
		t.Errorf("Storno view %+v, %v", storno, err)
	}

	got, err := svc.Cancel(ctx, w.office, id, "The guest teacher is ill.")
	if err != nil || got.Event.Status != domain.EventCanceled || got.Sessions[0].Lesson.Status != domain.LessonCanceled {
		t.Fatalf("canceled %+v, %v", got, err)
	}

	regs, err := svc.ListRegistrations(ctx, id)
	if err != nil || len(regs) != 2 || regs[1].Registration.Payment != domain.EventRefunded ||
		regs[1].Registration.Status != domain.EventRegistrationCanceled {
		t.Errorf("registrations %+v, %v", regs, err)
	}

	audits := read(w, func(ctx context.Context, tx store.Tx) ([]domain.AuditEvent, error) {
		return tx.Audit().List(ctx, 100)
	})
	if len(audits) != 6 || audits[0].Action != domain.AuditEventCanceled {
		t.Errorf("audits %+v", audits)
	}

	if _, err := svc.AddSession(ctx, draft.Event.ID, session); err != nil {
		t.Fatal(err)
	}

	if err := svc.Delete(ctx, draft.Event.ID); err != nil {
		t.Fatal(err)
	}

	list, err := svc.List(ctx)
	if err != nil || len(list) != 1 || list[0].Event.ID != id {
		t.Errorf("events %+v, %v", list, err)
	}
}

// eventLastPlace: two registrations for the last place of an event at
// once: exactly one gets it, the other is told the event is full, and the
// event has one registration.
func eventLastPlace(t *testing.T, w *world) {
	ctx := t.Context()
	svc := w.events()

	e, err := svc.Create(ctx, w.office, service.EventData{
		Title: "Imaginary Intensive", CourseTypeID: w.courseType, Capacity: 1,
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := svc.AddSession(ctx, e.Event.ID, service.SessionData{
		Date: domain.Date{Year: 2026, Month: time.October, Day: 30}, StartTime: domain.TimeOfDay{Hour: 10},
		Duration: time.Hour, RoomID: w.room, InstructorID: w.instructor,
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := svc.Publish(ctx, e.Event.ID); err != nil {
		t.Fatal(err)
	}

	persons := []domain.Person{w.person("Ada", "Dancer", ""), w.person("Ben", "Dancer", "")}
	errs := make([]error, len(persons))

	var wg sync.WaitGroup

	for i, p := range persons {
		wg.Go(func() {
			_, errs[i] = svc.AddRegistration(ctx, w.office, e.Event.ID, service.NewRegistration{PersonID: p.ID})
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

	regs, err := svc.ListRegistrations(ctx, e.Event.ID)
	if err != nil || len(regs) != 1 {
		t.Errorf("registrations %+v, %v", regs, err)
	}
}
