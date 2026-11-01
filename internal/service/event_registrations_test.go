package service

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/store"
)

func TestAddRegistration(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newEventFixture(t)
	e, _ := f.published(t, f.eventData())
	svc := f.events(t)

	mia, err := svc.AddRegistration(ctx, f.office, e.ID, NewRegistration{PersonID: f.mia.ID})
	if err != nil {
		t.Fatal(err)
	}

	r := mia.Registration
	if r.EventID != e.ID || r.Status != domain.EventRegistered || r.Payment != domain.EventUnpaid ||
		r.Price != 4500 || r.VATRate != 1900 || r.RegisteredBy != f.office.AccountID || mia.Name != "Mia Example" ||
		f.registration(t, r.ID) != r {
		t.Errorf("registered %+v", mia)
	}

	guest, err := svc.AddRegistration(ctx, f.office, e.ID, NewRegistration{
		GuestName: " Gina Guest ", GuestEmail: " gina@example.org ",
	})
	if err != nil || guest.Registration.GuestName != "Gina Guest" || guest.Registration.GuestEmail != "gina@example.org" ||
		guest.Name != "Gina Guest" {
		t.Fatalf("guest %+v, %v", guest, err)
	}

	// Two places taken: full, unless the office confirms.
	if _, err := svc.AddRegistration(ctx, f.office, e.ID, NewRegistration{PersonID: f.tom.ID}); !errors.Is(err, ErrEventFull) {
		t.Errorf("full: %v", err)
	}

	if _, err := svc.AddRegistration(ctx, f.office, e.ID, NewRegistration{
		PersonID: f.tom.ID, ConfirmOverCapacity: true,
	}); err != nil {
		t.Errorf("confirmed: %v", err)
	}

	if _, err := svc.AddRegistration(ctx, f.office, e.ID, NewRegistration{
		PersonID: f.mia.ID, ConfirmOverCapacity: true,
	}); !errors.Is(err, domain.ErrDuplicate) {
		t.Errorf("twice: %v", err)
	}

	for field, in := range map[string]NewRegistration{
		"personId":    {PersonID: ids.New[domain.PersonID](f.seq)},
		"guest.name":  {GuestEmail: "nobody@example.org"},
		"guest.email": {GuestName: "Gina Guest", GuestEmail: "not an address"},
	} {
		_, err := svc.AddRegistration(ctx, f.office, e.ID, in)
		wantFieldError(t, err, field)
	}

	if _, err := svc.AddRegistration(ctx, f.office, ids.New[domain.EventID](f.seq), NewRegistration{
		PersonID: f.anna.ID,
	}); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("unknown event: %v", err)
	}

	// The audit names the event and the registration; a guest's has no
	// target.
	audits := f.audits(t)
	if len(audits) != 3 {
		t.Fatalf("audits %+v", audits)
	}

	for i, want := range []domain.EventRegistration{r, guest.Registration} {
		a := audits[i]
		if a.Action != domain.AuditEventRegistrationAdded || a.ActorID != f.office.AccountID ||
			a.Details.EventID != e.ID || a.Details.RegistrationID != want.ID || a.TargetPersonID != want.PersonID {
			t.Errorf("audit %+v", a)
		}
	}

	if audits[1].TargetKind != "" || audits[0].TargetKind != domain.AuditTargetPerson {
		t.Errorf("targets %q, %q", audits[0].TargetKind, audits[1].TargetKind)
	}
}

// TestAddRegistrationClosedAndFree: a draft and a canceled event take no
// registrations; a free event's are paid from the start.
func TestAddRegistrationClosedAndFree(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newEventFixture(t)
	svc := f.events(t)

	draft, err := svc.Create(ctx, f.office, f.eventData())
	if err != nil {
		t.Fatal(err)
	}

	if _, err := svc.AddRegistration(ctx, f.office, draft.Event.ID, NewRegistration{PersonID: f.mia.ID}); !errors.Is(err, ErrEventClosed) {
		t.Errorf("draft: %v", err)
	}

	d := f.eventData()
	d.Price, d.VATRate = 0, 0
	free, _ := f.published(t, d)

	r := f.register(t, free, NewRegistration{PersonID: f.mia.ID})
	if r.Payment != domain.EventPaid || !r.ReceiptID.IsZero() {
		t.Errorf("free registration %+v", r)
	}

	if _, err := svc.Cancel(ctx, f.office, free.ID, ""); err != nil {
		t.Fatal(err)
	}

	if _, err := svc.AddRegistration(ctx, f.office, free.ID, NewRegistration{PersonID: f.mia.ID}); !errors.Is(err, ErrEventClosed) {
		t.Errorf("canceled: %v", err)
	}
}

