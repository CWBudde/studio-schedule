package service

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/mail"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// The instructor view tests run on the staff fixture (Europe/Berlin): at
// bookingNow, Wednesday 2026-10-21, 12:00 in Berlin, the default range is
// the school dates 2026-10-14 to 2026-10-28.

func (f *staffFixture) teach(t *testing.T, now string) *TeachService {
	t.Helper()

	return NewTeachService(f.st, clock.NewFixed(mustInstant(t, now)), f.seq, newTestRenderer(t))
}

// otherStaff returns Otto's staff record.
func (f *staffFixture) otherStaff(t *testing.T) domain.StaffID {
	t.Helper()

	var id domain.StaffID

	f.write(t, func(ctx context.Context, tx store.Tx) error {
		staff, err := tx.Staff().ListByPerson(ctx, f.otto.ID)
		if err != nil || len(staff) != 1 {
			return errors.Join(err, errors.New("otto has no staff record"))
		}

		id = staff[0].ID

		return nil
	})

	return id
}

// lessonBy stores a one-off lesson of an hour from start with instructor
// and accompanist (none if zero).
func (f *staffFixture) lessonBy(t *testing.T, start string, instructor, accompanist domain.StaffID) domain.Lesson {
	t.Helper()

	l := f.regular(t, start)
	l.InstructorID, l.AccompanistID = instructor, accompanist
	f.write(t, func(ctx context.Context, tx store.Tx) error { return tx.Lessons().Update(ctx, l) })

	return l
}

func (f *staffFixture) teachLessons(t *testing.T, now string, p Principal, q TeachQuery) TeachLessons {
	t.Helper()

	out, err := f.teach(t, now).Lessons(t.Context(), p, q)
	if err != nil {
		t.Fatalf("Lessons: %v", err)
	}

	return out
}

func teachIDs(ls []TeachLesson) []domain.LessonID {
	out := make([]domain.LessonID, len(ls))
	for i, l := range ls {
		out[i] = l.Lesson.ID
	}

	return out
}

func wantLessons(t *testing.T, got TeachLessons, want ...domain.Lesson) {
	t.Helper()

	ids := make([]domain.LessonID, len(want))
	for i, l := range want {
		ids[i] = l.ID
	}

	if !slices.Equal(teachIDs(got.Lessons), ids) {
		t.Fatalf("lessons %v, want %v", teachIDs(got.Lessons), ids)
	}
}

func TestTeachWindow(t *testing.T) {
	t.Parallel()

	f := newStaffFixture(t)
	ivy := f.principal(t, f.ivy)

	// Midnight in Berlin is 22:00 UTC the day before (summer time until
	// 2026-10-25), 23:00 UTC after.
	f.lessonBy(t, "2026-10-13T21:59:00Z", f.instructor, domain.StaffID{}) // 2026-10-13 in Berlin
	first := f.lessonBy(t, "2026-10-13T22:00:00Z", f.instructor, domain.StaffID{})
	last := f.lessonBy(t, "2026-10-28T22:59:00Z", f.instructor, domain.StaffID{})
	later := f.lessonBy(t, "2026-10-28T23:00:00Z", f.instructor, domain.StaffID{}) // 2026-10-29

	got := f.teachLessons(t, bookingNow, ivy, TeachQuery{})
	wantLessons(t, got, first, last)

	if got.From != mustDate(t, "2026-10-14") || got.To != mustDate(t, "2026-10-29") || got.Timezone != "Europe/Berlin" ||
		!got.Now.Equal(mustInstant(t, bookingNow)) {
		t.Errorf("window %v – %v in %s at %v", got.From, got.To, got.Timezone, got.Now)
	}

	// A from alone gets the default length; a to alone starts a week back.
	got = f.teachLessons(t, bookingNow, ivy, TeachQuery{From: mustDate(t, "2026-10-28")})
	if got.To != mustDate(t, "2026-11-12") {
		t.Errorf("to = %v", got.To)
	}

	wantLessons(t, got, last, later)

	got = f.teachLessons(t, bookingNow, ivy, TeachQuery{To: mustDate(t, "2026-10-15")})
	wantLessons(t, got, first)

	// At most 31 days, and to after from.
	f.teachLessons(t, bookingNow, ivy, TeachQuery{From: mustDate(t, "2026-10-01"), To: mustDate(t, "2026-11-01")})

	for name, q := range map[string]TeachQuery{
		"32 days":     {From: mustDate(t, "2026-10-01"), To: mustDate(t, "2026-11-02")},
		"to = from":   {From: mustDate(t, "2026-10-01"), To: mustDate(t, "2026-10-01")},
		"to < from":   {From: mustDate(t, "2026-10-02"), To: mustDate(t, "2026-10-01")},
		"invalid day": {From: domain.Date{Year: 2026, Month: time.February, Day: 30}},
	} {
		if _, err := f.teach(t, bookingNow).Lessons(t.Context(), ivy, q); !errors.Is(err, domain.ErrValidation) {
			t.Errorf("%s: want ErrValidation, got %v", name, err)
		}
	}
}

