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
				full.MemberNo = "M-0001"
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
		"ListByIDs": func(e *env) {
			byIDs := func(ids ...domain.PersonID) []domain.Person {
				return mustQuery(e, func(ctx context.Context, tx store.Tx) ([]domain.Person, error) {
					return tx.Persons().ListByIDs(ctx, ids)
				})
			}
			b := e.person("Ben", "Berg")
			a := e.person("Anna", "Berg")
			z := e.person("Zoe", "Arnold")
			e.person("Not", "Asked")

			// Ordered like List, each person once, missing IDs simply absent.
			wantSlice(e.t, byIDs(b.ID, newID[domain.PersonID](e), z.ID, a.ID, b.ID), []domain.Person{z, a, b})
			wantSlice(e.t, byIDs(), nil)
			wantSlice(e.t, byIDs(newID[domain.PersonID](e)), nil)
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
		"ListGuardianLinks": func(e *env) {
			links := func(dependents ...domain.PersonID) []domain.Guardian {
				return mustQuery(e, func(ctx context.Context, tx store.Tx) ([]domain.Guardian, error) {
					return tx.Persons().ListGuardianLinks(ctx, dependents)
				})
			}
			parent := e.person("Paula", "Parent")
			other := e.person("Otto", "Parent")
			kid1 := e.person("Alex", "Child")
			kid2 := e.person("Zara", "Child")
			kid3 := e.person("Nora", "Child")
			all := []domain.Guardian{
				{GuardianID: parent.ID, DependentID: kid1.ID},
				{GuardianID: other.ID, DependentID: kid1.ID},
				{GuardianID: parent.ID, DependentID: kid2.ID},
				{GuardianID: other.ID, DependentID: kid3.ID},
			}

			e.mustWrite(func(ctx context.Context, tx store.Tx) error {
				for _, g := range all {
					if err := tx.Persons().AddGuardian(ctx, g); err != nil {
						return err
					}
				}

				return nil
			})

			// Ordered by dependent and guardian ID, each link once; persons
			// without guardians and missing IDs simply add nothing.
			wantSlice(e.t, links(kid2.ID, parent.ID, kid1.ID, newID[domain.PersonID](e), kid2.ID), all[:3])
			wantSlice(e.t, links(), nil)
			wantSlice(e.t, links(parent.ID), nil)
		},
		"GuardianConflicts": func(e *env) {
			parent := e.person("Paula", "Parent")
			kid := e.person("Kim", "Child")
			link := domain.Guardian{GuardianID: parent.ID, DependentID: kid.ID}
			e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Persons().AddGuardian(ctx, link) })

			err := e.write(func(ctx context.Context, tx store.Tx) error { return tx.Persons().AddGuardian(ctx, link) })
			wantErr(e.t, err, domain.ErrDuplicate)

			missing := domain.Guardian{GuardianID: parent.ID, DependentID: newID[domain.PersonID](e)}
			err = e.write(func(ctx context.Context, tx store.Tx) error { return tx.Persons().AddGuardian(ctx, missing) })
			wantErr(e.t, err, domain.ErrConflict)
			wantNotErr(e.t, err, domain.ErrDuplicate)

			err = e.write(func(ctx context.Context, tx store.Tx) error { return tx.Persons().RemoveGuardian(ctx, missing) })
			wantErr(e.t, err, domain.ErrNotFound)

			err = e.read(func(ctx context.Context, tx store.Tx) error { return tx.Persons().RemoveGuardian(ctx, link) })
			wantErr(e.t, err, store.ErrReadOnly)

			// A link between persons that exist, refused only for the
			// transaction; the dependent stays without a second guardian.
			other := e.person("Otto", "Parent")
			second := domain.Guardian{GuardianID: other.ID, DependentID: kid.ID}
			err = e.read(func(ctx context.Context, tx store.Tx) error { return tx.Persons().AddGuardian(ctx, second) })
			wantErr(e.t, err, store.ErrReadOnly)

			guardians := mustQuery(e, func(ctx context.Context, tx store.Tx) ([]domain.Person, error) {
				return tx.Persons().ListGuardians(ctx, kid.ID)
			})
			wantSlice(e.t, guardians, []domain.Person{parent})
		},
		"Lock": func(e *env) {
			a, b := e.person("Ada", "Lock"), e.person("Bea", "Lock")

			e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Persons().Lock(ctx, b.ID, a.ID, b.ID) })

			err := e.write(func(ctx context.Context, tx store.Tx) error {
				return tx.Persons().Lock(ctx, a.ID, newID[domain.PersonID](e))
			})
			wantErr(e.t, err, domain.ErrNotFound)

			err = e.read(func(ctx context.Context, tx store.Tx) error { return tx.Persons().Lock(ctx, a.ID) })
			wantErr(e.t, err, store.ErrReadOnly)
		},
		"LockSerialisesChecks": func(e *env) { testPersonLock(e) },
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

			owner := e.person("Ow", "Owner")
			e.pass(e.passType("10-visit pass"), owner, 3, t0)
			deleteFails(owner.ID)

			survivor := e.person("Sue", "Survivor")
			e.merged(e.person("Sue", "Survivor"), survivor)
			deleteFails(survivor.ID)
		},
		"MemberNo": func(e *env) {
			a := e.person("Ada", "Member")
			a.MemberNo = "M-0001"
			e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Persons().Update(ctx, a) })

			byNo := func(no string) (domain.Person, error) {
				return query(e, func(ctx context.Context, tx store.Tx) (domain.Person, error) {
					return tx.Persons().GetByMemberNo(ctx, no)
				})
			}

			got, err := byNo("M-0001")
			if err != nil {
				e.t.Fatalf("GetByMemberNo: %v", err)
			}

			wantEqual(e.t, got, a)

			// Exactly: neither case nor an empty number matches.
			for _, no := range []string{"m-0001", "", "M-0002"} {
				_, err := byNo(no)
				wantErr(e.t, err, domain.ErrNotFound)
			}

			// Unique, on create and update.
			b := domain.Person{ID: newID[domain.PersonID](e), GivenName: "Bea", FamilyName: "Member", MemberNo: "M-0001"}
			err = e.write(func(ctx context.Context, tx store.Tx) error { return tx.Persons().Create(ctx, b) })
			wantErr(e.t, err, domain.ErrDuplicate)

			b.MemberNo = "M-0002"
			e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Persons().Create(ctx, b) })

			b.MemberNo = "M-0001"
			err = e.write(func(ctx context.Context, tx store.Tx) error { return tx.Persons().Update(ctx, b) })
			wantErr(e.t, err, domain.ErrDuplicate)

			// The number moves once its holder gives it up, in one transaction.
			a.MemberNo, b.MemberNo = "", "M-0001"
			e.mustWrite(func(ctx context.Context, tx store.Tx) error {
				return errors.Join(tx.Persons().Update(ctx, a), tx.Persons().Update(ctx, b))
			})

			got, err = byNo("M-0001")
			if err != nil {
				e.t.Fatalf("GetByMemberNo: %v", err)
			}

			wantEqual(e.t, got, b)

			// The number b gave up, and that of a deleted person, are free.
			_, err = byNo("M-0002")
			wantErr(e.t, err, domain.ErrNotFound)

			c := domain.Person{ID: newID[domain.PersonID](e), GivenName: "Cy", FamilyName: "Member", MemberNo: "M-0002"}
			e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Persons().Create(ctx, c) })
			e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Persons().Delete(ctx, c.ID) })

			_, err = byNo("M-0002")
			wantErr(e.t, err, domain.ErrNotFound)

			c.ID = newID[domain.PersonID](e)
			e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Persons().Create(ctx, c) })
		},
		"Merged": func(e *env) {
			survivor := e.person("Sue", "Survivor")
			other := e.person("Ole", "Other")
			tomb2 := e.merged(e.person("Sue", "B-Survivor"), survivor)
			tomb1 := e.merged(e.person("Sue", "A-Survivor"), survivor)

			// Left out of List, but found by Get, ListByIDs and ListMerged.
			wantSlice(e.t, list(e), []domain.Person{other, survivor})

			got := mustQuery(e, func(ctx context.Context, tx store.Tx) (domain.Person, error) {
				return tx.Persons().Get(ctx, tomb1.ID)
			})
			wantEqual(e.t, got, tomb1)

			byIDs := mustQuery(e, func(ctx context.Context, tx store.Tx) ([]domain.Person, error) {
				return tx.Persons().ListByIDs(ctx, []domain.PersonID{tomb2.ID, survivor.ID})
			})
			wantSlice(e.t, byIDs, []domain.Person{tomb2, survivor})

			merged := func(into domain.PersonID) []domain.Person {
				return mustQuery(e, func(ctx context.Context, tx store.Tx) ([]domain.Person, error) {
					return tx.Persons().ListMerged(ctx, into)
				})
			}
			wantSlice(e.t, merged(survivor.ID), []domain.Person{tomb1, tomb2})
			wantSlice(e.t, merged(other.ID), nil)

			// Merged into a person that does not exist.
			ghost := domain.Person{
				ID: newID[domain.PersonID](e), GivenName: "Gus", FamilyName: "Ghost", MergedInto: newID[domain.PersonID](e),
			}
			err := e.write(func(ctx context.Context, tx store.Tx) error { return tx.Persons().Create(ctx, ghost) })
			wantErr(e.t, err, domain.ErrConflict)
			wantNotErr(e.t, err, domain.ErrDuplicate)

			// A tombstone moves on to another survivor.
			tomb1.MergedInto = other.ID
			e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Persons().Update(ctx, tomb1) })
			wantSlice(e.t, merged(other.ID), []domain.Person{tomb1})
		},
	})
}

