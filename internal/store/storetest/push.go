package storetest

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// pushSub builds a subscription of a for the endpoint created at at, with
// keys derived from its ID; it is not written.
func (e *env) pushSub(a domain.Account, endpoint string, at time.Time) domain.PushSubscription {
	e.t.Helper()

	s := domain.PushSubscription{
		ID: newID[domain.PushSubscriptionID](e), AccountID: a.ID, Endpoint: endpoint, DeviceLabel: "Firefox on Linux",
		CreatedAt: at, LastUsedAt: at,
	}
	s.P256DH[0] = 4
	copy(s.P256DH[1:], s.ID[:])
	copy(s.Auth[:], s.ID[:])
	valid(e.t, s)

	return s
}

func (e *env) savePushSubs(subs ...domain.PushSubscription) {
	e.t.Helper()

	e.mustWrite(func(ctx context.Context, tx store.Tx) error {
		for _, s := range subs {
			if err := tx.PushSubscriptions().Save(ctx, s); err != nil {
				return err
			}
		}

		return nil
	})
}

func (e *env) pushSubsOf(a domain.Account) []domain.PushSubscription {
	e.t.Helper()

	return mustQuery(e, func(ctx context.Context, tx store.Tx) ([]domain.PushSubscription, error) {
		return tx.PushSubscriptions().ListByAccount(ctx, a.ID)
	})
}

// pushMessage builds a pending message to s created at at; it is not
// written.
func (e *env) pushMessage(s domain.PushSubscription, at time.Time) domain.PushMessage {
	e.t.Helper()

	m := domain.PushMessage{
		ID: newID[domain.PushMessageID](e), SubscriptionID: s.ID, Payload: `{"title": "A place is free", "url": "x"}`,
		CreatedAt: at, ExpiresAt: at.Add(time.Hour), NextAttemptAt: at,
	}
	valid(e.t, m)

	return m
}

func (e *env) createPushMessages(ms ...domain.PushMessage) {
	e.t.Helper()

	e.mustWrite(func(ctx context.Context, tx store.Tx) error {
		for _, m := range ms {
			if err := tx.PushOutbox().Create(ctx, m); err != nil {
				return err
			}
		}

		return nil
	})
}

// pushMessageGone reports whether message id does not exist.
func (e *env) pushMessageGone(id domain.PushMessageID) bool {
	e.t.Helper()

	_, err := query(e, func(ctx context.Context, tx store.Tx) (domain.PushMessage, error) {
		return tx.PushOutbox().Get(ctx, id)
	})
	if err != nil && !isNotFound(err) {
		e.t.Fatal(err)
	}

	return err != nil
}

