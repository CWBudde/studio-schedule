package storetest

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// feed builds an active feed of a for scope (and location) created at at,
// with a token hash derived from its ID; it is not written.
func (e *env) feed(a domain.Account, scope domain.FeedScope, location domain.LocationID, at time.Time) domain.CalendarFeed {
	e.t.Helper()

	f := domain.CalendarFeed{
		ID: newID[domain.CalendarFeedID](e), AccountID: a.ID, Scope: scope, LocationID: location, CreatedAt: at,
	}
	f.TokenHash = domain.HashToken(f.ID.String())
	valid(e.t, f)

	return f
}

func (e *env) createFeeds(fs ...domain.CalendarFeed) {
	e.t.Helper()

	e.mustWrite(func(ctx context.Context, tx store.Tx) error {
		for _, f := range fs {
			if err := tx.CalendarFeeds().Create(ctx, f); err != nil {
				return err
			}
		}

		return nil
	})
}

func (e *env) feedsOf(a domain.Account) []domain.CalendarFeed {
	e.t.Helper()

	return mustQuery(e, func(ctx context.Context, tx store.Tx) ([]domain.CalendarFeed, error) {
		return tx.CalendarFeeds().ListByAccount(ctx, a.ID)
	})
}

func (e *env) getFeed(id domain.CalendarFeedID) domain.CalendarFeed {
	e.t.Helper()

	return mustQuery(e, func(ctx context.Context, tx store.Tx) (domain.CalendarFeed, error) {
		return tx.CalendarFeeds().Get(ctx, id)
	})
}

