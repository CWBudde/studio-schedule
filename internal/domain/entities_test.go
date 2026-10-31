package domain_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
)

func TestEnums(t *testing.T) {
	t.Parallel()

	t.Run("StaffKind", func(t *testing.T) { t.Parallel(); checkEnum(t, domain.StaffKinds()) })
	t.Run("Level", func(t *testing.T) { t.Parallel(); checkEnum(t, domain.Levels()) })
	t.Run("LessonStatus", func(t *testing.T) { t.Parallel(); checkEnum(t, domain.LessonStatuses()) })
	t.Run("LessonVisibility", func(t *testing.T) { t.Parallel(); checkEnum(t, domain.LessonVisibilities()) })
	t.Run("LessonKind", func(t *testing.T) { t.Parallel(); checkEnum(t, domain.LessonKinds()) })
	t.Run("BookingStatus", func(t *testing.T) { t.Parallel(); checkEnum(t, domain.BookingStatuses()) })
	t.Run("BookingSource", func(t *testing.T) { t.Parallel(); checkEnum(t, domain.BookingSources()) })
	t.Run("WaitlistStatus", func(t *testing.T) { t.Parallel(); checkEnum(t, domain.WaitlistStatuses()) })
	t.Run("ReceiptKind", func(t *testing.T) { t.Parallel(); checkEnum(t, domain.ReceiptKinds()) })
	t.Run("TaxMode", func(t *testing.T) { t.Parallel(); checkEnum(t, domain.TaxModes()) })
	t.Run("PaymentMethod", func(t *testing.T) { t.Parallel(); checkEnum(t, domain.PaymentMethods()) })
	t.Run("PassKind", func(t *testing.T) { t.Parallel(); checkEnum(t, domain.PassKinds()) })
	t.Run("PassStatus", func(t *testing.T) { t.Parallel(); checkEnum(t, domain.PassStatuses()) })
	t.Run("PassOrigin", func(t *testing.T) { t.Parallel(); checkEnum(t, domain.PassOrigins()) })
	t.Run("RedemptionMode", func(t *testing.T) { t.Parallel(); checkEnum(t, domain.RedemptionModes()) })
	t.Run("Audience", func(t *testing.T) { t.Parallel(); checkEnum(t, domain.Audiences()) })
	t.Run("FeedScope", func(t *testing.T) { t.Parallel(); checkEnum(t, domain.FeedScopes()) })
	t.Run("TokenPurpose", func(t *testing.T) { t.Parallel(); checkEnum(t, domain.TokenPurposes()) })
	t.Run("MailStatus", func(t *testing.T) { t.Parallel(); checkEnum(t, domain.MailStatuses()) })
	t.Run("AuditAction", func(t *testing.T) { t.Parallel(); checkEnum(t, domain.AuditActions()) })
	t.Run("AuditTargetKind", func(t *testing.T) { t.Parallel(); checkEnum(t, domain.AuditTargetKinds()) })
}

func TestPersonValidate(t *testing.T) {
	t.Parallel()

	checkValidate(t, func() domain.Person {
		return domain.Person{
			ID: newID[domain.PersonID](), GivenName: "Ada", FamilyName: "Example",
			Email: "ada@example.org", BirthDate: domain.Date{Year: 2015, Month: time.May, Day: 4}, MemberNo: "M-0001",
		}
	}, map[string]func(*domain.Person){
		"member no surrounds":  func(p *domain.Person) { p.MemberNo = "M-0001 " },
		"member no too long":   func(p *domain.Person) { p.MemberNo = strings.Repeat("9", domain.MaxMemberNoLength+1) },
		"merged into itself":   func(p *domain.Person) { p.MergedInto = p.ID },
		"merged with a number": func(p *domain.Person) { p.MergedInto = newID[domain.PersonID]() },
		"zero id":              func(p *domain.Person) { p.ID = domain.PersonID{} },
		"no given name":        func(p *domain.Person) { p.GivenName = " " },
		"no family name":       func(p *domain.Person) { p.FamilyName = "" },
		"bad email":            func(p *domain.Person) { p.Email = "not-an-email" },
		"named email":          func(p *domain.Person) { p.Email = "Ada <ada@example.org>" },
		"email surrounds":      func(p *domain.Person) { p.Email = " ada@example.org" },
		"bad birth date":       func(p *domain.Person) { p.BirthDate = domain.Date{Year: 2015, Month: 2, Day: 30} },
	})
}

func TestAnonymizedPersonValidate(t *testing.T) {
	t.Parallel()

	checkValidate(t, func() domain.Person {
		return domain.Person{
			ID: newID[domain.PersonID](), GivenName: "Ada", FamilyName: "Example", Email: "ada@example.org",
			Phone: "+49 000 0000000", BirthDate: domain.Date{Year: 2015, Month: time.May, Day: 4}, Notes: "Notes.",
			MemberNo: "M-0001",
		}.Anonymize(t0)
	}, map[string]func(*domain.Person){
		"a name":       func(p *domain.Person) { p.GivenName = "Ada" },
		"an email":     func(p *domain.Person) { p.Email = "ada@example.org" },
		"a phone":      func(p *domain.Person) { p.Phone = "+49 000 0000000" },
		"a birth date": func(p *domain.Person) { p.BirthDate = domain.Date{Year: 2015, Month: time.May, Day: 4} },
		"notes":        func(p *domain.Person) { p.Notes = "Notes." },
		"a member no":  func(p *domain.Person) { p.MemberNo = "M-0001" },
	})

	p := domain.Person{ID: newID[domain.PersonID](), GivenName: "Ada", FamilyName: "Example"}.Anonymize(t0)
	if !p.Anonymized() || p.GivenName != domain.AnonymizedGivenName || p.FamilyName != domain.AnonymizedFamilyName {
		t.Errorf("anonymized %+v", p)
	}
}

func TestDuplicateDismissalValidate(t *testing.T) {
	t.Parallel()

	checkValidate(t, func() domain.DuplicateDismissal {
		return domain.NewDuplicateDismissal(newID[domain.PersonID](), newID[domain.PersonID](), newID[domain.AccountID](), t0)
	}, map[string]func(*domain.DuplicateDismissal){
		"zero person": func(d *domain.DuplicateDismissal) { d.PersonA = domain.PersonID{} },
		"same person": func(d *domain.DuplicateDismissal) { d.PersonB = d.PersonA },
		"not ordered": func(d *domain.DuplicateDismissal) { d.PersonA, d.PersonB = d.PersonB, d.PersonA },
		"no account":  func(d *domain.DuplicateDismissal) { d.DismissedBy = domain.AccountID{} },
		"no time":     func(d *domain.DuplicateDismissal) { d.DismissedAt = time.Time{} },
	})

	a, b := newID[domain.PersonID](), newID[domain.PersonID]()
	if domain.NewDuplicateDismissal(b, a, newID[domain.AccountID](), t0).PersonA != a {
		t.Error("NewDuplicateDismissal does not order the pair")
	}
}

func TestGuardianValidate(t *testing.T) {
	t.Parallel()

	checkValidate(t, func() domain.Guardian {
		return domain.Guardian{GuardianID: newID[domain.PersonID](), DependentID: newID[domain.PersonID]()}
	}, map[string]func(*domain.Guardian){
		"zero guardian":  func(g *domain.Guardian) { g.GuardianID = domain.PersonID{} },
		"zero dependent": func(g *domain.Guardian) { g.DependentID = domain.PersonID{} },
		"self":           func(g *domain.Guardian) { g.DependentID = g.GuardianID },
	})
}

func TestAccountValidate(t *testing.T) {
	t.Parallel()

	checkValidate(t, func() domain.Account {
		return domain.Account{
			ID: newID[domain.AccountID](), PersonID: newID[domain.PersonID](),
			Email: "ada@example.org", Role: domain.RoleMember, VisibleDays: 14, Locale: "de",
		}
	}, map[string]func(*domain.Account){
		"zero id":       func(a *domain.Account) { a.ID = domain.AccountID{} },
		"zero person":   func(a *domain.Account) { a.PersonID = domain.PersonID{} },
		"no email":      func(a *domain.Account) { a.Email = "" },
		"bad role":      func(a *domain.Account) { a.Role = "root" },
		"guest role":    func(a *domain.Account) { a.Role = domain.RoleGuest },
		"negative days": func(a *domain.Account) { a.VisibleDays = -1 },
		"too many days": func(a *domain.Account) { a.VisibleDays = 367 },
		"blank locale":  func(a *domain.Account) { a.Locale = " " },
	})
}

