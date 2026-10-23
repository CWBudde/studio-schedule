package domain

import "time"

// Account is the login of a person. Its password is a separate Credential.
type Account struct {
	ID       AccountID
	PersonID PersonID
	// Email is the login name; the store keeps it unique case-insensitively.
	Email string
	Role  Role
	// VisibleDays overrides Settings.DefaultVisibleDays (early access)
	// when positive.
	VisibleDays int
	// ConfirmedAt is set once the email address is confirmed. Unconfirmed
	// accounts cannot log in.
	ConfirmedAt time.Time
	// DisabledAt is set when the account may no longer log in.
	DisabledAt time.Time
	// Locale is the language of the mails the account receives, a mail
	// template locale such as "de"; empty means the school's default.
	Locale string
}

// Validate checks the account for consistency. All errors wrap
// ErrValidation.
func (a Account) Validate() error {
	return validate(
		rule{!a.ID.IsZero(), "account id must be set"},
		rule{!a.PersonID.IsZero(), "account person must be set"},
		rule{validEmail(a.Email), "invalid account email"},
		rule{a.Role.Valid() && a.Role != RoleGuest, "invalid account role"},
		rule{a.VisibleDays >= 0 && a.VisibleDays <= maxVisibleDays, "visible days must be between 0 and 366"},
		rule{a.Locale == "" || !blank(a.Locale), "account locale must not be blank"},
	)
}