func TestTeachOwnLessons(t *testing.T) {
	t.Parallel()

	f := newStaffFixture(t)
	other := f.otherStaff(t)

	taught := f.lessonBy(t, "2026-10-22T16:00:00Z", f.instructor, f.pianist)
	foreign := f.lessonBy(t, "2026-10-22T17:00:00Z", other, domain.StaffID{})
	accompanied := f.lessonBy(t, "2026-10-23T16:00:00Z", other, f.instructor)

	roles := func(got TeachLessons) []TeachRole {
		out := make([]TeachRole, len(got.Lessons))
		for i, l := range got.Lessons {
			out[i] = l.Role
		}

		return out
	}

	ivy := f.teachLessons(t, bookingNow, f.principal(t, f.ivy), TeachQuery{})
	wantLessons(t, ivy, taught, accompanied)

	if got := roles(ivy); !slices.Equal(got, []TeachRole{TeachInstructor, TeachAccompanist}) {
		t.Errorf("ivy's roles %v", got)
	}

	paul := f.teachLessons(t, bookingNow, f.principal(t, f.paul), TeachQuery{})
	wantLessons(t, paul, taught)

	if paul.Lessons[0].Role != TeachAccompanist || paul.Lessons[0].Instructor.DisplayName != "Ada Example" ||
		paul.Lessons[0].Accompanist.DisplayName != "Ben Example" || paul.Lessons[0].CourseType.Name != "Imaginary Ballet" ||
		paul.Lessons[0].Room.Name != "Room One" || paul.Lessons[0].Location.Name != "Studio Nowhere" {
		t.Errorf("paul's lesson %+v", paul.Lessons[0])
	}

	wantLessons(t, f.teachLessons(t, bookingNow, f.principal(t, f.otto), TeachQuery{}), foreign, accompanied)

	// Without a staff link, nothing (and no error).
	nils := f.teachLessons(t, bookingNow, f.principal(t, f.nils), TeachQuery{})
	wantLessons(t, nils)

	if nils.Timezone != "Europe/Berlin" {
		t.Errorf("nils %+v", nils)
	}

	// The filter narrows an instructor's lessons, never widens them.
	ivyP := f.principal(t, f.ivy)
	wantLessons(t, f.teachLessons(t, bookingNow, ivyP, TeachQuery{Staff: f.instructor}), taught, accompanied)
	wantLessons(t, f.teachLessons(t, bookingNow, ivyP, TeachQuery{Staff: other}))

	// The office sees every lesson, or one staff member's.
	office := f.teachLessons(t, bookingNow, f.principal(t, f.olga), TeachQuery{})
	wantLessons(t, office, taught, foreign, accompanied)

	if got := roles(office); !slices.Equal(got, []TeachRole{TeachOffice, TeachOffice, TeachOffice}) {
		t.Errorf("office roles %v", got)
	}

	wantLessons(t, f.teachLessons(t, bookingNow, f.principal(t, f.olga), TeachQuery{Staff: f.instructor}),
		taught, accompanied)
	wantLessons(t, f.teachLessons(t, bookingNow, f.principal(t, f.olga), TeachQuery{Staff: f.pianist}), taught)

	// An inactive staff record loses its lessons.
	f.write(t, func(ctx context.Context, tx store.Tx) error {
		s, err := tx.Staff().Get(ctx, f.instructor)
		if err != nil {
			return err
		}

		s.Active = false

		return tx.Staff().Update(ctx, s)
	})

	wantLessons(t, f.teachLessons(t, bookingNow, f.principal(t, f.ivy), TeachQuery{}))
}

