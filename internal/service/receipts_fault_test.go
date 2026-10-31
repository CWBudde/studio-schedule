package service

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/mail"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// faulty returns the pass and receipt services of f at bookingNow on a
// FaultStore over f's store.
func (f *passFixture) faulty(t *testing.T) (*FaultStore, *PassService, *ReceiptService) {
	t.Helper()

	fs := NewFaultStore(f.st)
	clk := clock.NewFixed(mustInstant(t, bookingNow))
	rs := NewReceiptService(fs, clk, f.seq, testMails(newTestRenderer(t)), f.pdf)

	return fs, NewPassService(fs, clk, f.seq, rs), rs
}

// cleanCalls returns a FaultSweep.Setup that records the calls of the
// fault-free run (the first) in *calls, for a Check that tells the
// transactions of a multi-transaction operation apart.
func cleanCalls(setup func(t *testing.T) (*FaultStore, func() error), calls *[]FaultCall) func(t *testing.T) (*FaultStore, func() error) {
	var clean *FaultStore

	return func(t *testing.T) (*FaultStore, func() error) {
		fs, op := setup(t)
		if clean == nil {
			clean = fs

			return fs, func() error {
				err := op()
				*calls = fs.Calls()

				return err
			}
		}

		return fs, op
	}
}

// afterFirstCommit reports whether call came after the first commit in
// calls: in a later transaction than the first.
func afterFirstCommit(calls []FaultCall, call FaultCall) bool {
	commit := slices.Index(calls, FaultCall{Method: "Commit", N: 1})
	i := slices.Index(calls, call)

	return commit >= 0 && i > commit
}

// TestSellStoreFaults: a store failure anywhere in a sale is returned and
// leaves no pass, no receipt, no PDF, no mail and no receipt number taken
// (the counter is data): receipt numbers stay gapless.
func TestSellStoreFaults(t *testing.T) {
	t.Parallel()

	SweepFaults(t, FaultSweep{Setup: func(t *testing.T) (*FaultStore, func() error) {
		f := newPassFixture(t, domain.RedeemOnAttendance)
		fs, svc, _ := f.faulty(t)

		return fs, func() error {
			// Anna pays for Mia: the payer and the owner are both looked up,
			// and the receipt goes to Anna's account.
			_, err := svc.Sell(t.Context(), f.office, f.mia.ID, PassSale{
				PassTypeID: f.tenVisits.ID, PayerID: f.anna.ID, PaymentMethod: domain.PaymentCard,
			})

			return err
		}
	}})
}

// TestSellFailedCommitTakesNoNumber: a sale whose commit fails took no
// receipt number; the next sale gets the first one of the year.
func TestSellFailedCommitTakesNoNumber(t *testing.T) {
	t.Parallel()

	f := newPassFixture(t, domain.RedeemOnAttendance)
	fs, svc, _ := f.faulty(t)
	fs.Fail("Commit", 1)

	if _, err := svc.Sell(t.Context(), f.office, f.anna.ID, PassSale{PassTypeID: f.single.ID}); !errors.Is(err, ErrInjected) {
		t.Fatalf("Sell = %v, want the injected fault", err)
	}

	if f.receiptCount(t) != 0 || f.pdf.count() != 1 {
		t.Fatalf("%d receipts, %d renders after the failed commit, want none stored after one render",
			f.receiptCount(t), f.pdf.count())
	}

	if r, _ := f.receipt(t, f.sell(t, f.anna, f.single, "").ReceiptID); r.Number != "SD-2026-0001" {
		t.Fatalf("number %s after a failed commit, want SD-2026-0001", r.Number)
	}
}

// TestCancelReceiptStoreFaults: a store failure anywhere in a Storno is returned
// and leaves no cancellation receipt, no mail, no number taken and the pass
// active: the Storno is atomic.
func TestCancelReceiptStoreFaults(t *testing.T) {
	t.Parallel()

	SweepFaults(t, FaultSweep{Setup: func(t *testing.T) (*FaultStore, func() error) {
		f := newPassFixture(t, domain.RedeemOnAttendance)
		pass := f.sell(t, f.anna, f.tenVisits, "")
		fs, _, rs := f.faulty(t)

		return fs, func() error {
			_, err := rs.Cancel(t.Context(), f.office, pass.ReceiptID, "Sold twice")

			return err
		}
	}})
}

