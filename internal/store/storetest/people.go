package storetest

import (
	"context"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/store"
)

func testPersons(t *testing.T, newStore func(*testing.T) store.Store) {
	t.Helper()

	t.Run("CRUD", func(t *testing.T) {
		testCRUD(t, newStore, crud[domain.PersonID, domain.Person]{
			id: func(p domain.Person) domain.PersonID { return p.ID },
			get: func(ctx context.Context, tx store.Tx, id domain.PersonID) (domain.Person, error) {
				return tx.Persons().Get(ctx, id)
			},
			create: func(ctx context.Context, tx store.Tx, p domain.Person) error { return tx.Persons().Create(ctx, p) },
			update: func(ctx context.Context, tx store.Tx, p domain.Person) error { return tx.Persons().Update(ctx, p) },
			remove: func(ctx context.Context, tx store.Tx, id domain.PersonID) error { return tx.Persons().Delete(ctx, id) },
			fixtures: func(e *env) (domain.Person, domain.Person) {
				minimal := domain.Person{ID: newID[domain.PersonID](e), GivenName: "Mira", FamilyName: "Minimal"}
				full := minimal
				full.Email = "mira@example.org"
				full.Phone = "+49 000 0000000"
				full.BirthDate = domain.Date{Year: 2015, Month: time.June, Day: 30}
				full.Notes = "Prefers the late class."
				valid(e.t, minimal)
				valid(e.t, full)

				return minimal, full
			},
		})
	})

	list := func(e *env) []domain.Person {
		return mustQuery(e, func(ctx context.Context, tx store.Tx) ([]domain.Person, error) { return tx.Persons().List(ctx) })
	}

	cases(t, newStore, map[string]func(e *env){
		"ListEmpty": func(e *env) { wantSlice(e.t, list(e), nil) },
		"ListOrder": func(e *env) {
			// Names compare byte-wise: upper case sorts before lower case.
			b2 := e.person("Ben", "Berg")
			a := e.person("Anna", "Berg")
			lower := e.person("adam", "berg")
			z := e.person("Zoe", "Arnold")
			b1 := e.person("Ben", "Berg") // same name: the ID decides
			wantSlice(e.t, list(e), []domain.Person{z, a, b2, b1, lower})
		},
		"Guardians": func(e *env) {
			parent := e.person("Paula", "Parent")
			other := e.person("Otto", "Parent")
			kid2 := e.person("Zara", "Child")
			kid1 := e.person("Alex", "Child")

			e.mustWrite(func(ctx context.Context, tx store.Tx) error {
				for _, g := range []domain.Guardian{
					{GuardianID: parent.ID, DependentID: kid2.ID},
					{GuardianID: parent.ID, DependentID: kid1.ID},
					{GuardianID: other.ID, DependentID: kid1.ID},
				} {
					if err := tx.Persons().AddGuardian(ctx, g); err != nil {
						return err
					}
				}

				return nil
			})

			deps := mustQuery(e, func(ctx context.Context, tx store.Tx) ([]domain.Person, error) {
				return tx.Persons().ListDependents(ctx, parent.ID)
			})
			wantSlice(e.t, deps, []domain.Person{kid1, kid2})

			guardians := mustQuery(e, func(ctx context.Context, tx store.Tx) ([]domain.Person, error) {
				return tx.Persons().ListGuardians(ctx, kid1.ID)
			})
			wantSlice(e.t, guardians, []domain.Person{other, parent})

			none := mustQuery(e, func(ctx context.Context, tx store.Tx) ([]domain.Person, error) {
				return tx.Persons().ListDependents(ctx, kid1.ID)
			})
			wantSlice(e.t, none, nil)

			e.mustWrite(func(ctx context.Context, tx store.Tx) error {
				return tx.Persons().RemoveGuardian(ctx, domain.Guardian{GuardianID: parent.ID, DependentID: kid2.ID})
			})

			deps = mustQuery(e, func(ctx context.Context, tx store.Tx) ([]domain.Person, error) {
				return tx.Persons().ListDependents(ctx, parent.ID)
			})
			wantSlice(e.t, deps, []domain.Person{kid1})
		},
		"GuardianConflicts": func(e *env) {
			parent := e.person("Paula", "Parent")
			kid := e.person("Kim", "Child")
			link := domain.Guardian{GuardianID: parent.ID, DependentID: kid.ID}
			e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Persons().AddGuardian(ctx, link) })

			err := e.write(func(ctx context.Context, tx store.Tx) error { return tx.Persons().AddGuardian(ctx, link) })
			wantErr(e.t, err, domain.ErrConflict)

			missing := domain.Guardian{GuardianID: parent.ID, DependentID: newID[domain.PersonID](e)}
			err = e.write(func(ctx context.Context, tx store.Tx) error { return tx.Persons().AddGuardian(ctx, missing) })
			wantErr(e.t, err, domain.ErrConflict)

			err = e.write(func(ctx context.Context, tx store.Tx) error { return tx.Persons().RemoveGuardian(ctx, missing) })
			wantErr(e.t, err, domain.ErrNotFound)

			err = e.read(func(ctx context.Context, tx store.Tx) error { return tx.Persons().RemoveGuardian(ctx, link) })
			wantErr(e.t, err, store.ErrReadOnly)
		},
		"DeleteReferencedIsConflict": func(e *env) {
			deleteFails := func(id domain.PersonID) {
				e.t.Helper()

				err := e.write(func(ctx context.Context, tx store.Tx) error { return tx.Persons().Delete(ctx, id) })
				wantErr(e.t, err, domain.ErrConflict)
			}

			parent, kid := e.person("Paula", "Parent"), e.person("Kim", "Child")
			e.mustWrite(func(ctx context.Context, tx store.Tx) error {
				return tx.Persons().AddGuardian(ctx, domain.Guardian{GuardianID: parent.ID, DependentID: kid.ID})
			})
			deleteFails(parent.ID)
			deleteFails(kid.ID)

			deleteFails(e.account(e.person("Ada", "Account"), "ada@example.org").PersonID)

			withStaff := e.person("Sam", "Staff")
			e.mustWrite(func(ctx context.Context, tx store.Tx) error {
				return tx.Staff().Create(ctx, domain.Staff{
					ID: newID[domain.StaffID](e), PersonID: withStaff.ID, DisplayName: "Sam", Kind: domain.StaffInstructor,
				})
			})
			deleteFails(withStaff.ID)

			s := e.setting()
			member := e.person("Mo", "Member")
			c := e.class(s, time.Monday, domain.TimeOfDay{Hour: 17})
			e.mustWrite(func(ctx context.Context, tx store.Tx) error {
				return tx.Classes().AddMember(ctx, domain.ClassMember{ClassID: c.ID, PersonID: member.ID})
			})
			deleteFails(member.ID)

			booker := e.person("Bo", "Booker")
			e.booking(e.lesson(s, t0, 5), booker, t0.Add(-time.Hour))
			deleteFails(booker.ID)
		},
	})
}

