package seed

import (
	_ "embed"
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
)

// werkstattBackground is the Werkstatt school's background image, a CC0
// photo (assets/README.md).
//
//go:embed assets/werkstatt-background.jpg
var werkstattBackground string

// einzelstunde is the werkstatt school's single-lesson pass type.
const einzelstunde = "Einzelstunde"

// werkstatt returns the tables of the werkstatt school (SchoolWerkstatt), a
// German school in one house with its own people, catalog and weekly
// plan. Its catalog and plan are shaped like the previous app's (the
// course kinds, the level codes, the durations and the rhythm of the week;
// spec 2026-11-01, §3), moved to other days, rooms and times and renamed;
// everything else is invented: every person, name, email, description,
// price and address. Its households mirror the programme school's record by
// record (the same roles, families and guardians), so the same scenarios
// exist: a member with several children, a managed teenager, a walk-in
// guest, a former instructor, an accompanist with a login. The programme
// school's rules apply (DemoSettings). Each call returns fresh tables and a
// fresh copy of the background, since the store owns what it is given.
func werkstatt() schoolSpec {
	return schoolSpec{
		settings:   werkstattSettings(),
		background: domain.Background{ContentType: domain.BackgroundJPEG, Data: []byte(werkstattBackground)},
		households: []householdSpec{
			// Families with children. Paul is three, so the member login's
			// family has a child in the Tanzzwerge, the youngest class.
			{
				"Ahrenfeld", "post.example",
				[]adultSpec{{"Katrin", domain.RoleMember, "+49 000 0000301"}, {"Stefan", domain.RoleMember, ""}},
				[]childSpec{{"Marie", date(2018, time.April, 12), ""}, {"Paul", date(2023, time.May, 3), ""}},
			},
			{
				"Birkholz", "mail.example",
				[]adultSpec{{"Sabine", domain.RoleMember, "+49 000 0000302"}},
				[]childSpec{{"Hannah", date(2014, time.February, 20), ""}, {"Emma", date(2021, time.June, 15), ""}},
			},
			{
				"Clausing", "post.example",
				[]adultSpec{{"Jürgen", domain.RoleMember, ""}},
				[]childSpec{{"Luca", date(2013, time.November, 8), ""}},
			},
			{
				"Dürrbach", "mail.example",
				[]adultSpec{{"Anja", domain.RoleMember, "+49 000 0000304"}, {"Mehmet", "", ""}},
				[]childSpec{{"Lina", date(2017, time.January, 25), ""}},
			},
			{
				"Eichhorst", "post.example",
				[]adultSpec{{"Birgit", domain.RoleMember, ""}},
				[]childSpec{
					{"Greta", date(2012, time.July, 30), ""},
					{"Moritz", date(2015, time.March, 14), ""},
					{"Frieda", date(2020, time.October, 1), ""},
				},
			},
			{
				"Fährmann", "mail.example",
				[]adultSpec{{"Ulrike", domain.RoleMember, ""}, {"Matthias", domain.RoleMember, "+49 000 0000306"}},
				[]childSpec{{"Jannik", date(2011, time.May, 19), ""}},
			},
			{
				"Grünhagen", "post.example",
				[]adultSpec{{"Yasemin", domain.RoleMember, ""}},
				[]childSpec{{"Smilla", date(2016, time.December, 2), ""}, {"Henri", date(2014, time.August, 22), ""}},
			},
			{
				"Hollerbach", "mail.example",
				[]adultSpec{{"Thomas", domain.RoleMember, ""}},
				// Charlotte is a teenager with a login of her own: a managed
				// dependent, who books her classes herself.
				[]childSpec{{"Charlotte", date(2010, time.March, 11), domain.RoleMember}},
			},
			{
				"Imhoff", "post.example",
				[]adultSpec{{"Daniela", domain.RoleMember, "+49 000 0000309"}},
				[]childSpec{{"Anton", date(2022, time.July, 7), ""}},
			},

			// Adults without children.
			{"Kranich", "mail.example", []adultSpec{{"Ingrid", domain.RoleMember, ""}}, nil},
			{"Lerchenfeld", "post.example", []adultSpec{{"Florian", domain.RoleMember, ""}}, nil},
			{"Mohnhaupt", "mail.example", []adultSpec{{"Miriam", domain.RoleMember, "+49 000 0000312"}}, nil},
			{"Nesselbach", "post.example", []adultSpec{{"Bernd", domain.RoleMember, ""}}, nil},
			{"Pappelstein", "mail.example", []adultSpec{{"Ronja", domain.RoleMember, ""}}, nil},
			{"Quendel", "post.example", []adultSpec{{"Torsten", domain.RoleMember, ""}}, nil},
			{"Rabenau", "mail.example", []adultSpec{{"Vanessa", domain.RoleMember, ""}}, nil},
			{"Weißdorn", "post.example", []adultSpec{{"Wiebke", domain.RoleMember, ""}}, nil},
			{"Tauberich", "", []adultSpec{{"Ursula", "", "+49 000 0000318"}}, nil}, // walk-in guest without an account

			// Staff and office.
			{"Sternberg", "tanzhaus-lindenhof.example", []adultSpec{{"Johanna", domain.RoleInstructor, ""}}, nil},
			{"Quast", "", []adultSpec{{"Marlene", "", ""}}, nil},
			// Konrad accompanies; his instructor account sees the lessons he
			// plays for. Johanna comes first, so she stays the demo's
			// instructor login.
			{"Flügel", "tanzhaus-lindenhof.example", []adultSpec{{"Konrad", domain.RoleInstructor, ""}}, nil},
			{"Buchwald", "tanzhaus-lindenhof.example", []adultSpec{{"Heike", domain.RoleOffice, ""}}, nil},
			{"Ostwald", "tanzhaus-lindenhof.example", []adultSpec{{"Rainer", domain.RoleAdmin, ""}}, nil},
		},
		// One house with two halls, the online room and a training room, in
		// the house's order. The address is fictional.
		locations: []locationSpec{
			{"Tanzhaus am Lindenhof", "Lindenhofweg 12, 10999 Beispielstadt", []roomSpec{
				{name: "Oben", capacity: 24},
				{name: "Unten", capacity: 18},
				{name: "Online", capacity: 40},
				{name: "Trainingsraum", capacity: 8},
			}},
		},
		// Each instructor teaches one kind of course, so the plan reads:
		// Johanna and Valentina ballet, Marlene the children's classes,
		// Lars contemporary, Nadine jazz and the beginners' classes, Kilian
		// step, Irish and hip-hop, Gudrun the health classes. Konrad plays
		// for Johanna's ballet classes, Elise for Valentina's.
		staff: []staffSpec{
			{"Johanna Sternberg", "Johanna Sternberg", domain.StaffInstructor, true},
			{"Marlene Quast", "Marlene Quast", domain.StaffInstructor, true},
			{"Valentina Rossbach", "", domain.StaffInstructor, true},
			{"Lars Feldkamp", "", domain.StaffInstructor, true},
			{"Nadine Brenner", "", domain.StaffInstructor, true},
			{"Kilian Hartwig", "", domain.StaffInstructor, true},
			{"Gudrun Seidel", "", domain.StaffInstructor, true},
			{"Werner Altmann", "", domain.StaffInstructor, false}, // former instructor
			{"Konrad Flügel", "Konrad Flügel", domain.StaffAccompanist, true},
			{"Elise Hammerschlag", "", domain.StaffAccompanist, true},
		},
		levels: []levelSpec{
			{"Einsteiger", "E"},
			{"Anfänger", "A"},
			{"Mittelstufe", "M"},
			{"Fortgeschritten", "F"},
			{"Profi", "P"},
		},
		// The 23 course types of the weekly plan, then three that only the
		// Sunday workshops use.
		courseTypes: []domain.CourseType{
			{Name: "Tanzzwerge", Description: "Spielerisches Bewegen zu Musik und Geschichten für die Kleinsten ab drei Jahren."},
			{Name: "Tänzerische Früherziehung", Description: "Rhythmus, Raumgefühl und erste Tanzschritte für Kinder von vier bis sechs Jahren."},
			{Name: "Kinderballett Vorstufe", Description: "Die Vorbereitung auf das Kinderballett mit Haltung, Fußarbeit und viel Freude an der Bewegung."},
			{Name: "Kinderballett", Description: "Klassisches Ballett mit Übungen an der Ballettstange und im Raum, Stufe für Stufe aufgebaut."},
			{Name: "Ballett Jugend", Description: "Ballettunterricht für Jugendliche mit Stangenarbeit, Mitte und Sprüngen."},
			{Name: "Ballett Jugend & Spitze", Description: "Ballett für Jugendliche mit einem Teil Spitzenarbeit am Ende der Stunde."},
			{Name: "Spitze", Description: "Kurze Spitzenstunde zur Stärkung der Füße und für die Technik auf Spitze."},
			{Name: "Ballett Erwachsene", Description: "Klassisches Ballett für Erwachsene mit Klavierbegleitung."},
			{Name: "Ballett 45+", Description: "Ballett in ruhigerem Tempo für Erwachsene ab 45 mit Freude an sauberer Technik."},
			{Name: "Tanz für Einsteiger", Description: "Grundschritte aus verschiedenen Stilen für alle, die neu mit dem Tanzen beginnen."},
			{Name: "Tanzen 50+", Description: "Schwungvolle Choreografien für Junggebliebene ab 50 ohne Leistungsdruck."},
			{Name: "Contemporary", Description: "Bodenarbeit, Release-Technik und Improvisation zu zeitgenössischer Musik."},
			{Name: "Contemporary Kids", Description: "Zeitgenössischer Tanz für Kinder mit viel Raum für eigene Ideen."},
			{Name: "Hip-Hop & Contemporary Kids", Description: "Grooves aus dem Hip-Hop und fließende Bewegungen aus dem Contemporary für Kinder."},
			{Name: "Hip-Hop & Contemporary Teens", Description: "Hip-Hop und Contemporary für Jugendliche mit eigenen kleinen Choreografien."},
			{Name: "Modern Jazz", Description: "Isolationen, Drehungen und Sprünge zu Jazz- und Popmusik."},
			{Name: "Jazz Dance", Description: "Energiegeladene Jazztanz-Kombinationen für alle Level."},
			{Name: "Stepptanz", Description: "Rhythmus zum Hören mit Steppschuhen, vom ersten Shuffle an."},
			{Name: "Irish Dance", Description: "Schnelle Fußarbeit und Sprünge zu irischer Musik."},
			{Name: "Stretch & Strength", Description: "Dehnen und Kräftigen für Tänzerinnen, Tänzer und alle anderen."},
			{Name: "Qigong", Description: "Ruhige, fließende Übungen für Atem, Haltung und Gelassenheit."},
			{Name: "Taiji", Description: "Langsame Formen aus dem Taijiquan für Gleichgewicht und Konzentration."},
			{Name: "Morning Workout", Description: "Eine halbe Stunde Bewegung am Morgen, die wach macht."},
			{Name: "Floor Barre", Description: "Ballettübungen am Boden, die Kraft und Ausrichtung ohne Belastung der Gelenke schulen."},
			{Name: "Pas de deux", Description: "Partnerarbeit aus dem klassischen Ballett mit Hebungen und Promenaden."},
			{Name: "Yoga für Tänzerinnen und Tänzer", Description: "Yoga zur Regeneration und Beweglichkeit, abgestimmt auf den Tanzalltag."},
		},
		classes: werkstattClasses(),
		// The workshops take Sunday, the day without classes (workshopsOn),
		// at a room and time of their own each, so no two meet whichever
		// Sunday they fall on. The first lies within the first week after
		// the anchor and is usually released already; the later ones are
		// published early. One only signed-in members see, one only staff.
		workshops: []workshopSpec{
			{3, at(10, 0), 120, "Oben", "Floor Barre", "Valentina Rossbach", "", 18, ""},
			{17, at(13, 30), 120, "Unten", "Modern Jazz", "Nadine Brenner", "", 18, ""},
			{24, at(13, 30), 120, "Oben", "Pas de deux", "Johanna Sternberg", "Konrad Flügel", 16, ""},
			{5, at(10, 0), 120, "Trainingsraum", "Yoga für Tänzerinnen und Tänzer", "Gudrun Seidel", "", 8, domain.VisibilityMembers},
			{6, at(10, 0), 120, "Unten", "Stepptanz", "Kilian Hartwig", "", 15, domain.VisibilityHidden},
		},
		// The full workshop lies in the second week after the anchor. Its
		// members booked it two days before the anchor and joined its waiting
		// list the evening before; nobody of a demo login's family is on it.
		fullWorkshop: fullWorkshopSpec{
			workshop: workshopSpec{10, at(13, 30), 90, "Trainingsraum", "Irish Dance", "Kilian Hartwig", "", 4, ""},
			booked:   []string{"Ingrid Kranich", "Florian Lerchenfeld", "Bernd Nesselbach", "Ronja Pappelstein"},
			waiting:  []waitingSpec{{"Vanessa Rabenau", true}, {"Wiebke Weißdorn", false}},
		},
		// The participants Johanna added to her Pas de deux workshop: a guest
		// with an email, a walk-in guest without one, and a trial with a
		// note for her.
		staffBookings: staffBookingsSpec{
			instructor: "Johanna Sternberg",
			participants: []participantSpec{
				{guest: "Nele Gastberg", email: "nele.gastberg@mail.example"},
				{guest: "Jana Laufmann"},
				{person: "Ursula Tauberich", trial: true, note: "Seit Jahren die erste Ballettstunde; bitte auf das linke Knie achten."},
			},
		},
		// The rental lies on a Wednesday evening in Unten, after its last
		// class: staff see it in its room, nobody else.
		rental:   rentalSpec{4, at(20, 0), 120, "Unten", "Theatergruppe Kupferblatt"},
		rentalOn: on(time.Wednesday),
		// The pass catalog: a single lesson, a card of ten and a monthly
		// pass, invented gross prices in whole euros with 19 % VAT.
		passTypes: []domain.PassType{
			{Name: einzelstunde, Kind: domain.PassVisits, Visits: 1, ValidityDays: 90, Price: 1600, VATRate: 1900, Active: true},
			{Name: "10er-Karte", Kind: domain.PassVisits, Visits: 10, ValidityDays: 180, Price: 14000, VATRate: 1900, Active: true},
			{Name: "Monatskarte", Kind: domain.PassTime, ValidityDays: 30, Price: 7900, VATRate: 1900, Active: true},
		},
		// The passes of the member login's family (Katrin with her own card
		// and her daughter's, which she paid for), a monthly pass, a pass
		// given away and one sold by mistake and voided. Each drop-in is in
		// a class whose weekday and start no other class shares, of which
		// the person is no member.
		passes: []passSpec{
			{
				person: "Katrin Ahrenfeld", passType: "10er-Karte", soldDays: -14, method: domain.PaymentCard,
				dropIns: []classSlot{{time.Monday, at(18, 0)}, {time.Saturday, at(13, 30)}},
			},
			{
				person: "Marie Ahrenfeld", passType: "10er-Karte", soldDays: -10, payer: "Katrin Ahrenfeld",
				method: domain.PaymentTransfer, dropIns: []classSlot{{time.Friday, at(16, 20)}},
			},
			{
				person: "Florian Lerchenfeld", passType: "Monatskarte", soldDays: -7, method: domain.PaymentCash,
				dropIns: []classSlot{{time.Friday, at(19, 15)}},
			},
			{
				person: "Ingrid Kranich", passType: einzelstunde, soldDays: -3, assigned: true,
				note: "Kulanz: Ihr Workshop ist ausgefallen.",
			},
			{
				person: "Vanessa Rabenau", passType: einzelstunde, soldDays: -20, voided: true, method: domain.PaymentCash,
				note: "An die falsche Person verkauft.",
			},
		},
		// Two announcements of the office: one for everyone, and one for the
		// members of the Tanzzwerge, which the demo member sees through her
		// son.
		announcements: []announcementSpec{
			{
				title: "Willkommen im neuen Buchungssystem",
				body: "Ab sofort buchen und stornieren Sie Ihre Stunden hier, für sich und Ihre Kinder.\n" +
					"Fragen? Sprechen Sie uns gern im Büro an.",
				days: -3,
			},
			{
				title: "Kostümanprobe für die Winteraufführung",
				body: "Bitte kommen Sie am Samstagvormittag ins Büro zur Kostümanprobe.\n" +
					"Sie dauert etwa zwanzig Minuten.",
				courseType: "Tanzzwerge", days: -1, expiresDays: 14,
			},
		},
		// Two pairs of persons the office's duplicate detection finds (ADR
		// 0015): Lea twice with the same email, ignoring case (a strong
		// match), and Timo twice with the same name, once without a birth
		// date (a weak one). None has an account, so either pair can be
		// merged.
		duplicates: []domain.Person{
			{
				GivenName: "Lea", FamilyName: "Wacholder", Email: "lea.wacholder@post.example",
				Phone: "+49 000 0000401", MemberNo: "M-2042",
			},
			{
				GivenName: "Lea", FamilyName: "Wacholder", Email: "LEA.Wacholder@post.example",
				BirthDate: date(1994, time.August, 17), Notes: "Im Büro angemeldet.",
			},
			{GivenName: "Timo", FamilyName: "Holunder", BirthDate: date(2014, time.May, 12), MemberNo: "M-2077"},
			{GivenName: "Timo", FamilyName: "Holunder", Phone: "+49 000 0000402", Notes: "Spontan vorbeigekommen, Probestunde."},
		},
		// Heike is the office: she sells the passes, writes the
		// announcements and records the attendance of instructors without a
		// login.
		office: "Heike Buchwald",
		// The adult ballet classes' instructors are behind with the
		// attendance, so the instructor and office views show lessons to take
		// attendance of.
		attendanceSkipped: "Ballett Erwachsene",
		// The walk-in guests the instructors added to past lessons, in turn;
		// the trialists tried a class they are no member of (nobody of the
		// member login's family).
		guests:      []string{"Mats Vorbeck", "Leonie Gastmann", "Jule Kommher", "Ole Durchreich"},
		trialists:   []string{"Daniela Imhoff", "Yasemin Grünhagen", "Sabine Birkholz", "Bernd Nesselbach", "Birgit Eichhorst"},
		trialNotes:  []string{"Überlegt, nach den Ferien einzusteigen.", "", "Möchte einen zweiten Stil ausprobieren."},
		termName:    schoolYear("Schuljahr"),
		workshopsOn: on(time.Sunday),
	}
}

