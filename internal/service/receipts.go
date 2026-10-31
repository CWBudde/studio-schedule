package service

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/mail"
	"github.com/cwbudde/studio-schedule/internal/receiptfmt"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// Receipts (ADR 0012) are payment confirmations for pass sales: issued in
// the sale's transaction with a gapless number per year, immutable, with the
// issuer, the tax data and the rendered PDF snapshotted, and mailed with the
// PDF to whoever paid. A cancellation receipt (Storno) negates one and voids
// its pass, if the pass was not used.

var (
	// ErrReceiptSettingsIncomplete refuses a receipt (and so a sale) while
	// the school's receipt settings lack the issuer or the tax mode.
	ErrReceiptSettingsIncomplete = fmt.Errorf("%w: the receipt settings are incomplete", domain.ErrConflict)
	// ErrPassUsed refuses to cancel the receipt of a pass that paid for
	// visits; the office voids the pass instead.
	ErrPassUsed = fmt.Errorf("%w: the pass paid for visits", domain.ErrConflict)
	// ErrReceiptCanceled refuses to cancel a receipt twice.
	ErrReceiptCanceled = fmt.Errorf("%w: the receipt is canceled already", domain.ErrConflict)
	// ErrCancelCancellation refuses to cancel a cancellation receipt.
	ErrCancelCancellation = fmt.Errorf("%w: the receipt is a cancellation", domain.ErrConflict)
	// ErrNoPDFRenderer reports an installation without a PDF renderer: it
	// cannot issue receipts.
	ErrNoPDFRenderer = errors.New("no PDF renderer configured")
)

// ReceiptRenderer renders the PDF of a receipt from the input of the typst
// module (pdf.Input). The server runs the module with wazero
// (internal/pdf/typstwasm), the web demo in the browser (internal/wasmbridge):
// the PDF renderer is a swappable adapter like the store and the mailer.
type ReceiptRenderer interface {
	Render(ctx context.Context, input []byte) ([]byte, error)
}

// readyRenderer is a ReceiptRenderer that can wait until it is ready to
// render: the server's once the module is compiled, the demo's once the
// module is downloaded and compiled.
type readyRenderer interface {
	Ready(ctx context.Context) error
}

// renderTimeout bounds a PDF render inside a write transaction (ADR 0012:
// the receipt is issued atomically with its sale or cancellation), so a
// hung renderer cannot hold the transaction, and with it the receipt-counter
// lock that serializes sales, indefinitely. It starts once the renderer is
// ready (awaitRenderer, before the transaction): a warm render takes
// milliseconds.
const renderTimeout = 30 * time.Second

// ReceiptService issues, cancels and lists receipts and keeps the receipt
// settings.
type ReceiptService struct {
	store    store.Store
	clock    clock.Clock
	ids      ids.Source
	mails    Mails
	renderer ReceiptRenderer
	// renderTimeout bounds one render; a field so tests can shrink it.
	renderTimeout time.Duration
}

// NewReceiptService returns a ReceiptService; renderer may be nil, and then
// receipts (and so sales) fail with ErrNoPDFRenderer.
func NewReceiptService(
	st store.Store, clk clock.Clock, src ids.Source, mails Mails, renderer ReceiptRenderer,
) *ReceiptService {
	return &ReceiptService{store: st, clock: clk, ids: src, mails: mails, renderer: renderer, renderTimeout: renderTimeout}
}

// awaitRenderer waits until the renderer is ready, if it can tell: called
// before the write transaction that issues a receipt, so renderTimeout
// bounds the render alone, never the module's compilation or download.
func (s *ReceiptService) awaitRenderer(ctx context.Context) error {
	if s == nil {
		return nil
	}

	r, ok := s.renderer.(readyRenderer)
	if !ok {
		return nil
	}

	if err := r.Ready(ctx); err != nil {
		// Not the caller's input: %v keeps it a 500.
		return fmt.Errorf("receipt renderer: %v", err) //nolint:errorlint // see above
	}

	return nil
}