// bookDirect stores an active booking of person, or of a guest, in l.
func (f *staffFixture) bookDirect(t *testing.T, l domain.Lesson, b domain.Booking) domain.Booking {
	t.Helper()

	b.ID, b.LessonID, b.BookedAt = ids.New[domain.BookingID](f.seq), l.ID, mustInstant(t, bookingNow)
	b.Status = cmp.Or(b.Status, domain.BookingBooked)

	if b.Source == "" {
		b.Source = domain.BookingSourceSelf
		if b.PersonID.IsZero() {
			b.Source = domain.BookingSourceStaff
		}
	}

	if b.Status == domain.BookingCanceled {
		b.CanceledAt = b.BookedAt
	}

	f.write(t, func(ctx context.Context, tx store.Tx) error { return tx.Bookings().Create(ctx, b) })

	return b
}

func TestTeachCountsAndAttendance(t *testing.T) {
	t.Parallel()

	f := newStaffFixture(t)
	l := f.lessonWithPianist(t, 5)
	empty := f.lessonBy(t, "2026-10-22T18:00:00Z", f.instructor, domain.StaffID{})

	anna := f.bookDirect(t, l, domain.Booking{PersonID: f.anna.ID})
	f.bookDirect(t, l, domain.Booking{PersonID: f.mia.ID, Trial: true})
	f.bookDirect(t, l, domain.Booking{GuestName: "Gina Guest", GuestEmail: "gina@example.org", Trial: true})
	f.bookDirect(t, l, domain.Booking{PersonID: f.sam.ID, Status: domain.BookingCanceled})
	f.bookDirect(t, l, domain.Booking{GuestName: "Gone Guest", Status: domain.BookingCanceled})

	joined := mustInstant(t, "2026-10-20T10:00:00Z")
	f.write(t, func(ctx context.Context, tx store.Tx) error {
		return errors.Join(
			tx.Waitlist().Create(ctx, domain.WaitlistEntry{
				ID: ids.New[domain.WaitlistEntryID](f.seq), LessonID: l.ID, PersonID: f.tom.ID,
				Status: domain.WaitlistInvited, JoinedAt: joined, InviteTokenHash: domain.HashToken("tom"),
				InvitedAt: joined, ExpiresAt: mustInstant(t, staffLesson),
			}),
			tx.Waitlist().Create(ctx, domain.WaitlistEntry{
				ID: ids.New[domain.WaitlistEntryID](f.seq), LessonID: l.ID, PersonID: f.sam.ID,
				Status: domain.WaitlistWaiting, JoinedAt: joined.Add(time.Minute),
			}),
		)
	})

	ivy, olga := f.principal(t, f.ivy), f.principal(t, f.olga)

	got := f.teachLessons(t, bookingNow, ivy, TeachQuery{})
	wantLessons(t, got, l, empty)

	tl := got.Lessons[0]
	if tl.Active != 3 || tl.Trials != 2 || tl.Guests != 1 || tl.Held != 1 || tl.Waiting != 1 || tl.FreePlaces != 1 ||
		tl.Lesson.Capacity != 5 || !tl.DayOverAt.Equal(mustInstant(t, staffDayOver)) {
		t.Errorf("counts %+v", tl)
	}

	// Before the start, attendance is neither open nor taken.
	for _, tl := range got.Lessons {
		if tl.AttendanceOpen || tl.AttendanceTaken {
			t.Errorf("before the start: open %v, taken %v", tl.AttendanceOpen, tl.AttendanceTaken)
		}
	}

	attendance := func(now string, p Principal) (bool, bool) {
		t.Helper()

		tl := f.teachLessons(t, now, p, TeachQuery{}).Lessons[0]

		return tl.AttendanceOpen, tl.AttendanceTaken
	}

	// From the start until the day is over for the instructor, at any time
	// after the start for the office.
	for _, tt := range []struct {
		now        string
		p          Principal
		open, done bool
	}{
		{staffLesson, ivy, true, false},
		{staffLate, ivy, true, false},
		{staffDayOver, ivy, false, false},
		{staffDayOver, olga, true, false},
	} {
		if open, done := attendance(tt.now, tt.p); open != tt.open || done != tt.done {
			t.Errorf("at %s as %v: open %v, taken %v; want %v, %v", tt.now, tt.p.Role, open, done, tt.open, tt.done)
		}
	}

	// The lesson without bookings has nothing left to take.
	if e := f.teachLessons(t, staffLate, ivy, TeachQuery{}).Lessons[1]; !e.AttendanceOpen || !e.AttendanceTaken {
		t.Errorf("empty lesson: open %v, taken %v", e.AttendanceOpen, e.AttendanceTaken)
	}

	// Taken once no active booking is left unknown.
	if _, err := f.participants(t, staffStarted).MarkAllAttended(t.Context(), ivy, l.ID); err != nil {
		t.Fatal(err)
	}

	if open, done := attendance(staffStarted, ivy); !open || !done {
		t.Errorf("all attended: open %v, taken %v", open, done)
	}

	// A no-show counts as taken; back to booked does not.
	if _, err := f.participants(t, staffStarted).SetAttendance(t.Context(), ivy, anna.ID, domain.BookingNoShow); err != nil {
		t.Fatal(err)
	}

	if _, done := attendance(staffStarted, ivy); !done {
		t.Error("no-show: not taken")
	}

	if _, err := f.participants(t, staffStarted).SetAttendance(t.Context(), ivy, anna.ID, domain.BookingBooked); err != nil {
		t.Fatal(err)
	}

	if _, done := attendance(staffStarted, ivy); done {
		t.Error("back to booked: taken")
	}

	// A canceled lesson takes no attendance.
	f.write(t, func(ctx context.Context, tx store.Tx) error {
		l.Status = domain.LessonCanceled

		return tx.Lessons().Update(ctx, l)
	})

	if open, done := attendance(staffStarted, olga); open || done {
		t.Errorf("canceled: open %v, taken %v", open, done)
	}
}

