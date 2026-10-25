package service

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/mail"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// The announcement tests run on the booking fixture (Europe/Berlin) from
// bookingNow, Wednesday 2026-10-21, 12:00 in Berlin: Anna (member, German
// mails) is the guardian of Mia (no account, no address) and of Tom (an
// account of his own); Stella is the guardian of Sam; Olga is the office.
// Mia is a member of the Monday class; Sam is booked for a one-off lesson
// on Thursday.

type announcementFixture struct {
	*bookingFixture

	class  domain.Class
	lesson domain.Lesson
	stella domain.Person
}

func newAnnouncementFixture(t *testing.T) *announcementFixture {
	t.Helper()

	f := &announcementFixture{bookingFixture: newBookingFixture(t)}
	f.class = f.addClass(t, time.Monday, domain.TimeOfDay{Hour: 17})
	f.lesson = f.regular(t, staffLesson)
	f.bookFor(t, f.lesson, f.sam)

	f.write(t, func(ctx context.Context, tx store.Tx) error {
		guardians, err := tx.Persons().ListGuardians(ctx, f.sam.ID)
		if err != nil {
			return err
		}

		f.stella = guardians[0]

		return tx.Classes().AddMember(ctx, domain.ClassMember{ClassID: f.class.ID, PersonID: f.mia.ID})
	})

	return f
}

func (f *announcementFixture) service(t *testing.T, now string) *AnnouncementService {
	t.Helper()

	return f.serviceAt(t, mustInstant(t, now))
}

func (f *announcementFixture) serviceAt(t *testing.T, now time.Time) *AnnouncementService {
	t.Helper()

	return NewAnnouncementService(f.st, clock.NewFixed(now), f.seq, newTestRenderer(t), nil)
}

// sendDue runs the announcements job at now and returns how many mails it
// queued, failing the test on error.
func (f *announcementFixture) sendDue(t *testing.T, svc *AnnouncementService) int {
	t.Helper()

	n, err := svc.SendDue(t.Context())
	if err != nil {
		t.Fatalf("SendDue: %v", err)
	}

	return n
}

// get returns the stored announcement.
func (f *announcementFixture) get(t *testing.T, id domain.AnnouncementID) domain.Announcement {
	t.Helper()

	var a domain.Announcement

	f.write(t, func(ctx context.Context, tx store.Tx) error {
		var err error
		a, err = tx.Announcements().Get(ctx, id)

		return err
	})

	return a
}

// create publishes an announcement as Olga, failing the test on error.
func (f *announcementFixture) create(t *testing.T, svc *AnnouncementService, in AnnouncementInput) domain.Announcement {
	t.Helper()

	e, err := svc.Create(t.Context(), f.principal(t, f.olga), in)
	if err != nil {
		t.Fatalf("create %q: %v", in.Title, err)
	}

	return e.Announcement
}

func titles(vs []AnnouncementView) []string {
	out := make([]string, len(vs))
	for i, v := range vs {
		out[i] = v.Announcement.Title
		if v.Dismissed {
			out[i] += " (dismissed)"
		}
	}

	return out
}

func audience(kind domain.Audience) AnnouncementAudience { return AnnouncementAudience{Audience: kind} }

