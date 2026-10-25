package servicetest

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/auth/password"
	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/mail"
	"github.com/cwbudde/studio-schedule/internal/service"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// Privacy runs the scenarios of ADR 0016 (export, anonymization, account
// administration, retention) on fresh stores from newStore.
func Privacy(t *testing.T, newStore func(t *testing.T) store.Store) {
	t.Helper()

	for name, fn := range map[string]func(t *testing.T, w *world){
		"AnonymizeErases":         anonymizeErases,
		"AnonymizeRefuses":        anonymizeRefuses,
		"AnonymizedTakesNoWrites": anonymizedTakesNoWrites,
		"Export":                  exportPerson,
		"ExportFamily":            exportFamily,
		"AccountAdmin":            accountAdmin,
		"AccountAdminGuards":      accountAdminGuards,
		"LastAdminSignsIn":        lastAdminSignsIn,
		"ImpersonationsEnd":       impersonationsEnd,
		"DeleteOwnAccount":        deleteOwnAccount,
		"Retention":               retention,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fn(t, newWorld(t, newStore(t)))
		})
	}
}

// privacyFixture is Paula with everything a person can have: a guardian, a
// dependent who has another guardian, an account with a session, a link and
// a calendar feed, a class membership, a past and a future booking paid by
// her pass, a waiting-list entry and a receipt.
type privacyFixture struct {
	paula, grandma, kid, otherParent domain.Person
	account                          domain.Account
	session                          domain.Session
	feed                             domain.CalendarFeed
	past, future                     domain.Booking
	pastLesson                       domain.Lesson
	entry                            domain.WaitlistEntry
	pass                             domain.Pass
	receipt                          domain.Receipt
}

func newPrivacyFixture(w *world) privacyFixture {
	var f privacyFixture

	f.paula = w.person("Paula", "Private", "paula@example.org")
	f.paula.Phone, f.paula.Notes, f.paula.MemberNo = "+49 000 0000001", "Allergic to dust.", "M-0100"
	f.paula.BirthDate = domain.Date{Year: 1990, Month: time.May, Day: 1}
	f.paula = w.update(f.paula)

	f.grandma = w.person("Greta", "Private", "")
	f.kid = w.person("Kai", "Private", "")
	f.otherParent = w.person("Piet", "Private", "piet@example.org")
	w.link(f.grandma, f.paula)
	w.link(f.paula, f.kid)
	w.link(f.otherParent, f.kid)

	f.account = w.account(f.paula, "paula@example.org")
	f.session = domain.Session{
		ID: newID[domain.SessionID](w), AccountID: f.account.ID, TokenHash: domain.HashToken("paula"),
		CreatedAt: now.Add(-time.Hour), LastSeenAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour),
	}
	f.feed = domain.CalendarFeed{
		ID: newID[domain.CalendarFeedID](w), AccountID: f.account.ID, Scope: domain.FeedBookings, CreatedAt: now,
		TokenHash: domain.HashToken("feed"),
	}
	w.write(func(ctx context.Context, tx store.Tx) error {
		return errors.Join(
			tx.Sessions().Create(ctx, f.session),
			tx.CalendarFeeds().Create(ctx, f.feed),
			tx.Credentials().Put(ctx, domain.Credential{AccountID: f.account.ID, PasswordHash: "x", ChangedAt: now}),
		)
	})

	w.member(w.classes[0], f.paula)

	f.pastLesson = w.lesson(domain.ClassID{}, now.Add(-48*time.Hour))
	f.past = w.bookingAs(f.pastLesson, f.paula, domain.BookingSourceSelf, domain.BookingAttended)
	f.future = w.booking(w.lessons[0], f.paula, false)
	f.pass = w.pass(f.paula, domain.Person{}, 8)
	w.redeem(f.pass, f.past)
	w.redeem(f.pass, f.future)

	f.entry = domain.WaitlistEntry{
		ID: newID[domain.WaitlistEntryID](w), LessonID: w.lessons[1].ID, PersonID: f.paula.ID,
		Status: domain.WaitlistWaiting, JoinedAt: now.Add(-time.Hour),
	}
	w.write(func(ctx context.Context, tx store.Tx) error { return tx.Waitlist().Create(ctx, f.entry) })

	f.receipt = w.receipt(f.paula)

	return f
}

