package storetest

import (
	"context"
	"errors"
	"maps"
	"slices"
	"sync"
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
				full.SlotDate = slot0
				full.AccompanistID = e.staffMember("Accompanist").ID
				full.StartsAt = t0.Add(24 * time.Hour)
				full.EndsAt = full.StartsAt.Add(90 * time.Minute)
				full.Status = domain.LessonCanceled
				full.Visibility = domain.VisibilityMembers
				full.Kind = domain.LessonWorkshop
				full.Capacity = 30
				full.Detached = true
				full.CancelNote = "Studio closed"
				full.CanceledAt = t0.Add(-time.Hour)
				full.CanceledBy = e.account(e.person("Olga", "Office"), "office@example.org").ID
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

			// Bounds are truncated to microseconds like stored instants.
			got = mustQuery(e, func(ctx context.Context, tx store.Tx) ([]domain.Lesson, error) {
				return tx.Lessons().ListRange(ctx, t0.Add(500*time.Nanosecond), t0.Add(30*time.Minute+500*time.Nanosecond))
			})
			wantSlice(e.t, got, []domain.Lesson{atStart2, atStart1})
		},
		"ListByStaff": func(e *env) {
			s := e.setting()
			other, third := e.staffMember("Instructor Two"), e.staffMember("Pianist")

			own := e.lesson(s, t0.Add(time.Hour), 5)
			// Accompanying another instructor's lesson.
			accompanied := e.newLesson(s, t0, 5)
			accompanied.InstructorID, accompanied.AccompanistID = other.ID, s.instructor.ID
			// Both of the asked staff members on one lesson: listed once.
			both := e.newLesson(s, t0, 5)
			both.InstructorID, both.AccompanistID = other.ID, third.ID
			// Out of the range, and someone else's.
			before := e.newLesson(s, t0.Add(-time.Microsecond), 5)
			atEnd := e.newLesson(s, t0.Add(48*time.Hour), 5)
			foreign := e.newLesson(s, t0.Add(time.Hour), 5)
			foreign.InstructorID = third.ID

			e.mustWrite(func(ctx context.Context, tx store.Tx) error {
				for _, l := range []domain.Lesson{accompanied, both, before, atEnd, foreign} {
					if err := tx.Lessons().Create(ctx, l); err != nil {
						return err
					}
				}

				return nil
			})

			list := func(staff ...domain.StaffID) []domain.Lesson {
				e.t.Helper()

				return mustQuery(e, func(ctx context.Context, tx store.Tx) ([]domain.Lesson, error) {
					return tx.Lessons().ListByStaff(ctx, staff, t0, t0.Add(48*time.Hour))
				})
			}

			wantSlice(e.t, list(s.instructor.ID), []domain.Lesson{accompanied, own})
			// Same start: ordered by ID, which the sequence hands out in order.
			wantSlice(e.t, list(other.ID, third.ID), []domain.Lesson{accompanied, both, foreign})
			wantSlice(e.t, list(third.ID, s.instructor.ID), []domain.Lesson{accompanied, both, own, foreign})

			wantSlice(e.t, list(newID[domain.StaffID](e)), nil)
			wantSlice(e.t, list(), nil)
		},
		"ListByIDs": func(e *env) {
			s := e.setting()
			late := e.lesson(s, t0.Add(time.Hour), 5)
			tie2 := e.lesson(s, t0, 5)
			tie1 := e.lesson(s, t0, 5)         // a later ID: after tie2
			e.lesson(s, t0.Add(-time.Hour), 5) // not asked for

			list := func(ids ...domain.LessonID) []domain.Lesson {
				e.t.Helper()

				return mustQuery(e, func(ctx context.Context, tx store.Tx) ([]domain.Lesson, error) {
					return tx.Lessons().ListByIDs(ctx, ids)
				})
			}

			// Each once, by start and ID; unknown IDs are left out.
			wantSlice(e.t, list(late.ID, tie1.ID, newID[domain.LessonID](e), tie2.ID, late.ID),
				[]domain.Lesson{tie2, tie1, late})
			wantSlice(e.t, list(), nil)
		},
		"ClassSlotIsUnique": func(e *env) {
			s := e.setting()
			c := e.class(s, time.Monday, domain.TimeOfDay{Hour: 9})
			e.classLesson(s, c, slot0, t0)

			dup := e.newLesson(s, t0.Add(time.Hour), 5)
			dup.ClassID, dup.SlotDate = c.ID, slot0
			err := e.write(func(ctx context.Context, tx store.Tx) error { return tx.Lessons().Create(ctx, dup) })
			wantErr(e.t, err, domain.ErrConflict)

			// One-off lessons at the same time are fine.
			e.lesson(s, t0, 5)
			e.lesson(s, t0, 5)

			// A lesson moved onto the start of another slot is fine too: the
			// slot, not the start, identifies it.
			moved := e.classLesson(s, c, slot0.AddDays(7), t0)

			// Moving it into the taken slot fails.
			moved.SlotDate = slot0
			err = e.write(func(ctx context.Context, tx store.Tx) error { return tx.Lessons().Update(ctx, moved) })
			wantErr(e.t, err, domain.ErrConflict)
		},
		"Delete": func(e *env) {
			s := e.setting()
			free, booked := e.lesson(s, t0, 5), e.lesson(s, t0.Add(time.Hour), 5)
			b := e.booking(booked, e.person("Bo", "Booker"), t0.Add(-time.Hour))

			err := e.read(func(ctx context.Context, tx store.Tx) error { return tx.Lessons().Delete(ctx, free.ID) })
			wantErr(e.t, err, store.ErrReadOnly)

			e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Lessons().Delete(ctx, free.ID) })
			err = e.read(func(ctx context.Context, tx store.Tx) error {
				_, err := tx.Lessons().Get(ctx, free.ID)

				return err
			})
			wantErr(e.t, err, domain.ErrNotFound)

			err = e.write(func(ctx context.Context, tx store.Tx) error { return tx.Lessons().Delete(ctx, free.ID) })
			wantErr(e.t, err, domain.ErrNotFound)

			// A booking blocks the delete, a canceled one too.
			b.Status, b.CanceledAt = domain.BookingCanceled, t0.Add(-30*time.Minute)
			e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Bookings().Update(ctx, b) })

			err = e.write(func(ctx context.Context, tx store.Tx) error { return tx.Lessons().Delete(ctx, booked.ID) })
			wantErr(e.t, err, domain.ErrConflict)
		},
		"ListByClassStartingAfter": func(e *env) {
			s := e.setting()
			c := e.class(s, time.Monday, domain.TimeOfDay{Hour: 9})
			other := e.class(s, time.Tuesday, domain.TimeOfDay{Hour: 9})

			e.classLesson(s, c, slot0.AddDays(-7), t0.Add(-7*24*time.Hour))
			e.classLesson(s, c, slot0, t0) // starts exactly at the bound: not after it
			third := e.classLesson(s, c, slot0.AddDays(14), t0.Add(14*24*time.Hour))
			// A lesson of an earlier slot that the office moved into the future.
			moved := e.classLesson(s, c, slot0.AddDays(-14), t0.Add(time.Hour))
			e.classLesson(s, other, slot0.AddDays(1), t0.Add(24*time.Hour))
			e.lesson(s, t0.Add(2*time.Hour), 5)

			got := mustQuery(e, func(ctx context.Context, tx store.Tx) ([]domain.Lesson, error) {
				return tx.Lessons().ListByClassStartingAfter(ctx, c.ID, t0)
			})
			wantSlice(e.t, got, []domain.Lesson{moved, third})

			got = mustQuery(e, func(ctx context.Context, tx store.Tx) ([]domain.Lesson, error) {
				return tx.Lessons().ListByClassStartingAfter(ctx, c.ID, t0.Add(30*24*time.Hour))
			})
			wantSlice(e.t, got, nil)
		},
		"ListByClass": func(e *env) {
			s := e.setting()
			c := e.class(s, time.Monday, domain.TimeOfDay{Hour: 9})
			other := e.class(s, time.Tuesday, domain.TimeOfDay{Hour: 9})

			past := e.classLesson(s, c, slot0.AddDays(-7), t0.Add(-7*24*time.Hour))
			first := e.classLesson(s, c, slot0, t0)
			third := e.classLesson(s, c, slot0.AddDays(14), t0.Add(14*24*time.Hour))
			// Moved before the lesson of the slot before it.
			second := e.classLesson(s, c, slot0.AddDays(21), t0.Add(time.Hour))
			e.classLesson(s, other, slot0.AddDays(1), t0.Add(24*time.Hour))
			e.lesson(s, t0, 5)

			got := mustQuery(e, func(ctx context.Context, tx store.Tx) ([]domain.Lesson, error) {
				return tx.Lessons().ListByClass(ctx, c.ID, slot0)
			})
			wantSlice(e.t, got, []domain.Lesson{first, second, third})

			got = mustQuery(e, func(ctx context.Context, tx store.Tx) ([]domain.Lesson, error) {
				return tx.Lessons().ListByClass(ctx, c.ID, slot0.AddDays(22))
			})
			wantSlice(e.t, got, nil)

			// A zero date lists them all.
			got = mustQuery(e, func(ctx context.Context, tx store.Tx) ([]domain.Lesson, error) {
				return tx.Lessons().ListByClass(ctx, c.ID, domain.Date{})
			})
			wantSlice(e.t, got, []domain.Lesson{past, first, second, third})
		},
		"ListRoomOverlaps": func(e *env) {
			s := e.setting()
			other := e.room(e.location("South Studio"), "Room B")
			at := func(start time.Time, d time.Duration, mutate func(*domain.Lesson)) domain.Lesson {
				l := e.newLesson(s, start, 5)
				l.EndsAt = start.Add(d)
				if mutate != nil {
					mutate(&l)
				}
				valid(e.t, l)
				e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Lessons().Create(ctx, l) })

				return l
			}

			// Query [t0+1h, t0+2h) in s.room.
			from, to := t0.Add(time.Hour), t0.Add(2*time.Hour)
			at(t0, time.Hour, nil)                                 // ends at from: no overlap
			at(to, time.Hour, nil)                                 // starts at to: no overlap
			early := at(t0.Add(30*time.Minute), time.Hour, nil)    // overlaps the start
			late := at(t0.Add(90*time.Minute), time.Hour, nil)     // overlaps the end
			around := at(t0.Add(30*time.Minute), 3*time.Hour, nil) // covers it
			inside := at(t0.Add(70*time.Minute), 10*time.Minute, nil)
			at(from, time.Hour, func(l *domain.Lesson) { l.RoomID = other.ID }) // another room
			at(from, time.Hour, func(l *domain.Lesson) { l.Status = domain.LessonCanceled })

			got := mustQuery(e, func(ctx context.Context, tx store.Tx) ([]domain.Lesson, error) {
				return tx.Lessons().ListRoomOverlaps(ctx, s.room.ID, from, to, domain.LessonID{})
			})
			wantSlice(e.t, got, []domain.Lesson{early, around, inside, late})

			// The lesson being edited does not overlap itself.
			got = mustQuery(e, func(ctx context.Context, tx store.Tx) ([]domain.Lesson, error) {
				return tx.Lessons().ListRoomOverlaps(ctx, s.room.ID, from, to, around.ID)
			})
			wantSlice(e.t, got, []domain.Lesson{early, inside, late})

			got = mustQuery(e, func(ctx context.Context, tx store.Tx) ([]domain.Lesson, error) {
				return tx.Lessons().ListRoomOverlaps(ctx, other.ID, t0.Add(-time.Hour), t0, domain.LessonID{})
			})
			wantSlice(e.t, got, nil)
		},
		"MissingReferenceIsConflict": func(e *env) {
			s := e.setting()
			for name, mutate := range map[string]func(*domain.Lesson){
				"class":       func(l *domain.Lesson) { l.ClassID, l.SlotDate = newID[domain.ClassID](e), slot0 },
				"course type": func(l *domain.Lesson) { l.CourseTypeID = newID[domain.CourseTypeID](e) },
				"room":        func(l *domain.Lesson) { l.RoomID = newID[domain.RoomID](e) },
				"instructor":  func(l *domain.Lesson) { l.InstructorID = newID[domain.StaffID](e) },
				"accompanist": func(l *domain.Lesson) { l.AccompanistID = newID[domain.StaffID](e) },
				"canceler": func(l *domain.Lesson) {
					l.Status, l.CanceledAt, l.CanceledBy = domain.LessonCanceled, t0, newID[domain.AccountID](e)
				},
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
				full.Source = domain.BookingSourceMember
				full.Trial = true
				full.TrialNote = "First class after a knee injury"
				full.CanceledAt = t0.Add(-30 * time.Minute)
				full.Late = true
				full.Released = true
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
				// A walk-in guest without an email, whom staff booked.
				guest := domain.Booking{
					ID: newID[domain.BookingID](e), LessonID: e.lesson(e.setting(), t0, 5).ID,
					GuestName: "Gina Guest", Status: domain.BookingBooked, Source: domain.BookingSourceStaff, BookedAt: t0,
				}
				attended := guest
				attended.GuestEmail = "gina@example.org"
				attended.Status = domain.BookingAttended
				attended.Trial = true
				attended.TrialNote = "Danced jazz before"
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

			// A second active booking is a duplicate, which callers tell
			// apart from a missing reference.
			dup := e.newBooking(l, p, t0.Add(-2*time.Hour))
			err := e.write(func(ctx context.Context, tx store.Tx) error { return tx.Bookings().Create(ctx, dup) })
			wantErr(e.t, err, domain.ErrDuplicate)

			gone := e.newBooking(l, p, t0.Add(-2*time.Hour))
			gone.PersonID = newID[domain.PersonID](e)
			err = e.write(func(ctx context.Context, tx store.Tx) error { return tx.Bookings().Create(ctx, gone) })
			wantErr(e.t, err, domain.ErrConflict)
			wantNotErr(e.t, err, domain.ErrDuplicate)

			// After canceling, the person may book again.
			first.Status, first.CanceledAt = domain.BookingCanceled, t0.Add(-150*time.Minute)
			e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Bookings().Update(ctx, first) })
			e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Bookings().Create(ctx, dup) })

			// Reactivating the canceled booking now conflicts.
			first.Status, first.CanceledAt = domain.BookingBooked, time.Time{}
			err = e.write(func(ctx context.Context, tx store.Tx) error { return tx.Bookings().Update(ctx, first) })
			wantErr(e.t, err, domain.ErrDuplicate)

			// Guests are not limited.
			for range 2 {
				g := domain.Booking{
					ID: newID[domain.BookingID](e), LessonID: l.ID, GuestName: "Gina Guest",
					GuestEmail: "gina@example.org", Status: domain.BookingBooked, Source: domain.BookingSourceSelf,
					BookedAt: t0,
				}
				e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Bookings().Create(ctx, g) })
			}

			n := mustQuery(e, func(ctx context.Context, tx store.Tx) (int, error) { return tx.Bookings().CountActive(ctx, l.ID) })
			wantEqual(e.t, n, 3)
		},
		"ListByIDs": func(e *env) {
			s := e.setting()
			l := e.lesson(s, t0, 5)
			later := e.booking(l, e.person("Ada", "Booker"), t0.Add(-time.Hour))
			earlier := e.booking(e.lesson(s, t0.Add(time.Hour), 5), e.person("Bob", "Booker"), t0.Add(-2*time.Hour))
			e.booking(l, e.person("Cy", "Booker"), t0.Add(-3*time.Hour))

			got := mustQuery(e, func(ctx context.Context, tx store.Tx) ([]domain.Booking, error) {
				return tx.Bookings().ListByIDs(ctx, []domain.BookingID{later.ID, earlier.ID, later.ID, newID[domain.BookingID](e)})
			})
			wantSlice(e.t, got, []domain.Booking{earlier, later})
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
		"CountActiveByLessons": func(e *env) {
			s := e.setting()
			l1, l2, l3 := e.lesson(s, t0, 5), e.lesson(s, t0.Add(time.Hour), 5), e.lesson(s, t0.Add(2*time.Hour), 5)
			p1, p2, p3, p4 := e.person("Ann", "One"), e.person("Ben", "Two"), e.person("Cy", "Three"), e.person("Di", "Four")

			e.booking(l1, p1, t0.Add(-3*time.Hour))
			attended := e.booking(l1, p2, t0.Add(-3*time.Hour))
			noShow := e.booking(l1, p3, t0.Add(-3*time.Hour))
			canceled := e.booking(l1, p4, t0.Add(-3*time.Hour))
			gone := e.booking(l3, p1, t0.Add(-3*time.Hour))
			e.mustWrite(func(ctx context.Context, tx store.Tx) error {
				attended.Status, noShow.Status = domain.BookingAttended, domain.BookingNoShow
				canceled.Status, canceled.CanceledAt = domain.BookingCanceled, t0.Add(-2*time.Hour)
				gone.Status, gone.CanceledAt, gone.Late = domain.BookingCanceled, t0.Add(-time.Minute), true

				for _, b := range []domain.Booking{attended, noShow, canceled, gone} {
					if err := tx.Bookings().Update(ctx, b); err != nil {
						return err
					}
				}

				return nil
			})

			guest := domain.Booking{
				ID: newID[domain.BookingID](e), LessonID: l2.ID, GuestName: "Gina Guest",
				GuestEmail: "gina@example.org", Status: domain.BookingBooked, Source: domain.BookingSourceSelf,
				BookedAt: t0,
			}
			e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Bookings().Create(ctx, guest) })

			// Attended and no-show bookings hold their place; canceled ones
			// do not. Lessons without an active booking, and unknown ones,
			// are left out.
			got := mustQuery(e, func(ctx context.Context, tx store.Tx) (map[domain.LessonID]int, error) {
				return tx.Bookings().CountActiveByLessons(ctx, []domain.LessonID{l1.ID, l2.ID, l3.ID, newID[domain.LessonID](e), l1.ID})
			})
			if len(got) != 2 || got[l1.ID] != 3 || got[l2.ID] != 1 {
				e.t.Fatalf("CountActiveByLessons = %v, want %s: 3, %s: 1", got, l1.ID, l2.ID)
			}

			got = mustQuery(e, func(ctx context.Context, tx store.Tx) (map[domain.LessonID]int, error) {
				return tx.Bookings().CountActiveByLessons(ctx, []domain.LessonID{l2.ID})
			})
			if len(got) != 1 || got[l2.ID] != 1 {
				e.t.Fatalf("CountActiveByLessons(l2) = %v", got)
			}

			got = mustQuery(e, func(ctx context.Context, tx store.Tx) (map[domain.LessonID]int, error) {
				return tx.Bookings().CountActiveByLessons(ctx, nil)
			})
			if len(got) != 0 {
				e.t.Fatalf("CountActiveByLessons(nil) = %v", got)
			}
		},
		"CountsByLessons": func(e *env) {
			s := e.setting()
			l1, l2, l3 := e.lesson(s, t0, 5), e.lesson(s, t0.Add(time.Hour), 5), e.lesson(s, t0.Add(2*time.Hour), 5)
			empty := e.lesson(s, t0.Add(3*time.Hour), 5)
			p1, p2, p3, p4 := e.person("Ann", "One"), e.person("Ben", "Two"), e.person("Cy", "Three"), e.person("Di", "Four")

			e.booking(l1, p1, t0.Add(-time.Hour))
			trial := e.newBooking(l1, p2, t0.Add(-2*time.Hour))
			trial.Trial, trial.TrialNote = true, "First time"
			noShow := e.booking(l1, p3, t0.Add(-3*time.Hour))
			canceled := e.newBooking(l1, p4, t0.Add(-4*time.Hour))
			canceled.Trial = true
			e.booking(l2, p1, t0.Add(-3*time.Hour))
			e.booking(l3, p2, t0.Add(-5*time.Hour)) // a lesson not asked for
			gone := e.booking(empty, p1, t0.Add(-5*time.Hour))

			guest := domain.Booking{
				ID: newID[domain.BookingID](e), LessonID: l2.ID, GuestName: "Gina Guest", Trial: true,
				Status: domain.BookingAttended, Source: domain.BookingSourceStaff, BookedAt: t0.Add(-2 * time.Hour),
			}
			canceledGuest := domain.Booking{
				ID: newID[domain.BookingID](e), LessonID: l2.ID, GuestName: "Gone Guest",
				Status: domain.BookingCanceled, Source: domain.BookingSourceStaff, BookedAt: t0.Add(-2 * time.Hour),
				CanceledAt: t0.Add(-time.Hour), Late: true,
			}
			lateOnly := e.lesson(s, t0.Add(4*time.Hour), 5)
			lateCancel := e.booking(lateOnly, p3, t0.Add(-5*time.Hour))
			e.mustWrite(func(ctx context.Context, tx store.Tx) error {
				noShow.Status = domain.BookingNoShow
				canceled.Status, canceled.CanceledAt = domain.BookingCanceled, t0.Add(-90*time.Minute)
				gone.Status, gone.CanceledAt = domain.BookingCanceled, t0.Add(-90*time.Minute)
				lateCancel.Status, lateCancel.CanceledAt, lateCancel.Late = domain.BookingCanceled, t0.Add(-time.Hour), true

				return errors.Join(
					tx.Bookings().Create(ctx, trial), tx.Bookings().Create(ctx, canceled),
					tx.Bookings().Update(ctx, noShow), tx.Bookings().Update(ctx, gone),
					tx.Bookings().Create(ctx, guest), tx.Bookings().Create(ctx, canceledGuest),
					tx.Bookings().Update(ctx, lateCancel),
				)
			})

			// Canceled bookings are left out, late ones too, and so are
			// lessons without an active booking and unknown ones.
			got := mustQuery(e, func(ctx context.Context, tx store.Tx) (map[domain.LessonID]store.BookingCounts, error) {
				return tx.Bookings().CountsByLessons(ctx, []domain.LessonID{
					l1.ID, l2.ID, empty.ID, lateOnly.ID, newID[domain.LessonID](e),
				})
			})

			want := map[domain.LessonID]store.BookingCounts{
				l1.ID: {Active: 3, Trials: 1, Guests: 0, Booked: 2, NoShow: 1},
				l2.ID: {Active: 2, Trials: 1, Guests: 1, Booked: 1, Attended: 1},
			}
			if len(got) != len(want) || got[l1.ID] != want[l1.ID] || got[l2.ID] != want[l2.ID] {
				e.t.Fatalf("CountsByLessons = %+v, want %+v", got, want)
			}

			got = mustQuery(e, func(ctx context.Context, tx store.Tx) (map[domain.LessonID]store.BookingCounts, error) {
				return tx.Bookings().CountsByLessons(ctx, nil)
			})
			if len(got) != 0 {
				e.t.Fatalf("CountsByLessons(nil) = %+v", got)
			}
		},
		"CountLateCanceledByLessons": func(e *env) {
			s := e.setting()
			l1, l2, l3 := e.lesson(s, t0, 5), e.lesson(s, t0.Add(time.Hour), 5), e.lesson(s, t0.Add(2*time.Hour), 5)
			empty := e.lesson(s, t0.Add(3*time.Hour), 5)
			p1, p2, p3 := e.person("Ann", "One"), e.person("Ben", "Two"), e.person("Cy", "Three")

			late1, late2 := e.booking(l1, p1, t0.Add(-5*time.Hour)), e.booking(l1, p2, t0.Add(-5*time.Hour))
			e.booking(l1, p3, t0.Add(-5*time.Hour)) // active
			inTime := e.booking(l2, p1, t0.Add(-5*time.Hour))
			lateGuest := domain.Booking{
				ID: newID[domain.BookingID](e), LessonID: l2.ID, GuestName: "Gone Guest",
				Status: domain.BookingCanceled, Source: domain.BookingSourceStaff, BookedAt: t0.Add(-2 * time.Hour),
				CanceledAt: t0.Add(-time.Hour), Late: true,
			}
			notAsked := e.booking(l3, p1, t0.Add(-5*time.Hour))
			emptyInTime := e.booking(empty, p2, t0.Add(-5*time.Hour))
			e.mustWrite(func(ctx context.Context, tx store.Tx) error {
				for _, b := range []*domain.Booking{&late1, &late2, &notAsked} {
					b.Status, b.CanceledAt, b.Late = domain.BookingCanceled, t0.Add(-time.Hour), true
				}

				for _, b := range []*domain.Booking{&inTime, &emptyInTime} {
					b.Status, b.CanceledAt = domain.BookingCanceled, t0.Add(-time.Hour)
				}

				return errors.Join(
					tx.Bookings().Update(ctx, late1), tx.Bookings().Update(ctx, late2),
					tx.Bookings().Update(ctx, notAsked), tx.Bookings().Update(ctx, inTime),
					tx.Bookings().Update(ctx, emptyInTime), tx.Bookings().Create(ctx, lateGuest),
				)
			})

			// Only late cancellations count, a guest's too; lessons without
			// one are left out, and so are unknown ones.
			got := mustQuery(e, func(ctx context.Context, tx store.Tx) (map[domain.LessonID]int, error) {
				return tx.Bookings().CountLateCanceledByLessons(ctx, []domain.LessonID{
					l1.ID, l2.ID, empty.ID, newID[domain.LessonID](e),
				})
			})
			if want := map[domain.LessonID]int{l1.ID: 2, l2.ID: 1}; !maps.Equal(got, want) {
				e.t.Fatalf("CountLateCanceledByLessons = %v, want %v", got, want)
			}

			got = mustQuery(e, func(ctx context.Context, tx store.Tx) (map[domain.LessonID]int, error) {
				return tx.Bookings().CountLateCanceledByLessons(ctx, nil)
			})
			if len(got) != 0 {
				e.t.Fatalf("CountLateCanceledByLessons(nil) = %v", got)
			}
		},
		"CountActivePersons": func(e *env) {
			s := e.setting()
			l1, l2, l3 := e.lesson(s, t0, 5), e.lesson(s, t0.Add(time.Hour), 5), e.lesson(s, t0.Add(2*time.Hour), 5)
			ann, ben, cy := e.person("Ann", "One"), e.person("Ben", "Two"), e.person("Cy", "Three")

			e.booking(l1, ann, t0.Add(-time.Hour))
			e.booking(l2, ann, t0.Add(-time.Hour)) // the same person twice
			e.booking(l3, ben, t0.Add(-time.Hour)) // a lesson not asked for
			gone := e.booking(l2, cy, t0.Add(-time.Hour))
			guest := domain.Booking{
				ID: newID[domain.BookingID](e), LessonID: l1.ID, GuestName: "Gina Guest",
				Status: domain.BookingBooked, Source: domain.BookingSourceStaff, BookedAt: t0.Add(-time.Hour),
			}
			e.mustWrite(func(ctx context.Context, tx store.Tx) error {
				gone.Status, gone.CanceledAt = domain.BookingCanceled, t0.Add(-time.Minute)

				return errors.Join(tx.Bookings().Update(ctx, gone), tx.Bookings().Create(ctx, guest))
			})

			count := func(lessons ...domain.LessonID) int {
				return mustQuery(e, func(ctx context.Context, tx store.Tx) (int, error) {
					return tx.Bookings().CountActivePersons(ctx, lessons)
				})
			}
			// Canceled bookings and guests do not count, a person once.
			wantEqual(e.t, count(l1.ID, l2.ID), 1)
			wantEqual(e.t, count(l1.ID, l2.ID, l3.ID, newID[domain.LessonID](e)), 2)
			wantEqual(e.t, count(), 0)
		},
		"ListActiveByLessonsAndPersons": func(e *env) {
			s := e.setting()
			l1, l2, l3 := e.lesson(s, t0, 5), e.lesson(s, t0.Add(time.Hour), 5), e.lesson(s, t0.Add(2*time.Hour), 5)
			me, child, stranger := e.person("Mia", "Member"), e.person("Kim", "Member"), e.person("Sam", "Stranger")

			late := e.booking(l1, me, t0.Add(-time.Hour))
			early2 := e.booking(l2, child, t0.Add(-2*time.Hour))
			early1 := e.booking(l1, child, t0.Add(-2*time.Hour))
			e.booking(l1, stranger, t0.Add(-3*time.Hour)) // not asked for
			e.booking(l3, me, t0.Add(-3*time.Hour))       // lesson not asked for

			canceled := e.booking(l2, me, t0.Add(-4*time.Hour))
			canceled.Status, canceled.CanceledAt = domain.BookingCanceled, t0.Add(-90*time.Minute)
			e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Bookings().Update(ctx, canceled) })

			guest := domain.Booking{
				ID: newID[domain.BookingID](e), LessonID: l1.ID, GuestName: "Gina Guest",
				GuestEmail: "gina@example.org", Status: domain.BookingBooked, Source: domain.BookingSourceSelf,
				BookedAt: t0.Add(-5 * time.Hour),
			}
			e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Bookings().Create(ctx, guest) })

			list := func(lessons []domain.LessonID, persons []domain.PersonID) []domain.Booking {
				return mustQuery(e, func(ctx context.Context, tx store.Tx) ([]domain.Booking, error) {
					return tx.Bookings().ListActiveByLessonsAndPersons(ctx, lessons, persons)
				})
			}

			wantSlice(e.t, list([]domain.LessonID{l1.ID, l2.ID}, []domain.PersonID{me.ID, child.ID}),
				[]domain.Booking{early2, early1, late})
			wantSlice(e.t, list([]domain.LessonID{l2.ID}, []domain.PersonID{me.ID}), nil)
			wantSlice(e.t, list(nil, []domain.PersonID{me.ID}), nil)
			wantSlice(e.t, list([]domain.LessonID{l1.ID}, nil), nil)
		},
		"ListByLessonsAndPersons": func(e *env) {
			s := e.setting()
			l1, l2, l3 := e.lesson(s, t0, 5), e.lesson(s, t0.Add(time.Hour), 5), e.lesson(s, t0.Add(2*time.Hour), 5)
			me, child, stranger := e.person("Mia", "Member"), e.person("Kim", "Member"), e.person("Sam", "Stranger")

			late := e.booking(l1, me, t0.Add(-time.Hour))
			early := e.booking(l2, child, t0.Add(-2*time.Hour))
			e.booking(l1, stranger, t0.Add(-3*time.Hour)) // not asked for
			e.booking(l3, me, t0.Add(-3*time.Hour))       // lesson not asked for

			// Canceled ones are included.
			canceled := e.booking(l2, me, t0.Add(-4*time.Hour))
			canceled.Status, canceled.CanceledAt, canceled.Released = domain.BookingCanceled, t0.Add(-90*time.Minute), true
			e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Bookings().Update(ctx, canceled) })

			guest := domain.Booking{
				ID: newID[domain.BookingID](e), LessonID: l1.ID, GuestName: "Gina Guest",
				GuestEmail: "gina@example.org", Status: domain.BookingBooked, Source: domain.BookingSourceSelf,
				BookedAt: t0.Add(-5 * time.Hour),
			}
			e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Bookings().Create(ctx, guest) })

			list := func(lessons []domain.LessonID, persons []domain.PersonID) []domain.Booking {
				return mustQuery(e, func(ctx context.Context, tx store.Tx) ([]domain.Booking, error) {
					return tx.Bookings().ListByLessonsAndPersons(ctx, lessons, persons)
				})
			}

			wantSlice(e.t, list([]domain.LessonID{l1.ID, l2.ID}, []domain.PersonID{me.ID, child.ID}),
				[]domain.Booking{canceled, early, late})
			wantSlice(e.t, list(nil, []domain.PersonID{me.ID}), nil)
			wantSlice(e.t, list([]domain.LessonID{l1.ID}, nil), nil)
		},
		"ListByPersons": func(e *env) {
			s := e.setting()
			me, child, stranger := e.person("Mia", "Member"), e.person("Kim", "Member"), e.person("Sam", "Stranger")

			// Lessons of an hour: early ends at t0, now (t0 + 30m) is
			// within running, late starts at t0 + 2h.
			early := e.lesson(s, t0.Add(-time.Hour), 5)
			running := e.lesson(s, t0, 5)
			parallel := e.lesson(s, t0, 5) // same start, a later ID
			late := e.lesson(s, t0.Add(2*time.Hour), 5)
			last := e.lesson(s, t0.Add(48*time.Hour), 5)

			mine := e.booking(running, me, t0.Add(-3*time.Hour))
			childsNow := e.booking(running, child, t0.Add(-2*time.Hour))
			// Booked between the two of running, but listed after them: the
			// bookings of a lesson stay together.
			inParallel := e.booking(parallel, me, t0.Add(-150*time.Minute))
			childsLate := e.booking(late, child, t0.Add(-4*time.Hour))
			mineLate := e.booking(late, me, t0.Add(-4*time.Hour)) // same lesson and time: after it by ID
			e.booking(running, stranger, t0.Add(-5*time.Hour))    // someone else's

			// Canceled ones are included, as is a booking again after it.
			canceled := e.booking(early, me, t0.Add(-6*time.Hour))
			canceled.Status, canceled.CanceledAt = domain.BookingCanceled, t0.Add(-5*time.Hour)
			e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Bookings().Update(ctx, canceled) })
			again := e.booking(early, me, t0.Add(-4*time.Hour))
			final := e.booking(last, child, t0.Add(-time.Hour))

			guest := domain.Booking{
				ID: newID[domain.BookingID](e), LessonID: running.ID, GuestName: "Gina Guest",
				Status: domain.BookingBooked, Source: domain.BookingSourceStaff, BookedAt: t0.Add(-time.Hour),
			}
			e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Bookings().Create(ctx, guest) })

			family := []domain.PersonID{me.ID, child.ID}
			list := func(q store.BookingQuery) []domain.Booking {
				e.t.Helper()

				return mustQuery(e, func(ctx context.Context, tx store.Tx) ([]domain.Booking, error) {
					return tx.Bookings().ListByPersons(ctx, q)
				})
			}

			all := []domain.Booking{canceled, again, mine, childsNow, inParallel, childsLate, mineLate, final}
			wantSlice(e.t, list(store.BookingQuery{Persons: family}), all)

			reversed := slices.Clone(all)
			slices.Reverse(reversed)
			wantSlice(e.t, list(store.BookingQuery{Persons: family, Desc: true}), reversed)

			// Start bounds are half-open: from t0 up to (not including)
			// t0 + 48h.
			wantSlice(e.t, list(store.BookingQuery{Persons: family, StartsFrom: t0, StartsBefore: t0.Add(48 * time.Hour)}),
				[]domain.Booking{mine, childsNow, inParallel, childsLate, mineLate})
			wantSlice(e.t, list(store.BookingQuery{
				Persons: family, StartsFrom: t0.Add(-time.Hour).Add(time.Microsecond), StartsBefore: t0,
			}), nil)

			// The lessons not over at t0 + 30m, and those over by t0 (early
			// ends exactly then).
			wantSlice(e.t, list(store.BookingQuery{Persons: family, EndsAfter: t0.Add(30 * time.Minute)}),
				[]domain.Booking{mine, childsNow, inParallel, childsLate, mineLate, final})
			wantSlice(e.t, list(store.BookingQuery{Persons: family, EndsAfter: t0.Add(time.Hour)}),
				[]domain.Booking{childsLate, mineLate, final})
			wantSlice(e.t, list(store.BookingQuery{Persons: family, EndsBy: t0}), []domain.Booking{canceled, again})
			wantSlice(e.t, list(store.BookingQuery{Persons: family, EndsBy: t0.Add(-time.Microsecond)}), nil)

			// The limit cuts after the order, both ways.
			wantSlice(e.t, list(store.BookingQuery{Persons: family, Limit: 3}), []domain.Booking{canceled, again, mine})
			wantSlice(e.t, list(store.BookingQuery{Persons: family, Desc: true, Limit: 2, StartsBefore: t0}),
				[]domain.Booking{again, canceled})

			// Active leaves the canceled ones out.
			wantSlice(e.t, list(store.BookingQuery{Persons: family, Active: true}), all[1:])
			wantSlice(e.t, list(store.BookingQuery{Persons: family, Active: true, Desc: true, StartsBefore: t0}),
				[]domain.Booking{again})

			wantSlice(e.t, list(store.BookingQuery{Persons: []domain.PersonID{child.ID}, EndsBy: t0.Add(time.Hour)}),
				[]domain.Booking{childsNow})
			wantSlice(e.t, list(store.BookingQuery{}), nil)
		},
		"DeleteByLessonAndSource": func(e *env) {
			s := e.setting()
			l, other := e.lesson(s, t0, 5), e.lesson(s, t0.Add(time.Hour), 5)
			actor := e.account(e.person("Olga", "Office"), "olga@example.org")

			member := func(lesson domain.Lesson, p domain.Person) domain.Booking {
				b := e.newBooking(lesson, p, t0.Add(-3*time.Hour))
				b.Source = domain.BookingSourceMember
				e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Bookings().Create(ctx, b) })

				return b
			}

			active := member(l, e.person("Ann", "Member"))
			released := member(l, e.person("Ben", "Member"))
			released.Status, released.CanceledAt, released.Late = domain.BookingCanceled, t0.Add(-time.Hour), true
			e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Bookings().Update(ctx, released) })

			self := e.booking(l, e.person("Cy", "Self"), t0.Add(-2*time.Hour))
			elsewhere := member(other, e.person("Di", "Member"))

			for _, b := range []domain.Booking{active, released, self, elsewhere} {
				ev := domain.BookingEvent{
					ID: newID[domain.BookingEventID](e), BookingID: b.ID, At: b.BookedAt, Status: domain.BookingBooked,
					ActorAccountID: actor.ID,
				}
				e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Bookings().AddEvent(ctx, ev) })
			}

			deleteBy := func(lesson domain.Lesson, source domain.BookingSource) int {
				var n int

				e.mustWrite(func(ctx context.Context, tx store.Tx) error {
					var err error
					n, err = tx.Bookings().DeleteByLessonAndSource(ctx, lesson.ID, source)

					return err
				})

				return n
			}

			// The lesson's member bookings go with their events, canceled
			// ones included; other sources and other lessons stay.
			wantEqual(e.t, deleteBy(l, domain.BookingSourceMember), 2)

			got := mustQuery(e, func(ctx context.Context, tx store.Tx) ([]domain.Booking, error) {
				return tx.Bookings().ListByLesson(ctx, l.ID)
			})
			wantSlice(e.t, got, []domain.Booking{self})

			for _, b := range []domain.Booking{active, released} {
				err := e.read(func(ctx context.Context, tx store.Tx) error {
					_, err := tx.Bookings().Get(ctx, b.ID)

					return err
				})
				wantErr(e.t, err, domain.ErrNotFound)

				events := mustQuery(e, func(ctx context.Context, tx store.Tx) ([]domain.BookingEvent, error) {
					return tx.Bookings().ListEvents(ctx, b.ID)
				})
				wantEqual(e.t, len(events), 0)
			}

			for _, b := range []domain.Booking{self, elsewhere} {
				events := mustQuery(e, func(ctx context.Context, tx store.Tx) ([]domain.BookingEvent, error) {
					return tx.Bookings().ListEvents(ctx, b.ID)
				})
				wantEqual(e.t, len(events), 1)
			}

			got = mustQuery(e, func(ctx context.Context, tx store.Tx) ([]domain.Booking, error) {
				return tx.Bookings().ListByLesson(ctx, other.ID)
			})
			wantSlice(e.t, got, []domain.Booking{elsewhere})

			// Nothing left to delete; then the lesson without member
			// bookings goes once its other booking is gone too.
			wantEqual(e.t, deleteBy(l, domain.BookingSourceMember), 0)
			wantEqual(e.t, deleteBy(l, domain.BookingSourceSelf), 1)
			e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Lessons().Delete(ctx, l.ID) })

			err := e.read(func(ctx context.Context, tx store.Tx) error {
				_, err := tx.Bookings().DeleteByLessonAndSource(ctx, other.ID, domain.BookingSourceMember)

				return err
			})
			wantErr(e.t, err, store.ErrReadOnly)
		},
		"LockSerialisesTheLastPlace": func(e *env) {
			// Every capacity check runs under the lesson lock, so of many
			// concurrent bookings of the last place exactly one succeeds.
			const n = 8

			l := e.lesson(e.setting(), t0, 2)
			e.booking(l, e.person("First", "Booker"), t0.Add(-2*time.Hour))

			bookings := make([]domain.Booking, n)
			for i := range bookings {
				bookings[i] = e.newBooking(l, e.person("Racer", string(rune('A'+i))), t0.Add(-time.Hour))
			}

			errFull := errors.New("full")
			results := make(chan error, n)

			var wg sync.WaitGroup

			for _, b := range bookings {
				wg.Go(func() {
					results <- e.write(func(ctx context.Context, tx store.Tx) error {
						locked, err := tx.Lessons().Lock(ctx, b.LessonID)
						if err != nil {
							return err
						}

						active, err := tx.Bookings().CountActive(ctx, locked.ID)
						if err != nil {
							return err
						}

						if active >= locked.Capacity {
							return errFull
						}

						return tx.Bookings().Create(ctx, b)
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
				case !errors.Is(err, errFull):
					e.t.Fatalf("unexpected error: %v", err)
				}
			}

			wantEqual(e.t, ok, 1)

			active := mustQuery(e, func(ctx context.Context, tx store.Tx) (int, error) { return tx.Bookings().CountActive(ctx, l.ID) })
			wantEqual(e.t, active, 2)
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
				Status: domain.BookingCanceled, ActorAccountID: actor.ID, Note: "Called in sick.", Late: true,
			}
			first2 := domain.BookingEvent{
				ID: newID[domain.BookingEventID](e), BookingID: b.ID, At: t0.Add(-time.Hour), Status: domain.BookingBooked,
				Override: true,
			}
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
