package service

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/mail"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// A valid P-256 public key and auth secret of a browser's subscription, in
// base64url (as in servicetest).
const (
	testPushP256DH = "BCVxsr7N_eNgVRqvHtD0zTZsEc6-VV-JvLexhqUzORcxaOzi6-AYWXvTBHm4bjyPjs7Vd8pZGH6SRpkNtoIAiw4"
	testPushAuth   = "BTBZMqHH6r4Tts7J_aSIgg"
)

// pushRecorder is a PushSender that answers each endpoint with answers
// (nil: taken) and records the endpoints it was asked to deliver to.
type pushRecorder struct {
	mu      sync.Mutex
	answers map[string]error
	// during, if set, runs within each send, before the answer.
	during func(ctx context.Context, sub domain.PushSubscription)
	asked  []string
}

func (p *pushRecorder) PublicKey() string { return "" }

func (p *pushRecorder) Send(ctx context.Context, sub domain.PushSubscription, _ []byte, _ time.Duration) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.asked = append(p.asked, sub.Endpoint)
	if p.during != nil {
		p.during(ctx, sub)
	}

	return p.answers[sub.Endpoint]
}

func (p *pushRecorder) askedFor(endpoint string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	return slices.Contains(p.asked, endpoint)
}

// pushFixture is the booking fixture with Anna's phone and laptop and
// Stella's phone subscribed.
type pushFixture struct {
	*bookingFixture

	stella domain.Person
	// subs are Anna's phone, Anna's laptop and Stella's phone.
	subs []domain.PushSubscription
}

func newPushFixture(t *testing.T) *pushFixture {
	t.Helper()

	f := &pushFixture{bookingFixture: newBookingFixture(t)}
	f.stella = f.bookingFixture.stella(t)
	svc := NewPushService(f.st, clock.NewFixed(mustInstant(t, bookingNow)), f.seq, &pushRecorder{})

	for _, d := range []struct {
		person   domain.Person
		endpoint string
	}{
		{f.anna, "https://push.example.org/anna/phone"},
		{f.anna, "https://push.example.org/anna/laptop"},
		{f.stella, "https://push.example.org/stella/phone"},
	} {
		p := f.principal(t, d.person)

		err := svc.Subscribe(t.Context(), p, PushSubscriptionInput{
			Endpoint: d.endpoint, P256DH: testPushP256DH, Auth: testPushAuth,
		})
		if err != nil {
			t.Fatal(err)
		}

		f.write(t, func(ctx context.Context, tx store.Tx) error {
			subs, err := tx.PushSubscriptions().ListByAccount(ctx, p.AccountID)
			for _, s := range subs {
				if s.Endpoint == d.endpoint {
					f.subs = append(f.subs, s)
				}
			}

			return err
		})
	}

	return f
}

// queue stores a message for each subscription, due at bookingNow and
// expiring an hour later.
func (f *pushFixture) queue(t *testing.T, subs ...domain.PushSubscription) []domain.PushMessage {
	t.Helper()

	now := mustInstant(t, bookingNow)

	var out []domain.PushMessage

	f.write(t, func(ctx context.Context, tx store.Tx) error {
		for _, s := range subs {
			m := domain.PushMessage{
				ID: ids.New[domain.PushMessageID](f.seq), SubscriptionID: s.ID, Payload: `{"title":"A place is free"}`,
				CreatedAt: now, ExpiresAt: now.Add(time.Hour), NextAttemptAt: now,
			}
			if err := tx.PushOutbox().Create(ctx, m); err != nil {
				return err
			}

			out = append(out, m)
		}

		return nil
	})

	return out
}

// message returns the stored message, or false if it is gone.
func (f *pushFixture) message(t *testing.T, id domain.PushMessageID) (domain.PushMessage, bool) {
	t.Helper()

	var (
		m     domain.PushMessage
		found bool
	)

	f.write(t, func(ctx context.Context, tx store.Tx) error {
		var err error
		m, err = tx.PushOutbox().Get(ctx, id)
		found = err == nil

		if errors.Is(err, domain.ErrNotFound) {
			return nil
		}

		return err
	})

	return m, found
}

// subscription returns the stored subscription, or false if it is gone.
func (f *pushFixture) subscription(t *testing.T, id domain.PushSubscriptionID) (domain.PushSubscription, bool) {
	t.Helper()

	var (
		s     domain.PushSubscription
		found bool
	)

	f.write(t, func(ctx context.Context, tx store.Tx) error {
		var err error
		s, err = tx.PushSubscriptions().Get(ctx, id)
		found = err == nil

		if errors.Is(err, domain.ErrNotFound) {
			return nil
		}

		return err
	})

	return s, found
}