func anonymizeErases(t *testing.T, w *world) {
	ctx := t.Context()
	f := newPrivacyFixture(w)

	sum, err := w.persons().Anonymize(ctx, w.office, f.paula.ID)
	if err != nil {
		t.Fatalf("anonymize: %v", err)
	}

	want := service.AnonymizeSummary{
		Person:           f.paula.Anonymize(now),
		BookingsCanceled: 1, WaitlistWithdrawn: 1, MembershipsEnded: 1, LinksRemoved: 2, AccountDeleted: true,
	}
	if sum != want {
		t.Errorf("summary %+v, want %+v", sum, want)
	}

	if got := w.get(f.paula.ID); got != want.Person || got.GivenName != "Deleted" || got.Email != "" {
		t.Errorf("person %+v", got)
	}

	read(w, func(ctx context.Context, tx store.Tx) (any, error) {
		guardians, err := tx.Persons().ListGuardians(ctx, f.paula.ID)
		if err != nil || len(guardians) != 0 {
			t.Errorf("guardians %v, %v", guardians, err)
		}

		// The kid keeps its other parent.
		kidsGuardians, err := tx.Persons().ListGuardians(ctx, f.kid.ID)
		if err != nil || len(kidsGuardians) != 1 || kidsGuardians[0].ID != f.otherParent.ID {
			t.Errorf("the kid's guardians %v, %v", kidsGuardians, err)
		}

		if m, err := tx.Classes().ListMemberships(ctx, f.paula.ID); err != nil || len(m) != 0 {
			t.Errorf("memberships %v, %v", m, err)
		}

		if _, err := tx.Accounts().Get(ctx, f.account.ID); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("account: %v", err)
		}

		if _, err := tx.Sessions().GetByToken(ctx, f.session.TokenHash); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("session: %v", err)
		}

		if _, err := tx.CalendarFeeds().Get(ctx, f.feed.ID); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("feed: %v", err)
		}

		if e, err := tx.Waitlist().Get(ctx, f.entry.ID); err != nil || e.Status != domain.WaitlistWithdrawn {
			t.Errorf("waiting-list entry %+v, %v", e, err)
		}

		// The receipt is untouched, its snapshot included.
		if r, err := tx.Receipts().Get(ctx, f.receipt.ID); err != nil || r.RecipientName != "Paula Private" ||
			r.PayerID != f.paula.ID {
			t.Errorf("receipt %+v, %v", r, err)
		}

		events, err := tx.Audit().List(ctx, 10)
		if err != nil || len(events) == 0 || events[0].Action != domain.AuditPersonAnonymized ||
			events[0].TargetPersonID != f.paula.ID || events[0].ActorID != w.office.AccountID || !events[0].Details.IsZero() {
			t.Errorf("audit %+v, %v", events, err)
		}

		return nil, nil
	})

	// The future booking is canceled on time and its visit came back; the
	// past one stays, paid; the pass stays with the person.
	got := w.bookingsByID(f.past, f.future)
	if b := got[f.future.ID]; b.Status != domain.BookingCanceled || b.Late || !b.CanceledAt.Equal(now) {
		t.Errorf("future booking %+v", b)
	}

	if b := got[f.past.ID]; b.Status != domain.BookingAttended {
		t.Errorf("past booking %+v", b)
	}

	if p := w.passOf(f.pass.ID); p.VisitsRemaining != 9 || p.PersonID != f.paula.ID {
		t.Errorf("pass %+v; want 9 visits, still Paula's", p)
	}

	// Where her history names her, the views tell that she is anonymized,
	// so that the app shows "Deleted person" in its language.
	mails := service.Mails{Renderer: w.renderer(), AppURL: "https://school.example.org/"}

	passes, err := service.NewPassService(w.st, clock.NewFixed(now), w.seq, nil).PersonPasses(ctx, f.paula.ID)
	if err != nil || len(passes) != 1 || !passes[0].OwnerAnonymized || passes[0].OwnerName != "Deleted person" {
		t.Errorf("passes %+v, %v", passes, err)
	}

	lp, err := service.NewParticipantService(w.st, clock.NewFixed(now), w.seq, mails).
		Participants(ctx, w.office, f.pastLesson.ID)
	if err != nil || len(lp.Bookings) != 1 || !lp.Bookings[0].Anonymized {
		t.Errorf("participants %+v, %v", lp.Bookings, err)
	}

	// Lists and searches leave the person out; reading it finds it.
	found, err := w.persons().SearchPersons(ctx, "private")
	if err != nil || slices.ContainsFunc(found, func(p domain.Person) bool { return p.ID == f.paula.ID }) {
		t.Errorf("search: %v, %v", found, err)
	}

	if _, err := w.persons().Person(ctx, f.paula.ID); err != nil {
		t.Errorf("read: %v", err)
	}
}

