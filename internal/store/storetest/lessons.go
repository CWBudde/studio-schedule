package storetest

import (
	"context"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/store"
)

func testLessons(t *testing.T, newStore func(*testing.T) store.Store) {
	t.Helper()

	t.Run("CRUD", func(t *testing.T) {
		testCRUD(t, newStore, crud[domain.LessonID, domain.Lesson]{
			id: func(l domain.Lesson) domain.LessonID { return l.ID },
			get: func(ctx context.Context, tx store.Tx, id domain.LessonID) (domain.Lesson, error) {
				return tx.Lessons().Get(ctx, id)
			},
			create: func(ctx context.Context, tx store.Tx, l domain.Lesson) error { return tx.Lessons().Create(ctx, l) },
			update: func(ctx context.Context, tx store.Tx, l domain.Lesson) error { return tx.Lessons().Update(ctx, l) },
			fixtures: func(e *env) (domain.Lesson, domain.Lesson) {
				s := e.setting()
				minimal := e.newLesson(s, t0, 8)
				full := minimal
				full.ClassID = e.class(s, time.Monday, domain.TimeOfDay{Hour: 10}).ID
				full.AccompanistID = e.staffMember("Accompanist").ID
				full.StartsAt = t0.Add(24 * time.Hour)
				full.EndsAt = full.StartsAt.Add(90 * time.Minute)
				full.Status = domain.LessonCanceled
				full.Visibility = domain.VisibilityMembers
				full.Kind = domain.LessonWorkshop
				full.Capacity = 30
				valid(e.t, full)

				return minimal, full
			},
		})
	})

	cases(t, newStore, map[string]func(e *env){
		"ListRangeIsHalfOpenAndOrdered": func(e *env) {
			s := e.setting()
			before := e.lesson(s, t0.Add(-time.Microsecond), 5)
			atStart2 := e.lesson(s, t0, 5)
			atStart1 := e.lesson(s, t0, 5)
			middle := e.lesson(s, t0.Add(-time.Hour).Add(26*time.Hour), 5)
			early := e.lesson(s, t0.Add(30*time.Minute), 5)
			e.lesson(s, t0.Add(48*time.Hour), 5) // the end is exclusive

			got := mustQuery(e, func(ctx context.Context, tx store.Tx) ([]domain.Lesson, error) {
				return tx.Lessons().ListRange(ctx, t0, t0.Add(48*time.Hour))
			})
			wantSlice(e.t, got, []domain.Lesson{atStart2, atStart1, early, middle})

			got = mustQuery(e, func(ctx context.Context, tx store.Tx) ([]domain.Lesson, error) {
				return tx.Lessons().ListRange(ctx, t0.Add(-time.Hour), t0)
			})
			wantSlice(e.t, got, []domain.Lesson{before})

			got = mustQuery(e, func(ctx context.Context, tx store.Tx) ([]domain.Lesson, error) {
				return tx.Lessons().ListRange(ctx, t0.Add(72*time.Hour), t0.Add(96*time.Hour))
			})
			wantSlice(e.t, got, nil)
		},
		"ClassStartIsUnique": func(e *env) {
			s := e.setting()
			c := e.class(s, time.Monday, domain.TimeOfDay{Hour: 9})

			first := e.newLesson(s, t0, 5)
			first.ClassID = c.ID
			e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Lessons().Create(ctx, first) })

			dup := e.newLesson(s, t0, 5)
			dup.ClassID = c.ID
			err := e.write(func(ctx context.Context, tx store.Tx) error { return tx.Lessons().Create(ctx, dup) })
			wantErr(e.t, err, domain.ErrConflict)

			// One-off lessons at the same time are fine.
			e.lesson(s, t0, 5)
			e.lesson(s, t0, 5)

			// Moving another lesson of the class onto the taken start fails.
			other := e.newLesson(s, t0.Add(time.Hour), 5)
			other.ClassID = c.ID
			e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Lessons().Create(ctx, other) })

			other.StartsAt, other.EndsAt = t0, t0.Add(time.Hour)
			err = e.write(func(ctx context.Context, tx store.Tx) error { return tx.Lessons().Update(ctx, other) })
			wantErr(e.t, err, domain.ErrConflict)
		},
		"MissingReferenceIsConflict": func(e *env) {
			s := e.setting()
			for name, mutate := range map[string]func(*domain.Lesson){
				"class":       func(l *domain.Lesson) { l.ClassID = newID[domain.ClassID](e) },
				"course type": func(l *domain.Lesson) { l.CourseTypeID = newID[domain.CourseTypeID](e) },
				"room":        func(l *domain.Lesson) { l.RoomID = newID[domain.RoomID](e) },
				"instructor":  func(l *domain.Lesson) { l.InstructorID = newID[domain.StaffID](e) },
				"accompanist": func(l *domain.Lesson) { l.AccompanistID = newID[domain.StaffID](e) },
			} {
				l := e.newLesson(s, t0, 5)
				mutate(&l)

				err := e.write(func(ctx context.Context, tx store.Tx) error { return tx.Lessons().Create(ctx, l) })
				if !errorsIs(err, domain.ErrConflict) {
					e.t.Errorf("lesson with missing %s: want ErrConflict, got %v", name, err)
				}
			}
		},
	})
}