func TestCatalogueValidate(t *testing.T) {
	t.Parallel()

	t.Run("Location", func(t *testing.T) {
		t.Parallel()
		checkValidate(t, func() domain.Location {
			return domain.Location{ID: newID[domain.LocationID](), Name: "Main Studio"}
		}, map[string]func(*domain.Location){
			"zero id": func(l *domain.Location) { l.ID = domain.LocationID{} },
			"no name": func(l *domain.Location) { l.Name = "" },
		})
	})

	t.Run("Room", func(t *testing.T) {
		t.Parallel()
		checkValidate(t, func() domain.Room {
			return domain.Room{ID: newID[domain.RoomID](), LocationID: newID[domain.LocationID](), Name: "Studio A", Capacity: 20}
		}, map[string]func(*domain.Room){
			"zero id":       func(r *domain.Room) { r.ID = domain.RoomID{} },
			"zero location": func(r *domain.Room) { r.LocationID = domain.LocationID{} },
			"no name":       func(r *domain.Room) { r.Name = "" },
			"zero capacity": func(r *domain.Room) { r.Capacity = 0 },
		})
	})

	t.Run("Staff", func(t *testing.T) {
		t.Parallel()
		checkValidate(t, func() domain.Staff {
			return domain.Staff{ID: newID[domain.StaffID](), DisplayName: "Ms. Example", Kind: domain.StaffInstructor, Active: true}
		}, map[string]func(*domain.Staff){
			"zero id":  func(s *domain.Staff) { s.ID = domain.StaffID{} },
			"no name":  func(s *domain.Staff) { s.DisplayName = "" },
			"bad kind": func(s *domain.Staff) { s.Kind = "dancer" },
		})
	})

	t.Run("CourseType", func(t *testing.T) {
		t.Parallel()
		checkValidate(t, func() domain.CourseType {
			return domain.CourseType{ID: newID[domain.CourseTypeID](), Name: "Classical Ballet"}
		}, map[string]func(*domain.CourseType){
			"zero id": func(c *domain.CourseType) { c.ID = domain.CourseTypeID{} },
			"no name": func(c *domain.CourseType) { c.Name = "" },
		})
	})
}

func TestTimetableValidate(t *testing.T) {
	t.Parallel()

	t.Run("Term", func(t *testing.T) {
		t.Parallel()
		checkValidate(t, func() domain.Term {
			return domain.Term{
				ID: newID[domain.TermID](), Name: "Autumn",
				StartsOn: domain.Date{Year: 2026, Month: time.September, Day: 1},
				EndsOn:   domain.Date{Year: 2026, Month: time.September, Day: 1},
			}
		}, map[string]func(*domain.Term){
			"zero id":          func(x *domain.Term) { x.ID = domain.TermID{} },
			"no name":          func(x *domain.Term) { x.Name = "" },
			"no start":         func(x *domain.Term) { x.StartsOn = domain.Date{} },
			"no end":           func(x *domain.Term) { x.EndsOn = domain.Date{} },
			"end before start": func(x *domain.Term) { x.EndsOn = domain.Date{Year: 2026, Month: time.August, Day: 31} },
		})
	})

	t.Run("Class", func(t *testing.T) {
		t.Parallel()
		checkValidate(t, func() domain.Class {
			return domain.Class{
				ID: newID[domain.ClassID](), TermID: newID[domain.TermID](), CourseTypeID: newID[domain.CourseTypeID](),
				RoomID: newID[domain.RoomID](), InstructorID: newID[domain.StaffID](), AccompanistID: newID[domain.StaffID](),
				Level: domain.LevelBeginner, Weekday: time.Monday, StartTime: domain.TimeOfDay{Hour: 17, Minute: 30},
				Duration: 90 * time.Minute, Capacity: 16,
			}
		}, map[string]func(*domain.Class){
			"zero id":             func(c *domain.Class) { c.ID = domain.ClassID{} },
			"zero term":           func(c *domain.Class) { c.TermID = domain.TermID{} },
			"zero course type":    func(c *domain.Class) { c.CourseTypeID = domain.CourseTypeID{} },
			"zero room":           func(c *domain.Class) { c.RoomID = domain.RoomID{} },
			"zero instructor":     func(c *domain.Class) { c.InstructorID = domain.StaffID{} },
			"accompanist teaches": func(c *domain.Class) { c.AccompanistID = c.InstructorID },
			"bad level":           func(c *domain.Class) { c.Level = "expert" },
			"bad weekday":         func(c *domain.Class) { c.Weekday = 7 },
			"bad start":           func(c *domain.Class) { c.StartTime = domain.TimeOfDay{Hour: 25} },
			"zero duration":       func(c *domain.Class) { c.Duration = 0 },
			"sub-minute duration": func(c *domain.Class) { c.Duration = 90*time.Minute + time.Second },
			"crosses midnight":    func(c *domain.Class) { c.StartTime = domain.TimeOfDay{Hour: 23}; c.Duration = 2 * time.Hour },
			"zero capacity":       func(c *domain.Class) { c.Capacity = 0 },
			"overflowing length":  func(c *domain.Class) { c.Duration = time.Duration(1<<63-1) / time.Minute * time.Minute },
		})
	})

	t.Run("ClassMember", func(t *testing.T) {
		t.Parallel()
		checkValidate(t, func() domain.ClassMember {
			return domain.ClassMember{ClassID: newID[domain.ClassID](), PersonID: newID[domain.PersonID]()}
		}, map[string]func(*domain.ClassMember){
			"zero class":  func(m *domain.ClassMember) { m.ClassID = domain.ClassID{} },
			"zero person": func(m *domain.ClassMember) { m.PersonID = domain.PersonID{} },
			"bad since":   func(m *domain.ClassMember) { m.Since = domain.Date{Year: 2026, Month: 2, Day: 31} },
			"bad pause":   func(m *domain.ClassMember) { m.PausedUntil = domain.Date{Year: 2026, Month: 2, Day: 31} },
		})
	})
}

func TestClassMemberActiveOn(t *testing.T) {
	t.Parallel()

	day := domain.Date{Year: 2026, Month: time.November, Day: 3}
	always := domain.ClassMember{}
	since := domain.ClassMember{Since: day}
	paused := domain.ClassMember{Since: day.AddDays(-30), PausedUntil: day}

	for _, c := range []struct {
		m    domain.ClassMember
		d    domain.Date
		want bool
	}{
		{always, day.AddDays(-400), true},
		{since, day.AddDays(-1), false},
		{since, day, true},
		{since, day.AddDays(1), true},
		{paused, day.AddDays(-31), false},
		{paused, day.AddDays(-30), false},
		{paused, day, false},
		{paused, day.AddDays(1), true},
	} {
		if got := c.m.ActiveOn(c.d); got != c.want {
			t.Errorf("%+v.ActiveOn(%s) = %v, want %v", c.m, c.d, got, c.want)
		}
	}
}

