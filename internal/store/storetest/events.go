package storetest

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// newEvent returns a free, published event of the course type with ten
// places, without creating it.
func (e *env) newEvent(ct domain.CourseType, slug string) domain.Event {
	e.t.Helper()

	ev := domain.Event{
		ID: newID[domain.EventID](e), Slug: slug, Title: "Summer Intensive", CourseTypeID: ct.ID, Capacity: 10,
		Status: domain.EventPublished, CreatedAt: t0.Add(-7 * 24 * time.Hour), UpdatedAt: t0.Add(-7 * 24 * time.Hour),
	}
	valid(e.t, ev)

	return ev
}

func (e *env) event(ct domain.CourseType, slug string) domain.Event {
	e.t.Helper()

	ev := e.newEvent(ct, slug)
	e.createEvents(ev)

	return ev
}

func (e *env) createEvents(evs ...domain.Event) {
	e.t.Helper()
	e.mustWrite(func(ctx context.Context, tx store.Tx) error {
		for _, ev := range evs {
			if err := tx.Events().Create(ctx, ev); err != nil {
				return err
			}
		}

		return nil
	})
}

// newEventSession returns a one-hour workshop lesson of the event starting at
// start, without creating it.
func (e *env) newEventSession(s setting, ev domain.Event, start time.Time) domain.Lesson {
	e.t.Helper()

	l := e.newLesson(s, start, ev.Capacity)
	l.Kind, l.EventID, l.CourseTypeID = domain.LessonWorkshop, ev.ID, ev.CourseTypeID
	valid(e.t, l)

	return l
}

func (e *env) eventSession(s setting, ev domain.Event, start time.Time) domain.Lesson {
	e.t.Helper()

	l := e.newEventSession(s, ev, start)
	e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Lessons().Create(ctx, l) })

	return l
}

// canceledEventSession creates a session of the event the school canceled.
func (e *env) canceledEventSession(s setting, ev domain.Event, start time.Time) domain.Lesson {
	e.t.Helper()

	l := e.newEventSession(s, ev, start)
	l.Status, l.CanceledAt = domain.LessonCanceled, t0
	valid(e.t, l)
	e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Lessons().Create(ctx, l) })

	return l
}

// newRegistration returns a free registration of p with a place, made at
// at, without creating it.
func (e *env) newRegistration(ev domain.Event, p domain.Person, at time.Time) domain.EventRegistration {
	e.t.Helper()

	r := domain.EventRegistration{
		ID: newID[domain.EventRegistrationID](e), EventID: ev.ID, PersonID: p.ID, Status: domain.EventRegistered,
		Payment: domain.EventPaid, CreatedAt: at, UpdatedAt: at,
	}
	valid(e.t, r)

	return r
}

// newGuestRegistration returns a free registration of a guest with a place,
// made at at, without creating it.
func (e *env) newGuestRegistration(ev domain.Event, name, email string, at time.Time) domain.EventRegistration {
	e.t.Helper()

	r := domain.EventRegistration{
		ID: newID[domain.EventRegistrationID](e), EventID: ev.ID, GuestName: name, GuestEmail: email,
		Status: domain.EventRegistered, Payment: domain.EventPaid, CreatedAt: at, UpdatedAt: at,
	}
	valid(e.t, r)

	return r
}

func (e *env) createRegistrations(rs ...domain.EventRegistration) {
	e.t.Helper()
	e.mustWrite(func(ctx context.Context, tx store.Tx) error {
		for _, r := range rs {
			valid(e.t, r)

			if err := tx.EventRegistrations().Create(ctx, r); err != nil {
				return err
			}
		}

		return nil
	})
}

func (e *env) updateRegistration(r domain.EventRegistration) {
	e.t.Helper()
	valid(e.t, r)
	e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.EventRegistrations().Update(ctx, r) })
}

func (e *env) getRegistration(id domain.EventRegistrationID) domain.EventRegistration {
	e.t.Helper()

	return mustQuery(e, func(ctx context.Context, tx store.Tx) (domain.EventRegistration, error) {
		return tx.EventRegistrations().Get(ctx, id)
	})
}

// invited turns r into an invite with the token hash, sent a minute after
// it was made and expiring at expires; it is not stored.
func invited(r domain.EventRegistration, token byte, expires time.Time) domain.EventRegistration {
	r.Status, r.InvitedAt, r.ExpiresAt = domain.EventInvited, r.CreatedAt.Add(time.Minute), expires
	r.InviteTokenHash = [32]byte{31: token}

	return r
}

// waiting turns r into a registration waiting for a place; it is not
// stored.
func waiting(r domain.EventRegistration) domain.EventRegistration {
	r.Status = domain.EventWaiting

	return r
}

// canceledReg turns r into a registration canceled at r's creation plus a
// minute; it is not stored.
func canceledReg(r domain.EventRegistration) domain.EventRegistration {
	r.Status, r.CanceledAt = domain.EventRegistrationCanceled, r.CreatedAt.Add(time.Minute)
	r.UpdatedAt = r.CanceledAt
	r.InviteTokenHash, r.InvitedAt, r.ExpiresAt = [32]byte{}, time.Time{}, time.Time{}

	return r
}

// paidWith turns r into a registration for 45.00 paid with the receipt; it
// is not stored.
func paidWith(r domain.EventRegistration, receipt domain.Receipt) domain.EventRegistration {
	r.Price, r.VATRate, r.Payment, r.ReceiptID = 4500, 1900, domain.EventPaid, receipt.ID

	return r
}