// noticeMails returns the newest queued notice of each recipient.
func (f *staffFixture) noticeMails(t *testing.T) map[string]domain.OutboxMail {
	t.Helper()

	out := map[string]domain.OutboxMail{}
	for _, m := range f.mailsOf(t, mail.TemplateLessonNotice) {
		out[m.To] = m
	}

	return out
}

func (f *staffFixture) notices(t *testing.T) []domain.LessonNotice {
	t.Helper()

	var out []domain.LessonNotice

	err := f.st.InTx(t.Context(), store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		accounts, err := tx.Accounts().List(ctx)
		if err != nil {
			return err
		}

		for _, a := range accounts {
			ns, err := tx.Notices().ListBySender(ctx, a.ID, time.Time{})
			if err != nil {
				return err
			}

			out = append(out, ns...)
		}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	return out
}

func TestNotify(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newStaffFixture(t)
	l := f.lessonWithPianist(t, 5)

	// Anna books herself and Mia, who has no account: both mails go to
	// Anna's account, once, in German. Tom has his own. A guest with an
	// email gets it in the school's language, one without and a canceled
	// booking nothing.
	f.bookDirect(t, l, domain.Booking{PersonID: f.anna.ID})
	f.bookDirect(t, l, domain.Booking{PersonID: f.mia.ID})
	f.bookDirect(t, l, domain.Booking{PersonID: f.tom.ID})
	f.bookDirect(t, l, domain.Booking{GuestName: "Gina Guest", GuestEmail: "gina@example.org"})
	f.bookDirect(t, l, domain.Booking{GuestName: "Walter Walk-in"})
	f.bookDirect(t, l, domain.Booking{PersonID: f.sam.ID, Status: domain.BookingCanceled})

	ivy := f.principal(t, f.ivy)

	n, err := f.teach(t, bookingNow).Notify(ctx, ivy, l.ID, "\r\n  Please bring ballet shoes.\r\nSee you!  \n")
	if err != nil || n != 3 {
		t.Fatalf("Notify = %d, %v; want 3", n, err)
	}

	mails := f.noticeMails(t)
	if len(mails) != 3 || len(f.mailsOf(t, mail.TemplateLessonNotice)) != 3 {
		t.Fatalf("notices to %v", mails)
	}

	for to, want := range map[string][]string{
		// Ivy is the lesson's instructor: the mail names her as the
		// schedule does.
		"anna@example.org": {"Hallo Anna", "Ada Example schickt eine Nachricht", "Anna Example, Mia Example"},
		"tom@example.org":  {"Hello Tom", "Ada Example sends a message", "Booked for: Tom Example"},
		"gina@example.org": {"Hello Gina Guest", "Booked for: Gina Guest"},
	} {
		m := mails[to]
		if m.ReplyTo != "ivy@example.net" {
			t.Errorf("%s: reply-to %q", to, m.ReplyTo)
		}

		for _, w := range append(want, "Please bring ballet shoes.\nSee you!\n\n", "Imaginary Ballet") {
			if !strings.Contains(m.Text, w) {
				t.Errorf("%s: no %q in\n%s", to, w, m.Text)
			}
		}
	}

	notices := f.notices(t)
	if len(notices) != 1 || notices[0].LessonID != l.ID || notices[0].SenderID != ivy.AccountID ||
		notices[0].Recipients != 3 || !notices[0].SentAt.Equal(mustInstant(t, bookingNow)) {
		t.Errorf("notices %+v", notices)
	}

	// The accompanist and the office may too, on a canceled lesson as well;
	// other instructors, instructors without a staff link and unknown
	// lessons are 404s.
	if _, err := f.teach(t, bookingNow).Notify(ctx, f.principal(t, f.paul), l.ID, "Hi"); err != nil {
		t.Errorf("accompanist: %v", err)
	}

	if m := f.noticeMails(t)["tom@example.org"]; !strings.Contains(m.Text, "Ben Example sends") ||
		m.ReplyTo != "paul@example.net" {
		t.Errorf("accompanist's notice %+v", m)
	}

	f.write(t, func(ctx context.Context, tx store.Tx) error {
		l.Status = domain.LessonCanceled

		return tx.Lessons().Update(ctx, l)
	})

	if _, err := f.teach(t, bookingNow).Notify(ctx, f.principal(t, f.olga), l.ID, "Sorry!"); err != nil {
		t.Errorf("office: %v", err)
	}

	if m := f.noticeMails(t); len(f.mailsOf(t, mail.TemplateLessonNotice)) != 9 ||
		!strings.Contains(m["tom@example.org"].Text, "The lesson is canceled.") ||
		!strings.Contains(m["tom@example.org"].Text, "Olga Example sends") ||
		m["tom@example.org"].ReplyTo != "olga@example.net" {
		t.Errorf("office notice %+v", m["tom@example.org"])
	}

	for name, p := range map[string]Principal{"other": f.principal(t, f.otto), "no link": f.principal(t, f.nils)} {
		if _, err := f.teach(t, bookingNow).Notify(ctx, p, l.ID, "Hi"); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("%s: want ErrNotFound, got %v", name, err)
		}
	}

	if _, err := f.teach(t, bookingNow).Notify(ctx, ivy, domain.LessonID{1}, "Hi"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("unknown lesson: want ErrNotFound, got %v", err)
	}

	// A lesson nobody is mailed for: no mail, no notice.
	empty := f.lessonBy(t, "2026-10-22T18:00:00Z", f.instructor, domain.StaffID{})
	if n, err := f.teach(t, bookingNow).Notify(ctx, ivy, empty.ID, "Hi"); err != nil || n != 0 {
		t.Errorf("empty lesson: %d, %v", n, err)
	}

	if got := len(f.notices(t)); got != 3 {
		t.Errorf("%d notices, want 3", got)
	}
}

