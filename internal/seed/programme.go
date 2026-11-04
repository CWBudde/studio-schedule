package seed

import (
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
)

// programme returns the tables of the programme school (SchoolProgramme),
// the demo school in the app's own look with DemoSettings and no
// background. Each call returns fresh tables.
//
// All names, addresses, phone numbers and emails are invented. Emails use
// the reserved example domains only.
func programme() schoolSpec {
	return schoolSpec{
		settings: DemoSettings(),
		households: []householdSpec{
			// Families with children.
			{
				"Amberly", "example.org",
				[]adultSpec{{"Clara", domain.RoleMember, "+49 000 0000101"}, {"Tom", domain.RoleMember, ""}},
				[]childSpec{{"Ella", date(2016, time.April, 12), ""}, {"Finn", date(2019, time.September, 3), ""}},
			},
			{
				"Birchmoor", "example.com",
				[]adultSpec{{"Hanna", domain.RoleMember, "+49 000 0000102"}},
				[]childSpec{{"Lotta", date(2014, time.February, 20), ""}, {"Mia", date(2020, time.June, 15), ""}},
			},
			{
				"Cloudsley", "example.org",
				[]adultSpec{{"Jan", domain.RoleMember, ""}},
				[]childSpec{{"Noah", date(2013, time.November, 8), ""}},
			},
			{
				"Dunfield", "example.com",
				[]adultSpec{{"Sara", domain.RoleMember, "+49 000 0000104"}, {"Ali", "", ""}},
				[]childSpec{{"Leni", date(2017, time.January, 25), ""}},
			},
			{
				"Elderwick", "example.org",
				[]adultSpec{{"Petra", domain.RoleMember, ""}},
				[]childSpec{
					{"Ida", date(2012, time.July, 30), ""},
					{"Emil", date(2015, time.March, 14), ""},
					{"Rosa", date(2018, time.October, 1), ""},
				},
			},
			{
				"Fernwhistle", "example.com",
				[]adultSpec{{"Marta", domain.RoleMember, ""}, {"Lukas", domain.RoleMember, "+49 000 0000106"}},
				[]childSpec{{"Jona", date(2011, time.May, 19), ""}},
			},
			{
				"Gildenmoor", "example.org",
				[]adultSpec{{"Yara", domain.RoleMember, ""}},
				[]childSpec{{"Kira", date(2016, time.December, 2), ""}, {"Ben", date(2014, time.August, 22), ""}},
			},
			{
				"Hollowmere", "example.com",
				[]adultSpec{{"David", domain.RoleMember, ""}},
				// Sophie is a teenager with a login of her own: a managed
				// dependent, who books her classes herself.
				[]childSpec{{"Sophie", date(2010, time.March, 11), domain.RoleMember}},
			},
			{
				"Ivorycombe", "example.org",
				[]adultSpec{{"Nora", domain.RoleMember, "+49 000 0000109"}},
				[]childSpec{{"Jakob", date(2015, time.July, 7), ""}},
			},

			// Adults without children.
			{"Inkwell", "example.com", []adultSpec{{"Irene", domain.RoleMember, ""}}, nil},
			{"Lanternby", "example.org", []adultSpec{{"Kai", domain.RoleMember, ""}}, nil},
			{"Mistlewood", "example.com", []adultSpec{{"Lina", domain.RoleMember, "+49 000 0000112"}}, nil},
			{"Nettlebury", "example.org", []adultSpec{{"Oskar", domain.RoleMember, ""}}, nil},
			{"Pinecrest", "example.com", []adultSpec{{"Rhea", domain.RoleMember, ""}}, nil},
			{"Quarrystone", "example.org", []adultSpec{{"Tilo", domain.RoleMember, ""}}, nil},
			{"Rushwick", "example.com", []adultSpec{{"Vera", domain.RoleMember, ""}}, nil},
			{"Sparrowhill", "example.org", []adultSpec{{"Wanda", domain.RoleMember, ""}}, nil},
			{"Tallowfield", "", []adultSpec{{"Ute", "", "+49 000 0000118"}}, nil}, // walk-in guest without an account

			// Staff and office.
			{"Starling", "example.net", []adultSpec{{"Alina", domain.RoleInstructor, ""}}, nil},
			{"Quillon", "", []adultSpec{{"Sofia", "", ""}}, nil},
			// Felix accompanies; his instructor account sees the lessons he plays
			// for. Alina comes first, so she stays the demo's instructor login.
			{"Keysworth", "example.net", []adultSpec{{"Felix", domain.RoleInstructor, ""}}, nil},
			{"Ledgerby", "example.net", []adultSpec{{"Olga", domain.RoleOffice, ""}}, nil},
			{"Keystone", "example.net", []adultSpec{{"Arno", domain.RoleAdmin, ""}}, nil},
		},
		locations: []locationSpec{
			{"Studio North", "Example Street 12, 00000 Example Town", []roomSpec{
				{name: "Studio A", capacity: 20}, {name: "Studio B", capacity: 14}, {name: "Studio C", capacity: 10},
			}},
			{"Studio South", "Sample Road 3, 00000 Example Town", []roomSpec{
				{name: "Hall 1", capacity: 24}, {name: "Hall 2", capacity: 12},
			}},
		},
		staff: []staffSpec{
			{"Alina Starling", "Alina Starling", domain.StaffInstructor, true},
			{"Sofia Quillon", "Sofia Quillon", domain.StaffInstructor, true},
			{"Marco Wrenfield", "", domain.StaffInstructor, true},
			{"Jonas Featherby", "", domain.StaffInstructor, true},
			{"Lea Moonfield", "", domain.StaffInstructor, true},
			{"Nina Tapworth", "", domain.StaffInstructor, true},
			{"Quentin Oldstage", "", domain.StaffInstructor, false}, // former instructor
			{"Felix Keysworth", "Felix Keysworth", domain.StaffAccompanist, true},
			{"Greta Ivorylane", "", domain.StaffAccompanist, true},
		},
		levels: []levelSpec{
			{"Beginner", "B"},
			{"Intermediate", "I"},
			{"Advanced", "A"},
			{"Professional", "P"},
		},
		courseTypes: []domain.CourseType{
			{Name: "Creative Dance", Description: "Playful movement and music for children aged 4 to 6."},
			{Name: "Pre-Ballet", Description: "First ballet steps and posture for children aged 6 to 8."},
			{Name: "Classical Ballet", Description: "Barre, center work and allegro with live piano."},
			{Name: "Pointe Work", Description: "Pointe technique for advanced ballet students."},
			{Name: "Contemporary", Description: "Floor work, release technique and improvisation."},
			{Name: "Jazz Dance", Description: "Isolations, turns and jumps to jazz and pop music."},
			{Name: "Hip-Hop", Description: "Grooves, footwork and choreography."},
			{Name: "Tap", Description: "Rhythm and sound with tap shoes."},
			{Name: "Barre Fitness", Description: "Strength and stretching at the barre, no experience needed."},
		},
		classes: []classSpec{
			// Monday
			{
				time.Monday, at(15, 30), 45, "Studio C", "Creative Dance", "Sofia Quillon", "", ages(48, 72), 10,
				[]string{"Mia Birchmoor", "Finn Amberly"},
			},
			{
				time.Monday, at(16, 30), 60, "Studio A", "Classical Ballet", "Alina Starling", "Felix Keysworth", level("Beginner"), 16,
				[]string{"Rosa Elderwick", "Leni Dunfield", "Ella Amberly"},
			},
			{
				time.Monday, at(17, 45), 75, "Studio A", "Classical Ballet", "Alina Starling", "Felix Keysworth", level("Intermediate"), 16,
				[]string{"Lotta Birchmoor", "Ben Gildenmoor", "Emil Elderwick"},
			},
			{
				time.Monday, at(18, 0), 60, "Hall 1", "Hip-Hop", "Jonas Featherby", "", level("Beginner"), 20,
				[]string{"Noah Cloudsley", "Jakob Ivorycombe"},
			},
			{
				time.Monday, at(19, 15), 90, "Studio A", "Classical Ballet", "Alina Starling", "Felix Keysworth", level("Advanced"), 16,
				[]string{"Rhea Pinecrest", "Wanda Sparrowhill"},
			},

			// Tuesday
			{
				time.Tuesday, at(16, 0), 45, "Studio B", "Pre-Ballet", "Sofia Quillon", "", ages(72, 96), 12,
				[]string{"Mia Birchmoor", "Rosa Elderwick"},
			},
			{
				time.Tuesday, at(17, 0), 60, "Hall 1", "Jazz Dance", "Nina Tapworth", "", level("Beginner"), 20,
				[]string{"Kira Gildenmoor", "Ella Amberly", "Emil Elderwick"},
			},
			{
				time.Tuesday, at(18, 30), 75, "Studio A", "Contemporary", "Marco Wrenfield", "", level("Intermediate"), 18,
				[]string{"Kai Lanternby", "Lina Mistlewood", "Marta Fernwhistle"},
			},
			{
				time.Tuesday, at(19, 0), 60, "Hall 2", "Barre Fitness", "Nina Tapworth", "", allLevels, 12,
				[]string{"Clara Amberly", "Irene Inkwell", "Vera Rushwick", "Hanna Birchmoor"},
			},

			// Wednesday
			{
				time.Wednesday, at(15, 30), 45, "Studio C", "Creative Dance", "Sofia Quillon", "", ages(48, 72), 10,
				[]string{"Mia Birchmoor"},
			},
			{
				time.Wednesday, at(16, 30), 75, "Studio A", "Classical Ballet", "Lea Moonfield", "Greta Ivorylane", level("Intermediate"), 16,
				[]string{"Ida Elderwick", "Kira Gildenmoor", "Lotta Birchmoor"},
			},
			{
				time.Wednesday, at(18, 0), 45, "Studio B", "Pointe Work", "Lea Moonfield", "Greta Ivorylane", level("Advanced"), 12,
				[]string{"Ida Elderwick", "Jona Fernwhistle", "Sophie Hollowmere"},
			},
			{
				time.Wednesday, at(18, 30), 60, "Hall 1", "Hip-Hop", "Jonas Featherby", "", level("Intermediate"), 20,
				[]string{"Noah Cloudsley", "Ben Gildenmoor"},
			},
			{
				time.Wednesday, at(19, 0), 90, "Studio A", "Classical Ballet", "Lea Moonfield", "Greta Ivorylane", level("Professional"), 16,
				[]string{"Wanda Sparrowhill"},
			},

			// Thursday
			{
				time.Thursday, at(16, 0), 60, "Hall 2", "Tap", "Nina Tapworth", "", level("Beginner"), 12,
				[]string{"Leni Dunfield", "Jakob Ivorycombe"},
			},
			{
				time.Thursday, at(17, 0), 60, "Studio A", "Contemporary", "Marco Wrenfield", "", level("Beginner"), 18,
				[]string{"Lotta Birchmoor", "Ida Elderwick"},
			},
			{
				time.Thursday, at(18, 30), 90, "Studio A", "Classical Ballet", "Alina Starling", "Felix Keysworth",
				audienceSpec{from: "Beginner", to: "Intermediate"},
				16,
				[]string{"Oskar Nettlebury", "Tilo Quarrystone", "Sara Dunfield", "Petra Elderwick"},
			},
			{
				time.Thursday, at(19, 30), 60, "Hall 1", "Jazz Dance", "Marco Wrenfield", "", level("Intermediate"), 20,
				[]string{"Lukas Fernwhistle", "Kai Lanternby"},
			},

			// Friday
			{
				time.Friday, at(15, 30), 45, "Studio B", "Pre-Ballet", "Sofia Quillon", "", ages(72, 96), 12,
				[]string{"Finn Amberly"},
			},
			{
				time.Friday, at(16, 30), 60, "Studio A", "Classical Ballet", "Lea Moonfield", "Greta Ivorylane", level("Beginner"), 16,
				[]string{"Leni Dunfield", "Kira Gildenmoor"},
			},
			{
				time.Friday, at(17, 45), 60, "Hall 1", "Hip-Hop", "Jonas Featherby", "", allLevels, 24,
				[]string{"Jan Cloudsley", "Tilo Quarrystone"},
			},
			{
				time.Friday, at(18, 0), 75, "Studio B", "Pointe Work", "Lea Moonfield", "", level("Advanced"), 12,
				[]string{"Sophie Hollowmere"},
			},

			// Saturday
			{time.Saturday, at(10, 0), 45, "Studio C", "Creative Dance", "Sofia Quillon", "", ages(48, 72), 10, nil},
			{
				time.Saturday, at(10, 0), 90, "Studio A", "Classical Ballet", "Alina Starling", "Felix Keysworth", level("Advanced"), 16,
				[]string{"Jona Fernwhistle", "Sophie Hollowmere", "Ida Elderwick"},
			},
			{
				time.Saturday, at(11, 0), 60, "Hall 2", "Tap", "Nina Tapworth", "", allLevels, 12,
				[]string{"Irene Inkwell", "Nora Ivorycombe", "David Hollowmere"},
			},
			{
				time.Saturday, at(12, 0), 75, "Studio A", "Contemporary", "Marco Wrenfield", "", allLevels, 18,
				[]string{"Lina Mistlewood", "Yara Gildenmoor"},
			},
		},
		// The workshops start at 13:30 and end at 15:30, when no weekly class
		// runs, so they never clash with one on whatever weekday they fall. The
		// demo releases one week ahead (DemoSettings), so the first workshop is
		// usually released already and the others lie beyond every release
		// window: the schedule shows them because workshops are published
		// early. Two more show the other visibilities: one only signed-in
		// members see, and one only staff see.
		workshops: []workshopSpec{
			{3, at(13, 30), 120, "Studio A", "Contemporary", "Marco Wrenfield", "", 18, ""},
			{17, at(13, 30), 120, "Hall 1", "Jazz Dance", "Nina Tapworth", "", 20, ""},
			{24, at(13, 30), 120, "Studio A", "Classical Ballet", "Alina Starling", "Felix Keysworth", 16, ""},
			{5, at(13, 30), 120, "Studio B", "Contemporary", "Marco Wrenfield", "", 12, domain.VisibilityMembers},
			{6, at(13, 30), 120, "Hall 1", "Hip-Hop", "Nina Tapworth", "", 15, domain.VisibilityHidden},
		},
		// The full workshop lies in the second week after the anchor, after
		// the first workshops, when no other workshop takes place. Its members
		// booked it two days before the anchor and joined its waiting list the
		// evening before, so the demo shows a full lesson, its list and the
		// places on it; nobody of a demo login's family is on it.
		fullWorkshop: fullWorkshopSpec{
			workshop: workshopSpec{10, at(13, 30), 90, "Studio C", "Tap", "Nina Tapworth", "", 4, ""},
			booked:   []string{"Irene Inkwell", "Kai Lanternby", "Oskar Nettlebury", "Rhea Pinecrest"},
			waiting:  []waitingSpec{{"Vera Rushwick", true}, {"Wanda Sparrowhill", false}},
		},
		// The staff bookings show the staff actions in the participant list of
		// Alina's ballet workshop: a guest with an email, a walk-in guest
		// without one, and a trial with a note for her.
		staffBookings: staffBookingsSpec{
			instructor: "Alina Starling",
			participants: []participantSpec{
				{guest: "Mara Gastfield", email: "mara.gastfield@example.com"},
				{guest: "Pia Walkby"},
				{person: "Ute Tallowfield", trial: true, note: "First ballet class in years; mind her left knee."},
			},
		},
		// The rental lies on the day between the first two workshops, at their
		// time, when no weekly class runs: staff see it in its room, nobody
		// else.
		rental: rentalSpec{4, at(13, 30), 120, "Studio B", "Copperleaf Theater Group"},
		// The events (ADR 0019), with their sessions at times and in rooms
		// no class, workshop or the rental uses on whatever day they fall:
		// the intensive on the weekend after the anchor, booked up but one
		// place, which the member login can take for a child; the free taster
		// in the second week, with a guest the office added at the desk; and
		// a draft that only the office sees. Nobody of a demo login's family
		// is registered.
		events: []eventSpec{
			{
				slug: "contemporary-weekend-intensive", title: "Contemporary Weekend Intensive",
				description: "Three sessions over one weekend: floor work, release technique and a short piece " +
					"to take home. For dancers with some contemporary experience.",
				courseType: "Contemporary", capacity: 12, price: 12000, vatRate: 1900, status: domain.EventPublished,
				on: on(time.Saturday), days: 1,
				sessions: []sessionSpec{
					{days: 0, start: at(16, 0), minutes: 150, room: "Hall 1", instructor: "Marco Wrenfield"},
					{days: 1, start: at(10, 0), minutes: 150, room: "Hall 1", instructor: "Marco Wrenfield"},
					{days: 1, start: at(16, 0), minutes: 120, room: "Hall 1", instructor: "Marco Wrenfield"},
				},
				registered: []string{
					"Kai Lanternby", "Lina Mistlewood", "Marta Fernwhistle", "Jona Fernwhistle", "Sophie Hollowmere",
					"Ida Elderwick", "Lotta Birchmoor", "Yara Gildenmoor", "Rhea Pinecrest", "Wanda Sparrowhill",
					"Irene Inkwell",
				},
				paid: "Lina Mistlewood", method: domain.PaymentCard,
			},
			{
				slug: "open-ballet-taster", title: "Open Ballet Taster",
				description: "A free ballet class with live piano for anyone who wants to try ballet. " +
					"No experience needed; comfortable clothes are fine.",
				courseType: "Classical Ballet", capacity: 20, vatRate: 1900, status: domain.EventPublished,
				on: on(time.Sunday), days: 10,
				sessions: []sessionSpec{
					{
						days: 0, start: at(10, 30), minutes: 90, room: "Hall 1", instructor: "Lea Moonfield",
						accompanist: "Greta Ivorylane",
					},
				},
				registered: []string{"Kira Gildenmoor", "Tilo Quarrystone"},
				guests:     []participantSpec{{guest: "Juna Firstlight", email: "juna.firstlight@example.org"}},
			},
			{
				slug: "summer-showcase-rehearsal", title: "Summer Showcase Rehearsal",
				description: "The first rehearsal for the summer showcase.",
				courseType:  "Classical Ballet", capacity: 16, vatRate: 1900, status: domain.EventDraft,
				on: on(time.Sunday), days: 21,
				sessions: []sessionSpec{
					{
						days: 0, start: at(10, 0), minutes: 120, room: "Studio A", instructor: "Alina Starling",
						accompanist: "Felix Keysworth",
					},
				},
			},
		},
		// The pass catalog: a drop-in, a 10-visit pass and a 30-day pass, gross
		// prices with 19 % VAT.
		passTypes: []domain.PassType{
			{Name: "Single visit", Kind: domain.PassVisits, Visits: 1, ValidityDays: 90, Price: 1800, VATRate: 1900, Active: true},
			{Name: "10-visit pass", Kind: domain.PassVisits, Visits: 10, ValidityDays: 180, Price: 15000, VATRate: 1900, Active: true},
			{Name: "30-day pass", Kind: domain.PassTime, ValidityDays: 30, Price: 8900, VATRate: 1900, Active: true},
		},
		// The passes of the member login's family (Clara with her own card and
		// her daughter's, which she paid for), a time pass, a pass given away
		// and one sold by mistake and voided. The demo redeems on attendance
		// (DemoSettings), so the passes paid for the drop-ins when the office
		// took their attendance.
		passes: []passSpec{
			{
				person: "Clara Amberly", passType: "10-visit pass", soldDays: -14, method: domain.PaymentCard,
				dropIns: []classSlot{{time.Tuesday, at(18, 30)}, {time.Thursday, at(17, 0)}},
			},
			{
				person: "Ella Amberly", passType: "10-visit pass", soldDays: -10, payer: "Clara Amberly",
				method: domain.PaymentTransfer, dropIns: []classSlot{{time.Thursday, at(16, 0)}},
			},
			{
				person: "Kai Lanternby", passType: "30-day pass", soldDays: -7, method: domain.PaymentCash,
				dropIns: []classSlot{{time.Friday, at(17, 45)}},
			},
			{
				person: "Irene Inkwell", passType: "Single visit", soldDays: -3, assigned: true,
				note: "Goodwill: her workshop was canceled.",
			},
			{
				person: "Vera Rushwick", passType: "Single visit", soldDays: -20, voided: true, method: domain.PaymentCash,
				note: "Sold to the wrong person.",
			},
		},
		// Two announcements of the office: one for everyone, and one for the
		// members of the Creative Dance classes, which the demo member sees
		// through her son.
		announcements: []announcementSpec{
			{
				title: "Welcome to the new booking system",
				body: "From now on you book and cancel lessons here, for yourself and your children.\n" +
					"Questions? Just ask at the front desk.",
				days: -3,
			},
			{
				title: "Costume fitting for the winter show",
				body: "Please come to the front desk on Saturday morning for the costume fitting.\n" +
					"It takes about twenty minutes.",
				courseType: "Creative Dance", days: -1, expiresDays: 14,
			},
		},
		// Two pairs of persons the office's duplicate detection finds (ADR
		// 0015): Lena twice with the same email, ignoring case (a strong
		// match), and Jonas twice with the same name, once without a birth date
		// (a weak one). None has an account, so either pair can be merged.
		duplicates: []domain.Person{
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
		},
		// Olga is the office: she sells the passes, writes the announcements
		// and records the attendance of instructors without a login.
		office: "Olga Ledgerby",
		// Classical Ballet's instructors are behind with the attendance, so
		// the instructor and office views show lessons to take attendance of.
		attendanceSkipped: "Classical Ballet",
		// The walk-in guests the instructors added to past lessons, in turn;
		// the trialists tried a class they are no member of (nobody of the
		// member login's family).
		guests:     []string{"Theo Dropwell", "Lena Visby", "Jonte Comewell", "Ivy Passmore"},
		trialists:  []string{"Nora Ivorycombe", "Yara Gildenmoor", "Hanna Birchmoor", "Oskar Nettlebury", "Petra Elderwick"},
		trialNotes: []string{"Thinking about joining after the holidays.", "", "Wants to try a second style."},
		termName:   schoolYear("School Year"),
	}
}