// werkstattSettings are DemoSettings with the werkstatt school's identity:
// its own name, imprint, receipt issuer and prefix, theme, accent and
// language.
func werkstattSettings() domain.Settings {
	s := DemoSettings()

	// A fictional name; the accent passes the 4.5:1 contrast check, and its
	// dark variant is derived from it (domain.DarkAccent).
	s.SchoolName = "Tanzhaus am Lindenhof"
	s.Theme = domain.ThemeWerkstatt
	s.AccentColor = "#9c215b"
	s.DefaultLocale = "de"
	s.ReceiptPrefix = "TL"
	// A fictional imprint and issuer, like the programme school's: the
	// street, the town, the tax number and the bank exist nowhere.
	s.LegalNotice = "Tanzhaus am Lindenhof · Lindenhofweg 12 · 10999 Beispielstadt\n" +
		"Eine erfundene Schule: Alle Namen, Adressen und Nummern in dieser Demo sind ausgedacht."
	s.Receipts.Issuer = domain.ReceiptIssuer{
		Name: "Tanzhaus am Lindenhof", Address: "Lindenhofweg 12\n10999 Beispielstadt",
		TaxID: "99/999/99999", Email: "buero@tanzhaus-lindenhof.example", Phone: "+49 000 0000000",
	}
	s.Receipts.Footer = "Bankverbindung: Beispielbank · IBAN XX00 0000 0000 0000 0000 00"

	return s
}