// TestNotifyImpersonated sends as the admin: the sender, the Reply-To and
// the limits are the admin's.
func TestNotifyImpersonated(t *testing.T) {
	t.Parallel()

	f := newStaffFixture(t)
	l := f.lessonWithPianist(t, 5)
	f.bookDirect(t, l, domain.Booking{PersonID: f.tom.ID})

	admin := f.person(t, "Adele", "", "adele@example.net", "")
	f.write(t, func(ctx context.Context, tx store.Tx) error {
		acc, err := tx.Accounts().GetByPerson(ctx, admin.ID)
		if err != nil {
			return err
		}

		acc.Role = domain.RoleAdmin

		return tx.Accounts().Update(ctx, acc)
	})

	p := f.principal(t, f.ivy)
	p.ImpersonatorID = f.principal(t, admin).AccountID

	if _, err := f.teach(t, bookingNow).Notify(t.Context(), p, l.ID, "Hi"); err != nil {
		t.Fatal(err)
	}

	m := f.noticeMails(t)["tom@example.org"]
	if m.ReplyTo != "adele@example.net" || !strings.Contains(m.Text, "Adele Example sends") {
		t.Errorf("notice %+v", m)
	}

	if ns := f.notices(t); len(ns) != 1 || ns[0].SenderID != p.ImpersonatorID {
		t.Errorf("notices %+v", ns)
	}
}