func testCalendarFeeds(t *testing.T, newStore func(*testing.T) store.Store) {
	t.Helper()

	cases(t, newStore, map[string]func(e *env){
		"CreateGetAndList": func(e *env) {
			ada := e.account(e.person("Ada", "Lovelace"), "ada@example.org")
			bob := e.account(e.person("Bob", "Builder"), "bob@example.org")
			north := e.location("North Studio")

			bookings := e.feed(ada, domain.FeedBookings, domain.LocationID{}, t0.Add(time.Hour))
			school := e.feed(ada, domain.FeedSchedule, domain.LocationID{}, t0)
			// A fraction of a microsecond is cut off like in PostgreSQL.
			local := e.feed(ada, domain.FeedSchedule, north.ID, t0.Add(500*time.Nanosecond))
			local.LastUsedAt = t0.Add(time.Minute + 500*time.Nanosecond)
			other := e.feed(bob, domain.FeedBookings, domain.LocationID{}, t0)
			revoked := e.feed(bob, domain.FeedTeaching, domain.LocationID{}, t0)
			revoked.RevokedAt = t0.Add(time.Minute)
			e.createFeeds(bookings, school, local, other, revoked)
			local.CreatedAt, local.LastUsedAt = t0, t0.Add(time.Minute)

			wantEqual(e.t, e.getFeed(local.ID), local)
			wantEqual(e.t, e.getFeed(revoked.ID), revoked)

			got := mustQuery(e, func(ctx context.Context, tx store.Tx) (domain.CalendarFeed, error) {
				return tx.CalendarFeeds().GetByTokenHash(ctx, school.TokenHash)
			})
			wantEqual(e.t, got, school)

			// A revoked feed is found by its token, so it can be refused.
			got = mustQuery(e, func(ctx context.Context, tx store.Tx) (domain.CalendarFeed, error) {
				return tx.CalendarFeeds().GetByTokenHash(ctx, revoked.TokenHash)
			})
			wantEqual(e.t, got, revoked)

			_, err := query(e, func(ctx context.Context, tx store.Tx) (domain.CalendarFeed, error) {
				return tx.CalendarFeeds().GetByTokenHash(ctx, domain.HashToken("unknown"))
			})
			wantErr(e.t, err, domain.ErrNotFound)

			_, err = query(e, func(ctx context.Context, tx store.Tx) (domain.CalendarFeed, error) {
				return tx.CalendarFeeds().Get(ctx, newID[domain.CalendarFeedID](e))
			})
			wantErr(e.t, err, domain.ErrNotFound)

			// Active feeds only, by creation and then ID (the sequence's IDs
			// ascend).
			wantSlice(e.t, e.feedsOf(ada), []domain.CalendarFeed{school, local, bookings})
			wantSlice(e.t, e.feedsOf(bob), []domain.CalendarFeed{other})
			wantSlice(e.t, e.feedsOf(e.account(e.person("Cy", "Nobody"), "cy@example.org")), nil)
		},
		"Create": func(e *env) {
			ada := e.account(e.person("Ada", "Lovelace"), "ada@example.org")
			north := e.location("North Studio")
			f := e.feed(ada, domain.FeedSchedule, north.ID, t0)

			err := e.read(func(ctx context.Context, tx store.Tx) error { return tx.CalendarFeeds().Create(ctx, f) })
			wantErr(e.t, err, store.ErrReadOnly)

			e.createFeeds(f)

			sameID := e.feed(ada, domain.FeedBookings, domain.LocationID{}, t0)
			sameID.ID = f.ID
			sameToken := e.feed(ada, domain.FeedBookings, domain.LocationID{}, t0)
			sameToken.TokenHash = f.TokenHash
			noAccount := e.feed(ada, domain.FeedBookings, domain.LocationID{}, t0)
			noAccount.AccountID = newID[domain.AccountID](e)
			noLocation := e.feed(ada, domain.FeedSchedule, newID[domain.LocationID](e), t0)

			for name, bad := range map[string]domain.CalendarFeed{
				"id": sameID, "token": sameToken, "account": noAccount, "location": noLocation,
			} {
				err := e.write(func(ctx context.Context, tx store.Tx) error { return tx.CalendarFeeds().Create(ctx, bad) })
				if !errorsIs(err, domain.ErrConflict) || errorsIs(err, domain.ErrDuplicate) {
					e.t.Fatalf("%s: got %v, want a conflict that is no duplicate", name, err)
				}
			}
		},
		"OneActivePerSubject": func(e *env) {
			ada := e.account(e.person("Ada", "Lovelace"), "ada@example.org")
			bob := e.account(e.person("Bob", "Builder"), "bob@example.org")
			north, south := e.location("North Studio"), e.location("South Studio")

			school := e.feed(ada, domain.FeedSchedule, domain.LocationID{}, t0)
			e.createFeeds(school,
				// Other subjects and other accounts are free.
				e.feed(ada, domain.FeedSchedule, north.ID, t0), e.feed(ada, domain.FeedSchedule, south.ID, t0),
				e.feed(ada, domain.FeedBookings, domain.LocationID{}, t0),
				e.feed(ada, domain.FeedTeaching, domain.LocationID{}, t0),
				e.feed(bob, domain.FeedSchedule, domain.LocationID{}, t0),
			)

			for name, dup := range map[string]domain.CalendarFeed{
				"whole school": e.feed(ada, domain.FeedSchedule, domain.LocationID{}, t0),
				"location":     e.feed(ada, domain.FeedSchedule, north.ID, t0),
				"bookings":     e.feed(ada, domain.FeedBookings, domain.LocationID{}, t0),
			} {
				err := e.write(func(ctx context.Context, tx store.Tx) error { return tx.CalendarFeeds().Create(ctx, dup) })
				if !errorsIs(err, domain.ErrDuplicate) {
					e.t.Fatalf("%s: got %v, want %v", name, err, domain.ErrDuplicate)
				}
			}

			// A revoked feed does not count: revoked ones may pile up.
			old := e.feed(ada, domain.FeedSchedule, domain.LocationID{}, t0)
			old.RevokedAt = t0
			e.createFeeds(old)

			// Regenerating: revoke the active one, then create the new one.
			renewed := e.feed(ada, domain.FeedSchedule, domain.LocationID{}, t0.Add(time.Hour))
			e.mustWrite(func(ctx context.Context, tx store.Tx) error {
				n, err := tx.CalendarFeeds().RevokeActive(ctx, ada.ID, domain.FeedSchedule, domain.LocationID{}, t0.Add(time.Hour))
				if err != nil {
					return err
				}

				wantEqual(e.t, n, 1)

				return tx.CalendarFeeds().Create(ctx, renewed)
			})

			school.RevokedAt = t0.Add(time.Hour)
			wantEqual(e.t, e.getFeed(school.ID), school)
			wantEqual(e.t, e.getFeed(renewed.ID), renewed)
		},
		"RegenerateConcurrently": func(e *env) {
			// Regenerating a link under the account's person lock (the
			// service's way) never collides with a concurrent regeneration:
			// each revokes the one before, and one stays active.
			p := e.person("Ada", "Lovelace")
			ada := e.account(p, "ada@example.org")

			const n = 8

			feeds := make([]domain.CalendarFeed, n)
			for i := range feeds {
				feeds[i] = e.feed(ada, domain.FeedSchedule, domain.LocationID{}, t0.Add(time.Duration(i)*time.Minute))
			}

			var wg sync.WaitGroup

			errs := make(chan error, n)

			for _, f := range feeds {
				wg.Go(func() {
					errs <- e.write(func(ctx context.Context, tx store.Tx) error {
						if err := tx.Persons().Lock(ctx, p.ID); err != nil {
							return err
						}

						if _, err := tx.CalendarFeeds().RevokeActive(ctx, ada.ID, f.Scope, f.LocationID, f.CreatedAt.Add(time.Hour)); err != nil {
							return err
						}

						return tx.CalendarFeeds().Create(ctx, f)
					})
				})
			}

			wg.Wait()
			close(errs)

			for err := range errs {
				if err != nil {
					e.t.Fatal(err)
				}
			}

			if active := e.feedsOf(ada); len(active) != 1 {
				e.t.Fatalf("active feeds %+v, want one", active)
			}
		},
		"RevokeActive": func(e *env) {
			ada := e.account(e.person("Ada", "Lovelace"), "ada@example.org")
			north := e.location("North Studio")
			school := e.feed(ada, domain.FeedSchedule, domain.LocationID{}, t0)
			local := e.feed(ada, domain.FeedSchedule, north.ID, t0)
			e.createFeeds(school, local)

			revoke := func(scope domain.FeedScope, location domain.LocationID) int {
				e.t.Helper()

				var n int

				e.mustWrite(func(ctx context.Context, tx store.Tx) error {
					var err error
					n, err = tx.CalendarFeeds().RevokeActive(ctx, ada.ID, scope, location, t0.Add(time.Minute))

					return err
				})

				return n
			}

			err := e.read(func(ctx context.Context, tx store.Tx) error {
				_, err := tx.CalendarFeeds().RevokeActive(ctx, ada.ID, domain.FeedSchedule, north.ID, t0)

				return err
			})
			wantErr(e.t, err, store.ErrReadOnly)

			// Only the subject's feed: the location's, not the school's.
			wantEqual(e.t, revoke(domain.FeedSchedule, north.ID), 1)
			wantEqual(e.t, revoke(domain.FeedSchedule, north.ID), 0)
			wantEqual(e.t, revoke(domain.FeedBookings, domain.LocationID{}), 0)

			local.RevokedAt = t0.Add(time.Minute)
			wantEqual(e.t, e.getFeed(local.ID), local)
			wantEqual(e.t, e.getFeed(school.ID), school)
			wantSlice(e.t, e.feedsOf(ada), []domain.CalendarFeed{school})

			wantEqual(e.t, revoke(domain.FeedSchedule, domain.LocationID{}), 1)
			wantSlice(e.t, e.feedsOf(ada), nil)
		},
		"Revoke": func(e *env) {
			ada := e.account(e.person("Ada", "Lovelace"), "ada@example.org")
			f, kept := e.feed(ada, domain.FeedBookings, domain.LocationID{}, t0), e.feed(ada, domain.FeedTeaching, domain.LocationID{}, t0)
			e.createFeeds(f, kept)

			revoke := func(id domain.CalendarFeedID, at time.Time) error {
				return e.write(func(ctx context.Context, tx store.Tx) error { return tx.CalendarFeeds().Revoke(ctx, id, at) })
			}

			err := e.read(func(ctx context.Context, tx store.Tx) error { return tx.CalendarFeeds().Revoke(ctx, f.ID, t0) })
			wantErr(e.t, err, store.ErrReadOnly)

			if err := revoke(f.ID, t0.Add(time.Hour)); err != nil {
				e.t.Fatal(err)
			}

			// Revoked already, or no feed at all.
			wantErr(e.t, revoke(f.ID, t0.Add(2*time.Hour)), domain.ErrNotFound)
			wantErr(e.t, revoke(newID[domain.CalendarFeedID](e), t0), domain.ErrNotFound)

			f.RevokedAt = t0.Add(time.Hour)
			wantEqual(e.t, e.getFeed(f.ID), f)
			wantSlice(e.t, e.feedsOf(ada), []domain.CalendarFeed{kept})
		},
		"TouchLastUsed": func(e *env) {
			ada := e.account(e.person("Ada", "Lovelace"), "ada@example.org")
			f := e.feed(ada, domain.FeedBookings, domain.LocationID{}, t0)
			e.createFeeds(f)

			touch := func(id domain.CalendarFeedID, at time.Time) error {
				return e.write(func(ctx context.Context, tx store.Tx) error {
					return tx.CalendarFeeds().TouchLastUsed(ctx, id, at)
				})
			}

			err := e.read(func(ctx context.Context, tx store.Tx) error {
				return tx.CalendarFeeds().TouchLastUsed(ctx, f.ID, t0)
			})
			wantErr(e.t, err, store.ErrReadOnly)

			for _, at := range []time.Time{t0.Add(time.Hour), t0.Add(25*time.Hour + 500*time.Nanosecond)} {
				if err := touch(f.ID, at); err != nil {
					e.t.Fatal(err)
				}
			}

			f.LastUsedAt = t0.Add(25 * time.Hour)
			wantEqual(e.t, e.getFeed(f.ID), f)

			// A revoked feed is touched too; it is still refused when used.
			if err := e.write(func(ctx context.Context, tx store.Tx) error {
				return tx.CalendarFeeds().Revoke(ctx, f.ID, t0.Add(26*time.Hour))
			}); err != nil {
				e.t.Fatal(err)
			}

			if err := touch(f.ID, t0.Add(27*time.Hour)); err != nil {
				e.t.Fatal(err)
			}

			wantErr(e.t, touch(newID[domain.CalendarFeedID](e), t0), domain.ErrNotFound)
		},
		"GoWithTheirLocation": func(e *env) {
			ada := e.account(e.person("Ada", "Lovelace"), "ada@example.org")
			north, south := e.location("North Studio"), e.location("South Studio")
			gone, revoked := e.feed(ada, domain.FeedSchedule, north.ID, t0), e.feed(ada, domain.FeedSchedule, north.ID, t0)
			revoked.RevokedAt = t0
			kept := e.feed(ada, domain.FeedSchedule, south.ID, t0)
			e.createFeeds(gone, revoked, kept)

			// A feed does not keep its location from being deleted, and goes
			// with it.
			e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Locations().Delete(ctx, north.ID) })

			for _, f := range []domain.CalendarFeed{gone, revoked} {
				_, err := query(e, func(ctx context.Context, tx store.Tx) (domain.CalendarFeed, error) {
					return tx.CalendarFeeds().Get(ctx, f.ID)
				})
				wantErr(e.t, err, domain.ErrNotFound)
			}

			wantSlice(e.t, e.feedsOf(ada), []domain.CalendarFeed{kept})
		},
	})
}
