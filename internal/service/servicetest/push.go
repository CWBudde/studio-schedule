package servicetest

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/mail"
	"github.com/cwbudde/studio-schedule/internal/service"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// pushAppURL is the app of the scenarios' links.
const pushAppURL = "https://school.example.org/"

// Push runs the web push scenarios (ADR 0017) on stores from newStore:
// devices subscribe and leave, a waiting-list invite queues a notification
// for every device of the invited person's accounts, and delivery records
// what the push services answer.
func Push(t *testing.T, newStore func(t *testing.T) store.Store) {
	t.Helper()

	scenarios := map[string]func(t *testing.T, w *world){
		"Subscribe":                   pushSubscribe,
		"InviteQueuesNotifications":   pushInvite,
		"NothingQueuedWhilePushIsOff": pushOff,
		"DeliverRecordsTheAnswers":    pushDeliver,
		"DeliverGivesUp":              pushGiveUp,
	}

	for name, run := range scenarios {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			run(t, newWorld(t, newStore(t)))
		})
	}
}

// fakeSender answers each endpoint with the error in answers (nil: sent)
// and records what it sent.
type fakeSender struct {
	mu      sync.Mutex
	answers map[string]error
	sent    map[string][]string
}

func (f *fakeSender) PublicKey() string { return "BPublicKey" }

func (f *fakeSender) Send(_ context.Context, sub domain.PushSubscription, payload []byte, ttl time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if ttl <= 0 {
		return errors.New("no ttl")
	}

	if f.sent == nil {
		f.sent = map[string][]string{}
	}

	f.sent[sub.Endpoint] = append(f.sent[sub.Endpoint], string(payload))

	return f.answers[sub.Endpoint]
}

func (f *fakeSender) count(endpoint string) int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return len(f.sent[endpoint])
}

// pushKeys are a valid P-256 public key and an auth secret in base64url.
const (
	pushP256DH = "BCVxsr7N_eNgVRqvHtD0zTZsEc6-VV-JvLexhqUzORcxaOzi6-AYWXvTBHm4bjyPjs7Vd8pZGH6SRpkNtoIAiw4"
	pushAuth   = "BTBZMqHH6r4Tts7J_aSIgg"
)

func (w *world) principal(a domain.Account) service.Principal {
	return service.Principal{AccountID: a.ID, PersonID: a.PersonID, Email: a.Email, Role: a.Role}
}

func (w *world) subscribe(svc *service.PushService, p service.Principal, endpoint string) {
	w.t.Helper()

	err := svc.Subscribe(w.t.Context(), p, service.PushSubscriptionInput{
		Endpoint: endpoint, P256DH: pushP256DH, Auth: pushAuth, DeviceLabel: "Firefox on Linux",
	})
	if err != nil {
		w.t.Fatal(err)
	}
}

func (w *world) pushSubs(a domain.AccountID) []domain.PushSubscription {
	w.t.Helper()

	return read(w, func(ctx context.Context, tx store.Tx) ([]domain.PushSubscription, error) {
		return tx.PushSubscriptions().ListByAccount(ctx, a)
	})
}

// pushMessages returns the messages of the subscriptions of account a
// that are due by at, in the order ListDue has them.
func (w *world) pushMessages(at time.Time) []domain.PushMessage {
	w.t.Helper()

	var out []domain.PushMessage

	w.write(func(ctx context.Context, tx store.Tx) error {
		var err error
		out, err = tx.PushOutbox().ListDue(ctx, at, 100)

		return err
	})

	return out
}

func (w *world) pushMessage(id domain.PushMessageID) (domain.PushMessage, bool) {
	w.t.Helper()

	var (
		m     domain.PushMessage
		found bool
	)

	err := w.st.InTx(w.t.Context(), store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		var err error
		m, err = tx.PushOutbox().Get(ctx, id)
		found = err == nil

		if errors.Is(err, domain.ErrNotFound) {
			return nil
		}

		return err
	})
	if err != nil {
		w.t.Fatal(err)
	}

	return m, found
}