func TestLessonValidate(t *testing.T) {
	t.Parallel()

	checkValidate(t, lessonFixture, map[string]func(*domain.Lesson){
		"zero id":             func(l *domain.Lesson) { l.ID = domain.LessonID{} },
		"zero course type":    func(l *domain.Lesson) { l.CourseTypeID = domain.CourseTypeID{} },
		"zero room":           func(l *domain.Lesson) { l.RoomID = domain.RoomID{} },
		"zero instructor":     func(l *domain.Lesson) { l.InstructorID = domain.StaffID{} },
		"accompanist teaches": func(l *domain.Lesson) { l.AccompanistID = l.InstructorID },
		"no start":            func(l *domain.Lesson) { l.StartsAt = time.Time{} },
		"end before start":    func(l *domain.Lesson) { l.EndsAt = l.StartsAt },
		"bad status":          func(l *domain.Lesson) { l.Status = "done" },
		"bad visibility":      func(l *domain.Lesson) { l.Visibility = "secret" },
		"bad kind":            func(l *domain.Lesson) { l.Kind = "party" },
		"zero capacity":       func(l *domain.Lesson) { l.Capacity = 0 },
		"slot without class":  func(l *domain.Lesson) { l.SlotDate = domain.Date{Year: 2026, Month: time.September, Day: 7} },
		"class without slot":  func(l *domain.Lesson) { l.ClassID = newID[domain.ClassID]() },
		"bad slot": func(l *domain.Lesson) {
			l.ClassID, l.SlotDate = newID[domain.ClassID](), domain.Date{Year: 2026, Month: time.February, Day: 30}
		},
		"detached one-off":           func(l *domain.Lesson) { l.Detached = true },
		"note on a scheduled lesson": func(l *domain.Lesson) { l.CancelNote = "Rain" },
		"canceled time on a scheduled lesson": func(l *domain.Lesson) {
			l.CanceledAt = t0.Add(-time.Hour)
		},
		"canceler on a scheduled lesson": func(l *domain.Lesson) { l.CanceledBy = newID[domain.AccountID]() },
		"blank note": func(l *domain.Lesson) {
			l.Status, l.CancelNote = domain.LessonCanceled, "  "
		},
		"long note": func(l *domain.Lesson) {
			l.Status, l.CancelNote = domain.LessonCanceled, strings.Repeat("ä", domain.MaxCancelNoteLength+1)
		},
	})

	// A detached lesson of a class, and a canceled lesson with or without
	// details, are valid.
	for name, l := range map[string]domain.Lesson{
		"detached": func() domain.Lesson {
			l := lessonFixture()
			l.ClassID, l.SlotDate, l.Detached = newID[domain.ClassID](), domain.Date{Year: 2026, Month: time.September, Day: 7}, true

			return l
		}(),
		"canceled": func() domain.Lesson { l := lessonFixture(); l.Status = domain.LessonCanceled; return l }(),
		"canceled with details": func() domain.Lesson {
			l := lessonFixture()
			l.Status, l.CancelNote = domain.LessonCanceled, strings.Repeat("ä", domain.MaxCancelNoteLength)
			l.CanceledAt, l.CanceledBy = t0.Add(-time.Hour), newID[domain.AccountID]()

			return l
		}(),
	} {
		if err := l.Validate(); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestLessonValidateRental(t *testing.T) {
	t.Parallel()

	checkValidate(t, rentalFixture, map[string]func(*domain.Lesson){
		"instructor":  func(l *domain.Lesson) { l.InstructorID = newID[domain.StaffID]() },
		"accompanist": func(l *domain.Lesson) { l.AccompanistID = newID[domain.StaffID]() },
		"capacity":    func(l *domain.Lesson) { l.Capacity = 4 },
		"public":      func(l *domain.Lesson) { l.Visibility = domain.VisibilityPublic },
		"members":     func(l *domain.Lesson) { l.Visibility = domain.VisibilityMembers },
		"no renter":   func(l *domain.Lesson) { l.Renter = "" },
		"blank renter": func(l *domain.Lesson) {
			l.Renter = "  "
		},
		"long renter": func(l *domain.Lesson) {
			l.Renter = strings.Repeat("ä", domain.MaxRenterLength+1)
		},
		"of a class": func(l *domain.Lesson) {
			l.ClassID, l.SlotDate = newID[domain.ClassID](), domain.Date{Year: 2026, Month: time.September, Day: 7}
		},
	})

	if err := rentalFixture().Validate(); err != nil {
		t.Errorf("rental: %v", err)
	}

	long := rentalFixture()
	long.Renter = strings.Repeat("ä", domain.MaxRenterLength)

	if err := long.Validate(); err != nil {
		t.Errorf("longest renter: %v", err)
	}

	// Only a rental names a renter.
	regular := lessonFixture()
	regular.Renter = "Copperleaf Theater Group"

	if err := regular.Validate(); err == nil {
		t.Error("a regular lesson with a renter is valid")
	}

	if domain.LessonRental.Bookable() || !domain.LessonWorkshop.Bookable() {
		t.Error("only a room rental takes no bookings")
	}
}

// rentalFixture is a valid room rental: no instructor, no capacity, hidden,
// with a renter.
func rentalFixture() domain.Lesson {
	l := lessonFixture()
	l.Kind, l.InstructorID, l.Capacity = domain.LessonRental, domain.StaffID{}, 0
	l.Visibility, l.Renter = domain.VisibilityHidden, "Copperleaf Theater Group"

	return l
}

func lessonFixture() domain.Lesson {
	return domain.Lesson{
		ID: newID[domain.LessonID](), CourseTypeID: newID[domain.CourseTypeID](), RoomID: newID[domain.RoomID](),
		InstructorID: newID[domain.StaffID](), StartsAt: t0, EndsAt: t0.Add(time.Hour),
		Status: domain.LessonScheduled, Visibility: domain.VisibilityPublic, Kind: domain.LessonRegular, Capacity: 12,
	}
}

func TestBookingValidate(t *testing.T) {
	t.Parallel()

	member := func() domain.Booking {
		return domain.Booking{
			ID: newID[domain.BookingID](), LessonID: newID[domain.LessonID](), PersonID: newID[domain.PersonID](),
			Status: domain.BookingBooked, Source: domain.BookingSourceSelf, BookedAt: t0,
		}
	}

	t.Run("Member", func(t *testing.T) {
		t.Parallel()
		checkValidate(t, member, map[string]func(*domain.Booking){
			"zero id":               func(b *domain.Booking) { b.ID = domain.BookingID{} },
			"zero lesson":           func(b *domain.Booking) { b.LessonID = domain.LessonID{} },
			"person and guest":      func(b *domain.Booking) { b.GuestName = "Guest" },
			"neither":               func(b *domain.Booking) { b.PersonID = domain.PersonID{} },
			"bad status":            func(b *domain.Booking) { b.Status = "maybe" },
			"no source":             func(b *domain.Booking) { b.Source = "" },
			"bad source":            func(b *domain.Booking) { b.Source = "robot" },
			"guest email only":      func(b *domain.Booking) { b.PersonID, b.GuestEmail = domain.PersonID{}, "g@example.org" },
			"note without trial":    func(b *domain.Booking) { b.TrialNote = "First time" },
			"late not canceled":     func(b *domain.Booking) { b.Late = true },
			"released not canceled": func(b *domain.Booking) { b.Released = true },
			"no booked at":          func(b *domain.Booking) { b.BookedAt = time.Time{} },
			"canceled no time":      func(b *domain.Booking) { b.Status = domain.BookingCanceled },
			"time not canceled":     func(b *domain.Booking) { b.CanceledAt = t0.Add(time.Hour) },
			"canceled early": func(b *domain.Booking) {
				b.Status, b.CanceledAt = domain.BookingCanceled, t0.Add(-time.Hour)
			},
		})
	})

	t.Run("Guest", func(t *testing.T) {
		t.Parallel()
		checkValidate(t, func() domain.Booking {
			b := member()
			b.PersonID, b.GuestName, b.GuestEmail, b.Trial = domain.PersonID{}, "Guest Example", "guest@example.org", true
			b.Status, b.CanceledAt, b.Late = domain.BookingCanceled, t0.Add(time.Hour), true

			return b
		}, map[string]func(*domain.Booking){
			"no name":      func(b *domain.Booking) { b.GuestName = " " },
			"padded name":  func(b *domain.Booking) { b.GuestName = " Guest Example" },
			"bad email":    func(b *domain.Booking) { b.GuestEmail = "guest@" },
			"padded email": func(b *domain.Booking) { b.GuestEmail = "guest@example.org " },
		})
	})

	t.Run("StaffGuestWithoutEmail", func(t *testing.T) {
		t.Parallel()
		checkValidate(t, func() domain.Booking {
			b := member()
			b.PersonID, b.GuestName, b.Source = domain.PersonID{}, "Walk-in Example", domain.BookingSourceStaff
			b.Trial, b.TrialNote = true, "First ballet class; tell her about the barre."

			return b
		}, map[string]func(*domain.Booking){
			"note without trial": func(b *domain.Booking) { b.Trial = false },
			"padded note":        func(b *domain.Booking) { b.TrialNote = "note " },
			"long note":          func(b *domain.Booking) { b.TrialNote = strings.Repeat("ä", domain.MaxTrialNoteLength+1) },
		})
	})

	t.Run("Event", func(t *testing.T) {
		t.Parallel()
		checkValidate(t, func() domain.BookingEvent {
			return domain.BookingEvent{
				ID: newID[domain.BookingEventID](), BookingID: newID[domain.BookingID](),
				At: t0, Status: domain.BookingAttended, ActorAccountID: newID[domain.AccountID](), Override: true,
			}
		}, map[string]func(*domain.BookingEvent){
			"late not canceled": func(e *domain.BookingEvent) { e.Late = true },
			"zero id":           func(e *domain.BookingEvent) { e.ID = domain.BookingEventID{} },
			"zero booking":      func(e *domain.BookingEvent) { e.BookingID = domain.BookingID{} },
			"no time":           func(e *domain.BookingEvent) { e.At = time.Time{} },
			"bad status":        func(e *domain.BookingEvent) { e.Status = "" },
		})
	})
}

func TestWaitlistEntryValidate(t *testing.T) {
	t.Parallel()

	waiting := func() domain.WaitlistEntry {
		return domain.WaitlistEntry{
			ID: newID[domain.WaitlistEntryID](), LessonID: newID[domain.LessonID](), PersonID: newID[domain.PersonID](),
			Status: domain.WaitlistWaiting, JoinedAt: t0,
		}
	}

	t.Run("Waiting", func(t *testing.T) {
		t.Parallel()
		checkValidate(t, waiting, map[string]func(*domain.WaitlistEntry){
			"zero id":      func(w *domain.WaitlistEntry) { w.ID = domain.WaitlistEntryID{} },
			"zero lesson":  func(w *domain.WaitlistEntry) { w.LessonID = domain.LessonID{} },
			"zero person":  func(w *domain.WaitlistEntry) { w.PersonID = domain.PersonID{} },
			"bad status":   func(w *domain.WaitlistEntry) { w.Status = "queued" },
			"no joined at": func(w *domain.WaitlistEntry) { w.JoinedAt = time.Time{} },
			"has invite":   func(w *domain.WaitlistEntry) { w.InvitedAt = t0 },
			"has expiry":   func(w *domain.WaitlistEntry) { w.ExpiresAt = t0.Add(time.Hour) },
		})

		// Back on the list after an invite, it keeps the old link's hash
		// until a new invite replaces it.
		w := waiting()
		w.InviteTokenHash[0] = 1

		if err := w.Validate(); err != nil {
			t.Fatalf("waiting with the old link: %v", err)
		}
	})

	t.Run("Invited", func(t *testing.T) {
		t.Parallel()
		checkValidate(t, func() domain.WaitlistEntry {
			w := waiting()
			w.Status, w.InvitedAt, w.ExpiresAt = domain.WaitlistInvited, t0.Add(time.Minute), t0.Add(time.Hour)
			w.InviteTokenHash[31] = 0xff

			return w
		}, map[string]func(*domain.WaitlistEntry){
			"no token":          func(w *domain.WaitlistEntry) { w.InviteTokenHash = [32]byte{} },
			"no invited at":     func(w *domain.WaitlistEntry) { w.InvitedAt = time.Time{} },
			"invited pre-join":  func(w *domain.WaitlistEntry) { w.InvitedAt = t0.Add(-time.Minute) },
			"expires on invite": func(w *domain.WaitlistEntry) { w.ExpiresAt = w.InvitedAt },
		})
	})

	t.Run("Accepted", func(t *testing.T) {
		t.Parallel()
		checkValidate(t, func() domain.WaitlistEntry {
			w := waiting()
			w.Status, w.InvitedAt, w.ExpiresAt = domain.WaitlistAccepted, t0.Add(time.Minute), t0.Add(time.Hour)
			w.InviteTokenHash[0] = 1

			return w
		}, map[string]func(*domain.WaitlistEntry){
			"no invite": func(w *domain.WaitlistEntry) {
				w.InviteTokenHash, w.InvitedAt, w.ExpiresAt = [32]byte{}, time.Time{}, time.Time{}
			},
			"no token": func(w *domain.WaitlistEntry) { w.InviteTokenHash = [32]byte{} },
		})
	})

	// An entry ends with or without the invite it had.
	for _, status := range []domain.WaitlistStatus{domain.WaitlistExpired, domain.WaitlistWithdrawn} {
		t.Run(string(status), func(t *testing.T) {
			t.Parallel()

			w := waiting()
			w.Status = status

			if err := w.Validate(); err != nil {
				t.Fatalf("without an invite: %v", err)
			}

			w.InvitedAt, w.ExpiresAt = t0.Add(time.Minute), t0.Add(time.Hour)
			w.InviteTokenHash[0] = 1

			if err := w.Validate(); err != nil {
				t.Fatalf("with an invite: %v", err)
			}

			w.ExpiresAt = time.Time{}
			if err := w.Validate(); !errors.Is(err, domain.ErrValidation) {
				t.Errorf("half an invite: want ErrValidation, got %v", err)
			}

			w.InvitedAt = time.Time{}
			if err := w.Validate(); !errors.Is(err, domain.ErrValidation) {
				t.Errorf("a token without its invite: want ErrValidation, got %v", err)
			}
		})
	}
}

func TestPassValidate(t *testing.T) {
	t.Parallel()

	oct1 := domain.Date{Year: 2026, Month: time.October, Day: 1}

	t.Run("PassType", func(t *testing.T) {
		t.Parallel()
		checkValidate(t, func() domain.PassType {
			return domain.PassType{
				ID: newID[domain.PassTypeID](), Name: "10-visit pass", Kind: domain.PassVisits, Visits: 10,
				Price: 12000, ValidityDays: 180, VATRate: 1900, Active: true, CreatedAt: t0,
			}
		}, map[string]func(*domain.PassType){
			"zero id":           func(p *domain.PassType) { p.ID = domain.PassTypeID{} },
			"no name":           func(p *domain.PassType) { p.Name = " " },
			"padded name":       func(p *domain.PassType) { p.Name = " 10-visit pass" },
			"no kind":           func(p *domain.PassType) { p.Kind = "" },
			"zero visits":       func(p *domain.PassType) { p.Visits = 0 },
			"too many visits":   func(p *domain.PassType) { p.Visits = domain.MaxPassVisits + 1 },
			"time with visits":  func(p *domain.PassType) { p.Kind = domain.PassTime },
			"time without days": func(p *domain.PassType) { p.Kind, p.Visits, p.ValidityDays = domain.PassTime, 0, 0 },
			"negative price":    func(p *domain.PassType) { p.Price = -1 },
			"huge price":        func(p *domain.PassType) { p.Price = domain.MaxPassPrice + 1 },
			"negative validity": func(p *domain.PassType) { p.ValidityDays = -1 },
			"huge validity":     func(p *domain.PassType) { p.ValidityDays = domain.MaxPassValidityDays + 1 },
			"negative VAT":      func(p *domain.PassType) { p.VATRate = -1 },
			"VAT above 100%":    func(p *domain.PassType) { p.VATRate = domain.MaxVATRate + 1 },
			"no creation time":  func(p *domain.PassType) { p.CreatedAt = time.Time{} },
		})

		timePass := domain.PassType{
			ID: newID[domain.PassTypeID](), Name: "30-day pass", Kind: domain.PassTime, ValidityDays: 30, CreatedAt: t0,
		}
		if err := timePass.Validate(); err != nil {
			t.Errorf("a free time pass without VAT: %v", err)
		}

		if got, want := timePass.ValidUntil(oct1), (domain.Date{Year: 2026, Month: time.October, Day: 30}); got != want {
			t.Errorf("ValidUntil = %v, want %v", got, want)
		}

		if got := (domain.PassType{Kind: domain.PassVisits, Visits: 1}).ValidUntil(oct1); !got.IsZero() {
			t.Errorf("ValidUntil without validity = %v, want none", got)
		}
	})

	t.Run("Pass", func(t *testing.T) {
		t.Parallel()
		checkValidate(t, func() domain.Pass {
			return domain.Pass{
				ID: newID[domain.PassID](), PassTypeID: newID[domain.PassTypeID](), PersonID: newID[domain.PersonID](),
				Kind: domain.PassVisits, VisitsTotal: 10, VisitsRemaining: 0, ValidFrom: oct1, ValidUntil: oct1,
				Status: domain.PassVoided, Origin: domain.PassSold, SoldAt: t0, SoldBy: newID[domain.AccountID](),
				PayerID: newID[domain.PersonID](), ReceiptID: newID[domain.ReceiptID](), PricePaid: 12000,
				Note: "Paid in cash", VoidedAt: t0,
			}
		}, map[string]func(*domain.Pass){
			"zero id":            func(p *domain.Pass) { p.ID = domain.PassID{} },
			"zero type":          func(p *domain.Pass) { p.PassTypeID = domain.PassTypeID{} },
			"zero person":        func(p *domain.Pass) { p.PersonID = domain.PersonID{} },
			"no kind":            func(p *domain.Pass) { p.Kind = "" },
			"zero total":         func(p *domain.Pass) { p.VisitsTotal = 0 },
			"negative remaining": func(p *domain.Pass) { p.VisitsRemaining = -1 },
			"remaining > total":  func(p *domain.Pass) { p.VisitsRemaining = 11 },
			"time with visits":   func(p *domain.Pass) { p.Kind = domain.PassTime },
			"time without end": func(p *domain.Pass) {
				p.Kind, p.VisitsTotal, p.ValidUntil = domain.PassTime, 0, domain.Date{}
			},
			"no first day":        func(p *domain.Pass) { p.ValidFrom = domain.Date{} },
			"ends early":          func(p *domain.Pass) { p.ValidUntil = domain.Date{Year: 2026, Month: time.September, Day: 30} },
			"no status":           func(p *domain.Pass) { p.Status = "" },
			"no origin":           func(p *domain.Pass) { p.Origin = "" },
			"no sale time":        func(p *domain.Pass) { p.SoldAt = time.Time{} },
			"assigned with payer": func(p *domain.Pass) { p.Origin, p.ReceiptID, p.PricePaid = domain.PassAssigned, domain.ReceiptID{}, 0 },
			"assigned with receipt": func(p *domain.Pass) {
				p.Origin, p.PayerID, p.PricePaid = domain.PassAssigned, domain.PersonID{}, 0
			},
			"assigned with price": func(p *domain.Pass) {
				p.Origin, p.PayerID, p.ReceiptID = domain.PassAssigned, domain.PersonID{}, domain.ReceiptID{}
			},
			"negative price":  func(p *domain.Pass) { p.PricePaid = -1 },
			"padded note":     func(p *domain.Pass) { p.Note = "Paid in cash " },
			"long note":       func(p *domain.Pass) { p.Note = strings.Repeat("x", domain.MaxPassNoteLength+1) },
			"voided, no time": func(p *domain.Pass) { p.VoidedAt = time.Time{} },
			"active, voided":  func(p *domain.Pass) { p.Status = domain.PassActive },
			"voided early":    func(p *domain.Pass) { p.VoidedAt = t0.Add(-time.Second) },
		})
	})

	t.Run("Covers", func(t *testing.T) {
		t.Parallel()

		oct31 := domain.Date{Year: 2026, Month: time.October, Day: 31}
		visits := domain.Pass{
			Kind: domain.PassVisits, VisitsTotal: 10, VisitsRemaining: 1, ValidFrom: oct1, ValidUntil: oct31,
			Status: domain.PassActive,
		}
		timePass := visits
		timePass.Kind, timePass.VisitsTotal, timePass.VisitsRemaining = domain.PassTime, 0, 0
		forever := visits
		forever.ValidUntil = domain.Date{}
		used := visits
		used.VisitsRemaining = 0
		voided := visits
		voided.Status = domain.PassVoided

		for _, tc := range []struct {
			name string
			pass domain.Pass
			day  domain.Date
			want bool
		}{
			{"first day", visits, oct1, true},
			{"last day", visits, oct31, true},
			{"before", visits, oct1.AddDays(-1), false},
			{"after", visits, oct31.AddDays(1), false},
			{"time pass", timePass, oct31, true},
			{"time pass after", timePass, oct31.AddDays(1), false},
			{"no expiry", forever, oct31.AddDays(1000), true},
			{"used up", used, oct1, false},
			{"voided", voided, oct1, false},
		} {
			if got := tc.pass.Covers(tc.day); got != tc.want {
				t.Errorf("%s: Covers(%v) = %v, want %v", tc.name, tc.day, got, tc.want)
			}

			// A used-up pass stays valid for the visits it paid.
			if got, want := tc.pass.ValidOn(tc.day), tc.want || tc.name == "used up"; got != want {
				t.Errorf("%s: ValidOn(%v) = %v, want %v", tc.name, tc.day, got, want)
			}
		}
	})

	t.Run("Redemption", func(t *testing.T) {
		t.Parallel()
		checkValidate(t, func() domain.PassRedemption {
			return domain.PassRedemption{PassID: newID[domain.PassID](), BookingID: newID[domain.BookingID](), At: t0}
		}, map[string]func(*domain.PassRedemption){
			"zero pass":    func(r *domain.PassRedemption) { r.PassID = domain.PassID{} },
			"zero booking": func(r *domain.PassRedemption) { r.BookingID = domain.BookingID{} },
			"no time":      func(r *domain.PassRedemption) { r.At = time.Time{} },
		})
	})
}

func TestReceiptValidate(t *testing.T) {
	t.Parallel()

	sale := func() domain.Receipt {
		lines := []domain.ReceiptLine{
			domain.NewReceiptLine(1, "10-visit pass", 1, 12000, 1900, domain.TaxStandard),
			domain.NewReceiptLine(2, "Single visit", 2, 500, 700, domain.TaxStandard),
		}

		return domain.Receipt{
			ID: newID[domain.ReceiptID](), Number: "R-2026-0001", Year: 2026, Sequence: 1,
			Kind: domain.ReceiptSale, IssuedAt: t0, IssueDate: domain.Date{Year: 2026, Month: 3, Day: 2}, Locale: "de",
			Issuer:  domain.ReceiptIssuer{Name: "Example Dance", Address: "Example Street 1\n00000 Example Town", TaxID: "12/345/67890"},
			TaxMode: domain.TaxStandard, RecipientName: "Ada Example", PaymentMethod: domain.PaymentCash, Currency: "EUR",
			Net: lines[0].Net + lines[1].Net, VAT: lines[0].VAT + lines[1].VAT, Gross: 13000, Lines: lines,
		}
	}

	t.Run("Sale", func(t *testing.T) {
		t.Parallel()
		checkValidate(t, sale, map[string]func(*domain.Receipt){
			"zero id":           func(r *domain.Receipt) { r.ID = domain.ReceiptID{} },
			"no number":         func(r *domain.Receipt) { r.Number = "" },
			"zero year":         func(r *domain.Receipt) { r.Year = 0 },
			"zero sequence":     func(r *domain.Receipt) { r.Sequence = 0 },
			"bad kind":          func(r *domain.Receipt) { r.Kind = "refund" },
			"cancels something": func(r *domain.Receipt) { r.CancelsReceiptID = newID[domain.ReceiptID]() },
			"no issued at":      func(r *domain.Receipt) { r.IssuedAt = time.Time{} },
			"date of another year": func(r *domain.Receipt) {
				r.IssueDate = domain.Date{Year: 2025, Month: 12, Day: 31}
			},
			"no locale":           func(r *domain.Receipt) { r.Locale = "" },
			"no issuer":           func(r *domain.Receipt) { r.Issuer.Name = "" },
			"no issuer address":   func(r *domain.Receipt) { r.Issuer.Address = "" },
			"no tax id":           func(r *domain.Receipt) { r.Issuer.TaxID = "" },
			"bad tax mode":        func(r *domain.Receipt) { r.TaxMode = "zero" },
			"exempt without note": func(r *domain.Receipt) { r.TaxMode = domain.TaxExempt },
			"bad method":          func(r *domain.Receipt) { r.PaymentMethod = "barter" },
			"bad currency":        func(r *domain.Receipt) { r.Currency = "euro" },
			"no recipient":        func(r *domain.Receipt) { r.RecipientName = "" },
			"bad email":           func(r *domain.Receipt) { r.RecipientEmail = "ada" },
			"reason on a sale":    func(r *domain.Receipt) { r.Reason = "typo" },
			"no lines":            func(r *domain.Receipt) { r.Lines, r.Gross, r.Net, r.VAT = nil, 0, 0, 0 },
			"wrong total":         func(r *domain.Receipt) { r.Gross = 12999 },
			"wrong net":           func(r *domain.Receipt) { r.Net++ },
			"wrong line total":    func(r *domain.Receipt) { r.Lines[1].Gross = 999; r.Gross = 12999 },
			"wrong line VAT":      func(r *domain.Receipt) { r.Lines[0].VAT++; r.Lines[0].Net--; r.Net--; r.VAT++ },
			"wrong position":      func(r *domain.Receipt) { r.Lines[1].Position = 3 },
			"no description":      func(r *domain.Receipt) { r.Lines[0].Description = "" },
			"zero quantity": func(r *domain.Receipt) {
				r.Lines[1] = domain.NewReceiptLine(2, "Single visit", 0, 500, 700, domain.TaxStandard)
				r.Gross, r.Net, r.VAT = 12000, r.Lines[0].Net, r.Lines[0].VAT
			},
			"VAT in small business mode": func(r *domain.Receipt) { r.TaxMode = domain.TaxSmallBusiness },
			"huge unit price": func(r *domain.Receipt) {
				r.Lines = []domain.ReceiptLine{domain.NewReceiptLine(1, "x", 1, 1<<62, 0, domain.TaxStandard)}
				r.Gross, r.Net, r.VAT = 1<<62, 1<<62, 0
			},
			"negative sale": func(r *domain.Receipt) {
				r.Lines = []domain.ReceiptLine{domain.NewReceiptLine(1, "x", 1, -100, 0, domain.TaxStandard)}
				r.Gross, r.Net, r.VAT = -100, -100, 0
			},
		})
	})

	t.Run("Cancellation", func(t *testing.T) {
		t.Parallel()
		checkValidate(t, func() domain.Receipt {
			r := sale()
			r.Kind, r.CancelsReceiptID, r.Reason = domain.ReceiptCancellation, newID[domain.ReceiptID](), "Sold twice"
			for i, l := range r.Lines {
				r.Lines[i] = l.Negated()
			}

			r.Net, r.VAT, r.Gross = -r.Net, -r.VAT, -r.Gross

			return r
		}, map[string]func(*domain.Receipt){
			"no reference": func(r *domain.Receipt) { r.CancelsReceiptID = domain.ReceiptID{} },
			"cancels self": func(r *domain.Receipt) { r.CancelsReceiptID = r.ID },
			"positive line": func(r *domain.Receipt) {
				r.Lines[1] = r.Lines[1].Negated()
				r.Net, r.VAT, r.Gross = r.Lines[0].Net+r.Lines[1].Net, r.Lines[0].VAT+r.Lines[1].VAT, r.Lines[0].Gross+r.Lines[1].Gross
			},
		})
	})
}

func TestNewReceiptLine(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		name          string
		qty           int
		unit          domain.Money
		rate          int
		mode          domain.TaxMode
		net, vat, rat domain.Money
	}{
		{"19 % of 150.00", 1, 15000, 1900, domain.TaxStandard, 12605, 2395, 1900},
		{"7 % of 18.00", 1, 1800, 700, domain.TaxStandard, 1682, 118, 700},
		{"rounds half away from zero", 1, 107, 700, domain.TaxStandard, 100, 7, 700},
		{"half rounds up", 1, 1, 10000, domain.TaxStandard, 1, 0, 10000},
		{"quantity", 3, 1190, 1900, domain.TaxStandard, 3000, 570, 1900},
		{"no rate", 1, 999, 0, domain.TaxStandard, 999, 0, 0},
		{"small business charges no VAT", 1, 15000, 1900, domain.TaxSmallBusiness, 15000, 0, 0},
		{"exempt charges no VAT", 1, 15000, 1900, domain.TaxExempt, 15000, 0, 0},
		{"negative mirrors", 1, -15000, 1900, domain.TaxStandard, -12605, -2395, 1900},
		{"huge amounts stay exact", 10_000, 1_000_000_000_000, 1900, domain.TaxStandard, 8_403_361_344_537_815, 1_596_638_655_462_185, 1900},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			l := domain.NewReceiptLine(1, "x", c.qty, c.unit, c.rate, c.mode)
			if l.Net != c.net || l.VAT != c.vat || int(c.rat) != l.VATRate || l.Gross != domain.Money(c.qty)*c.unit {
				t.Fatalf("got net %d VAT %d rate %d gross %d, want net %d VAT %d rate %d", l.Net, l.VAT, l.VATRate, l.Gross, c.net, c.vat, c.rat)
			}

			if n := l.Negated(); n != domain.NewReceiptLine(1, "x", c.qty, -c.unit, c.rate, c.mode) {
				t.Fatalf("negated %+v does not round like the negated price", n)
			}
		})
	}
}

