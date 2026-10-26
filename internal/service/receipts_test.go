package service

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/mail"
	"github.com/cwbudde/studio-schedule/internal/pdf/pdftext"
	"github.com/cwbudde/studio-schedule/internal/pdf/typstwasm"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// fakePDF "renders" the template data as a PDF stand-in, "%PDF-fake" and the
// JSON, so tests can read what the template would print. It fails while err
// is set and counts its renders.
type fakePDF struct {
	mu    sync.Mutex
	err   error
	calls int
	// during runs within each render.
	during func()
}

func (f *fakePDF) Render(_ context.Context, input []byte) ([]byte, error) {
	f.mu.Lock()
	during := f.during
	f.mu.Unlock()

	if during != nil {
		during()
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	f.calls++
	if f.err != nil {
		return nil, f.err
	}

	n := binary.LittleEndian.Uint32(input)

	return append([]byte("%PDF-fake"), input[4:4+n]...), nil
}

func (f *fakePDF) fail(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.err = err
}

func (f *fakePDF) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.calls
}

// doc decodes the template data of a fake PDF.
func doc(t *testing.T, pdf []byte) receiptDoc {
	t.Helper()

	var d receiptDoc
	if err := json.Unmarshal(bytes.TrimPrefix(pdf, []byte("%PDF-fake")), &d); err != nil {
		t.Fatalf("not a fake PDF: %v", err)
	}

	return d
}

