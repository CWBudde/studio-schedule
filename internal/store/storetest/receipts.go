package storetest

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// newReceipt returns a cash sale receipt of one 10-visit pass paid by payer,
// numbered year and seq, without creating it.
func (e *env) newReceipt(year, seq int, payer domain.Person) domain.Receipt {
	e.t.Helper()

	line := domain.NewReceiptLine(1, "10-visit pass", 1, 15000, 1900, domain.TaxStandard)
	r := domain.Receipt{
		ID: newID[domain.ReceiptID](e), Number: domain.ReceiptNumber("CS", year, seq), Year: year, Sequence: seq,
		Kind: domain.ReceiptSale, IssuedAt: t0, IssueDate: domain.Date{Year: year, Month: time.March, Day: 2},
		Locale: "en", Issuer: domain.ReceiptIssuer{Name: "Contract School", Address: "Example Street 1\n00000 Example Town", TaxID: "12/345/67890"},
		TaxMode: domain.TaxStandard, PayerID: payer.ID, RecipientName: payer.GivenName + " " + payer.FamilyName,
		PaymentMethod: domain.PaymentCash, Currency: "EUR", Net: line.Net, VAT: line.VAT, Gross: line.Gross,
		Lines: []domain.ReceiptLine{line},
	}
	valid(e.t, r)

	return r
}

func (e *env) receipt(r domain.Receipt) domain.Receipt {
	e.t.Helper()
	e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Receipts().Create(ctx, r) })

	return r
}

// cancellationOf returns the cancellation receipt of r, numbered seq, without
// creating it.
func (e *env) cancellationOf(r domain.Receipt, seq int) domain.Receipt {
	e.t.Helper()

	c := r
	c.ID, c.Number, c.Sequence = newID[domain.ReceiptID](e), domain.ReceiptNumber("CS", r.Year, seq), seq
	c.Kind, c.CancelsReceiptID, c.Reason = domain.ReceiptCancellation, r.ID, "Sold by mistake"
	c.Net, c.VAT, c.Gross = -r.Net, -r.VAT, -r.Gross
	c.Lines = make([]domain.ReceiptLine, len(r.Lines))

	for i, l := range r.Lines {
		c.Lines[i] = l.Negated()
	}

	valid(e.t, c)

	return c
}

func compareIDs(a, b domain.ReceiptID) int { return bytes.Compare(a[:], b[:]) }

func wantReceipt(t *testing.T, got, want domain.Receipt) {
	t.Helper()

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %+v\nwant %+v", got, want)
	}
}

func wantNumbers(t *testing.T, got []domain.Receipt, want ...string) {
	t.Helper()

	numbers := make([]string, len(got))
	for i, r := range got {
		numbers[i] = r.Number
	}

	if !slices.Equal(numbers, want) {
		t.Fatalf("got receipts %v, want %v", numbers, want)
	}
}