func TestAnnouncementReach(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newAnnouncementFixture(t)
	svc := f.service(t, bookingNow)
	now := mustInstant(t, bookingNow)

	in := func(title string, aud AnnouncementAudience, publish time.Time) AnnouncementInput {
		return AnnouncementInput{AnnouncementAudience: aud, Title: title, Body: "Hello.", PublishAt: publish}
	}

	f.create(t, svc, in("Everyone", audience(domain.AudienceEveryone), now.Add(-time.Hour)))
	f.create(t, svc, in("Office", AnnouncementAudience{Audience: domain.AudienceRole, Role: domain.RoleOffice}, now.Add(-2*time.Hour)))
	f.create(t, svc, in("Class", AnnouncementAudience{Audience: domain.AudienceClass, ClassID: f.class.ID}, now.Add(-3*time.Hour)))
	f.create(t, svc, in("Course type", AnnouncementAudience{Audience: domain.AudienceCourseType, CourseTypeID: f.courseType}, now.Add(-4*time.Hour)))
	f.create(t, svc, in("Lesson", AnnouncementAudience{Audience: domain.AudienceLesson, LessonID: f.lesson.ID}, now.Add(-5*time.Hour)))
	f.create(t, svc, in("Later", audience(domain.AudienceEveryone), now.Add(time.Minute)))

	// Written while it was current.
	expired := in("Expired", audience(domain.AudienceEveryone), now.Add(-48*time.Hour))
	expired.ExpiresAt = now
	f.create(t, f.serviceAt(t, now.Add(-48*time.Hour)), expired)

	for name, tc := range map[string]struct {
		person domain.Person
		want   []string
	}{
		// Through her daughter, a member of the class of the course type.
		"anna": {f.anna, []string{"Everyone", "Class", "Course type"}},
		"olga": {f.olga, []string{"Everyone", "Office"}},
		// Through Sam, booked for the lesson of the course type.
		"stella": {f.stella, []string{"Everyone", "Course type", "Lesson"}},
		// A dependent's own account does not see what its guardian does.
		"tom": {f.tom, []string{"Everyone"}},
	} {
		got, err := svc.ForAccount(ctx, f.principal(t, tc.person))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}

		if !slices.Equal(titles(got), tc.want) {
			t.Errorf("%s sees %q, want %q", name, titles(got), tc.want)
		}
	}

	// Once the lesson is over, Sam's booking no longer counts for the course
	// type, but still for the lesson.
	later := f.serviceAt(t, mustInstant(t, staffDayOver))

	got, err := later.ForAccount(ctx, f.principal(t, f.stella))
	if err != nil {
		t.Fatal(err)
	}

	if want := []string{"Later", "Everyone", "Lesson"}; !slices.Equal(titles(got), want) {
		t.Errorf("stella sees %q later, want %q", titles(got), want)
	}
}