// ReceiptRecipient is whom the receipt of a sale names and mails. Zero
// fields take the payer's: the name, no address, the payer's account email.
type ReceiptRecipient struct {
	Name    string
	Address string
	// Email is where the receipt goes if the payer has no account.
	Email string
}

// issueSale issues, in tx, the receipt of the sale of pass (of type t, to
// owner, paid by payer) by the office account p: it takes the next number of
// the year, prints the price paid as one line taxed in the school's tax mode
// at the type's VAT rate, renders and stores the PDF and queues the mail.
// The caller sets pass.ReceiptID and creates the pass afterwards.
func (s *ReceiptService) issueSale(
	ctx context.Context, tx store.Tx, settings domain.Settings, loc *time.Location, p Principal,
	pass domain.Pass, t domain.PassType, owner, payer domain.Person, method domain.PaymentMethod, to ReceiptRecipient,
) (domain.Receipt, error) {
	if err := receiptSettingsComplete(settings); err != nil {
		return domain.Receipt{}, err
	}

	r, err := s.newReceipt(ctx, tx, settings, loc, p, domain.ReceiptSale)
	if err != nil {
		return domain.Receipt{}, err
	}

	r.PayerID, r.PaymentMethod, r.Currency = payer.ID, method, settings.Currency
	r.RecipientName = cmp.Or(strings.TrimSpace(to.Name), fullName(payer))
	r.RecipientAddress = strings.TrimSpace(to.Address)

	// The typed address is kept only where it is used: a payer with an
	// account gets the receipt there.
	switch _, err := tx.Accounts().GetByPerson(ctx, payer.ID); {
	case errors.Is(err, domain.ErrNotFound):
		r.RecipientEmail = strings.TrimSpace(to.Email)
	case err != nil:
		return domain.Receipt{}, err
	}

	item := func(locale string) string {
		return receiptfmt.PassLine(locale, t.Name, fullName(owner), pass.ValidFrom, pass.ValidUntil)
	}
	r.Lines = []domain.ReceiptLine{domain.NewReceiptLine(1, item(r.Locale), 1, pass.PricePaid, t.VATRate, r.TaxMode)}
	r.Net, r.VAT, r.Gross = r.Lines[0].Net, r.Lines[0].VAT, r.Lines[0].Gross

	if err := s.issue(ctx, tx, r, nil, settings, payer.ID, item); err != nil {
		return domain.Receipt{}, err
	}

	return r, nil
}

// receiptSettingsComplete reports incomplete receipt settings as
// ErrReceiptSettingsIncomplete, naming what is missing.
func receiptSettingsComplete(settings domain.Settings) error {
	if err := settings.Receipts.Complete(); err != nil {
		var fe *domain.FieldError
		if errors.As(err, &fe) {
			return fmt.Errorf("%w: %s", ErrReceiptSettingsIncomplete, fe.Msg)
		}

		return fmt.Errorf("%w: %v", ErrReceiptSettingsIncomplete, err) //nolint:errorlint // a conflict, not a 422
	}

	return nil
}

// newReceipt returns a receipt of kind issued now by p with the next number
// of the year, the school's language and issuer; the tax mode and note are
// the school's.
func (s *ReceiptService) newReceipt(
	ctx context.Context, tx store.Tx, settings domain.Settings, loc *time.Location, p Principal, kind domain.ReceiptKind,
) (domain.Receipt, error) {
	now := s.clock.Now()
	day := domain.DateOf(now, loc)

	seq, err := tx.ReceiptCounters().Next(ctx, day.Year)
	if err != nil {
		return domain.Receipt{}, fmt.Errorf("receipt number: %w", err)
	}

	rs := settings.Receipts

	return domain.Receipt{
		ID: ids.New[domain.ReceiptID](s.ids), Number: domain.ReceiptNumber(settings.ReceiptPrefix, day.Year, seq),
		Year: day.Year, Sequence: seq, Kind: kind, IssuedAt: now, IssueDate: day, Locale: settings.DefaultLocale,
		Issuer: rs.Issuer, TaxMode: rs.TaxMode, TaxNote: rs.TaxNote, Footer: rs.Footer, CreatedBy: actorOf(p),
	}, nil
}