func testAccounts(t *testing.T, newStore func(*testing.T) store.Store) {
	t.Helper()

	t.Run("CRUD", func(t *testing.T) {
		testCRUD(t, newStore, crud[domain.AccountID, domain.Account]{
			id: func(a domain.Account) domain.AccountID { return a.ID },
			get: func(ctx context.Context, tx store.Tx, id domain.AccountID) (domain.Account, error) {
				return tx.Accounts().Get(ctx, id)
			},
			create: func(ctx context.Context, tx store.Tx, a domain.Account) error { return tx.Accounts().Create(ctx, a) },
			update: func(ctx context.Context, tx store.Tx, a domain.Account) error { return tx.Accounts().Update(ctx, a) },
			fixtures: func(e *env) (domain.Account, domain.Account) {
				p := e.person("Ada", "Account")
				minimal := domain.Account{ID: newID[domain.AccountID](e), PersonID: p.ID, Email: "ada@example.org", Role: domain.RoleMember}
				full := minimal
				full.Email = "Ada.Account@Example.org"
				full.Role = domain.RoleOffice
				full.VisibleDays = 30
				full.DisabledAt = t0
				valid(e.t, minimal)
				valid(e.t, full)

				return minimal, full
			},
		})
	})

	getByEmail := func(e *env, email string) (domain.Account, error) {
		return query(e, func(ctx context.Context, tx store.Tx) (domain.Account, error) {
			return tx.Accounts().GetByEmail(ctx, email)
		})
	}

	cases(t, newStore, map[string]func(e *env){
		"EmailIsCaseInsensitive": func(e *env) {
			a := e.account(e.person("Ada", "Account"), "Ada@Example.org")

			got, err := getByEmail(e, "ADA@example.ORG")
			if err != nil {
				e.t.Fatalf("GetByEmail: %v", err)
			}

			wantEqual(e.t, got, a) // the stored spelling is kept

			_, err = getByEmail(e, "nobody@example.org")
			wantErr(e.t, err, domain.ErrNotFound)

			other := domain.Account{
				ID: newID[domain.AccountID](e), PersonID: e.person("Bea", "Other").ID,
				Email: "ada@EXAMPLE.org", Role: domain.RoleMember,
			}
			err = e.write(func(ctx context.Context, tx store.Tx) error { return tx.Accounts().Create(ctx, other) })
			wantErr(e.t, err, domain.ErrConflict)
		},
		"OnlyASCIICaseIsFolded": func(e *env) {
			// Like PostgreSQL's lower() under collation "C": "Ä" and "ä" differ.
			upper := e.account(e.person("Ada", "Upper"), "Äda@example.org")
			lower := e.account(e.person("Ada", "Lower"), "äda@EXAMPLE.org")

			got, err := getByEmail(e, "äda@example.org")
			if err != nil {
				e.t.Fatalf("GetByEmail: %v", err)
			}

			wantEqual(e.t, got, lower)

			got, err = getByEmail(e, "Äda@Example.org")
			if err != nil {
				e.t.Fatalf("GetByEmail: %v", err)
			}

			wantEqual(e.t, got, upper)
		},
		"UpdateToTakenEmailIsConflict": func(e *env) {
			e.account(e.person("Ada", "Account"), "ada@example.org")
			b := e.account(e.person("Bea", "Account"), "bea@example.org")
			b.Email = "ADA@example.org"

			err := e.write(func(ctx context.Context, tx store.Tx) error { return tx.Accounts().Update(ctx, b) })
			wantErr(e.t, err, domain.ErrConflict)

			// Changing only the case of the own email is fine.
			b.Email = "BEA@example.org"
			e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Accounts().Update(ctx, b) })
		},
		"OneAccountPerPerson": func(e *env) {
			a := e.account(e.person("Ada", "Account"), "ada@example.org")
			second := domain.Account{ID: newID[domain.AccountID](e), PersonID: a.PersonID, Email: "ada2@example.org", Role: domain.RoleMember}

			err := e.write(func(ctx context.Context, tx store.Tx) error { return tx.Accounts().Create(ctx, second) })
			wantErr(e.t, err, domain.ErrConflict)
		},
		"MissingPersonIsConflict": func(e *env) {
			a := domain.Account{ID: newID[domain.AccountID](e), PersonID: newID[domain.PersonID](e), Email: "x@example.org", Role: domain.RoleMember}

			err := e.write(func(ctx context.Context, tx store.Tx) error { return tx.Accounts().Create(ctx, a) })
			wantErr(e.t, err, domain.ErrConflict)
		},
		"ListOrder": func(e *env) {
			c := e.account(e.person("C", "C"), "carl@example.org")
			b := e.account(e.person("B", "B"), "Bea@example.org")
			a := e.account(e.person("A", "A"), "anna@example.org")

			got := mustQuery(e, func(ctx context.Context, tx store.Tx) ([]domain.Account, error) { return tx.Accounts().List(ctx) })
			wantSlice(e.t, got, []domain.Account{a, b, c})
		},
	})
}
