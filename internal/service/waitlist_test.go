package service

import (
	"context"
	"errors"
	"fmt"
	"regexp"
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

// testAppURL is the app URL of the test mails.
const testAppURL = "https://school.example.org/"

// testMails sends mails rendered by r, with links into testAppURL.
func testMails(r MailRenderer) Mails { return Mails{Renderer: r, AppURL: testAppURL} }

// newTestRenderer returns the embedded mail templates.
func newTestRenderer(t *testing.T) *mail.Renderer {
	t.Helper()

	r, err := mail.NewRenderer()
	if err != nil {
		t.Fatal(err)
	}

	return r
}

// The waiting-list tests run on the booking fixture (Europe/Berlin, booking
// cutoff 1 h, invites valid for 12 h) from bookingNow, Wednesday 2026-10-21,
// 12:00 in Berlin. Their lesson is a one-off for one person on Friday
// 2026-10-23 at 18:00 (16:00 UTC), whose booking closes at 15:00 UTC; Stella,
// a stranger, has its place.

const waitlistLesson = "2026-10-23T16:00:00Z"

var inviteLink = regexp.MustCompile(`https://school\.example\.org/waitlist/accept\?token=([A-Za-z0-9_-]+)`)

func (f *bookingFixture) waitlistService(t *testing.T, now string) *WaitlistService {
	t.Helper()

	return NewWaitlistService(f.st, clock.NewFixed(mustInstant(t, now)), f.seq, testMails(newTestRenderer(t)))
}

// stella is the stranger who booked the waiting-list lesson.
func (f *bookingFixture) stella(t *testing.T) domain.Person {
	t.Helper()

	var p domain.Person

	err := f.st.InTx(t.Context(), store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		acc, err := tx.Accounts().GetByEmail(ctx, "stella@example.org")
		if err != nil {
			return err
		}

		p, err = tx.Persons().Get(ctx, acc.PersonID)

		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	return p
}

// fullLesson stores the waiting-list lesson with capacity places, all
// booked by Stella and as many new persons as needed.
func (f *bookingFixture) fullLesson(t *testing.T, capacity int) domain.Lesson {
	t.Helper()

	l := f.oneOff(t, waitlistLesson, capacity, domain.VisibilityPublic, domain.LessonWorkshop)
	f.bookAs(t, f.stella(t), l)

	for i := 1; i < capacity; i++ {
		f.bookAs(t, f.person(t, fmt.Sprintf("Filler%d", i), "", "", ""), l)
	}

	return l
}

func (f *bookingFixture) entries(t *testing.T, l domain.Lesson) []domain.WaitlistEntry {
	t.Helper()

	var out []domain.WaitlistEntry

	err := f.st.InTx(t.Context(), store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		var err error
		out, err = tx.Waitlist().ListByLesson(ctx, l.ID)

		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	return out
}

func (f *bookingFixture) entry(t *testing.T, id domain.WaitlistEntryID) domain.WaitlistEntry {
	t.Helper()

	var e domain.WaitlistEntry

	err := f.st.InTx(t.Context(), store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		var err error
		e, err = tx.Waitlist().Get(ctx, id)

		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	return e
}

// mailsOf returns the queued mails of template, oldest first.
func (f *bookingFixture) mailsOf(t *testing.T, template string) []domain.OutboxMail {
	t.Helper()

	return slices.DeleteFunc(f.mails(t), func(m domain.OutboxMail) bool { return m.Template != template })
}

// token returns the invite token of the newest waitlist-invite mail to.
func (f *bookingFixture) token(t *testing.T, to string) string {
	t.Helper()

	invites := f.mailsOf(t, mail.TemplateWaitlistInvite)
	for i := len(invites) - 1; i >= 0; i-- {
		if invites[i].To == to {
			m := inviteLink.FindStringSubmatch(invites[i].Text)
			if m == nil {
				t.Fatalf("no link in %q", invites[i].Text)
			}

			return m[1]
		}
	}

	t.Fatalf("no invite to %s", to)

	return ""
}

// join puts person on l's list as p at now.
func (f *bookingFixture) join(
	t *testing.T, now string, p Principal, l domain.Lesson, person domain.Person, stay bool,
) WaitlistPlace {
	t.Helper()

	place, err := f.waitlistService(t, now).Join(t.Context(), p, l.ID, person.ID, stay)
	if err != nil {
		t.Fatalf("join %s: %v", person.GivenName, err)
	}

	return place
}

// cancelStella cancels Stella's booking of l at now, which frees its place.
func (f *bookingFixture) cancelStella(t *testing.T, now string, l domain.Lesson) {
	t.Helper()

	stella := f.stella(t)
	if _, err := f.bookingService(t, now).Cancel(t.Context(), f.principal(t, stella), f.bookingIn(t, l, stella).ID); err != nil {
		t.Fatal(err)
	}
}

func wantStatus(t *testing.T, e domain.WaitlistEntry, want domain.WaitlistStatus) {
	t.Helper()

	if e.Status != want {
		t.Fatalf("entry %+v: status %s, want %s", e, e.Status, want)
	}
}

func TestWaitlistJoin(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newBookingFixture(t)
	anna := f.principal(t, f.anna)
	l := f.fullLesson(t, 1)
	svc := f.waitlistService(t, bookingNow)

	mia, err := svc.Join(ctx, anna, l.ID, f.mia.ID, false)
	if err != nil {
		t.Fatal(err)
	}

	if e := mia.Entry; e.Status != domain.WaitlistWaiting || e.PersonID != f.mia.ID || e.LessonID != l.ID ||
		e.StayOnList || e.JoinedBy != anna.AccountID || !e.JoinedAt.Equal(mustInstant(t, bookingNow)) ||
		mia.Position != 1 || f.entry(t, e.ID) != e {
		t.Fatalf("Mia's place %+v", mia)
	}

	me := f.join(t, "2026-10-21T10:01:00Z", anna, l, f.anna, true)
	if me.Position != 2 || !me.Entry.StayOnList {
		t.Fatalf("Anna's place %+v", me)
	}

	if mails := f.mails(t); len(mails) != 0 {
		t.Errorf("joining mailed %+v", mails)
	}

	for name, tt := range map[string]struct {
		p      Principal
		lesson domain.LessonID
		person domain.PersonID
		want   error
	}{
		"twice":          {anna, l.ID, f.mia.ID, ErrAlreadyWaiting},
		"booked":         {f.principal(t, f.stella(t)), l.ID, f.stella(t).ID, ErrAlreadyBooked},
		"stranger":       {anna, l.ID, f.sam.ID, domain.ErrNotFound},
		"unknown lesson": {anna, ids.New[domain.LessonID](f.seq), f.tom.ID, domain.ErrNotFound},
		"not full":       {anna, f.regular(t, waitlistLesson).ID, f.tom.ID, ErrNotFull},
	} {
		if _, err := svc.Join(ctx, tt.p, tt.lesson, tt.person, false); !errors.Is(err, tt.want) {
			t.Errorf("%s: want %v, got %v", name, tt.want, err)
		}
	}

	// Closed lessons say why, like booking.
	_, err = f.waitlistService(t, "2026-10-23T15:00:00Z").Join(ctx, anna, l.ID, f.tom.ID, false)
	wantClosed(t, err, ClosedCutoff)

	canceled := f.fullLesson(t, 1)
	if _, err := f.lessonService(t, bookingNow).CancelLesson(ctx, f.principal(t, f.olga), canceled.ID, ""); err != nil {
		t.Fatal(err)
	}

	_, err = svc.Join(ctx, anna, canceled.ID, f.tom.ID, false)
	wantClosed(t, err, ClosedCanceled)

	if got := f.entries(t, l); len(got) != 2 {
		t.Errorf("entries %+v, want Mia's and Anna's", got)
	}
}

// TestWaitlistInvite frees the place: the oldest entry is invited by mail
// to its recipients, and its invite reserves the place against bookings and
// in the schedule until it is accepted through the link.
func TestWaitlistInvite(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newBookingFixture(t)
	anna := f.principal(t, f.anna)
	l := f.fullLesson(t, 1)
	mia := f.join(t, bookingNow, anna, l, f.mia, false)
	me := f.join(t, "2026-10-21T10:01:00Z", anna, l, f.anna, true)

	f.cancelStella(t, "2026-10-21T12:00:00Z", l)

	invited := f.entry(t, mia.Entry.ID)
	wantStatus(t, invited, domain.WaitlistInvited)

	// 12 hours, well before the booking closes.
	if !invited.InvitedAt.Equal(mustInstant(t, "2026-10-21T12:00:00Z")) ||
		!invited.ExpiresAt.Equal(mustInstant(t, "2026-10-22T00:00:00Z")) {
		t.Fatalf("invite %+v", invited)
	}

	wantStatus(t, f.entry(t, me.Entry.ID), domain.WaitlistWaiting)

	// Mia has neither a login nor an address: her guardian Anna is mailed,
	// in German, with the link and the expiry in school time.
	invites := f.mailsOf(t, mail.TemplateWaitlistInvite)
	if len(invites) != 1 || invites[0].To != "anna@example.org" ||
		!strings.Contains(invites[0].Text, "reserviert bis Donnerstag, 22. Oktober 2026, 02:00 Uhr") ||
		!strings.Contains(invites[0].Text, "Mia Example") {
		t.Fatalf("invites %+v", invites)
	}

	token := f.token(t, "anna@example.org")
	if domain.HashToken(token) != invited.InviteTokenHash {
		t.Fatal("the mailed token does not match the stored hash")
	}

	t.Run("reserved", func(t *testing.T) {
		_, err := f.bookingService(t, "2026-10-21T13:00:00Z").Book(ctx, f.principal(t, f.tom), l.ID, f.tom.ID)
		if !errors.Is(err, ErrLessonFull) {
			t.Fatalf("book the reserved place: want ErrLessonFull, got %v", err)
		}

		sl, err := NewScheduleService(f.st, clock.NewFixed(mustInstant(t, "2026-10-21T13:00:00Z"))).Lesson(ctx, anna, l.ID)
		if err != nil {
			t.Fatal(err)
		}

		if sl.FreePlaces != 0 || len(sl.Waiting) != 2 || sl.Waiting[0].Person.ID != f.anna.ID ||
			sl.Waiting[0].Position != 1 || sl.Waiting[0].Status != domain.WaitlistWaiting ||
			sl.Waiting[1].Person.ID != f.mia.ID || sl.Waiting[1].Status != domain.WaitlistInvited ||
			sl.Waiting[1].Position != 0 {
			t.Fatalf("lesson for Anna: free %d, waiting %+v", sl.FreePlaces, sl.Waiting)
		}

		// A guest sees no entries.
		guest, err := NewScheduleService(f.st, clock.NewFixed(mustInstant(t, "2026-10-21T13:00:00Z"))).Lesson(ctx, Principal{}, l.ID)
		if err != nil || guest.FreePlaces != 0 || guest.Waiting != nil {
			t.Fatalf("lesson for a guest: %+v, %v", guest, err)
		}
	})

	t.Run("view", func(t *testing.T) {
		v, err := f.waitlistService(t, "2026-10-21T13:00:00Z").Invite(ctx, token)
		if err != nil {
			t.Fatal(err)
		}

		if v.State != InviteOpen || v.Person.ID != f.mia.ID || v.Lesson.Lesson.ID != l.ID || v.Closed != "" ||
			v.Entry.ID != mia.Entry.ID {
			t.Fatalf("view %+v", v)
		}

		if _, err := f.waitlistService(t, "2026-10-21T13:00:00Z").Invite(ctx, "no-such-token"); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("unknown token: want ErrNotFound, got %v", err)
		}
	})

	t.Run("accept by link", func(t *testing.T) {
		svc := f.waitlistService(t, "2026-10-21T14:00:00Z")

		v, err := svc.AcceptInvite(ctx, token)
		if err != nil {
			t.Fatal(err)
		}

		if v.State != InviteAccepted {
			t.Fatalf("view after accepting %+v", v)
		}

		wantStatus(t, f.entry(t, mia.Entry.ID), domain.WaitlistAccepted)

		b, events := f.booking(t, f.bookingIn(t, l, f.mia).ID)
		if b.Source != domain.BookingSourceSelf || b.Status != domain.BookingBooked ||
			len(events) != 1 || events[0].Note != waitlistNote || !events[0].ActorAccountID.IsZero() {
			t.Fatalf("booking %+v, events %+v", b, events)
		}

		confirmed := f.mailsOf(t, mail.TemplateBookingConfirmed)
		if len(confirmed) != 1 || confirmed[0].To != "anna@example.org" ||
			!strings.Contains(confirmed[0].Text, "Gebucht für: Mia Example") {
			t.Fatalf("confirmations %+v", confirmed)
		}

		// Single use.
		if _, err := svc.AcceptInvite(ctx, token); !errors.Is(err, ErrInviteUsed) {
			t.Fatalf("again: want ErrInviteUsed, got %v", err)
		}

		if v, err := svc.Invite(ctx, token); err != nil || v.State != InviteAccepted {
			t.Fatalf("view: %+v, %v", v, err)
		}

		if _, err := svc.AcceptInvite(ctx, "no-such-token"); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("unknown token: want ErrNotFound, got %v", err)
		}

		// The lesson is full again, and Anna still waits.
		wantStatus(t, f.entry(t, me.Entry.ID), domain.WaitlistWaiting)

		if n := len(f.mailsOf(t, mail.TemplateWaitlistInvite)); n != 1 {
			t.Errorf("%d invites, want 1", n)
		}
	})
}

// TestWaitlistExpiry lets invites run out: the job ends them (with a mail),
// sends the stay-on-list entry to the end of the list, invites the next,
// and does nothing on a second run. An invite never outlasts the booking
// cutoff, and one that ran out holds no place even before the job.
func TestWaitlistExpiry(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newBookingFixture(t)
	anna := f.principal(t, f.anna)
	tom := f.principal(t, f.tom)
	l := f.fullLesson(t, 1)
	mia := f.join(t, bookingNow, anna, l, f.mia, false)
	stays := f.join(t, "2026-10-21T10:01:00Z", tom, l, f.tom, true)
	me := f.join(t, "2026-10-21T10:02:00Z", anna, l, f.anna, false)

	f.cancelStella(t, "2026-10-21T12:00:00Z", l)
	miaToken := f.token(t, "anna@example.org")

	// Past the expiry, before the job: the invite is of no use, and its
	// place holds nothing.
	late := "2026-10-22T00:00:00Z"
	if _, err := f.waitlistService(t, late).AcceptInvite(ctx, miaToken); !errors.Is(err, ErrInviteExpired) {
		t.Fatalf("accept at the expiry: want ErrInviteExpired, got %v", err)
	}

	if _, err := f.waitlistService(t, late).Accept(ctx, anna, mia.Entry.ID); !errors.Is(err, ErrInviteExpired) {
		t.Fatalf("accept in the app at the expiry: want ErrInviteExpired, got %v", err)
	}

	sl, err := NewScheduleService(f.st, clock.NewFixed(mustInstant(t, late))).Lesson(ctx, anna, l.ID)
	if err != nil || sl.FreePlaces != 1 || sl.Waiting[1].Status != domain.WaitlistExpired {
		t.Fatalf("lesson at the expiry: %+v, %v", sl, err)
	}

	n, err := f.waitlistService(t, late).ExpireDue(ctx)
	if err != nil || n != 1 {
		t.Fatalf("ExpireDue = %d, %v; want 1", n, err)
	}

	// Mia did not stay on the list: her entry ends, Anna learns of it, and
	// Tom is next.
	wantStatus(t, f.entry(t, mia.Entry.ID), domain.WaitlistExpired)
	wantStatus(t, f.entry(t, stays.Entry.ID), domain.WaitlistInvited)
	wantStatus(t, f.entry(t, me.Entry.ID), domain.WaitlistWaiting)

	expired := f.mailsOf(t, mail.TemplateWaitlistExpired)
	if len(expired) != 1 || expired[0].To != "anna@example.org" ||
		!strings.Contains(expired[0].Text, "steht nicht mehr auf der Warteliste") {
		t.Fatalf("expiry mails %+v", expired)
	}

	if invites := f.mailsOf(t, mail.TemplateWaitlistInvite); len(invites) != 2 || invites[1].To != "tom@example.org" {
		t.Fatalf("invites %+v", invites)
	}

	if v, err := f.waitlistService(t, late).Invite(ctx, miaToken); err != nil || v.State != InviteExpired {
		t.Fatalf("Mia's link: %+v, %v", v, err)
	}

	// Idempotent.
	before := len(f.mails(t))
	if n, err := f.waitlistService(t, late).ExpireDue(ctx); err != nil || n != 0 || len(f.mails(t)) != before {
		t.Fatalf("second run = %d, %v, %d new mails", n, err, len(f.mails(t))-before)
	}

	// Tom stays on the list: he goes to its end, after Anna, who is
	// invited now; his old link says that he waits.
	tomToken := f.token(t, "tom@example.org")
	later := "2026-10-22T12:00:00Z"

	if n, err := f.waitlistService(t, later).ExpireDue(ctx); err != nil || n != 1 {
		t.Fatalf("ExpireDue = %d, %v; want 1", n, err)
	}

	requeued := f.entry(t, stays.Entry.ID)
	if requeued.Status != domain.WaitlistWaiting || !requeued.JoinedAt.Equal(mustInstant(t, later)) ||
		requeued.InviteTokenHash != domain.HashToken(tomToken) || !requeued.ExpiresAt.IsZero() {
		t.Fatalf("Tom's entry %+v", requeued)
	}

	wantStatus(t, f.entry(t, me.Entry.ID), domain.WaitlistInvited)

	if expired = f.mailsOf(t, mail.TemplateWaitlistExpired); len(expired) != 2 || expired[1].To != "tom@example.org" ||
		!strings.Contains(expired[1].Text, "stays on the waiting list") {
		t.Fatalf("expiry mails %+v", expired)
	}

	if v, err := f.waitlistService(t, later).Invite(ctx, tomToken); err != nil || v.State != InviteWaiting ||
		v.Position != 1 || v.Person.ID != f.tom.ID {
		t.Fatalf("Tom's old link: %+v, %v", v, err)
	}

	if _, err := f.waitlistService(t, later).AcceptInvite(ctx, tomToken); !errors.Is(err, ErrInviteExpired) {
		t.Fatalf("accept by Tom's old link: want ErrInviteExpired, got %v", err)
	}

	// Anna's invite runs 12 hours, until well before the booking closes.
	if e := f.entry(t, me.Entry.ID); !e.ExpiresAt.Equal(mustInstant(t, "2026-10-23T00:00:00Z")) {
		t.Fatalf("Anna's invite %+v", e)
	}

	// When it ends, Tom is invited again with a new link, and the old one
	// finds nothing.
	if _, err := f.waitlistService(t, "2026-10-23T00:00:00Z").ExpireDue(ctx); err != nil {
		t.Fatal(err)
	}

	wantStatus(t, f.entry(t, stays.Entry.ID), domain.WaitlistInvited)

	if newToken := f.token(t, "tom@example.org"); newToken == tomToken {
		t.Fatal("Tom's new invite has the old token")
	}

	if _, err := f.waitlistService(t, "2026-10-23T00:00:00Z").Invite(ctx, tomToken); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Tom's old link after the new invite: want ErrNotFound, got %v", err)
	}
}

// TestWaitlistInviteCutoff invites close to the booking cutoff: the invite
// ends with the booking, and then nobody goes back to the list or is
// invited.
// reinvitingStore re-invites the waiting-list entry with a new token right
// after the service locks the entry's lesson: what an accept that waited for
// the lock reads on PostgreSQL when a re-invite committed meanwhile.
type reinvitingStore struct {
	store.Store

	entry domain.WaitlistEntryID
}

func (s reinvitingStore) InTx(ctx context.Context, mode store.TxMode, fn func(context.Context, store.Tx) error) error {
	return s.Store.InTx(ctx, mode, func(ctx context.Context, tx store.Tx) error {
		return fn(ctx, reinvitingTx{Tx: tx, entry: s.entry})
	})
}

type reinvitingTx struct {
	store.Tx

	entry domain.WaitlistEntryID
}

func (t reinvitingTx) Lessons() store.LessonRepo { return reinvitingLessons{t.Tx.Lessons(), t} }

type reinvitingLessons struct {
	store.LessonRepo

	tx reinvitingTx
}

func (r reinvitingLessons) Lock(ctx context.Context, id domain.LessonID) (domain.Lesson, error) {
	l, err := r.LessonRepo.Lock(ctx, id)
	if err != nil {
		return l, err
	}

	e, err := r.tx.Waitlist().Get(ctx, r.tx.entry)
	if err != nil {
		return l, err
	}

	e.InviteTokenHash = domain.HashToken("the-new-invite")

	return l, r.tx.Waitlist().Update(ctx, e)
}

// TestWaitlistAcceptInviteReinvited: a link whose entry was invited again
// while the accept waited for the lesson's lock is expired, and books
// nothing.
func TestWaitlistAcceptInviteReinvited(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newBookingFixture(t)
	l := f.fullLesson(t, 1)
	mia := f.join(t, bookingNow, f.principal(t, f.anna), l, f.mia, false)

	f.cancelStella(t, "2026-10-21T12:00:00Z", l)

	token := f.token(t, "anna@example.org")
	svc := NewWaitlistService(reinvitingStore{Store: f.st, entry: mia.Entry.ID},
		clock.NewFixed(mustInstant(t, "2026-10-21T13:00:00Z")), f.seq, testMails(newTestRenderer(t)))

	if _, err := svc.AcceptInvite(ctx, token); !errors.Is(err, ErrInviteExpired) {
		t.Fatalf("accept the old link: want ErrInviteExpired, got %v", err)
	}

	wantStatus(t, f.entry(t, mia.Entry.ID), domain.WaitlistInvited)

	if n := len(f.mailsOf(t, mail.TemplateBookingConfirmed)); n != 0 {
		t.Errorf("%d booking confirmations, want none", n)
	}
}

func TestWaitlistInviteCutoff(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newBookingFixture(t)
	anna := f.principal(t, f.anna)

	// Wednesday 22:00 in Berlin: booking closes at 19:00 UTC.
	l := f.oneOff(t, "2026-10-21T20:00:00Z", 1, domain.VisibilityPublic, domain.LessonWorkshop)
	f.bookAs(t, f.stella(t), l)
	tom := f.join(t, bookingNow, f.principal(t, f.tom), l, f.tom, true)
	me := f.join(t, "2026-10-21T10:01:00Z", anna, l, f.anna, true)

	f.cancelStella(t, "2026-10-21T12:00:00Z", l)

	if e := f.entry(t, tom.Entry.ID); e.Status != domain.WaitlistInvited ||
		!e.ExpiresAt.Equal(mustInstant(t, "2026-10-21T19:00:00Z")) {
		t.Fatalf("Tom's invite %+v, want it to end at the cutoff", e)
	}

	if n, err := f.waitlistService(t, "2026-10-21T19:00:00Z").ExpireDue(ctx); err != nil || n != 1 {
		t.Fatalf("ExpireDue = %d, %v; want 1", n, err)
	}

	wantStatus(t, f.entry(t, tom.Entry.ID), domain.WaitlistExpired)
	wantStatus(t, f.entry(t, me.Entry.ID), domain.WaitlistWaiting)

	expired := f.mailsOf(t, mail.TemplateWaitlistExpired)
	if len(expired) != 1 || !strings.Contains(expired[0].Text, "no longer on the waiting list") {
		t.Fatalf("expiry mails %+v", expired)
	}

	if n := len(f.mailsOf(t, mail.TemplateWaitlistInvite)); n != 1 {
		t.Errorf("%d invites, want only Tom's", n)
	}

	// At the start, the open entries end, without a mail.
	before := len(f.mails(t))
	if n, err := f.waitlistService(t, "2026-10-21T20:00:00Z").ExpireDue(ctx); err != nil || n != 1 {
		t.Fatalf("ExpireDue at the start = %d, %v; want 1", n, err)
	}

	wantStatus(t, f.entry(t, me.Entry.ID), domain.WaitlistExpired)

	if len(f.mails(t)) != before {
		t.Errorf("the start mailed %+v", f.mails(t)[before:])
	}
}

// TestWaitlistInApp accepts, declines and leaves in the app: only the
// family's entries, in the right state, each passing a freed place on.
func TestWaitlistInApp(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newBookingFixture(t)
	anna := f.principal(t, f.anna)
	tom := f.principal(t, f.tom)
	stranger := f.principal(t, f.stella(t))
	l := f.fullLesson(t, 1)
	mia := f.join(t, bookingNow, anna, l, f.mia, true)
	tomsEntry := f.join(t, "2026-10-21T10:01:00Z", tom, l, f.tom, false)
	me := f.join(t, "2026-10-21T10:02:00Z", anna, l, f.anna, false)
	f.cancelStella(t, "2026-10-21T12:00:00Z", l)

	svc := f.waitlistService(t, "2026-10-21T13:00:00Z")

	for name, tt := range map[string]struct {
		p    Principal
		id   domain.WaitlistEntryID
		want error
	}{
		"stranger":   {stranger, mia.Entry.ID, domain.ErrNotFound},
		"unknown":    {anna, ids.New[domain.WaitlistEntryID](f.seq), domain.ErrNotFound},
		"not yet":    {tom, tomsEntry.Entry.ID, ErrNotInvited},
		"guardian's": {tom, me.Entry.ID, domain.ErrNotFound},
	} {
		if _, err := svc.Accept(ctx, tt.p, tt.id); !errors.Is(err, tt.want) {
			t.Errorf("accept %s: want %v, got %v", name, tt.want, err)
		}

		if _, err := svc.Decline(ctx, tt.p, tt.id); !errors.Is(err, tt.want) {
			t.Errorf("decline %s: want %v, got %v", name, tt.want, err)
		}
	}

	// Mia declines but stays on the list: she goes to its end, and Tom is
	// invited. Her link says that she waits.
	miaToken := f.token(t, "anna@example.org")

	declined, err := svc.Decline(ctx, anna, mia.Entry.ID)
	if err != nil {
		t.Fatal(err)
	}

	if declined.Entry.Status != domain.WaitlistWaiting || declined.Position != 2 ||
		!declined.Entry.JoinedAt.Equal(mustInstant(t, "2026-10-21T13:00:00Z")) {
		t.Fatalf("declined %+v", declined)
	}

	wantStatus(t, f.entry(t, tomsEntry.Entry.ID), domain.WaitlistInvited)

	if v, err := svc.Invite(ctx, miaToken); err != nil || v.State != InviteWaiting || v.Position != 2 {
		t.Fatalf("Mia's link after declining: %+v, %v", v, err)
	}

	if n := len(f.mailsOf(t, mail.TemplateWaitlistExpired)); n != 0 {
		t.Errorf("declining mailed %d expiry mails", n)
	}

	// Tom declines without staying: his entry ends, and Anna is invited.
	declined, err = svc.Decline(ctx, tom, tomsEntry.Entry.ID)
	if err != nil || declined.Entry.Status != domain.WaitlistExpired || declined.Position != 0 {
		t.Fatalf("Tom declined: %+v, %v", declined, err)
	}

	wantStatus(t, f.entry(t, me.Entry.ID), domain.WaitlistInvited)

	if _, err := svc.Decline(ctx, tom, tomsEntry.Entry.ID); !errors.Is(err, ErrInviteExpired) {
		t.Fatalf("decline twice: want ErrInviteExpired, got %v", err)
	}

	// Anna leaves while invited: the place goes to Mia, back in line.
	if err := svc.Leave(ctx, anna, me.Entry.ID); err != nil {
		t.Fatal(err)
	}

	wantStatus(t, f.entry(t, me.Entry.ID), domain.WaitlistWithdrawn)
	wantStatus(t, f.entry(t, mia.Entry.ID), domain.WaitlistInvited)

	if err := svc.Leave(ctx, anna, me.Entry.ID); !errors.Is(err, ErrNotWaiting) {
		t.Fatalf("leave twice: want ErrNotWaiting, got %v", err)
	}

	if err := svc.Leave(ctx, stranger, mia.Entry.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("leave a stranger's: want ErrNotFound, got %v", err)
	}

	if _, err := svc.Accept(ctx, anna, me.Entry.ID); !errors.Is(err, ErrInviteUsed) {
		t.Fatalf("accept a withdrawn entry: want ErrInviteUsed, got %v", err)
	}

	// Anna accepts Mia's invite in the app: Mia is booked, by Anna, who
	// gets the confirmation like for a booking.
	b, err := svc.Accept(ctx, anna, mia.Entry.ID)
	if err != nil {
		t.Fatal(err)
	}

	_, events := f.booking(t, b.ID)
	if b.PersonID != f.mia.ID || b.Source != domain.BookingSourceSelf || len(events) != 1 ||
		events[0].ActorAccountID != anna.AccountID || events[0].Note != waitlistNote {
		t.Fatalf("booking %+v, events %+v", b, events)
	}

	wantStatus(t, f.entry(t, mia.Entry.ID), domain.WaitlistAccepted)

	if confirmed := f.mailsOf(t, mail.TemplateBookingConfirmed); len(confirmed) != 1 || confirmed[0].To != "anna@example.org" {
		t.Fatalf("confirmations %+v", confirmed)
	}

	if _, err := svc.Accept(ctx, anna, mia.Entry.ID); !errors.Is(err, ErrInviteUsed) {
		t.Fatalf("accept twice: want ErrInviteUsed, got %v", err)
	}
}

// TestWaitlistLessonEdits: a higher capacity invites as many as it frees, a
// capacity below the bookings and held places is refused, a canceled
// lesson voids its invites (back to waiting, in place), its restore invites
// again, and a lesson moved past its cutoff cannot be taken any more.
func TestWaitlistLessonEdits(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newBookingFixture(t)
	anna := f.principal(t, f.anna)
	olga := f.principal(t, f.olga)
	l := f.fullLesson(t, 1)
	entries := []WaitlistPlace{
		f.join(t, bookingNow, anna, l, f.mia, false),
		f.join(t, "2026-10-21T10:01:00Z", f.principal(t, f.tom), l, f.tom, false),
		f.join(t, "2026-10-21T10:02:00Z", anna, l, f.anna, false),
	}
	lessons := f.lessonService(t, "2026-10-21T12:00:00Z")

	data := func(capacity int) LessonData {
		start := mustInstant(t, waitlistLesson).In(berlin(t))

		return LessonData{
			Date: domain.DateOf(start, berlin(t)), StartTime: domain.TimeOfDay{Hour: start.Hour(), Minute: start.Minute()},
			Duration: time.Hour, RoomID: f.room, InstructorID: f.instructor, Capacity: capacity,
			Visibility: domain.VisibilityPublic, Kind: domain.LessonWorkshop,
		}
	}

	sl, err := lessons.UpdateLesson(ctx, l.ID, data(3))
	if err != nil {
		t.Fatal(err)
	}

	wantStatus(t, f.entry(t, entries[0].Entry.ID), domain.WaitlistInvited)
	wantStatus(t, f.entry(t, entries[1].Entry.ID), domain.WaitlistInvited)
	wantStatus(t, f.entry(t, entries[2].Entry.ID), domain.WaitlistWaiting)

	if sl.FreePlaces != 0 || sl.Admin.ActiveBookings != 1 {
		t.Fatalf("after raising: %+v", sl)
	}

	if n := len(f.mailsOf(t, mail.TemplateWaitlistInvite)); n != 2 {
		t.Fatalf("%d invites, want 2", n)
	}

	// The booking and two held places: 3 is the least capacity.
	_, err = lessons.UpdateLesson(ctx, l.ID, data(2))
	wantFieldError(t, err, "capacity")

	miaToken := f.token(t, "anna@example.org")

	// Canceling voids the invites: they wait again, first in line.
	if _, err := lessons.CancelLesson(ctx, olga, l.ID, ""); err != nil {
		t.Fatal(err)
	}

	for i, place := range entries {
		e := f.entry(t, place.Entry.ID)
		if e.Status != domain.WaitlistWaiting || !e.JoinedAt.Equal(place.Entry.JoinedAt) {
			t.Fatalf("entry %d after the cancellation: %+v", i, e)
		}
	}

	if v, err := f.waitlistService(t, "2026-10-21T12:00:00Z").Invite(ctx, miaToken); err != nil ||
		v.State != InviteWaiting || v.Position != 1 {
		t.Fatalf("a voided link: %+v, %v", v, err)
	}

	if _, err := f.waitlistService(t, "2026-10-21T12:00:00Z").AcceptInvite(ctx, miaToken); !errors.Is(err, ErrInviteExpired) {
		t.Fatalf("accept by a voided link: want ErrInviteExpired, got %v", err)
	}

	if n := len(f.mailsOf(t, mail.TemplateWaitlistExpired)); n != 0 {
		t.Errorf("voiding mailed %d expiry mails", n)
	}

	// Restoring invites the first two again.
	if _, err := lessons.RestoreLesson(ctx, l.ID, false); err != nil {
		t.Fatal(err)
	}

	wantStatus(t, f.entry(t, entries[0].Entry.ID), domain.WaitlistInvited)
	wantStatus(t, f.entry(t, entries[1].Entry.ID), domain.WaitlistInvited)

	if n := len(f.mailsOf(t, mail.TemplateWaitlistInvite)); n != 4 {
		t.Fatalf("%d invites, want 4", n)
	}

	// Moved to start within the hour, the lesson is closed for booking:
	// the invite cannot be taken any more.
	moved := data(3)
	moved.Date, moved.StartTime = domain.Date{Year: 2026, Month: time.October, Day: 21}, domain.TimeOfDay{Hour: 14, Minute: 30}

	if _, err := lessons.UpdateLesson(ctx, l.ID, moved); err != nil {
		t.Fatal(err)
	}

	_, err = f.waitlistService(t, "2026-10-21T12:00:00Z").AcceptInvite(ctx, f.token(t, "anna@example.org"))
	wantClosed(t, err, ClosedCutoff)

	v, err := f.waitlistService(t, "2026-10-21T12:00:00Z").Invite(ctx, f.token(t, "anna@example.org"))
	if err != nil || v.State != InviteOpen || v.Closed != ClosedCutoff {
		t.Fatalf("view: %+v, %v", v, err)
	}
}

// TestWaitlistBookedOtherwise ends a person's entry when they get booked
// some other way: booking with their own invite takes its place, and a
// waiting entry is withdrawn.
func TestWaitlistBookedOtherwise(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newBookingFixture(t)
	anna := f.principal(t, f.anna)
	l := f.fullLesson(t, 1)
	mia := f.join(t, bookingNow, anna, l, f.mia, false)
	me := f.join(t, "2026-10-21T10:01:00Z", anna, l, f.anna, false)
	f.cancelStella(t, "2026-10-21T12:00:00Z", l)

	// Mia's invite holds the place: booking her takes it.
	b, err := f.bookingService(t, "2026-10-21T13:00:00Z").Book(ctx, anna, l.ID, f.mia.ID)
	if err != nil {
		t.Fatal(err)
	}

	if _, events := f.booking(t, b.ID); len(events) != 1 || events[0].Note != waitlistNote {
		t.Fatalf("events %+v", events)
	}

	wantStatus(t, f.entry(t, mia.Entry.ID), domain.WaitlistAccepted)
	wantStatus(t, f.entry(t, me.Entry.ID), domain.WaitlistWaiting)

	// A free place while Anna waits (as after an invite that ran out, or
	// skipped a person no mail reaches): booking her withdraws her entry.
	f.write(t, func(ctx context.Context, tx store.Tx) error {
		l.Capacity = 2

		return tx.Lessons().Update(ctx, l)
	})

	b, err = f.bookingService(t, "2026-10-21T13:00:00Z").Book(ctx, anna, l.ID, f.anna.ID)
	if err != nil {
		t.Fatal(err)
	}

	if _, events := f.booking(t, b.ID); len(events) != 1 || events[0].Note != "" {
		t.Fatalf("events %+v", events)
	}

	wantStatus(t, f.entry(t, me.Entry.ID), domain.WaitlistWithdrawn)
}

// TestWaitlistSkipsUnreachable passes over a person whom no mail would
// reach: they keep waiting, and the next is invited.
func TestWaitlistSkipsUnreachable(t *testing.T) {
	t.Parallel()

	f := newBookingFixture(t)
	l := f.fullLesson(t, 1)
	nobody := f.person(t, "Nobody", "", "", "")

	f.write(t, func(ctx context.Context, tx store.Tx) error {
		return tx.Waitlist().Create(ctx, domain.WaitlistEntry{
			ID: ids.New[domain.WaitlistEntryID](f.seq), LessonID: l.ID, PersonID: nobody.ID,
			Status: domain.WaitlistWaiting, JoinedAt: mustInstant(t, "2026-10-20T10:00:00Z"),
		})
	})

	tom := f.join(t, bookingNow, f.principal(t, f.tom), l, f.tom, false)
	f.cancelStella(t, "2026-10-21T12:00:00Z", l)

	wantStatus(t, f.entry(t, tom.Entry.ID), domain.WaitlistInvited)

	if got := f.entries(t, l); got[0].PersonID != nobody.ID || got[0].Status != domain.WaitlistWaiting {
		t.Fatalf("entries %+v", got)
	}
}

// TestWaitlistRemovePerson withdraws the entries of a removed dependent and
// of a person the office deletes, passing their places on.
func TestWaitlistRemovePerson(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newBookingFixture(t)
	anna := f.principal(t, f.anna)
	l := f.fullLesson(t, 1)
	mia := f.join(t, bookingNow, anna, l, f.mia, false)
	tom := f.join(t, "2026-10-21T10:01:00Z", f.principal(t, f.tom), l, f.tom, false)
	f.cancelStella(t, "2026-10-21T12:00:00Z", l)
	wantStatus(t, f.entry(t, mia.Entry.ID), domain.WaitlistInvited)

	clk := clock.NewFixed(mustInstant(t, "2026-10-21T13:00:00Z"))
	mails := testMails(newTestRenderer(t))

	// Mia has no bookings and no other guardian: she is deleted with her
	// entry, and Tom gets the place.
	if err := NewFamilyService(f.st, clk, f.seq, mails).RemoveDependent(ctx, anna, f.mia.ID); err != nil {
		t.Fatal(err)
	}

	if got := f.entries(t, l); len(got) != 1 || got[0].ID != tom.Entry.ID || got[0].Status != domain.WaitlistInvited {
		t.Fatalf("entries %+v", got)
	}

	// The office deletes a person on the list the same way.
	ida := f.person(t, "Ida", "ida@example.org", "", "")
	f.write(t, func(ctx context.Context, tx store.Tx) error {
		return tx.Waitlist().Create(ctx, domain.WaitlistEntry{
			ID: ids.New[domain.WaitlistEntryID](f.seq), LessonID: l.ID, PersonID: ida.ID,
			Status: domain.WaitlistWaiting, JoinedAt: mustInstant(t, "2026-10-21T12:30:00Z"),
		})
	})

	if err := NewPersonService(f.st, clk, f.seq, mails).DeletePerson(ctx, ida.ID); err != nil {
		t.Fatal(err)
	}

	if got := f.entries(t, l); len(got) != 1 {
		t.Fatalf("entries %+v", got)
	}
}

// TestWaitlistClassMember: the timetable ends a member's entry when it
// books them, and a class edit that raises the capacity of its lessons
// invites into the new places.
func TestWaitlistClassMember(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newBookingFixture(t)
	svc := f.timetable(t, bookingNow)

	c, err := svc.CreateClass(ctx, f.term.ID, f.classData())
	if err != nil {
		t.Fatal(err)
	}

	first := f.classLessons(t, c.ID)[0]
	f.book(t, first, c.Capacity)

	mia := f.join(t, bookingNow, f.principal(t, f.anna), first, f.mia, false)
	tom := f.join(t, bookingNow, f.principal(t, f.tom), first, f.tom, false)

	if _, err := svc.AddClassMember(ctx, f.principal(t, f.olga), domain.ClassMember{ClassID: c.ID, PersonID: f.mia.ID}); err != nil {
		t.Fatal(err)
	}

	wantStatus(t, f.entry(t, mia.Entry.ID), domain.WaitlistWithdrawn)
	wantStatus(t, f.entry(t, tom.Entry.ID), domain.WaitlistWaiting)

	// Two more places for every lesson: Mia took one beyond the capacity,
	// so one is free, and Tom is invited.
	data := f.classData()
	data.Capacity = c.Capacity + 2

	if _, err := svc.UpdateClass(ctx, c.ID, data); err != nil {
		t.Fatal(err)
	}

	wantStatus(t, f.entry(t, tom.Entry.ID), domain.WaitlistInvited)

	// Lowering it below the bookings and Tom's held place is refused.
	data.Capacity = c.Capacity + 1

	_, err = svc.UpdateClass(ctx, c.ID, data)
	wantFieldError(t, err, "capacity")
}

// invitedEntry stores an entry of person in l that is invited until
// expires, joined and invited the day before bookingNow.
func (f *bookingFixture) invitedEntry(
	t *testing.T, l domain.Lesson, person domain.Person, token byte, expires string,
) domain.WaitlistEntry {
	t.Helper()

	e := domain.WaitlistEntry{
		ID: ids.New[domain.WaitlistEntryID](f.seq), LessonID: l.ID, PersonID: person.ID,
		Status: domain.WaitlistInvited, JoinedAt: mustInstant(t, "2026-10-20T09:00:00Z"),
		InviteTokenHash: [32]byte{31: token}, InvitedAt: mustInstant(t, "2026-10-20T10:00:00Z"),
		ExpiresAt: mustInstant(t, expires),
	}
	f.write(t, func(ctx context.Context, tx store.Tx) error { return tx.Waitlist().Create(ctx, e) })

	return e
}

// TestWaitlistQueueFirst: an invite that has run out but that the job has
// not ended yet does not free its place for a direct booking or a new
// entry: they settle the list first, and the next waiting entry gets the
// place, even when the booking is refused.
func TestWaitlistQueueFirst(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newBookingFixture(t)
	anna := f.principal(t, f.anna)
	l := f.fullLesson(t, 1)
	mia := f.join(t, bookingNow, anna, l, f.mia, false)
	tom := f.join(t, "2026-10-21T10:01:00Z", f.principal(t, f.tom), l, f.tom, false)
	f.cancelStella(t, "2026-10-21T12:00:00Z", l)
	wantStatus(t, f.entry(t, mia.Entry.ID), domain.WaitlistInvited)

	// Mia's invite ran out at midnight; at 01:00 Anna books herself.
	_, err := f.bookingService(t, "2026-10-22T01:00:00Z").Book(ctx, anna, l.ID, f.anna.ID)
	if !errors.Is(err, ErrLessonFull) {
		t.Fatalf("book after the expiry: want ErrLessonFull, got %v", err)
	}

	wantStatus(t, f.entry(t, mia.Entry.ID), domain.WaitlistExpired)
	wantStatus(t, f.entry(t, tom.Entry.ID), domain.WaitlistInvited)

	if n := len(f.mailsOf(t, mail.TemplateWaitlistExpired)); n != 1 {
		t.Errorf("%d expiry mails, want 1", n)
	}

	// Tom's invite runs out too, and Anna joins rather than books: Tom's
	// entry ends, and the lesson is not full any more.
	_, err = f.waitlistService(t, "2026-10-22T14:00:00Z").Join(ctx, anna, l.ID, f.anna.ID, false)
	if !errors.Is(err, ErrNotFull) {
		t.Fatalf("join after the expiry: want ErrNotFull, got %v", err)
	}

	wantStatus(t, f.entry(t, tom.Entry.ID), domain.WaitlistExpired)
}

// TestWaitlistJoinSettles joins behind an invite that ran out: the list
// is settled first, so the next waiting entry is invited and the joiner
// waits.
func TestWaitlistJoinSettles(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newBookingFixture(t)
	anna := f.principal(t, f.anna)
	l := f.fullLesson(t, 1)
	mia := f.join(t, bookingNow, anna, l, f.mia, false)
	tom := f.join(t, "2026-10-21T10:01:00Z", f.principal(t, f.tom), l, f.tom, false)
	f.cancelStella(t, "2026-10-21T12:00:00Z", l)

	me, err := f.waitlistService(t, "2026-10-22T01:00:00Z").Join(ctx, anna, l.ID, f.anna.ID, false)
	if err != nil || me.Position != 1 {
		t.Fatalf("join: %+v, %v", me, err)
	}

	wantStatus(t, f.entry(t, mia.Entry.ID), domain.WaitlistExpired)
	wantStatus(t, f.entry(t, tom.Entry.ID), domain.WaitlistInvited)
}

// TestWaitlistLessonEditGuards: an unchanged capacity stays editable
// although bookings and held places exceed it (a member's override), and
// a lesson that takes invites again (hidden, then visible) offers its free
// places.
func TestWaitlistLessonEditGuards(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newBookingFixture(t)
	l := f.fullLesson(t, 1)
	tom := f.join(t, bookingNow, f.principal(t, f.tom), l, f.tom, false)
	lessons := f.lessonService(t, "2026-10-21T12:00:00Z")

	data := func(vis domain.LessonVisibility) LessonData {
		start := mustInstant(t, waitlistLesson).In(berlin(t))

		return LessonData{
			Date: domain.DateOf(start, berlin(t)), StartTime: domain.TimeOfDay{Hour: start.Hour(), Minute: start.Minute()},
			Duration: time.Hour, RoomID: f.room, InstructorID: f.instructor, Capacity: 1,
			Visibility: vis, Kind: domain.LessonWorkshop,
		}
	}

	if _, err := lessons.UpdateLesson(ctx, l.ID, data(domain.VisibilityHidden)); err != nil {
		t.Fatal(err)
	}

	// Hidden, the freed place goes to nobody.
	f.cancelStella(t, "2026-10-21T12:00:00Z", l)
	wantStatus(t, f.entry(t, tom.Entry.ID), domain.WaitlistWaiting)

	// Visible again, Tom is invited.
	if _, err := lessons.UpdateLesson(ctx, l.ID, data(domain.VisibilityPublic)); err != nil {
		t.Fatal(err)
	}

	wantStatus(t, f.entry(t, tom.Entry.ID), domain.WaitlistInvited)

	// A member's override books beyond the place Tom holds; the lesson
	// stays editable while its capacity is not lowered.
	f.bookAs(t, f.stella(t), l)

	if _, err := lessons.UpdateLesson(ctx, l.ID, data(domain.VisibilityPublic)); err != nil {
		t.Fatalf("edit with the capacity unchanged: %v", err)
	}
}

// TestWaitlistEnrollOverride records an override for a class member booked
// into a place that someone else's invite holds.
func TestWaitlistEnrollOverride(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newBookingFixture(t)
	svc := f.timetable(t, bookingNow)

	c, err := svc.CreateClass(ctx, f.term.ID, f.classData())
	if err != nil {
		t.Fatal(err)
	}

	first := f.classLessons(t, c.ID)[0]
	f.book(t, first, c.Capacity-1)
	f.invitedEntry(t, first, f.tom, 1, "2026-10-22T10:00:00Z")

	if _, err := svc.AddClassMember(ctx, f.principal(t, f.olga), domain.ClassMember{ClassID: c.ID, PersonID: f.mia.ID}); err != nil {
		t.Fatal(err)
	}

	_, events := f.booking(t, f.bookingIn(t, first, f.mia).ID)
	if len(events) != 1 || !events[0].Override {
		t.Fatalf("Mia's events %+v, want an override", events)
	}
}

// failingLockStore fails Lessons().Lock for one lesson.
type failingLockStore struct {
	store.Store

	lesson domain.LessonID
}

var errLockFailed = errors.New("lock failed")

func (s failingLockStore) InTx(ctx context.Context, mode store.TxMode, fn func(context.Context, store.Tx) error) error {
	return s.Store.InTx(ctx, mode, func(ctx context.Context, tx store.Tx) error {
		return fn(ctx, failingLockTx{Tx: tx, lesson: s.lesson})
	})
}

type failingLockTx struct {
	store.Tx

	lesson domain.LessonID
}

func (t failingLockTx) Lessons() store.LessonRepo {
	return failingLockLessons{t.Tx.Lessons(), t.lesson}
}

type failingLockLessons struct {
	store.LessonRepo

	lesson domain.LessonID
}

func (r failingLockLessons) Lock(ctx context.Context, id domain.LessonID) (domain.Lesson, error) {
	if id == r.lesson {
		return domain.Lesson{}, errLockFailed
	}

	return r.LessonRepo.Lock(ctx, id)
}

// TestWaitlistExpireDueIsolatesLessons settles every due lesson in its own
// transaction: one that fails does not keep the others from settling.
func TestWaitlistExpireDueIsolatesLessons(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newBookingFixture(t)
	a, b := f.fullLesson(t, 1), f.fullLesson(t, 1)
	inA := f.invitedEntry(t, a, f.tom, 1, "2026-10-21T09:00:00Z")
	inB := f.invitedEntry(t, b, f.tom, 2, "2026-10-21T09:00:00Z")

	svc := NewWaitlistService(failingLockStore{Store: f.st, lesson: a.ID}, clock.NewFixed(mustInstant(t, bookingNow)),
		f.seq, testMails(newTestRenderer(t)))

	n, err := svc.ExpireDue(ctx)
	if !errors.Is(err, errLockFailed) || n != 1 {
		t.Fatalf("ExpireDue = %d, %v; want 1 and the lock error", n, err)
	}

	wantStatus(t, f.entry(t, inA.ID), domain.WaitlistInvited)
	wantStatus(t, f.entry(t, inB.ID), domain.WaitlistExpired)
}