// issue validates and stores r, renders and stores its PDF, and mails it to
// the payer's account, else to r.RecipientEmail, else to nobody. item
// describes what was paid in a locale's language for the mail, or is empty.
func (s *ReceiptService) issue(
	ctx context.Context, tx store.Tx, r domain.Receipt, orig *domain.Receipt, settings domain.Settings,
	payer domain.PersonID, item func(locale string) string,
) error {
	if err := r.Validate(); err != nil {
		return err
	}

	if err := tx.Receipts().Create(ctx, r); err != nil {
		return fmt.Errorf("receipt: %w", err)
	}

	pdf, err := s.render(ctx, tx, r, orig)
	if err != nil {
		return err
	}

	if err := tx.Receipts().PutPDF(ctx, r.ID, pdf); err != nil {
		return fmt.Errorf("receipt PDF: %w", err)
	}

	return s.mail(ctx, tx, r, orig, settings, payer, item)
}

// render renders the PDF of r with the school's current logo, inside tx and
// so bounded by renderTimeout.
func (s *ReceiptService) render(
	ctx context.Context, tx store.Tx, r domain.Receipt, orig *domain.Receipt,
) ([]byte, error) {
	input, err := s.input(ctx, tx, r, orig)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, s.renderTimeout)
	defer cancel()

	return s.renderInput(ctx, r, input)
}

// input returns the renderer's input for r with the school's current logo.
func (s *ReceiptService) input(
	ctx context.Context, tx store.Tx, r domain.Receipt, orig *domain.Receipt,
) ([]byte, error) {
	if s.renderer == nil {
		return nil, ErrNoPDFRenderer
	}

	logo, err := tx.Settings().Logo(ctx)
	if err != nil {
		return nil, fmt.Errorf("logo: %w", err)
	}

	return receiptInput(r, orig, logo)
}

// renderInput renders the PDF of r from its input.
func (s *ReceiptService) renderInput(ctx context.Context, r domain.Receipt, input []byte) ([]byte, error) {
	out, err := s.renderer.Render(ctx, input)
	if err != nil {
		// Not the caller's input: %v keeps validation errors out (a 500).
		return nil, fmt.Errorf("render receipt %s: %v", r.Number, err) //nolint:errorlint // see above
	}

	return out, nil
}

// mail queues the receipt mail with the PDF attached: to the payer's
// account in its language, else to the address typed at the sale in the
// school's, else nowhere. A payer merged into another person (ADR 0015) is
// that person: the receipt stays on the tombstone, the mail goes to the
// survivor's account (a disabled account of the tombstone gets none). The
// mail is all in its recipient's language, whatever language the PDF is in.
func (s *ReceiptService) mail(
	ctx context.Context, tx store.Tx, r domain.Receipt, orig *domain.Receipt, settings domain.Settings,
	payer domain.PersonID, item func(locale string) string,
) error {
	to := recipient{email: r.RecipientEmail}

	if !payer.IsZero() {
		person, err := tx.Persons().Get(ctx, payer)
		if err != nil {
			return fmt.Errorf("payer: %w", refErr(err))
		}

		if person.Merged() { // tombstones never chain
			if person, err = tx.Persons().Get(ctx, person.MergedInto); err != nil {
				return fmt.Errorf("payer merged into: %w", refErr(err))
			}
		}

		acc, err := tx.Accounts().GetByPerson(ctx, person.ID)

		switch {
		case err == nil:
			to = recipient{email: acc.Email, locale: acc.Locale, givenName: person.GivenName, account: true, accountID: acc.ID}
		case !errors.Is(err, domain.ErrNotFound):
			return err
		}
	}

	if to.email == "" || s.mails.Renderer == nil {
		return nil
	}

	locale := cmp.Or(to.locale, settings.DefaultLocale)
	_, l := receiptLanguage(locale)
	data := mail.ReceiptData{
		SchoolName: settings.SchoolName, GivenName: to.givenName, Number: r.Number,
		Cancellation: r.Kind == domain.ReceiptCancellation, Amount: l.formatMoney(max(r.Gross, -r.Gross), r.Currency),
		Item: item(locale),
	}

	if orig != nil {
		data.Cancels = orig.Number
	}

	return sendMails(ctx, tx, s.ids, s.mails.Renderer, settings, s.clock.Now(), []recipient{to},
		mail.TemplateReceipt, mailOpts{receipt: r.ID}, func(recipient) any { return data })
}