// inviteData is valid data of the waiting-list invite.
func inviteData(t *testing.T) func(recipient) any {
	t.Helper()

	start := mustInstant(t, staffLesson)

	return func(r recipient) any {
		return mail.WaitlistInviteData{
			WaitlistData: mail.WaitlistData{
				SchoolName: "Studio Example", GivenName: r.givenName, Participant: "Mia Example",
				CourseType: "Imaginary Ballet", StartsAt: start, EndsAt: start.Add(time.Hour), Room: "Room One",
				Location: "Studio Nowhere",
			},
			Link: testAppURL + "waitlist/accept?token=x", ExpiresAt: start.Add(-time.Hour),
		}
	}
}

// TestPushTo: a notification goes to every subscription of each
// recipient's account, in the recipient's language or, without a template
// for it, the default one; recipients without an account or without a
// device get none. The deliver-push job is woken once, after the commit,
// and only if something was queued.
func TestPushTo(t *testing.T) {
	t.Parallel()

	f := newPushFixture(t)
	now := mustInstant(t, bookingNow)
	note := pushNote{template: mail.TemplateWaitlistInvite, url: "https://school.example/x", tag: "t", expires: now.Add(time.Hour)}
	of := func(p domain.Person, locale string) recipient {
		return recipient{
			email: p.GivenName + "@example.org", locale: locale, givenName: p.GivenName, account: true,
			accountID: f.principal(t, p).AccountID,
		}
	}

	wakes, wakesInTx := 0, 0
	mails := Mails{Renderer: newTestRenderer(t), Push: PushQueue{On: true, Wake: func() { wakes++ }}}
	push := func(mails Mails, recipients ...recipient) {
		t.Helper()

		f.write(t, func(ctx context.Context, tx store.Tx) error {
			err := pushTo(ctx, tx, f.seq, mails, bookingSettings(), now, recipients, note, inviteData(t))
			wakesInTx += wakes

			return err
		})
	}

	guest := recipient{email: "gina@example.org", givenName: "Gina", guest: true}

	// Nobody with a device: nothing queued, no wake.
	push(mails, guest, of(f.tom, ""))

	if got := f.queuedPush(t); len(got) != 0 || wakes != 0 {
		t.Fatalf("%d messages, %d wakes for recipients without devices", len(got), wakes)
	}

	push(mails, guest, of(f.tom, ""), of(f.anna, "de"), of(f.stella, "fr-CH"))

	if wakes != 1 || wakesInTx != 0 {
		t.Errorf("%d wakes, %d before the commit; want one, after it", wakes, wakesInTx)
	}

	langs := map[domain.PushSubscriptionID]string{}

	for _, m := range f.queuedPush(t) {
		var p pushPayload
		if err := json.Unmarshal([]byte(m.Payload), &p); err != nil {
			t.Fatal(err)
		}

		if p.URL != note.url || p.Tag != note.tag || p.Title == "" || !m.ExpiresAt.Equal(note.expires) {
			t.Errorf("message %+v, payload %+v", m, p)
		}

		langs[m.SubscriptionID] = p.Lang
	}

	want := map[domain.PushSubscriptionID]string{f.subs[0].ID: "de", f.subs[1].ID: "de", f.subs[2].ID: "en"}
	if len(langs) != len(want) {
		t.Fatalf("messages to %v, want %v", langs, want)
	}

	for id, lang := range want {
		if langs[id] != lang {
			t.Errorf("subscription %v got %q, want %q", id, langs[id], lang)
		}
	}

	// While push is off, nothing.
	before := len(f.queuedPush(t))
	push(Mails{Renderer: mails.Renderer}, of(f.anna, "de"))

	if n := len(f.queuedPush(t)); n != before {
		t.Errorf("push off queued %d messages", n-before)
	}
}

// queuedPush returns the messages due by an hour after bookingNow.
func (f *pushFixture) queuedPush(t *testing.T) []domain.PushMessage {
	t.Helper()

	var out []domain.PushMessage

	f.write(t, func(ctx context.Context, tx store.Tx) error {
		var err error
		out, err = tx.PushOutbox().ListDue(ctx, mustInstant(t, bookingNow).Add(time.Hour), 100)

		return err
	})

	return out
}