// TestCancelMergedPayerStoreFaults: a Storno mailed to the survivor of a
// merged payer (ADR 0015) fails as a whole on a store failure too.
func TestCancelMergedPayerStoreFaults(t *testing.T) {
	t.Parallel()

	SweepFaults(t, FaultSweep{Setup: func(t *testing.T) (*FaultStore, func() error) {
		f := newPassFixture(t, domain.RedeemOnAttendance)
		dup := f.person(t, "Anna", "", "", "")

		v, err := f.passService(t, bookingNow).Sell(t.Context(), f.office, f.mia.ID,
			PassSale{PassTypeID: f.single.ID, PayerID: dup.ID})
		if err != nil {
			t.Fatal(err)
		}

		persons := NewPersonService(f.st, clock.NewFixed(mustInstant(t, bookingNow)), f.seq, Mails{})
		if _, err := persons.Merge(t.Context(), f.office, MergeRequest{SurvivorID: f.anna.ID, MergedID: dup.ID}, true); err != nil {
			t.Fatal(err)
		}

		fs, _, rs := f.faulty(t)

		return fs, func() error {
			_, err := rs.Cancel(t.Context(), f.office, v.Pass.ReceiptID, "")

			return err
		}
	}})
}

// TestCancelNeedsReceiptSettings: incomplete receipt settings refuse a
// Storno like a sale, and the pass it would void stays active.
func TestCancelNeedsReceiptSettings(t *testing.T) {
	t.Parallel()

	f := newPassFixture(t, domain.RedeemOnAttendance)
	pass := f.sell(t, f.anna, f.tenVisits, "")
	f.updateSettings(t, func(s *domain.Settings) { s.Receipts.Issuer.TaxID = "" })

	_, err := f.receiptService(t, bookingNow).Cancel(t.Context(), f.office, pass.ReceiptID, "")
	if !errors.Is(err, ErrReceiptSettingsIncomplete) || !strings.Contains(err.Error(), "tax number") {
		t.Fatalf("Cancel = %v, want ErrReceiptSettingsIncomplete naming the tax number", err)
	}

	if p := f.pass(t, pass.ID); p.Status != domain.PassActive || f.receiptCount(t) != 1 {
		t.Fatalf("pass %s with %d receipts after a refused Storno, want active with the sale's only",
			p.Status, f.receiptCount(t))
	}
}

// TestCancelReceiptOfVoidedPass: the receipt of a pass the office voided
// before can still be canceled; the pass stays voided as it was.
func TestCancelReceiptOfVoidedPass(t *testing.T) {
	t.Parallel()

	f := newPassFixture(t, domain.RedeemOnAttendance)
	pass := f.sell(t, f.anna, f.tenVisits, "")

	if _, err := f.passService(t, bookingNow).Void(t.Context(), pass.ID); err != nil {
		t.Fatal(err)
	}

	voided := f.pass(t, pass.ID)

	v, err := f.receiptService(t, staffStarted).Cancel(t.Context(), f.office, pass.ReceiptID, "")
	if err != nil {
		t.Fatal(err)
	}

	if v.Receipt.Kind != domain.ReceiptCancellation || v.Cancels == nil || v.Cancels.ID != pass.ReceiptID {
		t.Errorf("view %+v, want the Storno of the sale", v)
	}

	if p := f.pass(t, pass.ID); p != voided {
		t.Errorf("pass %+v after the Storno, want it unchanged %+v", p, voided)
	}
}

// TestCancelReceiptWithoutPass cancels a receipt no pass refers to (the
// seed's): the Storno is issued and mailed to the payer.
func TestCancelReceiptWithoutPass(t *testing.T) {
	t.Parallel()

	f := newPassFixture(t, domain.RedeemOnAttendance)
	r := f.newSeededReceipt(t)

	v, err := f.receiptService(t, bookingNow).Cancel(t.Context(), f.office, r.ID, "")
	if err != nil {
		t.Fatal(err)
	}

	if v.Receipt.Number != "SD-2026-0002" || v.Receipt.Gross != -r.Gross || !v.Pass.ID.IsZero() {
		t.Errorf("Storno %+v of %s", v.Receipt, r.Number)
	}

	if mails := f.mailsOf(t, mail.TemplateReceipt); len(mails) != 1 || mails[0].To != "anna@example.org" {
		t.Errorf("mails %+v, want the Storno to Anna", mails)
	}
}