func TestNotifyMessage(t *testing.T) {
	t.Parallel()

	f := newStaffFixture(t)
	l := f.lessonWithPianist(t, 5)
	f.bookDirect(t, l, domain.Booking{PersonID: f.tom.ID})
	svc := f.teach(t, bookingNow)
	ivy := f.principal(t, f.ivy)

	for name, msg := range map[string]string{
		"empty":           "",
		"blank":           " \r\n\t ",
		"too long":        strings.Repeat("ä", MaxNoticeLen+1),
		"NUL":             "Hi\x00there",
		"escape":          "Hi \x1b[31mred",
		"bad UTF-8":       "Hi \xff",
		"bidi":            "Pay \u202eredro\u202c now",
		"isolate":         "Hi \u2066there\u2069",
		"zero-width":      "Hi\u200bthere",
		"BOM":             "\ufeffHi",
		"only separators": "\u2028\u2029",
	} {
		var field *domain.FieldError
		if _, err := svc.Notify(t.Context(), ivy, l.ID, msg); !errors.As(err, &field) || field.Field != "message" {
			t.Errorf("%s: want a field error on message, got %v", name, err)
		}
	}

	// 1000 characters, not bytes, counted after trimming; tabs are fine.
	if _, err := svc.Notify(t.Context(), ivy, l.ID, "\r\n ä\t"+strings.Repeat("ä", MaxNoticeLen-2)+" \r\n"); err != nil {
		t.Errorf("1000 characters: %v", err)
	}

	// Emoji sequences keep their joiners; the Unicode line separators
	// become line breaks.
	if _, err := svc.Notify(t.Context(), ivy, l.ID, "Family day \U0001F468\u200D\U0001F469\u2028Bring them!"); err != nil {
		t.Fatalf("emoji: %v", err)
	}

	if m := f.noticeMails(t)["tom@example.org"]; !strings.Contains(m.Text, "Family day \U0001F468\u200D\U0001F469\nBring them!") {
		t.Errorf("emoji notice:\n%s", m.Text)
	}
}

