package service

import (
	"cmp"
	"context"
	"crypto/ecdh"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/mail"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// Web push (ADR 0017): notifications on the devices of an account, besides
// some mails. A notification goes to every subscription of the account; it
// is queued in the transaction that causes it, like a mail, and sent by the
// deliver-push job.

// PushSender delivers web push messages: the webpush package on the
// server, the worker's notifications in the web demo. Without one push is
// off.
type PushSender interface {
	// PublicKey is the VAPID public key (base64url) browsers subscribe
	// with; empty if the sender needs none (the web demo).
	PublicKey() string
	// Send delivers payload to the subscription's push service, which keeps
	// it for an offline device for ttl. ErrPushGone reports a subscription
	// the push service no longer knows, ErrPushRefused a message it will
	// never take; any other error is retried.
	Send(ctx context.Context, sub domain.PushSubscription, payload []byte, ttl time.Duration) error
}

var (
	// ErrPushGone is returned by a PushSender for a subscription that no
	// longer exists (404, 410): it is deleted.
	ErrPushGone = errors.New("push subscription gone")
	// ErrPushRefused is returned by a PushSender for a message the push
	// service refuses for good: it is given up.
	ErrPushRefused = errors.New("push message refused")
)

// PushQueue configures the notifications the services queue (Mails.Push).
// The zero value queues none.
type PushQueue struct {
	// On: a PushSender delivers messages; without one nothing is queued.
	On bool
	// Wake asks the deliver-push job to send soon instead of on its next
	// tick; nil: on its tick. It may run inside the transaction that
	// queued the messages: a run before the commit finds nothing yet and
	// leaves them to the tick (the memory store makes the run wait).
	Wake func()
}

// Push delivery, like the mail outbox's (outbox.go): each message is
// claimed for one attempt by moving its next attempt on, sent outside any
// transaction, and then recorded.
const (
	// MaxPushAttempts is how often delivery is tried before it is given up.
	MaxPushAttempts = 5
	// pushBatch is how many messages one Deliver run sends at most.
	pushBatch = 50
	// PushRetention is how long messages stay, sent or not.
	PushRetention = 7 * 24 * time.Hour
	// PushSubscriptionRetentionMonths: a subscription unused this long is
	// deleted; the browser has most likely dropped it.
	PushSubscriptionRetentionMonths = 6
	// pushTTLCap bounds how long a push service keeps a message (some
	// refuse more than four weeks).
	pushTTLCap = 7 * 24 * time.Hour
)

// pushRetryDelays are the waits after the 1st, 2nd, ... attempt.
var pushRetryDelays = []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute, time.Hour}

func pushRetryDelay(attempts int) time.Duration {
	return pushRetryDelays[min(max(attempts, 1), len(pushRetryDelays))-1]
}

// pushPayload is the JSON a notification carries, read by the service
// worker (web/src/sw.ts): what it shows and the page a click opens. It
// names the lesson and its time, never a person (ADR 0017).
type pushPayload struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	// URL is the page a click on the notification opens.
	URL string `json:"url"`
	// Tag replaces an earlier notification of the same subject.
	Tag  string `json:"tag"`
	Lang string `json:"lang"`
}

// pushNote is one notification to queue for the recipients' accounts.
type pushNote struct {
	template string // one of mail.PushTemplateNames
	url, tag string
	expires  time.Time
}

// pushTo queues the notification n, rendered from the data that data makes
// of each recipient's mail data in the recipient's language, for every
// subscription of the recipients' accounts. Recipients without an account
// get none. It does nothing unless push is on.
func pushTo(
	ctx context.Context, tx store.Tx, src ids.Source, mails Mails, settings domain.Settings, now time.Time,
	recipients []recipient, n pushNote, data func(recipient) any,
) error {
	if !mails.Push.On {
		return nil
	}

	queued := 0

	for _, r := range recipients {
		if r.accountID.IsZero() {
			continue
		}

		subs, err := tx.PushSubscriptions().ListByAccount(ctx, r.accountID)
		if err != nil || len(subs) == 0 {
			if err != nil {
				return err
			}

			continue
		}

		locale, found := mails.Renderer.MatchLocale(cmp.Or(r.locale, settings.DefaultLocale))
		if !found {
			locale = mail.DefaultLocale
		}

		text, err := mails.Renderer.RenderPush(locale, n.template, data(r))
		if err != nil {
			// Broken data or templates: a 500, as for mails (sendMails).
			return fmt.Errorf("render %s notification: %v", n.template, err) //nolint:errorlint // see above
		}

		payload, err := json.Marshal(pushPayload{Title: text.Title, Body: text.Body, URL: n.url, Tag: n.tag, Lang: locale})
		if err != nil {
			return fmt.Errorf("push payload: %w", err)
		}

		for _, sub := range subs {
			if err := enqueuePush(ctx, tx, src, now, sub.ID, string(payload), n.expires); err != nil {
				return err
			}

			queued++
		}
	}

	if queued > 0 && mails.Push.Wake != nil {
		mails.Push.Wake()
	}

	return nil
}