func testPushSubscriptions(t *testing.T, newStore func(*testing.T) store.Store) {
	t.Helper()

	cases(t, newStore, map[string]func(e *env){
		"SaveGetAndList": func(e *env) {
			ada := e.account(e.person("Ada", "Lovelace"), "ada@example.org")
			bob := e.account(e.person("Bob", "Builder"), "bob@example.org")

			// A fraction of a microsecond is cut off like in PostgreSQL.
			phone := e.pushSub(ada, "https://push.example.net/push/phone", t0.Add(500*time.Nanosecond))
			laptop := e.pushSub(ada, "https://push.example.net/push/laptop", t0.Add(-time.Hour))
			laptop.DeviceLabel = ""
			other := e.pushSub(bob, "https://push.example.org/push/bob", t0)
			e.savePushSubs(phone, laptop, other)
			phone.CreatedAt, phone.LastUsedAt = t0, t0

			got := mustQuery(e, func(ctx context.Context, tx store.Tx) (domain.PushSubscription, error) {
				return tx.PushSubscriptions().Get(ctx, phone.ID)
			})
			wantEqual(e.t, got, phone)

			_, err := query(e, func(ctx context.Context, tx store.Tx) (domain.PushSubscription, error) {
				return tx.PushSubscriptions().Get(ctx, newID[domain.PushSubscriptionID](e))
			})
			wantErr(e.t, err, domain.ErrNotFound)

			wantSlice(e.t, e.pushSubsOf(ada), []domain.PushSubscription{laptop, phone})
			wantSlice(e.t, e.pushSubsOf(bob), []domain.PushSubscription{other})
			wantSlice(e.t, e.pushSubsOf(e.account(e.person("Cy", "Nobody"), "cy@example.org")), nil)
		},
		"SaveAgain": func(e *env) {
			ada := e.account(e.person("Ada", "Lovelace"), "ada@example.org")
			sub := e.pushSub(ada, "https://push.example.net/push/phone", t0)
			e.savePushSubs(sub)
			e.createPushMessages(e.pushMessage(sub, t0))

			err := e.read(func(ctx context.Context, tx store.Tx) error { return tx.PushSubscriptions().Save(ctx, sub) })
			wantErr(e.t, err, store.ErrReadOnly)

			// The same account's endpoint with new keys: the ID and the creation
			// stay, the rest changes, the messages stay.
			again := e.pushSub(ada, sub.Endpoint, t0.Add(time.Hour))
			again.DeviceLabel = "Chrome on Android"
			e.savePushSubs(again)

			want := again
			want.ID, want.CreatedAt = sub.ID, sub.CreatedAt
			wantSlice(e.t, e.pushSubsOf(ada), []domain.PushSubscription{want})

			due := mustWriteQuery(e, func(ctx context.Context, tx store.Tx) ([]domain.PushMessage, error) {
				return tx.PushOutbox().ListDue(ctx, t0, 10)
			})
			wantEqual(e.t, len(due), 1)
		},
		"SaveMovesAnEndpoint": func(e *env) {
			ada := e.account(e.person("Ada", "Lovelace"), "ada@example.org")
			bob := e.account(e.person("Bob", "Builder"), "bob@example.org")
			sub := e.pushSub(ada, "https://push.example.net/push/shared", t0)
			e.savePushSubs(sub)
			msg := e.pushMessage(sub, t0)
			e.createPushMessages(msg)

			// Bob signs in on Ada's device: the subscription is his now, and
			// Ada's message does not reach him.
			moved := e.pushSub(bob, sub.Endpoint, t0.Add(time.Hour))
			e.savePushSubs(moved)

			wantSlice(e.t, e.pushSubsOf(ada), nil)
			wantSlice(e.t, e.pushSubsOf(bob), []domain.PushSubscription{moved})
			wantEqual(e.t, e.pushMessageGone(msg.ID), true)
		},
		"SaveMovesAnEndpointWithinATransaction": func(e *env) {
			ada := e.account(e.person("Ada", "Lovelace"), "ada@example.org")
			bob := e.account(e.person("Bob", "Builder"), "bob@example.org")
			sub := e.pushSub(ada, "https://push.example.net/push/shared", t0)
			msg := e.pushMessage(sub, t0)
			moved := e.pushSub(bob, sub.Endpoint, t0.Add(time.Hour))

			// Ada's subscription and message, then Bob's save of the same
			// endpoint, in one transaction: the endpoint is Bob's, and Ada's
			// message does not reach him.
			e.mustWrite(func(ctx context.Context, tx store.Tx) error {
				if err := tx.PushSubscriptions().Save(ctx, sub); err != nil {
					return err
				}

				if err := tx.PushOutbox().Create(ctx, msg); err != nil {
					return err
				}

				return tx.PushSubscriptions().Save(ctx, moved)
			})

			wantSlice(e.t, e.pushSubsOf(ada), nil)
			wantSlice(e.t, e.pushSubsOf(bob), []domain.PushSubscription{moved})
			wantEqual(e.t, e.pushMessageGone(msg.ID), true)
		},
		"DeleteByAccount": func(e *env) {
			ada := e.account(e.person("Ada", "Lovelace"), "ada@example.org")
			bob := e.account(e.person("Bob", "Builder"), "bob@example.org")
			phone := e.pushSub(ada, "https://push.example.net/push/phone", t0)
			laptop := e.pushSub(ada, "https://push.example.net/push/laptop", t0)
			other := e.pushSub(bob, "https://push.example.net/push/bob", t0)
			e.savePushSubs(phone, laptop, other)
			msg := e.pushMessage(phone, t0)
			e.createPushMessages(msg)

			err := e.read(func(ctx context.Context, tx store.Tx) error {
				_, err := tx.PushSubscriptions().DeleteByAccount(ctx, ada.ID)

				return err
			})
			wantErr(e.t, err, store.ErrReadOnly)

			n := mustWriteQuery(e, func(ctx context.Context, tx store.Tx) (int, error) {
				return tx.PushSubscriptions().DeleteByAccount(ctx, ada.ID)
			})
			wantEqual(e.t, n, 2)
			wantSlice(e.t, e.pushSubsOf(ada), nil)
			wantSlice(e.t, e.pushSubsOf(bob), []domain.PushSubscription{other})
			wantEqual(e.t, e.pushMessageGone(msg.ID), true)
		},
		"SaveConflicts": func(e *env) {
			ada := e.account(e.person("Ada", "Lovelace"), "ada@example.org")
			sub := e.pushSub(ada, "https://push.example.net/push/phone", t0)
			e.savePushSubs(sub)

			sameID := e.pushSub(ada, "https://push.example.net/push/laptop", t0)
			sameID.ID = sub.ID
			noAccount := e.pushSub(ada, "https://push.example.net/push/ghost", t0)
			noAccount.AccountID = newID[domain.AccountID](e)

			for name, s := range map[string]domain.PushSubscription{"same id": sameID, "no account": noAccount} {
				err := e.write(func(ctx context.Context, tx store.Tx) error { return tx.PushSubscriptions().Save(ctx, s) })
				if !isConflict(err) {
					e.t.Errorf("%s: want ErrConflict, got %v", name, err)
				}
			}

			wantSlice(e.t, e.pushSubsOf(ada), []domain.PushSubscription{sub})
		},
		"Delete": func(e *env) {
			ada := e.account(e.person("Ada", "Lovelace"), "ada@example.org")
			bob := e.account(e.person("Bob", "Builder"), "bob@example.org")
			phone := e.pushSub(ada, "https://push.example.net/push/phone", t0)
			laptop := e.pushSub(ada, "https://push.example.net/push/laptop", t0)
			e.savePushSubs(phone, laptop)
			msg := e.pushMessage(phone, t0)
			e.createPushMessages(msg)

			e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.PushSubscriptions().Delete(ctx, phone.ID) })
			wantSlice(e.t, e.pushSubsOf(ada), []domain.PushSubscription{laptop})
			wantEqual(e.t, e.pushMessageGone(msg.ID), true)

			err := e.write(func(ctx context.Context, tx store.Tx) error {
				return tx.PushSubscriptions().Delete(ctx, phone.ID)
			})
			wantErr(e.t, err, domain.ErrNotFound)

			// By endpoint, the account's own only.
			deleted := func(account domain.Account, endpoint string) int {
				return mustWriteQuery(e, func(ctx context.Context, tx store.Tx) (int, error) {
					return tx.PushSubscriptions().DeleteByEndpoint(ctx, account.ID, endpoint)
				})
			}
			wantEqual(e.t, deleted(bob, laptop.Endpoint), 0)
			wantEqual(e.t, deleted(ada, "https://push.example.net/push/unknown"), 0)
			wantEqual(e.t, deleted(ada, laptop.Endpoint), 1)
			wantSlice(e.t, e.pushSubsOf(ada), nil)
		},
		"TouchAndDeleteUnused": func(e *env) {
			ada := e.account(e.person("Ada", "Lovelace"), "ada@example.org")
			old := e.pushSub(ada, "https://push.example.net/push/old", t0)
			used := e.pushSub(ada, "https://push.example.net/push/used", t0)
			fresh := e.pushSub(ada, "https://push.example.net/push/fresh", t0.Add(48*time.Hour))
			e.savePushSubs(old, used, fresh)
			msg := e.pushMessage(old, t0)
			e.createPushMessages(msg)

			e.mustWrite(func(ctx context.Context, tx store.Tx) error {
				return tx.PushSubscriptions().Touch(ctx, used.ID, t0.Add(48*time.Hour+500*time.Nanosecond))
			})
			used.LastUsedAt = t0.Add(48 * time.Hour)

			err := e.write(func(ctx context.Context, tx store.Tx) error {
				return tx.PushSubscriptions().Touch(ctx, newID[domain.PushSubscriptionID](e), t0)
			})
			wantErr(e.t, err, domain.ErrNotFound)

			n := mustWriteQuery(e, func(ctx context.Context, tx store.Tx) (int, error) {
				return tx.PushSubscriptions().DeleteUnusedBefore(ctx, t0.Add(48*time.Hour))
			})
			wantEqual(e.t, n, 1)
			wantSlice(e.t, e.pushSubsOf(ada), []domain.PushSubscription{used, fresh})
			wantEqual(e.t, e.pushMessageGone(msg.ID), true)
		},
		"GoWithTheirAccount": func(e *env) {
			ada := e.account(e.person("Ada", "Lovelace"), "ada@example.org")
			sub := e.pushSub(ada, "https://push.example.net/push/phone", t0)
			e.savePushSubs(sub)
			msg := e.pushMessage(sub, t0)
			e.createPushMessages(msg)

			e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Accounts().Delete(ctx, ada.ID) })

			_, err := query(e, func(ctx context.Context, tx store.Tx) (domain.PushSubscription, error) {
				return tx.PushSubscriptions().Get(ctx, sub.ID)
			})
			wantErr(e.t, err, domain.ErrNotFound)
			wantEqual(e.t, e.pushMessageGone(msg.ID), true)
		},
	})
}