func (f *passFixture) receipt(t *testing.T, id domain.ReceiptID) (domain.Receipt, []byte) {
	t.Helper()

	var (
		r   domain.Receipt
		pdf []byte
	)

	err := f.st.InTx(t.Context(), store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		var err error
		if r, err = tx.Receipts().Get(ctx, id); err != nil {
			return err
		}

		pdf, err = tx.Receipts().PDF(ctx, id)

		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	return r, pdf
}

func (f *passFixture) updateSettings(t *testing.T, change func(*domain.Settings)) {
	t.Helper()

	f.write(t, func(ctx context.Context, tx store.Tx) error {
		s, err := tx.Settings().Get(ctx)
		if err != nil {
			return err
		}

		change(&s)

		return tx.Settings().Put(ctx, s)
	})
}

func (f *passFixture) receiptCount(t *testing.T) int {
	t.Helper()

	var n int

	err := f.st.InTx(t.Context(), store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		_, total, err := tx.Receipts().List(ctx, store.ReceiptFilter{})
		n = total

		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	return n
}

// TestSellIssuesReceipt: a sale issues its receipt in the same
// transaction, numbered per year, with the price as a gross line taxed at
// the type's rate, the PDF stored and mailed to the payer's account.
func TestSellIssuesReceipt(t *testing.T) {
	t.Parallel()

	f := newPassFixture(t, domain.RedeemOnAttendance)
	svc := f.passService(t, bookingNow)

	v, err := svc.Sell(t.Context(), f.office, f.mia.ID, PassSale{
		PassTypeID: f.tenVisits.ID, PayerID: f.anna.ID, PaymentMethod: domain.PaymentCard,
		// The typed address is not kept: Anna has an account.
		Recipient: ReceiptRecipient{Address: "Example Street 1\n00000 Example Town", Email: "anna.old@example.org"},
	})
	if err != nil {
		t.Fatal(err)
	}

	if v.Pass.ReceiptID.IsZero() {
		t.Fatal("the sale has no receipt")
	}

	r, pdf := f.receipt(t, v.Pass.ReceiptID)

	if r.Number != "SD-2026-0001" || r.Year != 2026 || r.Sequence != 1 || r.Kind != domain.ReceiptSale {
		t.Errorf("receipt %s (%d/%d, %s), want the first sale of 2026", r.Number, r.Year, r.Sequence, r.Kind)
	}

	if r.PayerID != f.anna.ID || r.RecipientName != "Anna Example" || r.PaymentMethod != domain.PaymentCard ||
		r.RecipientEmail != "" ||
		r.RecipientAddress != "Example Street 1\n00000 Example Town" || r.CreatedBy != f.office.AccountID {
		t.Errorf("payer %v %q, method %s, address %q, by %v", r.PayerID, r.RecipientName, r.PaymentMethod,
			r.RecipientAddress, r.CreatedBy)
	}

	if r.TaxMode != domain.TaxStandard || r.Issuer.Name != "Studio Demo Dance School" || r.Locale != "en" {
		t.Errorf("tax mode %s, issuer %q, locale %q: want the school's settings", r.TaxMode, r.Issuer.Name, r.Locale)
	}

	// 120.00 gross at 19 %: net 100.84, VAT 19.16.
	want := domain.ReceiptLine{
		Position: 1, Description: "10-visit pass for Mia Example, valid 21 Oct 2026 – 18 Apr 2027", Quantity: 1,
		UnitPrice: 12000, VATRate: 1900, Net: 10084, VAT: 1916, Gross: 12000,
	}
	if len(r.Lines) != 1 || r.Lines[0] != want || r.Gross != 12000 || r.Net != 10084 || r.VAT != 1916 {
		t.Errorf("lines %+v, totals %d/%d/%d; want %+v", r.Lines, r.Net, r.VAT, r.Gross, want)
	}

	d := doc(t, pdf)
	if d.Title != "Receipt SD-2026-0001" || d.Total[1] != "€120.00" || !d.ShowVAT ||
		len(d.Summary) != 2 || d.Summary[1] != [2]string{"VAT 19%", "€19.16"} {
		t.Errorf("printed %+v", d)
	}

	mails := f.mailsOf(t, mail.TemplateReceipt)
	// Anna reads German, the receipt is English (the school's language):
	// the mail is German throughout.
	if len(mails) != 1 || mails[0].To != "anna@example.org" || mails[0].ReceiptID != r.ID ||
		!strings.Contains(mails[0].Subject, "SD-2026-0001") || !strings.Contains(mails[0].Text, "120,00") ||
		!strings.Contains(mails[0].Text, "10-visit pass für Mia Example, gültig 21.10.2026 – 18.04.2027") {
		t.Fatalf("mails %+v, want Anna's in German with the receipt attached", mails)
	}

	// The next sale takes the next number.
	next := f.sell(t, f.anna, f.single, "")
	if r, _ := f.receipt(t, next.ReceiptID); r.Number != "SD-2026-0002" {
		t.Errorf("second receipt %s, want SD-2026-0002", r.Number)
	}
}

// TestSellTypedRecipient mails the receipt to the typed address when the
// payer has no account, and to nobody without one.
func TestSellTypedRecipient(t *testing.T) {
	t.Parallel()

	f := newPassFixture(t, domain.RedeemOnAttendance)
	svc := f.passService(t, bookingNow)

	v, err := svc.Sell(t.Context(), f.office, f.mia.ID, PassSale{
		PassTypeID: f.single.ID, PaymentMethod: domain.PaymentCash,
		Recipient: ReceiptRecipient{Name: "Mia's grandma", Email: "grandma@example.org"},
	})
	if err != nil {
		t.Fatal(err)
	}

	r, _ := f.receipt(t, v.Pass.ReceiptID)
	if r.PayerID != f.mia.ID || r.RecipientName != "Mia's grandma" || r.RecipientEmail != "grandma@example.org" {
		t.Errorf("receipt %+v", r)
	}

	if _, err := svc.Sell(t.Context(), f.office, f.mia.ID, PassSale{
		PassTypeID: f.single.ID, PaymentMethod: domain.PaymentCash,
	}); err != nil {
		t.Fatal(err)
	}

	mails := f.mailsOf(t, mail.TemplateReceipt)
	if len(mails) != 1 || mails[0].To != "grandma@example.org" || !strings.Contains(mails[0].Text, "€15.00") {
		t.Fatalf("mails %+v, want one to grandma in English", mails)
	}

	_, err = svc.Sell(t.Context(), f.office, f.mia.ID, PassSale{
		PassTypeID: f.single.ID, PaymentMethod: domain.PaymentCash, Recipient: ReceiptRecipient{Email: "no address"},
	})

	var fe *domain.FieldError
	if !errors.As(err, &fe) || fe.Field != "recipientEmail" {
		t.Fatalf("an invalid address: %v, want a field error on recipientEmail", err)
	}

	_, err = svc.Sell(t.Context(), f.office, f.mia.ID, PassSale{PassTypeID: f.single.ID, PaymentMethod: "barter"})
	if !errors.As(err, &fe) || fe.Field != "paymentMethod" {
		t.Fatalf("an unknown method: %v, want a field error on paymentMethod", err)
	}
}

// TestSellNeedsReceiptSettings refuses a sale without complete receipt
// settings, and a failing render: the sale is atomic with its receipt, and a
// refused one takes no number.
func TestSellNeedsReceiptSettings(t *testing.T) {
	t.Parallel()

	f := newPassFixture(t, domain.RedeemOnAttendance)
	svc := f.passService(t, bookingNow)
	sale := PassSale{PassTypeID: f.single.ID, PaymentMethod: domain.PaymentCash}

	f.updateSettings(t, func(s *domain.Settings) { s.Receipts.TaxMode = "" })

	_, err := svc.Sell(t.Context(), f.office, f.mia.ID, sale)
	if !errors.Is(err, ErrReceiptSettingsIncomplete) || !strings.Contains(err.Error(), "tax mode") {
		t.Fatalf("got %v, want ErrReceiptSettingsIncomplete naming the tax mode", err)
	}

	f.updateSettings(t, func(s *domain.Settings) { s.Receipts.TaxMode = domain.TaxStandard })
	f.pdf.fail(errors.New("typst: boom"))

	if _, err := svc.Sell(t.Context(), f.office, f.mia.ID, sale); err == nil || errors.Is(err, domain.ErrValidation) {
		t.Fatalf("a failing render: %v, want an internal error", err)
	}

	if n := f.receiptCount(t); n != 0 {
		t.Fatalf("%d receipts after refused sales", n)
	}

	if passes, _ := f.passService(t, bookingNow).PersonPasses(t.Context(), f.mia.ID); len(passes) != 0 {
		t.Fatalf("%d passes after refused sales", len(passes))
	}

	f.pdf.fail(nil)

	if p := f.sell(t, f.mia, f.single, ""); p.ReceiptID.IsZero() {
		t.Fatal("no receipt")
	} else if r, _ := f.receipt(t, p.ReceiptID); r.Number != "SD-2026-0001" {
		t.Fatalf("number %s after refused sales, want SD-2026-0001: no gap", r.Number)
	}

	// Without a renderer nothing is sold.
	bare := NewPassService(f.st, clock.NewFixed(mustInstant(t, bookingNow)), f.seq,
		NewReceiptService(f.st, clock.NewFixed(mustInstant(t, bookingNow)), f.seq, Mails{}, nil))
	if _, err := bare.Sell(t.Context(), f.office, f.mia.ID, sale); !errors.Is(err, ErrNoPDFRenderer) {
		t.Fatalf("without a renderer: %v", err)
	}
}

// blockingPDF renders only when its context ends, a hung renderer; Ready
// counts its calls.
type blockingPDF struct{ ready *atomic.Int32 }

func (blockingPDF) Render(ctx context.Context, _ []byte) ([]byte, error) {
	<-ctx.Done()

	return nil, ctx.Err()
}

func (b blockingPDF) Ready(context.Context) error {
	b.ready.Add(1)

	return nil
}

// TestReceiptRenderTimeout: a hung render fails the sale at the render
// timeout instead of holding the write transaction, and the renderer was
// awaited before it.
func TestReceiptRenderTimeout(t *testing.T) {
	t.Parallel()

	f := newPassFixture(t, domain.RedeemOnAttendance)
	clk := clock.NewFixed(mustInstant(t, bookingNow))

	pdf := blockingPDF{ready: new(atomic.Int32)}
	rs := NewReceiptService(f.st, clk, f.seq, Mails{}, pdf)
	rs.renderTimeout = 50 * time.Millisecond

	_, err := NewPassService(f.st, clk, f.seq, rs).Sell(t.Context(), f.office, f.mia.ID, PassSale{PassTypeID: f.single.ID})
	if err == nil || !strings.Contains(err.Error(), context.DeadlineExceeded.Error()) {
		t.Fatalf("Sell error = %v, want the render's deadline", err)
	}

	if n := f.receiptCount(t); n != 0 {
		t.Errorf("%d receipts after the failed sale, want none", n)
	}

	if got := pdf.ready.Load(); got != 1 {
		t.Errorf("Ready calls = %d, want 1 (before the transaction)", got)
	}
}

// TestReceiptTaxModes prints the tax data of each mode.
func TestReceiptTaxModes(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		mode      domain.TaxMode
		note      string
		vat       domain.Money
		showVAT   bool
		wantNote  string
		wantLines int
	}{
		{domain.TaxStandard, "", 1916, true, "Paid in cash.", 2},
		{domain.TaxSmallBusiness, "", 0, false, "No VAT is charged under the small business rule (§ 19 UStG).", 0},
		{domain.TaxExempt, "Exempt under Example Law § 4", 0, false, "Exempt under Example Law § 4", 0},
	} {
		t.Run(string(c.mode), func(t *testing.T) {
			t.Parallel()

			f := newPassFixture(t, domain.RedeemOnAttendance)
			f.updateSettings(t, func(s *domain.Settings) { s.Receipts.TaxMode, s.Receipts.TaxNote = c.mode, c.note })

			r, pdf := f.receipt(t, f.sell(t, f.anna, f.tenVisits, "").ReceiptID)
			if r.VAT != c.vat || r.Gross != 12000 || r.Net != 12000-c.vat || r.TaxMode != c.mode || r.TaxNote != c.note {
				t.Errorf("receipt %+v", r)
			}

			d := doc(t, pdf)
			if d.ShowVAT != c.showVAT || len(d.Summary) != c.wantLines || d.Notes[0] != c.wantNote {
				t.Errorf("printed VAT %v, summary %v, notes %v", d.ShowVAT, d.Summary, d.Notes)
			}
		})
	}
}

// TestReceiptYearInSchoolTime numbers a receipt by the school's date: half
// past midnight on New Year's Day in Berlin is still the old year in UTC.
func TestReceiptYearInSchoolTime(t *testing.T) {
	t.Parallel()

	f := newPassFixture(t, domain.RedeemOnAttendance)

	v, err := f.passService(t, "2026-12-31T23:30:00Z").Sell(t.Context(), f.office, f.anna.ID,
		PassSale{PassTypeID: f.single.ID, PaymentMethod: domain.PaymentCash})
	if err != nil {
		t.Fatal(err)
	}

	r, _ := f.receipt(t, v.Pass.ReceiptID)
	if r.Number != "SD-2027-0001" || r.IssueDate != (domain.Date{Year: 2027, Month: 1, Day: 1}) {
		t.Fatalf("receipt %s of %v, want SD-2027-0001 of 2027-01-01", r.Number, r.IssueDate)
	}
}

// TestCancelReceipt: a Storno negates the receipt, voids the unused pass,
// and refuses a used pass, a second Storno and a Storno of a Storno.
func TestCancelReceipt(t *testing.T) {
	t.Parallel()

	f := newPassFixture(t, domain.RedeemOnBooking)
	rs := f.receiptService(t, bookingNow)
	ctx := t.Context()

	pass := f.sell(t, f.anna, f.tenVisits, "")
	orig, _ := f.receipt(t, pass.ReceiptID)

	v, err := rs.Cancel(ctx, f.office, orig.ID, " Sold to the wrong person ")
	if err != nil {
		t.Fatal(err)
	}

	c, pdf := f.receipt(t, v.Receipt.ID)
	if c.Kind != domain.ReceiptCancellation || c.CancelsReceiptID != orig.ID || c.Number != "SD-2026-0002" ||
		c.Gross != -orig.Gross || c.Net != -orig.Net || c.VAT != -orig.VAT || c.Lines[0] != orig.Lines[0].Negated() ||
		c.Reason != "Sold to the wrong person" || c.PayerID != orig.PayerID || c.PaymentMethod != orig.PaymentMethod {
		t.Fatalf("cancellation %+v of %+v", c, orig)
	}

	if v.Cancels == nil || v.Cancels.ID != orig.ID || v.Cancelable() {
		t.Errorf("view %+v", v)
	}

	d := doc(t, pdf)
	if d.Heading != "Cancellation" || d.Total[1] != "−€120.00" || !strings.Contains(d.Intro, orig.Number) {
		t.Errorf("printed %+v", d)
	}

	if p := f.pass(t, pass.ID); p.Status != domain.PassVoided {
		t.Errorf("pass %s, want voided", p.Status)
	}

	if mails := f.mailsOf(t, mail.TemplateReceipt); len(mails) != 2 || mails[1].ReceiptID != c.ID {
		t.Errorf("mails %+v, want the receipt and the cancellation to Anna", mails)
	}

	if _, err := rs.Cancel(ctx, f.office, orig.ID, ""); !errors.Is(err, ErrReceiptCanceled) {
		t.Errorf("second Storno: %v", err)
	}

	if _, err := rs.Cancel(ctx, f.office, c.ID, ""); !errors.Is(err, ErrCancelCancellation) {
		t.Errorf("Storno of a Storno: %v", err)
	}

	if _, err := rs.Cancel(ctx, f.office, ids.New[domain.ReceiptID](f.seq), ""); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("unknown receipt: %v", err)
	}

	// A pass that paid for a visit keeps its receipt.
	used := f.sell(t, f.mia, f.tenVisits, "")
	lesson := f.regular(t, "2026-10-23T16:00:00Z")

	if _, err := f.bookingService(t, bookingNow).Book(ctx, f.principal(t, f.anna), lesson.ID, f.mia.ID); err != nil {
		t.Fatal(err)
	}

	before := f.receiptCount(t)

	if _, err := rs.Cancel(ctx, f.office, used.ReceiptID, ""); !errors.Is(err, ErrPassUsed) {
		t.Fatalf("Storno of a used pass: %v", err)
	}

	if p := f.pass(t, used.ID); p.Status != domain.PassActive || f.receiptCount(t) != before {
		t.Fatalf("a refused Storno changed the pass (%s) or issued a receipt", p.Status)
	}

	view, err := rs.Get(ctx, used.ReceiptID)
	if err != nil || view.Cancelable() || !view.PassUsed || view.Pass.ID != used.ID {
		t.Fatalf("view %+v, %v: want the used pass, not cancelable", view, err)
	}
}

