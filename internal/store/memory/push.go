package memory

import (
	"cmp"
	"context"
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
)

// ---------------------------------------------------------------- push subscriptions

type pushSubscriptionRepo struct{ tx *tx }

func (r pushSubscriptionRepo) Get(_ context.Context, id domain.PushSubscriptionID) (domain.PushSubscription, error) {
	s, ok := r.tx.state.pushSubs.get(id)
	if !ok {
		return domain.PushSubscription{}, notFound("push subscription")
	}

	return s, nil
}

func (r pushSubscriptionRepo) Save(_ context.Context, sub domain.PushSubscription) error {
	if err := r.tx.checkWritable(); err != nil {
		return err
	}

	st := &r.tx.state
	if !st.accounts.has(sub.AccountID) {
		return conflict("push subscription references a missing account")
	}

	sub.CreatedAt, sub.LastUsedAt = instant(sub.CreatedAt), instant(sub.LastUsedAt)

	if old, ok := r.byEndpoint(sub.Endpoint); ok {
		// The same account's endpoint is updated; another account's goes
		// with its messages, and s takes its place (the device changed
		// hands).
		if old.AccountID == sub.AccountID {
			old.P256DH, old.Auth, old.DeviceLabel, old.LastUsedAt = sub.P256DH, sub.Auth, sub.DeviceLabel, sub.LastUsedAt
			st.pushSubs.put(r.tx.gen, old.ID, old)

			return nil
		}

		r.delete(old.ID)
	}

	if st.pushSubs.has(sub.ID) {
		return conflict("push subscription %s exists", sub.ID)
	}

	st.pushSubs.put(r.tx.gen, sub.ID, sub)

	return nil
}

func (r pushSubscriptionRepo) byEndpoint(endpoint string) (domain.PushSubscription, bool) {
	found := r.tx.state.pushSubs.filter(func(s domain.PushSubscription) bool { return s.Endpoint == endpoint })
	if len(found) == 0 {
		return domain.PushSubscription{}, false
	}

	return found[0], true
}

func (r pushSubscriptionRepo) ListByAccount(
	_ context.Context, account domain.AccountID,
) ([]domain.PushSubscription, error) {
	subs := r.tx.state.pushSubs.filter(func(s domain.PushSubscription) bool { return s.AccountID == account })

	return sorted(subs, func(a, b domain.PushSubscription) int {
		return cmp.Or(a.CreatedAt.Compare(b.CreatedAt), compareID(a.ID, b.ID))
	}), nil
}

func (r pushSubscriptionRepo) Delete(_ context.Context, id domain.PushSubscriptionID) error {
	if err := r.tx.checkWritable(); err != nil {
		return err
	}

	if !r.tx.state.pushSubs.has(id) {
		return notFound("push subscription")
	}

	r.delete(id)

	return nil
}

func (r pushSubscriptionRepo) DeleteByAccount(_ context.Context, account domain.AccountID) (int, error) {
	if err := r.tx.checkWritable(); err != nil {
		return 0, err
	}

	subs := r.tx.state.pushSubs.filter(func(s domain.PushSubscription) bool { return s.AccountID == account })
	for _, s := range subs {
		r.delete(s.ID)
	}

	return len(subs), nil
}

func (r pushSubscriptionRepo) DeleteByEndpoint(
	_ context.Context, account domain.AccountID, endpoint string,
) (int, error) {
	if err := r.tx.checkWritable(); err != nil {
		return 0, err
	}

	s, ok := r.byEndpoint(endpoint)
	if !ok || s.AccountID != account {
		return 0, nil
	}

	r.delete(s.ID)

	return 1, nil
}

func (r pushSubscriptionRepo) Touch(_ context.Context, id domain.PushSubscriptionID, at time.Time) error {
	if err := r.tx.checkWritable(); err != nil {
		return err
	}

	s, ok := r.tx.state.pushSubs.get(id)
	if !ok {
		return notFound("push subscription")
	}

	s.LastUsedAt = instant(at)
	r.tx.state.pushSubs.put(r.tx.gen, id, s)

	return nil
}