func TestAnnouncementEmail(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newAnnouncementFixture(t)
	now := mustInstant(t, bookingNow)
	wakes := 0
	svc := NewAnnouncementService(f.st, clock.NewFixed(now), f.seq, newTestRenderer(t), func() { wakes++ })

	a := f.create(t, svc, AnnouncementInput{
		AnnouncementAudience: AnnouncementAudience{Audience: domain.AudienceClass, ClassID: f.class.ID},
		Title:                "Costume fitting", Body: "Please come on Saturday.\nBring the shoes.", Email: true,
	})

	// Saving sends nothing, but wakes the job, which sends it.
	if !a.EmailedAt.IsZero() || len(f.mailsOf(t, mail.TemplateAnnouncement)) != 0 || wakes != 1 {
		t.Fatalf("after create: emailed at %v, %d wakes; want nothing sent and one wake", a.EmailedAt, wakes)
	}

	if n := f.sendDue(t, svc); n != 1 {
		t.Fatalf("SendDue queued %d mails, want 1", n)
	}

	if a = f.get(t, a.ID); !a.EmailedAt.Equal(now) || a.EmailRecipients != 1 {
		t.Fatalf("emailed at %v to %d, want now to 1", a.EmailedAt, a.EmailRecipients)
	}

	// Mia has no account and no address: her guardian Anna gets it, in
	// German, for Mia.
	mails := f.mailsOf(t, mail.TemplateAnnouncement)
	if len(mails) != 1 || mails[0].To != "anna@example.org" {
		t.Fatalf("mails = %+v, want one to Anna", mails)
	}

	for _, want := range []string{"Hallo Anna", "für Mia Example", "Costume fitting", "Bring the shoes."} {
		if !strings.Contains(mails[0].Text, want) {
			t.Errorf("mail does not contain %q:\n%s", want, mails[0].Text)
		}
	}

	// An edit does not mail again, even with email off.
	if _, err := svc.Update(ctx, a.ID, AnnouncementInput{
		AnnouncementAudience: audience(domain.AudienceEveryone), Title: "Costume fitting", Body: "Changed.",
	}); err != nil {
		t.Fatal(err)
	}

	if n := f.sendDue(t, svc); n != 0 || len(f.mailsOf(t, mail.TemplateAnnouncement)) != 1 || wakes != 1 {
		t.Errorf("after the edit: the job queued %d, %d wakes; want nothing more", n, wakes)
	}

	list, err := svc.List(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if len(list) != 1 || !list[0].Announcement.Email || list[0].Announcement.EmailRecipients != 1 ||
		list[0].Announcement.Body != "Changed." || list[0].Author.GivenName != "Olga" {
		t.Errorf("list = %+v", list)
	}

	// For everyone: every confirmed account, each in its language.
	f.create(t, svc, AnnouncementInput{
		AnnouncementAudience: audience(domain.AudienceEveryone), Title: "Hello all", Body: "Hi.", Email: true,
	})
	f.sendDue(t, svc)

	var to []string

	for _, m := range f.mailsOf(t, mail.TemplateAnnouncement)[1:] {
		to = append(to, m.To)
	}

	slices.Sort(to)

	want := []string{"anna@example.org", "olga@example.net", "stella@example.org", "tom@example.org"}
	if !slices.Equal(to, want) {
		t.Errorf("mailed to %v, want %v", to, want)
	}
}

func TestAnnouncementScheduledEmail(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newAnnouncementFixture(t)
	now := mustInstant(t, bookingNow)
	publish := now.Add(time.Hour)

	wakes := 0
	scheduler := NewAnnouncementService(f.st, clock.NewFixed(now), f.seq, newTestRenderer(t), func() { wakes++ })

	a := f.create(t, scheduler, AnnouncementInput{
		AnnouncementAudience: AnnouncementAudience{Audience: domain.AudienceLesson, LessonID: f.lesson.ID},
		Title:                "Workshop", Body: "Room changed.", PublishAt: publish, Email: true,
	})
	if !a.EmailedAt.IsZero() || wakes != 0 {
		t.Fatalf("a scheduled announcement: emailed at %v, %d wakes; want neither", a.EmailedAt, wakes)
	}

	for _, at := range []time.Time{now, publish.Add(-time.Microsecond)} {
		if n, err := f.serviceAt(t, at).SendDue(ctx); err != nil || n != 0 {
			t.Fatalf("SendDue at %v = %d, %v; want nothing yet", at, n, err)
		}
	}

	for range 2 {
		n, err := f.serviceAt(t, publish).SendDue(ctx)
		if err != nil {
			t.Fatal(err)
		}

		if got := len(f.mailsOf(t, mail.TemplateAnnouncement)); got != 1 {
			t.Fatalf("%d mails (job sent %d), want exactly one", got, n)
		}
	}

	if m := f.mailsOf(t, mail.TemplateAnnouncement)[0]; m.To != "stella@example.org" {
		t.Errorf("mailed to %s, want Sam's guardian", m.To)
	}

	// One that expires before the job sends it is marked, not mailed.
	b := f.create(t, f.serviceAt(t, now), AnnouncementInput{
		AnnouncementAudience: audience(domain.AudienceEveryone), Title: "Short", Body: "Brief.",
		PublishAt: publish, ExpiresAt: publish.Add(time.Minute), Email: true,
	})

	if n, err := f.serviceAt(t, publish.Add(time.Hour)).SendDue(ctx); err != nil || n != 0 {
		t.Fatalf("SendDue = %d, %v; want nothing for an expired one", n, err)
	}

	got, err := f.serviceAt(t, now).List(ctx)
	if err != nil {
		t.Fatal(err)
	}

	for _, e := range got {
		if e.Announcement.ID == b.ID && (e.Announcement.EmailedAt.IsZero() || e.Announcement.EmailRecipients != 0) {
			t.Errorf("expired announcement: %+v", e.Announcement)
		}
	}
}

// TestAnnouncementSendsInBatches: a run of the job queues at most a batch of
// mails; the next goes on where it stopped, by address. An edit while it is
// being sent keeps it being sent, and a deletion records what went out.
func TestAnnouncementSendsInBatches(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newAnnouncementFixture(t)
	now := mustInstant(t, bookingNow)
	svc := f.serviceAt(t, now)
	svc.recipientBatch = 3

	all := f.create(t, svc, AnnouncementInput{
		AnnouncementAudience: audience(domain.AudienceEveryone), Title: "Hello all", Body: "Hi.", Email: true,
	})
	members := f.create(t, svc, AnnouncementInput{
		AnnouncementAudience: AnnouncementAudience{Audience: domain.AudienceRole, Role: domain.RoleMember},
		Title:                "Members", Body: "Hi.", Email: true,
	})

	to := func() []string {
		var out []string
		for _, m := range f.mailsOf(t, mail.TemplateAnnouncement) {
			out = append(out, strings.TrimPrefix(m.Subject, "Studio Example: ")+" → "+m.To)
		}

		return out
	}

	if n := f.sendDue(t, svc); n != 3 {
		t.Fatalf("first run queued %d, want 3", n)
	}

	a := f.get(t, all.ID)
	if !a.EmailedAt.IsZero() || a.EmailRecipients != 3 || a.MailCursor != "stella@example.org" {
		t.Fatalf("after the first run: %+v", a)
	}

	// An edit while it is being sent: email stays on, and so does the
	// state of the mailing. The rest get the new text.
	if _, err := svc.Update(ctx, all.ID, AnnouncementInput{
		AnnouncementAudience: audience(domain.AudienceEveryone), Title: "Hello all", Body: "Changed.",
	}); err != nil {
		t.Fatal(err)
	}

	if a = f.get(t, all.ID); !a.Email || a.EmailRecipients != 3 || a.MailCursor != "stella@example.org" {
		t.Fatalf("after the edit: %+v", a)
	}

	if n := f.sendDue(t, svc); n != 3 {
		t.Fatalf("second run queued %d, want 3", n)
	}

	if a = f.get(t, all.ID); !a.EmailedAt.Equal(now) || a.EmailRecipients != 4 || a.MailCursor != "" {
		t.Fatalf("after the second run: %+v", a)
	}

	if m := f.get(t, members.ID); m.EmailRecipients != 2 || m.MailCursor != "stella@example.org" {
		t.Fatalf("members after the second run: %+v", m)
	}

	mails := f.mailsOf(t, mail.TemplateAnnouncement)
	if last := mails[3]; last.To != "tom@example.org" || !strings.Contains(last.Text, "Changed.") {
		t.Errorf("the fourth mail: %s %q, want the changed text to Tom", last.To, last.Text)
	}

	// Deleting one being sent stops it; what went out keeps its place.
	if err := svc.Delete(ctx, members.ID); err != nil {
		t.Fatal(err)
	}

	if n := f.sendDue(t, svc); n != 0 {
		t.Errorf("third run queued %d, want nothing", n)
	}

	want := []string{
		"Hello all → anna@example.org", "Hello all → olga@example.net", "Hello all → stella@example.org",
		"Hello all → tom@example.org", "Members → anna@example.org", "Members → stella@example.org",
	}
	if got := to(); !slices.Equal(got, want) {
		t.Errorf("mailed %q, want %q", got, want)
	}

	var mailings []domain.AnnouncementMailing

	f.write(t, func(ctx context.Context, tx store.Tx) error {
		var err error
		mailings, err = tx.Announcements().ListMailings(ctx, now.Add(-time.Hour), now.Add(time.Hour))

		return err
	})

	if len(mailings) != 2 || mailings[0].Recipients+mailings[1].Recipients != 6 {
		t.Errorf("mailings %+v, want 4 and 2 recipients", mailings)
	}
}

func TestAnnouncementMailLimit(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newAnnouncementFixture(t)
	now := mustInstant(t, bookingNow)
	svc := f.serviceAt(t, now)
	emailed := func(title string, publish time.Time) AnnouncementInput {
		return AnnouncementInput{
			AnnouncementAudience: audience(domain.AudienceEveryone), Title: title, Body: "Hi.", PublishAt: publish,
			Email: true,
		}
	}

	// Two sent today and one scheduled for this evening fill the day.
	first := f.create(t, svc, emailed("One", time.Time{}))
	f.create(t, svc, emailed("Two", time.Time{}))
	f.sendDue(t, svc)
	f.create(t, svc, emailed("Three", now.Add(8*time.Hour)))

	var limited *TooManyAnnouncementMailsError

	_, err := svc.Create(ctx, f.principal(t, f.olga), emailed("Four", time.Time{}))
	if !errors.As(err, &limited) || limited.Day != mustDate(t, "2026-10-21") {
		t.Fatalf("fourth = %v, want the limit of 2026-10-21", err)
	}

	// Deleting a sent one does not free its place.
	if err := svc.Delete(ctx, first.ID); err != nil {
		t.Fatal(err)
	}

	if _, err := svc.Create(ctx, f.principal(t, f.olga), emailed("Four", time.Time{})); !errors.As(err, &limited) {
		t.Fatalf("after deleting a sent one = %v, want the limit", err)
	}

	// Without email, or on another day, it goes.
	f.create(t, svc, AnnouncementInput{AnnouncementAudience: audience(domain.AudienceEveryone), Title: "In app", Body: "Hi."})
	// Thursday starts at 22:00 UTC in Berlin.
	f.create(t, svc, emailed("Tomorrow", mustInstant(t, "2026-10-21T22:00:00Z")))
}

func TestAnnouncementDismiss(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newAnnouncementFixture(t)
	svc := f.service(t, bookingNow)
	all := f.create(t, svc, AnnouncementInput{AnnouncementAudience: audience(domain.AudienceEveryone), Title: "All", Body: "Hi."})
	office := f.create(t, svc, AnnouncementInput{
		AnnouncementAudience: AnnouncementAudience{Audience: domain.AudienceRole, Role: domain.RoleOffice},
		Title:                "Office", Body: "Hi.",
	})

	anna := f.principal(t, f.anna)

	// While an admin impersonates Anna, nothing is stored.
	impersonated := anna
	impersonated.ImpersonatorID = f.principal(t, f.olga).AccountID

	if err := svc.Dismiss(ctx, impersonated, all.ID); err != nil {
		t.Fatal(err)
	}

	got, _ := svc.ForAccount(ctx, anna)
	if want := []string{"All"}; !slices.Equal(titles(got), want) {
		t.Fatalf("after an impersonated dismissal: %q", titles(got))
	}

	if err := svc.Dismiss(ctx, anna, all.ID); err != nil {
		t.Fatal(err)
	}

	got, _ = svc.ForAccount(ctx, anna)
	if want := []string{"All (dismissed)"}; !slices.Equal(titles(got), want) {
		t.Errorf("after dismissing: %q", titles(got))
	}

	// Per account: Olga still has it.
	got, _ = svc.ForAccount(ctx, f.principal(t, f.olga))
	if want := []string{"Office", "All"}; !slices.Equal(titles(got), want) {
		t.Errorf("olga: %q", titles(got))
	}

	// What does not reach Anna, or does not exist, is not found.
	for _, id := range []domain.AnnouncementID{office.ID, {7}} {
		if err := svc.Dismiss(ctx, anna, id); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("dismiss %v = %v, want not found", id, err)
		}
	}
}