func anonymizeRefuses(t *testing.T, w *world) {
	ctx := t.Context()
	svc := w.persons()

	// The only guardian of a dependent.
	parent := w.person("Solo", "Parent", "solo@example.org")
	child := w.person("Solo", "Child", "")
	w.link(parent, child)

	if _, err := svc.Anonymize(ctx, w.office, parent.ID); !errors.Is(err, service.ErrSoleGuardian) {
		t.Errorf("sole guardian: %v", err)
	}

	if got := w.get(parent.ID); got != parent {
		t.Errorf("refused, but changed: %+v", got)
	}

	// Twice.
	once := w.person("Once", "Only", "")
	if _, err := svc.Anonymize(ctx, w.office, once.ID); err != nil {
		t.Fatal(err)
	}

	if _, err := svc.Anonymize(ctx, w.office, once.ID); !errors.Is(err, service.ErrAlreadyAnonymized) {
		t.Errorf("twice: %v", err)
	}

	// A tombstone and an unknown person.
	tomb := w.person("Tom", "Stone", "")
	tomb.MergedInto = parent.ID
	w.update(tomb)

	for _, id := range []domain.PersonID{tomb.ID, newID[domain.PersonID](w)} {
		if _, err := svc.Anonymize(ctx, w.office, id); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("anonymize %v: %v", id, err)
		}
	}

	// Their own person.
	if _, err := svc.Anonymize(ctx, w.office, w.office.PersonID); !errors.Is(err, service.ErrOwnAccount) {
		t.Errorf("own person: %v", err)
	}

	// An admin's person: not by the office, and not the last enabled admin.
	adminPerson := w.person("Ada", "Admin", "")
	adminAcc := w.account(adminPerson, "ada.admin@example.org")
	adminAcc.Role = domain.RoleAdmin
	w.write(func(ctx context.Context, tx store.Tx) error { return tx.Accounts().Update(ctx, adminAcc) })

	if _, err := svc.Anonymize(ctx, w.office, adminPerson.ID); !errors.Is(err, service.ErrAdminPerson) ||
		!errors.Is(err, domain.ErrForbidden) {
		t.Errorf("an admin's person by the office: %v", err)
	}

	// An admin whose own account is disabled meanwhile, as if two requests
	// had raced: Ada is the last enabled admin.
	formerAdmin := service.Principal{
		AccountID: newID[domain.AccountID](w), PersonID: newID[domain.PersonID](w), Role: domain.RoleAdmin,
	}
	if _, err := svc.Anonymize(ctx, formerAdmin, adminPerson.ID); !errors.Is(err, service.ErrLastAdmin) {
		t.Errorf("the last admin's person: %v", err)
	}

	// A person whose account issued receipts: the office's, by someone else.
	w.receipt(parent)

	ada := service.Principal{AccountID: adminAcc.ID, PersonID: adminPerson.ID, Role: domain.RoleAdmin}

	_, err := svc.Anonymize(ctx, ada, w.office.PersonID)
	if !errors.Is(err, service.ErrAccountKept) || !errors.Is(err, domain.ErrInUse) {
		t.Errorf("an account receipts name: %v", err)
	}

	if got := w.get(w.office.PersonID); got.Anonymized() {
		t.Error("refused, but anonymized")
	}
}

func anonymizedTakesNoWrites(t *testing.T, w *world) {
	ctx := t.Context()
	svc := w.persons()
	p := w.person("Gone", "Soon", "")

	if _, err := svc.Anonymize(ctx, w.office, p.ID); err != nil {
		t.Fatal(err)
	}

	if _, err := svc.UpdatePerson(ctx, p.ID, service.PersonRecord{GivenName: "Back", FamilyName: "Again"}); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("update: %v", err)
	}

	other := w.person("Other", "Person", "")
	if _, err := svc.AddGuardian(ctx, other.ID, p.ID); err == nil {
		t.Error("linked an anonymized guardian")
	}

	if _, err := svc.Merge(ctx, w.office, service.MergeRequest{SurvivorID: other.ID, MergedID: p.ID}, false); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("merge: %v", err)
	}
}