// enqueuePush writes a push message to the outbox in tx, the sibling of
// enqueueMail; the deliver-push job sends it after the transaction
// commits.
func enqueuePush(
	ctx context.Context, tx store.Tx, src ids.Source, now time.Time, sub domain.PushSubscriptionID, payload string,
	expires time.Time,
) error {
	m := domain.PushMessage{
		ID: ids.New[domain.PushMessageID](src), SubscriptionID: sub, Payload: payload, CreatedAt: now,
		ExpiresAt: expires, NextAttemptAt: now,
	}
	if err := m.Validate(); err != nil {
		return fmt.Errorf("push message: %v", err) //nolint:errorlint // our data, not the caller's: a 500
	}

	return tx.PushOutbox().Create(ctx, m)
}

// PushService manages the web push subscriptions of accounts and delivers
// their messages (ADR 0017).
type PushService struct {
	store  store.Store
	clock  clock.Clock
	ids    ids.Source
	sender PushSender
}

// NewPushService returns a PushService that delivers through sender; with
// a nil sender push is off.
func NewPushService(st store.Store, clk clock.Clock, src ids.Source, sender PushSender) *PushService {
	return &PushService{store: st, clock: clk, ids: src, sender: sender}
}

// PushStatus tells whether push is on and the key to subscribe with.
type PushStatus struct {
	Enabled bool
	// PublicKey is the VAPID public key; empty while push is off, and in
	// the web demo, which needs none.
	PublicKey string
}

// Status returns whether push is on.
func (s *PushService) Status() PushStatus {
	if s.sender == nil {
		return PushStatus{}
	}

	return PushStatus{Enabled: true, PublicKey: s.sender.PublicKey()}
}

// PushSubscriptionInput is a browser's subscription as it gives it
// (PushSubscription.toJSON()): the keys in base64url.
type PushSubscriptionInput struct {
	Endpoint    string
	P256DH      string
	Auth        string
	DeviceLabel string
}

// Subscribe saves a subscription of p's device (PushSubscriptionRepo.Save):
// its notifications go to p's account from now on. It is
// domain.ErrNotFound while push is off, and refused while an admin is
// signed in as the account (ErrImpersonating): the admin's device would get
// the account's notifications.
func (s *PushService) Subscribe(ctx context.Context, p Principal, in PushSubscriptionInput) error {
	switch {
	case s.sender == nil:
		return fmt.Errorf("push is off: %w", domain.ErrNotFound)
	case !p.ImpersonatorID.IsZero():
		return ErrImpersonating
	}

	p256dh, err := decodeKey(in.P256DH, 65)
	if err != nil {
		return &domain.FieldError{Field: "keys.p256dh", Msg: "p256dh must be the base64url of a P-256 public key"}
	}

	if _, err := ecdh.P256().NewPublicKey(p256dh); err != nil {
		return &domain.FieldError{Field: "keys.p256dh", Msg: "p256dh must be the base64url of a P-256 public key"}
	}

	auth, err := decodeKey(in.Auth, 16)
	if err != nil {
		return &domain.FieldError{Field: "keys.auth", Msg: "auth must be the base64url of 16 bytes"}
	}

	now := s.clock.Now()
	sub := domain.PushSubscription{
		ID: ids.New[domain.PushSubscriptionID](s.ids), AccountID: p.AccountID, Endpoint: in.Endpoint,
		P256DH: [65]byte(p256dh), Auth: [16]byte(auth), DeviceLabel: strings.TrimSpace(in.DeviceLabel),
		CreatedAt: now, LastUsedAt: now,
	}
	if err := sub.Validate(); err != nil {
		return err
	}

	err = s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		return tx.PushSubscriptions().Save(ctx, sub)
	})
	if err != nil {
		return fmt.Errorf("save push subscription: %w", err)
	}

	return nil
}

// decodeKey decodes base64url (padding optional) of n bytes.
func decodeKey(s string, n int) ([]byte, error) {
	b, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(strings.TrimSpace(s), "="))
	if err == nil && len(b) != n {
		err = fmt.Errorf("%d bytes, want %d", len(b), n)
	}

	return b, err
}

// Unsubscribe deletes the subscription of the endpoint if it is p's
// account's; anything else is nothing to do. It works while push is off
// too, so a device can always leave.
func (s *PushService) Unsubscribe(ctx context.Context, p Principal, endpoint string) error {
	err := s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		_, err := tx.PushSubscriptions().DeleteByEndpoint(ctx, p.AccountID, endpoint)

		return err
	})
	if err != nil {
		return fmt.Errorf("delete push subscription: %w", err)
	}

	return nil
}