// TestPushToRenderFailure: a notification that cannot be rendered is our
// broken data, not the caller's input: an internal error, even though the
// renderer's validation failed, and nothing is queued.
func TestPushToRenderFailure(t *testing.T) {
	t.Parallel()

	f := newPushFixture(t)
	now := mustInstant(t, bookingNow)
	mails := Mails{Renderer: newTestRenderer(t), Push: PushQueue{On: true}}
	anna := recipient{email: "anna@example.org", account: true, accountID: f.principal(t, f.anna).AccountID}

	err := f.st.InTx(t.Context(), store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		return pushTo(ctx, tx, f.seq, mails, bookingSettings(), now, []recipient{anna},
			pushNote{template: mail.TemplateWaitlistInvite, url: "u", tag: "t", expires: now.Add(time.Hour)},
			func(recipient) any { return mail.WaitlistInviteData{} })
	})
	if err == nil || errors.Is(err, domain.ErrValidation) || !strings.Contains(err.Error(), "render") {
		t.Fatalf("pushTo = %v, want an internal render error", err)
	}

	if got := f.queuedPush(t); len(got) != 0 {
		t.Errorf("%d messages queued", len(got))
	}
}

// TestPushToStoreFaults: a store failure while notifications are queued
// fails the causing transaction, which keeps none of them and wakes no job.
func TestPushToStoreFaults(t *testing.T) {
	t.Parallel()

	var wakes int

	SweepFaults(t, FaultSweep{
		Setup: func(t *testing.T) (*FaultStore, func() error) {
			f := newPushFixture(t)
			now := mustInstant(t, bookingNow)
			wakes = 0
			mails := Mails{Renderer: newTestRenderer(t), Push: PushQueue{On: true, Wake: func() { wakes++ }}}
			recipients := []recipient{
				{email: "anna@example.org", account: true, accountID: f.principal(t, f.anna).AccountID},
				{email: "stella@example.org", account: true, accountID: f.principal(t, f.stella).AccountID},
			}
			note := pushNote{template: mail.TemplateWaitlistInvite, url: "u", tag: "t", expires: now.Add(time.Hour)}
			fs := NewFaultStore(f.st)

			return fs, func() error {
				return fs.InTx(t.Context(), store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
					return pushTo(ctx, tx, f.seq, mails, bookingSettings(), now, recipients, note, inviteData(t))
				})
			}
		},
		Check: func(t *testing.T, fs *FaultStore, before StoreData, call FaultCall) {
			if changed := before.Changed(fs.Snapshot(t)); len(changed) > 0 || wakes != 0 {
				t.Errorf("with %s failing, the store changed %v, %d wakes", call, changed, wakes)
			}
		},
	})
}

// TestPushSubscriptionStoreFaults:a failed subscribe saves nothing, a
// failed unsubscribe deletes nothing.
func TestPushSubscriptionStoreFaults(t *testing.T) {
	t.Parallel()

	t.Run("Subscribe", func(t *testing.T) {
		t.Parallel()

		SweepFaults(t, FaultSweep{Setup: func(t *testing.T) (*FaultStore, func() error) {
			f := newPushFixture(t)
			fs := NewFaultStore(f.st)
			svc := NewPushService(fs, clock.NewFixed(mustInstant(t, bookingNow)), f.seq, &pushRecorder{})
			tom := f.principal(t, f.tom)

			return fs, func() error {
				return svc.Subscribe(t.Context(), tom, PushSubscriptionInput{
					Endpoint: "https://push.example.org/tom/phone", P256DH: testPushP256DH, Auth: testPushAuth,
				})
			}
		}})
	})

	t.Run("Unsubscribe", func(t *testing.T) {
		t.Parallel()

		SweepFaults(t, FaultSweep{Setup: func(t *testing.T) (*FaultStore, func() error) {
			f := newPushFixture(t)
			f.queue(t, f.subs[0])
			fs := NewFaultStore(f.st)
			svc := NewPushService(fs, clock.NewFixed(mustInstant(t, bookingNow)), f.seq, nil)
			anna := f.principal(t, f.anna)

			return fs, func() error { return svc.Unsubscribe(t.Context(), anna, f.subs[0].Endpoint) }
		}})
	})
}

