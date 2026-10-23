package domain

import "time"

// PassType is a product in the pass catalog, e.g. a 10-visit pass.
type PassType struct {
	ID     PassTypeID
	Name   string
	Visits int
	Price  Money
	// ValidityDays is how long a pass stays valid after purchase; zero means
	// it does not expire.
	ValidityDays int
	Active       bool
}

// Validate checks the pass type for consistency. All errors wrap
// ErrValidation.
func (p PassType) Validate() error {
	return validate(
		rule{!p.ID.IsZero(), "pass type id must be set"},
		rule{!blank(p.Name), "pass type name must not be empty"},
		rule{p.Visits > 0, "pass type visits must be positive"},
		rule{p.Price >= 0, "pass type price must not be negative"},
		rule{p.ValidityDays >= 0, "pass type validity must not be negative"},
	)
}

// Pass is a prepaid set of visits owned by a person.
type Pass struct {
	ID              PassID
	PassTypeID      PassTypeID
	PersonID        PersonID
	VisitsTotal     int
	VisitsRemaining int
	PurchasedOn     Date
	ExpiresOn       Date      // optional: zero means no expiry
	ReceiptID       ReceiptID // optional
}

// Validate checks the pass for consistency. All errors wrap ErrValidation.
func (p Pass) Validate() error {
	return validate(
		rule{!p.ID.IsZero(), "pass id must be set"},
		rule{!p.PassTypeID.IsZero(), "pass type must be set"},
		rule{!p.PersonID.IsZero(), "pass owner must be set"},
		rule{p.VisitsTotal > 0, "pass visits must be positive"},
		rule{p.VisitsRemaining >= 0 && p.VisitsRemaining <= p.VisitsTotal, "remaining visits must be between 0 and the total"},
		rule{p.PurchasedOn.Valid(), "invalid pass purchase date"},
		rule{optionalDate(p.ExpiresOn), "invalid pass expiry date"},
		rule{p.ExpiresOn.IsZero() || p.ExpiresOn.Compare(p.PurchasedOn) >= 0, "pass cannot expire before it was bought"},
	)
}

// PassRedemption records that a booking used one visit of a pass. A booking
// redeems at most one pass; the store enforces that.
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