func TestListRegistrations(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newEventFixture(t)
	e, _ := f.published(t, f.eventData())
	mia := f.register(t, e, NewRegistration{PersonID: f.mia.ID})
	guest := f.register(t, e, NewRegistration{GuestName: "Gina Guest", GuestEmail: "gina@example.org"})

	if _, err := f.events(t).CancelRegistration(ctx, f.office, mia.ID, false); err != nil {
		t.Fatal(err)
	}

	f.write(t, func(ctx context.Context, tx store.Tx) error {
		p, err := tx.Persons().Get(ctx, f.mia.ID)
		if err != nil {
			return err
		}

		return tx.Persons().Update(ctx, p.Anonymize(mustInstant(t, bookingNow)))
	})

	got, err := f.events(t).ListRegistrations(ctx, e.ID)
	if err != nil {
		t.Fatal(err)
	}

	if len(got) != 2 || got[0].Registration.ID != mia.ID || !got[0].Anonymized ||
		got[0].Registration.Status != domain.EventRegistrationCanceled || got[1].Registration.ID != guest.ID ||
		got[1].Name != "Gina Guest" || got[1].Anonymized {
		t.Errorf("registrations %+v", got)
	}

	if _, err := f.events(t).ListRegistrations(ctx, ids.New[domain.EventID](f.seq)); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("unknown event: %v", err)
	}
}

func TestCancelRegistration(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newEventFixture(t)
	e, _ := f.published(t, f.eventData())
	svc := f.events(t)

	unpaid := f.register(t, e, NewRegistration{PersonID: f.mia.ID})

	got, err := svc.CancelRegistration(ctx, f.office, unpaid.ID, true)
	if err != nil {
		t.Fatal(err)
	}

	if r := got.Registration; r.Status != domain.EventRegistrationCanceled || r.Payment != domain.EventUnpaid ||
		!r.CanceledAt.Equal(mustInstant(t, bookingNow)) || got.Name != "Mia Example" {
		t.Errorf("canceled %+v", got)
	}

	if _, err := svc.CancelRegistration(ctx, f.office, unpaid.ID, true); !errors.Is(err, ErrRegistrationCanceled) {
		t.Errorf("twice: %v", err)
	}

	if _, err := svc.CancelRegistration(ctx, f.office, ids.New[domain.EventRegistrationID](f.seq), false); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("unknown: %v", err)
	}

	// Paid: kept without a refund, refunded with one, once the event has
	// started too.
	kept := f.paid(t, f.register(t, e, NewRegistration{PersonID: f.tom.ID}))
	if got, err = svc.CancelRegistration(ctx, f.office, kept.ID, false); err != nil ||
		got.Registration.Payment != domain.EventPaid {
		t.Errorf("kept %+v, %v", got, err)
	}

	refunded := f.paid(t, f.register(t, e, NewRegistration{GuestName: "Gina Guest", GuestEmail: "gina@example.org"}))
	before := f.receiptCount(t)

	got, err = f.eventsAt(t, "2026-10-23T17:00:00Z").CancelRegistration(ctx, f.office, refunded.ID, true)
	if err != nil {
		t.Fatal(err)
	}

	r := got.Registration
	if r.Payment != domain.EventRefunded || r.CancellationReceiptID.IsZero() || f.receiptCount(t) != before+1 {
		t.Errorf("refunded %+v", r)
	}

	storno, _ := f.receipt(t, r.CancellationReceiptID)
	if storno.CancelsReceiptID != r.ReceiptID || storno.Gross != -4500 || storno.RecipientEmail != "gina@example.org" {
		t.Errorf("Storno %+v", storno)
	}

	var refunds []bool

	for _, a := range f.audits(t) {
		if a.Action == domain.AuditEventRegistrationCanceled {
			refunds = append(refunds, a.Details.Refund)
		}
	}

	if !slices.Equal(refunds, []bool{false, false, true}) {
		t.Errorf("audited refunds %v", refunds)
	}
}

