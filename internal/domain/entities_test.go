package domain_test

import (
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
	t.Run("WaitlistStatus", func(t *testing.T) { t.Parallel(); checkEnum(t, domain.WaitlistStatuses()) })
	t.Run("ReceiptKind", func(t *testing.T) { t.Parallel(); checkEnum(t, domain.ReceiptKinds()) })
	t.Run("Audience", func(t *testing.T) { t.Parallel(); checkEnum(t, domain.Audiences()) })
	t.Run("FeedScope", func(t *testing.T) { t.Parallel(); checkEnum(t, domain.FeedScopes()) })
	t.Run("ImportStatus", func(t *testing.T) { t.Parallel(); checkEnum(t, domain.ImportStatuses()) })
}

func TestPersonValidate(t *testing.T) {
	t.Parallel()

	checkValidate(t, func() domain.Person {
		return domain.Person{
			ID: newID[domain.PersonID](), GivenName: "Ada", FamilyName: "Example",
			Email: "ada@example.org", BirthDate: domain.Date{Year: 2015, Month: time.May, Day: 4},
		}
	}, map[string]func(*domain.Person){
		"zero id":         func(p *domain.Person) { p.ID = domain.PersonID{} },
		"no given name":   func(p *domain.Person) { p.GivenName = " " },
		"no family name":  func(p *domain.Person) { p.FamilyName = "" },
		"bad email":       func(p *domain.Person) { p.Email = "not-an-email" },
		"named email":     func(p *domain.Person) { p.Email = "Ada <ada@example.org>" },
		"email surrounds": func(p *domain.Person) { p.Email = " ada@example.org" },
		"bad birth date":  func(p *domain.Person) { p.BirthDate = domain.Date{Year: 2015, Month: 2, Day: 30} },
	})
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
			Email: "ada@example.org", Role: domain.RoleMember, VisibleDays: 14,
		}
	}, map[string]func(*domain.Account){
		"zero id":       func(a *domain.Account) { a.ID = domain.AccountID{} },
		"zero person":   func(a *domain.Account) { a.PersonID = domain.PersonID{} },
		"no email":      func(a *domain.Account) { a.Email = "" },
		"bad role":      func(a *domain.Account) { a.Role = "root" },
		"guest role":    func(a *domain.Account) { a.Role = domain.RoleGuest },
		"negative days": func(a *domain.Account) { a.VisibleDays = -1 },
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
		})
	})
}

func TestLessonValidate(t *testing.T) {
	t.Parallel()

	checkValidate(t, func() domain.Lesson {
		return domain.Lesson{
			ID: newID[domain.LessonID](), CourseTypeID: newID[domain.CourseTypeID](), RoomID: newID[domain.RoomID](),
			InstructorID: newID[domain.StaffID](), StartsAt: t0, EndsAt: t0.Add(time.Hour),
			Status: domain.LessonScheduled, Visibility: domain.VisibilityPublic, Kind: domain.LessonRegular, Capacity: 12,
		}
	}, map[string]func(*domain.Lesson){
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
	})
}

