package storetest

import (
	"context"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/store"
)

func testTimetable(t *testing.T, newStore func(*testing.T) store.Store) {
	t.Helper()

	t.Run("Terms", func(t *testing.T) {
		testCRUD(t, newStore, crud[domain.TermID, domain.Term]{
			id: func(tm domain.Term) domain.TermID { return tm.ID },
			get: func(ctx context.Context, tx store.Tx, id domain.TermID) (domain.Term, error) {
				return tx.Terms().Get(ctx, id)
			},
			create: func(ctx context.Context, tx store.Tx, tm domain.Term) error { return tx.Terms().Create(ctx, tm) },
			update: func(ctx context.Context, tx store.Tx, tm domain.Term) error { return tx.Terms().Update(ctx, tm) },
			remove: func(ctx context.Context, tx store.Tx, id domain.TermID) error { return tx.Terms().Delete(ctx, id) },
			fixtures: func(e *env) (domain.Term, domain.Term) {
				minimal := domain.Term{
					ID: newID[domain.TermID](e), Name: "Summer",
					StartsOn: domain.Date{Year: 2026, Month: time.July, Day: 1}, EndsOn: domain.Date{Year: 2026, Month: time.July, Day: 1},
				}
				full := minimal
				full.Name = "Summer (extended)"
				full.EndsOn = domain.Date{Year: 2026, Month: time.August, Day: 31}

				return minimal, full
			},
		})
	})

	t.Run("Classes", func(t *testing.T) {
		testCRUD(t, newStore, crud[domain.ClassID, domain.Class]{
			id: func(c domain.Class) domain.ClassID { return c.ID },
			get: func(ctx context.Context, tx store.Tx, id domain.ClassID) (domain.Class, error) {
				return tx.Classes().Get(ctx, id)
			},
			create: func(ctx context.Context, tx store.Tx, c domain.Class) error { return tx.Classes().Create(ctx, c) },
			update: func(ctx context.Context, tx store.Tx, c domain.Class) error { return tx.Classes().Update(ctx, c) },
			remove: func(ctx context.Context, tx store.Tx, id domain.ClassID) error { return tx.Classes().Delete(ctx, id) },
			fixtures: func(e *env) (domain.Class, domain.Class) {
				minimal := e.newClass(e.setting(), time.Wednesday, domain.TimeOfDay{Hour: 16, Minute: 45})
				full := minimal
				full.AccompanistID = e.staffMember("Accompanist").ID
				full.Level = domain.LevelAdvanced
				full.Weekday = time.Saturday
				full.StartTime = domain.TimeOfDay{Hour: 10}
				full.Duration = 90 * time.Minute
				full.Capacity = 14
				full.Retired = true
				valid(e.t, full)

				return minimal, full
			},
		})
	})

	cases(t, newStore, map[string]func(e *env){
		"TermListOrder": func(e *env) {
			late := e.term("Autumn", domain.Date{Year: 2026, Month: time.September, Day: 1})
			early2 := e.term("Spring B", domain.Date{Year: 2026, Month: time.March, Day: 1})
			early1 := e.term("Spring A", domain.Date{Year: 2026, Month: time.March, Day: 1})
			winter := e.term("Winter", domain.Date{Year: 2025, Month: time.December, Day: 1})

			got := mustQuery(e, func(ctx context.Context, tx store.Tx) ([]domain.Term, error) { return tx.Terms().List(ctx) })
			wantSlice(e.t, got, []domain.Term{winter, early2, early1, late})
		},
		"ListByTerm": func(e *env) {
			s := e.setting()
			satEarly := e.class(s, time.Saturday, domain.TimeOfDay{Hour: 9})
			monLate := e.class(s, time.Monday, domain.TimeOfDay{Hour: 18, Minute: 30})
			monEarly := e.class(s, time.Monday, domain.TimeOfDay{Hour: 9, Minute: 15})
			sun := e.class(s, time.Sunday, domain.TimeOfDay{Hour: 11})
			monLate2 := e.class(s, time.Monday, domain.TimeOfDay{Hour: 18, Minute: 30})

			other := s
			other.term = e.term("Other", domain.Date{Year: 2027, Month: time.January, Day: 1})
			e.class(other, time.Monday, domain.TimeOfDay{Hour: 8})

			got := mustQuery(e, func(ctx context.Context, tx store.Tx) ([]domain.Class, error) {
				return tx.Classes().ListByTerm(ctx, s.term.ID)
			})
			wantSlice(e.t, got, []domain.Class{sun, monEarly, monLate, monLate2, satEarly})
		},
		"ListByIDs": func(e *env) {
			s := e.setting()
			first := e.class(s, time.Saturday, domain.TimeOfDay{Hour: 9})
			second := e.class(s, time.Monday, domain.TimeOfDay{Hour: 9})
			e.class(s, time.Sunday, domain.TimeOfDay{Hour: 11}) // not asked for

			list := func(ids ...domain.ClassID) []domain.Class {
				e.t.Helper()

				return mustQuery(e, func(ctx context.Context, tx store.Tx) ([]domain.Class, error) {
					return tx.Classes().ListByIDs(ctx, ids)
				})
			}

			// Each once, by ID (the sequence hands them out in order);
			// unknown IDs are left out.
			wantSlice(e.t, list(second.ID, newID[domain.ClassID](e), first.ID, second.ID),
				[]domain.Class{first, second})
			wantSlice(e.t, list(), nil)
		},
		"MissingReferenceIsConflict": func(e *env) {
			s := e.setting()
			for name, mutate := range map[string]func(*domain.Class){
				"term":        func(c *domain.Class) { c.TermID = newID[domain.TermID](e) },
				"course type": func(c *domain.Class) { c.CourseTypeID = newID[domain.CourseTypeID](e) },
				"room":        func(c *domain.Class) { c.RoomID = newID[domain.RoomID](e) },
				"instructor":  func(c *domain.Class) { c.InstructorID = newID[domain.StaffID](e) },
				"accompanist": func(c *domain.Class) { c.AccompanistID = newID[domain.StaffID](e) },
			} {
				c := e.newClass(s, time.Friday, domain.TimeOfDay{Hour: 17})
				mutate(&c)

				err := e.write(func(ctx context.Context, tx store.Tx) error { return tx.Classes().Create(ctx, c) })
				if !errorsIs(err, domain.ErrConflict) {
					e.t.Errorf("class with missing %s: want ErrConflict, got %v", name, err)
				}
			}
		},
		"Members": func(e *env) {
			c := e.class(e.setting(), time.Thursday, domain.TimeOfDay{Hour: 17})
			p1, p2 := e.person("Lea", "Member"), e.person("Max", "Member")
			m2 := domain.ClassMember{
				ClassID: c.ID, PersonID: p2.ID, Since: domain.Date{Year: 2026, Month: time.March, Day: 5},
				PausedUntil: domain.Date{Year: 2026, Month: time.April, Day: 30},
			}
			m1 := domain.ClassMember{ClassID: c.ID, PersonID: p1.ID}

			e.mustWrite(func(ctx context.Context, tx store.Tx) error {
				if err := tx.Classes().AddMember(ctx, m2); err != nil {
					return err
				}

				return tx.Classes().AddMember(ctx, m1)
			})

			list := func() []domain.ClassMember {
				return mustQuery(e, func(ctx context.Context, tx store.Tx) ([]domain.ClassMember, error) {
					return tx.Classes().ListMembers(ctx, c.ID)
				})
			}
			wantSlice(e.t, list(), []domain.ClassMember{m1, m2})

			err := e.write(func(ctx context.Context, tx store.Tx) error { return tx.Classes().AddMember(ctx, m1) })
			wantErr(e.t, err, domain.ErrDuplicate)

			err = e.write(func(ctx context.Context, tx store.Tx) error {
				return tx.Classes().AddMember(ctx, domain.ClassMember{ClassID: c.ID, PersonID: newID[domain.PersonID](e)})
			})
			wantErr(e.t, err, domain.ErrConflict)
			wantNotErr(e.t, err, domain.ErrDuplicate)

			err = e.write(func(ctx context.Context, tx store.Tx) error { return tx.Classes().Delete(ctx, c.ID) })
			wantErr(e.t, err, domain.ErrConflict)

			// UpdateMember replaces the dates, and clears them.
			m1.Since, m1.PausedUntil = domain.Date{Year: 2026, Month: time.May, Day: 4}, domain.Date{Year: 2026, Month: time.June, Day: 1}
			e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Classes().UpdateMember(ctx, m1) })
			wantSlice(e.t, list(), []domain.ClassMember{m1, m2})

			m2.Since, m2.PausedUntil = domain.Date{}, domain.Date{}
			e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Classes().UpdateMember(ctx, m2) })
			wantSlice(e.t, list(), []domain.ClassMember{m1, m2})

			stranger := domain.ClassMember{ClassID: c.ID, PersonID: e.person("Sam", "Stranger").ID}
			err = e.write(func(ctx context.Context, tx store.Tx) error { return tx.Classes().UpdateMember(ctx, stranger) })
			wantErr(e.t, err, domain.ErrNotFound)

			err = e.read(func(ctx context.Context, tx store.Tx) error { return tx.Classes().UpdateMember(ctx, m1) })
			wantErr(e.t, err, store.ErrReadOnly)

			err = e.read(func(ctx context.Context, tx store.Tx) error { return tx.Classes().RemoveMember(ctx, c.ID, p1.ID) })
			wantErr(e.t, err, store.ErrReadOnly)

			e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Classes().RemoveMember(ctx, c.ID, p1.ID) })
			wantSlice(e.t, list(), []domain.ClassMember{m2})

			err = e.write(func(ctx context.Context, tx store.Tx) error { return tx.Classes().RemoveMember(ctx, c.ID, p1.ID) })
			wantErr(e.t, err, domain.ErrNotFound)
		},
		"Memberships": func(e *env) {
			s := e.setting()
			c1, c2 := e.class(s, time.Monday, domain.TimeOfDay{Hour: 17}), e.class(s, time.Tuesday, domain.TimeOfDay{Hour: 17})
			p, other := e.person("Lea", "Member"), e.person("Max", "Member")
			m1 := domain.ClassMember{ClassID: c1.ID, PersonID: p.ID, Since: domain.Date{Year: 2026, Month: time.March, Day: 5}}
			m2 := domain.ClassMember{ClassID: c2.ID, PersonID: p.ID}

			e.mustWrite(func(ctx context.Context, tx store.Tx) error {
				for _, m := range []domain.ClassMember{m2, {ClassID: c1.ID, PersonID: other.ID}, m1} {
					if err := tx.Classes().AddMember(ctx, m); err != nil {
						return err
					}
				}

				return nil
			})

			memberships := func(id domain.PersonID) []domain.ClassMember {
				return mustQuery(e, func(ctx context.Context, tx store.Tx) ([]domain.ClassMember, error) {
					return tx.Classes().ListMemberships(ctx, id)
				})
			}
			wantSlice(e.t, memberships(p.ID), []domain.ClassMember{m1, m2}) // c1 has the smaller ID
			wantSlice(e.t, memberships(e.person("Nia", "None").ID), nil)
		},
		"Lock": func(e *env) {
			c := e.class(e.setting(), time.Monday, domain.TimeOfDay{Hour: 9})

			var got domain.Class

			e.mustWrite(func(ctx context.Context, tx store.Tx) error {
				var err error
				got, err = tx.Classes().Lock(ctx, c.ID)

				return err
			})
			wantEqual(e.t, got, c)

			err := e.read(func(ctx context.Context, tx store.Tx) error {
				_, err := tx.Classes().Lock(ctx, c.ID)

				return err
			})
			wantErr(e.t, err, store.ErrReadOnly)

			err = e.write(func(ctx context.Context, tx store.Tx) error {
				_, err := tx.Classes().Lock(ctx, newID[domain.ClassID](e))

				return err
			})
			wantErr(e.t, err, domain.ErrNotFound)
		},
		"TermLock": func(e *env) {
			tm := e.term("Autumn", domain.Date{Year: 2026, Month: time.September, Day: 1})

			var got domain.Term

			e.mustWrite(func(ctx context.Context, tx store.Tx) error {
				var err error
				got, err = tx.Terms().Lock(ctx, tm.ID)

				return err
			})
			wantEqual(e.t, got, tm)

			err := e.read(func(ctx context.Context, tx store.Tx) error {
				_, err := tx.Terms().Lock(ctx, tm.ID)

				return err
			})
			wantErr(e.t, err, store.ErrReadOnly)

			err = e.write(func(ctx context.Context, tx store.Tx) error {
				_, err := tx.Terms().Lock(ctx, newID[domain.TermID](e))

				return err
			})
			wantErr(e.t, err, domain.ErrNotFound)
		},
		"DeleteClassWithLessonIsConflict": func(e *env) {
			s := e.setting()
			c := e.class(s, time.Monday, domain.TimeOfDay{Hour: 9})
			e.classLesson(s, c, slot0, t0)

			err := e.write(func(ctx context.Context, tx store.Tx) error { return tx.Classes().Delete(ctx, c.ID) })
			wantErr(e.t, err, domain.ErrConflict)
		},
	})
}