func TestReceiptSettings(t *testing.T) {
	t.Parallel()

	complete := domain.ReceiptSettings{
		Issuer:  domain.ReceiptIssuer{Name: "Example Dance", Address: "Example Street 1", TaxID: "DE000000000"},
		TaxMode: domain.TaxSmallBusiness,
	}
	if err := complete.Complete(); err != nil {
		t.Fatalf("complete settings: %v", err)
	}

	for name, mutate := range map[string]func(*domain.ReceiptSettings){
		"no name":             func(s *domain.ReceiptSettings) { s.Issuer.Name = " " },
		"no address":          func(s *domain.ReceiptSettings) { s.Issuer.Address = "" },
		"no tax id":           func(s *domain.ReceiptSettings) { s.Issuer.TaxID = "" },
		"no tax mode":         func(s *domain.ReceiptSettings) { s.TaxMode = "" },
		"exempt without note": func(s *domain.ReceiptSettings) { s.TaxMode = domain.TaxExempt },
	} {
		s := complete
		mutate(&s)

		var fe *domain.FieldError
		if err := s.Complete(); !errors.As(err, &fe) {
			t.Errorf("%s: want a field error, got %v", name, err)
		}
	}
}

func TestLogoValidate(t *testing.T) {
	t.Parallel()

	png := []byte("\x89PNG\r\n\x1a\n....")
	checkValidate(t, func() domain.Logo { return domain.Logo{ContentType: domain.LogoPNG, Data: png} },
		map[string]func(*domain.Logo){
			"gif":              func(l *domain.Logo) { l.ContentType = "image/gif" },
			"empty":            func(l *domain.Logo) { l.Data = nil },
			"jpeg with png":    func(l *domain.Logo) { l.ContentType = domain.LogoJPEG },
			"too large":        func(l *domain.Logo) { l.Data = append(png, make([]byte, domain.MaxLogoBytes)...) },
			"not a png at all": func(l *domain.Logo) { l.Data = []byte("<svg/>") },
		})

	if err := (domain.Logo{ContentType: domain.LogoJPEG, Data: []byte{0xFF, 0xD8, 0xFF, 0xE0}}).Validate(); err != nil {
		t.Fatalf("jpeg: %v", err)
	}
}