// TestPushDeliverStoreFaults: like the mail outbox, push delivery claims,
// sends and records each message in transactions of their own. Whichever
// store call fails, Deliver returns the failure and reports only the
// messages recorded as sent; a message the push service took, or answered
// "gone" for, but that could not be recorded stays claimed until its
// retry; a gone subscription is deleted only with its messages; a message
// not sent stays as it was.
func TestPushDeliverStoreFaults(t *testing.T) {
	t.Parallel()

	now := mustInstant(t, bookingNow)

	var (
		f      *pushFixture
		sender *pushRecorder
		queued []domain.PushMessage
		sent   int
	)

	SweepFaults(t, FaultSweep{
		Setup: func(t *testing.T) (*FaultStore, func() error) {
			f = newPushFixture(t)
			queued = f.queue(t, f.subs...)
			// Anna's laptop is gone.
			sender = &pushRecorder{answers: map[string]error{f.subs[1].Endpoint: ErrPushGone}}
			fs := NewFaultStore(f.st)
			svc := NewPushService(fs, clock.NewFixed(now), f.seq, sender)

			return fs, func() error {
				var err error
				sent, err = svc.Deliver(t.Context())

				return err
			}
		},
		Check: func(t *testing.T, _ *FaultStore, _ StoreData, call FaultCall) {
			recorded := 0

			for i, q := range queued {
				sub := f.subs[i]
				m, found := f.message(t, q.ID)
				_, subFound := f.subscription(t, sub.ID)
				claimed := found && m.Attempts == 1 && m.NextAttemptAt.Equal(now.Add(pushRetryDelay(1))) &&
					m.LastError == "" && m.FailedAt.IsZero() && m.SentAt.IsZero()

				switch {
				case !sender.askedFor(sub.Endpoint):
					if !found || m != q || !subFound {
						t.Errorf("with %s failing, the unsent message to %s changed: %+v", call, sub.Endpoint, m)
					}
				case found && !m.SentAt.IsZero():
					recorded++
				case !found:
					if subFound {
						t.Errorf("with %s failing, the message to %s went without its subscription", call, sub.Endpoint)
					}
				case !claimed || !subFound:
					t.Errorf("with %s failing, the unrecorded message to %s is %+v (subscription kept %v), "+
						"want it claimed until its retry", call, sub.Endpoint, m, subFound)
				}
			}

			if sent != recorded {
				t.Errorf("with %s failing, Deliver reported %d sent, %d are recorded", call, sent, recorded)
			}
		},
	})
}

// TestPushDeliverEdges: without a sender nothing is delivered; a run sends
// at most a batch; a send that fails because the job is shutting down is
// not recorded as a failed attempt.
func TestPushDeliverEdges(t *testing.T) {
	t.Parallel()

	now := mustInstant(t, bookingNow)

	t.Run("no sender", func(t *testing.T) {
		t.Parallel()

		f := newPushFixture(t)
		q := f.queue(t, f.subs[0])[0]

		if n, err := NewPushService(f.st, clock.NewFixed(now), f.seq, nil).Deliver(t.Context()); err != nil || n != 0 {
			t.Fatalf("Deliver = %d, %v", n, err)
		}

		if m, _ := f.message(t, q.ID); m != q {
			t.Errorf("message %+v, want it untouched", m)
		}
	})

	t.Run("batch", func(t *testing.T) {
		t.Parallel()

		f := newPushFixture(t)

		var subs []domain.PushSubscription
		for range pushBatch + 1 {
			subs = append(subs, f.subs[0])
		}

		f.queue(t, subs...)
		svc := NewPushService(f.st, clock.NewFixed(now), f.seq, &pushRecorder{})

		for _, want := range []int{pushBatch, 1, 0} {
			if n, err := svc.Deliver(t.Context()); err != nil || n != want {
				t.Fatalf("Deliver = %d, %v; want %d", n, err, want)
			}
		}
	})

	t.Run("shutdown", func(t *testing.T) {
		t.Parallel()

		f := newPushFixture(t)
		qs := f.queue(t, f.subs[0], f.subs[2])

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		sender := &pushRecorder{
			answers: map[string]error{f.subs[0].Endpoint: errors.New("connection closed")},
			during:  func(context.Context, domain.PushSubscription) { cancel() },
		}

		n, err := NewPushService(f.st, clock.NewFixed(now), f.seq, sender).Deliver(ctx)
		if !errors.Is(err, context.Canceled) || n != 0 {
			t.Fatalf("Deliver = %d, %v; want the cancellation", n, err)
		}

		if m, _ := f.message(t, qs[0].ID); m.Attempts != 1 || m.LastError != "" || !m.FailedAt.IsZero() ||
			!m.NextAttemptAt.Equal(now.Add(pushRetryDelay(1))) {
			t.Errorf("first message %+v, want it claimed, without an error", m)
		}

		if m, _ := f.message(t, qs[1].ID); m != qs[1] {
			t.Errorf("second message %+v, want it untouched", m)
		}
	})
}
