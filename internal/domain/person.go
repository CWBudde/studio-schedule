package domain

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
}

// Validate checks the person for consistency. All errors wrap ErrValidation.
func (p Person) Validate() error {
	return validate(
		rule{!p.ID.IsZero(), "person id must be set"},
		on("givenName", !blank(p.GivenName), "given name must not be empty"),
		on("familyName", !blank(p.FamilyName), "family name must not be empty"),
		on("email", optionalEmail(p.Email), "invalid email"),
		on("birthDate", optionalDate(p.BirthDate), "invalid birth date"),
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