// TestCancelReceiptOfMergedPayer mails the Storno of a receipt whose payer
// was merged (ADR 0015) to the survivor's account; the receipts keep the
// tombstone as their payer.
func TestCancelReceiptOfMergedPayer(t *testing.T) {
	t.Parallel()

	f := newPassFixture(t, domain.RedeemOnBooking)
	ctx := t.Context()

	// Anna's duplicate, without an account, paid for Mia's pass: the sale
	// mailed nobody.
	dup := f.person(t, "Anna", "", "", "")

	v, err := f.passService(t, bookingNow).Sell(ctx, f.office, f.mia.ID, PassSale{PassTypeID: f.tenVisits.ID, PayerID: dup.ID})
	if err != nil {
		t.Fatal(err)
	}

	if mails := f.mailsOf(t, mail.TemplateReceipt); len(mails) != 0 {
		t.Fatalf("mails %+v, want none", mails)
	}

	persons := NewPersonService(f.st, clock.NewFixed(mustInstant(t, bookingNow)), f.seq, Mails{})
	if _, err := persons.Merge(ctx, f.office, MergeRequest{SurvivorID: f.anna.ID, MergedID: dup.ID}, true); err != nil {
		t.Fatal(err)
	}

	c, err := f.receiptService(t, bookingNow).Cancel(ctx, f.office, v.Pass.ReceiptID, "")
	if err != nil {
		t.Fatal(err)
	}

	if r, _ := f.receipt(t, c.Receipt.ID); r.PayerID != dup.ID {
		t.Errorf("cancellation payer %v, want the tombstone", r.PayerID)
	}

	mails := f.mailsOf(t, mail.TemplateReceipt)
	if len(mails) != 1 || mails[0].To != "anna@example.org" || mails[0].ReceiptID != c.Receipt.ID {
		t.Fatalf("mails %+v, want the cancellation to Anna's account", mails)
	}
}