func exportPerson(t *testing.T, w *world) {
	ctx := t.Context()
	f := newPrivacyFixture(w)

	// A tombstone merged into Paula paid a receipt too.
	tomb := w.person("Paula", "Duplicate", "")
	old := w.receipt(tomb)
	tomb.MergedInto = f.paula.ID
	w.update(tomb)

	x, err := w.persons().Export(ctx, f.paula.ID)
	if err != nil {
		t.Fatal(err)
	}

	if x.Person != f.paula || !x.ExportedAt.Equal(now) {
		t.Errorf("person %+v at %v", x.Person, x.ExportedAt)
	}

	if len(x.Guardians) != 1 || x.Guardians[0].ID != f.grandma.ID || len(x.Dependents) != 1 ||
		x.Dependents[0].ID != f.kid.ID || len(x.Merged) != 1 || x.Merged[0].ID != tomb.ID {
		t.Errorf("links %v / %v / %v", x.Guardians, x.Dependents, x.Merged)
	}

	if x.Account == nil || x.Account.Email != "paula@example.org" || len(x.CalendarFeeds) != 1 {
		t.Errorf("account %+v, feeds %v", x.Account, x.CalendarFeeds)
	}

	if len(x.Classes) != 1 || x.Classes[0].Class.ID != w.classes[0] {
		t.Errorf("classes %+v", x.Classes)
	}

	if len(x.Bookings) != 2 || x.Bookings[0].Booking.ID != f.past.ID || x.Bookings[0].CourseType.Name != "Imaginary Ballet" ||
		x.Bookings[1].Lesson.ID != w.lessons[0].ID {
		t.Errorf("bookings %+v", x.Bookings)
	}

	if len(x.Waitlist) != 1 || x.Waitlist[0].Entry.ID != f.entry.ID {
		t.Errorf("waiting list %+v", x.Waitlist)
	}

	if len(x.Passes) != 1 || x.Passes[0].Type.Name != "10-visit pass" || len(x.Passes[0].Redemptions) != 2 {
		t.Errorf("passes %+v", x.Passes)
	}

	receipts := mapIDs(x.Receipts, func(r service.ExportReceipt) domain.ReceiptID { return r.Receipt.ID })
	if len(receipts) != 2 || !slices.Contains(receipts, f.receipt.ID) || !slices.Contains(receipts, old.ID) {
		t.Errorf("receipts %v", receipts)
	}

	// The tombstone itself is exported too.
	if x, err := w.persons().Export(ctx, tomb.ID); err != nil || x.Person.MergedInto != f.paula.ID {
		t.Errorf("tombstone %+v, %v", x.Person, err)
	}

	if _, err := w.persons().Export(ctx, newID[domain.PersonID](w)); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("unknown: %v", err)
	}
}

func exportFamily(t *testing.T, w *world) {
	ctx := t.Context()
	f := newPrivacyFixture(w)
	family := service.NewFamilyService(w.st, clock.NewFixed(now), w.seq, service.Mails{})
	p := service.Principal{AccountID: f.account.ID, PersonID: f.paula.ID, Email: f.account.Email, Role: domain.RoleMember}

	own, err := family.Export(ctx, p, f.paula.ID)
	if err != nil || own.Account == nil {
		t.Fatalf("own: %+v, %v", own.Account, err)
	}

	// The kid has an account of its own: the parent exports it without.
	kidAccount := w.account(f.kid, "kai@example.org")

	kid, err := family.Export(ctx, p, f.kid.ID)
	if err != nil || kid.Person.ID != f.kid.ID || kid.Account != nil {
		t.Errorf("dependent %+v, %+v, %v", kid.Person, kid.Account, err)
	}

	kidSelf := service.Principal{AccountID: kidAccount.ID, PersonID: f.kid.ID, Role: domain.RoleMember}
	if x, err := family.Export(ctx, kidSelf, f.kid.ID); err != nil || x.Account == nil {
		t.Errorf("the kid itself: %+v, %v", x.Account, err)
	}

	// Anyone else is unknown: the grandmother, a stranger.
	for _, id := range []domain.PersonID{f.grandma.ID, w.office.PersonID} {
		if _, err := family.Export(ctx, p, id); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("export %v: %v", id, err)
		}
	}
}

// adminFixture is two admins and a member, each with a session.
type adminFixture struct {
	admin, other service.Principal
	member       domain.Account
	session      domain.Session
}

func newAdminFixture(w *world) adminFixture {
	var f adminFixture

	for _, who := range []struct {
		p     *service.Principal
		given string
	}{{&f.admin, "Ada"}, {&f.other, "Abe"}} {
		person := w.person(who.given, "Admin", who.given+"@example.org")
		acc := w.account(person, who.given+"@example.org")
		acc.Role = domain.RoleAdmin
		w.write(func(ctx context.Context, tx store.Tx) error { return tx.Accounts().Update(ctx, acc) })
		*who.p = service.Principal{AccountID: acc.ID, PersonID: person.ID, Email: acc.Email, Role: domain.RoleAdmin}
	}

	f.member = w.account(w.person("Mia", "Member", ""), "mia@example.org")
	f.session = domain.Session{
		ID: newID[domain.SessionID](w), AccountID: f.member.ID, TokenHash: domain.HashToken("mia"),
		CreatedAt: now.Add(-time.Hour), LastSeenAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour),
	}
	w.write(func(ctx context.Context, tx store.Tx) error { return tx.Sessions().Create(ctx, f.session) })

	return f
}

