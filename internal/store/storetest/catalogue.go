package storetest

import (
	"context"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/store"
)

func testCatalogue(t *testing.T, newStore func(*testing.T) store.Store) {
	t.Helper()

	t.Run("Locations", func(t *testing.T) {
		testCRUD(t, newStore, crud[domain.LocationID, domain.Location]{
			id: func(l domain.Location) domain.LocationID { return l.ID },
			get: func(ctx context.Context, tx store.Tx, id domain.LocationID) (domain.Location, error) {
				return tx.Locations().Get(ctx, id)
			},
			create: func(ctx context.Context, tx store.Tx, l domain.Location) error { return tx.Locations().Create(ctx, l) },
			update: func(ctx context.Context, tx store.Tx, l domain.Location) error { return tx.Locations().Update(ctx, l) },
			remove: func(ctx context.Context, tx store.Tx, id domain.LocationID) error {
				return tx.Locations().Delete(ctx, id)
			},
			fixtures: func(e *env) (domain.Location, domain.Location) {
				minimal := domain.Location{ID: newID[domain.LocationID](e), Name: "South Studio"}
				full := minimal
				full.Address = "Example Street 1, 00000 Example Town"

				return minimal, full
			},
		})
	})

	t.Run("Rooms", func(t *testing.T) {
		testCRUD(t, newStore, crud[domain.RoomID, domain.Room]{
			id: func(r domain.Room) domain.RoomID { return r.ID },
			get: func(ctx context.Context, tx store.Tx, id domain.RoomID) (domain.Room, error) {
				return tx.Rooms().Get(ctx, id)
			},
			create: func(ctx context.Context, tx store.Tx, r domain.Room) error { return tx.Rooms().Create(ctx, r) },
			update: func(ctx context.Context, tx store.Tx, r domain.Room) error { return tx.Rooms().Update(ctx, r) },
			remove: func(ctx context.Context, tx store.Tx, id domain.RoomID) error { return tx.Rooms().Delete(ctx, id) },
			fixtures: func(e *env) (domain.Room, domain.Room) {
				minimal := domain.Room{ID: newID[domain.RoomID](e), LocationID: e.location("South Studio").ID, Name: "Room B", Capacity: 8}
				full := minimal
				full.LocationID = e.location("East Studio").ID
				full.Name = "Room B (large)"
				full.Capacity = 20

				return minimal, full
			},
		})
	})

	t.Run("Staff", func(t *testing.T) {
		testCRUD(t, newStore, crud[domain.StaffID, domain.Staff]{
			id: func(s domain.Staff) domain.StaffID { return s.ID },
			get: func(ctx context.Context, tx store.Tx, id domain.StaffID) (domain.Staff, error) {
				return tx.Staff().Get(ctx, id)
			},
			create: func(ctx context.Context, tx store.Tx, s domain.Staff) error { return tx.Staff().Create(ctx, s) },
			update: func(ctx context.Context, tx store.Tx, s domain.Staff) error { return tx.Staff().Update(ctx, s) },
			remove: func(ctx context.Context, tx store.Tx, id domain.StaffID) error { return tx.Staff().Delete(ctx, id) },
			fixtures: func(e *env) (domain.Staff, domain.Staff) {
				minimal := domain.Staff{ID: newID[domain.StaffID](e), DisplayName: "Pianist", Kind: domain.StaffAccompanist}
				full := minimal
				full.PersonID = e.person("Petra", "Piano").ID
				full.Active = true
				valid(e.t, minimal)
				valid(e.t, full)

				return minimal, full
			},
		})
	})

	t.Run("CourseTypes", func(t *testing.T) {
		testCRUD(t, newStore, crud[domain.CourseTypeID, domain.CourseType]{
			id: func(c domain.CourseType) domain.CourseTypeID { return c.ID },
			get: func(ctx context.Context, tx store.Tx, id domain.CourseTypeID) (domain.CourseType, error) {
				return tx.CourseTypes().Get(ctx, id)
			},
			create: func(ctx context.Context, tx store.Tx, c domain.CourseType) error {
				return tx.CourseTypes().Create(ctx, c)
			},
			update: func(ctx context.Context, tx store.Tx, c domain.CourseType) error {
				return tx.CourseTypes().Update(ctx, c)
			},
			remove: func(ctx context.Context, tx store.Tx, id domain.CourseTypeID) error {
				return tx.CourseTypes().Delete(ctx, id)
			},
			fixtures: func(e *env) (domain.CourseType, domain.CourseType) {
				minimal := domain.CourseType{ID: newID[domain.CourseTypeID](e), Name: "Jazz"}
				full := minimal
				full.Description = "Jazz dance for all ages."

				return minimal, full
			},
		})
	})

	cases(t, newStore, map[string]func(e *env){
		"ListOrder": func(e *env) {
			lb, la := e.location("b-Studio"), e.location("C-Studio")
			wantSlice(e.t, mustQuery(e, func(ctx context.Context, tx store.Tx) ([]domain.Location, error) {
				return tx.Locations().List(ctx)
			}), []domain.Location{la, lb})

			r2, r1, r3 := e.room(la, "Room 2"), e.room(la, "Room 1"), e.room(lb, "Room 1")
			wantSlice(e.t, mustQuery(e, func(ctx context.Context, tx store.Tx) ([]domain.Room, error) {
				return tx.Rooms().List(ctx, domain.LocationID{})
			}), []domain.Room{r1, r3, r2})
			wantSlice(e.t, mustQuery(e, func(ctx context.Context, tx store.Tx) ([]domain.Room, error) {
				return tx.Rooms().List(ctx, la.ID)
			}), []domain.Room{r1, r2})

			s2, s1 := e.staffMember("Zed"), e.staffMember("Amy")
			wantSlice(e.t, mustQuery(e, func(ctx context.Context, tx store.Tx) ([]domain.Staff, error) {
				return tx.Staff().List(ctx)
			}), []domain.Staff{s1, s2})

			c2, c1 := e.courseType("Tap"), e.courseType("Modern")
			wantSlice(e.t, mustQuery(e, func(ctx context.Context, tx store.Tx) ([]domain.CourseType, error) {
				return tx.CourseTypes().List(ctx)
			}), []domain.CourseType{c1, c2})
		},
		"StaffByPerson": func(e *env) {
			p, other := e.person("Sam", "Staff"), e.person("Oli", "Other")
			linked := func(name string, person domain.PersonID) domain.Staff {
				s := domain.Staff{ID: newID[domain.StaffID](e), PersonID: person, DisplayName: name, Kind: domain.StaffInstructor}
				e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Staff().Create(ctx, s) })

				return s
			}

			zed, amy := linked("Zed", p.ID), linked("Amy", p.ID)
			linked("Oli", other.ID)
			e.staffMember("Nobody") // no person

			byPerson := func(id domain.PersonID) []domain.Staff {
				return mustQuery(e, func(ctx context.Context, tx store.Tx) ([]domain.Staff, error) {
					return tx.Staff().ListByPerson(ctx, id)
				})
			}
			wantSlice(e.t, byPerson(p.ID), []domain.Staff{amy, zed})
			wantSlice(e.t, byPerson(e.person("Nia", "None").ID), nil)
		},
		"DuplicateNames": func(e *env) {
			main, other := e.location("Main Hall"), e.location("Side Hall")

			dup := domain.Location{ID: newID[domain.LocationID](e), Name: "main HALL"}
			wantErr(e.t, e.write(func(ctx context.Context, tx store.Tx) error { return tx.Locations().Create(ctx, dup) }), domain.ErrDuplicate)

			other.Name = "MAIN hall"
			wantErr(e.t, e.write(func(ctx context.Context, tx store.Tx) error { return tx.Locations().Update(ctx, other) }), domain.ErrDuplicate)

			// A location keeps its own name in another case.
			main.Name = "MAIN HALL"
			e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Locations().Update(ctx, main) })

			// Room names are unique per location only.
			room := e.room(main, "Studio 1")
			e.room(other, "Studio 1")

			dupRoom := domain.Room{ID: newID[domain.RoomID](e), LocationID: main.ID, Name: "studio 1", Capacity: 4}
			wantErr(e.t, e.write(func(ctx context.Context, tx store.Tx) error { return tx.Rooms().Create(ctx, dupRoom) }), domain.ErrDuplicate)

			moved := e.room(other, "Studio 2")
			moved.LocationID, moved.Name = main.ID, "STUDIO 1"
			wantErr(e.t, e.write(func(ctx context.Context, tx store.Tx) error { return tx.Rooms().Update(ctx, moved) }), domain.ErrDuplicate)

			room.Capacity = 30
			e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Rooms().Update(ctx, room) })

			ballet, jazz := e.courseType("Ballet"), e.courseType("Jazz")

			dupType := domain.CourseType{ID: newID[domain.CourseTypeID](e), Name: "BALLET"}
			wantErr(e.t, e.write(func(ctx context.Context, tx store.Tx) error { return tx.CourseTypes().Create(ctx, dupType) }), domain.ErrDuplicate)

			jazz.Name = "ballet"
			wantErr(e.t, e.write(func(ctx context.Context, tx store.Tx) error { return tx.CourseTypes().Update(ctx, jazz) }), domain.ErrDuplicate)

			ballet.Name, ballet.Description = "BALLET", "Classical"
			e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.CourseTypes().Update(ctx, ballet) })
		},
		"MissingReferenceIsConflict": func(e *env) {
			room := domain.Room{ID: newID[domain.RoomID](e), LocationID: newID[domain.LocationID](e), Name: "Nowhere", Capacity: 1}
			err := e.write(func(ctx context.Context, tx store.Tx) error { return tx.Rooms().Create(ctx, room) })
			wantErr(e.t, err, domain.ErrConflict)

			staff := domain.Staff{ID: newID[domain.StaffID](e), PersonID: newID[domain.PersonID](e), DisplayName: "Ghost", Kind: domain.StaffInstructor}
			err = e.write(func(ctx context.Context, tx store.Tx) error { return tx.Staff().Create(ctx, staff) })
			wantErr(e.t, err, domain.ErrConflict)

			r := e.room(e.location("Real"), "Real Room")
			r.LocationID = newID[domain.LocationID](e)
			err = e.write(func(ctx context.Context, tx store.Tx) error { return tx.Rooms().Update(ctx, r) })
			wantErr(e.t, err, domain.ErrConflict)
		},
		"DeleteReferencedIsConflict": func(e *env) {
			s := e.setting()
			e.class(s, time.Tuesday, domain.TimeOfDay{Hour: 18})

			loc, err := query(e, func(ctx context.Context, tx store.Tx) (domain.LocationID, error) {
				r, err := tx.Rooms().Get(ctx, s.room.ID)

				return r.LocationID, err
			})
			if err != nil {
				e.t.Fatal(err)
			}

			for name, del := range map[string]func(context.Context, store.Tx) error{
				"location":    func(ctx context.Context, tx store.Tx) error { return tx.Locations().Delete(ctx, loc) },
				"room":        func(ctx context.Context, tx store.Tx) error { return tx.Rooms().Delete(ctx, s.room.ID) },
				"staff":       func(ctx context.Context, tx store.Tx) error { return tx.Staff().Delete(ctx, s.instructor.ID) },
				"course type": func(ctx context.Context, tx store.Tx) error { return tx.CourseTypes().Delete(ctx, s.courseType.ID) },
				"term":        func(ctx context.Context, tx store.Tx) error { return tx.Terms().Delete(ctx, s.term.ID) },
			} {
				if err := e.write(del); !errorsIs(err, domain.ErrConflict) {
					e.t.Errorf("delete referenced %s: want ErrConflict, got %v", name, err)
				}
			}
		},
		"DeleteUsedByLessonIsConflict": func(e *env) {
			s := e.setting()
			e.lesson(s, t0, 5) // a one-off lesson, no class

			for name, del := range map[string]func(context.Context, store.Tx) error{
				"room":        func(ctx context.Context, tx store.Tx) error { return tx.Rooms().Delete(ctx, s.room.ID) },
				"staff":       func(ctx context.Context, tx store.Tx) error { return tx.Staff().Delete(ctx, s.instructor.ID) },
				"course type": func(ctx context.Context, tx store.Tx) error { return tx.CourseTypes().Delete(ctx, s.courseType.ID) },
			} {
				if err := e.write(del); !errorsIs(err, domain.ErrConflict) {
					e.t.Errorf("delete %s used by a lesson: want ErrConflict, got %v", name, err)
				}
			}
		},
	})
}