// unreadyPDF is a renderer that cannot get ready (the demo's module failed
// to download).
type unreadyPDF struct{ fakePDF }

func (*unreadyPDF) Ready(context.Context) error { return errors.New("module download failed") }

// TestReceiptRendererNotReady: a renderer that cannot get ready fails a sale
// and a Storno before their transaction, as an internal error that does not
// wrap the renderer's.
func TestReceiptRendererNotReady(t *testing.T) {
	t.Parallel()

	f := newPassFixture(t, domain.RedeemOnAttendance)
	pass := f.sell(t, f.anna, f.single, "")

	fs := NewFaultStore(f.st)
	clk := clock.NewFixed(mustInstant(t, bookingNow))
	rs := NewReceiptService(fs, clk, f.seq, Mails{}, &unreadyPDF{})
	ps := NewPassService(fs, clk, f.seq, rs)

	_, err := ps.Sell(t.Context(), f.office, f.mia.ID, PassSale{PassTypeID: f.single.ID})
	if err == nil || !strings.Contains(err.Error(), "module download failed") {
		t.Errorf("Sell = %v, want the renderer's failure", err)
	}

	if _, err := rs.Cancel(t.Context(), f.office, pass.ReceiptID, ""); err == nil ||
		!strings.Contains(err.Error(), "module download failed") {
		t.Errorf("Cancel = %v, want the renderer's failure", err)
	}

	if n := fs.Count("InTx"); n != 0 {
		t.Errorf("%d transactions, want none", n)
	}
}

// TestSellWithoutReceiptService: a pass service without a receipt service
// sells nothing (every sale has a receipt) and stores nothing.
func TestSellWithoutReceiptService(t *testing.T) {
	t.Parallel()

	f := newPassFixture(t, domain.RedeemOnAttendance)
	svc := NewPassService(f.st, clock.NewFixed(mustInstant(t, bookingNow)), f.seq, nil)

	if _, err := svc.Sell(t.Context(), f.office, f.mia.ID, PassSale{PassTypeID: f.single.ID}); !errors.Is(err, ErrNoPDFRenderer) {
		t.Fatalf("Sell = %v, want ErrNoPDFRenderer", err)
	}

	if passes, err := svc.PersonPasses(t.Context(), f.mia.ID); err != nil || len(passes) != 0 || f.receiptCount(t) != 0 {
		t.Fatalf("passes %+v (%v), %d receipts after a refused sale", passes, err, f.receiptCount(t))
	}
}

// receiptReads sets up a sale with a Storno, a used pass's sale, and Anna's
// merged duplicate as a payer, and returns the receipt service on a
// FaultStore and the receipts.
func receiptReads(t *testing.T) (*passFixture, *FaultStore, *ReceiptService, domain.Pass) {
	t.Helper()

	f := newPassFixture(t, domain.RedeemOnBooking)
	canceled := f.sell(t, f.anna, f.single, "")

	if _, err := f.receiptService(t, bookingNow).Cancel(t.Context(), f.office, canceled.ReceiptID, ""); err != nil {
		t.Fatal(err)
	}

	used := f.sell(t, f.anna, f.tenVisits, "")
	f.book(t, bookingNow, f.regular(t, "2026-10-23T16:00:00Z"), f.anna)

	fs, _, rs := f.faulty(t)

	return f, fs, rs, used
}