func (w *world) auth() *service.AuthService {
	w.t.Helper()

	r, err := mail.NewRenderer()
	if err != nil {
		w.t.Fatal(err)
	}

	hasher, err := password.NewWithParams(password.Params{Memory: 8, Time: 1, Threads: 1, SaltLen: 16, KeyLen: 32}, nil)
	if err != nil {
		w.t.Fatal(err)
	}

	return service.NewAuthService(w.st, clock.NewFixed(now), w.seq, hasher, r, "https://school.example.org/")
}

func (w *world) account1(id domain.AccountID) (domain.Account, error) {
	w.t.Helper()

	var a domain.Account

	err := w.st.InTx(w.t.Context(), store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		var err error
		a, err = tx.Accounts().Get(ctx, id)

		return err
	})

	return a, err
}

func (w *world) audit() []domain.AuditEvent {
	return read(w, func(ctx context.Context, tx store.Tx) ([]domain.AuditEvent, error) { return tx.Audit().List(ctx, 50) })
}

func accountAdmin(t *testing.T, w *world) {
	ctx := t.Context()
	f := newAdminFixture(w)
	svc := w.auth()

	e, err := svc.SetAccountDisabled(ctx, f.admin, f.member.ID, true)
	if err != nil || !e.Account.DisabledAt.Equal(now) || e.Person.GivenName != "Mia" {
		t.Fatalf("disable: %+v, %v", e, err)
	}

	err = w.st.InTx(ctx, store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		_, err := tx.Sessions().GetByToken(ctx, f.session.TokenHash)

		return err
	})
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("the disabled account's session: %v", err)
	}

	// Disabling again changes nothing and is not audited.
	if _, err := svc.SetAccountDisabled(ctx, f.admin, f.member.ID, true); err != nil {
		t.Fatal(err)
	}

	if e, err := svc.SetAccountDisabled(ctx, f.admin, f.member.ID, false); err != nil || !e.Account.DisabledAt.IsZero() {
		t.Fatalf("enable: %+v, %v", e, err)
	}

	if _, err := svc.SetAccountRole(ctx, f.admin, f.member.ID, domain.RoleGuest); !errors.Is(err, domain.ErrValidation) {
		t.Errorf("guest role: %v", err)
	}

	if e, err := svc.SetAccountRole(ctx, f.admin, f.member.ID, domain.RoleInstructor); err != nil ||
		e.Account.Role != domain.RoleInstructor {
		t.Fatalf("role: %+v, %v", e, err)
	}

	if err := svc.DeleteAccount(ctx, f.admin, f.member.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}

	if _, err := w.account1(f.member.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("deleted account: %v", err)
	}

	if p := w.get(f.member.PersonID); p.GivenName != "Mia" {
		t.Errorf("the person stays: %+v", p)
	}

	// Newest first. The events that targeted the deleted account target its
	// person now.
	events := w.audit()
	actions := mapIDs(events, func(e domain.AuditEvent) domain.AuditAction { return e.Action })

	want := []domain.AuditAction{
		domain.AuditAccountDeleted, domain.AuditAccountRoleChanged, domain.AuditAccountEnabled, domain.AuditAccountDisabled,
	}
	if !slices.Equal(actions, want) {
		t.Fatalf("audit %v, want %v", actions, want)
	}

	for _, e := range events {
		if e.ActorID != f.admin.AccountID || e.TargetKind != domain.AuditTargetPerson || e.TargetPersonID != f.member.PersonID {
			t.Errorf("event %+v", e)
		}
	}

	if d := events[1].Details; d.PreviousRole != domain.RoleMember || d.Role != domain.RoleInstructor {
		t.Errorf("role change details %+v", d)
	}
}