// merged turns p into a tombstone merged into survivor and returns it.
func (e *env) merged(p, survivor domain.Person) domain.Person {
	e.t.Helper()

	p.MergedInto = survivor.ID
	valid(e.t, p)
	e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Persons().Update(ctx, p) })

	return p
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
				full.ConfirmedAt = t0.Add(-time.Hour)
				full.DisabledAt = t0
				full.Locale = "de"
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
		"MaxVisibleDays": func(e *env) {
			maxDays := func() int {
				n, err := query(e, func(ctx context.Context, tx store.Tx) (int, error) {
					return tx.Accounts().MaxVisibleDays(ctx)
				})
				if err != nil {
					e.t.Fatalf("MaxVisibleDays: %v", err)
				}

				return n
			}

			wantEqual(e.t, maxDays(), 0) // no accounts

			e.account(e.person("Ada", "Account"), "ada@example.org") // no override
			wantEqual(e.t, maxDays(), 0)

			for i, days := range []int{30, 45, 7} {
				a := e.account(e.person("Bea", fmt.Sprint("Other", i)), fmt.Sprintf("bea%d@example.org", i))
				a.VisibleDays = days
				e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Accounts().Update(ctx, a) })
			}

			wantEqual(e.t, maxDays(), 45)
		},
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
			wantErr(e.t, err, domain.ErrDuplicate)
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
			wantErr(e.t, err, domain.ErrDuplicate)

			// Changing only the case of the own email is fine.
			b.Email = "BEA@example.org"
			e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Accounts().Update(ctx, b) })
		},
		"GetByPerson": func(e *env) {
			e.account(e.person("Bea", "Other"), "bea@example.org")
			a := e.account(e.person("Ada", "Account"), "ada@example.org")

			got, err := query(e, func(ctx context.Context, tx store.Tx) (domain.Account, error) {
				return tx.Accounts().GetByPerson(ctx, a.PersonID)
			})
			if err != nil {
				e.t.Fatalf("GetByPerson: %v", err)
			}

			wantEqual(e.t, got, a)

			none := e.person("Nia", "None")
			_, err = query(e, func(ctx context.Context, tx store.Tx) (domain.Account, error) {
				return tx.Accounts().GetByPerson(ctx, none.ID)
			})
			wantErr(e.t, err, domain.ErrNotFound)
		},
		"ListByPersons": func(e *env) {
			byPersons := func(ids ...domain.PersonID) []domain.Account {
				return mustQuery(e, func(ctx context.Context, tx store.Tx) ([]domain.Account, error) {
					return tx.Accounts().ListByPersons(ctx, ids)
				})
			}
			c := e.account(e.person("C", "C"), "carl@example.org")
			b := e.account(e.person("B", "B"), "Bea@example.org")
			a := e.account(e.person("A", "A"), "anna@example.org")
			e.account(e.person("D", "D"), "dora@example.org")
			none := e.person("N", "N")

			// Ordered like List, each account once; persons without an
			// account and missing IDs simply absent.
			wantSlice(e.t, byPersons(c.PersonID, none.ID, newID[domain.PersonID](e), a.PersonID, b.PersonID, c.PersonID),
				[]domain.Account{a, b, c})
			wantSlice(e.t, byPersons(), nil)
			wantSlice(e.t, byPersons(none.ID), nil)
		},
		"OneAccountPerPerson": func(e *env) {
			a := e.account(e.person("Ada", "Account"), "ada@example.org")
			second := domain.Account{ID: newID[domain.AccountID](e), PersonID: a.PersonID, Email: "ada2@example.org", Role: domain.RoleMember}

			err := e.write(func(ctx context.Context, tx store.Tx) error { return tx.Accounts().Create(ctx, second) })
			wantErr(e.t, err, domain.ErrDuplicate)
		},
		"MissingPersonIsConflict": func(e *env) {
			a := domain.Account{ID: newID[domain.AccountID](e), PersonID: newID[domain.PersonID](e), Email: "x@example.org", Role: domain.RoleMember}

			err := e.write(func(ctx context.Context, tx store.Tx) error { return tx.Accounts().Create(ctx, a) })
			wantErr(e.t, err, domain.ErrConflict)
			wantNotErr(e.t, err, domain.ErrDuplicate)
		},
		"ListOrder": func(e *env) {
			c := e.account(e.person("C", "C"), "carl@example.org")
			b := e.account(e.person("B", "B"), "Bea@example.org")
			a := e.account(e.person("A", "A"), "anna@example.org")

			got := mustQuery(e, func(ctx context.Context, tx store.Tx) ([]domain.Account, error) { return tx.Accounts().List(ctx) })
			wantSlice(e.t, got, []domain.Account{a, b, c})
		},
		"ListByIDs": func(e *env) {
			byIDs := func(ids ...domain.AccountID) []domain.Account {
				return mustQuery(e, func(ctx context.Context, tx store.Tx) ([]domain.Account, error) {
					return tx.Accounts().ListByIDs(ctx, ids)
				})
			}
			c := e.account(e.person("C", "C"), "carl@example.org")
			b := e.account(e.person("B", "B"), "Bea@example.org")
			a := e.account(e.person("A", "A"), "anna@example.org")
			e.account(e.person("D", "D"), "dora@example.org")

			// Ordered like List, each account once, missing IDs simply absent.
			wantSlice(e.t, byIDs(c.ID, newID[domain.AccountID](e), a.ID, b.ID, c.ID), []domain.Account{a, b, c})
			wantSlice(e.t, byIDs(), nil)
			wantSlice(e.t, byIDs(newID[domain.AccountID](e)), nil)
		},
	})
}