// TestReceiptPDFRenderedOnce renders the PDF of a receipt that has none
// (the seed's) on the first read, and stores it: later reads return the
// same bytes without rendering. Members read only what they paid.
func TestReceiptPDFRenderedOnce(t *testing.T) {
	t.Parallel()

	f := newPassFixture(t, domain.RedeemOnAttendance)
	rs := f.receiptService(t, bookingNow)
	r := f.newSeededReceipt(t)

	// The render runs outside any transaction: the store takes a write
	// meanwhile (the memory store would wait for the render forever).
	var during error

	f.pdf.during = func() {
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()

		during = f.st.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
			_, err := tx.ReceiptCounters().Next(ctx, 2030)

			return err
		})
	}

	file, err := rs.PDF(t.Context(), r.ID)
	if during != nil {
		t.Fatalf("a write during the render: %v", during)
	}

	if err != nil {
		t.Fatal(err)
	}

	if file.Filename != r.Number+".pdf" || doc(t, file.PDF).Title != "Receipt "+r.Number || f.pdf.count() != 1 {
		t.Fatalf("file %s, %d renders", file.Filename, f.pdf.count())
	}

	again, err := rs.PDF(t.Context(), r.ID)
	if err != nil || !bytes.Equal(again.PDF, file.PDF) || f.pdf.count() != 1 {
		t.Fatalf("second read rendered again (%d) or differs: %v", f.pdf.count(), err)
	}

	if _, err := rs.MyPDF(t.Context(), f.principal(t, f.anna), r.ID); err != nil {
		t.Errorf("Anna's own receipt: %v", err)
	}

	if _, err := rs.MyPDF(t.Context(), f.principal(t, f.tom), r.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("Tom read Anna's receipt: %v", err)
	}

	mine, err := rs.Mine(t.Context(), f.principal(t, f.tom))
	if err != nil || len(mine) != 0 {
		t.Errorf("Tom's receipts %+v, %v", mine, err)
	}

	mine, err = rs.Mine(t.Context(), f.principal(t, f.anna))
	if err != nil || len(mine) != 1 || mine[0].Receipt.ID != r.ID || mine[0].PayerName != "Anna Example" {
		t.Errorf("Anna's receipts %+v, %v", mine, err)
	}
}