func accountAdminGuards(t *testing.T, w *world) {
	ctx := t.Context()
	f := newAdminFixture(w)
	svc := w.auth()

	// Their own account.
	if _, err := svc.SetAccountDisabled(ctx, f.admin, f.admin.AccountID, true); !errors.Is(err, service.ErrOwnAccount) {
		t.Errorf("disable own: %v", err)
	}

	if _, err := svc.SetAccountRole(ctx, f.admin, f.admin.AccountID, domain.RoleMember); !errors.Is(err, service.ErrOwnAccount) {
		t.Errorf("demote own: %v", err)
	}

	if err := svc.DeleteAccount(ctx, f.admin, f.admin.AccountID); !errors.Is(err, service.ErrOwnAccount) {
		t.Errorf("delete own: %v", err)
	}

	// Another admin, while there are two: fine.
	if _, err := svc.SetAccountDisabled(ctx, f.admin, f.other.AccountID, true); err != nil {
		t.Fatal(err)
	}

	// Now f.admin is the last enabled admin. Whoever acts on it (here the
	// disabled admin, as if two requests had raced) is refused.
	if _, err := svc.SetAccountDisabled(ctx, f.other, f.admin.AccountID, true); !errors.Is(err, service.ErrLastAdmin) {
		t.Errorf("disable the last admin: %v", err)
	}

	if _, err := svc.SetAccountRole(ctx, f.other, f.admin.AccountID, domain.RoleOffice); !errors.Is(err, service.ErrLastAdmin) {
		t.Errorf("demote the last admin: %v", err)
	}

	if err := svc.DeleteAccount(ctx, f.other, f.admin.AccountID); !errors.Is(err, service.ErrLastAdmin) {
		t.Errorf("delete the last admin: %v", err)
	}

	// Promoting is never refused; and an account that acted is kept.
	if _, err := svc.SetAccountRole(ctx, f.admin, f.member.ID, domain.RoleAdmin); err != nil {
		t.Errorf("promote: %v", err)
	}

	if err := svc.DeleteAccount(ctx, f.admin, f.other.AccountID); err != nil {
		t.Errorf("delete the disabled admin, who never acted: %v", err)
	}

	if err := svc.DeleteAccount(ctx, f.admin, f.member.ID); err != nil {
		t.Fatalf("delete the promoted member: %v", err)
	}

	w.receipt(w.get(f.admin.PersonID))

	if err := svc.DeleteAccount(ctx, f.admin, w.office.AccountID); !errors.Is(err, service.ErrAccountKept) {
		t.Errorf("delete the office, which issued a receipt: %v", err)
	}

	if _, err := svc.SetAccountDisabled(ctx, f.admin, newID[domain.AccountID](w), true); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("unknown: %v", err)
	}
}

func deleteOwnAccount(t *testing.T, w *world) {
	ctx := t.Context()
	svc := w.auth()
	f := newAdminFixture(w)

	hash, err := password.NewWithParams(password.Params{Memory: 8, Time: 1, Threads: 1, SaltLen: 16, KeyLen: 32}, nil)
	if err != nil {
		t.Fatal(err)
	}

	setPassword := func(acc domain.AccountID) { w.setPassword(hash, acc) }

	setPassword(f.member.ID)
	member := service.Principal{
		AccountID: f.member.ID, PersonID: f.member.PersonID, Email: f.member.Email, Role: domain.RoleMember,
		SessionID: f.session.ID,
	}

	if err := svc.DeleteOwnAccount(ctx, member, "wrong", ""); !errors.Is(err, service.ErrWrongPassword) {
		t.Errorf("wrong password: %v", err)
	}

	impersonated := member
	impersonated.ImpersonatorID = f.admin.AccountID

	if err := svc.DeleteOwnAccount(ctx, impersonated, "correct horse", ""); !errors.Is(err, service.ErrImpersonating) {
		t.Errorf("impersonating: %v", err)
	}

	if err := svc.DeleteOwnAccount(ctx, member, "correct horse", "de"); err != nil {
		t.Fatalf("delete: %v", err)
	}

	if _, err := w.account1(f.member.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("account: %v", err)
	}

	mails := read(w, func(ctx context.Context, tx store.Tx) ([]domain.OutboxMail, error) {
		return tx.Outbox().List(ctx, store.OutboxFilter{Template: mail.TemplateAccountDeleted, Limit: 10})
	})
	if len(mails) != 1 || mails[0].To != "mia@example.org" || mails[0].Subject != "Dein Konto bei "+w.settings.SchoolName+" wurde gelöscht" {
		t.Errorf("mails %+v", mails)
	}

	events := w.audit()
	if len(events) != 1 || events[0].Action != domain.AuditAccountDeleted || !events[0].ActorID.IsZero() ||
		events[0].TargetPersonID != f.member.PersonID {
		t.Errorf("audit %+v", events)
	}

	// The last enabled admin cannot leave.
	if _, err := svc.SetAccountDisabled(ctx, f.admin, f.other.AccountID, true); err != nil {
		t.Fatal(err)
	}

	setPassword(f.admin.AccountID)

	if err := svc.DeleteOwnAccount(ctx, f.admin, "correct horse", ""); !errors.Is(err, service.ErrLastAdmin) {
		t.Errorf("the last admin: %v", err)
	}
}

// setPassword gives the account id the password "correct horse".
func (w *world) setPassword(hash *password.Argon2id, id domain.AccountID) {
	w.t.Helper()

	h, err := hash.Hash("correct horse")
	if err != nil {
		w.t.Fatal(err)
	}

	w.write(func(ctx context.Context, tx store.Tx) error {
		return tx.Credentials().Put(ctx, domain.Credential{AccountID: id, PasswordHash: h, ChangedAt: now})
	})
}