func testReceipts(t *testing.T, newStore func(*testing.T) store.Store) {
	t.Helper()

	get := func(e *env, id domain.ReceiptID) (domain.Receipt, error) {
		return query(e, func(ctx context.Context, tx store.Tx) (domain.Receipt, error) { return tx.Receipts().Get(ctx, id) })
	}

	cases(t, newStore, map[string]func(e *env){
		"CreateThenGet": func(e *env) {
			r := e.receipt(e.newReceipt(2026, 1, e.person("Ada", "Payer")))

			got, err := get(e, r.ID)
			if err != nil {
				e.t.Fatal(err)
			}

			wantReceipt(e.t, got, r)

			byNumber := mustQuery(e, func(ctx context.Context, tx store.Tx) (domain.Receipt, error) {
				return tx.Receipts().GetByNumber(ctx, r.Number)
			})
			wantReceipt(e.t, byNumber, r)
		},
		"CreateFullThenGet": func(e *env) {
			payer := e.person("Ada", "Payer")
			office := e.account(e.person("Olga", "Office"), "olga@example.org")

			r := e.newReceipt(2026, 1, payer)
			r.Issuer.Email, r.Issuer.Phone = "office@example.org", "+49 000 000000"
			r.TaxNote, r.Footer = "Paid in full.", "Thank you"
			r.RecipientAddress, r.RecipientEmail = "Payer Lane 2\n00000 Example Town", "ada@example.org"
			r.PaymentMethod, r.CreatedBy = domain.PaymentCard, office.ID
			r.Lines = append(r.Lines, domain.NewReceiptLine(2, "Single visit", 3, 1800, 700, domain.TaxStandard))
			r.Net, r.VAT, r.Gross = r.Lines[0].Net+r.Lines[1].Net, r.Lines[0].VAT+r.Lines[1].VAT, r.Lines[0].Gross+r.Lines[1].Gross
			valid(e.t, r)
			e.receipt(r)

			got, err := get(e, r.ID)
			if err != nil {
				e.t.Fatal(err)
			}

			wantReceipt(e.t, got, r)

			// The caller's copy is not the store's.
			got.Lines[0].Description = "changed"
			again, _ := get(e, r.ID)
			wantReceipt(e.t, again, r)
		},
		"GetMissingIsNotFound": func(e *env) {
			_, err := get(e, newID[domain.ReceiptID](e))
			wantErr(e.t, err, domain.ErrNotFound)

			_, err = query(e, func(ctx context.Context, tx store.Tx) (domain.Receipt, error) {
				return tx.Receipts().GetByNumber(ctx, "CS-2026-0001")
			})
			wantErr(e.t, err, domain.ErrNotFound)
		},
		"DuplicateNumberIsDuplicate": func(e *env) {
			payer := e.person("Ada", "Payer")
			e.receipt(e.newReceipt(2026, 1, payer))

			err := e.write(func(ctx context.Context, tx store.Tx) error {
				return tx.Receipts().Create(ctx, e.newReceipt(2026, 1, payer))
			})
			wantErr(e.t, err, domain.ErrDuplicate)
		},
		"DuplicateIDIsConflict": func(e *env) {
			r := e.receipt(e.newReceipt(2026, 1, e.person("Ada", "Payer")))
			again := r
			again.Number, again.Sequence = domain.ReceiptNumber("CS", 2026, 2), 2

			err := e.write(func(ctx context.Context, tx store.Tx) error { return tx.Receipts().Create(ctx, again) })
			wantErr(e.t, err, domain.ErrConflict)
			wantNotErr(e.t, err, domain.ErrDuplicate)
		},
		"MissingReferenceIsConflict": func(e *env) {
			payer := e.person("Ada", "Payer")

			for name, mutate := range map[string]func(r *domain.Receipt){
				"payer":   func(r *domain.Receipt) { r.PayerID = newID[domain.PersonID](e) },
				"creator": func(r *domain.Receipt) { r.CreatedBy = newID[domain.AccountID](e) },
				"canceled": func(r *domain.Receipt) {
					*r = e.cancellationOf(*r, 2)
					r.CancelsReceiptID = newID[domain.ReceiptID](e)
				},
			} {
				r := e.newReceipt(2026, 1, payer)
				mutate(&r)

				err := e.write(func(ctx context.Context, tx store.Tx) error { return tx.Receipts().Create(ctx, r) })
				if !errors.Is(err, domain.ErrConflict) || errors.Is(err, domain.ErrDuplicate) {
					e.t.Errorf("%s: want a conflict, got %v", name, err)
				}
			}
		},
		"RollbackCreate": func(e *env) {
			r := e.newReceipt(2026, 1, e.person("Ada", "Payer"))

			err := e.write(func(ctx context.Context, tx store.Tx) error {
				if err := tx.Receipts().Create(ctx, r); err != nil {
					return err
				}

				return errBoom
			})
			wantErr(e.t, err, errBoom)

			_, err = get(e, r.ID)
			wantErr(e.t, err, domain.ErrNotFound)
		},
		"ReadOnlyRejectsWrites": func(e *env) {
			payer := e.person("Ada", "Payer")
			r := e.newReceipt(2026, 1, payer)

			err := e.read(func(ctx context.Context, tx store.Tx) error { return tx.Receipts().Create(ctx, r) })
			wantErr(e.t, err, store.ErrReadOnly)

			_, err = get(e, r.ID)
			wantErr(e.t, err, domain.ErrNotFound)

			// A PDF for a receipt that exists, valid but for the transaction.
			stored := e.receipt(e.newReceipt(2026, 2, payer))
			err = e.read(func(ctx context.Context, tx store.Tx) error {
				return tx.Receipts().PutPDF(ctx, stored.ID, []byte("%PDF-1.7"))
			})
			wantErr(e.t, err, store.ErrReadOnly)

			_, err = query(e, func(ctx context.Context, tx store.Tx) ([]byte, error) { return tx.Receipts().PDF(ctx, stored.ID) })
			wantErr(e.t, err, domain.ErrNotFound)
		},
		"GetCancellationOfNoReceiptIsNotFound": func(e *env) {
			// A sale cancels no receipt: asking for the cancellation of the
			// zero ID must not find it.
			r := e.receipt(e.newReceipt(2026, 1, e.person("Ada", "Payer")))
			e.receipt(e.cancellationOf(r, 2))

			for name, id := range map[string]domain.ReceiptID{"zero": {}, "unknown": newID[domain.ReceiptID](e)} {
				_, err := query(e, func(ctx context.Context, tx store.Tx) (domain.Receipt, error) {
					return tx.Receipts().GetCancellation(ctx, id)
				})
				if !errors.Is(err, domain.ErrNotFound) {
					e.t.Errorf("%s: want ErrNotFound, got %v", name, err)
				}
			}
		},
		"ListCancellationsByCanceledReceipt": func(e *env) {
			payer := e.person("Ada", "Payer")
			first := e.receipt(e.newReceipt(2026, 1, payer))
			second := e.receipt(e.newReceipt(2026, 2, payer))
			kept := e.receipt(e.newReceipt(2026, 3, payer))

			// The newer cancellation cancels the newer receipt, so the order by
			// canceled receipt is the reverse of the newest-first order of List.
			ofFirst := e.receipt(e.cancellationOf(first, 4))
			ofSecond := e.receipt(e.cancellationOf(second, 5))

			if compareIDs(first.ID, second.ID) >= 0 {
				e.t.Fatalf("want ascending IDs from the sequence, got %v, %v", first.ID, second.ID)
			}

			got := mustQuery(e, func(ctx context.Context, tx store.Tx) ([]domain.Receipt, error) {
				return tx.Receipts().ListCancellations(ctx, []domain.ReceiptID{second.ID, kept.ID, {}, first.ID})
			})
			wantNumbers(e.t, got, ofFirst.Number, ofSecond.Number)
			wantReceipt(e.t, got[0], ofFirst)
			wantReceipt(e.t, got[1], ofSecond)

			got = mustQuery(e, func(ctx context.Context, tx store.Tx) ([]domain.Receipt, error) {
				return tx.Receipts().ListCancellations(ctx, nil)
			})
			wantNumbers(e.t, got)
		},
		"CanceledOnce": func(e *env) {
			r := e.receipt(e.newReceipt(2026, 1, e.person("Ada", "Payer")))

			_, err := query(e, func(ctx context.Context, tx store.Tx) (domain.Receipt, error) {
				return tx.Receipts().GetCancellation(ctx, r.ID)
			})
			wantErr(e.t, err, domain.ErrNotFound)

			c := e.receipt(e.cancellationOf(r, 2))

			got := mustQuery(e, func(ctx context.Context, tx store.Tx) (domain.Receipt, error) {
				return tx.Receipts().GetCancellation(ctx, r.ID)
			})
			wantReceipt(e.t, got, c)

			err = e.write(func(ctx context.Context, tx store.Tx) error {
				return tx.Receipts().Create(ctx, e.cancellationOf(r, 3))
			})
			wantErr(e.t, err, domain.ErrDuplicate)

			list := mustQuery(e, func(ctx context.Context, tx store.Tx) ([]domain.Receipt, error) {
				return tx.Receipts().ListCancellations(ctx, []domain.ReceiptID{r.ID, c.ID, newID[domain.ReceiptID](e)})
			})
			wantNumbers(e.t, list, c.Number)
		},
		"List": func(e *env) {
			ada, bob := e.person("Ada", "Payer"), e.person("Bob", "Builder")
			r25 := e.receipt(e.newReceipt(2025, 7, bob))
			r1 := e.receipt(e.newReceipt(2026, 1, ada))
			r2 := e.receipt(e.newReceipt(2026, 2, bob))
			r3 := e.receipt(e.cancellationOf(r1, 3))

			list := func(f store.ReceiptFilter) ([]domain.Receipt, int) {
				e.t.Helper()

				var (
					out   []domain.Receipt
					total int
				)

				e.mustWrite(func(ctx context.Context, tx store.Tx) error {
					var err error
					out, total, err = tx.Receipts().List(ctx, f)

					return err
				})

				return out, total
			}

			got, total := list(store.ReceiptFilter{Limit: 10})
			wantNumbers(e.t, got, r3.Number, r2.Number, r1.Number, r25.Number)
			wantEqual(e.t, total, 4)
			wantReceipt(e.t, got[0], r3)

			got, total = list(store.ReceiptFilter{Year: 2026, Limit: 2})
			wantNumbers(e.t, got, r3.Number, r2.Number)
			wantEqual(e.t, total, 3)

			got, total = list(store.ReceiptFilter{Year: 2026, Offset: 2, Limit: 2})
			wantNumbers(e.t, got, r1.Number)
			wantEqual(e.t, total, 3)

			got, total = list(store.ReceiptFilter{Query: "bob", Limit: 10})
			wantNumbers(e.t, got, r2.Number, r25.Number)
			wantEqual(e.t, total, 2)

			got, _ = list(store.ReceiptFilter{Query: "cs-2026-000", Limit: 10})
			wantNumbers(e.t, got, r3.Number, r2.Number, r1.Number)

			got, total = list(store.ReceiptFilter{Query: "nobody", Limit: 10})
			wantNumbers(e.t, got)
			wantEqual(e.t, total, 0)

			got, total = list(store.ReceiptFilter{})
			wantNumbers(e.t, got)
			wantEqual(e.t, total, 4)

			byPayer := mustQuery(e, func(ctx context.Context, tx store.Tx) ([]domain.Receipt, error) {
				return tx.Receipts().ListByPayers(ctx, []domain.PersonID{bob.ID})
			})
			wantNumbers(e.t, byPayer, r2.Number, r25.Number)

			byIDs := mustQuery(e, func(ctx context.Context, tx store.Tx) ([]domain.Receipt, error) {
				return tx.Receipts().ListByIDs(ctx, []domain.ReceiptID{r3.ID, r25.ID, newID[domain.ReceiptID](e), r3.ID})
			})
			if len(byIDs) != 2 || compareIDs(byIDs[0].ID, byIDs[1].ID) >= 0 {
				e.t.Fatalf("by IDs: %+v", byIDs)
			}

			for _, r := range byIDs {
				wantReceipt(e.t, r, map[domain.ReceiptID]domain.Receipt{r3.ID: r3, r25.ID: r25}[r.ID])
			}
		},
		// The search folds ASCII letters only, also in the query: "Öztürk"
		// finds itself whatever the database's default collation.
		"TotalsByDay": func(e *env) {
			payer := e.person("Paula", "Payer")
			on := func(r domain.Receipt, d domain.Date) domain.Receipt {
				r.IssueDate, r.Year = d, d.Year
				valid(e.t, r)

				return e.receipt(r)
			}
			newYearsEve := domain.Date{Year: 2025, Month: time.December, Day: 31}
			march2 := domain.Date{Year: 2026, Month: time.March, Day: 2}
			march3 := domain.Date{Year: 2026, Month: time.March, Day: 3}

			on(e.newReceipt(2025, 1, payer), newYearsEve)
			sale := on(e.newReceipt(2026, 1, payer), march2)
			on(e.newReceipt(2026, 2, payer), march2)
			chf := e.newReceipt(2026, 3, payer)
			chf.Currency = "CHF"
			on(chf, march2)
			on(e.cancellationOf(sale, 4), march3)
			on(e.newReceipt(2026, 5, payer), domain.Date{Year: 2026, Month: time.March, Day: 4}) // after the range

			got := mustQuery(e, func(ctx context.Context, tx store.Tx) ([]store.ReceiptDayTotals, error) {
				return tx.Receipts().TotalsByDay(ctx, newYearsEve, march3)
			})
			wantSlice(e.t, got, []store.ReceiptDayTotals{
				{Date: newYearsEve, Currency: "EUR", Sales: 1, Gross: sale.Gross},
				{Date: march2, Currency: "CHF", Sales: 1, Gross: sale.Gross},
				{Date: march2, Currency: "EUR", Sales: 2, Gross: 2 * sale.Gross},
				{Date: march3, Currency: "EUR", Cancellations: 1, Gross: -sale.Gross},
			})

			got = mustQuery(e, func(ctx context.Context, tx store.Tx) ([]store.ReceiptDayTotals, error) {
				return tx.Receipts().TotalsByDay(ctx, march3, march2)
			})
			wantSlice(e.t, got, nil)
		},
		"SearchFoldsASCIIOnly": func(e *env) {
			r := e.newReceipt(2026, 1, e.person("Jürgen", "Öztürk"))
			e.receipt(r)

			for q, want := range map[string]int{"Öztürk": 1, "JÜRGEN": 0, "jÜrgen": 0, "JüRGEN": 1, "öztürk": 0, "ztürk": 1} {
				var total int

				e.mustWrite(func(ctx context.Context, tx store.Tx) error {
					var err error
					_, total, err = tx.Receipts().List(ctx, store.ReceiptFilter{Query: q, Limit: 10})

					return err
				})

				if total != want {
					e.t.Errorf("query %q: %d receipts, want %d", q, total, want)
				}
			}
		},
		"PDFStoredOnce": func(e *env) {
			r := e.receipt(e.newReceipt(2026, 1, e.person("Ada", "Payer")))
			pdf := func() ([]byte, error) {
				return query(e, func(ctx context.Context, tx store.Tx) ([]byte, error) { return tx.Receipts().PDF(ctx, r.ID) })
			}

			_, err := pdf()
			wantErr(e.t, err, domain.ErrNotFound)

			e.mustWrite(func(ctx context.Context, tx store.Tx) error {
				return tx.Receipts().PutPDF(ctx, r.ID, []byte("%PDF-1.7 one"))
			})

			got, err := pdf()
			if err != nil || string(got) != "%PDF-1.7 one" {
				e.t.Fatalf("PDF: %q, %v", got, err)
			}

			err = e.write(func(ctx context.Context, tx store.Tx) error {
				return tx.Receipts().PutPDF(ctx, r.ID, []byte("%PDF-1.7 two"))
			})
			wantErr(e.t, err, domain.ErrDuplicate)

			err = e.write(func(ctx context.Context, tx store.Tx) error {
				return tx.Receipts().PutPDF(ctx, newID[domain.ReceiptID](e), []byte("%PDF-1.7"))
			})
			wantErr(e.t, err, domain.ErrConflict)
			wantNotErr(e.t, err, domain.ErrDuplicate)

			got, _ = pdf()
			wantEqual(e.t, string(got), "%PDF-1.7 one")
		},
		"EmptyPDFIsInvalid": func(e *env) {
			r := e.receipt(e.newReceipt(2026, 1, e.person("Ada", "Payer")))

			for name, pdf := range map[string][]byte{"nil": nil, "empty": {}} {
				err := e.write(func(ctx context.Context, tx store.Tx) error { return tx.Receipts().PutPDF(ctx, r.ID, pdf) })
				if !errors.Is(err, domain.ErrValidation) {
					e.t.Errorf("%s: want ErrValidation, got %v", name, err)
				}
			}

			_, err := query(e, func(ctx context.Context, tx store.Tx) ([]byte, error) { return tx.Receipts().PDF(ctx, r.ID) })
			wantErr(e.t, err, domain.ErrNotFound)

			// The rejected attempts left room for the PDF.
			e.mustWrite(func(ctx context.Context, tx store.Tx) error {
				return tx.Receipts().PutPDF(ctx, r.ID, []byte("%PDF-1.7"))
			})
		},
		"PayerCannotBeDeleted": func(e *env) {
			payer := e.person("Ada", "Payer")
			e.receipt(e.newReceipt(2026, 1, payer))

			err := e.write(func(ctx context.Context, tx store.Tx) error { return tx.Persons().Delete(ctx, payer.ID) })
			wantErr(e.t, err, domain.ErrConflict)
		},
		"PassOfReceipt": func(e *env) {
			payer := e.person("Ada", "Payer")
			r := e.receipt(e.newReceipt(2026, 1, payer))
			pt := e.passType("10-visit pass")

			p := e.newPass(pt, payer, 10, t0)
			p.Origin, p.PricePaid, p.ReceiptID = domain.PassSold, 15000, r.ID
			e.mustWrite(func(ctx context.Context, tx store.Tx) error { return tx.Passes().Create(ctx, p) })

			got := mustQuery(e, func(ctx context.Context, tx store.Tx) (domain.Pass, error) {
				return tx.Passes().GetByReceipt(ctx, r.ID)
			})
			wantEqual(e.t, got, p)

			listed := mustQuery(e, func(ctx context.Context, tx store.Tx) ([]domain.Pass, error) {
				return tx.Passes().ListByReceipts(ctx, []domain.ReceiptID{r.ID, newID[domain.ReceiptID](e), {}})
			})
			wantSlice(e.t, listed, []domain.Pass{p})

			_, err := query(e, func(ctx context.Context, tx store.Tx) (domain.Pass, error) {
				return tx.Passes().GetByReceipt(ctx, newID[domain.ReceiptID](e))
			})
			wantErr(e.t, err, domain.ErrNotFound)

			// A receipt confirms one sale.
			other := e.newPass(pt, payer, 10, t0)
			other.Origin, other.ReceiptID = domain.PassSold, r.ID
			err = e.write(func(ctx context.Context, tx store.Tx) error { return tx.Passes().Create(ctx, other) })
			wantErr(e.t, err, domain.ErrConflict)
		},
		"MailAttachesReceipt": func(e *env) {
			r := e.receipt(e.newReceipt(2026, 1, e.person("Ada", "Payer")))

			m := e.outboxMail(t0, t0)
			m.ReceiptID = r.ID
			e.createMails(m)

			got := mustQuery(e, func(ctx context.Context, tx store.Tx) (domain.OutboxMail, error) {
				return tx.Outbox().Get(ctx, m.ID)
			})
			wantEqual(e.t, got, m)

			missing := e.outboxMail(t0, t0)
			missing.ReceiptID = newID[domain.ReceiptID](e)
			err := e.write(func(ctx context.Context, tx store.Tx) error { return tx.Outbox().Create(ctx, missing) })
			wantErr(e.t, err, domain.ErrConflict)
		},
	})
}

