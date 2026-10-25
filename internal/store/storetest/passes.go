package storetest

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// oct1 is a school date for the validity of passes.
var oct1 = domain.Date{Year: 2026, Month: time.October, Day: 1}

// newPassType returns an active 10-visit pass type named name without
// creating it.
func (e *env) newPassType(name string) domain.PassType {
	e.t.Helper()

	pt := domain.PassType{
		ID: newID[domain.PassTypeID](e), Name: name, Kind: domain.PassVisits, Visits: 10, ValidityDays: 180,
		Price: 12000, VATRate: 1900, Active: true, CreatedAt: t0,
	}
	valid(e.t, pt)

	return pt
}

func (e *env) passType(name string) domain.PassType {
	e.t.Helper()

	pt := e.newPassType(name)
	e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.PassTypes().Create(ctx, pt) })

	return pt
}

// newPass returns an active visit pass of p with the given visits left,
// assigned at soldAt, without creating it.
func (e *env) newPass(pt domain.PassType, p domain.Person, remaining int, soldAt time.Time) domain.Pass {
	e.t.Helper()

	pass := domain.Pass{
		ID: newID[domain.PassID](e), PassTypeID: pt.ID, PersonID: p.ID, Kind: domain.PassVisits,
		VisitsTotal: max(remaining, 1), VisitsRemaining: remaining, ValidFrom: oct1, Status: domain.PassActive,
		Origin: domain.PassAssigned, SoldAt: soldAt,
	}
	valid(e.t, pass)

	return pass
}

func (e *env) pass(pt domain.PassType, p domain.Person, remaining int, soldAt time.Time) domain.Pass {
	e.t.Helper()

	pass := e.newPass(pt, p, remaining, soldAt)
	e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Passes().Create(ctx, pass) })

	return pass
}

func (e *env) redeem(pass domain.Pass, b domain.Booking, at time.Time) domain.PassRedemption {
	e.t.Helper()

	r := domain.PassRedemption{PassID: pass.ID, BookingID: b.ID, At: at}
	valid(e.t, r)
	e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Redemptions().Create(ctx, r) })

	return r
}

func (e *env) getPass(id domain.PassID) domain.Pass {
	e.t.Helper()

	return mustQuery(e, func(ctx context.Context, tx store.Tx) (domain.Pass, error) { return tx.Passes().Get(ctx, id) })
}

func testPassTypes(t *testing.T, newStore func(*testing.T) store.Store) {
	t.Helper()

	t.Run("CRUD", func(t *testing.T) {
		testCRUD(t, newStore, crud[domain.PassTypeID, domain.PassType]{
			id: func(pt domain.PassType) domain.PassTypeID { return pt.ID },
			get: func(ctx context.Context, tx store.Tx, id domain.PassTypeID) (domain.PassType, error) {
				return tx.PassTypes().Get(ctx, id)
			},
			create: func(ctx context.Context, tx store.Tx, pt domain.PassType) error {
				return tx.PassTypes().Create(ctx, pt)
			},
			update: func(ctx context.Context, tx store.Tx, pt domain.PassType) error {
				return tx.PassTypes().Update(ctx, pt)
			},
			remove: func(ctx context.Context, tx store.Tx, id domain.PassTypeID) error {
				return tx.PassTypes().Delete(ctx, id)
			},
			fixtures: func(e *env) (domain.PassType, domain.PassType) {
				// A free time pass without VAT, retired; and a visit pass.
				minimal := domain.PassType{
					ID: newID[domain.PassTypeID](e), Name: "Trial month", Kind: domain.PassTime, ValidityDays: 30,
					CreatedAt: t0,
				}
				full := e.newPassType("10-visit pass")
				full.ID = minimal.ID
				valid(e.t, minimal)

				return minimal, full
			},
		})
	})

	cases(t, newStore, map[string]func(e *env){
		"NamesAreUniqueIgnoringCase": func(e *env) {
			first := e.passType("10-Visit Pass")
			other := e.passType("Single visit")

			dup := e.newPassType("10-visit pass")
			err := e.write(func(ctx context.Context, tx store.Tx) error { return tx.PassTypes().Create(ctx, dup) })
			wantErr(e.t, err, domain.ErrDuplicate)

			other.Name = "10-VISIT PASS"
			err = e.write(func(ctx context.Context, tx store.Tx) error { return tx.PassTypes().Update(ctx, other) })
			wantErr(e.t, err, domain.ErrDuplicate)

			// Renaming a type to its own name in another case is fine.
			first.Name = "10-visit pass"
			e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.PassTypes().Update(ctx, first) })

			// Only ASCII letters fold, like the catalog names.
			e.passType("Ärztekarte")
			e.passType("ärztekarte")
		},
		"ListOrder": func(e *env) {
			b := e.passType("b pass")
			a2 := e.passType("a pass 2")
			upper := e.passType("Z pass") // byte order: upper case first
			a := e.passType("a pass")

			got := mustQuery(e, func(ctx context.Context, tx store.Tx) ([]domain.PassType, error) {
				return tx.PassTypes().List(ctx)
			})
			wantSlice(e.t, got, []domain.PassType{upper, a, a2, b})
		},
		"DeleteSoldIsConflict": func(e *env) {
			pt := e.passType("10-visit pass")
			e.pass(pt, e.person("Ada", "Owner"), 3, t0)

			err := e.write(func(ctx context.Context, tx store.Tx) error { return tx.PassTypes().Delete(ctx, pt.ID) })
			wantErr(e.t, err, domain.ErrConflict)
		},
	})
}