func TestNotifyLimits(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newStaffFixture(t)

	var lessons []domain.Lesson
	for _, start := range []string{"2026-10-22T16:00:00Z", "2026-10-22T17:00:00Z", "2026-10-23T16:00:00Z"} {
		l := f.lessonBy(t, start, f.instructor, domain.StaffID{})
		f.bookDirect(t, l, domain.Booking{PersonID: f.tom.ID})
		lessons = append(lessons, l)
	}

	ivy, olga := f.principal(t, f.ivy), f.principal(t, f.olga)

	notify := func(now string, p Principal, l domain.Lesson) error {
		t.Helper()

		_, err := f.teach(t, now).Notify(ctx, p, l.ID, "Hi")

		return err
	}

	tooMany := func(err error, retry time.Duration) {
		t.Helper()

		var limited *TooManyNoticesError
		if !errors.As(err, &limited) || limited.RetryAfter != retry {
			t.Fatalf("want TooManyNoticesError retrying after %v, got %v", retry, err)
		}
	}

	// Five notices an hour per account, over several lessons.
	for i, at := range []string{
		"2026-10-21T10:00:00Z", "2026-10-21T10:10:00Z", "2026-10-21T10:20:00Z", "2026-10-21T10:30:00Z",
		"2026-10-21T10:40:00Z",
	} {
		if err := notify(at, ivy, lessons[i%2]); err != nil {
			t.Fatalf("notice %d: %v", i+1, err)
		}
	}

	tooMany(notify("2026-10-21T10:50:00Z", ivy, lessons[2]), 10*time.Minute)
	// The rejected notice does not count: an hour after the first one
	// leaves the window, the next is allowed.
	tooMany(notify("2026-10-21T10:59:59Z", ivy, lessons[2]), time.Second)

	if err := notify("2026-10-21T11:00:01Z", ivy, lessons[2]); err != nil {
		t.Fatalf("after the hour: %v", err)
	}

	// Lesson 0 got three today (10:00, 10:20, 10:40): the office, with a
	// limit of its own, is refused until midnight in Berlin (22:00 UTC).
	tooMany(notify("2026-10-21T12:00:00Z", olga, lessons[0]), 10*time.Hour)

	if err := notify("2026-10-21T12:00:00Z", olga, lessons[2]); err != nil {
		t.Fatalf("office, other lesson: %v", err)
	}

	if err := notify("2026-10-21T22:00:00Z", olga, lessons[0]); err != nil {
		t.Fatalf("next day: %v", err)
	}

	// Both limits: the longer wait.
	for _, at := range []string{"2026-10-21T22:01:00Z", "2026-10-21T22:02:00Z"} {
		if err := notify(at, olga, lessons[0]); err != nil {
			t.Fatal(err)
		}
	}

	if err := notify("2026-10-21T22:03:00Z", olga, lessons[1]); err != nil {
		t.Fatal(err)
	}

	tooMany(notify("2026-10-21T22:04:00Z", olga, lessons[0]), 24*time.Hour-4*time.Minute)

	if err := notify("2026-10-21T22:03:30Z", olga, lessons[2]); err != nil {
		t.Fatal(err)
	}

	// Olga's fifth in the hour: 56 minutes for her, until midnight for the
	// lesson.
	tooMany(notify("2026-10-21T22:04:00Z", olga, lessons[1]), 56*time.Minute)
	tooMany(notify("2026-10-21T22:04:00Z", olga, lessons[0]), 24*time.Hour-4*time.Minute)

	if got := len(f.mailsOf(t, mail.TemplateLessonNotice)); got != 12 {
		t.Errorf("%d notices mailed, want 12", got)
	}
}