func testReceiptCounters(t *testing.T, newStore func(*testing.T) store.Store) {
	t.Helper()

	next := func(e *env, year int) int {
		e.t.Helper()

		var n int

		e.mustWrite(func(ctx context.Context, tx store.Tx) error {
			var err error
			n, err = tx.ReceiptCounters().Next(ctx, year)

			return err
		})

		return n
	}

	cases(t, newStore, map[string]func(e *env){
		"CountsPerYear": func(e *env) {
			years := func() []int {
				return mustQuery(e, func(ctx context.Context, tx store.Tx) ([]int, error) { return tx.ReceiptCounters().Years(ctx) })
			}
			wantSlice(e.t, years(), nil)

			wantEqual(e.t, next(e, 2026), 1)
			wantEqual(e.t, next(e, 2026), 2)
			wantEqual(e.t, next(e, 2025), 1)
			wantEqual(e.t, next(e, 2026), 3)
			wantSlice(e.t, years(), []int{2026, 2025})
		},
		"RollbackGivesTheNumberBack": func(e *env) {
			wantEqual(e.t, next(e, 2026), 1)

			err := e.write(func(ctx context.Context, tx store.Tx) error {
				n, err := tx.ReceiptCounters().Next(ctx, 2026)
				if err != nil {
					return err
				}

				wantEqual(e.t, n, 2)

				return errBoom
			})
			wantErr(e.t, err, errBoom)
			wantEqual(e.t, next(e, 2026), 2)
		},
		"ReadOnlyRejects": func(e *env) {
			err := e.read(func(ctx context.Context, tx store.Tx) error {
				_, err := tx.ReceiptCounters().Next(ctx, 2026)

				return err
			})
			wantErr(e.t, err, store.ErrReadOnly)
		},
		// Concurrent issuers, half of which roll back, leave the committed
		// numbers gapless: every number is taken once, and a rolled-back one
		// goes to a later transaction.
		"GaplessUnderConcurrency": func(e *env) {
			const n = 16

			var (
				mu        sync.Mutex
				committed []int
				wg        sync.WaitGroup
			)

			for i := range n {
				wg.Go(func() {
					var got int

					err := e.write(func(ctx context.Context, tx store.Tx) error {
						var err error
						if got, err = tx.ReceiptCounters().Next(ctx, 2026); err != nil {
							return err
						}

						if i%2 == 1 {
							return errBoom
						}

						return nil
					})
					if errors.Is(err, errBoom) {
						return
					}

					if err != nil {
						e.t.Errorf("next: %v", err)

						return
					}

					mu.Lock()
					committed = append(committed, got)
					mu.Unlock()
				})
			}

			wg.Wait()
			slices.Sort(committed)

			want := make([]int, n/2)
			for i := range want {
				want[i] = i + 1
			}

			if !slices.Equal(committed, want) {
				e.t.Fatalf("committed numbers %v, want %v", committed, want)
			}

			wantEqual(e.t, next(e, 2026), n/2+1)
		},
	})
}
