package domain

// Role is the authorisation role of an account.
type Role string

// Known roles, ordered from least to most privileged.
const (
	RoleGuest      Role = "guest"
	RoleMember     Role = "member"
	RoleInstructor Role = "instructor"
	RoleOffice     Role = "office"
	RoleAdmin      Role = "admin"
)

// Roles returns all known roles, ordered from least to most privileged.
func Roles() []Role {
	return []Role{RoleGuest, RoleMember, RoleInstructor, RoleOffice, RoleAdmin}
}

// Valid reports whether r is one of the known roles.
func (r Role) Valid() bool {
	switch r {
	case RoleGuest, RoleMember, RoleInstructor, RoleOffice, RoleAdmin:
		return true
	default:
		return false
	}
}

// String implements fmt.Stringer.
func (r Role) String() string { return string(r) }

// AtLeast reports whether r is a known role with at least the privileges
// of minimum, in the order of Roles. An unknown role has none.
func (r Role) AtLeast(minimum Role) bool {
	return r.rank() >= minimum.rank() && r.Valid()
}

func (r Role) rank() int {
	for i, known := range Roles() {
		if r == known {
			return i
		}
	}

	return -1
}

// Staff reports whether r is a staff role: instructor, office or admin.
func (r Role) Staff() bool { return r.AtLeast(RoleInstructor) }