func TestAnnouncementInput(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newAnnouncementFixture(t)
	now := mustInstant(t, bookingNow)
	svc := f.serviceAt(t, now)
	olga := f.principal(t, f.olga)
	valid := AnnouncementInput{AnnouncementAudience: audience(domain.AudienceEveryone), Title: "Title", Body: "Text"}

	for field, modify := range map[string]func(*AnnouncementInput){
		"title":    func(in *AnnouncementInput) { in.Title = " \n " },
		"body":     func(in *AnnouncementInput) { in.Body = strings.Repeat("x", domain.MaxAnnouncementBodyLen+1) },
		"audience": func(in *AnnouncementInput) { in.Audience = "world" },
		"role":     func(in *AnnouncementInput) { in.Audience = domain.AudienceRole },
		"classId":  func(in *AnnouncementInput) { in.Audience, in.ClassID = domain.AudienceClass, domain.ClassID{9} },
		"courseTypeId": func(in *AnnouncementInput) {
			in.Audience, in.CourseTypeID = domain.AudienceCourseType, domain.CourseTypeID{9}
		},
		"lessonId":  func(in *AnnouncementInput) { in.Audience, in.LessonID = domain.AudienceLesson, domain.LessonID{9} },
		"expiresAt": func(in *AnnouncementInput) { in.ExpiresAt = now.Add(-time.Second) },
	} {
		in := valid
		modify(&in)

		_, err := svc.Create(ctx, olga, in)
		wantFieldError(t, err, field)
	}

	// Published in the past and expired already: it would never show, and
	// must not take a place in the daily limit.
	in := valid
	in.PublishAt, in.ExpiresAt, in.Email = now.Add(-2*time.Hour), now, true
	_, err := svc.Create(ctx, olga, in)
	wantFieldError(t, err, "expiresAt")

	// Two lines in the title, and a bidi override in the text.
	in = valid
	in.Title = "Two\nlines"
	_, err = svc.Create(ctx, olga, in)
	wantFieldError(t, err, "title")

	in = valid
	in.Body = "Hi \u202e there"
	_, err = svc.Create(ctx, olga, in)
	wantFieldError(t, err, "body")

	// The text is normalized; the author is the admin while impersonating.
	in = valid
	in.Body = "  Line one\r\nLine two  "
	impersonating := f.principal(t, f.anna)
	impersonating.ImpersonatorID = olga.AccountID

	e, err := svc.Create(ctx, impersonating, in)
	if err != nil {
		t.Fatal(err)
	}

	if e.Announcement.Body != "Line one\nLine two" || e.Announcement.AuthorID != olga.AccountID ||
		!e.Announcement.PublishAt.Equal(now) {
		t.Errorf("created %+v", e.Announcement)
	}

	// An expired announcement is not edited, but deleted.
	later := f.serviceAt(t, now.Add(time.Hour))
	in.ExpiresAt = now.Add(time.Minute)

	if _, err := svc.Update(ctx, e.Announcement.ID, in); err != nil {
		t.Fatal(err)
	}

	if _, err := later.Update(ctx, e.Announcement.ID, in); !errors.Is(err, ErrAnnouncementExpired) {
		t.Errorf("edit expired = %v, want ErrAnnouncementExpired", err)
	}

	if err := later.Delete(ctx, e.Announcement.ID); err != nil {
		t.Errorf("delete expired: %v", err)
	}

	if _, err := later.Update(ctx, e.Announcement.ID, in); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("edit deleted = %v, want not found", err)
	}
}