// span is a class for the levels from one to another of the scale.
func span(from, to string) audienceSpec { return audienceSpec{from: from, to: to} }

// years is an age of whole years in months.
func years(n int) domain.AgeMonths { return domain.AgeMonths(n * 12) }

// werkstattClasses is the weekly plan of spec 2026-11-01, §3, row by row,
// Monday to Saturday (Sunday is the workshops' day). The level codes are
// Einsteiger E, Anfänger A, Mittelstufe M, Fortgeschritten F; "alle" is
// all levels; "St. n" a stage; ages are whole years. The members are 1 to 4
// per class, children in the children's classes, of the age a class states
// (TestWerkstattChildrenOfAge). No instructor or accompanist teaches two
// classes at once (TestDemoInstructorsNeverDoubleBooked).
func werkstattClasses() []classSpec {
	const (
		johanna   = "Johanna Sternberg"
		marlene   = "Marlene Quast"
		valentina = "Valentina Rossbach"
		lars      = "Lars Feldkamp"
		nadine    = "Nadine Brenner"
		kilian    = "Kilian Hartwig"
		gudrun    = "Gudrun Seidel"
		konrad    = "Konrad Flügel"
		elise     = "Elise Hammerschlag"

		einsteiger = "Einsteiger"
		anfaenger  = "Anfänger"
		mittel     = "Mittelstufe"
		fortgeschr = "Fortgeschritten"
	)

	alle := allLevels
	stage := func(n int) audienceSpec { return audienceSpec{stage: n} }
	stageAges := func(n, from, to int) audienceSpec {
		return audienceSpec{stage: n, ageFrom: years(from), ageTo: years(to)}
	}
	fromAge := func(n int) audienceSpec { return audienceSpec{ageFrom: years(n)} }
	withAges := func(a audienceSpec, from, to int) audienceSpec {
		a.ageFrom, a.ageTo = years(from), years(to)

		return a
	}

	return []classSpec{
		// Montag
		{time.Monday, at(10, 15), 60, "Online", "Qigong", gudrun, "", alle, 40, []string{"Torsten Quendel", "Wiebke Weißdorn"}},
		{time.Monday, at(14, 55), 50, "Oben", "Tanzzwerge", marlene, "", fromAge(3), 12, []string{"Paul Ahrenfeld", "Anton Imhoff"}},
		{time.Monday, at(15, 45), 50, "Oben", "Tänzerische Früherziehung", marlene, "", stageAges(2, 4, 6), 14, []string{"Emma Birkholz", "Frieda Eichhorst"}},
		{time.Monday, at(15, 45), 60, "Unten", "Ballett Jugend", valentina, elise, stage(2), 18, []string{"Moritz Eichhorst", "Henri Grünhagen"}},
		{time.Monday, at(16, 45), 75, "Oben", "Ballett Jugend & Spitze", valentina, elise, stage(1), 18, []string{"Hannah Birkholz", "Greta Eichhorst"}},
		{time.Monday, at(16, 45), 60, "Unten", "Contemporary Kids", lars, "", withAges(level(anfaenger), 8, 12), 16, []string{"Lina Dürrbach", "Smilla Grünhagen"}},
		{time.Monday, at(17, 45), 60, "Online", "Taiji", gudrun, "", alle, 40, []string{"Daniela Imhoff", "Miriam Mohnhaupt"}},
		{time.Monday, at(17, 45), 90, "Unten", "Irish Dance", kilian, "", level(fortgeschr), 18, []string{"Matthias Fährmann", "Ronja Pappelstein"}},
		{time.Monday, at(18, 0), 70, "Oben", "Contemporary", lars, "", span(anfaenger, mittel), 18, []string{"Florian Lerchenfeld", "Ulrike Fährmann", "Jürgen Clausing"}},
		{time.Monday, at(19, 15), 60, "Oben", "Stretch & Strength", gudrun, "", alle, 16, []string{"Anja Dürrbach", "Sabine Birkholz", "Vanessa Rabenau"}},
		{time.Monday, at(19, 15), 60, "Unten", "Stepptanz", kilian, "", span(einsteiger, anfaenger), 16, []string{"Thomas Hollerbach", "Mehmet Dürrbach"}},
		{time.Monday, at(20, 15), 90, "Oben", "Ballett Erwachsene", johanna, konrad, span(mittel, fortgeschr), 18, []string{"Katrin Ahrenfeld", "Ronja Pappelstein"}},
		{time.Monday, at(20, 15), 90, "Unten", "Ballett 45+", valentina, elise, level(mittel), 16, []string{"Birgit Eichhorst", "Ingrid Kranich"}},

		// Dienstag
		{time.Tuesday, at(8, 45), 30, "Online", "Morning Workout", gudrun, "", alle, 40, []string{"Torsten Quendel", "Miriam Mohnhaupt"}},
		{time.Tuesday, at(10, 30), 75, "Unten", "Contemporary", lars, "", span(einsteiger, anfaenger), 18, []string{"Yasemin Grünhagen", "Wiebke Weißdorn"}},
		{time.Tuesday, at(12, 0), 75, "Unten", "Ballett Erwachsene", johanna, konrad, span(anfaenger, mittel), 18, []string{"Ingrid Kranich", "Vanessa Rabenau"}},
		{time.Tuesday, at(15, 0), 60, "Oben", "Kinderballett Vorstufe", marlene, "", stage(1), 14, []string{"Marie Ahrenfeld", "Smilla Grünhagen"}},
		{time.Tuesday, at(15, 0), 60, "Unten", "Kinderballett Vorstufe", valentina, "", stage(3), 14, []string{"Lina Dürrbach"}},
		{time.Tuesday, at(16, 0), 60, "Oben", "Ballett Jugend", johanna, "", stage(1), 18, []string{"Luca Clausing", "Moritz Eichhorst"}},
		{time.Tuesday, at(16, 0), 60, "Unten", "Ballett Jugend", valentina, elise, stage(3), 18, []string{"Hannah Birkholz", "Henri Grünhagen"}},
		{time.Tuesday, at(17, 0), 30, "Trainingsraum", "Spitze", valentina, "", withAges(level(einsteiger), 12, 0), 8, []string{"Greta Eichhorst", "Charlotte Hollerbach"}},
		{time.Tuesday, at(17, 30), 75, "Oben", "Tanz für Einsteiger", nadine, "", level(einsteiger), 18, []string{"Stefan Ahrenfeld", "Mehmet Dürrbach"}},
		{time.Tuesday, at(17, 30), 60, "Unten", "Stepptanz", kilian, "", span(einsteiger, anfaenger), 16, []string{"Thomas Hollerbach", "Jürgen Clausing"}},
		{time.Tuesday, at(18, 45), 75, "Oben", "Tanzen 50+", nadine, "", alle, 18, []string{"Birgit Eichhorst", "Bernd Nesselbach"}},
		{time.Tuesday, at(18, 45), 75, "Unten", "Ballett Jugend & Spitze", valentina, elise, stage(3), 18, []string{"Charlotte Hollerbach", "Jannik Fährmann"}},
		{time.Tuesday, at(20, 0), 90, "Oben", "Contemporary", lars, "", level(fortgeschr), 18, []string{"Florian Lerchenfeld", "Ronja Pappelstein"}},
		{time.Tuesday, at(20, 0), 90, "Unten", "Tanz für Einsteiger", nadine, "", level(anfaenger), 18, []string{"Anja Dürrbach", "Daniela Imhoff"}},

		// Mittwoch
		{time.Wednesday, at(9, 45), 60, "Online", "Qigong", gudrun, "", alle, 40, []string{"Torsten Quendel", "Bernd Nesselbach"}},
		{time.Wednesday, at(11, 0), 75, "Oben", "Qigong", gudrun, "", alle, 16, []string{"Wiebke Weißdorn", "Yasemin Grünhagen"}},
		{time.Wednesday, at(11, 0), 75, "Unten", "Ballett Erwachsene", johanna, konrad, level(mittel), 18, []string{"Ingrid Kranich", "Miriam Mohnhaupt"}},
		{time.Wednesday, at(14, 25), 60, "Oben", "Kinderballett", marlene, "", stage(1), 14, []string{"Smilla Grünhagen", "Lina Dürrbach"}},
		{time.Wednesday, at(14, 25), 50, "Unten", "Tänzerische Früherziehung", johanna, "", stageAges(1, 4, 6), 14, []string{"Emma Birkholz", "Anton Imhoff"}},
		{time.Wednesday, at(15, 15), 60, "Unten", "Kinderballett Vorstufe", johanna, "", stage(2), 14, []string{"Marie Ahrenfeld"}},
		{time.Wednesday, at(15, 25), 60, "Oben", "Ballett Jugend", valentina, elise, stage(2), 18, []string{"Moritz Eichhorst", "Henri Grünhagen"}},
		{time.Wednesday, at(16, 15), 60, "Unten", "Hip-Hop & Contemporary Kids", kilian, "", withAges(alle, 8, 12), 16, []string{"Lina Dürrbach", "Marie Ahrenfeld"}},
		{time.Wednesday, at(16, 25), 30, "Trainingsraum", "Spitze", valentina, "", withAges(level(anfaenger), 12, 0), 8, []string{"Hannah Birkholz", "Greta Eichhorst"}},
		{time.Wednesday, at(17, 0), 75, "Oben", "Ballett Erwachsene", johanna, konrad, span(anfaenger, mittel), 18, []string{"Katrin Ahrenfeld", "Vanessa Rabenau"}},
		{time.Wednesday, at(17, 25), 75, "Unten", "Ballett Jugend & Spitze", valentina, elise, stage(2), 18, []string{"Greta Eichhorst", "Charlotte Hollerbach"}},
		{time.Wednesday, at(18, 20), 30, "Trainingsraum", "Spitze", johanna, "", withAges(span(einsteiger, anfaenger), 12, 0), 8, []string{"Hannah Birkholz"}},
		{time.Wednesday, at(18, 45), 60, "Unten", "Modern Jazz", nadine, "", alle, 18, []string{"Ulrike Fährmann", "Matthias Fährmann", "Stefan Ahrenfeld"}},
		{time.Wednesday, at(18, 55), 60, "Oben", "Hip-Hop & Contemporary Teens", lars, "", withAges(alle, 12, 17), 16, []string{"Luca Clausing", "Jannik Fährmann", "Henri Grünhagen"}},
		{time.Wednesday, at(20, 0), 75, "Oben", "Ballett Erwachsene", johanna, konrad, span(anfaenger, mittel), 18, []string{"Ronja Pappelstein", "Anja Dürrbach"}},

		// Donnerstag
		{time.Thursday, at(14, 40), 50, "Unten", "Tanzzwerge", marlene, "", fromAge(3), 12, []string{"Paul Ahrenfeld"}},
		{time.Thursday, at(15, 30), 50, "Oben", "Tänzerische Früherziehung", marlene, "", stage(3), 14, []string{"Frieda Eichhorst", "Emma Birkholz"}},
		{time.Thursday, at(15, 30), 60, "Unten", "Kinderballett Vorstufe", valentina, "", stage(1), 14, []string{"Smilla Grünhagen"}},
		{time.Thursday, at(16, 30), 60, "Unten", "Kinderballett", marlene, "", stage(2), 14, []string{"Marie Ahrenfeld", "Lina Dürrbach"}},
		{time.Thursday, at(16, 30), 75, "Oben", "Ballett Jugend", valentina, elise, stage(4), 18, []string{"Greta Eichhorst", "Hannah Birkholz", "Jannik Fährmann"}},
		{time.Thursday, at(17, 30), 70, "Unten", "Taiji", gudrun, "", span(mittel, fortgeschr), 16, []string{"Daniela Imhoff", "Torsten Quendel"}},
		{time.Thursday, at(17, 45), 85, "Oben", "Ballett Erwachsene", johanna, konrad, span(anfaenger, mittel), 18, []string{"Katrin Ahrenfeld", "Ingrid Kranich", "Birgit Eichhorst"}},
		{time.Thursday, at(18, 45), 75, "Unten", "Contemporary", lars, "", span(einsteiger, anfaenger), 18, []string{"Yasemin Grünhagen", "Mehmet Dürrbach", "Sabine Birkholz"}},
		{time.Thursday, at(19, 15), 75, "Oben", "Irish Dance", kilian, "", level(mittel), 18, []string{"Matthias Fährmann", "Ulrike Fährmann"}},
		{time.Thursday, at(20, 0), 90, "Unten", "Contemporary", lars, "", level(mittel), 18, []string{"Florian Lerchenfeld", "Jürgen Clausing"}},

		// Freitag
		{time.Friday, at(10, 15), 75, "Oben", "Tanzen 50+", nadine, "", alle, 18, []string{"Bernd Nesselbach", "Wiebke Weißdorn"}},
		{time.Friday, at(12, 30), 75, "Oben", "Tanz für Einsteiger", nadine, "", span(einsteiger, anfaenger), 18, []string{"Miriam Mohnhaupt", "Mehmet Dürrbach"}},
		{time.Friday, at(15, 25), 50, "Oben", "Tänzerische Früherziehung", marlene, "", stageAges(1, 4, 6), 14, []string{"Anton Imhoff", "Frieda Eichhorst"}},
		{time.Friday, at(15, 50), 50, "Unten", "Tänzerische Früherziehung", johanna, "", stageAges(2, 4, 6), 14, []string{"Emma Birkholz"}},
		{time.Friday, at(16, 20), 60, "Oben", "Kinderballett", marlene, "", stage(1), 14, []string{"Smilla Grünhagen"}},
		{time.Friday, at(16, 45), 60, "Unten", "Kinderballett", johanna, "", stage(3), 14, []string{"Lina Dürrbach"}},
		{time.Friday, at(17, 25), 75, "Oben", "Tanz für Einsteiger", nadine, "", level(einsteiger), 18, []string{"Stefan Ahrenfeld", "Thomas Hollerbach"}},
		{time.Friday, at(17, 45), 90, "Unten", "Ballett Jugend & Spitze", valentina, elise, stage(1), 18, []string{"Charlotte Hollerbach", "Jannik Fährmann", "Greta Eichhorst"}},
		{time.Friday, at(18, 45), 60, "Online", "Taiji", gudrun, "", alle, 40, []string{"Daniela Imhoff", "Torsten Quendel"}},
		{time.Friday, at(18, 45), 75, "Oben", "Ballett Erwachsene", johanna, konrad, span(anfaenger, mittel), 18, []string{"Vanessa Rabenau", "Anja Dürrbach"}},
		{time.Friday, at(19, 15), 75, "Unten", "Jazz Dance", nadine, "", alle, 18, []string{"Ulrike Fährmann", "Jürgen Clausing"}},
		{time.Friday, at(20, 15), 90, "Oben", "Tanz für Einsteiger", kilian, "", level(anfaenger), 18, []string{"Sabine Birkholz", "Yasemin Grünhagen"}},

		// Samstag
		{time.Saturday, at(10, 0), 90, "Online", "Qigong", gudrun, "", span(mittel, fortgeschr), 40, []string{"Torsten Quendel", "Wiebke Weißdorn"}},
		{time.Saturday, at(10, 15), 90, "Oben", "Tanz für Einsteiger", nadine, "", level(anfaenger), 18, []string{"Stefan Ahrenfeld", "Katrin Ahrenfeld"}},
		{time.Saturday, at(11, 0), 90, "Unten", "Irish Dance", kilian, "", span(einsteiger, anfaenger), 18, []string{"Matthias Fährmann", "Thomas Hollerbach"}},
		{time.Saturday, at(12, 0), 75, "Oben", "Ballett Erwachsene", johanna, konrad, span(anfaenger, mittel), 18, []string{"Ingrid Kranich", "Miriam Mohnhaupt"}},
		{time.Saturday, at(13, 15), 75, "Unten", "Modern Jazz", nadine, "", alle, 18, []string{"Ulrike Fährmann", "Anja Dürrbach"}},
		{time.Saturday, at(13, 30), 75, "Oben", "Contemporary", lars, "", span(mittel, fortgeschr), 18, []string{"Florian Lerchenfeld", "Ronja Pappelstein"}},
	}
}
