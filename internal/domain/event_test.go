package domain_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
)

func eventFixture() domain.Event {
	return domain.Event{
		ID: newID[domain.EventID](), Slug: "summer-intensive-2026", Title: "Summer Intensive",
		Description: "Five mornings of technique and repertoire.", CourseTypeID: newID[domain.CourseTypeID](),
		Capacity: 16, Price: 12000, VATRate: 1900, Status: domain.EventPublished,
		SignupOpensAt: t0, SignupClosesAt: t0.Add(30 * 24 * time.Hour), CancelUntil: t0.Add(20 * 24 * time.Hour),
		CreatedAt: t0.Add(-time.Hour), UpdatedAt: t0, CreatedBy: newID[domain.AccountID](),
	}
}

func TestEventValidate(t *testing.T) {
	t.Parallel()

	checkValidate(t, eventFixture, map[string]func(*domain.Event){
		"zero id":               func(e *domain.Event) { e.ID = domain.EventID{} },
		"no slug":               func(e *domain.Event) { e.Slug = "" },
		"bad slug":              func(e *domain.Event) { e.Slug = "Summer Intensive" },
		"blank title":           func(e *domain.Event) { e.Title = "  " },
		"title surrounds":       func(e *domain.Event) { e.Title = "Summer " },
		"long title":            func(e *domain.Event) { e.Title = strings.Repeat("ä", domain.MaxEventTitleLength+1) },
		"description surrounds": func(e *domain.Event) { e.Description = "\nFive mornings." },
		"long description": func(e *domain.Event) {
			e.Description = strings.Repeat("ä", domain.MaxEventDescriptionLength+1)
		},
		"zero course type":     func(e *domain.Event) { e.CourseTypeID = domain.CourseTypeID{} },
		"zero capacity":        func(e *domain.Event) { e.Capacity = 0 },
		"huge capacity":        func(e *domain.Event) { e.Capacity = domain.MaxEventCapacity + 1 },
		"negative price":       func(e *domain.Event) { e.Price = -1 },
		"huge price":           func(e *domain.Event) { e.Price = domain.MaxEventPrice + 1 },
		"negative VAT":         func(e *domain.Event) { e.VATRate = -1 },
		"huge VAT":             func(e *domain.Event) { e.VATRate = domain.MaxVATRate + 1 },
		"bad status":           func(e *domain.Event) { e.Status = "archived" },
		"closes when it opens": func(e *domain.Event) { e.SignupClosesAt = e.SignupOpensAt },
		"closes before it opens": func(e *domain.Event) {
			e.SignupClosesAt = e.SignupOpensAt.Add(-time.Minute)
		},
		"no created at":  func(e *domain.Event) { e.CreatedAt = time.Time{} },
		"no updated at":  func(e *domain.Event) { e.UpdatedAt = time.Time{} },
		"updated before": func(e *domain.Event) { e.UpdatedAt = e.CreatedAt.Add(-time.Second) },
	})

	// The optional fields may be left out, and a free event is valid.
	for name, mutate := range map[string]func(*domain.Event){
		"free":             func(e *domain.Event) { e.Price, e.VATRate = 0, 0 },
		"no description":   func(e *domain.Event) { e.Description = "" },
		"no window":        func(e *domain.Event) { e.SignupOpensAt, e.SignupClosesAt = time.Time{}, time.Time{} },
		"opens only":       func(e *domain.Event) { e.SignupClosesAt = time.Time{} },
		"closes only":      func(e *domain.Event) { e.SignupOpensAt = time.Time{} },
		"no deadline":      func(e *domain.Event) { e.CancelUntil = time.Time{} },
		"no creator":       func(e *domain.Event) { e.CreatedBy = domain.AccountID{} },
		"draft":            func(e *domain.Event) { e.Status = domain.EventDraft },
		"canceled":         func(e *domain.Event) { e.Status = domain.EventCanceled },
		"longest title":    func(e *domain.Event) { e.Title = strings.Repeat("ä", domain.MaxEventTitleLength) },
		"one place":        func(e *domain.Event) { e.Capacity = 1 },
		"highest price":    func(e *domain.Event) { e.Price = domain.MaxEventPrice },
		"longest slug":     func(e *domain.Event) { e.Slug = strings.Repeat("a", domain.MaxEventSlugLength) },
		"single-char slug": func(e *domain.Event) { e.Slug = "x" },
	} {
		e := eventFixture()
		mutate(&e)

		if err := e.Validate(); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestValidEventSlug(t *testing.T) {
	t.Parallel()

	for slug, want := range map[string]bool{
		"summer-intensive-2026":                true,
		"2026":                                 true,
		"a-b-c":                                true,
		strings.Repeat("a", 80):                true,
		strings.Repeat("a", 40) + "-" + "2026": true,
		"":                                     false,
		strings.Repeat("a", 81):                false,
		"Summer":                               false,
		"summer_intensive":                     false,
		"summer intensive":                     false,
		"-summer":                              false,
		"summer-":                              false,
		"summer--intensive":                    false,
		"sömmer":                               false,
		"summer/intensive":                     false,
		"summer.intensive":                     false,
	} {
		if got := domain.ValidEventSlug(slug); got != want {
			t.Errorf("ValidEventSlug(%q) = %v, want %v", slug, got, want)
		}
	}
}

// registrationFixture is a person's unpaid registration of a paid event.
func registrationFixture() domain.EventRegistration {
	return domain.EventRegistration{
		ID: newID[domain.EventRegistrationID](), EventID: newID[domain.EventID](), PersonID: newID[domain.PersonID](),
		Status: domain.EventRegistered, Payment: domain.EventUnpaid, Price: 12000, VATRate: 1900,
		RegisteredBy: newID[domain.AccountID](), CreatedAt: t0, UpdatedAt: t0,
	}
}

func TestEventRegistrationValidate(t *testing.T) {
	t.Parallel()

	guest := func(r *domain.EventRegistration) {
		r.PersonID, r.GuestName, r.GuestEmail = domain.PersonID{}, "Gina Guest", "gina@example.org"
	}
	invite := func(r *domain.EventRegistration) {
		r.Status, r.InvitedAt, r.ExpiresAt = domain.EventInvited, t0.Add(time.Minute), t0.Add(time.Hour)
		r.InviteTokenHash[0] = 1
	}
	paid := func(r *domain.EventRegistration) {
		r.Payment, r.ReceiptID = domain.EventPaid, newID[domain.ReceiptID]()
	}
	refunded := func(r *domain.EventRegistration) {
		paid(r)
		r.Payment, r.CancellationReceiptID = domain.EventRefunded, newID[domain.ReceiptID]()
	}
	free := func(r *domain.EventRegistration) { r.Price, r.VATRate, r.Payment = 0, 0, domain.EventPaid }
	canceled := func(r *domain.EventRegistration) {
		r.Status, r.CanceledAt, r.UpdatedAt = domain.EventRegistrationCanceled, t0.Add(time.Hour), t0.Add(time.Hour)
	}

	checkValidate(t, registrationFixture, map[string]func(*domain.EventRegistration){
		"zero id":    func(r *domain.EventRegistration) { r.ID = domain.EventRegistrationID{} },
		"zero event": func(r *domain.EventRegistration) { r.EventID = domain.EventID{} },
		"neither":    func(r *domain.EventRegistration) { r.PersonID = domain.PersonID{} },
		"both": func(r *domain.EventRegistration) {
			r.GuestName, r.GuestEmail = "Gina Guest", "gina@example.org"
		},
		"person with a guest email": func(r *domain.EventRegistration) { r.GuestEmail = "gina@example.org" },
		"guest without a name":      func(r *domain.EventRegistration) { guest(r); r.GuestName = "" },
		"guest with a blank name":   func(r *domain.EventRegistration) { guest(r); r.GuestName = "  " },
		"guest name surrounds":      func(r *domain.EventRegistration) { guest(r); r.GuestName = "Gina " },
		"guest name too long": func(r *domain.EventRegistration) {
			guest(r)
			r.GuestName = strings.Repeat("ä", domain.MaxGuestNameLength+1)
		},
		"guest without an email": func(r *domain.EventRegistration) { guest(r); r.GuestEmail = "" },
		"anonymized guest with an email": func(r *domain.EventRegistration) {
			guest(r)
			r.GuestName, r.GuestEmail = domain.AnonymousGuestName, "gina"
		},
		"guest with a bad email": func(r *domain.EventRegistration) { guest(r); r.GuestEmail = "gina" },
		"bad status":             func(r *domain.EventRegistration) { r.Status = "pending" },
		"bad payment":            func(r *domain.EventRegistration) { r.Payment = "partial" },
		"negative price":         func(r *domain.EventRegistration) { r.Price = -1 },
		"huge price":             func(r *domain.EventRegistration) { r.Price = domain.MaxEventPrice + 1 },
		"negative VAT":           func(r *domain.EventRegistration) { r.VATRate = -1 },
		"huge VAT":               func(r *domain.EventRegistration) { r.VATRate = domain.MaxVATRate + 1 },
		"free and unpaid":        func(r *domain.EventRegistration) { r.Price = 0 },
		"free with a receipt":    func(r *domain.EventRegistration) { free(r); r.ReceiptID = newID[domain.ReceiptID]() },
		"free and refunded":      func(r *domain.EventRegistration) { refunded(r); r.Price = 0 },
		"paid without a receipt": func(r *domain.EventRegistration) { r.Payment = domain.EventPaid },
		"unpaid with a receipt":  func(r *domain.EventRegistration) { r.ReceiptID = newID[domain.ReceiptID]() },
		"refunded without a Storno": func(r *domain.EventRegistration) {
			paid(r)
			r.Payment = domain.EventRefunded
		},
		"refunded without a receipt": func(r *domain.EventRegistration) {
			refunded(r)
			r.ReceiptID = domain.ReceiptID{}
		},
		"paid with a Storno": func(r *domain.EventRegistration) {
			paid(r)
			r.CancellationReceiptID = newID[domain.ReceiptID]()
		},
		"Storno is the receipt": func(r *domain.EventRegistration) {
			refunded(r)
			r.CancellationReceiptID = r.ReceiptID
		},
		"invited without a token": func(r *domain.EventRegistration) {
			invite(r)
			r.InviteTokenHash = [32]byte{}
		},
		"invited without a time": func(r *domain.EventRegistration) { invite(r); r.InvitedAt = time.Time{} },
		"invite expires at once": func(r *domain.EventRegistration) { invite(r); r.ExpiresAt = r.InvitedAt },
		"invited before created": func(r *domain.EventRegistration) {
			invite(r)
			r.InvitedAt = t0.Add(-time.Minute)
		},
		"registered with a token": func(r *domain.EventRegistration) { r.InviteTokenHash[0] = 1 },
		"registered with an invite time": func(r *domain.EventRegistration) {
			r.InvitedAt = t0.Add(time.Minute)
		},
		"waiting with an expiry": func(r *domain.EventRegistration) {
			r.Status, r.ExpiresAt = domain.EventWaiting, t0.Add(time.Hour)
		},
		"no created at":           func(r *domain.EventRegistration) { r.CreatedAt = time.Time{} },
		"no updated at":           func(r *domain.EventRegistration) { r.UpdatedAt = time.Time{} },
		"updated before created":  func(r *domain.EventRegistration) { r.UpdatedAt = t0.Add(-time.Second) },
		"canceled without a time": func(r *domain.EventRegistration) { r.Status = domain.EventRegistrationCanceled },
		"cancel time while open":  func(r *domain.EventRegistration) { r.CanceledAt = t0.Add(time.Hour) },
		"canceled before created": func(r *domain.EventRegistration) {
			canceled(r)
			r.CanceledAt = t0.Add(-time.Hour)
		},
	})

	for name, mutate := range map[string]func(*domain.EventRegistration){
		"guest":               guest,
		"invited":             invite,
		"paid":                paid,
		"refunded":            refunded,
		"free":                free,
		"canceled":            canceled,
		"canceled and paid":   func(r *domain.EventRegistration) { paid(r); canceled(r) },
		"canceled, refunded":  func(r *domain.EventRegistration) { refunded(r); canceled(r) },
		"waiting":             func(r *domain.EventRegistration) { r.Status = domain.EventWaiting },
		"free and waiting":    func(r *domain.EventRegistration) { free(r); r.Status = domain.EventWaiting },
		"guest, paid":         func(r *domain.EventRegistration) { guest(r); paid(r) },
		"no registering user": func(r *domain.EventRegistration) { r.RegisteredBy = domain.AccountID{} },
		"anonymized guest": func(r *domain.EventRegistration) {
			guest(r)
			r.GuestName, r.GuestEmail = domain.AnonymousGuestName, ""
		},
		"longest guest name": func(r *domain.EventRegistration) {
			guest(r)
			r.GuestName = strings.Repeat("ä", domain.MaxGuestNameLength)
		},
	} {
		r := registrationFixture()
		mutate(&r)

		if err := r.Validate(); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestEventRegistrationOpen(t *testing.T) {
	t.Parallel()

	for status, want := range map[domain.EventRegistrationStatus]bool{
		domain.EventWaiting:              true,
		domain.EventInvited:              true,
		domain.EventRegistered:           true,
		domain.EventRegistrationCanceled: false,
		"":                               false,
	} {
		if got := (domain.EventRegistration{Status: status}).Open(); got != want {
			t.Errorf("Open(%q) = %v, want %v", status, got, want)
		}
	}
}

func TestEventFieldErrors(t *testing.T) {
	t.Parallel()

	event := func(f func(*domain.Event)) domain.Event { e := eventFixture(); f(&e); return e }

	for name, tc := range map[string]struct {
		entity interface{ Validate() error }
		field  string
	}{
		"slug":     {event(func(e *domain.Event) { e.Slug = "A" }), "slug"},
		"title":    {event(func(e *domain.Event) { e.Title = "" }), "title"},
		"capacity": {event(func(e *domain.Event) { e.Capacity = 0 }), "capacity"},
		"price":    {event(func(e *domain.Event) { e.Price = -1 }), "price"},
		"vatRate":  {event(func(e *domain.Event) { e.VATRate = -1 }), "vatRate"},
		"course":   {event(func(e *domain.Event) { e.CourseTypeID = domain.CourseTypeID{} }), "courseTypeId"},
		"status":   {event(func(e *domain.Event) { e.Status = "" }), "status"},
		"window":   {event(func(e *domain.Event) { e.SignupClosesAt = e.SignupOpensAt }), "signupClosesAt"},
		"guest email": {func() domain.EventRegistration {
			r := registrationFixture()
			r.PersonID, r.GuestName = domain.PersonID{}, "Gina Guest"

			return r
		}(), "guest.email"},
	} {
		err := tc.entity.Validate()

		var fe *domain.FieldError
		if !errors.As(err, &fe) || fe.Field != tc.field {
			t.Errorf("%s: %v, want a FieldError on %q", name, err, tc.field)
		}
	}
}