func TestAnnouncementValidate(t *testing.T) {
	t.Parallel()

	t.Run("Announcement", func(t *testing.T) {
		t.Parallel()
		checkValidate(t, func() domain.Announcement {
			return domain.Announcement{
				ID: newID[domain.AnnouncementID](), AuthorID: newID[domain.AccountID](), Title: "Holiday",
				Body: "The studio is closed next week.", Audience: domain.AudienceClass,
				ClassID: newID[domain.ClassID](), PublishAt: t0, ExpiresAt: t0.Add(7 * 24 * time.Hour),
				Email: true, EmailedAt: t0, EmailRecipients: 3, CreatedAt: t0, UpdatedAt: t0,
			}
		}, map[string]func(*domain.Announcement){
			"zero id":         func(a *domain.Announcement) { a.ID = domain.AnnouncementID{} },
			"zero author":     func(a *domain.Announcement) { a.AuthorID = domain.AccountID{} },
			"no title":        func(a *domain.Announcement) { a.Title = " " },
			"long title":      func(a *domain.Announcement) { a.Title = strings.Repeat("ä", 121) },
			"two-line title":  func(a *domain.Announcement) { a.Title = "Holiday\nnext week" },
			"padded title":    func(a *domain.Announcement) { a.Title = " Holiday" },
			"no body":         func(a *domain.Announcement) { a.Body = "\n" },
			"long body":       func(a *domain.Announcement) { a.Body = strings.Repeat("ä", 4001) },
			"bad audience":    func(a *domain.Announcement) { a.Audience = "world" },
			"no class":        func(a *domain.Announcement) { a.ClassID = domain.ClassID{} },
			"role too":        func(a *domain.Announcement) { a.Role = domain.RoleMember },
			"course type too": func(a *domain.Announcement) { a.CourseTypeID = newID[domain.CourseTypeID]() },
			"lesson too":      func(a *domain.Announcement) { a.LessonID = newID[domain.LessonID]() },
			"guest role": func(a *domain.Announcement) {
				a.Audience, a.ClassID, a.Role = domain.AudienceRole, domain.ClassID{}, domain.RoleGuest
			},
			"no publish":       func(a *domain.Announcement) { a.PublishAt = time.Time{} },
			"expires at pub":   func(a *domain.Announcement) { a.ExpiresAt = a.PublishAt },
			"mailed, no email": func(a *domain.Announcement) { a.Email = false },
			"counted unmailed": func(a *domain.Announcement) { a.EmailedAt = time.Time{} },
			"negative count":   func(a *domain.Announcement) { a.EmailRecipients = -1 },
			"no creation":      func(a *domain.Announcement) { a.CreatedAt = time.Time{} },
			"updated before":   func(a *domain.Announcement) { a.UpdatedAt = t0.Add(-time.Second) },
		})
	})

	t.Run("Audiences", func(t *testing.T) {
		t.Parallel()

		base := domain.Announcement{
			ID: newID[domain.AnnouncementID](), AuthorID: newID[domain.AccountID](), Title: "Hello", Body: "Hi",
			PublishAt: t0, CreatedAt: t0, UpdatedAt: t0,
		}
		for _, a := range []domain.Announcement{
			{Audience: domain.AudienceEveryone},
			{Audience: domain.AudienceRole, Role: domain.RoleInstructor},
			{Audience: domain.AudienceCourseType, CourseTypeID: newID[domain.CourseTypeID]()},
			{Audience: domain.AudienceLesson, LessonID: newID[domain.LessonID]()},
		} {
			v := base
			v.Audience, v.Role, v.CourseTypeID, v.LessonID = a.Audience, a.Role, a.CourseTypeID, a.LessonID
			if err := v.Validate(); err != nil {
				t.Errorf("%s: %v", a.Audience, err)
			}
		}
	})

	t.Run("Shown", func(t *testing.T) {
		t.Parallel()

		a := domain.Announcement{PublishAt: t0, ExpiresAt: t0.Add(time.Hour)}
		for at, want := range map[time.Time]bool{
			t0.Add(-time.Second): false, t0: true, t0.Add(time.Hour - 1): true, t0.Add(time.Hour): false,
		} {
			if got := a.ShownAt(at); got != want {
				t.Errorf("ShownAt(%v) = %v, want %v", at, got, want)
			}
		}

		a.ExpiresAt = time.Time{}
		if !a.ShownAt(t0.Add(1000 * time.Hour)) {
			t.Error("an announcement without expiry stops showing")
		}
	})

	t.Run("Dismissal", func(t *testing.T) {
		t.Parallel()
		checkValidate(t, func() domain.AnnouncementDismissal {
			return domain.AnnouncementDismissal{
				AnnouncementID: newID[domain.AnnouncementID](), AccountID: newID[domain.AccountID](), DismissedAt: t0,
			}
		}, map[string]func(*domain.AnnouncementDismissal){
			"zero announcement": func(d *domain.AnnouncementDismissal) { d.AnnouncementID = domain.AnnouncementID{} },
			"zero account":      func(d *domain.AnnouncementDismissal) { d.AccountID = domain.AccountID{} },
			"no time":           func(d *domain.AnnouncementDismissal) { d.DismissedAt = time.Time{} },
		})
	})

	t.Run("Mailing", func(t *testing.T) {
		t.Parallel()
		checkValidate(t, func() domain.AnnouncementMailing {
			return domain.AnnouncementMailing{
				ID: newID[domain.AnnouncementMailingID](), AnnouncementID: newID[domain.AnnouncementID](),
				SenderID: newID[domain.AccountID](), SentAt: t0, Recipients: 2,
			}
		}, map[string]func(*domain.AnnouncementMailing){
			"zero id":           func(m *domain.AnnouncementMailing) { m.ID = domain.AnnouncementMailingID{} },
			"zero announcement": func(m *domain.AnnouncementMailing) { m.AnnouncementID = domain.AnnouncementID{} },
			"zero sender":       func(m *domain.AnnouncementMailing) { m.SenderID = domain.AccountID{} },
			"no time":           func(m *domain.AnnouncementMailing) { m.SentAt = time.Time{} },
			"no recipients":     func(m *domain.AnnouncementMailing) { m.Recipients = 0 },
		})
	})
}