// testPersonLock lets concurrent transactions lock two persons, in either
// order, read a counter kept in one of them and write it back increased.
// Without the lock, or with a lock order that deadlocks, increments would
// get lost or transactions fail.
func testPersonLock(e *env) {
	const attempts = 12

	a, b := e.person("Ada", "Counter"), e.person("Bea", "Counter")

	var (
		wg   sync.WaitGroup
		errs = make([]error, attempts)
	)

	for i := range attempts {
		wg.Go(func() {
			errs[i] = e.write(func(ctx context.Context, tx store.Tx) error {
				ids := []domain.PersonID{a.ID, b.ID}
				if i%2 == 1 {
					ids = []domain.PersonID{b.ID, a.ID}
				}

				if err := tx.Persons().Lock(ctx, ids...); err != nil {
					return err
				}

				p, err := tx.Persons().Get(ctx, a.ID)
				if err != nil {
					return err
				}

				p.Notes += "x"

				return tx.Persons().Update(ctx, p)
			})
		})
	}

	wg.Wait()

	for _, err := range errs {
		if err != nil {
			e.t.Fatalf("unexpected error: %v", err)
		}
	}

	got := mustQuery(e, func(ctx context.Context, tx store.Tx) (domain.Person, error) { return tx.Persons().Get(ctx, a.ID) })
	wantEqual(e.t, len(got.Notes), attempts)
}
