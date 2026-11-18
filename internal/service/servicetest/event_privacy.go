package servicetest

import (
	"context"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/service"
	"github.com/cwbudde/studio-schedule/internal/store"
)

func eventPrivacyAnonymize(t *testing.T, w *world) {
	ctx := t.Context()
	svc := w.events()
	ada := w.person("Ada", "Example", "")
	ben := w.person("Ben", "Example", "")
	member := w.memberOf(ben, "ben@example.org")
	account := w.account(ada, "ada@example.org")
	started := w.publicEvent(svc, "Morning Practice", 1, 10, false)

	event, err := svc.Create(ctx, w.office, service.EventData{
		Title: "Afternoon Intensive", CourseTypeID: w.courseType, Capacity: 1, Price: 4500, VATRate: 1900,
	})
	if err != nil {
		t.Fatal(err)
	}
	session, err := svc.AddSession(ctx, event.Event.ID, service.SessionData{
		Date: domain.Date{Year: 2026, Month: time.October, Day: 30}, StartTime: domain.TimeOfDay{Hour: 14},
		Duration: time.Hour, RoomID: w.room, InstructorID: w.instructor,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Publish(ctx, event.Event.ID); err != nil {
		t.Fatal(err)
	}

	add := func(id domain.EventID) domain.EventRegistration {
		t.Helper()

		view, err := svc.AddRegistration(ctx, w.office, id, service.NewRegistration{PersonID: ada.ID})
		if err != nil {
			t.Fatal(err)
		}

		return view.Registration
	}
	old, future := add(started.ID), add(event.Event.ID)
	// Seed a registration made by the account that's about to be erased.
	future.RegisteredBy = account.ID
	w.write(func(ctx context.Context, tx store.Tx) error {
		return tx.EventRegistrations().Update(ctx, future)
	})
	paid, err := svc.MarkPaid(ctx, w.office, future.ID, service.EventPayment{})
	if err != nil {
		t.Fatal(err)
	}
	waiting, err := svc.JoinWaitlist(ctx, member, event.Event.ID, ben.ID)
	if err != nil {
		t.Fatal(err)
	}
	exported, err := w.persons().Export(ctx, ada.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(exported.EventRegistrations) != 2 ||
		exported.EventRegistrations[1].ReceiptNumber != paid.Receipt.Number ||
		exported.EventRegistrations[1].Title != event.Event.Title {
		t.Fatalf("exported event registrations: %+v", exported.EventRegistrations)
	}

	people := service.NewPersonService(w.st, clock.NewFixed(session.Lesson.StartsAt.Add(-time.Hour)), w.seq,
		service.Mails{Renderer: w.renderer(), AppURL: "https://school.example.org/"})
	if _, err := people.Anonymize(ctx, w.office, ada.ID); err != nil {
		t.Fatal(err)
	}
	inspect := func(id domain.EventRegistrationID) domain.EventRegistration {
		return read(w, func(ctx context.Context, tx store.Tx) (domain.EventRegistration, error) {
			return tx.EventRegistrations().Get(ctx, id)
		})
	}
	if got := inspect(future.ID); got.Status != domain.EventRegistrationCanceled || got.Payment != domain.EventPaid ||
		got.ReceiptID != paid.Receipt.ID || !got.CancellationReceiptID.IsZero() || !got.RegisteredBy.IsZero() {
		t.Errorf("anonymized future registration: %+v", got)
	}
	if got := inspect(old.ID); got.Status != domain.EventRegistered {
		t.Errorf("anonymized started registration: %+v", got)
	}
	if got := inspect(waiting.Registration.Registration.ID); got.Status != domain.EventInvited {
		t.Errorf("the released place was not offered: %+v", got)
	}
	views, err := svc.ListRegistrations(ctx, started.ID)
	if err != nil || len(views) != 1 || !views[0].Anonymized || views[0].Name != "Deleted person" {
		t.Errorf("anonymized event registrant: %+v, %v", views, err)
	}
}

func eventPrivacyMerge(t *testing.T, w *world) {
	ctx := t.Context()
	svc := w.events()
	both := w.publicEvent(svc, "Morning Practice", 2, 10, false)
	one := w.publicEvent(svc, "Afternoon Practice", 2, 14, false)
	survivor, duplicate := w.person("Ada", "Example", ""), w.person("Ada", "Duplicate", "")

	add := func(id domain.EventID, p domain.PersonID) domain.EventRegistrationID {
		t.Helper()

		v, err := svc.AddRegistration(ctx, w.office, id, service.NewRegistration{PersonID: p})
		if err != nil {
			t.Fatal(err)
		}

		return v.Registration.ID
	}
	keep, cancel := add(both.ID, survivor.ID), add(both.ID, duplicate.ID)
	move := add(one.ID, duplicate.ID)
	people := w.persons()
	req := service.MergeRequest{SurvivorID: survivor.ID, MergedID: duplicate.ID}
	if _, err := people.Merge(ctx, w.office, req, false); err != nil {
		t.Fatal(err)
	}

	reg := func(id domain.EventRegistrationID) domain.EventRegistration {
		return read(w, func(ctx context.Context, tx store.Tx) (domain.EventRegistration, error) {
			return tx.EventRegistrations().Get(ctx, id)
		})
	}
	if got := reg(cancel); got.PersonID != duplicate.ID || got.Status != domain.EventRegistered {
		t.Fatalf("preview changed registration: %+v", got)
	}
	if _, err := people.Merge(ctx, w.office, req, true); err != nil {
		t.Fatal(err)
	}
	for id, status := range map[domain.EventRegistrationID]domain.EventRegistrationStatus{
		keep: domain.EventRegistered, cancel: domain.EventRegistrationCanceled, move: domain.EventRegistered,
	} {
		if got := reg(id); got.PersonID != survivor.ID || got.Status != status {
			t.Errorf("merged registration: %+v, want %s for %v", got, status, survivor.ID)
		}
	}
}

func eventPrivacyRetention(t *testing.T, w *world) {
	ctx := t.Context()
	svc := w.events()
	event := w.publicEvent(svc, "Imaginary Intensive", 1, 10, false)
	guest, err := svc.AddRegistration(ctx, w.office, event.ID, service.NewRegistration{
		GuestName: "Gina Example", GuestEmail: "gina@example.org",
	})
	if err != nil {
		t.Fatal(err)
	}
	detail, err := svc.Get(ctx, event.ID)
	if err != nil {
		t.Fatal(err)
	}
	cutoff := detail.Sessions[0].Lesson.EndsAt.AddDate(0, 12, 0)
	for _, tc := range []struct {
		now  time.Time
		want int
	}{{cutoff, 0}, {cutoff.Add(time.Second), 1}, {cutoff.Add(time.Second), 0}} {
		purge, err := service.NewRetentionService(w.st, clock.NewFixed(tc.now)).Purge(ctx)
		if err != nil || purge.EventGuests != tc.want {
			t.Fatalf("retention at %s: %+v, %v", tc.now, purge, err)
		}
	}
	got := read(w, func(ctx context.Context, tx store.Tx) (domain.EventRegistration, error) {
		return tx.EventRegistrations().Get(ctx, guest.Registration.ID)
	})
	if got.GuestName != domain.AnonymousGuestName || got.GuestEmail != "" {
		t.Errorf("retained registration: %+v", got)
	}
}