func TestAnnouncementCountRecipients(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newAnnouncementFixture(t)
	svc := f.service(t, bookingNow)

	for name, tc := range map[string]struct {
		aud  AnnouncementAudience
		want int
	}{
		"everyone":    {audience(domain.AudienceEveryone), 4},
		"members":     {AnnouncementAudience{Audience: domain.AudienceRole, Role: domain.RoleMember}, 3},
		"admins":      {AnnouncementAudience{Audience: domain.AudienceRole, Role: domain.RoleAdmin}, 0},
		"class":       {AnnouncementAudience{Audience: domain.AudienceClass, ClassID: f.class.ID}, 1},
		"course type": {AnnouncementAudience{Audience: domain.AudienceCourseType, CourseTypeID: f.courseType}, 2},
		"lesson":      {AnnouncementAudience{Audience: domain.AudienceLesson, LessonID: f.lesson.ID}, 1},
	} {
		n, err := svc.CountRecipients(ctx, tc.aud)
		if err != nil || n != tc.want {
			t.Errorf("%s: %d, %v; want %d", name, n, err, tc.want)
		}
	}

	_, err := svc.CountRecipients(ctx, AnnouncementAudience{Audience: domain.AudienceClass})
	wantFieldError(t, err, "classId")

	_, err = svc.CountRecipients(ctx, AnnouncementAudience{Audience: domain.AudienceLesson, LessonID: domain.LessonID{9}})
	wantFieldError(t, err, "lessonId")
}

// TestAnnouncementReferences: a course type an announcement is for stays
// (the contract tests cover the class and the lesson).
func TestAnnouncementReferences(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newAnnouncementFixture(t)
	svc := f.service(t, bookingNow)
	f.create(t, svc, AnnouncementInput{
		AnnouncementAudience: AnnouncementAudience{Audience: domain.AudienceCourseType, CourseTypeID: f.courseType},
		Title:                "Type", Body: "Hi.",
	})

	err := f.st.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		return tx.CourseTypes().Delete(ctx, f.courseType)
	})
	if !errors.Is(err, domain.ErrConflict) {
		t.Errorf("delete the course type = %v, want a conflict", err)
	}
}