// Cancel issues the cancellation receipt (Storno) of a sale receipt as the
// office account p, with an optional reason: it negates every line, keeps
// the tax mode, the payer, the recipient and the payment method, and voids
// the pass the receipt confirms. A pass that paid for visits is
// ErrPassUsed (the office voids it instead), a receipt canceled before
// ErrReceiptCanceled, a cancellation ErrCancelCancellation; an unknown
// receipt domain.ErrNotFound.
func (s *ReceiptService) Cancel(ctx context.Context, p Principal, id domain.ReceiptID, reason string) (ReceiptView, error) {
	var out ReceiptView

	if err := s.awaitRenderer(ctx); err != nil {
		return ReceiptView{}, err
	}

	err := s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		settings, loc, err := loadSchool(ctx, tx)
		if err != nil {
			return err
		}

		orig, err := tx.Receipts().Get(ctx, id)
		if err != nil {
			return err
		}

		if orig.Kind == domain.ReceiptCancellation {
			return ErrCancelCancellation
		}

		if _, err := tx.Receipts().GetCancellation(ctx, id); err == nil {
			return ErrReceiptCanceled
		} else if !errors.Is(err, domain.ErrNotFound) {
			return err
		}

		if err := s.voidUnused(ctx, tx, id); err != nil {
			return err
		}

		if err := receiptSettingsComplete(settings); err != nil {
			return err
		}

		r, err := s.newReceipt(ctx, tx, settings, loc, p, domain.ReceiptCancellation)
		if err != nil {
			return err
		}

		r.CancelsReceiptID, r.Reason = orig.ID, strings.TrimSpace(reason)
		r.TaxMode, r.TaxNote, r.Currency = orig.TaxMode, orig.TaxNote, orig.Currency
		r.PayerID, r.RecipientName, r.RecipientAddress = orig.PayerID, orig.RecipientName, orig.RecipientAddress
		r.RecipientEmail, r.PaymentMethod = orig.RecipientEmail, orig.PaymentMethod
		r.Net, r.VAT, r.Gross = -orig.Net, -orig.VAT, -orig.Gross

		r.Lines = make([]domain.ReceiptLine, len(orig.Lines))
		for i, l := range orig.Lines {
			r.Lines[i] = l.Negated()
		}

		// What was canceled, in the words of the original if the mail is in
		// its language.
		item := func(locale string) string {
			if receiptfmt.Lang(locale) != receiptfmt.Lang(orig.Locale) {
				return ""
			}

			return orig.Lines[0].Description
		}

		if err := s.issue(ctx, tx, r, &orig, settings, orig.PayerID, item); err != nil {
			return err
		}

		out, err = s.view(ctx, tx, r)

		return err
	})
	if err != nil {
		return ReceiptView{}, fmt.Errorf("cancel receipt %v: %w", id, err)
	}

	return out, nil
}