// lastAdminSignsIn: an admin account that cannot sign in (here one not
// confirmed yet) does not count for the last-admin rule.
func lastAdminSignsIn(t *testing.T, w *world) {
	ctx := t.Context()
	f := newAdminFixture(w)
	svc := w.auth()

	abe, err := w.account1(f.other.AccountID)
	if err != nil {
		t.Fatal(err)
	}

	abe.ConfirmedAt = time.Time{}
	w.write(func(ctx context.Context, tx store.Tx) error { return tx.Accounts().Update(ctx, abe) })

	// Ada is the last admin who can sign in. Whoever acts on her (here
	// Abe, as if he had signed in before) is refused.
	if _, err := svc.SetAccountDisabled(ctx, f.other, f.admin.AccountID, true); !errors.Is(err, service.ErrLastAdmin) {
		t.Errorf("disable: %v", err)
	}

	if _, err := svc.SetAccountRole(ctx, f.other, f.admin.AccountID, domain.RoleOffice); !errors.Is(err, service.ErrLastAdmin) {
		t.Errorf("demote: %v", err)
	}

	if err := svc.DeleteAccount(ctx, f.other, f.admin.AccountID); !errors.Is(err, service.ErrLastAdmin) {
		t.Errorf("delete: %v", err)
	}

	if _, err := w.persons().Anonymize(ctx, f.other, f.admin.PersonID); !errors.Is(err, service.ErrLastAdmin) {
		t.Errorf("anonymize her person: %v", err)
	}

	hash, err := password.NewWithParams(password.Params{Memory: 8, Time: 1, Threads: 1, SaltLen: 16, KeyLen: 32}, nil)
	if err != nil {
		t.Fatal(err)
	}

	w.setPassword(hash, f.admin.AccountID)

	if err := svc.DeleteOwnAccount(ctx, f.admin, "correct horse", ""); !errors.Is(err, service.ErrLastAdmin) {
		t.Errorf("delete her own: %v", err)
	}

	// Abe never counted, so disabling him is fine.
	if _, err := svc.SetAccountDisabled(ctx, f.admin, f.other.AccountID, true); err != nil {
		t.Errorf("disable the unconfirmed admin: %v", err)
	}

	if acc, err := w.account1(f.admin.AccountID); err != nil || acc.Role != domain.RoleAdmin || !acc.DisabledAt.IsZero() {
		t.Errorf("Ada: %+v, %v", acc, err)
	}
}

// impersonationsEnd: disabling or deleting an account ends the
// impersonations of it, and each one that was running is audited as ended
// by the admin who started it, as signing out of it is.
func impersonationsEnd(t *testing.T, w *world) {
	ctx := t.Context()
	f := newAdminFixture(w)
	svc := w.auth()

	impersonate := func(startedAt time.Time) {
		s := domain.Session{
			ID: newID[domain.SessionID](w), AccountID: f.member.ID, TokenHash: domain.HashToken(startedAt.String()),
			CreatedAt: startedAt, LastSeenAt: startedAt, ExpiresAt: startedAt.Add(service.ImpersonationMaxAge),
			ImpersonatorID: f.admin.AccountID,
		}
		w.write(func(ctx context.Context, tx store.Tx) error { return tx.Sessions().Create(ctx, s) })
	}
	ended := func() []domain.AuditEvent {
		return slices.DeleteFunc(w.audit(), func(e domain.AuditEvent) bool {
			return e.Action != domain.AuditImpersonationEnded
		})
	}

	// A running impersonation, and one past its end, which ended then.
	impersonate(now.Add(-time.Minute))
	impersonate(now.Add(-2 * service.ImpersonationMaxAge))

	if _, err := svc.SetAccountDisabled(ctx, f.other, f.member.ID, true); err != nil {
		t.Fatal(err)
	}

	got := ended()
	if len(got) != 1 || got[0].ActorID != f.admin.AccountID || got[0].TargetKind != domain.AuditTargetAccount ||
		got[0].TargetID != f.member.ID {
		t.Fatalf("after disabling: %+v", got)
	}

	if _, err := svc.SetAccountDisabled(ctx, f.other, f.member.ID, false); err != nil {
		t.Fatal(err)
	}

	impersonate(now.Add(-time.Minute))

	if err := svc.DeleteAccount(ctx, f.other, f.member.ID); err != nil {
		t.Fatal(err)
	}

	// Both target the member's person now that the account is gone.
	got = ended()
	if len(got) != 2 {
		t.Fatalf("after deleting: %+v", got)
	}

	for _, e := range got {
		if e.ActorID != f.admin.AccountID || e.TargetKind != domain.AuditTargetPerson ||
			e.TargetPersonID != f.member.PersonID {
			t.Errorf("event %+v", e)
		}

		if err := e.Validate(); err != nil {
			t.Errorf("event %+v: %v", e, err)
		}
	}
}

