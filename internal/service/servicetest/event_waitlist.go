package servicetest

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/service"
	"github.com/cwbudde/studio-schedule/internal/store"
)

func eventWaitlistExpiry(t *testing.T, w *world) {
	ctx := t.Context()
	svc := w.events()
	event := w.publicEvent(svc, "Imaginary Intensive", 1, 10, false)
	people := []domain.Person{
		w.person("Ada", "Dancer", ""),
		w.person("Ben", "Jumper", ""),
		w.person("Cal", "Spinner", ""),
	}
	members := []service.Principal{
		w.memberOf(people[0], "ada@example.org"),
		w.memberOf(people[1], "ben@example.org"),
		w.memberOf(people[2], "cal@example.org"),
	}

	occupied, err := svc.Register(ctx, members[0], event.ID, people[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	first, err := svc.JoinWaitlist(ctx, members[1], event.ID, people[1].ID)
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.JoinWaitlist(ctx, members[2], event.ID, people[2].ID)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := svc.CancelRegistration(ctx, w.office, occupied.Registration.Registration.ID, false); err != nil {
		t.Fatal(err)
	}

	firstID, secondID := first.Registration.Registration.ID, second.Registration.Registration.ID
	reg := func(id domain.EventRegistrationID) domain.EventRegistration {
		return read(w, func(ctx context.Context, tx store.Tx) (domain.EventRegistration, error) {
			return tx.EventRegistrations().Get(ctx, id)
		})
	}
	invited := reg(firstID)
	if invited.Status != domain.EventInvited || reg(secondID).Status != domain.EventWaiting {
		t.Fatalf("offers created out of order: %+v, %+v", invited, reg(secondID))
	}

	mails := service.Mails{Renderer: w.renderer(), AppURL: "https://school.example.org/"}
	later := service.NewEventService(w.st, clock.NewFixed(invited.ExpiresAt), w.seq, mails, nil)

	var wg sync.WaitGroup
	wg.Go(func() {
		if _, err := later.AcceptInvite(ctx, members[1], firstID); !errors.Is(err, service.ErrInviteExpired) {
			t.Errorf("accept at expiry: %v", err)
		}
	})
	wg.Go(func() {
		if _, err := later.ExpireDueInvites(ctx); err != nil {
			t.Errorf("expire due invites: %v", err)
		}
	})
	wg.Wait()

	if got := reg(firstID); got.Status != domain.EventRegistrationCanceled || got.InviteTokenHash != [32]byte{} {
		t.Errorf("expired invite: %+v", got)
	}
	if got := reg(secondID); got.Status != domain.EventInvited || !got.InvitedAt.Equal(invited.ExpiresAt) {
		t.Errorf("next offer: %+v", got)
	}
	if n, err := later.ExpireDueInvites(ctx); err != nil || n != 0 {
		t.Errorf("repeat expiry: %d, %v", n, err)
	}
	if _, err := later.Register(ctx, members[0], event.ID, people[0].ID); !errors.Is(err, service.ErrEventFull) {
		t.Errorf("open offer must reserve the place: %v", err)
	}
}

func eventDeskRespectsQueue(t *testing.T, w *world) {
	ctx := t.Context()
	svc := w.events()
	event := w.publicEvent(svc, "Imaginary Intensive", 1, 10, false)
	ada := w.person("Ada", "Dancer", "")
	ben := w.person("Ben", "Jumper", "")
	cal := w.person("Cal", "Spinner", "")
	benMember := w.memberOf(ben, "ben@example.org")
	calMember := w.memberOf(cal, "cal@example.org")
	occupied, err := svc.AddRegistration(ctx, w.office, event.ID, service.NewRegistration{PersonID: ada.ID})
	if err != nil {
		t.Fatal(err)
	}
	first, err := svc.JoinWaitlist(ctx, benMember, event.ID, ben.ID)
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.JoinWaitlist(ctx, calMember, event.ID, cal.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CancelRegistration(ctx, w.office, occupied.Registration.ID, false); err != nil {
		t.Fatal(err)
	}

	readReg := func(id domain.EventRegistrationID) domain.EventRegistration {
		return read(w, func(ctx context.Context, tx store.Tx) (domain.EventRegistration, error) {
			return tx.EventRegistrations().Get(ctx, id)
		})
	}
	expiry := readReg(first.Registration.Registration.ID).ExpiresAt
	mails := service.Mails{Renderer: w.renderer(), AppURL: "https://school.example.org/"}
	later := service.NewEventService(w.st, clock.NewFixed(expiry), w.seq, mails, nil)
	desk := service.NewRegistration{PersonID: ada.ID}
	if _, err := later.AddRegistration(ctx, w.office, event.ID, desk); !errors.Is(err, service.ErrEventFull) {
		t.Fatalf("unconfirmed desk registration after expiry: %v", err)
	}
	if got := readReg(second.Registration.Registration.ID); got.Status != domain.EventInvited {
		t.Fatalf("the next waiting person should hold the place: %+v", got)
	}
	desk.ConfirmOverCapacity = true
	if _, err := later.AddRegistration(ctx, w.office, event.ID, desk); err != nil {
		t.Fatalf("confirmed desk override: %v", err)
	}
}