func testEvents(t *testing.T, newStore func(*testing.T) store.Store) {
	t.Helper()

	t.Run("CRUD", func(t *testing.T) {
		testCRUD(t, newStore, crud[domain.EventID, domain.Event]{
			id: func(ev domain.Event) domain.EventID { return ev.ID },
			get: func(ctx context.Context, tx store.Tx, id domain.EventID) (domain.Event, error) {
				return tx.Events().Get(ctx, id)
			},
			create: func(ctx context.Context, tx store.Tx, ev domain.Event) error { return tx.Events().Create(ctx, ev) },
			update: func(ctx context.Context, tx store.Tx, ev domain.Event) error { return tx.Events().Update(ctx, ev) },
			remove: func(ctx context.Context, tx store.Tx, id domain.EventID) error { return tx.Events().Delete(ctx, id) },
			fixtures: func(e *env) (domain.Event, domain.Event) {
				minimal := e.newEvent(e.courseType("Ballet Basics"), "summer-intensive-2026")
				full := minimal
				full.Slug = "summer-intensive-2026-b"
				full.Title = "Summer Intensive, Part B"
				full.Description = "Five mornings of technique and repertoire."
				full.CourseTypeID = e.courseType("Contemporary").ID
				full.Capacity = 24
				full.Price, full.VATRate = 18000, 1900
				full.Status = domain.EventCanceled
				full.SignupOpensAt = t0.Add(-30 * 24 * time.Hour)
				full.SignupClosesAt = t0.Add(-time.Hour)
				full.CancelUntil = t0.Add(-48 * time.Hour)
				full.UpdatedAt = t0.Add(-time.Hour)
				full.CreatedBy = e.account(e.person("Olga", "Office"), "office@example.org").ID
				valid(e.t, full)

				return minimal, full
			},
		})
	})

	cases(t, newStore, map[string]func(e *env){
		"InstantsAreUTCMicroseconds": func(e *env) {
			berlin, err := time.LoadLocation("Europe/Berlin")
			if err != nil {
				e.t.Fatal(err)
			}

			ev := e.newEvent(e.courseType("Ballet Basics"), "summer-intensive")
			ev.CreatedAt = t0.In(berlin).Add(1500 * time.Nanosecond)
			ev.UpdatedAt = ev.CreatedAt
			ev.SignupOpensAt = ev.CreatedAt
			ev.SignupClosesAt = t0.In(berlin).Add(time.Hour + 500*time.Nanosecond)
			ev.CancelUntil = ev.SignupClosesAt
			valid(e.t, ev)
			e.createEvents(ev)

			want := ev
			want.CreatedAt, want.UpdatedAt, want.SignupOpensAt = t0.Add(time.Microsecond), t0.Add(time.Microsecond),
				t0.Add(time.Microsecond)
			want.SignupClosesAt, want.CancelUntil = t0.Add(time.Hour), t0.Add(time.Hour)
			wantEqual(e.t, mustQuery(e, func(ctx context.Context, tx store.Tx) (domain.Event, error) {
				return tx.Events().Get(ctx, ev.ID)
			}), want)
		},
		"BySlug": func(e *env) {
			ct := e.courseType("Ballet Basics")
			a := e.event(ct, "summer-intensive")
			b := e.event(ct, "winter-intensive")

			bySlug := func(slug string) (domain.Event, error) {
				return query(e, func(ctx context.Context, tx store.Tx) (domain.Event, error) {
					return tx.Events().BySlug(ctx, slug)
				})
			}

			for _, want := range []domain.Event{a, b} {
				got, err := bySlug(want.Slug)
				if err != nil {
					e.t.Fatalf("BySlug %q: %v", want.Slug, err)
				}

				wantEqual(e.t, got, want)
			}

			_, err := bySlug("spring-intensive")
			wantErr(e.t, err, domain.ErrNotFound)

			// Exactly: no prefix or case-folded match.
			_, err = bySlug("summer")
			wantErr(e.t, err, domain.ErrNotFound)
			_, err = bySlug("Summer-Intensive")
			wantErr(e.t, err, domain.ErrNotFound)
		},
		"DuplicateSlugIsDuplicate": func(e *env) {
			ct := e.courseType("Ballet Basics")
			e.event(ct, "summer-intensive")
			other := e.event(ct, "winter-intensive")

			dup := e.newEvent(ct, "summer-intensive")
			err := e.write(func(ctx context.Context, tx store.Tx) error { return tx.Events().Create(ctx, dup) })
			wantErr(e.t, err, domain.ErrDuplicate)

			moved := other
			moved.Slug = "summer-intensive"
			err = e.write(func(ctx context.Context, tx store.Tx) error { return tx.Events().Update(ctx, moved) })
			wantErr(e.t, err, domain.ErrDuplicate)

			wantEqual(e.t, mustQuery(e, func(ctx context.Context, tx store.Tx) (domain.Event, error) {
				return tx.Events().Get(ctx, other.ID)
			}), other)
		},
		"ConflictIsNotDuplicate": func(e *env) {
			ct := e.courseType("Ballet Basics")
			taken := e.event(ct, "summer-intensive")

			again := taken
			again.Slug = "another-slug"
			err := e.write(func(ctx context.Context, tx store.Tx) error { return tx.Events().Create(ctx, again) })
			wantErr(e.t, err, domain.ErrConflict)
			wantNotErr(e.t, err, domain.ErrDuplicate)

			for i, c := range []struct {
				name   string
				mutate func(*domain.Event)
			}{
				{"course type", func(ev *domain.Event) { ev.CourseTypeID = newID[domain.CourseTypeID](e) }},
				{"creator", func(ev *domain.Event) { ev.CreatedBy = newID[domain.AccountID](e) }},
			} {
				name, mutate := c.name, c.mutate
				ev := e.newEvent(ct, fmt.Sprintf("missing-%d", i))
				mutate(&ev)

				err := e.write(func(ctx context.Context, tx store.Tx) error { return tx.Events().Create(ctx, ev) })
				if !errors.Is(err, domain.ErrConflict) || errors.Is(err, domain.ErrDuplicate) {
					e.t.Errorf("create with missing %s: want a conflict, got %v", name, err)
				}

				changed := taken
				mutate(&changed)

				err = e.write(func(ctx context.Context, tx store.Tx) error { return tx.Events().Update(ctx, changed) })
				if !errors.Is(err, domain.ErrConflict) || errors.Is(err, domain.ErrDuplicate) {
					e.t.Errorf("update with missing %s: want a conflict, got %v", name, err)
				}
			}

			wantEqual(e.t, mustQuery(e, func(ctx context.Context, tx store.Tx) (domain.Event, error) {
				return tx.Events().Get(ctx, taken.ID)
			}), taken)
		},
		"DeleteInUse": func(e *env) {
			s := e.setting()
			withSession := e.event(s.courseType, "with-session")
			withCanceledSession := e.event(s.courseType, "with-canceled-session")
			withRegistration := e.event(s.courseType, "with-registration")
			withCanceled := e.event(s.courseType, "with-canceled-registration")

			l := e.eventSession(s, withSession, t0)
			e.canceledEventSession(s, withCanceledSession, t0)
			e.createRegistrations(
				e.newRegistration(withRegistration, e.person("Ada", "Dancer"), t0),
				canceledReg(e.newRegistration(withCanceled, e.person("Ben", "Dancer"), t0)),
			)

			for _, ev := range []domain.Event{withSession, withCanceledSession, withRegistration, withCanceled} {
				err := e.write(func(ctx context.Context, tx store.Tx) error { return tx.Events().Delete(ctx, ev.ID) })
				wantErr(e.t, err, domain.ErrInUse)
				wantErr(e.t, err, domain.ErrConflict)

				wantEqual(e.t, mustQuery(e, func(ctx context.Context, tx store.Tx) (domain.Event, error) {
					return tx.Events().Get(ctx, ev.ID)
				}), ev)
			}

			// Without its session, the event goes.
			e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Lessons().Delete(ctx, l.ID) })
			e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Events().Delete(ctx, withSession.ID) })

			_, err := query(e, func(ctx context.Context, tx store.Tx) (domain.Event, error) {
				return tx.Events().Get(ctx, withSession.ID)
			})
			wantErr(e.t, err, domain.ErrNotFound)
		},
		"DeleteRemovesImage": func(e *env) {
			ct := e.courseType("Ballet Basics")
			ev := e.event(ct, "summer-intensive")
			kept := e.event(ct, "winter-intensive")
			img := domain.Background{ContentType: domain.BackgroundJPEG, Data: []byte{0xFF, 0xD8, 0xFF, 'e', 'v'}}

			e.mustWrite(func(ctx context.Context, tx store.Tx) error {
				if err := tx.Events().PutImage(ctx, ev.ID, img); err != nil {
					return err
				}

				return tx.Events().PutImage(ctx, kept.ID, img)
			})
			e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Events().Delete(ctx, ev.ID) })

			_, err := query(e, func(ctx context.Context, tx store.Tx) (domain.Background, error) {
				return tx.Events().Image(ctx, ev.ID)
			})
			wantErr(e.t, err, domain.ErrNotFound)
			_, err = query(e, func(ctx context.Context, tx store.Tx) (domain.BackgroundInfo, error) {
				return tx.Events().ImageInfo(ctx, ev.ID)
			})
			wantErr(e.t, err, domain.ErrNotFound)

			// The slug is free again, and the new event has no image.
			again := e.event(ct, ev.Slug)
			if got, err := query(e, func(ctx context.Context, tx store.Tx) (domain.Background, error) {
				return tx.Events().Image(ctx, again.ID)
			}); err != nil || !got.IsZero() {
				e.t.Fatalf("image of a new event: %v, %v", got.ContentType, err)
			}

			if bi, err := query(e, func(ctx context.Context, tx store.Tx) (domain.BackgroundInfo, error) {
				return tx.Events().ImageInfo(ctx, kept.ID)
			}); err != nil || bi != (domain.BackgroundInfo{ContentType: img.ContentType, Digest: img.Digest()}) {
				e.t.Fatalf("another event's image: %+v, %v", bi, err)
			}
		},
		"List": func(e *env) {
			s := e.setting()
			// Generated in one order, created in another: the order is not
			// the insertion order.
			tie1 := e.newEvent(s.courseType, "tie-one")
			tie2 := e.newEvent(s.courseType, "tie-two")
			first := e.newEvent(s.courseType, "first")
			none1 := e.newEvent(s.courseType, "none-one")
			none2 := e.newEvent(s.courseType, "none-two")
			e.createEvents(none2, tie2, none1, first, tie1)

			// tie2's first session is a canceled one, a day after t0: like
			// tie1's, so the ID decides.
			e.eventSession(s, tie2, t0.Add(48*time.Hour))
			e.canceledEventSession(s, tie2, t0.Add(24*time.Hour))
			e.eventSession(s, tie1, t0.Add(24*time.Hour))
			e.eventSession(s, first, t0.Add(72*time.Hour))
			e.eventSession(s, first, t0)
			e.lesson(s, t0.Add(-time.Hour), 5) // a lesson of no event

			got := mustQuery(e, func(ctx context.Context, tx store.Tx) ([]domain.Event, error) {
				return tx.Events().List(ctx)
			})
			wantSlice(e.t, got, []domain.Event{first, tie1, tie2, none1, none2})
		},
		"ListEmpty": func(e *env) {
			got := mustQuery(e, func(ctx context.Context, tx store.Tx) ([]domain.Event, error) {
				return tx.Events().List(ctx)
			})
			wantSlice(e.t, got, nil)
		},
		"ListByIDs": func(e *env) {
			ct := e.courseType("Ballet Basics")
			evs := []domain.Event{e.newEvent(ct, "one"), e.newEvent(ct, "two"), e.newEvent(ct, "three")}
			e.createEvents(evs[2], evs[0], evs[1])

			byID := slices.Clone(evs)
			slices.SortFunc(byID, func(a, b domain.Event) int { return strings.Compare(a.ID.String(), b.ID.String()) })

			list := func(ids ...domain.EventID) []domain.Event {
				e.t.Helper()

				return mustQuery(e, func(ctx context.Context, tx store.Tx) ([]domain.Event, error) {
					return tx.Events().ListByIDs(ctx, ids)
				})
			}

			// Each once, ordered by ID; unknown IDs are left out.
			wantSlice(e.t, list(evs[2].ID, evs[0].ID, evs[1].ID, evs[0].ID, newID[domain.EventID](e)), byID)
			wantSlice(e.t, list(evs[1].ID), []domain.Event{evs[1]})
			wantSlice(e.t, list(newID[domain.EventID](e)), nil)
			wantSlice(e.t, list(), nil)
		},
		"Lock": func(e *env) {
			ev := e.event(e.courseType("Ballet Basics"), "summer-intensive")

			var got domain.Event

			e.mustWrite(func(ctx context.Context, tx store.Tx) error {
				var err error
				got, err = tx.Events().Lock(ctx, ev.ID)

				return err
			})
			wantEqual(e.t, got, ev)

			err := e.read(func(ctx context.Context, tx store.Tx) error {
				_, err := tx.Events().Lock(ctx, ev.ID)

				return err
			})
			wantErr(e.t, err, store.ErrReadOnly)

			err = e.write(func(ctx context.Context, tx store.Tx) error {
				_, err := tx.Events().Lock(ctx, newID[domain.EventID](e))

				return err
			})
			wantErr(e.t, err, domain.ErrNotFound)
		},
		"LockSerialisesTheLastPlaces": func(e *env) {
			// Every capacity check runs under the event lock, so of many
			// concurrent registrations exactly the free places are filled.
			const n = 8

			ev := e.newEvent(e.courseType("Ballet Basics"), "summer-intensive")
			ev.Capacity = 3
			valid(e.t, ev)
			e.createEvents(ev)
			e.createRegistrations(e.newRegistration(ev, e.person("First", "Dancer"), t0.Add(-2*time.Hour)))

			regs := make([]domain.EventRegistration, n)
			for i := range regs {
				regs[i] = e.newRegistration(ev, e.person("Racer", string(rune('A'+i))), t0.Add(-time.Hour))
			}

			errNoPlace := errors.New("no place")
			results := make(chan error, n)

			var wg sync.WaitGroup

			for _, r := range regs {
				wg.Go(func() {
					results <- e.write(func(ctx context.Context, tx store.Tx) error {
						locked, err := tx.Events().Lock(ctx, r.EventID)
						if err != nil {
							return err
						}

						counts, err := tx.EventRegistrations().CountsByEvents(ctx, []domain.EventID{locked.ID}, t0)
						if err != nil {
							return err
						}

						if c := counts[locked.ID]; c.Registered+c.Invited >= locked.Capacity {
							return errNoPlace
						}

						return tx.EventRegistrations().Create(ctx, r)
					})
				})
			}

			wg.Wait()
			close(results)

			ok := 0

			for err := range results {
				switch {
				case err == nil:
					ok++
				case !errors.Is(err, errNoPlace):
					e.t.Fatalf("unexpected error: %v", err)
				}
			}

			wantEqual(e.t, ok, 2)

			counts := mustQuery(e, func(ctx context.Context, tx store.Tx) (map[domain.EventID]store.EventRegistrationCounts, error) {
				return tx.EventRegistrations().CountsByEvents(ctx, []domain.EventID{ev.ID}, t0)
			})
			wantEqual(e.t, counts[ev.ID], store.EventRegistrationCounts{Registered: 3})
		},
		"Image": func(e *env) {
			ct := e.courseType("Ballet Basics")
			ev := e.event(ct, "summer-intensive")
			other := e.event(ct, "winter-intensive")

			image := func(id domain.EventID) (domain.Background, error) {
				return query(e, func(ctx context.Context, tx store.Tx) (domain.Background, error) {
					return tx.Events().Image(ctx, id)
				})
			}
			info := func(id domain.EventID) (domain.BackgroundInfo, error) {
				return query(e, func(ctx context.Context, tx store.Tx) (domain.BackgroundInfo, error) {
					return tx.Events().ImageInfo(ctx, id)
				})
			}
			putImage := func(id domain.EventID, img domain.Background) error {
				return e.write(func(ctx context.Context, tx store.Tx) error { return tx.Events().PutImage(ctx, id, img) })
			}
			jpeg := domain.Background{ContentType: domain.BackgroundJPEG, Data: []byte{0xFF, 0xD8, 0xFF, 'e', 'v'}}
			png := domain.Background{ContentType: domain.BackgroundPNG, Data: []byte("\x89PNG\r\n\x1a\nevent")}
			webp := domain.Background{ContentType: domain.BackgroundWebP, Data: []byte("RIFF\x0c\x00\x00\x00WEBPVP8 ")}
			otherImg := domain.Background{ContentType: domain.BackgroundPNG, Data: []byte("\x89PNG\r\n\x1a\nother")}

			// An unknown event.
			unknown := newID[domain.EventID](e)
			_, err := image(unknown)
			wantErr(e.t, err, domain.ErrNotFound)
			_, err = info(unknown)
			wantErr(e.t, err, domain.ErrNotFound)
			wantErr(e.t, putImage(unknown, jpeg), domain.ErrNotFound)

			// None yet.
			if got, err := image(ev.ID); err != nil || !got.IsZero() {
				e.t.Fatalf("no image: got %+v, %v", got, err)
			}

			if bi, err := info(ev.ID); err != nil || bi != (domain.BackgroundInfo{}) {
				e.t.Fatalf("no image: info %+v, %v", bi, err)
			}

			// Removing none is fine.
			if err := putImage(ev.ID, domain.Background{}); err != nil {
				e.t.Fatalf("remove without an image: %v", err)
			}

			if err := putImage(other.ID, otherImg); err != nil {
				e.t.Fatal(err)
			}

			// Each content type, and a new one replaces the old with its own
			// digest.
			for _, img := range []domain.Background{jpeg, png, webp} {
				valid(e.t, img)

				if err := putImage(ev.ID, img); err != nil {
					e.t.Fatalf("put %s: %v", img.ContentType, err)
				}

				got, err := image(ev.ID)
				if err != nil || got.ContentType != img.ContentType || string(got.Data) != string(img.Data) {
					e.t.Fatalf("put %s: got %+v, %v", img.ContentType, got, err)
				}

				if bi, err := info(ev.ID); err != nil ||
					bi != (domain.BackgroundInfo{ContentType: img.ContentType, Digest: img.Digest()}) {
					e.t.Fatalf("put %s: info %+v, %v", img.ContentType, bi, err)
				}
			}

			// The images of two events stay apart.
			if got, err := image(other.ID); err != nil || string(got.Data) != string(otherImg.Data) {
				e.t.Fatalf("the other event's image: %+v, %v", got, err)
			}

			// An event write keeps the image, and Get does not need it.
			changed := ev
			changed.Title, changed.UpdatedAt = "Summer Intensive, renamed", t0
			e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Events().Update(ctx, changed) })

			if bi, err := info(ev.ID); err != nil || bi.Digest != webp.Digest() {
				e.t.Fatalf("image after an event write: info %+v, %v", bi, err)
			}

			// The largest image Validate accepts is stored.
			largest := domain.Background{ContentType: domain.BackgroundJPEG, Data: make([]byte, domain.MaxBackgroundBytes)}
			copy(largest.Data, jpeg.Data)
			valid(e.t, largest)

			if err := putImage(ev.ID, largest); err != nil {
				e.t.Fatalf("put %d bytes: %v", len(largest.Data), err)
			}

			if got, err := image(ev.ID); err != nil || len(got.Data) != domain.MaxBackgroundBytes {
				e.t.Fatalf("got %d bytes, %v; want %d", len(got.Data), err, domain.MaxBackgroundBytes)
			}

			err = e.read(func(ctx context.Context, tx store.Tx) error {
				return tx.Events().PutImage(ctx, ev.ID, domain.Background{})
			})
			wantErr(e.t, err, store.ErrReadOnly)

			// A rolled back write changes nothing.
			err = e.write(func(ctx context.Context, tx store.Tx) error {
				if err := tx.Events().PutImage(ctx, ev.ID, png); err != nil {
					return err
				}

				return errBoom
			})
			wantErr(e.t, err, errBoom)

			if bi, err := info(ev.ID); err != nil || bi.Digest != largest.Digest() {
				e.t.Fatalf("image after a rollback: info %+v, %v", bi, err)
			}

			if err := putImage(ev.ID, domain.Background{}); err != nil {
				e.t.Fatal(err)
			}

			if got, _ := image(ev.ID); !got.IsZero() {
				e.t.Fatalf("image not removed: %+v", got.ContentType)
			}

			if bi, err := info(ev.ID); err != nil || !bi.IsZero() {
				e.t.Fatalf("removed image: info %+v, %v", bi, err)
			}

			// Removing one event's image keeps the other's.
			if bi, err := info(other.ID); err != nil ||
				bi != (domain.BackgroundInfo{ContentType: otherImg.ContentType, Digest: otherImg.Digest()}) {
				e.t.Fatalf("the other event's image after a removal: %+v, %v", bi, err)
			}
		},
	})
}