func TestIntegrationValidate(t *testing.T) {
	t.Parallel()

	t.Run("CalendarFeed", func(t *testing.T) {
		t.Parallel()
		checkValidate(t, func() domain.CalendarFeed {
			f := domain.CalendarFeed{
				ID: newID[domain.CalendarFeedID](), AccountID: newID[domain.AccountID](),
				Scope: domain.FeedSchedule, LocationID: newID[domain.LocationID](), CreatedAt: t0,
				LastUsedAt: t0, RevokedAt: t0,
			}
			f.TokenHash[0] = 1

			return f
		}, map[string]func(*domain.CalendarFeed){
			"zero id":        func(f *domain.CalendarFeed) { f.ID = domain.CalendarFeedID{} },
			"zero account":   func(f *domain.CalendarFeed) { f.AccountID = domain.AccountID{} },
			"bad scope":      func(f *domain.CalendarFeed) { f.Scope = "all" },
			"no token":       func(f *domain.CalendarFeed) { f.TokenHash = [32]byte{} },
			"no created at":  func(f *domain.CalendarFeed) { f.CreatedAt = time.Time{} },
			"revoked before": func(f *domain.CalendarFeed) { f.RevokedAt = t0.Add(-time.Second) },
			"used before":    func(f *domain.CalendarFeed) { f.LastUsedAt = t0.Add(-time.Second) },
			"location of a bookings feed": func(f *domain.CalendarFeed) {
				f.Scope = domain.FeedBookings
			},
		})
	})

	t.Run("ImportRun", func(t *testing.T) {
		t.Parallel()
		checkValidate(t, func() domain.ImportRun {
			return domain.ImportRun{
				ID: newID[domain.ImportRunID](), FileName: "members.csv", StartedAt: t0, FinishedAt: t0.Add(time.Minute),
				Counts: domain.ImportCounts{Rows: 4, Created: 1, Updated: 1, Unchanged: 1, Failed: 1, LinksAdded: 2},
				Errors: []domain.ImportRowError{{Line: 3, Code: "required", Field: "givenName"}},
			}
		}, map[string]func(*domain.ImportRun){
			"zero id":         func(r *domain.ImportRun) { r.ID = domain.ImportRunID{} },
			"no file name":    func(r *domain.ImportRun) { r.FileName = " " },
			"no start":        func(r *domain.ImportRun) { r.StartedAt = time.Time{} },
			"not finished":    func(r *domain.ImportRun) { r.FinishedAt = time.Time{} },
			"finished early":  func(r *domain.ImportRun) { r.FinishedAt = t0.Add(-time.Minute) },
			"negative count":  func(r *domain.ImportRun) { r.Counts.LinksAdded = -1 },
			"rows not summed": func(r *domain.ImportRun) { r.Counts.Rows = 5 },
			"too many errors": func(r *domain.ImportRun) { r.Errors = append(r.Errors, r.Errors[0]) },
		})
	})
}