// TestReceiptReadStoreFaults: the receipt reads return a store failure
// (and write nothing).
func TestReceiptReadStoreFaults(t *testing.T) {
	t.Parallel()

	for name, read := range map[string]func(t *testing.T, f *passFixture, rs *ReceiptService, used domain.Pass) error{
		"List": func(t *testing.T, _ *passFixture, rs *ReceiptService, _ domain.Pass) error {
			_, err := rs.List(t.Context(), store.ReceiptFilter{})

			return err
		},
		"Get": func(t *testing.T, _ *passFixture, rs *ReceiptService, used domain.Pass) error {
			_, err := rs.Get(t.Context(), used.ReceiptID)

			return err
		},
		"Mine": func(t *testing.T, f *passFixture, rs *ReceiptService, _ domain.Pass) error {
			_, err := rs.Mine(t.Context(), f.principal(t, f.anna))

			return err
		},
		"PDF": func(t *testing.T, _ *passFixture, rs *ReceiptService, used domain.Pass) error {
			_, err := rs.PDF(t.Context(), used.ReceiptID)

			return err
		},
		"MyPDF": func(t *testing.T, f *passFixture, rs *ReceiptService, used domain.Pass) error {
			_, err := rs.MyPDF(t.Context(), f.principal(t, f.anna), used.ReceiptID)

			return err
		},
		"Settings": func(t *testing.T, _ *passFixture, rs *ReceiptService, _ domain.Pass) error {
			_, err := rs.Settings(t.Context())

			return err
		},
		"Logo": func(t *testing.T, _ *passFixture, rs *ReceiptService, _ domain.Pass) error {
			_, err := rs.Logo(t.Context())

			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			SweepFaults(t, FaultSweep{Setup: func(t *testing.T) (*FaultStore, func() error) {
				f, fs, rs, used := receiptReads(t)

				return fs, func() error { return read(t, f, rs, used) }
			}})
		})
	}
}

// TestReceiptPDFRenderStoreFaults: a store failure while the PDF of a
// receipt without one (a seeded Storno, rendered with the receipt it
// cancels) is read, rendered and stored is returned, and nothing is
// stored: the next read renders it again.
func TestReceiptPDFRenderStoreFaults(t *testing.T) {
	t.Parallel()

	SweepFaults(t, FaultSweep{Setup: func(t *testing.T) (*FaultStore, func() error) {
		f := newPassFixture(t, domain.RedeemOnAttendance)
		_, r := f.newSeededCancellation(t)
		fs, _, rs := f.faulty(t)

		return fs, func() error {
			_, err := rs.MyPDF(t.Context(), f.principal(t, f.anna), r.ID)

			return err
		}
	}})
}

// TestReceiptSettingsWriteStoreFaults: a store failure while the receipt
// settings or the logo are written is returned. SetSettings reads the
// result back in a second transaction: a failure there is returned too,
// but the settings were stored.
func TestReceiptSettingsWriteStoreFaults(t *testing.T) {
	t.Parallel()

	t.Run("SetSettings", func(t *testing.T) {
		t.Parallel()

		var calls []FaultCall

		SweepFaults(t, FaultSweep{
			Setup: cleanCalls(func(t *testing.T) (*FaultStore, func() error) {
				f := newPassFixture(t, domain.RedeemOnAttendance)
				fs, _, rs := f.faulty(t)

				return fs, func() error {
					_, err := rs.SetSettings(t.Context(), domain.ReceiptSettings{
						Issuer:  domain.ReceiptIssuer{Name: "Example School", Address: "Street 1", TaxID: "1"},
						TaxMode: domain.TaxSmallBusiness,
					}, "EX")

					return err
				}
			}, &calls),
			Check: func(t *testing.T, fs *FaultStore, before StoreData, call FaultCall) {
				changed := before.Changed(fs.Snapshot(t))
				if afterFirstCommit(calls, call) {
					if !slices.Equal(changed, []string{"settings"}) {
						t.Errorf("with %s failing in the read-back, changed %v, want the stored settings", call, changed)
					}

					return
				}

				if len(changed) > 0 {
					t.Errorf("with %s failing, the store changed: %v", call, changed)
				}
			},
		})
	})

	t.Run("SetLogo", func(t *testing.T) {
		t.Parallel()

		SweepFaults(t, FaultSweep{Setup: func(t *testing.T) (*FaultStore, func() error) {
			f := newPassFixture(t, domain.RedeemOnAttendance)
			fs, _, rs := f.faulty(t)

			return fs, func() error {
				return rs.SetLogo(t.Context(), domain.Logo{ContentType: domain.LogoPNG, Data: []byte("\x89PNG\r\n\x1a\nlogo")})
			}
		}})
	})
}