// paid marks r paid at bookingNow in cash and returns it.
func (f *eventFixture) paid(t *testing.T, r domain.EventRegistration) domain.EventRegistration {
	t.Helper()

	v, err := f.events(t).MarkPaid(t.Context(), f.office, r.ID, EventPayment{})
	if err != nil {
		t.Fatal(err)
	}

	return v.Registration
}

func TestMarkPaid(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newEventFixture(t)
	e, _ := f.published(t, f.eventData())
	svc := f.events(t)
	r := f.register(t, e, NewRegistration{PersonID: f.mia.ID})
	before := len(f.mails(t))

	got, err := svc.MarkPaid(ctx, f.office, r.ID, EventPayment{Method: domain.PaymentCard, PayerID: f.anna.ID})
	if err != nil {
		t.Fatal(err)
	}

	paid := got.Registration
	if paid.Payment != domain.EventPaid || paid.ReceiptID.IsZero() || got.Name != "Mia Example" ||
		f.registration(t, r.ID) != paid {
		t.Errorf("paid %+v", got)
	}

	receipt, pdf := f.receipt(t, paid.ReceiptID)
	if receipt.PayerID != f.anna.ID || receipt.PaymentMethod != domain.PaymentCard || receipt.Gross != 4500 ||
		receipt.RecipientName != "Anna Example" || len(receipt.Lines) != 1 || len(pdf) == 0 ||
		receipt.Lines[0].Description != "Imaginary Intensive for Mia Example, 23 Oct 2026 – 24 Oct 2026" {
		t.Errorf("receipt %+v", receipt)
	}

	// Anna's account gets the receipt.
	if mails := f.mails(t)[before:]; len(mails) != 1 || mails[0].To != "anna@example.org" ||
		mails[0].Template != "receipt" || mails[0].ReceiptID != receipt.ID {
		t.Errorf("mails %+v", mails)
	}

	if _, err := svc.MarkPaid(ctx, f.office, r.ID, EventPayment{}); !errors.Is(err, ErrAlreadyPaid) {
		t.Errorf("twice: %v", err)
	}

	// A guest pays for themself and gets the receipt at their address.
	guest := f.register(t, e, NewRegistration{GuestName: "Gina Guest", GuestEmail: "gina@example.org"})

	_, err = svc.MarkPaid(ctx, f.office, guest.ID, EventPayment{PayerID: f.anna.ID})
	wantFieldError(t, err, "payerId")

	paid = f.paid(t, guest)
	if receipt, _ = f.receipt(t, paid.ReceiptID); !receipt.PayerID.IsZero() || receipt.RecipientName != "Gina Guest" ||
		receipt.RecipientEmail != "gina@example.org" || receipt.PaymentMethod != domain.PaymentCash ||
		!strings.HasPrefix(receipt.Lines[0].Description, "Imaginary Intensive for Gina Guest") {
		t.Errorf("guest's receipt %+v", receipt)
	}

	tom := f.register(t, e, NewRegistration{PersonID: f.tom.ID, ConfirmOverCapacity: true})

	_, err = svc.MarkPaid(ctx, f.office, tom.ID, EventPayment{PayerID: ids.New[domain.PersonID](f.seq)})
	wantFieldError(t, err, "payerId")

	f.updateSettings(t, func(s *domain.Settings) { s.Receipts.TaxMode = "" })

	if _, err := svc.MarkPaid(ctx, f.office, tom.ID, EventPayment{}); !errors.Is(err, ErrReceiptSettingsIncomplete) {
		t.Errorf("incomplete settings: %v", err)
	}

	if _, err := svc.CancelRegistration(ctx, f.office, tom.ID, false); err != nil {
		t.Fatal(err)
	}

	if _, err := svc.MarkPaid(ctx, f.office, tom.ID, EventPayment{}); !errors.Is(err, ErrRegistrationCanceled) {
		t.Errorf("canceled: %v", err)
	}

	if _, err := svc.MarkPaid(ctx, f.office, ids.New[domain.EventRegistrationID](f.seq), EventPayment{}); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("unknown: %v", err)
	}

	var paidAudits int

	for _, a := range f.audits(t) {
		if a.Action == domain.AuditEventRegistrationPaid {
			paidAudits++
		}
	}

	if paidAudits != 2 {
		t.Errorf("%d paid audits, want 2", paidAudits)
	}
}