func pushSubscribe(t *testing.T, w *world) {
	ctx := t.Context()
	anna := w.account(w.person("Anna", "Example", ""), "anna@example.net")
	bob := w.account(w.person("Bob", "Example", ""), "bob@example.net")
	svc := service.NewPushService(w.st, clock.NewFixed(now), w.seq, &fakeSender{})

	if s := svc.Status(); !s.Enabled || s.PublicKey != "BPublicKey" {
		t.Errorf("status %+v", s)
	}

	w.subscribe(svc, w.principal(anna), "https://push.example.net/push/phone")

	subs := w.pushSubs(anna.ID)
	if len(subs) != 1 || subs[0].DeviceLabel != "Firefox on Linux" || subs[0].LastUsedAt != now.UTC() {
		t.Fatalf("subscriptions %+v", subs)
	}

	// The same device again a day later: the same subscription, used again.
	later := service.NewPushService(w.st, clock.NewFixed(now.Add(24*time.Hour)), w.seq, &fakeSender{})
	w.subscribe(later, w.principal(anna), "https://push.example.net/push/phone")

	if again := w.pushSubs(anna.ID); len(again) != 1 || again[0].ID != subs[0].ID ||
		again[0].LastUsedAt != now.Add(24*time.Hour).UTC() {
		t.Fatalf("again %+v", again)
	}

	// Bob signs in on Anna's device: it is his now.
	w.subscribe(svc, w.principal(bob), "https://push.example.net/push/phone")

	if len(w.pushSubs(anna.ID)) != 0 || len(w.pushSubs(bob.ID)) != 1 {
		t.Fatalf("after the move: Anna %v, Bob %v", w.pushSubs(anna.ID), w.pushSubs(bob.ID))
	}

	// Leaving: only the own subscription.
	if err := svc.Unsubscribe(ctx, w.principal(anna), "https://push.example.net/push/phone"); err != nil {
		t.Fatal(err)
	}

	if len(w.pushSubs(bob.ID)) != 1 {
		t.Fatal("Anna removed Bob's subscription")
	}

	if err := svc.Unsubscribe(ctx, w.principal(bob), "https://push.example.net/push/phone"); err != nil {
		t.Fatal(err)
	}

	if len(w.pushSubs(bob.ID)) != 0 {
		t.Fatal("Bob's subscription stayed")
	}

	// Refusals.
	impersonated := w.principal(anna)
	impersonated.ImpersonatorID = w.office.AccountID

	input := service.PushSubscriptionInput{Endpoint: "https://push.example.net/push/x", P256DH: pushP256DH, Auth: pushAuth}
	if err := svc.Subscribe(ctx, impersonated, input); !errors.Is(err, service.ErrImpersonating) {
		t.Errorf("impersonating: want ErrImpersonating, got %v", err)
	}

	off := service.NewPushService(w.st, clock.NewFixed(now), w.seq, nil)
	if err := off.Subscribe(ctx, w.principal(anna), input); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("push off: want ErrNotFound, got %v", err)
	}

	if s := off.Status(); s.Enabled || s.PublicKey != "" {
		t.Errorf("push off: status %+v", s)
	}

	for field, in := range map[string]service.PushSubscriptionInput{
		"endpoint":    {Endpoint: "http://push.example.net/x", P256DH: pushP256DH, Auth: pushAuth},
		"keys.p256dh": {Endpoint: input.Endpoint, P256DH: "BA" + strings.Repeat("A", 85), Auth: pushAuth},
		"keys.auth":   {Endpoint: input.Endpoint, P256DH: pushP256DH, Auth: "AAAA"},
		"deviceLabel": {Endpoint: input.Endpoint, P256DH: pushP256DH, Auth: pushAuth, DeviceLabel: strings.Repeat("x", 101)},
	} {
		var fe *domain.FieldError
		if err := svc.Subscribe(ctx, w.principal(anna), in); !errors.As(err, &fe) || fe.Field != field {
			t.Errorf("%s: want a field error on it, got %v", field, err)
		}
	}

	if len(w.pushSubs(anna.ID)) != 0 {
		t.Error("a refused subscription was saved")
	}
}

// invitedFixture is a lesson of one place that a stranger booked, with
// Mia, Anna's daughter without an address of her own, on its waiting list,
// and Anna's two devices subscribed.
type invitedFixture struct {
	lesson   domain.Lesson
	stranger domain.Account
	booking  domain.Booking
	anna     domain.Account
	mia      domain.Person
	wakes    int
}

func newInvitedFixture(t *testing.T, w *world, push bool) (*invitedFixture, service.Mails) {
	t.Helper()

	f := &invitedFixture{lesson: w.lessons[0]}
	f.lesson.Capacity = 1
	w.write(func(ctx context.Context, tx store.Tx) error { return tx.Lessons().Update(ctx, f.lesson) })

	stella := w.person("Stella", "Stranger", "")
	f.stranger = w.account(stella, "stella@example.net")
	f.booking = w.booking(f.lesson, stella, false)

	annaPerson := w.person("Anna", "Example", "")
	f.anna = w.account(annaPerson, "anna@example.net")
	f.mia = w.person("Mia", "Example", "")
	w.link(annaPerson, f.mia)

	renderer, err := mail.NewRenderer()
	if err != nil {
		t.Fatal(err)
	}

	mails := service.Mails{Renderer: renderer, AppURL: pushAppURL}
	if push {
		mails.Push = service.PushQueue{On: true, Wake: func() { f.wakes++ }}
	}

	subs := service.NewPushService(w.st, clock.NewFixed(now), w.seq, &fakeSender{})
	w.subscribe(subs, w.principal(f.anna), "https://push.example.net/push/phone")
	w.subscribe(subs, w.principal(f.anna), "https://push.example.org/push/laptop")

	// Bob's device gets nothing.
	bob := w.account(w.person("Bob", "Example", ""), "bob@example.net")
	w.subscribe(subs, w.principal(bob), "https://push.example.net/push/bob")

	waitlist := service.NewWaitlistService(w.st, clock.NewFixed(now), w.seq, mails)
	if _, err := waitlist.Join(t.Context(), w.principal(f.anna), f.lesson.ID, f.mia.ID, false); err != nil {
		t.Fatalf("join: %v", err)
	}

	return f, mails
}