// newSeededReceipt stores a receipt paid by Anna without a PDF, as the seed
// does.
func (f *passFixture) newSeededReceipt(t *testing.T) domain.Receipt {
	t.Helper()

	line := domain.NewReceiptLine(1, "Single visit for Anna Example", 1, 1500, 0, domain.TaxStandard)
	r := domain.Receipt{
		ID: ids.New[domain.ReceiptID](f.seq), Number: "SD-2026-0001", Year: 2026, Sequence: 1, Kind: domain.ReceiptSale,
		IssuedAt: mustInstant(t, bookingNow), IssueDate: domain.Date{Year: 2026, Month: 10, Day: 21}, Locale: "en",
		Issuer:  domain.ReceiptIssuer{Name: "Studio Demo Dance School", Address: "Pirouette Lane 7", TaxID: "99"},
		TaxMode: domain.TaxStandard, PayerID: f.anna.ID, RecipientName: "Anna Example",
		PaymentMethod: domain.PaymentCash, Currency: "EUR", Net: line.Net, VAT: line.VAT, Gross: line.Gross,
		Lines: []domain.ReceiptLine{line},
	}
	f.write(t, func(ctx context.Context, tx store.Tx) error {
		if _, err := tx.ReceiptCounters().Next(ctx, 2026); err != nil {
			return err
		}

		return tx.Receipts().Create(ctx, r)
	})

	return r
}