func TestAuthValidate(t *testing.T) {
	t.Parallel()

	t.Run("Credential", func(t *testing.T) {
		t.Parallel()
		checkValidate(t, func() domain.Credential {
			return domain.Credential{AccountID: newID[domain.AccountID](), PasswordHash: "$argon2id$v=19$m=1,t=1,p=1$c2FsdA$a2V5", ChangedAt: t0}
		}, map[string]func(*domain.Credential){
			"zero account": func(c *domain.Credential) { c.AccountID = domain.AccountID{} },
			"no hash":      func(c *domain.Credential) { c.PasswordHash = " " },
			"no time":      func(c *domain.Credential) { c.ChangedAt = time.Time{} },
		})
	})

	t.Run("Session", func(t *testing.T) {
		t.Parallel()
		checkValidate(t, func() domain.Session {
			return domain.Session{
				ID: newID[domain.SessionID](), AccountID: newID[domain.AccountID](), TokenHash: domain.HashToken("token"),
				CreatedAt: t0, LastSeenAt: t0, ExpiresAt: t0.Add(time.Hour),
			}
		}, map[string]func(*domain.Session){
			"zero id":          func(s *domain.Session) { s.ID = domain.SessionID{} },
			"zero account":     func(s *domain.Session) { s.AccountID = domain.AccountID{} },
			"no token":         func(s *domain.Session) { s.TokenHash = [32]byte{} },
			"no creation":      func(s *domain.Session) { s.CreatedAt = time.Time{} },
			"seen before born": func(s *domain.Session) { s.LastSeenAt = t0.Add(-time.Second) },
			"expired at seen":  func(s *domain.Session) { s.ExpiresAt = s.LastSeenAt },
			"self-impersonate": func(s *domain.Session) { s.ImpersonatorID = s.AccountID },
		})
	})

	t.Run("AuditEvent", func(t *testing.T) {
		t.Parallel()
		checkValidate(t, func() domain.AuditEvent {
			return domain.AuditEvent{
				ID: newID[domain.AuditEventID](), At: t0, ActorID: newID[domain.AccountID](),
				Action: domain.AuditImpersonationStarted, TargetKind: domain.AuditTargetAccount,
				TargetID: newID[domain.AccountID](),
			}
		}, map[string]func(*domain.AuditEvent){
			"zero id":          func(e *domain.AuditEvent) { e.ID = domain.AuditEventID{} },
			"no time":          func(e *domain.AuditEvent) { e.At = time.Time{} },
			"no actor":         func(e *domain.AuditEvent) { e.ActorID = domain.AccountID{} },
			"bad action":       func(e *domain.AuditEvent) { e.Action = "deleted" },
			"no target":        func(e *domain.AuditEvent) { e.TargetID = domain.AccountID{} },
			"bad kind":         func(e *domain.AuditEvent) { e.TargetKind = "lesson" },
			"no kind":          func(e *domain.AuditEvent) { e.TargetKind = "" },
			"person too":       func(e *domain.AuditEvent) { e.TargetPersonID = newID[domain.PersonID]() },
			"person kind":      func(e *domain.AuditEvent) { e.TargetKind = domain.AuditTargetPerson },
			"impersonate none": func(e *domain.AuditEvent) { e.TargetKind, e.TargetID = "", domain.AccountID{} },
		})
	})

	t.Run("AuditMailRequeued", func(t *testing.T) {
		t.Parallel()
		checkValidate(t, func() domain.AuditEvent {
			return domain.AuditEvent{
				ID: newID[domain.AuditEventID](), At: t0, ActorID: newID[domain.AccountID](),
				Action:  domain.AuditMailRequeued,
				Details: domain.AuditDetails{MailID: newID[domain.OutboxMailID](), Recipient: "a@example.org", Template: "receipt"},
			}
		}, map[string]func(*domain.AuditEvent){
			"no mail": func(e *domain.AuditEvent) { e.Details.MailID = domain.OutboxMailID{} },
			"target": func(e *domain.AuditEvent) {
				e.TargetKind, e.TargetID = domain.AuditTargetAccount, newID[domain.AccountID]()
			},
		})
	})

	t.Run("AuditPersonMerged", func(t *testing.T) {
		t.Parallel()
		checkValidate(t, func() domain.AuditEvent {
			return domain.AuditEvent{
				ID: newID[domain.AuditEventID](), At: t0, ActorID: newID[domain.AccountID](),
				Action: domain.AuditPersonMerged, TargetKind: domain.AuditTargetPerson,
				TargetPersonID: newID[domain.PersonID](),
				Details:        domain.AuditDetails{MergedPersonID: newID[domain.PersonID](), MergedName: "Ada Example"},
			}
		}, map[string]func(*domain.AuditEvent){
			"no merged person": func(e *domain.AuditEvent) { e.Details.MergedPersonID = domain.PersonID{} },
			"merged into itself": func(e *domain.AuditEvent) {
				e.Details.MergedPersonID = e.TargetPersonID
			},
			"no target": func(e *domain.AuditEvent) {
				e.TargetKind, e.TargetPersonID = "", domain.PersonID{}
			},
		})
	})

	t.Run("AuditAccountChanges", func(t *testing.T) {
		t.Parallel()

		for _, action := range []domain.AuditAction{domain.AuditAccountDisabled, domain.AuditAccountEnabled} {
			checkValidate(t, func() domain.AuditEvent {
				return domain.AuditEvent{
					ID: newID[domain.AuditEventID](), At: t0, ActorID: newID[domain.AccountID](), Action: action,
					TargetKind: domain.AuditTargetAccount, TargetID: newID[domain.AccountID](),
				}
			}, map[string]func(*domain.AuditEvent){
				"no target": func(e *domain.AuditEvent) { e.TargetKind, e.TargetID = "", domain.AccountID{} },
				"roles": func(e *domain.AuditEvent) {
					e.Details.Role, e.Details.PreviousRole = domain.RoleAdmin, domain.RoleMember
				},
				"no actor": func(e *domain.AuditEvent) { e.ActorID = domain.AccountID{} },
			})
		}

		checkValidate(t, func() domain.AuditEvent {
			return domain.AuditEvent{
				ID: newID[domain.AuditEventID](), At: t0, ActorID: newID[domain.AccountID](),
				Action: domain.AuditAccountRoleChanged, TargetKind: domain.AuditTargetAccount,
				TargetID: newID[domain.AccountID](),
				Details:  domain.AuditDetails{PreviousRole: domain.RoleMember, Role: domain.RoleOffice},
			}
		}, map[string]func(*domain.AuditEvent){
			"no role":          func(e *domain.AuditEvent) { e.Details.Role = "" },
			"no previous role": func(e *domain.AuditEvent) { e.Details.PreviousRole = "" },
			"bad role":         func(e *domain.AuditEvent) { e.Details.Role = "boss" },
		})

		for _, action := range []domain.AuditAction{domain.AuditAccountDeleted, domain.AuditPersonAnonymized} {
			checkValidate(t, func() domain.AuditEvent {
				return domain.AuditEvent{
					ID: newID[domain.AuditEventID](), At: t0, ActorID: newID[domain.AccountID](), Action: action,
					TargetKind: domain.AuditTargetPerson, TargetPersonID: newID[domain.PersonID](),
				}
			}, map[string]func(*domain.AuditEvent){
				"an account": func(e *domain.AuditEvent) {
					e.TargetKind, e.TargetPersonID, e.TargetID = domain.AuditTargetAccount, domain.PersonID{}, newID[domain.AccountID]()
				},
			})
		}

		// Once its account is deleted, an impersonation or an account change
		// targets the account's person.
		for _, action := range []domain.AuditAction{
			domain.AuditImpersonationStarted, domain.AuditImpersonationEnded, domain.AuditAccountDisabled,
			domain.AuditAccountEnabled, domain.AuditAccountRoleChanged,
		} {
			e := domain.AuditEvent{
				ID: newID[domain.AuditEventID](), At: t0, ActorID: newID[domain.AccountID](), Action: action,
				TargetKind: domain.AuditTargetAccount, TargetID: newID[domain.AccountID](),
			}
			if action == domain.AuditAccountRoleChanged {
				e.Details = domain.AuditDetails{PreviousRole: domain.RoleMember, Role: domain.RoleOffice}
			}

			person := newID[domain.PersonID]()
			moved := e.Retarget(person)

			if moved.TargetKind != domain.AuditTargetPerson || !moved.TargetID.IsZero() || moved.TargetPersonID != person {
				t.Errorf("%s: retargeted to %+v", action, moved)
			}

			if err := moved.Validate(); err != nil {
				t.Errorf("%s of a deleted account: %v", action, err)
			}

			moved.TargetKind, moved.TargetPersonID = "", domain.PersonID{}
			if err := moved.Validate(); err == nil {
				t.Errorf("%s without a target", action)
			}
		}

		// Only an account its holder deleted has no actor.
		selfDeleted := domain.AuditEvent{
			ID: newID[domain.AuditEventID](), At: t0, Action: domain.AuditAccountDeleted,
			TargetKind: domain.AuditTargetPerson, TargetPersonID: newID[domain.PersonID](),
		}
		if err := selfDeleted.Validate(); err != nil {
			t.Errorf("a self-deleted account: %v", err)
		}

		selfDeleted.Action = domain.AuditPersonAnonymized
		if err := selfDeleted.Validate(); err == nil {
			t.Error("an anonymization without an actor")
		}
	})

	t.Run("LessonNotice", func(t *testing.T) {
		t.Parallel()
		checkValidate(t, func() domain.LessonNotice {
			return domain.LessonNotice{
				ID: newID[domain.LessonNoticeID](), LessonID: newID[domain.LessonID](),
				SenderID: newID[domain.AccountID](), SentAt: t0, Recipients: 1,
			}
		}, map[string]func(*domain.LessonNotice){
			"zero id":       func(n *domain.LessonNotice) { n.ID = domain.LessonNoticeID{} },
			"no lesson":     func(n *domain.LessonNotice) { n.LessonID = domain.LessonID{} },
			"no sender":     func(n *domain.LessonNotice) { n.SenderID = domain.AccountID{} },
			"no time":       func(n *domain.LessonNotice) { n.SentAt = time.Time{} },
			"no recipients": func(n *domain.LessonNotice) { n.Recipients = 0 },
		})
	})

	t.Run("AccountToken", func(t *testing.T) {
		t.Parallel()
		checkValidate(t, func() domain.AccountToken {
			return domain.AccountToken{
				Hash: domain.HashToken("token"), AccountID: newID[domain.AccountID](), Purpose: domain.TokenConfirmEmail,
				Email: "ada@example.org", CreatedAt: t0, ExpiresAt: t0.Add(24 * time.Hour),
			}
		}, map[string]func(*domain.AccountToken){
			"no hash":      func(a *domain.AccountToken) { a.Hash = [32]byte{} },
			"zero account": func(a *domain.AccountToken) { a.AccountID = domain.AccountID{} },
			"bad purpose":  func(a *domain.AccountToken) { a.Purpose = "unlock-door" },
			"bad email":    func(a *domain.AccountToken) { a.Email = "ada" },
			"no creation":  func(a *domain.AccountToken) { a.CreatedAt = time.Time{} },
			"no expiry":    func(a *domain.AccountToken) { a.ExpiresAt = a.CreatedAt },
		})
	})

	t.Run("HashToken", func(t *testing.T) {
		t.Parallel()

		a, again := domain.HashToken("a"), domain.HashToken("a")
		if a != again || a == domain.HashToken("b") || a == [32]byte{} {
			t.Fatal("HashToken must be deterministic and distinguish tokens")
		}
	})
}