// voidUnused voids the pass the receipt confirms, under the lock of its
// owner's passes, unless it paid for visits (ErrPassUsed). A voided pass
// stays voided.
func (s *ReceiptService) voidUnused(ctx context.Context, tx store.Tx, receipt domain.ReceiptID) error {
	pass, err := tx.Passes().GetByReceipt(ctx, receipt)
	if errors.Is(err, domain.ErrNotFound) {
		return nil
	} else if err != nil {
		return err
	}

	locked, err := tx.Passes().LockByPersons(ctx, pass.PersonID)
	if err != nil {
		return err
	}

	i := slices.IndexFunc(locked, func(l domain.Pass) bool { return l.ID == pass.ID })
	if i < 0 {
		// A merge (ADR 0015) moved the pass to another owner after it was
		// read: refuse rather than write it back with the owner it had.
		return fmt.Errorf("pass %v changed its owner meanwhile: %w", pass.ID, domain.ErrConflict)
	}

	pass = locked[i]

	uses, err := tx.Redemptions().CountByPasses(ctx, []domain.PassID{pass.ID})
	if err != nil {
		return err
	}

	if uses[pass.ID] > 0 {
		return ErrPassUsed
	}

	if pass.Status == domain.PassVoided {
		return nil
	}

	pass.Status, pass.VoidedAt = domain.PassVoided, s.clock.Now()
	if err := pass.Validate(); err != nil {
		return err
	}

	return tx.Passes().Update(ctx, pass)
}

// ReceiptView is a receipt with what refers to it.
type ReceiptView struct {
	Receipt domain.Receipt
	// Cancels is the receipt a cancellation cancels; CanceledBy the
	// cancellation of a receipt, if any.
	Cancels    *domain.Receipt
	CanceledBy *domain.Receipt
	// Pass is the pass a sale receipt confirms (zero ID if none); PassUsed
	// tells that it paid for visits.
	Pass     domain.Pass
	PassUsed bool
	// PayerName is the payer's current name, empty without one;
	// PayerAnonymized tells that the payer is anonymized (ADR 0016), so that
	// it is the fixed one (the receipt keeps the name it printed).
	PayerName       string
	PayerAnonymized bool
}

// Cancelable reports whether the office can cancel the receipt now.
func (v ReceiptView) Cancelable() bool {
	return v.Receipt.Kind == domain.ReceiptSale && v.CanceledBy == nil && !v.PassUsed
}

// view returns the ReceiptView of r.
func (s *ReceiptService) view(ctx context.Context, tx store.Tx, r domain.Receipt) (ReceiptView, error) {
	views, err := s.views(ctx, tx, []domain.Receipt{r})
	if err != nil {
		return ReceiptView{}, err
	}

	return views[0], nil
}

// views combines receipts with their cancellations, the receipts they
// cancel, their passes, the passes' uses and the payers, with a fixed number
// of queries for any number of receipts.
func (s *ReceiptService) views(ctx context.Context, tx store.Tx, receipts []domain.Receipt) ([]ReceiptView, error) {
	out := make([]ReceiptView, len(receipts))
	if len(receipts) == 0 {
		return out, nil
	}

	ids := make([]domain.ReceiptID, len(receipts))
	canceledIDs := []domain.ReceiptID{}
	payers := []domain.PersonID{}

	for i, r := range receipts {
		ids[i] = r.ID
		if !r.CancelsReceiptID.IsZero() {
			canceledIDs = append(canceledIDs, r.CancelsReceiptID)
		}

		if !r.PayerID.IsZero() {
			payers = append(payers, r.PayerID)
		}
	}

	cancellations, err := tx.Receipts().ListCancellations(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("cancellations: %w", err)
	}

	byCanceled := make(map[domain.ReceiptID]domain.Receipt, len(cancellations))
	for _, c := range cancellations {
		byCanceled[c.CancelsReceiptID] = c
	}

	canceled, err := tx.Receipts().ListByIDs(ctx, canceledIDs)
	if err != nil {
		return nil, fmt.Errorf("canceled receipts: %w", err)
	}

	originals := make(map[domain.ReceiptID]domain.Receipt, len(canceled))
	for _, o := range canceled {
		originals[o.ID] = o
	}

	passes, err := tx.Passes().ListByReceipts(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("passes: %w", err)
	}

	byReceipt := make(map[domain.ReceiptID]domain.Pass, len(passes))
	for _, p := range passes {
		byReceipt[p.ReceiptID] = p
	}

	persons, err := tx.Persons().ListByIDs(ctx, payers)
	if err != nil {
		return nil, fmt.Errorf("payers: %w", err)
	}

	names := byID(persons)

	for i, r := range receipts {
		v := ReceiptView{Receipt: r, Pass: byReceipt[r.ID]}

		if c, ok := byCanceled[r.ID]; ok {
			v.CanceledBy = &c
		}

		if o, ok := originals[r.CancelsReceiptID]; ok {
			v.Cancels = &o
		}

		if p, ok := names[r.PayerID]; ok {
			v.PayerName, v.PayerAnonymized = fullName(p), p.Anonymized()
		}

		out[i] = v
	}

	return s.withUses(ctx, tx, out)
}