func TestBookingValidate(t *testing.T) {
	t.Parallel()

	member := func() domain.Booking {
		return domain.Booking{
			ID: newID[domain.BookingID](), LessonID: newID[domain.LessonID](), PersonID: newID[domain.PersonID](),
			Status: domain.BookingBooked, BookedAt: t0,
		}
	}

	t.Run("Member", func(t *testing.T) {
		t.Parallel()
		checkValidate(t, member, map[string]func(*domain.Booking){
			"zero id":           func(b *domain.Booking) { b.ID = domain.BookingID{} },
			"zero lesson":       func(b *domain.Booking) { b.LessonID = domain.LessonID{} },
			"person and guest":  func(b *domain.Booking) { b.GuestName = "Guest" },
			"neither":           func(b *domain.Booking) { b.PersonID = domain.PersonID{} },
			"bad status":        func(b *domain.Booking) { b.Status = "maybe" },
			"no booked at":      func(b *domain.Booking) { b.BookedAt = time.Time{} },
			"canceled no time":  func(b *domain.Booking) { b.Status = domain.BookingCanceled },
			"time not canceled": func(b *domain.Booking) { b.CanceledAt = t0.Add(time.Hour) },
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
			b.Status, b.CanceledAt = domain.BookingCanceled, t0.Add(time.Hour)

			return b
		}, map[string]func(*domain.Booking){
			"no name":   func(b *domain.Booking) { b.GuestName = " " },
			"no email":  func(b *domain.Booking) { b.GuestEmail = "" },
			"bad email": func(b *domain.Booking) { b.GuestEmail = "guest@" },
		})
	})

	t.Run("Event", func(t *testing.T) {
		t.Parallel()
		checkValidate(t, func() domain.BookingEvent {
			return domain.BookingEvent{
				ID: newID[domain.BookingEventID](), BookingID: newID[domain.BookingID](),
				At: t0, Status: domain.BookingAttended, ActorAccountID: newID[domain.AccountID](),
			}
		}, map[string]func(*domain.BookingEvent){
			"zero id":      func(e *domain.BookingEvent) { e.ID = domain.BookingEventID{} },
			"zero booking": func(e *domain.BookingEvent) { e.BookingID = domain.BookingID{} },
			"no time":      func(e *domain.BookingEvent) { e.At = time.Time{} },
			"bad status":   func(e *domain.BookingEvent) { e.Status = "" },
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
			"has token":    func(w *domain.WaitlistEntry) { w.InviteTokenHash[0] = 1 },
			"has invite":   func(w *domain.WaitlistEntry) { w.InvitedAt = t0 },
		})
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
}

func TestPassValidate(t *testing.T) {
	t.Parallel()

	t.Run("PassType", func(t *testing.T) {
		t.Parallel()
		checkValidate(t, func() domain.PassType {
			return domain.PassType{ID: newID[domain.PassTypeID](), Name: "10-visit pass", Visits: 10, Price: 12000, ValidityDays: 180}
		}, map[string]func(*domain.PassType){
			"zero id":           func(p *domain.PassType) { p.ID = domain.PassTypeID{} },
			"no name":           func(p *domain.PassType) { p.Name = "" },
			"zero visits":       func(p *domain.PassType) { p.Visits = 0 },
			"negative price":    func(p *domain.PassType) { p.Price = -1 },
			"negative validity": func(p *domain.PassType) { p.ValidityDays = -1 },
		})
	})

	t.Run("Pass", func(t *testing.T) {
		t.Parallel()
		checkValidate(t, func() domain.Pass {
			return domain.Pass{
				ID: newID[domain.PassID](), PassTypeID: newID[domain.PassTypeID](), PersonID: newID[domain.PersonID](),
				VisitsTotal: 10, VisitsRemaining: 0,
				PurchasedOn: domain.Date{Year: 2026, Month: time.October, Day: 1},
				ExpiresOn:   domain.Date{Year: 2026, Month: time.October, Day: 1},
			}
		}, map[string]func(*domain.Pass){
			"zero id":            func(p *domain.Pass) { p.ID = domain.PassID{} },
			"zero type":          func(p *domain.Pass) { p.PassTypeID = domain.PassTypeID{} },
			"zero person":        func(p *domain.Pass) { p.PersonID = domain.PersonID{} },
			"zero total":         func(p *domain.Pass) { p.VisitsTotal = 0 },
			"negative remaining": func(p *domain.Pass) { p.VisitsRemaining = -1 },
			"remaining > total":  func(p *domain.Pass) { p.VisitsRemaining = 11 },
			"no purchase date":   func(p *domain.Pass) { p.PurchasedOn = domain.Date{} },
			"expires early":      func(p *domain.Pass) { p.ExpiresOn = domain.Date{Year: 2026, Month: time.September, Day: 30} },
		})
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
		return domain.Receipt{
			ID: newID[domain.ReceiptID](), Number: "R-2026-0001", Year: 2026, Sequence: 1,
			Kind: domain.ReceiptSale, IssuedAt: t0, RecipientName: "Ada Example", Total: 13000,
			Lines: []domain.ReceiptLine{
				{Position: 1, Description: "10-visit pass", Quantity: 1, UnitPrice: 12000, Total: 12000},
				{Position: 2, Description: "Trial lesson", Quantity: 2, UnitPrice: 500, Total: 1000},
			},
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
			"no recipient":      func(r *domain.Receipt) { r.RecipientName = "" },
			"no lines":          func(r *domain.Receipt) { r.Lines, r.Total = nil, 0 },
			"wrong total":       func(r *domain.Receipt) { r.Total = 12999 },
			"wrong line total":  func(r *domain.Receipt) { r.Lines[1].Total = 999; r.Total = 12999 },
			"wrong position":    func(r *domain.Receipt) { r.Lines[1].Position = 3 },
			"no description":    func(r *domain.Receipt) { r.Lines[0].Description = "" },
			"zero quantity":     func(r *domain.Receipt) { r.Lines[1].Quantity, r.Lines[1].Total, r.Total = 0, 0, 12000 },
			"negative sale": func(r *domain.Receipt) {
				r.Lines[0].UnitPrice, r.Lines[0].Total, r.Total = -12000, -12000, -11000
			},
		})
	})

	t.Run("Cancellation", func(t *testing.T) {
		t.Parallel()
		checkValidate(t, func() domain.Receipt {
			r := sale()
			r.Kind, r.CancelsReceiptID, r.Total = domain.ReceiptCancellation, newID[domain.ReceiptID](), -12000
			r.Lines = []domain.ReceiptLine{{Position: 1, Description: "Cancellation", Quantity: 1, UnitPrice: -12000, Total: -12000}}

			return r
		}, map[string]func(*domain.Receipt){
			"no reference": func(r *domain.Receipt) { r.CancelsReceiptID = domain.ReceiptID{} },
			"cancels self": func(r *domain.Receipt) { r.CancelsReceiptID = r.ID },
			"positive": func(r *domain.Receipt) {
				r.Lines[0].UnitPrice, r.Lines[0].Total, r.Total = 12000, 12000, 12000
			},
		})
	})
}

func TestMessageValidate(t *testing.T) {
	t.Parallel()

	t.Run("Message", func(t *testing.T) {
		t.Parallel()
		checkValidate(t, func() domain.Message {
			return domain.Message{
				ID: newID[domain.MessageID](), AuthorAccountID: newID[domain.AccountID](), Title: "Holiday",
				Body: "The studio is closed next week.", Audience: domain.AudienceMembers, InApp: true,
				PublishedAt: t0, ExpiresAt: t0.Add(7 * 24 * time.Hour),
			}
		}, map[string]func(*domain.Message){
			"zero id":        func(m *domain.Message) { m.ID = domain.MessageID{} },
			"zero author":    func(m *domain.Message) { m.AuthorAccountID = domain.AccountID{} },
			"no title":       func(m *domain.Message) { m.Title = "" },
			"no body":        func(m *domain.Message) { m.Body = " " },
			"bad audience":   func(m *domain.Message) { m.Audience = "world" },
			"no channel":     func(m *domain.Message) { m.InApp = false },
			"no published":   func(m *domain.Message) { m.PublishedAt = time.Time{} },
			"expires at pub": func(m *domain.Message) { m.ExpiresAt = m.PublishedAt },
		})
	})

	t.Run("Read", func(t *testing.T) {
		t.Parallel()
		checkValidate(t, func() domain.MessageRead {
			return domain.MessageRead{MessageID: newID[domain.MessageID](), AccountID: newID[domain.AccountID](), ReadAt: t0}
		}, map[string]func(*domain.MessageRead){
			"zero message": func(r *domain.MessageRead) { r.MessageID = domain.MessageID{} },
			"zero account": func(r *domain.MessageRead) { r.AccountID = domain.AccountID{} },
			"no time":      func(r *domain.MessageRead) { r.ReadAt = time.Time{} },
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
				Scope: domain.FeedBookings, CreatedAt: t0, RevokedAt: t0,
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
		})
	})

	t.Run("ImportRun", func(t *testing.T) {
		t.Parallel()
		checkValidate(t, func() domain.ImportRun {
			return domain.ImportRun{
				ID: newID[domain.ImportRunID](), Source: "legacy-csv", Status: domain.ImportSucceeded,
				StartedAt: t0, FinishedAt: t0.Add(time.Minute),
			}
		}, map[string]func(*domain.ImportRun){
			"zero id":            func(r *domain.ImportRun) { r.ID = domain.ImportRunID{} },
			"no source":          func(r *domain.ImportRun) { r.Source = "" },
			"bad status":         func(r *domain.ImportRun) { r.Status = "paused" },
			"no start":           func(r *domain.ImportRun) { r.StartedAt = time.Time{} },
			"done, not finished": func(r *domain.ImportRun) { r.FinishedAt = time.Time{} },
			"running, finished":  func(r *domain.ImportRun) { r.Status = domain.ImportRunning },
			"finished early":     func(r *domain.ImportRun) { r.FinishedAt = t0.Add(-time.Minute) },
		})
	})
}