func TestOutboxMailValidate(t *testing.T) {
	t.Parallel()

	pending := func() domain.OutboxMail {
		return domain.OutboxMail{
			ID: newID[domain.OutboxMailID](), To: "ada@example.org", Subject: "Hello", Text: "Hi Ada",
			CreatedAt: t0, NextAttemptAt: t0,
		}
	}

	checkValidate(t, pending, map[string]func(*domain.OutboxMail){
		"zero id":        func(m *domain.OutboxMail) { m.ID = domain.OutboxMailID{} },
		"bad recipient":  func(m *domain.OutboxMail) { m.To = "Ada <ada@example.org>" },
		"bad reply-to":   func(m *domain.OutboxMail) { m.ReplyTo = "ada@" },
		"no subject":     func(m *domain.OutboxMail) { m.Subject = "" },
		"no text":        func(m *domain.OutboxMail) { m.Text = " " },
		"no creation":    func(m *domain.OutboxMail) { m.CreatedAt = time.Time{} },
		"negative tries": func(m *domain.OutboxMail) { m.Attempts = -1 },
		"sent, but due":  func(m *domain.OutboxMail) { m.SentAt = t0 },
		"sent too early": func(m *domain.OutboxMail) { m.SentAt, m.NextAttemptAt = t0.Add(-time.Second), time.Time{} },
	})

	sent, failed := pending(), pending()
	sent.SentAt, sent.NextAttemptAt = t0, time.Time{}
	failed.NextAttemptAt = time.Time{}

	for m, want := range map[domain.OutboxMail]domain.MailStatus{
		pending(): domain.MailPending, sent: domain.MailSent, failed: domain.MailFailed,
	} {
		if got := m.Status(); got != want {
			t.Errorf("Status() = %s, want %s", got, want)
		}
	}

	// The purge removes the bodies of sent mails only.
	purged := sent
	purged.Text = ""
	if err := purged.Validate(); err != nil || !purged.Purged() {
		t.Errorf("purged sent mail: Validate = %v, Purged = %v", err, purged.Purged())
	}

	purged.HTML = "<p>Hi</p>"
	if err := purged.Validate(); err == nil {
		t.Error("a mail with HTML but no text is valid")
	}
}

func TestFieldErrors(t *testing.T) {
	t.Parallel()

	room := domain.Room{ID: newID[domain.RoomID](), LocationID: newID[domain.LocationID](), Name: "Studio A", Capacity: 20}
	class := domain.Class{
		ID: newID[domain.ClassID](), TermID: newID[domain.TermID](), CourseTypeID: newID[domain.CourseTypeID](),
		RoomID: newID[domain.RoomID](), InstructorID: newID[domain.StaffID](), Level: domain.LevelOpen,
		Weekday: time.Monday, StartTime: domain.TimeOfDay{Hour: 18}, Duration: time.Hour, Capacity: 12,
	}
	classWith := func(f func(*domain.Class)) domain.Class { c := class; f(&c); return c }

	for name, tc := range map[string]struct {
		entity interface{ Validate() error }
		field  string
	}{
		"location name":     {domain.Location{ID: newID[domain.LocationID]()}, "name"},
		"room location":     {func() domain.Room { r := room; r.LocationID = domain.LocationID{}; return r }(), "locationId"},
		"room name":         {func() domain.Room { r := room; r.Name = " "; return r }(), "name"},
		"room capacity":     {func() domain.Room { r := room; r.Capacity = 0; return r }(), "capacity"},
		"course type":       {domain.CourseType{ID: newID[domain.CourseTypeID]()}, "name"},
		"staff name":        {domain.Staff{ID: newID[domain.StaffID](), Kind: domain.StaffInstructor}, "displayName"},
		"staff kind":        {domain.Staff{ID: newID[domain.StaffID](), DisplayName: "A", Kind: "dancer"}, "kind"},
		"class course type": {classWith(func(c *domain.Class) { c.CourseTypeID = domain.CourseTypeID{} }), "courseTypeId"},
		"class room":        {classWith(func(c *domain.Class) { c.RoomID = domain.RoomID{} }), "roomId"},
		"class instructor":  {classWith(func(c *domain.Class) { c.InstructorID = domain.StaffID{} }), "instructorId"},
		"class accompanist": {classWith(func(c *domain.Class) { c.AccompanistID = c.InstructorID }), "accompanistId"},
		"class level":       {classWith(func(c *domain.Class) { c.Level = "expert" }), "level"},
		"class weekday":     {classWith(func(c *domain.Class) { c.Weekday = 7 }), "weekday"},
		"class start":       {classWith(func(c *domain.Class) { c.StartTime = domain.TimeOfDay{Hour: 24} }), "startTime"},
		"class duration":    {classWith(func(c *domain.Class) { c.Duration = 0 }), "durationMinutes"},
		"class midnight":    {classWith(func(c *domain.Class) { c.Duration = 7 * time.Hour }), "durationMinutes"},
		"class capacity":    {classWith(func(c *domain.Class) { c.Capacity = 0 }), "capacity"},
	} {
		err := tc.entity.Validate()

		var fe *domain.FieldError
		if !errors.As(err, &fe) || fe.Field != tc.field || !errors.Is(err, domain.ErrValidation) {
			t.Errorf("%s: %v, want a FieldError on %q wrapping ErrValidation", name, err, tc.field)
		}
	}

	// A rule about the entity as a whole names no field.
	var fe *domain.FieldError
	if err := (domain.Room{}).Validate(); errors.As(err, &fe) {
		t.Errorf("zero room id: %v is a FieldError on %q", err, fe.Field)
	}

	for _, err := range []error{domain.ErrInUse, domain.ErrDuplicate} {
		if !errors.Is(err, domain.ErrConflict) {
			t.Errorf("%v does not wrap ErrConflict", err)
		}
	}
}