func testPushOutbox(t *testing.T, newStore func(*testing.T) store.Store) {
	t.Helper()

	cases(t, newStore, map[string]func(e *env){
		"CreateGetUpdate": func(e *env) {
			ada := e.account(e.person("Ada", "Lovelace"), "ada@example.org")
			sub := e.pushSub(ada, "https://push.example.net/push/phone", t0)
			e.savePushSubs(sub)

			m := e.pushMessage(sub, t0.Add(500*time.Nanosecond))
			e.createPushMessages(m)
			m.CreatedAt, m.ExpiresAt, m.NextAttemptAt = t0, t0.Add(time.Hour), t0

			get := func() domain.PushMessage {
				return mustQuery(e, func(ctx context.Context, tx store.Tx) (domain.PushMessage, error) {
					return tx.PushOutbox().Get(ctx, m.ID)
				})
			}
			// The payload comes back as written, spaces included.
			wantEqual(e.t, get(), m)

			err := e.write(func(ctx context.Context, tx store.Tx) error { return tx.PushOutbox().Create(ctx, m) })
			if !isConflict(err) {
				e.t.Errorf("same id: want ErrConflict, got %v", err)
			}

			ghost := e.pushMessage(sub, t0)
			ghost.SubscriptionID = newID[domain.PushSubscriptionID](e)

			err = e.write(func(ctx context.Context, tx store.Tx) error { return tx.PushOutbox().Create(ctx, ghost) })
			if !isConflict(err) {
				e.t.Errorf("no subscription: want ErrConflict, got %v", err)
			}

			// A failed attempt, then success; the subscription and the creation
			// are not updated.
			m.Attempts, m.NextAttemptAt, m.LastError = 1, t0.Add(5*time.Minute), "push service answered 500"
			changed := m
			changed.SubscriptionID, changed.CreatedAt = newID[domain.PushSubscriptionID](e), t0.Add(time.Minute)
			e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.PushOutbox().Update(ctx, changed) })
			wantEqual(e.t, get(), m)

			m.Attempts, m.NextAttemptAt, m.SentAt, m.LastError = 2, time.Time{}, t0.Add(6*time.Minute), ""
			e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.PushOutbox().Update(ctx, m) })
			wantEqual(e.t, get(), m)

			m.SentAt, m.FailedAt = time.Time{}, t0.Add(7*time.Minute)
			e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.PushOutbox().Update(ctx, m) })
			wantEqual(e.t, get(), m)

			err = e.write(func(ctx context.Context, tx store.Tx) error {
				return tx.PushOutbox().Update(ctx, e.pushMessage(sub, t0))
			})
			wantErr(e.t, err, domain.ErrNotFound)
		},
		"ListDue": func(e *env) {
			ada := e.account(e.person("Ada", "Lovelace"), "ada@example.org")
			sub := e.pushSub(ada, "https://push.example.net/push/phone", t0)
			e.savePushSubs(sub)

			late := e.pushMessage(sub, t0)
			late.NextAttemptAt = t0.Add(time.Minute)
			first := e.pushMessage(sub, t0)
			second := e.pushMessage(sub, t0)
			future := e.pushMessage(sub, t0)
			future.NextAttemptAt = t0.Add(time.Hour)
			sent := e.pushMessage(sub, t0)
			sent.NextAttemptAt, sent.SentAt = time.Time{}, t0
			failed := e.pushMessage(sub, t0)
			failed.NextAttemptAt, failed.FailedAt = time.Time{}, t0
			e.createPushMessages(late, first, second, future, sent, failed)

			_, err := query(e, func(ctx context.Context, tx store.Tx) ([]domain.PushMessage, error) {
				return tx.PushOutbox().ListDue(ctx, t0, 10)
			})
			wantErr(e.t, err, store.ErrReadOnly)

			due := func(now time.Time, limit int) []domain.PushMessage {
				return mustWriteQuery(e, func(ctx context.Context, tx store.Tx) ([]domain.PushMessage, error) {
					return tx.PushOutbox().ListDue(ctx, now, limit)
				})
			}
			wantSlice(e.t, due(t0.Add(time.Minute), 10), []domain.PushMessage{first, second, late})
			wantSlice(e.t, due(t0.Add(time.Minute), 2), []domain.PushMessage{first, second})
			wantSlice(e.t, due(t0, 0), nil)
		},
		"ListDueSkipsLockedMessages": func(e *env) {
			ada := e.account(e.person("Ada", "Lovelace"), "ada@example.org")
			sub := e.pushSub(ada, "https://push.example.net/push/phone", t0)
			e.savePushSubs(sub)
			e.createPushMessages(e.pushMessage(sub, t0), e.pushMessage(sub, t0))

			// Two workers at once never take the same message. The memory store
			// serializes them; PostgreSQL skips the locked rows.
			var (
				mu   sync.Mutex
				seen = map[domain.PushMessageID]int{}
				wg   sync.WaitGroup
			)

			for range 2 {
				wg.Go(func() {
					err := e.write(func(ctx context.Context, tx store.Tx) error {
						due, err := tx.PushOutbox().ListDue(ctx, t0, 1)
						if err != nil || len(due) == 0 {
							return err
						}

						m := due[0]
						m.Attempts, m.NextAttemptAt = 1, t0.Add(time.Hour)

						mu.Lock()
						seen[m.ID]++
						mu.Unlock()

						return tx.PushOutbox().Update(ctx, m)
					})
					if err != nil {
						e.t.Error(err)
					}
				})
			}

			wg.Wait()

			for id, n := range seen {
				if n > 1 {
					e.t.Errorf("message %v taken %d times", id, n)
				}
			}
		},
		"DeleteCreatedBefore": func(e *env) {
			ada := e.account(e.person("Ada", "Lovelace"), "ada@example.org")
			sub := e.pushSub(ada, "https://push.example.net/push/phone", t0)
			e.savePushSubs(sub)

			old := e.pushMessage(sub, t0.Add(-8*24*time.Hour))
			oldSent := e.pushMessage(sub, t0.Add(-8*24*time.Hour))
			oldSent.NextAttemptAt, oldSent.SentAt = time.Time{}, oldSent.CreatedAt
			recent := e.pushMessage(sub, t0)
			e.createPushMessages(old, oldSent, recent)

			n := mustWriteQuery(e, func(ctx context.Context, tx store.Tx) (int, error) {
				return tx.PushOutbox().DeleteCreatedBefore(ctx, t0.Add(-7*24*time.Hour))
			})
			wantEqual(e.t, n, 2)
			wantEqual(e.t, e.pushMessageGone(old.ID), true)
			wantEqual(e.t, e.pushMessageGone(oldSent.ID), true)
			wantEqual(e.t, e.pushMessageGone(recent.ID), false)
		},
	})
}

// mustWriteQuery runs fn in a read-write transaction and returns its
// result; it fails the test on error.
func mustWriteQuery[T any](e *env, fn func(ctx context.Context, tx store.Tx) (T, error)) T {
	e.t.Helper()

	var v T

	e.mustWrite(func(ctx context.Context, tx store.Tx) error {
		var err error
		v, err = fn(ctx, tx)

		return err
	})

	return v
}

func isNotFound(err error) bool { return errors.Is(err, domain.ErrNotFound) }

func isConflict(err error) bool { return errors.Is(err, domain.ErrConflict) }