func testPasses(t *testing.T, newStore func(*testing.T) store.Store) {
	t.Helper()

	t.Run("CRUD", func(t *testing.T) {
		testCRUD(t, newStore, crud[domain.PassID, domain.Pass]{
			id: func(p domain.Pass) domain.PassID { return p.ID },
			get: func(ctx context.Context, tx store.Tx, id domain.PassID) (domain.Pass, error) {
				return tx.Passes().Get(ctx, id)
			},
			create: func(ctx context.Context, tx store.Tx, p domain.Pass) error { return tx.Passes().Create(ctx, p) },
			update: func(ctx context.Context, tx store.Tx, p domain.Pass) error { return tx.Passes().Update(ctx, p) },
			fixtures: func(e *env) (domain.Pass, domain.Pass) {
				owner, payer := e.person("Ada", "Owner"), e.person("Paul", "Payer")
				seller := e.account(e.person("Olga", "Office"), "olga@example.org")

				// A used-up visit pass without expiry (zero visits left
				// round-trip as a number), assigned by nobody known.
				minimal := e.newPass(e.passType("10-visit pass"), owner, 0, t0)
				// A sold, voided time pass.
				full := minimal
				full.PassTypeID = e.passType("30-day pass").ID
				full.Kind, full.VisitsTotal, full.VisitsRemaining = domain.PassTime, 0, 0
				full.ValidUntil = oct1.AddDays(29)
				full.Status, full.VoidedAt = domain.PassVoided, t0.Add(time.Hour)
				full.Origin, full.SoldBy, full.PayerID = domain.PassSold, seller.ID, payer.ID
				full.ReceiptID = newID[domain.ReceiptID](e) // no receipts yet (Phase 6 B)
				full.PricePaid, full.Note = 9900, "Paid in cash"
				valid(e.t, full)

				return minimal, full
			},
		})
	})

	cases(t, newStore, map[string]func(e *env){
		"CreateWithMissingReferenceIsConflict": func(e *env) {
			pt := e.passType("10-visit pass")
			owner := e.person("Ada", "Owner")

			for name, mutate := range map[string]func(p *domain.Pass){
				"type":   func(p *domain.Pass) { p.PassTypeID = newID[domain.PassTypeID](e) },
				"owner":  func(p *domain.Pass) { p.PersonID = newID[domain.PersonID](e) },
				"payer":  func(p *domain.Pass) { p.Origin, p.PayerID = domain.PassSold, newID[domain.PersonID](e) },
				"seller": func(p *domain.Pass) { p.SoldBy = newID[domain.AccountID](e) },
			} {
				p := e.newPass(pt, owner, 3, t0)
				mutate(&p)
				valid(e.t, p)

				err := e.write(func(ctx context.Context, tx store.Tx) error { return tx.Passes().Create(ctx, p) })
				if !errorsIs(err, domain.ErrConflict) {
					e.t.Errorf("%s: want ErrConflict, got %v", name, err)
				}
			}
		},
		"ListByPersonsAndIDs": func(e *env) {
			pt := e.passType("10-visit pass")
			ada, bob, cy := e.person("Ada", "Owner"), e.person("Bob", "Owner"), e.person("Cy", "Owner")

			later := e.pass(pt, ada, 3, t0.Add(time.Hour))
			// A fraction of a microsecond is cut off like in PostgreSQL.
			earlier := e.newPass(pt, ada, 5, t0.Add(500*time.Nanosecond))
			e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Passes().Create(ctx, earlier) })
			earlier.SoldAt = t0
			sameTime := e.pass(pt, ada, 1, t0)
			bobs := e.pass(pt, bob, 2, t0)

			byPersons := func(persons ...domain.PersonID) []domain.Pass {
				return mustQuery(e, func(ctx context.Context, tx store.Tx) ([]domain.Pass, error) {
					return tx.Passes().ListByPersons(ctx, persons)
				})
			}

			// By sale time, then ID (the sequence's IDs ascend).
			wantSlice(e.t, byPersons(ada.ID), []domain.Pass{earlier, sameTime, later})
			wantSlice(e.t, byPersons(ada.ID, bob.ID), []domain.Pass{earlier, sameTime, bobs, later})
			wantSlice(e.t, byPersons(cy.ID), nil)
			wantSlice(e.t, byPersons(), nil)

			got := mustQuery(e, func(ctx context.Context, tx store.Tx) ([]domain.Pass, error) {
				return tx.Passes().ListByIDs(ctx, []domain.PassID{bobs.ID, later.ID, earlier.ID, later.ID, newID[domain.PassID](e)})
			})
			wantSlice(e.t, got, []domain.Pass{later, earlier, bobs})
		},
		"LockByPersons": func(e *env) {
			pt := e.passType("10-visit pass")
			ada, bob := e.person("Ada", "Owner"), e.person("Bob", "Owner")
			first := e.pass(pt, ada, 3, t0.Add(time.Hour))
			second := e.pass(pt, bob, 2, t0)
			e.pass(pt, e.person("Cy", "Owner"), 1, t0)

			err := e.read(func(ctx context.Context, tx store.Tx) error {
				_, err := tx.Passes().LockByPersons(ctx, ada.ID)

				return err
			})
			wantErr(e.t, err, store.ErrReadOnly)

			// The passes of the persons, by ID.
			e.mustWrite(func(ctx context.Context, tx store.Tx) error {
				got, err := tx.Passes().LockByPersons(ctx, bob.ID, ada.ID, bob.ID)
				if err != nil {
					return err
				}

				wantSlice(e.t, got, []domain.Pass{first, second})

				none, err := tx.Passes().LockByPersons(ctx, newID[domain.PersonID](e))
				wantSlice(e.t, none, nil)

				return err
			})
		},
		"LockSerialisesRedemptions": func(e *env) {
			// Every redemption runs under the lock of the owner's passes, so
			// of many concurrent redemptions of the last visits exactly as
			// many succeed as there are visits.
			const (
				n      = 8
				visits = 2
			)

			s := e.setting()
			owner := e.person("Ada", "Owner")
			pass := e.pass(e.passType("10-visit pass"), owner, visits, t0)

			bookings := make([]domain.Booking, n)
			for i := range bookings {
				bookings[i] = e.booking(e.lesson(s, t0.Add(time.Duration(i)*time.Hour), 5), owner, t0)
			}

			var (
				wg   sync.WaitGroup
				mu   sync.Mutex
				errs []error
			)

			for _, b := range bookings {
				wg.Go(func() {
					err := e.st.InTx(e.t.Context(), store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
						passes, err := tx.Passes().LockByPersons(ctx, owner.ID)
						if err != nil || len(passes) != 1 || passes[0].VisitsRemaining == 0 {
							return err // nothing left: the booking stays unpaid
						}

						// Widen the window: without the lock, the others
						// would read the same visit meanwhile.
						time.Sleep(10 * time.Millisecond)

						p := passes[0]
						p.VisitsRemaining--

						if err := tx.Passes().Update(ctx, p); err != nil {
							return err
						}

						return tx.Redemptions().Create(ctx, domain.PassRedemption{PassID: p.ID, BookingID: b.ID, At: t0})
					})

					mu.Lock()
					defer mu.Unlock()

					errs = append(errs, err)
				})
			}

			wg.Wait()

			for _, err := range errs {
				if err != nil {
					e.t.Fatalf("redemption: %v", err)
				}
			}

			wantEqual(e.t, e.getPass(pass.ID).VisitsRemaining, 0)

			got := mustQuery(e, func(ctx context.Context, tx store.Tx) ([]domain.PassRedemption, error) {
				return tx.Redemptions().ListByPasses(ctx, []domain.PassID{pass.ID}, 0)
			})
			wantEqual(e.t, len(got), visits)
		},
		"OwnersAndPayersStay": func(e *env) {
			pt := e.passType("10-visit pass")
			owner, payer := e.person("Ada", "Owner"), e.person("Paul", "Payer")
			p := e.newPass(pt, owner, 3, t0)
			p.Origin, p.PayerID, p.PricePaid = domain.PassSold, payer.ID, 12000
			e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Passes().Create(ctx, p) })

			for _, id := range []domain.PersonID{owner.ID, payer.ID} {
				err := e.write(func(ctx context.Context, tx store.Tx) error { return tx.Persons().Delete(ctx, id) })
				wantErr(e.t, err, domain.ErrConflict)
			}
		},
	})
}

