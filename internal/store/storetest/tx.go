package storetest

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/store"
)

var errFull = errors.New("storetest: lesson full")

func testTx(t *testing.T, newStore func(*testing.T) store.Store) {
	t.Helper()

	cases(t, newStore, map[string]func(e *env){
		"NestedIsRejected": func(e *env) {
			p := e.person("Nora", "Nested")

			var inner error

			err := e.write(func(ctx context.Context, tx store.Tx) error {
				if err := tx.Persons().Delete(ctx, p.ID); err != nil {
					return err
				}

				inner = e.st.InTx(ctx, store.ReadOnly, func(context.Context, store.Tx) error { return nil })

				return inner
			})
			wantErr(e.t, inner, store.ErrNestedTx)
			wantErr(e.t, err, store.ErrNestedTx)

			// The outer transaction failed, so the delete is rolled back.
			wantEqual(e.t, mustQuery(e, func(ctx context.Context, tx store.Tx) (domain.Person, error) {
				return tx.Persons().Get(ctx, p.ID)
			}), p)
		},
		"CanceledContextRunsNothing": func(e *env) {
			ctx, cancel := context.WithCancel(e.t.Context())
			cancel()

			called := false

			err := e.st.InTx(ctx, store.ReadWrite, func(context.Context, store.Tx) error {
				called = true

				return nil
			})
			wantErr(e.t, err, context.Canceled)

			if called {
				e.t.Fatal("InTx with a canceled context ran the callback")
			}
		},
		"CanceledBeforeCommitRollsBack": func(e *env) {
			ctx, cancel := context.WithCancel(e.t.Context())
			defer cancel()

			p := domain.Person{ID: newID[domain.PersonID](e), GivenName: "Cleo", FamilyName: "Canceled"}

			err := e.st.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
				if err := tx.Persons().Create(ctx, p); err != nil {
					return err
				}

				cancel()

				return nil
			})
			wantErr(e.t, err, context.Canceled)

			_, err = query(e, func(ctx context.Context, tx store.Tx) (domain.Person, error) {
				return tx.Persons().Get(ctx, p.ID)
			})
			wantErr(e.t, err, domain.ErrNotFound)
		},
		"PanicRollsBackAndPropagates": func(e *env) {
			p := domain.Person{ID: newID[domain.PersonID](e), GivenName: "Pia", FamilyName: "Panic"}

			func() {
				defer func() {
					if r := recover(); r != "boom" {
						e.t.Errorf("recovered %v, want the callback's panic", r)
					}
				}()

				_ = e.write(func(ctx context.Context, tx store.Tx) error {
					if err := tx.Persons().Create(ctx, p); err != nil {
						return err
					}

					panic("boom")
				})
			}()

			_, err := query(e, func(ctx context.Context, tx store.Tx) (domain.Person, error) {
				return tx.Persons().Get(ctx, p.ID)
			})
			wantErr(e.t, err, domain.ErrNotFound)

			// The store stays usable after the panic.
			e.person("Pia", "Afterwards")
		},
		"InstantsAreUTCMicroseconds": func(e *env) {
			s := e.setting()
			berlin, err := time.LoadLocation("Europe/Berlin")
			if err != nil {
				e.t.Fatal(err)
			}

			start := t0.In(berlin).Add(1500 * time.Nanosecond)
			l := e.newLesson(s, start, 5)
			e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Lessons().Create(ctx, l) })

			got := mustQuery(e, func(ctx context.Context, tx store.Tx) (domain.Lesson, error) {
				return tx.Lessons().Get(ctx, l.ID)
			})

			want := l
			want.StartsAt = t0.Add(time.Microsecond)
			want.EndsAt = want.StartsAt.Add(time.Hour)
			wantEqual(e.t, got, want)
		},
		"LockNeedsReadWrite": func(e *env) {
			l := e.lesson(e.setting(), t0, 5)

			err := e.read(func(ctx context.Context, tx store.Tx) error {
				_, err := tx.Lessons().Lock(ctx, l.ID)

				return err
			})
			wantErr(e.t, err, store.ErrReadOnly)
		},
		"LockMissingIsNotFound": func(e *env) {
			err := e.write(func(ctx context.Context, tx store.Tx) error {
				_, err := tx.Lessons().Lock(ctx, newID[domain.LessonID](e))

				return err
			})
			wantErr(e.t, err, domain.ErrNotFound)
		},
		"LockSerialisesCapacityChecks": func(e *env) { testLockCapacity(e) },
	})
}

// testLockCapacity lets many concurrent transactions book a small lesson.
// Each one locks the lesson, counts the active bookings and books only if a
// place is left, so exactly capacity bookings must succeed.
func testLockCapacity(e *env) {
	const (
		capacity = 5
		attempts = 20
	)

	l := e.lesson(e.setting(), t0, capacity)

	bookings := make([]domain.Booking, attempts)
	for i := range bookings {
		bookings[i] = e.newBooking(l, e.person("Guest", fmt.Sprintf("Number %02d", i)), t0.Add(-time.Hour))
	}

	var (
		wg   sync.WaitGroup
		errs = make([]error, attempts)
	)

	for i, b := range bookings {
		wg.Go(func() {
			errs[i] = e.write(func(ctx context.Context, tx store.Tx) error {
				lesson, err := tx.Lessons().Lock(ctx, b.LessonID)
				if err != nil {
					return err
				}

				n, err := tx.Bookings().CountActive(ctx, lesson.ID)
				if err != nil {
					return err
				}

				if n >= lesson.Capacity {
					return errFull
				}

				return tx.Bookings().Create(ctx, b)
			})
		})
	}

	wg.Wait()

	booked, full := 0, 0

	for _, err := range errs {
		switch {
		case err == nil:
			booked++
		case errors.Is(err, errFull):
			full++
		default:
			e.t.Fatalf("unexpected error: %v", err)
		}
	}

	if booked != capacity || full != attempts-capacity {
		e.t.Fatalf("booked %d, full %d; want %d and %d", booked, full, capacity, attempts-capacity)
	}

	n := mustQuery(e, func(ctx context.Context, tx store.Tx) (int, error) { return tx.Bookings().CountActive(ctx, l.ID) })
	wantEqual(e.t, n, capacity)
}