func testBookings(t *testing.T, newStore func(*testing.T) store.Store) {
	t.Helper()

	t.Run("CRUD", func(t *testing.T) {
		testCRUD(t, newStore, crud[domain.BookingID, domain.Booking]{
			id: func(b domain.Booking) domain.BookingID { return b.ID },
			get: func(ctx context.Context, tx store.Tx, id domain.BookingID) (domain.Booking, error) {
				return tx.Bookings().Get(ctx, id)
			},
			create: func(ctx context.Context, tx store.Tx, b domain.Booking) error { return tx.Bookings().Create(ctx, b) },
			update: func(ctx context.Context, tx store.Tx, b domain.Booking) error { return tx.Bookings().Update(ctx, b) },
			fixtures: func(e *env) (domain.Booking, domain.Booking) {
				minimal := e.newBooking(e.lesson(e.setting(), t0, 5), e.person("Bo", "Booker"), t0.Add(-time.Hour))
				full := minimal
				full.Status = domain.BookingCanceled
				full.Trial = true
				full.Prepaid = true
				full.CanceledAt = t0.Add(-30 * time.Minute)
				valid(e.t, full)

				return minimal, full
			},
		})
	})

	t.Run("GuestCRUD", func(t *testing.T) {
		testCRUD(t, newStore, crud[domain.BookingID, domain.Booking]{
			id: func(b domain.Booking) domain.BookingID { return b.ID },
			get: func(ctx context.Context, tx store.Tx, id domain.BookingID) (domain.Booking, error) {
				return tx.Bookings().Get(ctx, id)
			},
			create: func(ctx context.Context, tx store.Tx, b domain.Booking) error { return tx.Bookings().Create(ctx, b) },
			update: func(ctx context.Context, tx store.Tx, b domain.Booking) error { return tx.Bookings().Update(ctx, b) },
			fixtures: func(e *env) (domain.Booking, domain.Booking) {
				guest := domain.Booking{
					ID: newID[domain.BookingID](e), LessonID: e.lesson(e.setting(), t0, 5).ID,
					GuestName: "Gina Guest", GuestEmail: "gina@example.org", Status: domain.BookingBooked, BookedAt: t0,
				}
				attended := guest
				attended.Status = domain.BookingAttended
				attended.Trial = true
				valid(e.t, guest)
				valid(e.t, attended)

				return guest, attended
			},
		})
	})

	cases(t, newStore, map[string]func(e *env){
		"OneActiveBookingPerPerson": func(e *env) {
			l := e.lesson(e.setting(), t0, 5)
			p := e.person("Bo", "Booker")
			first := e.booking(l, p, t0.Add(-3*time.Hour))

			dup := e.newBooking(l, p, t0.Add(-2*time.Hour))
			err := e.write(func(ctx context.Context, tx store.Tx) error { return tx.Bookings().Create(ctx, dup) })
			wantErr(e.t, err, domain.ErrConflict)

			// After canceling, the person may book again.
			first.Status, first.CanceledAt = domain.BookingCanceled, t0.Add(-150*time.Minute)
			e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Bookings().Update(ctx, first) })
			e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Bookings().Create(ctx, dup) })

			// Reactivating the canceled booking now conflicts.
			first.Status, first.CanceledAt = domain.BookingBooked, time.Time{}
			err = e.write(func(ctx context.Context, tx store.Tx) error { return tx.Bookings().Update(ctx, first) })
			wantErr(e.t, err, domain.ErrConflict)

			// Guests are not limited.
			for range 2 {
				g := domain.Booking{
					ID: newID[domain.BookingID](e), LessonID: l.ID, GuestName: "Gina Guest",
					GuestEmail: "gina@example.org", Status: domain.BookingBooked, BookedAt: t0,
				}
				e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Bookings().Create(ctx, g) })
			}

			n := mustQuery(e, func(ctx context.Context, tx store.Tx) (int, error) { return tx.Bookings().CountActive(ctx, l.ID) })
			wantEqual(e.t, n, 3)
		},
		"ListByLessonAndCount": func(e *env) {
			s := e.setting()
			l, other := e.lesson(s, t0, 5), e.lesson(s, t0.Add(time.Hour), 5)
			late := e.booking(l, e.person("Late", "Booker"), t0.Add(-time.Hour))
			early2 := e.booking(l, e.person("Early", "Booker"), t0.Add(-2*time.Hour))
			early1 := e.booking(l, e.person("Earlier", "Booker"), t0.Add(-2*time.Hour))
			e.booking(other, e.person("Other", "Booker"), t0.Add(-2*time.Hour))

			canceled := early1
			canceled.Status, canceled.CanceledAt = domain.BookingCanceled, t0.Add(-90*time.Minute)
			noShow := late
			noShow.Status = domain.BookingNoShow
			e.mustWrite(func(ctx context.Context, tx store.Tx) error {
				if err := tx.Bookings().Update(ctx, canceled); err != nil {
					return err
				}

				return tx.Bookings().Update(ctx, noShow)
			})

			got := mustQuery(e, func(ctx context.Context, tx store.Tx) ([]domain.Booking, error) {
				return tx.Bookings().ListByLesson(ctx, l.ID)
			})
			wantSlice(e.t, got, []domain.Booking{early2, canceled, noShow})

			n := mustQuery(e, func(ctx context.Context, tx store.Tx) (int, error) { return tx.Bookings().CountActive(ctx, l.ID) })
			wantEqual(e.t, n, 2)

			n = mustQuery(e, func(ctx context.Context, tx store.Tx) (int, error) {
				return tx.Bookings().CountActive(ctx, newID[domain.LessonID](e))
			})
			wantEqual(e.t, n, 0)
		},
		"MissingReferenceIsConflict": func(e *env) {
			l := e.lesson(e.setting(), t0, 5)
			p := e.person("Bo", "Booker")

			noLesson := e.newBooking(l, p, t0)
			noLesson.LessonID = newID[domain.LessonID](e)
			err := e.write(func(ctx context.Context, tx store.Tx) error { return tx.Bookings().Create(ctx, noLesson) })
			wantErr(e.t, err, domain.ErrConflict)

			noPerson := e.newBooking(l, p, t0)
			noPerson.PersonID = newID[domain.PersonID](e)
			err = e.write(func(ctx context.Context, tx store.Tx) error { return tx.Bookings().Create(ctx, noPerson) })
			wantErr(e.t, err, domain.ErrConflict)
		},
		"Events": func(e *env) {
			b := e.booking(e.lesson(e.setting(), t0, 5), e.person("Bo", "Booker"), t0.Add(-time.Hour))
			actor := e.account(e.person("Olga", "Office"), "olga@example.org")

			later := domain.BookingEvent{
				ID: newID[domain.BookingEventID](e), BookingID: b.ID, At: t0.Add(-30 * time.Minute),
				Status: domain.BookingCanceled, ActorAccountID: actor.ID, Note: "Called in sick.",
			}
			first2 := domain.BookingEvent{ID: newID[domain.BookingEventID](e), BookingID: b.ID, At: t0.Add(-time.Hour), Status: domain.BookingBooked}
			first1 := domain.BookingEvent{ID: newID[domain.BookingEventID](e), BookingID: b.ID, At: t0.Add(-time.Hour), Status: domain.BookingBooked}

			for _, ev := range []domain.BookingEvent{later, first2, first1} {
				valid(e.t, ev)
				e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Bookings().AddEvent(ctx, ev) })
			}

			got := mustQuery(e, func(ctx context.Context, tx store.Tx) ([]domain.BookingEvent, error) {
				return tx.Bookings().ListEvents(ctx, b.ID)
			})
			wantSlice(e.t, got, []domain.BookingEvent{first2, first1, later})

			err := e.write(func(ctx context.Context, tx store.Tx) error { return tx.Bookings().AddEvent(ctx, later) })
			wantErr(e.t, err, domain.ErrConflict)

			orphan := first1
			orphan.ID, orphan.BookingID = newID[domain.BookingEventID](e), newID[domain.BookingID](e)
			err = e.write(func(ctx context.Context, tx store.Tx) error { return tx.Bookings().AddEvent(ctx, orphan) })
			wantErr(e.t, err, domain.ErrConflict)

			noActor := first1
			noActor.ID, noActor.ActorAccountID = newID[domain.BookingEventID](e), newID[domain.AccountID](e)
			err = e.write(func(ctx context.Context, tx store.Tx) error { return tx.Bookings().AddEvent(ctx, noActor) })
			wantErr(e.t, err, domain.ErrConflict)

			err = e.read(func(ctx context.Context, tx store.Tx) error {
				return tx.Bookings().AddEvent(ctx, domain.BookingEvent{
					ID: newID[domain.BookingEventID](e), BookingID: b.ID, At: t0, Status: domain.BookingAttended,
				})
			})
			wantErr(e.t, err, store.ErrReadOnly)
		},
	})
}