func testRedemptions(t *testing.T, newStore func(*testing.T) store.Store) {
	t.Helper()

	// fixture is a pass of Ada and two of her bookings.
	type fixture struct {
		setting setting
		pass    domain.Pass
		b1, b2  domain.Booking
	}

	setup := func(e *env) fixture {
		e.t.Helper()

		s := e.setting()
		ada := e.person("Ada", "Owner")

		return fixture{
			setting: s,
			pass:    e.pass(e.passType("10-visit pass"), ada, 5, t0),
			b1:      e.booking(e.lesson(s, t0, 5), ada, t0),
			b2:      e.booking(e.lesson(s, t0.Add(time.Hour), 5), ada, t0),
		}
	}
	byBooking := func(e *env, id domain.BookingID) (domain.PassRedemption, error) {
		return query(e, func(ctx context.Context, tx store.Tx) (domain.PassRedemption, error) {
			return tx.Redemptions().GetByBooking(ctx, id)
		})
	}

	cases(t, newStore, map[string]func(e *env){
		"CreateGetDelete": func(e *env) {
			f := setup(e)

			_, err := byBooking(e, f.b1.ID)
			wantErr(e.t, err, domain.ErrNotFound)

			// A fraction of a microsecond is cut off like in PostgreSQL.
			r := domain.PassRedemption{PassID: f.pass.ID, BookingID: f.b1.ID, At: t0.Add(500 * time.Nanosecond)}
			e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Redemptions().Create(ctx, r) })
			r.At = t0

			got, err := byBooking(e, f.b1.ID)
			if err != nil {
				e.t.Fatal(err)
			}

			wantEqual(e.t, got, r)

			e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Redemptions().Delete(ctx, f.b1.ID) })

			_, err = byBooking(e, f.b1.ID)
			wantErr(e.t, err, domain.ErrNotFound)

			err = e.write(func(ctx context.Context, tx store.Tx) error { return tx.Redemptions().Delete(ctx, f.b1.ID) })
			wantErr(e.t, err, domain.ErrNotFound)
		},
		"OnePerBooking": func(e *env) {
			f := setup(e)
			e.redeem(f.pass, f.b1, t0)

			other := e.pass(e.passType("Single visit"), e.person("Bob", "Owner"), 1, t0)

			for _, p := range []domain.Pass{f.pass, other} {
				err := e.write(func(ctx context.Context, tx store.Tx) error {
					return tx.Redemptions().Create(ctx, domain.PassRedemption{PassID: p.ID, BookingID: f.b1.ID, At: t0})
				})
				wantErr(e.t, err, domain.ErrDuplicate)
			}
		},
		"MissingReferenceIsConflict": func(e *env) {
			f := setup(e)

			for _, r := range []domain.PassRedemption{
				{PassID: newID[domain.PassID](e), BookingID: f.b1.ID, At: t0},
				{PassID: f.pass.ID, BookingID: newID[domain.BookingID](e), At: t0},
			} {
				err := e.write(func(ctx context.Context, tx store.Tx) error { return tx.Redemptions().Create(ctx, r) })
				wantErr(e.t, err, domain.ErrConflict)
				wantNotErr(e.t, err, domain.ErrDuplicate)
			}
		},
		"Lists": func(e *env) {
			f := setup(e)
			other := e.pass(e.passType("Single visit"), e.person("Bob", "Owner"), 1, t0)
			b3 := e.booking(e.lesson(f.setting, t0.Add(2*time.Hour), 5), e.person("Cy", "Booker"), t0)

			late := e.redeem(f.pass, f.b1, t0.Add(time.Hour))
			early := e.redeem(f.pass, f.b2, t0)
			others := e.redeem(other, b3, t0)

			byPasses := func(latest int) []domain.PassRedemption {
				return mustQuery(e, func(ctx context.Context, tx store.Tx) ([]domain.PassRedemption, error) {
					return tx.Redemptions().ListByPasses(ctx, []domain.PassID{other.ID, f.pass.ID}, latest)
				})
			}
			// By pass, then time; the latest of each pass only with a
			// limit.
			wantSlice(e.t, byPasses(0), []domain.PassRedemption{early, late, others})
			wantSlice(e.t, byPasses(1), []domain.PassRedemption{late, others})
			wantSlice(e.t, byPasses(2), []domain.PassRedemption{early, late, others})

			counts := mustQuery(e, func(ctx context.Context, tx store.Tx) (map[domain.PassID]int, error) {
				return tx.Redemptions().CountByPasses(ctx, []domain.PassID{f.pass.ID, other.ID, newID[domain.PassID](e)})
			})
			if len(counts) != 2 || counts[f.pass.ID] != 2 || counts[other.ID] != 1 {
				e.t.Errorf("counts %v", counts)
			}

			got := mustQuery(e, func(ctx context.Context, tx store.Tx) ([]domain.PassRedemption, error) {
				return tx.Redemptions().ListByBookings(ctx, []domain.BookingID{b3.ID, f.b2.ID, f.b1.ID, b3.ID})
			})
			// By booking.
			wantSlice(e.t, got, []domain.PassRedemption{late, early, others})

			got = mustQuery(e, func(ctx context.Context, tx store.Tx) ([]domain.PassRedemption, error) {
				return tx.Redemptions().ListByBookings(ctx, nil)
			})
			wantSlice(e.t, got, nil)
		},
		"RollbackAndReadOnly": func(e *env) {
			f := setup(e)
			r := domain.PassRedemption{PassID: f.pass.ID, BookingID: f.b1.ID, At: t0}

			err := e.read(func(ctx context.Context, tx store.Tx) error { return tx.Redemptions().Create(ctx, r) })
			wantErr(e.t, err, store.ErrReadOnly)

			err = e.write(func(ctx context.Context, tx store.Tx) error {
				if err := tx.Redemptions().Create(ctx, r); err != nil {
					return err
				}

				return errBoom
			})
			wantErr(e.t, err, errBoom)

			_, err = byBooking(e, f.b1.ID)
			wantErr(e.t, err, domain.ErrNotFound)

			e.redeem(f.pass, f.b1, t0)

			err = e.read(func(ctx context.Context, tx store.Tx) error { return tx.Redemptions().Delete(ctx, f.b1.ID) })
			wantErr(e.t, err, store.ErrReadOnly)

			err = e.write(func(ctx context.Context, tx store.Tx) error {
				if err := tx.Redemptions().Delete(ctx, f.b1.ID); err != nil {
					return err
				}

				return errBoom
			})
			wantErr(e.t, err, errBoom)

			if _, err := byBooking(e, f.b1.ID); err != nil {
				e.t.Fatalf("redemption gone after rollback: %v", err)
			}
		},
		"PaidBookingsStay": func(e *env) {
			s := e.setting()
			ada := e.person("Ada", "Member")
			l := e.lesson(s, t0, 5)
			b := e.newBooking(l, ada, t0)
			b.Source = domain.BookingSourceMember
			e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Bookings().Create(ctx, b) })
			e.redeem(e.pass(e.passType("10-visit pass"), ada, 3, t0), b, t0)

			err := e.write(func(ctx context.Context, tx store.Tx) error {
				_, err := tx.Bookings().DeleteByLessonAndSource(ctx, l.ID, domain.BookingSourceMember)

				return err
			})
			wantErr(e.t, err, domain.ErrConflict)

			got := mustQuery(e, func(ctx context.Context, tx store.Tx) ([]domain.Booking, error) {
				return tx.Bookings().ListByLesson(ctx, l.ID)
			})
			wantSlice(e.t, got, []domain.Booking{b})
		},
	})
}