// TestReceiptsList filters and pages the receipts.
func TestReceiptsList(t *testing.T) {
	t.Parallel()

	f := newPassFixture(t, domain.RedeemOnAttendance)
	rs := f.receiptService(t, bookingNow)

	f.sell(t, f.anna, f.single, "")
	second := f.sell(t, f.mia, f.single, "")

	if _, err := rs.Cancel(t.Context(), f.office, second.ReceiptID, ""); err != nil {
		t.Fatal(err)
	}

	page, err := rs.List(t.Context(), store.ReceiptFilter{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}

	if page.Total != 3 || len(page.Items) != 2 || page.Items[0].Receipt.Number != "SD-2026-0003" ||
		page.Items[1].CanceledBy == nil || page.Items[1].CanceledBy.Number != "SD-2026-0003" {
		t.Fatalf("page %+v", page)
	}

	page, err = rs.List(t.Context(), store.ReceiptFilter{Query: " sd-2026-0001 ", Limit: 500})
	if err != nil || page.Total != 1 || !page.Items[0].Cancelable() {
		t.Fatalf("query: %+v, %v", page, err)
	}
}

// TestReceiptSettings validates and keeps the receipt settings and the
// logo.
func TestReceiptSettings(t *testing.T) {
	t.Parallel()

	f := newPassFixture(t, domain.RedeemOnAttendance)
	rs := f.receiptService(t, bookingNow)
	ctx := t.Context()

	got, err := rs.SetSettings(ctx, domain.ReceiptSettings{
		Issuer:  domain.ReceiptIssuer{Name: " Example School ", Address: " Street 1 \r\n\r\n 00000 Town ", TaxID: "1"},
		TaxMode: domain.TaxExempt,
	}, "EX")
	if err != nil {
		t.Fatal(err)
	}

	if got.Settings.Issuer.Address != "Street 1\n00000 Town" || got.Prefix != "EX" || got.Missing != "taxNote" {
		t.Fatalf("settings %+v", got)
	}

	var fe *domain.FieldError

	for field, change := range map[string]func(*domain.ReceiptSettings, *string){
		"email":         func(s *domain.ReceiptSettings, _ *string) { s.Issuer.Email = "office" },
		"taxMode":       func(s *domain.ReceiptSettings, _ *string) { s.TaxMode = "none" },
		"receiptPrefix": func(_ *domain.ReceiptSettings, p *string) { *p = "ex-1" },
		"taxNumber":     func(s *domain.ReceiptSettings, _ *string) { s.Issuer.TaxID = "1\n2" },
	} {
		s, prefix := got.Settings, "EX"
		change(&s, &prefix)

		if _, err := rs.SetSettings(ctx, s, prefix); !errors.As(err, &fe) || fe.Field != field {
			t.Errorf("%s: %v", field, err)
		}
	}

	png := domain.Logo{ContentType: domain.LogoPNG, Data: []byte("\x89PNG\r\n\x1a\nlogo")}
	if err := rs.SetLogo(ctx, png); err != nil {
		t.Fatal(err)
	}

	if s, _ := rs.Settings(ctx); s.LogoType != domain.LogoPNG {
		t.Errorf("logo type %q", s.LogoType)
	}

	if err := rs.SetLogo(ctx, domain.Logo{ContentType: "image/gif", Data: []byte("GIF89a")}); !errors.As(err, &fe) {
		t.Errorf("a GIF: %v", err)
	}

	big := domain.Logo{ContentType: domain.LogoPNG, Data: append(png.Data, make([]byte, domain.MaxLogoBytes)...)}
	if err := rs.SetLogo(ctx, big); !errors.As(err, &fe) || fe.Field != "data" {
		t.Errorf("a large logo: %v", err)
	}

	if err := rs.SetLogo(ctx, domain.Logo{}); err != nil {
		t.Fatal(err)
	}

	if l, _ := rs.Logo(ctx); !l.IsZero() {
		t.Error("logo not removed")
	}
}

var typst = sync.OnceValue(func() *typstwasm.Renderer {
	opts := typstwasm.Options{}
	if dir, err := os.UserCacheDir(); err == nil {
		opts.CacheDir = filepath.Join(dir, "studio-schedule", "wazero-test")
	}

	return typstwasm.New(opts)
})

// TestReceiptPDFWithTypst renders a sale and its cancellation with the real
// typst module: the PDFs show the receipts' data in the school's language.
func TestReceiptPDFWithTypst(t *testing.T) {
	t.Parallel()

	f := newPassFixture(t, domain.RedeemOnAttendance)
	f.updateSettings(t, func(s *domain.Settings) { s.DefaultLocale = "de" })

	clk := clock.NewFixed(mustInstant(t, bookingNow))
	rs := NewReceiptService(f.st, clk, f.seq, testMails(newTestRenderer(t)), typst())
	ps := NewPassService(f.st, clk, f.seq, rs)

	v, err := ps.Sell(t.Context(), f.office, f.mia.ID, PassSale{
		PassTypeID: f.tenVisits.ID, PayerID: f.anna.ID, PaymentMethod: domain.PaymentTransfer,
	})
	if err != nil {
		t.Fatal(err)
	}

	c, err := rs.Cancel(t.Context(), f.office, v.Pass.ReceiptID, "Doppelt verkauft")
	if err != nil {
		t.Fatal(err)
	}

	for id, want := range map[domain.ReceiptID][]string{
		v.Pass.ReceiptID: {
			"Quittung", "SD-2026-0001", "21.10.2026", "Anna Example", "10-visit pass für Mia Example",
			"120,00\u00a0€", "100,84\u00a0€", "Umsatzsteuer 19\u00a0%", "19,16\u00a0€", "Per Überweisung bezahlt.",
			"Studio Demo Dance School", "Pirouette Lane 7", "Steuernummer 99/999/99999",
		},
		c.Receipt.ID: {
			"Stornobeleg", "SD-2026-0002", "Hiermit stornieren wir die Quittung SD-2026-0001 vom 21.10.2026",
			"−120,00\u00a0€", "Per Überweisung erstattet.", "Grund: Doppelt verkauft",
		},
	} {
		_, pdf := f.receipt(t, id)
		if !bytes.HasPrefix(pdf, []byte("%PDF-")) {
			t.Fatalf("not a PDF: %q", pdf[:min(len(pdf), 20)])
		}

		text, err := pdftext.Text(pdf)
		if err != nil {
			t.Fatal(err)
		}

		for _, s := range want {
			if !strings.Contains(text, s) {
				t.Errorf("the PDF does not show %q:\n%s", s, text)
			}
		}
	}
}

// capture records the messages it is asked to send.
type capture struct {
	mu   sync.Mutex
	sent []mail.Message
}

func (c *capture) Send(_ context.Context, m mail.Message) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.sent = append(c.sent, m)

	return nil
}