func retention(t *testing.T, w *world) {
	ctx := t.Context()
	cutoff := now.AddDate(0, -service.GuestRetentionMonths, 0)

	// Lessons of an hour: ended just before the cutoff, and just at it.
	old := w.lesson(domain.ClassID{}, cutoff.Add(-time.Hour-time.Microsecond))
	edge := w.lesson(domain.ClassID{}, cutoff.Add(-time.Hour))

	guest := func(l domain.Lesson) domain.Booking {
		b := domain.Booking{
			ID: newID[domain.BookingID](w), LessonID: l.ID, GuestName: "Gina Guest", GuestEmail: "gina@example.org",
			Status: domain.BookingAttended, Source: domain.BookingSourceStaff, BookedAt: l.StartsAt.Add(-time.Hour),
		}
		w.write(func(ctx context.Context, tx store.Tx) error { return tx.Bookings().Create(ctx, b) })

		return b
	}

	gone, kept := guest(old), guest(edge)

	auditAt := func(at time.Time) {
		w.write(func(ctx context.Context, tx store.Tx) error {
			return tx.Audit().Create(ctx, domain.AuditEvent{
				ID: newID[domain.AuditEventID](w), At: at, ActorID: w.office.AccountID, Action: domain.AuditPersonAnonymized,
				TargetKind: domain.AuditTargetPerson, TargetPersonID: w.office.PersonID,
			})
		})
	}
	auditAt(now.AddDate(-service.AuditRetentionYears, 0, 0).Add(-time.Microsecond))
	auditAt(now.AddDate(-service.AuditRetentionYears, 0, 0))

	runAt := func(at time.Time) {
		w.write(func(ctx context.Context, tx store.Tx) error {
			return tx.Imports().Create(ctx, domain.ImportRun{
				ID: newID[domain.ImportRunID](w), FileName: "members.csv", StartedAt: at, FinishedAt: at.Add(time.Second),
				Counts: domain.ImportCounts{Rows: 1, Unchanged: 1},
			})
		})
	}
	runAt(now.AddDate(-service.ImportRetentionYears, 0, 0).Add(-time.Microsecond))
	runAt(now.AddDate(-service.ImportRetentionYears, 0, 0))

	// Push subscriptions unused for six months, and push messages of a
	// week ago (ADR 0017).
	subAt := func(endpoint string, used time.Time) domain.PushSubscription {
		sub := domain.PushSubscription{
			ID: newID[domain.PushSubscriptionID](w), AccountID: w.office.AccountID,
			Endpoint: "https://push.example.net/" + endpoint, P256DH: [65]byte{4}, Auth: [16]byte{1},
			CreatedAt: used, LastUsedAt: used,
		}
		w.write(func(ctx context.Context, tx store.Tx) error { return tx.PushSubscriptions().Save(ctx, sub) })

		return sub
	}
	subAt("old", now.AddDate(0, -service.PushSubscriptionRetentionMonths, 0).Add(-time.Microsecond))
	recentSub := subAt("recent", now.AddDate(0, -service.PushSubscriptionRetentionMonths, 0))

	messageAt := func(at time.Time) {
		w.write(func(ctx context.Context, tx store.Tx) error {
			return tx.PushOutbox().Create(ctx, domain.PushMessage{
				ID: newID[domain.PushMessageID](w), SubscriptionID: recentSub.ID, Payload: `{"title":"x"}`,
				CreatedAt: at, ExpiresAt: at.Add(time.Hour), SentAt: at.Add(time.Minute),
			})
		})
	}
	messageAt(now.Add(-service.PushRetention).Add(-time.Microsecond))
	messageAt(now.Add(-service.PushRetention))

	svc := service.NewRetentionService(w.st, clock.NewFixed(now))

	n, err := svc.Purge(ctx)
	if err != nil || n != (service.Purged{
		Guests: 1, AuditEvents: 1, ImportRuns: 1, PushSubscriptions: 1, PushMessages: 1,
	}) {
		t.Fatalf("purge: %+v, %v", n, err)
	}

	got := w.bookingsByID(gone, kept)
	if b := got[gone.ID]; b.GuestName != domain.AnonymousGuestName || b.GuestEmail != "" || b.Status != domain.BookingAttended {
		t.Errorf("old guest %+v", b)
	}

	if b := got[kept.ID]; b != kept {
		t.Errorf("recent guest %+v", b)
	}

	if n, err := svc.Purge(ctx); err != nil || n != (service.Purged{}) {
		t.Errorf("again: %+v, %v", n, err)
	}
}
