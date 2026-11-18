package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/mail"
	"github.com/cwbudde/studio-schedule/internal/store"
)

func TestEventInviteExpiryOffersPlaceToNextMember(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newEventFixture(t)
	svc := f.events(t)
	anna := f.principal(t, f.anna)
	d := f.eventData()
	d.Capacity = 1
	e, _ := f.published(t, d)
	if _, err := svc.JoinWaitlist(ctx, anna, e.ID, f.mia.ID); !errors.Is(err, ErrEventNotFull) {
		t.Fatalf("join when a place is free: %v", err)
	}

	occupant := f.register(t, e, NewRegistration{PersonID: f.mia.ID})
	first, err := svc.JoinWaitlist(ctx, anna, e.ID, f.tom.ID)
	if err != nil {
		t.Fatal(err)
	}
	sven, svenPrincipal := f.stranger(t)
	second, err := svc.JoinWaitlist(ctx, svenPrincipal, e.ID, sven.ID)
	if err != nil {
		t.Fatal(err)
	}
	firstID, secondID := first.Registration.Registration.ID, second.Registration.Registration.ID
	if _, err := svc.CancelOwn(ctx, anna, occupant.ID); err != nil {
		t.Fatal(err)
	}
	invited := f.registration(t, firstID)
	if invited.Status != domain.EventInvited || invited.InviteTokenHash == [32]byte{} {
		t.Fatalf("first registration was not invited: %+v", invited)
	}
	if got := f.registration(t, secondID).Status; got != domain.EventWaiting {
		t.Fatalf("second registration: %s", got)
	}

	later := f.eventsAt(t, invited.ExpiresAt.Format(time.RFC3339))
	if _, err := later.AcceptInvite(ctx, anna, firstID); !errors.Is(err, ErrInviteExpired) {
		t.Fatalf("late accept: %v", err)
	}
	if got := f.registration(t, firstID); got.Status != domain.EventRegistrationCanceled || got.InviteTokenHash != [32]byte{} {
		t.Fatalf("first invite should be canceled: %+v", got)
	}
	if got := f.registration(t, secondID).Status; got != domain.EventInvited {
		t.Fatalf("second registration should be invited: %s", got)
	}
	if n, err := later.ExpireDueInvites(ctx); n != 0 || err != nil {
		t.Fatalf("repeat expiry: %d, %v", n, err)
	}
}

func TestEventWaitlistOffersOnChanges(t *testing.T) {
	t.Parallel()

	for _, action := range []string{"office cancel", "refund", "increase capacity", "decline", "cancel event"} {
		t.Run(action, func(t *testing.T) {
			t.Parallel()

			ctx := t.Context()
			f := newEventFixture(t)
			svc := f.events(t)
			anna := f.principal(t, f.anna)
			data := f.eventData()
			data.Capacity = 1
			event, _ := f.published(t, data)
			occupied := f.register(t, event, NewRegistration{PersonID: f.mia.ID})

			unreachable := f.person(t, "Pat", "", "", "")
			unreachableRegistration := svc.newRegistration(anna, event, svc.clock.Now())
			unreachableRegistration.PersonID, unreachableRegistration.Status = unreachable.ID, domain.EventWaiting
			if err := f.st.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
				return tx.EventRegistrations().Create(ctx, unreachableRegistration)
			}); err != nil {
				t.Fatal(err)
			}

			first, err := svc.JoinWaitlist(ctx, anna, event.ID, f.tom.ID)
			if err != nil {
				t.Fatal(err)
			}
			sven, member := f.stranger(t)
			second, err := svc.JoinWaitlist(ctx, member, event.ID, sven.ID)
			if err != nil {
				t.Fatal(err)
			}
			firstID, secondID := first.Registration.Registration.ID, second.Registration.Registration.ID

			wantFirst, wantSecond := domain.EventInvited, domain.EventWaiting
			wantMail := 1

			switch action {
			case "increase capacity":
				data.Capacity = 2
				_, err = svc.Update(ctx, event.ID, data)
			case "refund":
				if _, err = svc.MarkPaid(ctx, f.office, occupied.ID, EventPayment{}); err != nil {
					t.Fatal(err)
				}
				_, err = svc.CancelRegistration(ctx, f.office, occupied.ID, true)
			case "office cancel":
				_, err = svc.CancelRegistration(ctx, f.office, occupied.ID, false)
			case "decline":
				if _, err = svc.CancelOwn(ctx, anna, occupied.ID); err != nil {
					t.Fatal(err)
				}
				_, err = svc.DeclineInvite(ctx, anna, firstID)
				wantFirst, wantSecond, wantMail = domain.EventRegistrationCanceled, domain.EventInvited, 2
			case "cancel event":
				_, err = svc.Cancel(ctx, f.office, event.ID, "")
				wantFirst, wantSecond, wantMail = domain.EventRegistrationCanceled, domain.EventRegistrationCanceled, 0
			}
			if err != nil {
				t.Fatal(err)
			}

			if got := f.registration(t, firstID); got.Status != wantFirst {
				t.Errorf("first registration is %s want %s", got.Status, wantFirst)
			}
			if got := f.registration(t, secondID); got.Status != wantSecond {
				t.Errorf("second registration is %s want %s", got.Status, wantSecond)
			}
			if got := f.registration(t, unreachableRegistration.ID).Status; action != "cancel event" && got != domain.EventWaiting {
				t.Errorf("unreachable registration is %s want waiting", got)
			}
			if got := len(f.mailsOf(t, mail.TemplateEventInvite)); got != wantMail {
				t.Errorf("%d invite mails, want %d", got, wantMail)
			}
		})
	}
}

func TestCancelEventRefundsPreviouslyCanceledRegistration(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newEventFixture(t)
	svc := f.events(t)
	e, _ := f.published(t, f.eventData())
	reg := f.register(t, e, NewRegistration{PersonID: f.mia.ID})
	paid, err := svc.MarkPaid(ctx, f.office, reg.ID, EventPayment{})
	if err != nil {
		t.Fatal(err)
	}
	canceled, err := svc.CancelRegistration(ctx, f.office, reg.ID, false)
	if err != nil {
		t.Fatal(err)
	}

	later := f.eventsAt(t, "2026-10-22T12:00:00Z")
	if _, err := later.Cancel(ctx, f.office, e.ID, ""); err != nil {
		t.Fatal(err)
	}
	got := f.registration(t, reg.ID)
	if got.Payment != domain.EventRefunded || got.CancellationReceiptID.IsZero() ||
		got.ReceiptID != paid.Receipt.ID || !got.CanceledAt.Equal(canceled.Registration.CanceledAt) {
		t.Fatalf("previously canceled registration: %+v", got)
	}
}