func testEventRegistrations(t *testing.T, newStore func(*testing.T) store.Store) {
	t.Helper()

	t.Run("CRUD", func(t *testing.T) {
		testCRUD(t, newStore, crud[domain.EventRegistrationID, domain.EventRegistration]{
			id: func(r domain.EventRegistration) domain.EventRegistrationID { return r.ID },
			get: func(ctx context.Context, tx store.Tx, id domain.EventRegistrationID) (domain.EventRegistration, error) {
				return tx.EventRegistrations().Get(ctx, id)
			},
			create: func(ctx context.Context, tx store.Tx, r domain.EventRegistration) error {
				return tx.EventRegistrations().Create(ctx, r)
			},
			update: func(ctx context.Context, tx store.Tx, r domain.EventRegistration) error {
				return tx.EventRegistrations().Update(ctx, r)
			},
			fixtures: func(e *env) (domain.EventRegistration, domain.EventRegistration) {
				ev := e.event(e.courseType("Ballet Basics"), "summer-intensive")
				p := e.person("Ada", "Dancer")
				minimal := e.newRegistration(ev, p, t0)

				sale := e.receipt(e.newReceipt(2026, 1, p))
				storno := e.receipt(e.cancellationOf(sale, 2))
				full := paidWith(minimal, sale)
				full.Payment, full.CancellationReceiptID = domain.EventRefunded, storno.ID
				full.RegisteredBy = e.account(e.person("Olga", "Office"), "office@example.org").ID
				full = canceledReg(full)
				valid(e.t, full)

				return minimal, full
			},
		})
	})

	cases(t, newStore, map[string]func(e *env){
		"Guest": func(e *env) {
			ev := e.event(e.courseType("Ballet Basics"), "summer-intensive")
			g := e.newGuestRegistration(ev, "Gina Guest", "gina@example.org", t0)
			e.createRegistrations(g)
			wantEqual(e.t, e.getRegistration(g.ID), g)

			// Guests never collide: the same guest twice is fine.
			twin := e.newGuestRegistration(ev, g.GuestName, g.GuestEmail, t0)
			e.createRegistrations(twin)
			wantEqual(e.t, e.getRegistration(twin.ID), twin)
		},
		"Invite": func(e *env) {
			ev := e.event(e.courseType("Ballet Basics"), "summer-intensive")
			plain := e.newRegistration(ev, e.person("Pia", "Plain"), t0)
			r := waiting(e.newRegistration(ev, e.person("Ivy", "Invited"), t0))
			e.createRegistrations(plain, r)

			inv := invited(r, 7, t0.Add(2*time.Hour))
			inv.UpdatedAt = inv.InvitedAt
			e.updateRegistration(inv)
			wantEqual(e.t, e.getRegistration(r.ID), inv)

			byHash := func(hash [32]byte) (domain.EventRegistration, error) {
				return query(e, func(ctx context.Context, tx store.Tx) (domain.EventRegistration, error) {
					return tx.EventRegistrations().GetByInviteTokenHash(ctx, hash)
				})
			}

			got, err := byHash(inv.InviteTokenHash)
			if err != nil {
				e.t.Fatal(err)
			}

			wantEqual(e.t, got, inv)

			_, err = byHash([32]byte{31: 8})
			wantErr(e.t, err, domain.ErrNotFound)

			// The zero hash finds none, though registrations without an
			// invite exist.
			_, err = byHash([32]byte{})
			wantErr(e.t, err, domain.ErrNotFound)

			// Taking the place clears the invite.
			taken := inv
			taken.Status, taken.InviteTokenHash, taken.InvitedAt, taken.ExpiresAt = domain.EventRegistered, [32]byte{},
				time.Time{}, time.Time{}
			e.updateRegistration(taken)
			wantEqual(e.t, e.getRegistration(r.ID), taken)

			_, err = byHash(inv.InviteTokenHash)
			wantErr(e.t, err, domain.ErrNotFound)
		},
		"GetByReceipt": func(e *env) {
			ev := e.event(e.courseType("Ballet Basics"), "summer-intensive")
			p := e.person("Ada", "Dancer")
			sale := e.receipt(e.newReceipt(2026, 1, p))
			storno := e.receipt(e.cancellationOf(sale, 2))
			unrelated := e.receipt(e.newReceipt(2026, 3, p))

			r := paidWith(e.newRegistration(ev, p, t0), sale)
			e.createRegistrations(e.newRegistration(ev, e.person("Fia", "Free"), t0), r)

			byReceipt := func(id domain.ReceiptID) (domain.EventRegistration, error) {
				return query(e, func(ctx context.Context, tx store.Tx) (domain.EventRegistration, error) {
					return tx.EventRegistrations().GetByReceipt(ctx, id)
				})
			}

			got, err := byReceipt(sale.ID)
			if err != nil {
				e.t.Fatal(err)
			}

			wantEqual(e.t, got, r)

			// Refunded: still found by its receipt, not by the Storno.
			r.Payment, r.CancellationReceiptID = domain.EventRefunded, storno.ID
			r = canceledReg(r)
			e.updateRegistration(r)

			if got, err = byReceipt(sale.ID); err != nil || got != r {
				e.t.Fatalf("refunded: %+v, %v", got, err)
			}

			for _, id := range []domain.ReceiptID{storno.ID, unrelated.ID, newID[domain.ReceiptID](e), {}} {
				_, err = byReceipt(id)
				wantErr(e.t, err, domain.ErrNotFound)
			}
		},
		"GetMissingIsNotFound": func(e *env) {
			_, err := query(e, func(ctx context.Context, tx store.Tx) (domain.EventRegistration, error) {
				return tx.EventRegistrations().Get(ctx, newID[domain.EventRegistrationID](e))
			})
			wantErr(e.t, err, domain.ErrNotFound)

			_, err = query(e, func(ctx context.Context, tx store.Tx) (domain.EventRegistration, error) {
				return tx.EventRegistrations().GetByInviteTokenHash(ctx, [32]byte{31: 1})
			})
			wantErr(e.t, err, domain.ErrNotFound)

			_, err = query(e, func(ctx context.Context, tx store.Tx) (domain.EventRegistration, error) {
				return tx.EventRegistrations().GetByReceipt(ctx, newID[domain.ReceiptID](e))
			})
			wantErr(e.t, err, domain.ErrNotFound)
		},
		"OneOpenRegistrationPerPerson": func(e *env) {
			ct := e.courseType("Ballet Basics")
			ev, other := e.event(ct, "summer-intensive"), e.event(ct, "winter-intensive")
			p := e.person("Ada", "Dancer")

			create := func(r domain.EventRegistration) error {
				valid(e.t, r)

				return e.write(func(ctx context.Context, tx store.Tx) error { return tx.EventRegistrations().Create(ctx, r) })
			}
			update := func(r domain.EventRegistration) error {
				valid(e.t, r)

				return e.write(func(ctx context.Context, tx store.Tx) error { return tx.EventRegistrations().Update(ctx, r) })
			}

			first := e.newRegistration(ev, p, t0)
			e.createRegistrations(first)

			// A second open one, in each open status, is a duplicate.
			for _, r := range []domain.EventRegistration{
				e.newRegistration(ev, p, t0.Add(time.Minute)),
				waiting(e.newRegistration(ev, p, t0.Add(time.Minute))),
				invited(waiting(e.newRegistration(ev, p, t0.Add(time.Minute))), 1, t0.Add(time.Hour)),
			} {
				wantErr(e.t, create(r), domain.ErrDuplicate)
			}

			// The same person for another event is fine, and so is a
			// canceled one beside the open one.
			e.createRegistrations(e.newRegistration(other, p, t0))
			beside := canceledReg(e.newRegistration(ev, p, t0.Add(time.Minute)))
			e.createRegistrations(beside)

			// Once the first is canceled, a new one is fine; two canceled
			// ones coexist.
			first = canceledReg(first)
			e.updateRegistration(first)

			second := waiting(e.newRegistration(ev, p, t0.Add(2*time.Minute)))
			e.createRegistrations(second)

			second = canceledReg(second)
			e.updateRegistration(second)

			third := e.newRegistration(ev, p, t0.Add(3*time.Minute))
			e.createRegistrations(third)

			// Reopening a canceled one while another is open is a duplicate.
			reopened := first
			reopened.Status, reopened.CanceledAt = domain.EventRegistered, time.Time{}
			wantErr(e.t, update(reopened), domain.ErrDuplicate)
			wantEqual(e.t, e.getRegistration(first.ID), first)

			// Without the open one, it is fine.
			e.updateRegistration(canceledReg(third))

			if err := update(reopened); err != nil {
				e.t.Fatalf("reopen: %v", err)
			}

			wantEqual(e.t, e.getRegistration(first.ID), reopened)
		},
		"UniqueReferencesAreConflict": func(e *env) {
			ev := e.event(e.courseType("Ballet Basics"), "summer-intensive")
			p := e.person("Ada", "Dancer")
			sale := e.receipt(e.newReceipt(2026, 1, p))
			storno := e.receipt(e.cancellationOf(sale, 2))

			inv := invited(waiting(e.newRegistration(ev, p, t0)), 5, t0.Add(time.Hour))
			paid := e.newRegistration(ev, e.person("Ben", "Paid"), t0)
			paid = paidWith(paid, sale)
			paid.Payment, paid.CancellationReceiptID = domain.EventRefunded, storno.ID
			paid = canceledReg(paid)
			e.createRegistrations(inv, paid)

			otherSale := e.receipt(e.newReceipt(2026, 3, p))

			for i, c := range []struct {
				name   string
				mutate func(r domain.EventRegistration) domain.EventRegistration
			}{
				{"token hash", func(r domain.EventRegistration) domain.EventRegistration {
					return invited(waiting(r), 5, t0.Add(time.Hour))
				}},
				{"receipt", func(r domain.EventRegistration) domain.EventRegistration { return paidWith(r, sale) }},
				{"storno", func(r domain.EventRegistration) domain.EventRegistration {
					r = paidWith(r, otherSale)
					r.Payment, r.CancellationReceiptID = domain.EventRefunded, storno.ID

					return r
				}},
			} {
				r := e.newRegistration(ev, e.person("Cleo", fmt.Sprintf("Clash %d", i)), t0)
				clash := c.mutate(r)
				valid(e.t, clash)

				err := e.write(func(ctx context.Context, tx store.Tx) error { return tx.EventRegistrations().Create(ctx, clash) })
				if !errors.Is(err, domain.ErrConflict) || errors.Is(err, domain.ErrDuplicate) {
					e.t.Errorf("create with a taken %s: want a conflict, got %v", c.name, err)
				}

				e.createRegistrations(r)

				err = e.write(func(ctx context.Context, tx store.Tx) error { return tx.EventRegistrations().Update(ctx, clash) })
				if !errors.Is(err, domain.ErrConflict) || errors.Is(err, domain.ErrDuplicate) {
					e.t.Errorf("update to a taken %s: want a conflict, got %v", c.name, err)
				}

				wantEqual(e.t, e.getRegistration(r.ID), r)
			}

			// A taken ID, for another person: a conflict, not a duplicate.
			again := inv
			again.PersonID = e.person("Dot", "Double").ID
			again.InviteTokenHash = [32]byte{31: 6}

			err := e.write(func(ctx context.Context, tx store.Tx) error { return tx.EventRegistrations().Create(ctx, again) })
			wantErr(e.t, err, domain.ErrConflict)
			wantNotErr(e.t, err, domain.ErrDuplicate)
			wantEqual(e.t, e.getRegistration(inv.ID), inv)
		},
		"MissingReferenceIsConflict": func(e *env) {
			ev := e.event(e.courseType("Ballet Basics"), "summer-intensive")
			p := e.person("Ada", "Dancer")
			sale := e.receipt(e.newReceipt(2026, 1, p))

			for i, c := range []struct {
				name   string
				mutate func(r domain.EventRegistration) domain.EventRegistration
			}{
				{"event", func(r domain.EventRegistration) domain.EventRegistration {
					r.EventID = newID[domain.EventID](e)

					return r
				}},
				{"person", func(r domain.EventRegistration) domain.EventRegistration {
					r.PersonID = newID[domain.PersonID](e)

					return r
				}},
				{"receipt", func(r domain.EventRegistration) domain.EventRegistration {
					return paidWith(r, domain.Receipt{ID: newID[domain.ReceiptID](e)})
				}},
				{"storno", func(r domain.EventRegistration) domain.EventRegistration {
					r = paidWith(r, sale)
					r.Payment, r.CancellationReceiptID = domain.EventRefunded, newID[domain.ReceiptID](e)

					return r
				}},
				{"registering account", func(r domain.EventRegistration) domain.EventRegistration {
					r.RegisteredBy = newID[domain.AccountID](e)

					return r
				}},
			} {
				r := e.newRegistration(ev, e.person("Mia", fmt.Sprintf("Missing %d", i)), t0)
				broken := c.mutate(r)
				valid(e.t, broken)

				err := e.write(func(ctx context.Context, tx store.Tx) error {
					return tx.EventRegistrations().Create(ctx, broken)
				})
				if !errors.Is(err, domain.ErrConflict) || errors.Is(err, domain.ErrDuplicate) {
					e.t.Errorf("create with a missing %s: want a conflict, got %v", c.name, err)
				}

				e.createRegistrations(r)

				err = e.write(func(ctx context.Context, tx store.Tx) error {
					return tx.EventRegistrations().Update(ctx, broken)
				})
				if !errors.Is(err, domain.ErrConflict) || errors.Is(err, domain.ErrDuplicate) {
					e.t.Errorf("update with a missing %s: want a conflict, got %v", c.name, err)
				}

				wantEqual(e.t, e.getRegistration(r.ID), r)
			}
		},
		"ListByEvent": func(e *env) {
			ct := e.courseType("Ballet Basics")
			ev, other := e.event(ct, "summer-intensive"), e.event(ct, "winter-intensive")

			// IDs in the order generated; created in another order. tie1
			// and tie2 were made at the same instant: the ID decides.
			tie1 := canceledReg(e.newRegistration(ev, e.person("Tia", "One"), t0))
			late := e.newRegistration(ev, e.person("Lea", "Late"), t0.Add(2*time.Minute))
			tie2 := waiting(e.newRegistration(ev, e.person("Tia", "Two"), t0))
			guest := e.newGuestRegistration(ev, "Gina Guest", "gina@example.org", t0.Add(time.Minute))
			early := e.newRegistration(ev, e.person("Eli", "Early"), t0.Add(-time.Minute))
			elsewhere := e.newRegistration(other, e.person("Oda", "Other"), t0)
			e.createRegistrations(late, tie2, elsewhere, guest, tie1, early)

			list := func(id domain.EventID) []domain.EventRegistration {
				e.t.Helper()

				return mustQuery(e, func(ctx context.Context, tx store.Tx) ([]domain.EventRegistration, error) {
					return tx.EventRegistrations().ListByEvent(ctx, id)
				})
			}

			wantSlice(e.t, list(ev.ID), []domain.EventRegistration{early, tie1, tie2, guest, late})
			wantSlice(e.t, list(other.ID), []domain.EventRegistration{elsewhere})
			wantSlice(e.t, list(newID[domain.EventID](e)), nil)
		},
		"ListByPersons": func(e *env) {
			ct := e.courseType("Ballet Basics")
			ev, other := e.event(ct, "summer-intensive"), e.event(ct, "winter-intensive")
			ada, ben, cid := e.person("Ada", "Dancer"), e.person("Ben", "Dancer"), e.person("Cid", "Dancer")

			benTie := e.newRegistration(other, ben, t0)
			adaLate := e.newRegistration(ev, ada, t0.Add(2*time.Minute))
			adaTie := canceledReg(e.newRegistration(other, ada, t0))
			benEarly := waiting(e.newRegistration(ev, ben, t0.Add(-time.Minute)))
			cids := e.newRegistration(ev, cid, t0)
			guest := e.newGuestRegistration(ev, "Gina Guest", "gina@example.org", t0)
			e.createRegistrations(adaTie, guest, adaLate, cids, benEarly, benTie)

			list := func(persons ...domain.PersonID) []domain.EventRegistration {
				e.t.Helper()

				return mustQuery(e, func(ctx context.Context, tx store.Tx) ([]domain.EventRegistration, error) {
					return tx.EventRegistrations().ListByPersons(ctx, persons)
				})
			}

			wantSlice(e.t, list(ada.ID, ben.ID), []domain.EventRegistration{benEarly, benTie, adaTie, adaLate})
			wantSlice(e.t, list(ada.ID), []domain.EventRegistration{adaTie, adaLate})
			wantSlice(e.t, list(cid.ID), []domain.EventRegistration{cids})
			wantSlice(e.t, list(newID[domain.PersonID](e)), nil)
			wantSlice(e.t, list(), nil)
		},
		"ListByReceipts": func(e *env) {
			ct := e.courseType("Ballet Basics")
			ev, other := e.event(ct, "summer-intensive"), e.event(ct, "winter-intensive")
			ada, ben := e.person("Ada", "Dancer"), e.person("Ben", "Dancer")
			first := e.receipt(e.newReceipt(2026, 1, ada))
			second := e.receipt(e.newReceipt(2026, 2, ben))
			storno := e.receipt(e.cancellationOf(first, 3))
			unrelated := e.receipt(e.newReceipt(2026, 4, ada))

			late := paidWith(e.newRegistration(ev, ben, t0.Add(time.Minute)), second)
			early := paidWith(e.newRegistration(other, ada, t0), first)
			early.Payment, early.CancellationReceiptID = domain.EventRefunded, storno.ID
			early = canceledReg(early)
			free := e.newRegistration(ev, ada, t0)
			e.createRegistrations(late, free, early)

			list := func(receipts ...domain.ReceiptID) []domain.EventRegistration {
				e.t.Helper()

				return mustQuery(e, func(ctx context.Context, tx store.Tx) ([]domain.EventRegistration, error) {
					return tx.EventRegistrations().ListByReceipts(ctx, receipts)
				})
			}

			// By the receipt of the payment, not by the Storno; the zero ID
			// finds no free registration.
			wantSlice(e.t, list(second.ID, first.ID, unrelated.ID), []domain.EventRegistration{early, late})
			wantSlice(e.t, list(storno.ID, domain.ReceiptID{}), nil)
			wantSlice(e.t, list(), nil)
		},
		"CountsByEvents": func(e *env) {
			ct := e.courseType("Ballet Basics")
			full := e.event(ct, "full")
			onlyCanceled := e.event(ct, "only-canceled")
			onlyExpired := e.event(ct, "only-expired")
			notAsked := e.event(ct, "not-asked")
			empty := e.event(ct, "empty")

			at := t0.Add(time.Hour)
			reg := func(ev domain.Event, name string) domain.EventRegistration {
				return e.newRegistration(ev, e.person(name, "Counted"), t0)
			}

			e.createRegistrations(
				reg(full, "Ada"),
				e.newGuestRegistration(full, "Gina Guest", "gina@example.org", t0),
				invited(waiting(reg(full, "Ivy")), 1, at.Add(time.Microsecond)), // still open
				invited(waiting(reg(full, "Eve")), 2, at),                       // expires at the instant
				waiting(reg(full, "Wim")),
				waiting(e.newGuestRegistration(full, "Walt Guest", "walt@example.org", t0)),
				canceledReg(reg(full, "Cal")),
				canceledReg(reg(onlyCanceled, "Cora")),
				invited(waiting(reg(onlyExpired, "Ezra")), 3, at.Add(-time.Minute)),
				reg(notAsked, "Nina"),
			)

			counts := func(at time.Time, events ...domain.EventID) map[domain.EventID]store.EventRegistrationCounts {
				e.t.Helper()

				return mustQuery(e, func(ctx context.Context, tx store.Tx) (map[domain.EventID]store.EventRegistrationCounts, error) {
					return tx.EventRegistrations().CountsByEvents(ctx, events, at)
				})
			}

			for _, instant := range []time.Time{at, at.Add(500 * time.Nanosecond)} {
				got := counts(instant, full.ID, onlyCanceled.ID, onlyExpired.ID, empty.ID, newID[domain.EventID](e))
				if len(got) != 1 || got[full.ID] != (store.EventRegistrationCounts{Registered: 2, Invited: 1, Waiting: 2}) {
					e.t.Fatalf("counts at %v: %+v", instant, got)
				}
			}

			// A microsecond earlier, the invite expiring at the instant still
			// holds a place.
			got := counts(at.Add(-time.Microsecond), full.ID)
			if len(got) != 1 || got[full.ID] != (store.EventRegistrationCounts{Registered: 2, Invited: 2, Waiting: 2}) {
				e.t.Fatalf("counts a microsecond before: %+v", got)
			}

			got = counts(at.Add(-time.Hour), onlyExpired.ID, notAsked.ID)
			if len(got) != 2 || got[onlyExpired.ID] != (store.EventRegistrationCounts{Invited: 1}) ||
				got[notAsked.ID] != (store.EventRegistrationCounts{Registered: 1}) {
				e.t.Fatalf("counts an hour before: %+v", got)
			}

			if got := counts(at); len(got) != 0 {
				e.t.Fatalf("counts of no events: %+v", got)
			}
		},
		"AnonymizeGuests": func(e *env) {
			s := e.setting()
			cutoff := t0.Add(7 * 24 * time.Hour)
			ended := e.event(s.courseType, "ended")
			edge := e.event(s.courseType, "edge")
			running := e.event(s.courseType, "running")
			noSessions := e.event(s.courseType, "no-sessions")

			// Sessions last an hour. ended's last (a canceled one) ended a
			// microsecond before the cutoff; edge's last (canceled too)
			// exactly at it; running's after it.
			e.eventSession(s, ended, cutoff.Add(-48*time.Hour))
			e.canceledEventSession(s, ended, cutoff.Add(-time.Hour-time.Microsecond))
			e.eventSession(s, edge, cutoff.Add(-48*time.Hour))
			e.canceledEventSession(s, edge, cutoff.Add(-time.Hour))
			e.eventSession(s, running, cutoff.Add(-48*time.Hour))
			e.eventSession(s, running, cutoff.Add(time.Hour))

			office := e.account(e.person("Olga", "Office"), "office@example.org")
			guest := func(ev domain.Event, name, email string) domain.EventRegistration {
				r := e.newGuestRegistration(ev, name, email, t0)
				r.RegisteredBy = office.ID

				return r
			}

			p := e.person("Mia", "Member")
			sale := e.receipt(e.newReceipt(2026, 1, p))
			old := paidWith(guest(ended, "Gina Guest", "gina@example.org"), sale)
			oldCanceled := canceledReg(guest(ended, "Gus Guest", "gus@example.org"))
			oldWaiting := waiting(guest(ended, "Walt Guest", "walt@example.org"))
			member := e.newRegistration(ended, p, t0)
			atEdge := guest(edge, "Edda Edge", "edda@example.org")
			ongoing := guest(running, "Rita Running", "rita@example.org")
			noDates := guest(noSessions, "Nora None", "nora@example.org")
			e.createRegistrations(old, oldCanceled, oldWaiting, member, atEdge, ongoing, noDates)

			anonymize := func() int {
				e.t.Helper()

				var n int

				e.mustWrite(func(ctx context.Context, tx store.Tx) error {
					var err error
					n, err = tx.EventRegistrations().AnonymizeGuests(ctx, cutoff)

					return err
				})

				return n
			}

			wantEqual(e.t, anonymize(), 3)
			wantEqual(e.t, anonymize(), 0) // anonymized already

			for _, r := range []*domain.EventRegistration{&old, &oldCanceled, &oldWaiting} {
				r.GuestName, r.GuestEmail = domain.AnonymousGuestName, ""
				valid(e.t, *r)
			}

			for _, want := range []domain.EventRegistration{
				old, oldCanceled, oldWaiting, member, atEdge, ongoing, noDates,
			} {
				wantEqual(e.t, e.getRegistration(want.ID), want)
			}

			_, err := query(e, func(ctx context.Context, tx store.Tx) (int, error) {
				return tx.EventRegistrations().AnonymizeGuests(ctx, cutoff)
			})
			wantErr(e.t, err, store.ErrReadOnly)

			// A rolled back run changes nothing.
			err = e.write(func(ctx context.Context, tx store.Tx) error {
				if _, err := tx.EventRegistrations().AnonymizeGuests(ctx, cutoff.Add(48*time.Hour)); err != nil {
					return err
				}

				return errBoom
			})
			wantErr(e.t, err, errBoom)
			wantEqual(e.t, e.getRegistration(atEdge.ID), atEdge)
		},
	})
}