// Deliver makes one delivery attempt for every due message, up to a batch,
// and returns how many it sent. A message whose time has passed is given
// up unsent. Delivery failures are recorded on the message (a gone
// subscription is deleted), not returned; the error reports store problems
// and cancellation only.
func (s *PushService) Deliver(ctx context.Context) (int, error) {
	if s.sender == nil {
		return 0, nil
	}

	sent := 0

	for range pushBatch {
		c, ok, err := s.claim(ctx)
		if err != nil || !ok {
			return sent, err
		}

		if c.expired {
			continue
		}

		sendErr := s.sender.Send(ctx, c.sub, []byte(c.msg.Payload), c.ttl)
		if sendErr != nil && ctx.Err() != nil {
			// Shutting down: the claim expires and the message is retried.
			return sent, ctx.Err()
		}

		// A sent message is recorded even if ctx ends now, or it would be
		// sent again.
		if err := s.record(context.WithoutCancel(ctx), c, sendErr); err != nil {
			return sent, err
		}

		if sendErr == nil {
			sent++
		}
	}

	return sent, nil
}

// claimed is a message taken for one attempt.
type claimed struct {
	msg domain.PushMessage
	sub domain.PushSubscription
	ttl time.Duration
	// expired: the message's time had passed; it was given up unsent.
	expired bool
}

// claim takes the next due message for one attempt; ok is false if none
// is due.
func (s *PushService) claim(ctx context.Context) (claimed, bool, error) {
	var (
		c  claimed
		ok bool
	)

	err := s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		c, ok = claimed{}, false
		now := s.clock.Now()

		due, err := tx.PushOutbox().ListDue(ctx, now, 1)
		if err != nil || len(due) == 0 {
			return err
		}

		c.msg, ok = due[0], true

		if !now.Before(c.msg.ExpiresAt) {
			c.expired = true
			c.msg.NextAttemptAt, c.msg.FailedAt, c.msg.LastError = time.Time{}, now, "expired before delivery"

			return tx.PushOutbox().Update(ctx, c.msg)
		}

		if c.sub, err = tx.PushSubscriptions().Get(ctx, c.msg.SubscriptionID); err != nil {
			return fmt.Errorf("subscription of push message %v: %w", c.msg.ID, refErr(err))
		}

		c.ttl = min(c.msg.ExpiresAt.Sub(now), pushTTLCap)
		c.msg.Attempts++
		c.msg.NextAttemptAt = now.Add(pushRetryDelay(c.msg.Attempts))

		return tx.PushOutbox().Update(ctx, c.msg)
	})
	if err != nil {
		return claimed{}, false, fmt.Errorf("claim push message: %w", err)
	}

	return c, ok, nil
}

// record stores the outcome of one attempt: a sent message is done and
// its subscription used; a gone subscription is deleted with its
// messages; a refused message, or one out of attempts, is given up.
func (s *PushService) record(ctx context.Context, c claimed, sendErr error) error {
	err := s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		now := s.clock.Now()

		if errors.Is(sendErr, ErrPushGone) {
			err := tx.PushSubscriptions().Delete(ctx, c.sub.ID)
			if errors.Is(err, domain.ErrNotFound) {
				return nil // deleted meanwhile
			}

			return err
		}

		m, err := tx.PushOutbox().Get(ctx, c.msg.ID)

		switch {
		case errors.Is(err, domain.ErrNotFound):
			return nil // its subscription went meanwhile
		case err != nil:
			return err
		}

		if sendErr == nil {
			m.SentAt, m.NextAttemptAt, m.LastError = now, time.Time{}, ""

			if err := tx.PushOutbox().Update(ctx, m); err != nil {
				return err
			}

			return tx.PushSubscriptions().Touch(ctx, c.sub.ID, now)
		}

		m.LastError = truncate(sendErr.Error(), maxMailErrorLen)
		if errors.Is(sendErr, ErrPushRefused) || m.Attempts >= MaxPushAttempts || !m.NextAttemptAt.Before(m.ExpiresAt) {
			m.NextAttemptAt, m.FailedAt = time.Time{}, now // given up
		}

		return tx.PushOutbox().Update(ctx, m)
	})
	if err != nil {
		return fmt.Errorf("record push delivery: %w", err)
	}

	return nil
}

// pushInvite queues the notification of a waiting-list invite (ADR 0010)
// for the recipients' devices: the lesson and its time, with the link that
// accepts the place, until the invite expires.
func (w waitlister) pushInvite(
	ctx context.Context, tx store.Tx, settings domain.Settings, now time.Time, e domain.WaitlistEntry, link string,
	recipients []recipient, data func(recipient) any,
) error {
	n := pushNote{
		template: mail.TemplateWaitlistInvite, url: link, tag: "waitlist-invite-" + e.ID.String(), expires: e.ExpiresAt,
	}

	return pushTo(ctx, tx, w.ids, w.mails, settings, now, recipients, n, data)
}
