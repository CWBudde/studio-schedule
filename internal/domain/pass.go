package domain

import (
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

// Passes are prepaid lessons (ADR 0012): a pass type is a product of the
// school, a pass is one sold to (or assigned to) a person, and a redemption
// records which booking a pass paid for.

// PassKind tells a pass of a number of visits from a pass of a period.
type PassKind string

// Known pass kinds.
const (
	// PassVisits is a number of visits, valid for a number of days from its
	// first day or without an expiry.
	PassVisits PassKind = "visits"
	// PassTime is a period of days from its first day with any number of
	// visits (one booking per lesson, as always).
	PassTime PassKind = "time"
)

// PassKinds returns all pass kinds.
func PassKinds() []PassKind { return []PassKind{PassVisits, PassTime} }

// Valid reports whether k is a known pass kind.
func (k PassKind) Valid() bool { return slices.Contains(PassKinds(), k) }

// Bounds of pass types and passes. They keep the numbers sane, and a price
// within the bounds of a receipt line.
const (
	// MaxPassVisits is the most visits of a pass.
	MaxPassVisits = 1000
	// MaxPassValidityDays is the longest validity or period of a pass, ten
	// years.
	MaxPassValidityDays = 3660
	// MaxPassPrice is the highest price of a pass, in minor units.
	MaxPassPrice Money = 100_000_000
	// MaxVATRate is the highest VAT rate, in basis points (100 %).
	MaxVATRate = 10_000
	// MaxPassNoteLength is the longest note on a pass, in characters.
	MaxPassNoteLength = 500
)

// PassType is a product in the pass catalog, e.g. a 10-visit pass or a
// 30-day pass. A pass copies what it needs of its type when it is sold, so
// a change of the type affects only later sales.
type PassType struct {
	ID   PassTypeID
	Name string
	Kind PassKind
	// Visits is the number of visits of a visit pass; zero for a time pass.
	Visits int
	// ValidityDays is, for a visit pass, how many days it stays valid from
	// its first day (zero: it does not expire), and for a time pass the
	// number of days it lasts (at least one).
	ValidityDays int
	// Price is the gross price in minor units.
	Price Money
	// VATRate is the VAT rate of the price in basis points (1900 = 19 %);
	// zero for none.
	VATRate int
	// Active types are sold; a retired type stays for the passes sold.
	Active    bool
	CreatedAt time.Time
}

// Validate checks the pass type for consistency. All errors wrap
// ErrValidation.
func (t PassType) Validate() error {
	visits := t.Kind == PassVisits

	return validate(
		rule{!t.ID.IsZero(), "pass type id must be set"},
		on("name", !blank(t.Name), "pass type name must not be empty"),
		on("name", t.Name == strings.TrimSpace(t.Name), "pass type name must not start or end with spaces"),
		on("kind", t.Kind.Valid(), "invalid pass kind"),
		on("visits", !visits || (t.Visits > 0 && t.Visits <= MaxPassVisits), "visits must be between 1 and 1000"),
		on("visits", visits || t.Visits == 0, "a time pass has no number of visits"),
		on("validityDays", t.ValidityDays >= 0 && t.ValidityDays <= MaxPassValidityDays,
			"validity must be between 0 and 3660 days"),
		on("validityDays", visits || t.ValidityDays > 0, "a time pass lasts at least one day"),
		on("price", t.Price >= 0 && t.Price <= MaxPassPrice, "price out of range"),
		on("vatRate", t.VATRate >= 0 && t.VATRate <= MaxVATRate, "VAT rate must be between 0 and 10000 basis points"),
		rule{!t.CreatedAt.IsZero(), "pass type creation time must be set"},
	)
}

// ValidUntil returns the last day of a pass of type t whose first day is
// from: from plus ValidityDays − 1, or zero (no expiry) for a visit pass
// without a validity.
func (t PassType) ValidUntil(from Date) Date {
	if t.ValidityDays == 0 {
		return Date{}
	}

	return from.AddDays(t.ValidityDays - 1)
}

// PassStatus is the state of a pass.
type PassStatus string

// Known pass statuses.
const (
	// PassActive passes pay for visits while they are valid.
	PassActive PassStatus = "active"
	// PassVoided passes pay for nothing more; what they paid stays paid.
	PassVoided PassStatus = "voided"
)

// PassStatuses returns all pass statuses.
func PassStatuses() []PassStatus { return []PassStatus{PassActive, PassVoided} }

// Valid reports whether s is a known pass status.
func (s PassStatus) Valid() bool { return slices.Contains(PassStatuses(), s) }

// PassOrigin tells how a person got a pass.
type PassOrigin string

// Known pass origins.
const (
	// PassSold passes were paid for; Phase 6 B issues their receipts.
	PassSold PassOrigin = "sale"
	// PassAssigned passes were given without payment (a goodwill gesture, a
	// replacement), with a note why.
	PassAssigned PassOrigin = "assignment"
)

// PassOrigins returns all pass origins.
func PassOrigins() []PassOrigin { return []PassOrigin{PassSold, PassAssigned} }

// Valid reports whether o is a known pass origin.
func (o PassOrigin) Valid() bool { return slices.Contains(PassOrigins(), o) }

// Pass is a pass of one person (ADR 0012): a number of visits, or a period
// with any number of visits.
type Pass struct {
	ID         PassID
	PassTypeID PassTypeID
	PersonID   PersonID
	Kind       PassKind
	// VisitsTotal and VisitsRemaining count the visits of a visit pass;
	// both are zero for a time pass.
	VisitsTotal     int
	VisitsRemaining int
	// ValidFrom is the first school date the pass pays for; ValidUntil the
	// last, zero for a visit pass that does not expire.
	ValidFrom  Date
	ValidUntil Date
	Status     PassStatus
	Origin     PassOrigin
	SoldAt     time.Time
	SoldBy     AccountID // optional: the office account that sold or assigned it
	PayerID    PersonID  // optional: who paid, if not the owner; sales only
	ReceiptID  ReceiptID // optional: the receipt of the sale (Phase 6 B)
	PricePaid  Money     // zero for an assignment
	Note       string    // optional
	// VoidedAt is set exactly when Status is PassVoided.
	VoidedAt time.Time
}

// Validate checks the pass for consistency. All errors wrap ErrValidation.
func (p Pass) Validate() error {
	visits := p.Kind == PassVisits
	sold := p.Origin == PassSold

	return validate(
		rule{!p.ID.IsZero(), "pass id must be set"},
		rule{!p.PassTypeID.IsZero(), "pass type must be set"},
		rule{!p.PersonID.IsZero(), "pass owner must be set"},
		rule{p.Kind.Valid(), "invalid pass kind"},
		rule{!visits || (p.VisitsTotal > 0 && p.VisitsTotal <= MaxPassVisits), "pass visits must be between 1 and 1000"},
		rule{
			!visits || (p.VisitsRemaining >= 0 && p.VisitsRemaining <= p.VisitsTotal),
			"remaining visits must be between 0 and the total",
		},
		rule{visits || (p.VisitsTotal == 0 && p.VisitsRemaining == 0), "a time pass has no visits"},
		on("validFrom", p.ValidFrom.Valid(), "invalid first day"),
		rule{optionalDate(p.ValidUntil), "invalid last day"},
		rule{visits || !p.ValidUntil.IsZero(), "a time pass needs a last day"},
		on("validFrom", p.ValidUntil.IsZero() || p.ValidUntil.Compare(p.ValidFrom) >= 0,
			"the pass cannot end before it starts"),
		rule{p.Status.Valid(), "invalid pass status"},
		rule{p.Origin.Valid(), "invalid pass origin"},
		rule{!p.SoldAt.IsZero(), "pass sale time must be set"},
		on("payerId", sold || p.PayerID.IsZero(), "only a sold pass has a payer"),
		rule{sold || p.ReceiptID.IsZero(), "only a sold pass has a receipt"},
		on("price", p.PricePaid >= 0 && p.PricePaid <= MaxPassPrice, "price out of range"),
		on("price", sold || p.PricePaid == 0, "an assigned pass is free"),
		on("note", utf8.RuneCountInString(p.Note) <= MaxPassNoteLength, "the note is too long"),
		on("note", p.Note == strings.TrimSpace(p.Note), "the note must not start or end with spaces"),
		rule{(p.Status == PassVoided) == !p.VoidedAt.IsZero(), "void time must be set exactly for voided passes"},
		rule{notBefore(p.VoidedAt, p.SoldAt), "a pass cannot be voided before it was sold"},
	)
}

// ValidOn reports whether the pass is active and the school date d lies
// within its days, whatever visits it has left: whether a visit it paid for
// on d stays paid.
func (p Pass) ValidOn(d Date) bool {
	switch {
	case p.Status != PassActive, d.Compare(p.ValidFrom) < 0:
		return false
	case !p.ValidUntil.IsZero() && d.Compare(p.ValidUntil) > 0:
		return false
	}

	return true
}

// Covers reports whether the pass can pay for a new visit on the school
// date d: it is valid on d, and a visit pass has a visit left.
func (p Pass) Covers(d Date) bool {
	return p.ValidOn(d) && (p.Kind == PassTime || p.VisitsRemaining > 0)
}

// PassRedemption records that a pass paid for a booking: a visit of a visit
// pass, or a lesson within a time pass's period. A booking redeems at most
// one pass; the store enforces that.
type PassRedemption struct {
	PassID    PassID
	BookingID BookingID
	At        time.Time
}

// Validate checks the redemption for consistency. All errors wrap
// ErrValidation.
func (r PassRedemption) Validate() error {
	return validate(
		rule{!r.PassID.IsZero(), "redemption pass must be set"},
		rule{!r.BookingID.IsZero(), "redemption booking must be set"},
		rule{!r.At.IsZero(), "redemption time must be set"},
	)
}

// RedemptionMode is when bookings use a pass (ADR 0012), a school setting.
type RedemptionMode string

// Known redemption modes.
const (
	// RedeemOnBooking uses a pass when the lesson is booked; a timely
	// cancellation gives the visit back.
	RedeemOnBooking RedemptionMode = "booking"
	// RedeemOnAttendance uses a pass when attendance is taken (attended or
	// no-show) or the booking is canceled late.
	RedeemOnAttendance RedemptionMode = "attendance"
)

// RedemptionModes returns all redemption modes.
func RedemptionModes() []RedemptionMode { return []RedemptionMode{RedeemOnBooking, RedeemOnAttendance} }

// Valid reports whether m is a known redemption mode.
func (m RedemptionMode) Valid() bool { return slices.Contains(RedemptionModes(), m) }
