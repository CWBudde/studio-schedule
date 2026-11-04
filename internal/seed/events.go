package seed

import (
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/receiptfmt"
)

// eventSpec is an event of the school (ADR 0019) with its sessions and
// registrations. Its first day is on.date(anchor, days); the sessions count
// their days from it. The office created a published event two weeks
// before the anchor, a draft two days before it.
type eventSpec struct {
	slug, title, description string
	courseType               string
	capacity                 int
	price                    domain.Money
	vatRate                  int
	status                   domain.EventStatus
	on                       onDay
	days                     int
	sessions                 []sessionSpec
	// registered are the persons registered, in the order they registered,
	// each by their own account or a guardian's.
	registered []string
	// paid is the registered person whose fee the office took at the desk
	// two days before the anchor, by method, with its receipt.
	paid   string
	method domain.PaymentMethod
	// guests are the guests the office registered at the desk after the
	// persons, each with an email.
	guests []participantSpec
}

// sessionSpec is a session of an event: days counts from the event's first
// day.
type sessionSpec struct {
	days        int
	start       domain.TimeOfDay
	minutes     int
	room        string
	instructor  string
	accompanist string
}

// events adds the school's events, their sessions (public workshop lessons
// with the event's course type and capacity, as addEventSession creates
// them) and registrations, and the receipt of each paid fee, issued after
// the passes' receipts with the next number of its year.
func (b *builder) events(anchor domain.Date) {
	office := b.accountOf(lookup(b.persons, b.s.office))

	for _, spec := range b.s.events {
		created := anchor.AddDays(-14).At(at(9, 0), b.loc)
		if spec.status == domain.EventDraft {
			created = anchor.AddDays(-2).At(at(9, 0), b.loc)
		}

		e := domain.Event{
			ID: ids.New[domain.EventID](b.seq), Slug: spec.slug, Title: spec.title, Description: spec.description,
			CourseTypeID: lookup(b.courseTypes, spec.courseType), Capacity: spec.capacity, Price: spec.price,
			VATRate: spec.vatRate, Status: spec.status, CreatedAt: created, UpdatedAt: created, CreatedBy: office,
		}
		b.d.Events = append(b.d.Events, e)

		first := spec.on.date(anchor, spec.days)
		sessions := make([]domain.Lesson, 0, len(spec.sessions))

		for _, s := range spec.sessions {
			start := first.AddDays(s.days).At(s.start, b.loc)
			l := domain.Lesson{
				ID:           ids.New[domain.LessonID](b.seq),
				CourseTypeID: e.CourseTypeID,
				RoomID:       lookup(b.rooms, s.room),
				InstructorID: lookup(b.staff, s.instructor),
				StartsAt:     start,
				EndsAt:       start.Add(time.Duration(s.minutes) * time.Minute),
				Status:       domain.LessonScheduled,
				Visibility:   domain.VisibilityPublic,
				Kind:         domain.LessonWorkshop,
				Capacity:     e.Capacity,
				EventID:      e.ID,
			}
			if s.accompanist != "" {
				l.AccompanistID = lookup(b.staff, s.accompanist)
			}

			sessions = append(sessions, l)
		}

		b.d.Lessons = append(b.d.Lessons, sessions...)
		b.registrations(anchor, spec, e, sessions, office)
	}
}

// registrations adds the registrations of spec for e: the persons from six
// days before the anchor on, seven minutes apart, then the guests at the
// desk, by the office. A free registration is paid from the start; the fee
// of spec.paid was paid two days before the anchor, with its receipt.
func (b *builder) registrations(
	anchor domain.Date, spec eventSpec, e domain.Event, sessions []domain.Lesson, office domain.AccountID,
) {
	payment := domain.EventUnpaid
	if e.Price == 0 {
		payment = domain.EventPaid
	}

	from := anchor.AddDays(-6).At(at(18, 0), b.loc)
	n := 0
	next := func() time.Time {
		t := from.Add(time.Duration(n) * 7 * time.Minute)
		n++

		return t
	}

	for _, name := range spec.registered {
		person := lookup(b.persons, name)
		created := next()
		r := domain.EventRegistration{
			ID: ids.New[domain.EventRegistrationID](b.seq), EventID: e.ID, PersonID: person,
			Status: domain.EventRegistered, Payment: payment, Price: e.Price, VATRate: e.VATRate,
			RegisteredBy: b.registrant(person), CreatedAt: created, UpdatedAt: created,
		}

		if name == spec.paid {
			paidAt := anchor.AddDays(-2).At(at(10, 30), b.loc)
			receipt := b.eventFee(e, r, sessions, paidAt, spec.method, office)
			r.Payment, r.ReceiptID, r.UpdatedAt = domain.EventPaid, receipt.ID, paidAt
		}

		b.d.EventRegistrations = append(b.d.EventRegistrations, r)
	}

	for _, g := range spec.guests {
		created := next()
		b.d.EventRegistrations = append(b.d.EventRegistrations, domain.EventRegistration{
			ID: ids.New[domain.EventRegistrationID](b.seq), EventID: e.ID, GuestName: g.guest, GuestEmail: g.email,
			Status: domain.EventRegistered, Payment: payment, Price: e.Price, VATRate: e.VATRate,
			RegisteredBy: office, CreatedAt: created, UpdatedAt: created,
		})
	}
}

// registrant returns the account that registers person: their own, or else
// the first guardian's with one.
func (b *builder) registrant(person domain.PersonID) domain.AccountID {
	for _, a := range b.d.Accounts {
		if a.PersonID == person {
			return a.ID
		}
	}

	for _, g := range b.d.Guardians {
		if g.DependentID != person {
			continue
		}

		for _, a := range b.d.Accounts {
			if a.PersonID == g.GuardianID {
				return a.ID
			}
		}
	}

	panic("seed: nobody to register " + person.String())
}

// eventFee issues the receipt of the fee of registration r, paid by its
// person at paidAt, as ReceiptService.issueEventFee does: one line for the
// event and the dates of its sessions (first to last) at the
// registration's price and VAT rate, with the next number of its year.
func (b *builder) eventFee(
	e domain.Event, r domain.EventRegistration, sessions []domain.Lesson, paidAt time.Time,
	method domain.PaymentMethod, office domain.AccountID,
) domain.Receipt {
	first := domain.DateOf(sessions[0].StartsAt, b.loc)
	last := domain.DateOf(sessions[len(sessions)-1].StartsAt, b.loc)

	receipt := b.newReceipt(paidAt, r.PersonID, method, office)
	desc := receiptfmt.EventLine(receipt.Locale, e.Title, b.name(r.PersonID), first, last)
	receipt.Lines = []domain.ReceiptLine{domain.NewReceiptLine(1, desc, 1, r.Price, r.VATRate, receipt.TaxMode)}
	receipt.Net, receipt.VAT, receipt.Gross = receipt.Lines[0].Net, receipt.Lines[0].VAT, receipt.Lines[0].Gross
	b.d.Receipts = append(b.d.Receipts, receipt)

	return receipt
}
