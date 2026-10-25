package domain

import (
	"bytes"
	"strings"
	"time"
	"unicode/utf8"
)

// MaxMemberNoLength is the longest member number, in characters.
const MaxMemberNoLength = 40

// MaxNotesLength is the longest notes of a person, in characters.
const MaxNotesLength = 2000

// The fixed name of an anonymized person (ADR 0016). The stored values are
// these; the web app shows a translated label instead.
const (
	AnonymizedGivenName  = "Deleted"
	AnonymizedFamilyName = "person"
)

// Person is any human the school deals with: a member, a child, a guest or
// a staff member. Optional fields use the zero value for "absent".
type Person struct {
	ID         PersonID
	GivenName  string
	FamilyName string
	Email      string // optional
	Phone      string // optional
	BirthDate  Date   // optional
	Notes      string // optional, internal
	// MemberNo is the school's own member number (ADR 0015), optional and
	// unique among persons: the key by which the member import finds a
	// person again.
	MemberNo string
	// MergedInto is the person this one was merged into (ADR 0015); zero
	// for a live person. A merged person is a tombstone: nothing refers to
	// it any more but its receipts and the audit log, it is left out of
	// lists and searches, and it has no member number.
	MergedInto PersonID
	// AnonymizedAt is when the office anonymized the person (ADR 0016);
	// zero for one that is not. An anonymized person has the fixed name
	// AnonymizedGivenName AnonymizedFamilyName and no other personal data;
	// like a tombstone it stays for its history (past bookings, passes,
	// receipts) and nothing new refers to it.
	AnonymizedAt time.Time
}

// Merged reports whether the person is a tombstone of a merge.
func (p Person) Merged() bool { return !p.MergedInto.IsZero() }

// Anonymized reports whether the office anonymized the person.
func (p Person) Anonymized() bool { return !p.AnonymizedAt.IsZero() }

// Anonymize returns p with the fixed name and without any other personal
// data, anonymized at the instant.
func (p Person) Anonymize(at time.Time) Person {
	return Person{
		ID: p.ID, GivenName: AnonymizedGivenName, FamilyName: AnonymizedFamilyName, MergedInto: p.MergedInto,
		AnonymizedAt: at,
	}
}

// Validate checks the person for consistency. All errors wrap ErrValidation.
func (p Person) Validate() error {
	return validate(
		rule{!p.ID.IsZero(), "person id must be set"},
		on("givenName", !blank(p.GivenName), "given name must not be empty"),
		on("familyName", !blank(p.FamilyName), "family name must not be empty"),
		on("email", optionalEmail(p.Email), "invalid email"),
		on("birthDate", optionalDate(p.BirthDate), "invalid birth date"),
		on("notes", utf8.RuneCountInString(p.Notes) <= MaxNotesLength, "notes are too long"),
		on("memberNo", p.MemberNo == strings.TrimSpace(p.MemberNo), "member number must not start or end with spaces"),
		on("memberNo", utf8.RuneCountInString(p.MemberNo) <= MaxMemberNoLength, "member number is too long"),
		rule{p.MergedInto != p.ID, "a person cannot be merged into itself"},
		rule{!p.Merged() || p.MemberNo == "", "a merged person has no member number"},
		rule{
			!p.Anonymized() || p == p.Anonymize(p.AnonymizedAt),
			"an anonymized person has the fixed name and no other personal data",
		},
	)
}

// Guardian links a guardian (parent) to a dependent person they may manage
// and book for. It has no ID of its own.
type Guardian struct {
	GuardianID  PersonID
	DependentID PersonID
}

// Validate checks the link for consistency. All errors wrap ErrValidation.
func (g Guardian) Validate() error {
	return validate(
		rule{!g.GuardianID.IsZero(), "guardian id must be set"},
		rule{!g.DependentID.IsZero(), "dependent id must be set"},
		on("guardianId", g.GuardianID != g.DependentID, "a person cannot be their own guardian"),
	)
}

// DuplicateDismissal records that the office looked at two persons the
// duplicate detection pairs and found that they are not the same (ADR
// 0015). The pair is ordered: PersonA's ID sorts before PersonB's, so a
// pair has one key whichever way it was named.
type DuplicateDismissal struct {
	PersonA     PersonID
	PersonB     PersonID
	DismissedBy AccountID
	DismissedAt time.Time
}

// NewDuplicateDismissal returns the dismissal of the pair a and b, ordered.
func NewDuplicateDismissal(a, b PersonID, by AccountID, at time.Time) DuplicateDismissal {
	if bytes.Compare(a[:], b[:]) > 0 {
		a, b = b, a
	}

	return DuplicateDismissal{PersonA: a, PersonB: b, DismissedBy: by, DismissedAt: at}
}

// Validate checks the dismissal for consistency. All errors wrap
// ErrValidation.
func (d DuplicateDismissal) Validate() error {
	return validate(
		rule{!d.PersonA.IsZero() && !d.PersonB.IsZero(), "both persons must be set"},
		rule{bytes.Compare(d.PersonA[:], d.PersonB[:]) < 0, "the pair must be ordered and of two persons"},
		rule{!d.DismissedBy.IsZero(), "the dismissing account must be set"},
		rule{!d.DismissedAt.IsZero(), "the dismissal time must be set"},
	)
}