// TestMarkPaidFreeAndDates: a free registration is paid already; the
// receipt names one day for an event on one day, and the days of all
// sessions when the school canceled every one.
func TestMarkPaidFreeAndDates(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newEventFixture(t)
	svc := f.events(t)

	d := f.eventData()
	d.Price = 0
	free, _ := f.published(t, d)

	if _, err := svc.MarkPaid(ctx, f.office, f.register(t, free, NewRegistration{PersonID: f.mia.ID}).ID, EventPayment{}); !errors.Is(err, ErrAlreadyPaid) {
		t.Errorf("free: %v", err)
	}

	e, sessions := f.publishedAt(t, f.eventData(), 10)
	for _, l := range sessions {
		if _, err := f.lessonService(t, bookingNow).CancelLesson(ctx, f.office, l.ID, ""); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := svc.AddSession(ctx, e.ID, f.sessionAt(t, "2026-10-25", 10)); err != nil {
		t.Fatal(err)
	}

	r := f.paid(t, f.register(t, e, NewRegistration{PersonID: f.mia.ID}))
	if receipt, _ := f.receipt(t, r.ReceiptID); receipt.Lines[0].Description != "Imaginary Intensive for Mia Example, 25 Oct 2026" {
		t.Errorf("one day: %q", receipt.Lines[0].Description)
	}

	all, err := svc.Create(ctx, f.office, f.eventData())
	if err != nil {
		t.Fatal(err)
	}

	got := f.eventDaysOf(t, all.Event.ID)
	if !got[0].IsZero() || !got[1].IsZero() {
		t.Errorf("days of an event without sessions %v", got)
	}

	got = f.eventDaysOf(t, e.ID)
	if got[0] != mustDate(t, "2026-10-25") || got[1] != mustDate(t, "2026-10-25") {
		t.Errorf("days %v", got)
	}

	// All canceled: their days.
	canceled, sessions := f.publishedAt(t, f.eventData(), 8)
	for _, l := range sessions {
		if _, err := f.lessonService(t, bookingNow).CancelLesson(ctx, f.office, l.ID, ""); err != nil {
			t.Fatal(err)
		}
	}

	if got = f.eventDaysOf(t, canceled.ID); got[0] != mustDate(t, "2026-10-23") || got[1] != mustDate(t, "2026-10-24") {
		t.Errorf("days of canceled sessions %v", got)
	}
}

func (f *eventFixture) eventDaysOf(t *testing.T, id domain.EventID) [2]domain.Date {
	t.Helper()

	var out [2]domain.Date

	f.write(t, func(ctx context.Context, tx store.Tx) error {
		var err error
		out[0], out[1], err = eventDays(ctx, tx, berlin(t), id)

		return err
	})

	return out
}

// TestCancelEventFeeReceipt: canceling an event fee's receipt refunds its
// registration, until the event starts; its view names the event.
func TestCancelEventFeeReceipt(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newEventFixture(t)
	e, sessions := f.published(t, f.eventData())
	r := f.paid(t, f.register(t, e, NewRegistration{PersonID: f.mia.ID}))
	late := f.paid(t, f.register(t, e, NewRegistration{PersonID: f.tom.ID}))

	view, err := f.receiptService(t, bookingNow).Get(ctx, r.ReceiptID)
	if err != nil || view.EventID != e.ID || view.EventTitle != e.Title || view.EventStarted || !view.Cancelable() {
		t.Errorf("view %+v, %v", view, err)
	}

	storno, err := f.receiptService(t, bookingNow).Cancel(ctx, f.office, r.ReceiptID, "Paid twice")
	if err != nil {
		t.Fatal(err)
	}

	if storno.EventID != e.ID || storno.EventTitle != e.Title || storno.Cancelable() {
		t.Errorf("Storno view %+v", storno)
	}

	got := f.registration(t, r.ID)
	if got.Payment != domain.EventRefunded || got.CancellationReceiptID != storno.Receipt.ID ||
		got.Status != domain.EventRegistered {
		t.Errorf("refunded registration %+v", got)
	}

	started := sessions[0].StartsAt.Format("2006-01-02T15:04:05Z07:00")

	if view, err = f.receiptService(t, started).Get(ctx, late.ReceiptID); err != nil || !view.EventStarted || view.Cancelable() {
		t.Errorf("started view %+v, %v", view, err)
	}

	if _, err := f.receiptService(t, started).Cancel(ctx, f.office, late.ReceiptID, ""); !errors.Is(err, ErrEventAttended) {
		t.Errorf("started: %v, want ErrEventAttended", err)
	}

	// A pass's receipt names no event.
	pass := f.sell(t, f.mia, f.single, "")
	if view, err = f.receiptService(t, bookingNow).Get(ctx, pass.ReceiptID); err != nil || !view.EventID.IsZero() {
		t.Errorf("pass receipt %+v, %v", view, err)
	}
}

func TestCancelEvent(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newEventFixture(t)
	e, sessions := f.published(t, f.eventData())
	mia := f.paid(t, f.register(t, e, NewRegistration{PersonID: f.mia.ID}))
	tom := f.register(t, e, NewRegistration{PersonID: f.tom.ID})
	guest := f.paid(t, f.register(t, e, NewRegistration{
		GuestName: "Gina Guest", GuestEmail: "gina@example.org", ConfirmOverCapacity: true,
	}))
	gone := f.register(t, e, NewRegistration{PersonID: f.anna.ID, ConfirmOverCapacity: true})

	if _, err := f.events(t).CancelRegistration(ctx, f.office, gone.ID, false); err != nil {
		t.Fatal(err)
	}

	// Friday's session has started when the event is canceled.
	now := "2026-10-23T16:30:00Z"
	before := len(f.mails(t))

	got, err := f.eventsAt(t, now).Cancel(ctx, f.office, e.ID, " The guest teacher is ill. ")
	if err != nil {
		t.Fatal(err)
	}

	if got.Event.Status != domain.EventCanceled || got.Counts != (store.EventRegistrationCounts{}) {
		t.Errorf("canceled %+v", got)
	}

	if l := f.lesson(t, sessions[0].ID); l.Status != domain.LessonScheduled {
		t.Errorf("the started session %+v", l)
	}

	if l := f.lesson(t, sessions[1].ID); l.Status != domain.LessonCanceled || l.CancelNote != "The guest teacher is ill." ||
		l.CanceledBy != f.office.AccountID {
		t.Errorf("the session ahead %+v", l)
	}

	for _, c := range []struct {
		r       domain.EventRegistration
		payment domain.EventPayment
	}{{mia, domain.EventRefunded}, {tom, domain.EventUnpaid}, {guest, domain.EventRefunded}} {
		got := f.registration(t, c.r.ID)
		if got.Status != domain.EventRegistrationCanceled || got.Payment != c.payment {
			t.Errorf("registration %+v, want canceled and %s", got, c.payment)
		}
	}

	if f.registration(t, gone.ID).CanceledAt.Equal(mustInstant(t, now)) {
		t.Error("a canceled registration was canceled again")
	}

	// One event-canceled mail per recipient, no lesson-canceled mail; the
	// Stornos are mailed as their receipts were: Mia, who paid hers, has
	// neither an account nor an address, the guest has one.
	type sent struct{ template, to string }

	var mails []sent

	for _, m := range f.mails(t)[before:] {
		mails = append(mails, sent{m.Template, m.To})

		refunds := strings.Contains(m.Text, "refunded") || strings.Contains(m.Text, "erstattet")
		if m.Template == "event-canceled" && refunds != (m.To != "tom@example.org") {
			t.Errorf("refund of %s:\n%s", m.To, m.Text)
		}
	}

	want := []sent{
		{"receipt", "gina@example.org"},
		{"event-canceled", "anna@example.org"},
		{"event-canceled", "tom@example.org"},
		{"event-canceled", "gina@example.org"},
	}
	if !slices.Equal(mails, want) {
		t.Errorf("mails %v, want %v", mails, want)
	}

	audits := f.audits(t)
	if last := audits[len(audits)-1]; last.Action != domain.AuditEventCanceled || last.Details.EventID != e.ID ||
		last.TargetKind != "" {
		t.Errorf("audit %+v", last)
	}

	if _, err := f.events(t).Cancel(ctx, f.office, e.ID, ""); !errors.Is(err, ErrEventClosed) {
		t.Errorf("twice: %v", err)
	}

	if _, err := f.events(t).Cancel(ctx, f.office, ids.New[domain.EventID](f.seq), ""); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("unknown: %v", err)
	}
}