// cancel lets the stranger cancel, which invites Mia.
func (f *invitedFixture) cancel(t *testing.T, w *world, mails service.Mails) {
	t.Helper()

	bookings := service.NewBookingService(w.st, clock.NewFixed(now), w.seq, mails)
	if _, err := bookings.Cancel(t.Context(), w.principal(f.stranger), f.booking.ID); err != nil {
		t.Fatalf("cancel: %v", err)
	}
}

func pushInvite(t *testing.T, w *world) {
	f, mails := newInvitedFixture(t, w, true)
	f.cancel(t, w, mails)

	msgs := w.pushMessages(now)
	if len(msgs) != 2 || f.wakes == 0 {
		t.Fatalf("%d messages, %d wakes; want 2 and a wake", len(msgs), f.wakes)
	}

	annaSubs := map[domain.PushSubscriptionID]bool{}
	for _, s := range w.pushSubs(f.anna.ID) {
		annaSubs[s.ID] = true
	}

	entry := read(w, func(ctx context.Context, tx store.Tx) ([]domain.WaitlistEntry, error) {
		return tx.Waitlist().ListOpenByLessons(ctx, []domain.LessonID{f.lesson.ID})
	})
	if len(entry) != 1 || entry[0].Status != domain.WaitlistInvited {
		t.Fatalf("entries %+v", entry)
	}

	for _, m := range msgs {
		if !annaSubs[m.SubscriptionID] {
			t.Errorf("message to subscription %v, not one of Anna's", m.SubscriptionID)
		}

		if m.ExpiresAt != entry[0].ExpiresAt || m.CreatedAt != now.UTC() {
			t.Errorf("message expires %v (invite %v), created %v", m.ExpiresAt, entry[0].ExpiresAt, m.CreatedAt)
		}

		var p struct{ Title, Body, URL, Tag, Lang string }
		if err := json.Unmarshal([]byte(m.Payload), &p); err != nil {
			t.Fatal(err)
		}

		if p.Title != "A place is free: Imaginary Ballet" || !strings.Contains(p.Body, "Reserved until") ||
			!strings.HasPrefix(p.URL, pushAppURL+"waitlist/accept?token=") ||
			p.Tag != "waitlist-invite-"+entry[0].ID.String() || p.Lang != "en" {
			t.Errorf("payload %+v", p)
		}

		if strings.Contains(m.Payload, "Mia") || strings.Contains(m.Payload, "Anna") {
			t.Errorf("the payload names a person: %s", m.Payload)
		}
	}
}

func pushOff(t *testing.T, w *world) {
	f, mails := newInvitedFixture(t, w, false)
	f.cancel(t, w, mails)

	if msgs := w.pushMessages(now.Add(time.Hour)); len(msgs) != 0 {
		t.Fatalf("%d messages queued while push is off", len(msgs))
	}
}

func pushDeliver(t *testing.T, w *world) {
	f, mails := newInvitedFixture(t, w, true)
	f.cancel(t, w, mails)

	queued := w.pushMessages(now)

	sender := &fakeSender{answers: map[string]error{
		"https://push.example.org/push/laptop": service.ErrPushGone,
	}}
	later := now.Add(10 * time.Second)
	svc := service.NewPushService(w.st, clock.NewFixed(later), w.seq, sender)

	sent, err := svc.Deliver(t.Context())
	if err != nil || sent != 1 {
		t.Fatalf("deliver: %d, %v", sent, err)
	}

	if sender.count("https://push.example.net/push/phone") != 1 || sender.count("https://push.example.org/push/laptop") != 1 {
		t.Fatalf("sent %v", sender.sent)
	}

	// The phone's message is sent and the phone used; the laptop's
	// subscription is gone with its message.
	subs := w.pushSubs(f.anna.ID)
	if len(subs) != 1 || subs[0].Endpoint != "https://push.example.net/push/phone" || subs[0].LastUsedAt != later.UTC() {
		t.Fatalf("subscriptions %+v", subs)
	}

	for _, q := range queued {
		m, found := w.pushMessage(q.ID)

		switch {
		case q.SubscriptionID == subs[0].ID:
			if !found || m.SentAt != later.UTC() || !m.NextAttemptAt.IsZero() || m.Attempts != 1 {
				t.Errorf("phone message %+v", m)
			}
		case found:
			t.Errorf("the laptop's message stayed: %+v", m)
		}
	}

	if sent, err := svc.Deliver(t.Context()); err != nil || sent != 0 {
		t.Errorf("again: %d, %v", sent, err)
	}
}

