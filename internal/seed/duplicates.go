package seed

import (
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
)

// demoDuplicates are two pairs of persons the office's duplicate detection
// finds (ADR 0015): Lena twice with the same email, ignoring case (a strong
// match), and Jonas twice with the same name, once without a birth date (a
// weak one). None has an account, so either pair can be merged.
var demoDuplicates = []domain.Person{
	{
		GivenName: "Lena", FamilyName: "Quillfeather", Email: "lena.quillfeather@example.com",
		Phone: "+49 000 0000201", MemberNo: "M-1042",
	},
	{
		GivenName: "Lena", FamilyName: "Quillfeather", Email: "LENA.Quillfeather@example.com",
		BirthDate: date(1994, time.August, 17), Notes: "Registered at the front desk.",
	},
	{GivenName: "Jonas", FamilyName: "Brightwater", BirthDate: date(2014, time.May, 12), MemberNo: "M-1077"},
	{GivenName: "Jonas", FamilyName: "Brightwater", Phone: "+49 000 0000202", Notes: "Walk-in, trial lesson."},
}

// duplicates adds the demo's duplicate pairs.
func (b *builder) duplicates() {
	for _, p := range demoDuplicates {
		b.person(p, "", "")
	}
}