// withUses sets PassUsed on the views whose pass paid for visits.
func (s *ReceiptService) withUses(ctx context.Context, tx store.Tx, views []ReceiptView) ([]ReceiptView, error) {
	passes := []domain.PassID{}

	for _, v := range views {
		if !v.Pass.ID.IsZero() {
			passes = append(passes, v.Pass.ID)
		}
	}

	if len(passes) == 0 {
		return views, nil
	}

	uses, err := tx.Redemptions().CountByPasses(ctx, passes)
	if err != nil {
		return nil, fmt.Errorf("pass uses: %w", err)
	}

	for i := range views {
		views[i].PassUsed = uses[views[i].Pass.ID] > 0
	}

	return views, nil
}

// MaxReceiptsPage is the most receipts List returns at once.
const MaxReceiptsPage = 100

// ReceiptPage is a page of receipts and how many match in all.
type ReceiptPage struct {
	Items []ReceiptView
	Total int
	// Years are the years with receipts, the latest first, for a filter.
	Years []int
}

// List returns the receipts that match f (Limit at most MaxReceiptsPage),
// newest first.
func (s *ReceiptService) List(ctx context.Context, f store.ReceiptFilter) (ReceiptPage, error) {
	f.Limit = min(max(f.Limit, 0), MaxReceiptsPage)
	f.Query = strings.TrimSpace(f.Query)

	var out ReceiptPage

	err := s.store.InTx(ctx, store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		receipts, total, err := tx.Receipts().List(ctx, f)
		if err != nil {
			return err
		}

		out.Total = total
		if out.Years, err = tx.ReceiptCounters().Years(ctx); err != nil {
			return err
		}

		out.Items, err = s.views(ctx, tx, receipts)

		return err
	})
	if err != nil {
		return ReceiptPage{}, fmt.Errorf("list receipts: %w", err)
	}

	return out, nil
}

// Get returns a receipt (domain.ErrNotFound for an unknown one).
func (s *ReceiptService) Get(ctx context.Context, id domain.ReceiptID) (ReceiptView, error) {
	var out ReceiptView

	err := s.store.InTx(ctx, store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		r, err := tx.Receipts().Get(ctx, id)
		if err != nil {
			return err
		}

		out, err = s.view(ctx, tx, r)

		return err
	})
	if err != nil {
		return ReceiptView{}, fmt.Errorf("receipt %v: %w", id, err)
	}

	return out, nil
}

// Mine returns the receipts the signed-in person paid, newest first, those
// of persons merged into them included (ADR 0015).
func (s *ReceiptService) Mine(ctx context.Context, p Principal) ([]ReceiptView, error) {
	var out []ReceiptView

	err := s.store.InTx(ctx, store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		if p.PersonID.IsZero() {
			out = []ReceiptView{}

			return nil
		}

		payers, err := payersOf(ctx, tx, p.PersonID)
		if err != nil {
			return err
		}

		receipts, err := tx.Receipts().ListByPayers(ctx, payers)
		if err != nil {
			return err
		}

		out, err = s.views(ctx, tx, receipts)

		return err
	})
	if err != nil {
		return nil, fmt.Errorf("my receipts: %w", err)
	}

	return out, nil
}