// TestReceiptPDFStoredMeanwhile: when another request stores the PDF while
// this one renders it, the stored PDF wins: both return the same bytes.
func TestReceiptPDFStoredMeanwhile(t *testing.T) {
	t.Parallel()

	f := newPassFixture(t, domain.RedeemOnAttendance)
	r := f.newSeededReceipt(t)
	theirs := []byte("%PDF-theirs")

	f.pdf.during = func() {
		f.pdf.during = nil

		f.write(t, func(ctx context.Context, tx store.Tx) error { return tx.Receipts().PutPDF(ctx, r.ID, theirs) })
	}

	file, err := f.receiptService(t, bookingNow).PDF(t.Context(), r.ID)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(file.PDF, theirs) || file.Filename != r.Number+".pdf" || f.pdf.count() != 1 {
		t.Fatalf("PDF %q (%s) after %d renders, want the stored one after one", file.PDF, file.Filename, f.pdf.count())
	}

	if _, pdf := f.receipt(t, r.ID); !bytes.Equal(pdf, theirs) {
		t.Fatalf("stored %q, want the other request's", pdf)
	}
}

// TestReceiptPDFRenderFails: a failing render of a receipt without a PDF is
// an internal error and stores nothing; once the renderer works, the PDF
// is rendered and stored.
func TestReceiptPDFRenderFails(t *testing.T) {
	t.Parallel()

	f := newPassFixture(t, domain.RedeemOnAttendance)
	rs := f.receiptService(t, bookingNow)
	r := f.newSeededReceipt(t)

	f.pdf.fail(errors.New("typst: out of memory"))

	if _, err := rs.PDF(t.Context(), r.ID); err == nil || errors.Is(err, domain.ErrValidation) ||
		!strings.Contains(err.Error(), r.Number) {
		t.Fatalf("PDF = %v, want an internal error naming the receipt", err)
	}

	f.pdf.fail(nil)

	file, err := rs.PDF(t.Context(), r.ID)
	if _, stored := f.receipt(t, r.ID); err != nil || !bytes.Equal(stored, file.PDF) {
		t.Fatalf("PDF after the renderer recovered: %v, stored %q", err, stored)
	}
}

// newSeededCancellation stores a seeded receipt and its Storno, both
// without a PDF, and returns them.
func (f *passFixture) newSeededCancellation(t *testing.T) (orig, c domain.Receipt) {
	t.Helper()

	orig = f.newSeededReceipt(t)
	line := orig.Lines[0].Negated()
	c = orig
	c.ID, c.Number, c.Sequence = ids.New[domain.ReceiptID](f.seq), "SD-2026-0002", 2
	c.Kind, c.CancelsReceiptID = domain.ReceiptCancellation, orig.ID
	c.Lines, c.Net, c.VAT, c.Gross = []domain.ReceiptLine{line}, line.Net, line.VAT, line.Gross
	f.write(t, func(ctx context.Context, tx store.Tx) error {
		if _, err := tx.ReceiptCounters().Next(ctx, 2026); err != nil {
			return err
		}

		return tx.Receipts().Create(ctx, c)
	})

	return orig, c
}

// TestReceiptPDFOfSeededCancellation renders the PDF of a seeded Storno
// that has none with the receipt it cancels: it names the original.
func TestReceiptPDFOfSeededCancellation(t *testing.T) {
	t.Parallel()

	f := newPassFixture(t, domain.RedeemOnAttendance)
	orig, c := f.newSeededCancellation(t)

	file, err := f.receiptService(t, bookingNow).PDF(t.Context(), c.ID)
	if err != nil {
		t.Fatal(err)
	}

	if d := doc(t, file.PDF); d.Heading != "Cancellation" || !strings.Contains(d.Intro, orig.Number) {
		t.Fatalf("printed %q: %q, want the cancellation of %s", d.Heading, d.Intro, orig.Number)
	}
}

// TestReceiptsWithoutPerson: an account without a person (an admin's) paid
// nothing: no receipts of its own and no PDF.
func TestReceiptsWithoutPerson(t *testing.T) {
	t.Parallel()

	f := newPassFixture(t, domain.RedeemOnAttendance)
	rs := f.receiptService(t, bookingNow)
	r := f.newSeededReceipt(t)
	nobody := Principal{AccountID: f.office.AccountID, Role: domain.RoleAdmin}

	mine, err := rs.Mine(t.Context(), nobody)
	if err != nil || mine == nil || len(mine) != 0 {
		t.Errorf("Mine = %#v, %v, want an empty list", mine, err)
	}

	if _, err := rs.MyPDF(t.Context(), nobody, r.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("MyPDF = %v, want domain.ErrNotFound", err)
	}
}
