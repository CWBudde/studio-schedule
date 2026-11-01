package service

import (
	"strings"
	"testing"

	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/domain"
)

// faultyEvents returns the event service and the receipt service at now on
// a FaultStore over the fixture's store.
func (f *eventFixture) faultyEvents(t *testing.T, now string) (*FaultStore, *EventService, *ReceiptService) {
	t.Helper()

	fs := NewFaultStore(f.st)
	clk := clock.NewFixed(mustInstant(t, now))
	receipts := NewReceiptService(fs, clk, f.seq, testMails(newTestRenderer(t)), f.pdf)

	return fs, NewEventService(fs, clk, f.seq, testMails(newTestRenderer(t)), receipts), receipts
}

// eventScene is a published priced event with two sessions ahead, Mia
// registered and paid, the guest and Tom registered and unpaid, and an
// image; another published event without registrations; and a draft with
// a session.
type eventScene struct {
	event    domain.Event
	sessions []domain.Lesson
	mia      domain.EventRegistration
	guest    domain.EventRegistration
	tom      domain.EventRegistration
	bare     domain.Event
	draft    domain.Event
}

func (f *eventFixture) newEventScene(t *testing.T) eventScene {
	t.Helper()

	var s eventScene

	s.event, s.sessions = f.published(t, f.eventData())
	s.mia = f.paid(t, f.register(t, s.event, NewRegistration{PersonID: f.mia.ID}))
	s.guest = f.register(t, s.event, NewRegistration{GuestName: "Gina Guest", GuestEmail: "gina@example.org"})
	s.tom = f.register(t, s.event, NewRegistration{PersonID: f.tom.ID, ConfirmOverCapacity: true})
	s.bare, _ = f.publishedAt(t, f.eventData(), 10)

	img := domain.Background{ContentType: domain.BackgroundPNG, Data: []byte("\x89PNG\r\n\x1a\nimage")}
	if err := f.events(t).PutImage(t.Context(), s.event.ID, img); err != nil {
		t.Fatal(err)
	}

	draft, err := f.events(t).Create(t.Context(), f.office, f.eventData())
	if err != nil {
		t.Fatal(err)
	}

	s.draft = draft.Event

	if _, err := f.events(t).AddSession(t.Context(), s.draft.ID, f.sessionAt(t, "2026-10-26", 18)); err != nil {
		t.Fatal(err)
	}

	return s
}

// TestEventStoreFaults: a store failure in any of the office's event
// operations is returned and changes nothing.
func TestEventStoreFaults(t *testing.T) {
	t.Parallel()

	type op func(t *testing.T, f *eventFixture, s eventScene, svc *EventService, receipts *ReceiptService) error

	ops := map[string]op{
		"Create": func(t *testing.T, f *eventFixture, _ eventScene, svc *EventService, _ *ReceiptService) error {
			_, err := svc.Create(t.Context(), f.office, f.eventData())

			return err
		},
		"Update": func(t *testing.T, f *eventFixture, s eventScene, svc *EventService, _ *ReceiptService) error {
			d := f.eventData()
			d.Capacity = 5
			_, err := svc.Update(t.Context(), s.event.ID, d)

			return err
		},
		"Delete": func(t *testing.T, _ *eventFixture, s eventScene, svc *EventService, _ *ReceiptService) error {
			return svc.Delete(t.Context(), s.draft.ID)
		},
		"Publish": func(t *testing.T, _ *eventFixture, s eventScene, svc *EventService, _ *ReceiptService) error {
			_, err := svc.Publish(t.Context(), s.draft.ID)

			return err
		},
		"Unpublish": func(t *testing.T, _ *eventFixture, s eventScene, svc *EventService, _ *ReceiptService) error {
			_, err := svc.Unpublish(t.Context(), s.bare.ID)

			return err
		},
		"List": func(t *testing.T, _ *eventFixture, _ eventScene, svc *EventService, _ *ReceiptService) error {
			_, err := svc.List(t.Context())

			return err
		},
		"Get": func(t *testing.T, _ *eventFixture, s eventScene, svc *EventService, _ *ReceiptService) error {
			_, err := svc.Get(t.Context(), s.event.ID)

			return err
		},
		"Image": func(t *testing.T, _ *eventFixture, s eventScene, svc *EventService, _ *ReceiptService) error {
			_, err := svc.Image(t.Context(), s.event.ID)

			return err
		},
		"PutImage": func(t *testing.T, _ *eventFixture, s eventScene, svc *EventService, _ *ReceiptService) error {
			return svc.PutImage(t.Context(), s.draft.ID,
				domain.Background{ContentType: domain.BackgroundPNG, Data: []byte("\x89PNG\r\n\x1a\nother")})
		},
		"DeleteImage": func(t *testing.T, _ *eventFixture, s eventScene, svc *EventService, _ *ReceiptService) error {
			return svc.DeleteImage(t.Context(), s.event.ID)
		},
		"AddSession": func(t *testing.T, f *eventFixture, s eventScene, svc *EventService, _ *ReceiptService) error {
			_, err := svc.AddSession(t.Context(), s.event.ID, f.sessionAt(t, "2026-10-27", 12))

			return err
		},
		"AddRegistration": func(t *testing.T, f *eventFixture, s eventScene, svc *EventService, _ *ReceiptService) error {
			_, err := svc.AddRegistration(t.Context(), f.office, s.event.ID, NewRegistration{
				PersonID: f.anna.ID, ConfirmOverCapacity: true,
			})

			return err
		},
		"ListRegistrations": func(t *testing.T, _ *eventFixture, s eventScene, svc *EventService, _ *ReceiptService) error {
			_, err := svc.ListRegistrations(t.Context(), s.event.ID)

			return err
		},
		"CancelRegistration": func(t *testing.T, f *eventFixture, s eventScene, svc *EventService, _ *ReceiptService) error {
			_, err := svc.CancelRegistration(t.Context(), f.office, s.mia.ID, true)

			return err
		},
		"MarkPaid": func(t *testing.T, f *eventFixture, s eventScene, svc *EventService, _ *ReceiptService) error {
			_, err := svc.MarkPaid(t.Context(), f.office, s.guest.ID, EventPayment{})

			return err
		},
		"MarkPaid person": func(t *testing.T, f *eventFixture, s eventScene, svc *EventService, _ *ReceiptService) error {
			_, err := svc.MarkPaid(t.Context(), f.office, s.tom.ID, EventPayment{PayerID: f.anna.ID})

			return err
		},
		"Cancel": func(t *testing.T, f *eventFixture, s eventScene, svc *EventService, _ *ReceiptService) error {
			_, err := svc.Cancel(t.Context(), f.office, s.event.ID, "Canceled")

			return err
		},
		"CancelReceipt": func(t *testing.T, f *eventFixture, s eventScene, _ *EventService, receipts *ReceiptService) error {
			_, err := receipts.Cancel(t.Context(), f.office, s.mia.ReceiptID, "")

			return err
		},
		"ReceiptView": func(t *testing.T, _ *eventFixture, s eventScene, _ *EventService, receipts *ReceiptService) error {
			_, err := receipts.Get(t.Context(), s.mia.ReceiptID)

			return err
		},
	}

	for name, op := range ops {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			SweepFaults(t, FaultSweep{Setup: func(t *testing.T) (*FaultStore, func() error) {
				f := newEventFixture(t)
				s := f.newEventScene(t)
				fs, svc, receipts := f.faultyEvents(t, bookingNow)

				return fs, func() error { return op(t, f, s, svc, receipts) }
			}})
		})
	}
}