// ReceiptFile is the PDF of a receipt and its file name.
type ReceiptFile struct {
	Filename string
	PDF      []byte
}

// PDF returns the stored PDF of a receipt (domain.ErrNotFound for an unknown
// receipt). A receipt without one (the seed's) gets it rendered now and
// stored, once: later reads return the same bytes.
func (s *ReceiptService) PDF(ctx context.Context, id domain.ReceiptID) (ReceiptFile, error) {
	return s.pdf(ctx, id, func(context.Context, store.Tx, domain.Receipt) (bool, error) { return true, nil })
}

// MyPDF returns the PDF of a receipt the signed-in person paid, or a person
// merged into them paid; any other is domain.ErrNotFound.
func (s *ReceiptService) MyPDF(ctx context.Context, p Principal, id domain.ReceiptID) (ReceiptFile, error) {
	return s.pdf(ctx, id, func(ctx context.Context, tx store.Tx, r domain.Receipt) (bool, error) {
		if p.PersonID.IsZero() {
			return false, nil
		}

		payers, err := payersOf(ctx, tx, p.PersonID)

		return slices.Contains(payers, r.PayerID), err
	})
}

// payersOf returns person and the persons merged into it (ADR 0015), whose
// receipts are listed as the person's.
func payersOf(ctx context.Context, tx store.Tx, person domain.PersonID) ([]domain.PersonID, error) {
	merged, err := tx.Persons().ListMerged(ctx, person)
	if err != nil {
		return nil, err
	}

	out := []domain.PersonID{person}
	for _, m := range merged {
		out = append(out, m.ID)
	}

	return out, nil
}

func (s *ReceiptService) pdf(
	ctx context.Context, id domain.ReceiptID, visible func(context.Context, store.Tx, domain.Receipt) (bool, error),
) (ReceiptFile, error) {
	var (
		out   ReceiptFile
		r     domain.Receipt
		input []byte
	)

	err := s.store.InTx(ctx, store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		var err error
		if r, err = tx.Receipts().Get(ctx, id); err != nil {
			return err
		}

		switch ok, err := visible(ctx, tx, r); {
		case err != nil:
			return err
		case !ok:
			return fmt.Errorf("receipt: %w", domain.ErrNotFound)
		}

		out.Filename = r.Number + ".pdf"

		out.PDF, err = tx.Receipts().PDF(ctx, id)
		if !errors.Is(err, domain.ErrNotFound) {
			return err
		}

		// None yet (the seed's): what to render it from.
		var orig *domain.Receipt

		if !r.CancelsReceiptID.IsZero() {
			o, err := tx.Receipts().Get(ctx, r.CancelsReceiptID)
			if err != nil {
				return err
			}

			orig = &o
		}

		input, err = s.input(ctx, tx, r, orig)

		return err
	})

	switch {
	case err != nil:
		return ReceiptFile{}, fmt.Errorf("receipt %v PDF: %w", id, err)
	case input == nil:
		return out, nil
	}

	// Rendered outside any transaction: it takes a moment, and in the demo
	// the first render waits for the module's download and compilation,
	// which must not hold the store. A request that stored it meanwhile
	// stored the same bytes.
	if out.PDF, err = s.renderInput(ctx, r, input); err != nil {
		return ReceiptFile{}, err
	}

	err = s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		return tx.Receipts().PutPDF(ctx, id, out.PDF)
	})
	if errors.Is(err, domain.ErrDuplicate) {
		return s.pdf(ctx, id, visible)
	}

	if err != nil {
		return ReceiptFile{}, fmt.Errorf("receipt %v PDF: %w", id, err)
	}

	return out, nil
}

// ReceiptSettings are the receipt settings with what goes with them.
type ReceiptSettings struct {
	Settings domain.ReceiptSettings
	// Prefix starts every receipt number.
	Prefix string
	// LogoType is the content type of the logo, empty without one.
	LogoType string
	// Missing names the first missing setting that receipts need; empty
	// when they are complete.
	Missing string
	// AddressRequiredAbove is the gross amount above which a receipt should
	// name the recipient's address, zero if no such limit applies: in the
	// standard tax mode in euros, the limit of a small-amount invoice in
	// Germany (250 €, § 33 UStDV). A hint for the sale form, not enforced.
	AddressRequiredAbove domain.Money
}