// TestDeliverAttachesReceipt sends the receipt mail with the stored PDF
// attached, named after the receipt.
func TestDeliverAttachesReceipt(t *testing.T) {
	t.Parallel()

	f := newPassFixture(t, domain.RedeemOnAttendance)
	pass := f.sell(t, f.anna, f.tenVisits, "")
	_, pdf := f.receipt(t, pass.ReceiptID)

	mailer := &capture{}
	clk := clock.NewFixed(mustInstant(t, bookingNow))
	outbox := NewOutboxService(f.st, clk, mailer, f.receiptService(t, bookingNow))

	if _, err := outbox.Deliver(t.Context()); err != nil {
		t.Fatal(err)
	}

	var receipt *mail.Message

	for i, m := range mailer.sent {
		if len(m.Attachments) > 0 {
			receipt = &mailer.sent[i]
		}
	}

	if receipt == nil || receipt.To != "anna@example.org" || len(receipt.Attachments) != 1 {
		t.Fatalf("sent %+v, want the receipt mail with one attachment", mailer.sent)
	}

	if a := receipt.Attachments[0]; a.Filename != "SD-2026-0001.pdf" || a.ContentType != "application/pdf" ||
		!bytes.Equal(a.Content, pdf) {
		t.Fatalf("attachment %s (%s), %d bytes", a.Filename, a.ContentType, len(a.Content))
	}

	entries, err := outbox.List(t.Context(), 10)
	if err != nil {
		t.Fatal(err)
	}

	found := false

	for _, e := range entries {
		if e.Mail.ReceiptID == pass.ReceiptID {
			found = e.Attachment == "SD-2026-0001.pdf"
		}
	}

	if !found {
		t.Fatalf("mailbox %+v, want the attachment's name", entries)
	}

	file, err := outbox.Attachment(t.Context(), entries[0].Mail.ID)
	if err != nil || !bytes.Equal(file.PDF, pdf) {
		t.Fatalf("attachment: %v", err)
	}
}