func (r pushSubscriptionRepo) DeleteUnusedBefore(_ context.Context, before time.Time) (int, error) {
	if err := r.tx.checkWritable(); err != nil {
		return 0, err
	}

	before = instant(before)
	unused := r.tx.state.pushSubs.filter(func(s domain.PushSubscription) bool { return s.LastUsedAt.Before(before) })

	for _, s := range unused {
		r.delete(s.ID)
	}

	return len(unused), nil
}

// delete deletes subscription id and its messages (ON DELETE CASCADE).
func (r pushSubscriptionRepo) delete(id domain.PushSubscriptionID) {
	deletePushSubscription(&r.tx.state, r.tx.gen, id)
}

func deletePushSubscription(st *state, gen uint64, id domain.PushSubscriptionID) {
	for _, m := range st.pushOutbox.filter(func(m domain.PushMessage) bool { return m.SubscriptionID == id }) {
		st.pushOutbox.delete(gen, m.ID)
	}

	st.pushSubs.delete(gen, id)
}

// ---------------------------------------------------------------- push outbox

type pushOutboxRepo struct{ tx *tx }

func (r pushOutboxRepo) Get(_ context.Context, id domain.PushMessageID) (domain.PushMessage, error) {
	m, ok := r.tx.state.pushOutbox.get(id)
	if !ok {
		return domain.PushMessage{}, notFound("push message")
	}

	return m, nil
}

func (r pushOutboxRepo) Create(_ context.Context, m domain.PushMessage) error {
	if err := r.tx.checkWritable(); err != nil {
		return err
	}

	switch {
	case r.tx.state.pushOutbox.has(m.ID):
		return conflict("push message %s exists", m.ID)
	case !r.tx.state.pushSubs.has(m.SubscriptionID):
		return conflict("push message references a missing subscription")
	}

	r.put(m)

	return nil
}

func (r pushOutboxRepo) Update(_ context.Context, m domain.PushMessage) error {
	if err := r.tx.checkWritable(); err != nil {
		return err
	}

	old, ok := r.tx.state.pushOutbox.get(m.ID)
	if !ok {
		return notFound("push message")
	}

	m.SubscriptionID, m.CreatedAt = old.SubscriptionID, old.CreatedAt
	r.put(m)

	return nil
}

func (r pushOutboxRepo) put(m domain.PushMessage) {
	m.CreatedAt, m.ExpiresAt = instant(m.CreatedAt), instant(m.ExpiresAt)
	m.NextAttemptAt, m.SentAt, m.FailedAt = instant(m.NextAttemptAt), instant(m.SentAt), instant(m.FailedAt)
	r.tx.state.pushOutbox.put(r.tx.gen, m.ID, m)
}

func (r pushOutboxRepo) ListDue(_ context.Context, now time.Time, limit int) ([]domain.PushMessage, error) {
	// Like the PostgreSQL store, which locks the messages it returns.
	if err := r.tx.checkWritable(); err != nil {
		return nil, err
	}

	due := r.tx.state.pushOutbox.filter(func(m domain.PushMessage) bool {
		return m.Pending() && !m.NextAttemptAt.After(now)
	})
	due = sorted(due, func(a, b domain.PushMessage) int {
		return cmp.Or(a.NextAttemptAt.Compare(b.NextAttemptAt), compareID(a.ID, b.ID))
	})

	return due[:min(max(limit, 0), len(due))], nil
}

func (r pushOutboxRepo) DeleteCreatedBefore(_ context.Context, before time.Time) (int, error) {
	if err := r.tx.checkWritable(); err != nil {
		return 0, err
	}

	before = instant(before)
	old := r.tx.state.pushOutbox.filter(func(m domain.PushMessage) bool { return m.CreatedAt.Before(before) })

	for _, m := range old {
		r.tx.state.pushOutbox.delete(r.tx.gen, m.ID)
	}

	return len(old), nil
}