// smallAmountLimit is the German limit of a small-amount invoice (§ 33
// UStDV), in cents.
const smallAmountLimit domain.Money = 25_000

// Settings returns the receipt settings.
func (s *ReceiptService) Settings(ctx context.Context) (ReceiptSettings, error) {
	var out ReceiptSettings

	err := s.store.InTx(ctx, store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		settings, _, err := loadSchool(ctx, tx)
		if err != nil {
			return err
		}

		logo, err := tx.Settings().Logo(ctx)
		if err != nil {
			return err
		}

		out = ReceiptSettings{Settings: settings.Receipts, Prefix: settings.ReceiptPrefix, LogoType: logo.ContentType}
		if settings.Receipts.TaxMode == domain.TaxStandard && settings.Currency == "EUR" {
			out.AddressRequiredAbove = smallAmountLimit
		}

		var fe *domain.FieldError
		if err := settings.Receipts.Complete(); errors.As(err, &fe) {
			out.Missing = fe.Field
		}

		return nil
	})
	if err != nil {
		return ReceiptSettings{}, fmt.Errorf("receipt settings: %w", err)
	}

	return out, nil
}

// SetSettings replaces the receipt settings and the receipt number prefix
// (texts trimmed; invalid ones are validation errors on their fields).
// Receipts issued before keep what they were issued with.
func (s *ReceiptService) SetSettings(ctx context.Context, rs domain.ReceiptSettings, prefix string) (ReceiptSettings, error) {
	rs.Issuer.Name, rs.Issuer.Address = strings.TrimSpace(rs.Issuer.Name), trimLines(rs.Issuer.Address)
	rs.Issuer.TaxID, rs.Issuer.Email = strings.TrimSpace(rs.Issuer.TaxID), strings.TrimSpace(rs.Issuer.Email)
	rs.Issuer.Phone, rs.TaxNote = strings.TrimSpace(rs.Issuer.Phone), strings.TrimSpace(rs.TaxNote)
	rs.Footer = trimLines(rs.Footer)

	err := s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		settings, err := tx.Settings().Get(ctx)
		if err != nil {
			return err
		}

		settings.Receipts, settings.ReceiptPrefix = rs, strings.TrimSpace(prefix)
		if err := settings.Validate(); err != nil {
			return err
		}

		return tx.Settings().Put(ctx, settings)
	})
	if err != nil {
		return ReceiptSettings{}, fmt.Errorf("set receipt settings: %w", err)
	}

	return s.Settings(ctx)
}

// trimLines trims every line of a multi-line text and drops empty ones.
func trimLines(s string) string {
	return strings.Join(lines(strings.ReplaceAll(s, "\r\n", "\n")), "\n")
}

// Logo returns the school's logo, the zero Logo if it has none.
func (s *ReceiptService) Logo(ctx context.Context) (domain.Logo, error) {
	var out domain.Logo

	err := s.store.InTx(ctx, store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		var err error
		out, err = tx.Settings().Logo(ctx)

		return err
	})
	if err != nil {
		return domain.Logo{}, fmt.Errorf("logo: %w", err)
	}

	return out, nil
}

// SetLogo replaces the logo printed on receipts from now on; the zero Logo
// removes it. A logo that is not a PNG or JPEG of at most 512 KiB is a
// validation error on logo.
func (s *ReceiptService) SetLogo(ctx context.Context, l domain.Logo) error {
	if !l.IsZero() {
		if err := l.Validate(); err != nil {
			return err
		}
	}

	err := s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		return tx.Settings().PutLogo(ctx, l)
	})
	if err != nil {
		return fmt.Errorf("set logo: %w", err)
	}

	return nil
}