// TestReceiptPDFTaxModesWithTypst renders a sale and its Storno in every tax
// mode with the real typst module: each prints its tax data, and none fails
// for want of a list the mode has no use for.
func TestReceiptPDFTaxModesWithTypst(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		mode       domain.TaxMode
		note       string
		sale, stor []string
		absent     string
	}{
		{
			domain.TaxStandard, "",
			[]string{"Net (19% VAT)", "€100.84", "VAT 19%", "€19.16", "€120.00"},
			[]string{"−€100.84", "−€19.16", "−€120.00"},
			"",
		},
		{
			domain.TaxSmallBusiness, "",
			[]string{"No VAT is charged under the small business rule (§ 19 UStG).", "€120.00"},
			[]string{"No VAT is charged", "−€120.00"},
			"VAT 19%",
		},
		{
			domain.TaxExempt, "Exempt under Example Law § 4",
			[]string{"Exempt under Example Law § 4", "€120.00"},
			[]string{"Exempt under Example Law § 4", "−€120.00"},
			"VAT 19%",
		},
	} {
		t.Run(string(c.mode), func(t *testing.T) {
			t.Parallel()

			f := newPassFixture(t, domain.RedeemOnAttendance)
			f.updateSettings(t, func(s *domain.Settings) { s.Receipts.TaxMode, s.Receipts.TaxNote = c.mode, c.note })

			clk := clock.NewFixed(mustInstant(t, bookingNow))
			rs := NewReceiptService(f.st, clk, f.seq, testMails(newTestRenderer(t)), typst())
			ps := NewPassService(f.st, clk, f.seq, rs)

			v, err := ps.Sell(t.Context(), f.office, f.anna.ID, PassSale{PassTypeID: f.tenVisits.ID})
			if err != nil {
				t.Fatalf("sale: %v", err)
			}

			cv, err := rs.Cancel(t.Context(), f.office, v.Pass.ReceiptID, "")
			if err != nil {
				t.Fatalf("Storno: %v", err)
			}

			for id, want := range map[domain.ReceiptID][]string{v.Pass.ReceiptID: c.sale, cv.Receipt.ID: c.stor} {
				_, pdf := f.receipt(t, id)

				text, err := pdftext.Text(pdf)
				if err != nil {
					t.Fatal(err)
				}

				for _, s := range want {
					if !strings.Contains(text, s) {
						t.Errorf("the PDF does not show %q:\n%s", s, text)
					}
				}

				if c.absent != "" && strings.Contains(text, c.absent) {
					t.Errorf("the PDF shows %q:\n%s", c.absent, text)
				}
			}
		})
	}
}