// TestSessionLessonStoreFaults: a store failure while the lesson
// administration edits, deletes or cancels a session is returned and
// changes nothing.
func TestSessionLessonStoreFaults(t *testing.T) {
	t.Parallel()

	for name, op := range map[string]func(t *testing.T, f *eventFixture, svc *LessonService, l domain.Lesson) error{
		"UpdateLesson": func(t *testing.T, _ *eventFixture, svc *LessonService, l domain.Lesson) error {
			d := dataOf(t, l)
			d.StartTime = domain.TimeOfDay{Hour: 19}
			_, err := svc.UpdateLesson(t.Context(), l.ID, d)

			return err
		},
		"DeleteLesson": func(t *testing.T, _ *eventFixture, svc *LessonService, l domain.Lesson) error {
			return svc.DeleteLesson(t.Context(), l.ID)
		},
		"CancelLesson": func(t *testing.T, f *eventFixture, svc *LessonService, l domain.Lesson) error {
			_, err := svc.CancelLesson(t.Context(), f.office, l.ID, "")

			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			SweepFaults(t, FaultSweep{Setup: func(t *testing.T) (*FaultStore, func() error) {
				f := newEventFixture(t)
				e, sessions := f.published(t, f.eventData())

				// Deleting needs no open registration; the others mail
				// the registered.
				if name != "DeleteLesson" {
					f.register(t, e, NewRegistration{PersonID: f.mia.ID})
					f.register(t, e, NewRegistration{GuestName: "Gina Guest", GuestEmail: "gina@example.org"})
				}

				fs := NewFaultStore(f.st)
				svc := NewLessonService(fs, clock.NewFixed(mustInstant(t, bookingNow)), f.seq,
					testMails(newTestRenderer(t)))

				return fs, func() error { return op(t, f, svc, sessions[0]) }
			}})
		})
	}
}

// TestEventRendererNotReady: a renderer that cannot get ready fails taking
// a fee, a refunding cancellation and the cancellation of an event with
// paid registrations before their write transaction.
func TestEventRendererNotReady(t *testing.T) {
	t.Parallel()

	f := newEventFixture(t)
	s := f.newEventScene(t)
	fs := NewFaultStore(f.st)
	clk := clock.NewFixed(mustInstant(t, bookingNow))
	svc := NewEventService(fs, clk, f.seq, testMails(newTestRenderer(t)),
		NewReceiptService(fs, clk, f.seq, Mails{}, &unreadyPDF{}))

	for name, op := range map[string]func() error{
		"MarkPaid": func() error {
			_, err := svc.MarkPaid(t.Context(), f.office, s.guest.ID, EventPayment{})

			return err
		},
		"CancelRegistration": func() error {
			_, err := svc.CancelRegistration(t.Context(), f.office, s.mia.ID, true)

			return err
		},
		"Cancel": func() error {
			_, err := svc.Cancel(t.Context(), f.office, s.event.ID, "")

			return err
		},
	} {
		before := fs.Count("InTx")

		if err := op(); err == nil || !strings.Contains(err.Error(), "module download failed") {
			t.Errorf("%s = %v, want the renderer's failure", name, err)
		}

		// Cancel reads whether it refunds first.
		if n := fs.Count("InTx") - before; n != map[string]int{"Cancel": 1}[name] {
			t.Errorf("%s: %d transactions", name, n)
		}
	}

	// Without a refund, nothing is rendered.
	if _, err := svc.CancelRegistration(t.Context(), f.office, s.guest.ID, false); err != nil {
		t.Errorf("CancelRegistration without a refund: %v", err)
	}

	if _, err := svc.Cancel(t.Context(), f.office, s.bare.ID, ""); err != nil {
		t.Errorf("Cancel without a fee paid: %v", err)
	}
}