func pushGiveUp(t *testing.T, w *world) {
	anna := w.account(w.person("Anna", "Example", ""), "anna@example.net")
	sender := &fakeSender{answers: map[string]error{
		"https://push.example.net/push/flaky":   errors.New("push service answered 500"),
		"https://push.example.net/push/soon":    errors.New("push service answered 500"),
		"https://push.example.net/push/refused": service.ErrPushRefused,
	}}
	subs := service.NewPushService(w.st, clock.NewFixed(now), w.seq, sender)

	for _, d := range []string{"flaky", "soon", "refused", "late"} {
		w.subscribe(subs, w.principal(anna), "https://push.example.net/push/"+d)
	}

	byEndpoint := map[string]domain.PushSubscriptionID{}
	for _, s := range w.pushSubs(anna.ID) {
		byEndpoint[strings.TrimPrefix(s.Endpoint, "https://push.example.net/push/")] = s.ID
	}

	queue := func(device string, expires, next time.Time) domain.PushMessage {
		m := domain.PushMessage{
			ID: ids.New[domain.PushMessageID](w.seq), SubscriptionID: byEndpoint[device], Payload: `{"title":"x"}`,
			CreatedAt: now, ExpiresAt: expires, NextAttemptAt: next,
		}
		w.write(func(ctx context.Context, tx store.Tx) error { return tx.PushOutbox().Create(ctx, m) })

		return m
	}

	flaky := queue("flaky", now.Add(24*time.Hour), now)
	refused := queue("refused", now.Add(24*time.Hour), now)
	// Its retry would come after it expired.
	soon := queue("soon", now.Add(30*time.Second), now)
	// The job comes too late for it.
	late := queue("late", now.Add(30*time.Second), now.Add(40*time.Second))

	deliverAt := func(at time.Time) {
		t.Helper()

		svc := service.NewPushService(w.st, clock.NewFixed(at), w.seq, sender)
		if _, err := svc.Deliver(t.Context()); err != nil {
			t.Fatal(err)
		}
	}

	get := func(m domain.PushMessage) domain.PushMessage {
		t.Helper()

		got, found := w.pushMessage(m.ID)
		if !found {
			t.Fatalf("message %v is gone", m.ID)
		}

		return got
	}

	deliverAt(now)

	// A refusal is final, and so is a failure whose retry would come too
	// late; a server error is retried a minute later.
	if m := get(refused); m.FailedAt != now.UTC() || !m.NextAttemptAt.IsZero() || m.Attempts != 1 {
		t.Errorf("refused %+v", m)
	}

	if m := get(soon); m.FailedAt != now.UTC() || m.Attempts != 1 || m.LastError != "push service answered 500" {
		t.Errorf("soon %+v", m)
	}

	if m := get(flaky); m.Attempts != 1 || m.NextAttemptAt != now.Add(time.Minute).UTC() || m.LastError == "" ||
		!m.Pending() {
		t.Errorf("flaky %+v", m)
	}

	// Not due again before then; the late message has expired by its turn
	// and is given up unsent.
	deliverAt(now.Add(45 * time.Second))

	if sender.count("https://push.example.net/push/flaky") != 1 {
		t.Errorf("flaky sent %d times before its retry", sender.count("https://push.example.net/push/flaky"))
	}

	if m := get(late); m.FailedAt != now.Add(45*time.Second).UTC() || m.LastError != "expired before delivery" ||
		sender.count("https://push.example.net/push/late") != 0 {
		t.Errorf("late %+v", m)
	}

	at := now
	for _, wait := range []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute, time.Hour} {
		at = at.Add(wait)
		deliverAt(at)
	}

	if m := get(flaky); m.Attempts != service.MaxPushAttempts || m.FailedAt != at.UTC() || !m.NextAttemptAt.IsZero() {
		t.Errorf("flaky after %d attempts %+v", service.MaxPushAttempts, m)
	}

	// Every subscription stays: only a gone one is deleted.
	if n := len(w.pushSubs(anna.ID)); n != 4 {
		t.Errorf("%d subscriptions left, want 4", n)
	}
}
