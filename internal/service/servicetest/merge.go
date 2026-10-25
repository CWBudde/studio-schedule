package servicetest

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/service"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// Merge runs the merge scenarios (ADR 0015) on fresh stores from newStore.
func Merge(t *testing.T, newStore func(t *testing.T) store.Store) {
	t.Helper()

	for name, fn := range map[string]func(t *testing.T, w *world){
		"MovesEverything":        mergeMovesEverything,
		"MovesTheAccount":        mergeMovesTheAccount,
		"RefusesTwoAccounts":     mergeRefusesTwoAccounts,
		"RefusesACycle":          mergeRefusesACycle,
		"RefusesTombstones":      mergeRefusesTombstones,
		"KeepsHistory":           mergeKeepsHistory,
		"FollowsMembership":      mergeFollowsMembership,
		"MovesDismissals":        mergeMovesDismissals,
		"NotesTooLong":           mergeNotesTooLong,
		"TombstonesTakeNoWrites": mergeTombstonesTakeNoWrites,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fn(t, newWorld(t, newStore(t)))
		})
	}
}

// mergeFixture is Sara twice: the survivor with an account, and a duplicate
// from an import with a member number, a guardian, a dependent, bookings,
// a waiting-list entry, class memberships, a pass, a pass it paid for and a
// receipt.
type mergeFixture struct {
	survivor, merged, guardian, kid domain.Person

	survivorBooking            domain.Booking
	conflict, moving, canceled domain.Booking
	pass, paid                 domain.Pass
	receipt                    domain.Receipt
}

func newMergeFixture(w *world) mergeFixture {
	var f mergeFixture

	f.survivor = w.person("Sara", "Example", "sara@example.org")
	w.account(f.survivor, "sara@example.org")

	f.merged = w.person("Sara", "Example", "SARA@example.org")
	f.merged.MemberNo, f.merged.Notes, f.merged.Phone = "M-0007", "Imported.", "+49 000 0000007"
	f.merged = w.update(f.merged)

	f.guardian = w.person("Gerd", "Example", "")
	f.kid = w.person("Kim", "Example", "")
	w.link(f.guardian, f.merged)
	w.link(f.merged, f.kid)

	// Lesson 0: both are booked, the survivor's booking wins. Lesson 1:
	// only the duplicate. Lesson 2: the duplicate canceled, and waits.
	f.survivorBooking = w.booking(w.lessons[0], f.survivor, false)
	f.conflict = w.booking(w.lessons[0], f.merged, false)
	f.moving = w.booking(w.lessons[1], f.merged, false)
	f.canceled = w.booking(w.lessons[2], f.merged, true)

	w.write(func(ctx context.Context, tx store.Tx) error {
		return tx.Waitlist().Create(ctx, domain.WaitlistEntry{
			ID: newID[domain.WaitlistEntryID](w), LessonID: w.lessons[2].ID, PersonID: f.merged.ID,
			Status: domain.WaitlistWaiting, JoinedAt: now.Add(-30 * time.Minute),
		})
	})

	// Both are members of the first class; the duplicate of the second too.
	w.member(w.classes[0], f.survivor)
	w.member(w.classes[0], f.merged)
	w.member(w.classes[1], f.merged)

	// The duplicate's pass paid for both its active bookings.
	f.pass = w.pass(f.merged, domain.Person{}, 8)
	w.redeem(f.pass, f.conflict)
	w.redeem(f.pass, f.moving)

	// It paid for the kid's pass, and has a receipt.
	f.paid = w.pass(f.kid, f.merged, 10)
	f.receipt = w.receipt(f.merged)

	return f
}

func mergeMovesEverything(t *testing.T, w *world) {
	ctx := t.Context()
	f := newMergeFixture(w)
	svc := w.persons()
	req := service.MergeRequest{
		SurvivorID: f.survivor.ID, MergedID: f.merged.ID, Take: service.MergeTake{MemberNo: true, Phone: true},
	}

	// The preview tells what the merge does, and changes nothing.
	preview, err := svc.Merge(ctx, w.office, req, false)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}

	wantSummary(t, preview, f)

	if p := w.get(f.merged.ID); p.Merged() || p.MemberNo != "M-0007" {
		t.Fatalf("the preview merged: %+v", p)
	}

	if got := w.passOf(f.pass.ID); got.VisitsRemaining != 8 || got.PersonID != f.merged.ID {
		t.Fatalf("the preview changed the pass: %+v", got)
	}

	sum, err := svc.Merge(ctx, w.office, req, true)
	if err != nil {
		t.Fatalf("merge: %v", err)
	}

	wantSummary(t, sum, f)

	// The survivor took the member number and the phone, and both notes;
	// the duplicate is a tombstone without them.
	s, m := w.get(f.survivor.ID), w.get(f.merged.ID)
	if s.MemberNo != "M-0007" || s.Phone != "+49 000 0000007" || s.Email != "sara@example.org" ||
		s.Notes != "Imported." || s.Merged() {
		t.Errorf("survivor %+v", s)
	}

	if m.MergedInto != f.survivor.ID || m.MemberNo != "" {
		t.Errorf("tombstone %+v", m)
	}

	if s != sum.Survivor || m != sum.Merged {
		t.Errorf("summary persons %+v / %+v, stored %+v / %+v", sum.Survivor, sum.Merged, s, m)
	}

	// The tombstone is gone from the lists and searches.
	listed := read(w, func(ctx context.Context, tx store.Tx) ([]domain.Person, error) { return tx.Persons().List(ctx) })
	if slices.ContainsFunc(listed, func(p domain.Person) bool { return p.ID == f.merged.ID }) {
		t.Error("the tombstone is listed")
	}

	found, err := svc.SearchPersons(ctx, "sara example")
	if err != nil || len(found) != 1 || found[0].ID != f.survivor.ID {
		t.Errorf("search: %v, %v", found, err)
	}

	// Every booking moved; the one the survivor had a booking of too is
	// canceled, timely, and its visit came back. The other stays paid.
	bookings := read(w, func(ctx context.Context, tx store.Tx) ([]domain.Booking, error) {
		return tx.Bookings().ListByIDs(ctx, []domain.BookingID{f.conflict.ID, f.moving.ID, f.canceled.ID, f.survivorBooking.ID})
	})
	for _, b := range bookings {
		if b.PersonID != f.survivor.ID {
			t.Errorf("booking %v stays with %v", b.ID, b.PersonID)
		}

		switch b.ID {
		case f.conflict.ID:
			if b.Status != domain.BookingCanceled || b.Late || !b.CanceledAt.Equal(now) {
				t.Errorf("the conflicting booking %+v", b)
			}
		case f.moving.ID, f.survivorBooking.ID:
			if b.Status != domain.BookingBooked {
				t.Errorf("booking %+v", b)
			}
		}
	}

	if got := w.passOf(f.pass.ID); got.VisitsRemaining != 9 || got.PersonID != f.survivor.ID {
		t.Errorf("pass %+v; want 9 visits, owned by the survivor", got)
	}

	paid := read(w, func(ctx context.Context, tx store.Tx) ([]domain.PassRedemption, error) {
		return tx.Redemptions().ListByBookings(ctx, []domain.BookingID{f.conflict.ID, f.moving.ID})
	})
	if len(paid) != 1 || paid[0].BookingID != f.moving.ID || paid[0].PassID != f.pass.ID {
		t.Errorf("redemptions %+v; want the moved booking's only", paid)
	}

	if got := w.passOf(f.paid.ID); got.PayerID != f.survivor.ID || got.PersonID != f.kid.ID {
		t.Errorf("paid pass %+v", got)
	}

	// The waiting-list entry moved.
	entries := read(w, func(ctx context.Context, tx store.Tx) ([]domain.WaitlistEntry, error) {
		return tx.Waitlist().ListOpenByPersons(ctx, []domain.PersonID{f.survivor.ID, f.merged.ID})
	})
	if len(entries) != 1 || entries[0].PersonID != f.survivor.ID || entries[0].LessonID != w.lessons[2].ID {
		t.Errorf("open entries %+v", entries)
	}

	// The memberships: the survivor's own stays, the other one moved.
	for person, want := range map[domain.PersonID][]domain.ClassID{
		f.survivor.ID: {w.classes[0], w.classes[1]}, f.merged.ID: nil,
	} {
		got := read(w, func(ctx context.Context, tx store.Tx) ([]domain.ClassMember, error) {
			return tx.Classes().ListMemberships(ctx, person)
		})
		if ids := mapIDs(got, func(m domain.ClassMember) domain.ClassID { return m.ClassID }); !slices.Equal(ids, want) {
			t.Errorf("memberships of %v: %v, want %v", person, ids, want)
		}
	}

	// The guardian links moved, in both directions.
	links := read(w, func(ctx context.Context, tx store.Tx) ([]domain.Guardian, error) {
		return tx.Persons().ListGuardianLinks(ctx, []domain.PersonID{f.survivor.ID, f.merged.ID, f.kid.ID})
	})
	if !slices.Equal(links, []domain.Guardian{
		{GuardianID: f.guardian.ID, DependentID: f.survivor.ID}, {GuardianID: f.survivor.ID, DependentID: f.kid.ID},
	}) {
		t.Errorf("links %+v", links)
	}

	// The receipt stays on the tombstone and is the survivor's.
	if got := read(w, func(ctx context.Context, tx store.Tx) (domain.Receipt, error) {
		return tx.Receipts().Get(ctx, f.receipt.ID)
	}); got.PayerID != f.merged.ID {
		t.Errorf("receipt payer %v", got.PayerID)
	}

	acc := read(w, func(ctx context.Context, tx store.Tx) (domain.Account, error) {
		return tx.Accounts().GetByPerson(ctx, f.survivor.ID)
	})
	receipts := service.NewReceiptService(w.st, clock.NewFixed(now), w.seq, service.Mails{}, nil)

	mine, err := receipts.Mine(ctx, service.Principal{AccountID: acc.ID, PersonID: f.survivor.ID, Role: acc.Role})
	if err != nil || len(mine) != 1 || mine[0].Receipt.ID != f.receipt.ID {
		t.Errorf("My receipts: %+v, %v", mine, err)
	}

	// The office's view names the tombstone, and the tombstone its survivor.
	d, err := svc.Person(ctx, f.survivor.ID)
	if err != nil || len(d.Merged) != 1 || d.Merged[0].ID != f.merged.ID {
		t.Errorf("survivor detail: merged %+v, %v", d.Merged, err)
	}

	if d, err := svc.Person(ctx, f.merged.ID); err != nil || d.MergedInto.ID != f.survivor.ID {
		t.Errorf("tombstone detail: %+v, %v", d.MergedInto, err)
	}

	// The merge is audited.
	events := read(w, func(ctx context.Context, tx store.Tx) ([]domain.AuditEvent, error) { return tx.Audit().List(ctx, 10) })
	if len(events) != 1 || events[0].Action != domain.AuditPersonMerged || events[0].TargetPersonID != f.survivor.ID ||
		events[0].Details.MergedPersonID != f.merged.ID || events[0].ActorID != w.office.AccountID {
		t.Errorf("audit %+v", events)
	}

	// A tombstone is not merged or edited again.
	_, err = svc.Merge(ctx, w.office, service.MergeRequest{SurvivorID: f.survivor.ID, MergedID: f.merged.ID}, true)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("merging the tombstone again: %v", err)
	}
}

func wantSummary(t *testing.T, sum service.MergeSummary, f mergeFixture) {
	t.Helper()

	if sum.BookingsMoved != 3 || len(sum.Canceled) != 1 || sum.Canceled[0].Lesson.ID != f.conflict.LessonID ||
		sum.Canceled[0].CourseType.Name != "Imaginary Ballet" {
		t.Errorf("bookings: %d moved, canceled %+v", sum.BookingsMoved, sum.Canceled)
	}

	if sum.WaitlistMoved != 1 || sum.WaitlistWithdrawn != 0 || sum.MembershipsMoved != 1 ||
		len(sum.MembershipsDropped) != 1 || sum.PassesMoved != 1 || sum.PaidPassesMoved != 1 ||
		sum.LinksMoved != 2 || sum.LinksDropped != 0 || sum.StaffMoved != 0 || sum.AccountMoved || sum.Receipts != 1 {
		t.Errorf("summary %+v", sum)
	}

	if sum.Survivor.MemberNo != "M-0007" || sum.Merged.MergedInto != f.survivor.ID {
		t.Errorf("summary persons %+v / %+v", sum.Survivor, sum.Merged)
	}
}

func mergeMovesTheAccount(t *testing.T, w *world) {
	ctx := t.Context()
	survivor := w.person("Tom", "Example", "")
	merged := w.person("Thomas", "Example", "tom@example.org")
	acc := w.account(merged, "tom@example.org")

	// The merged person is the survivor's guardian: that link goes.
	w.link(merged, survivor)

	sum, err := w.persons().Merge(ctx, w.office, service.MergeRequest{
		SurvivorID: survivor.ID, MergedID: merged.ID, Take: service.MergeTake{Name: true, Email: true},
	}, true)
	if err != nil {
		t.Fatal(err)
	}

	if !sum.AccountMoved || sum.LinksDropped != 1 || sum.LinksMoved != 0 {
		t.Errorf("summary %+v", sum)
	}

	got := read(w, func(ctx context.Context, tx store.Tx) (domain.Account, error) { return tx.Accounts().Get(ctx, acc.ID) })
	if got.PersonID != survivor.ID {
		t.Errorf("account of %v, want the survivor", got.PersonID)
	}

	if s := w.get(survivor.ID); s.GivenName != "Thomas" || s.Email != "tom@example.org" {
		t.Errorf("survivor %+v", s)
	}
}

func mergeRefusesTwoAccounts(t *testing.T, w *world) {
	ctx := t.Context()
	a, b := w.person("Ann", "Example", ""), w.person("Ann", "Example", "")
	w.account(a, "ann@example.org")
	other := w.account(b, "ann.example@example.org")

	for _, apply := range []bool{false, true} {
		_, err := w.persons().Merge(ctx, w.office, service.MergeRequest{SurvivorID: a.ID, MergedID: b.ID}, apply)
		if !errors.Is(err, service.ErrMergeTwoAccounts) {
			t.Errorf("apply %v: %v, want ErrMergeTwoAccounts", apply, err)
		}
	}

	if w.get(b.ID).Merged() {
		t.Error("merged anyway")
	}

	_, err := w.persons().Merge(ctx, w.office, service.MergeRequest{SurvivorID: a.ID, MergedID: a.ID}, true)

	var fe *domain.FieldError
	if !errors.As(err, &fe) || fe.Field != "mergedId" {
		t.Errorf("merging a person with itself: %v", err)
	}

	// Once the duplicate's account is disabled, the merge goes ahead; the
	// disabled account stays with the tombstone.
	other.DisabledAt = now
	w.write(func(ctx context.Context, tx store.Tx) error { return tx.Accounts().Update(ctx, other) })

	sum, err := w.persons().Merge(ctx, w.office, service.MergeRequest{SurvivorID: a.ID, MergedID: b.ID}, true)
	if err != nil || sum.AccountMoved {
		t.Fatalf("merge with a disabled account: %+v, %v", sum, err)
	}

	got := read(w, func(ctx context.Context, tx store.Tx) (domain.Account, error) {
		return tx.Accounts().Get(ctx, other.ID)
	})
	if got.PersonID != b.ID || !w.get(b.ID).Merged() {
		t.Errorf("the disabled account %+v, its person %+v", got, w.get(b.ID))
	}

	// The admins' account list still names its person.
	accounts, err := service.NewAuthService(w.st, clock.NewFixed(now), w.seq, nil, nil, "").Accounts(ctx)
	if err != nil || !slices.ContainsFunc(accounts, func(e service.AccountEntry) bool {
		return e.Account.ID == other.ID && e.Person.ID == b.ID
	}) {
		t.Errorf("accounts: %v", err)
	}
}

func mergeRefusesACycle(t *testing.T, w *world) {
	ctx := t.Context()

	// Dora is the survivor's guardian and the duplicate's dependent: after
	// the merge she would be both.
	survivor, merged, dora := w.person("Uma", "Example", ""), w.person("Uma", "Example", ""), w.person("Dora", "Example", "")
	w.link(dora, survivor)
	w.link(merged, dora)

	_, err := w.persons().Merge(ctx, w.office, service.MergeRequest{SurvivorID: survivor.ID, MergedID: merged.ID}, true)
	if !errors.Is(err, service.ErrMergeCycle) {
		t.Fatalf("got %v, want ErrMergeCycle", err)
	}

	links := read(w, func(ctx context.Context, tx store.Tx) ([]domain.Guardian, error) {
		return tx.Persons().ListGuardianLinks(ctx, []domain.PersonID{survivor.ID, dora.ID})
	})
	if len(links) != 2 {
		t.Errorf("links %+v; the refused merge changed them", links)
	}
}

func mergeRefusesTombstones(t *testing.T, w *world) {
	ctx := t.Context()
	svc := w.persons()
	a, b, c := w.person("Eva", "Example", ""), w.person("Eva", "Example", ""), w.person("Eva", "Example", "")

	if _, err := svc.Merge(ctx, w.office, service.MergeRequest{SurvivorID: b.ID, MergedID: a.ID}, true); err != nil {
		t.Fatal(err)
	}

	// Merging b on into c carries the tombstone a along.
	if _, err := svc.Merge(ctx, w.office, service.MergeRequest{SurvivorID: c.ID, MergedID: b.ID}, true); err != nil {
		t.Fatal(err)
	}

	if got := w.get(a.ID).MergedInto; got != c.ID {
		t.Errorf("tombstone a is merged into %v, want c", got)
	}

	for _, req := range []service.MergeRequest{
		{SurvivorID: a.ID, MergedID: c.ID}, {SurvivorID: c.ID, MergedID: a.ID},
	} {
		if _, err := svc.Merge(ctx, w.office, req, false); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("merge %+v: %v, want ErrNotFound", req, err)
		}
	}

	if _, err := svc.UpdatePerson(ctx, a.ID, service.PersonRecord{GivenName: "Eva", FamilyName: "X"}); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("update a tombstone: %v", err)
	}

	if err := svc.DeletePerson(ctx, a.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("delete a tombstone: %v", err)
	}
}

func (w *world) passOf(id domain.PassID) domain.Pass {
	w.t.Helper()

	return read(w, func(ctx context.Context, tx store.Tx) (domain.Pass, error) { return tx.Passes().Get(ctx, id) })
}

func newID[I domain.AnyID](w *world) I { return ids.New[I](w.seq) }

func mapIDs[T, I any](s []T, f func(T) I) []I {
	out := make([]I, 0, len(s))
	for _, v := range s {
		out = append(out, f(v))
	}

	return out
}

// lesson stores a scheduled lesson of an hour from start, of class c (zero
// for a one-off lesson).
func (w *world) lesson(c domain.ClassID, start time.Time) domain.Lesson {
	w.t.Helper()

	l := domain.Lesson{
		ID: newID[domain.LessonID](w), CourseTypeID: w.courseType, RoomID: w.room, InstructorID: w.instructor,
		StartsAt: start, EndsAt: start.Add(time.Hour), Status: domain.LessonScheduled,
		Visibility: domain.VisibilityPublic, Kind: domain.LessonRegular, Capacity: 5,
	}
	if !c.IsZero() {
		l.ClassID, l.SlotDate = c, domain.DateOf(start, time.UTC)
	}

	w.write(func(ctx context.Context, tx store.Tx) error { return tx.Lessons().Create(ctx, l) })

	return l
}

// bookingAs stores an active booking of p in l with the source and status.
func (w *world) bookingAs(l domain.Lesson, p domain.Person, source domain.BookingSource, status domain.BookingStatus) domain.Booking {
	w.t.Helper()

	b := domain.Booking{
		ID: newID[domain.BookingID](w), LessonID: l.ID, PersonID: p.ID, Status: status, Source: source,
		BookedAt: now.Add(-72 * time.Hour),
	}
	w.write(func(ctx context.Context, tx store.Tx) error { return tx.Bookings().Create(ctx, b) })

	return b
}

func (w *world) bookingsByID(bookings ...domain.Booking) map[domain.BookingID]domain.Booking {
	w.t.Helper()

	got := read(w, func(ctx context.Context, tx store.Tx) ([]domain.Booking, error) {
		return tx.Bookings().ListByIDs(ctx, mapIDs(bookings, func(b domain.Booking) domain.BookingID { return b.ID }))
	})

	out := make(map[domain.BookingID]domain.Booking, len(got))
	for _, b := range got {
		out[b.ID] = b
	}

	return out
}

// mergeKeepsHistory: where both have a booking of a lesson that has
// started, both stay: the duplicate's moves to the survivor as it was,
// attendance and paid visit included; of a lesson to come, the duplicate's
// is canceled and its visit comes back.
func mergeKeepsHistory(t *testing.T, w *world) {
	ctx := t.Context()
	survivor, merged := w.person("Pia", "Example", ""), w.person("Pia", "Example", "")

	past := w.lesson(domain.ClassID{}, now.Add(-48*time.Hour))
	running := w.lesson(domain.ClassID{}, now.Add(-30*time.Minute)) // started, not over
	ahead := w.lessons[0]

	own := w.bookingAs(past, survivor, domain.BookingSourceSelf, domain.BookingAttended)
	attended := w.bookingAs(past, merged, domain.BookingSourceSelf, domain.BookingAttended)
	w.bookingAs(running, survivor, domain.BookingSourceSelf, domain.BookingBooked)
	inRunning := w.bookingAs(running, merged, domain.BookingSourceSelf, domain.BookingBooked)
	w.bookingAs(ahead, survivor, domain.BookingSourceSelf, domain.BookingBooked)
	toCome := w.bookingAs(ahead, merged, domain.BookingSourceSelf, domain.BookingBooked)

	pass := w.pass(merged, domain.Person{}, 7)
	w.redeem(pass, attended)
	w.redeem(pass, inRunning)
	w.redeem(pass, toCome)

	sum, err := w.persons().Merge(ctx, w.office, service.MergeRequest{SurvivorID: survivor.ID, MergedID: merged.ID}, true)
	if err != nil {
		t.Fatal(err)
	}

	if sum.History != 2 || sum.BookingsMoved != 3 || len(sum.Canceled) != 1 || sum.Canceled[0].Lesson.ID != ahead.ID ||
		sum.Canceled[0].Reason != service.MergeCancelBooked {
		t.Errorf("summary: history %d, moved %d, canceled %+v", sum.History, sum.BookingsMoved, sum.Canceled)
	}

	got := w.bookingsByID(own, attended, inRunning, toCome)

	for _, b := range []domain.Booking{attended, inRunning} {
		if g := got[b.ID]; g.PersonID != survivor.ID || g.MergedFrom != merged.ID || g.Status != b.Status {
			t.Errorf("history booking %+v; want the survivor's, merged from the duplicate, status %s", g, b.Status)
		}
	}

	if g := got[own.ID]; g.Status != domain.BookingAttended || !g.MergedFrom.IsZero() {
		t.Errorf("the survivor's own booking %+v", g)
	}

	if g := got[toCome.ID]; g.Status != domain.BookingCanceled || g.Late || g.PersonID != survivor.ID {
		t.Errorf("the booking to come %+v; want canceled in time", g)
	}

	// Both stay active in the lesson that started; the visits they paid
	// stay paid, the canceled one's came back.
	active := read(w, func(ctx context.Context, tx store.Tx) (int, error) { return tx.Bookings().CountActive(ctx, past.ID) })
	if active != 2 {
		t.Errorf("active bookings of the past lesson: %d, want 2", active)
	}

	paid := read(w, func(ctx context.Context, tx store.Tx) ([]domain.PassRedemption, error) {
		return tx.Redemptions().ListByBookings(ctx, []domain.BookingID{attended.ID, inRunning.ID, toCome.ID})
	})
	if len(paid) != 2 || w.passOf(pass.ID).VisitsRemaining != 8 {
		t.Errorf("redemptions %+v, pass %+v; want the history's two paid, one visit back", paid, w.passOf(pass.ID))
	}
}

// mergeFollowsMembership: where both are members of a class, the
// duplicate's automatic bookings of lessons to come follow the survivor's
// membership: none on a date the survivor's pause covers or that it
// released. Its own bookings, and lessons that started, stay.
func mergeFollowsMembership(t *testing.T, w *world) {
	ctx := t.Context()
	survivor, merged := w.person("Ole", "Example", ""), w.person("Ole", "Example", "")
	c := w.classes[0]

	past := w.lesson(c, now.Add(-7*24*time.Hour))
	paused := w.lesson(c, now.Add(2*24*time.Hour))
	released := w.lesson(c, now.Add(9*24*time.Hour))
	active := w.lesson(c, now.Add(16*24*time.Hour))
	ownPaused := w.lesson(c, now.Add(24*time.Hour+3*time.Hour)) // paused too

	w.write(func(ctx context.Context, tx store.Tx) error {
		return errors.Join(
			tx.Classes().AddMember(ctx, domain.ClassMember{ClassID: c, PersonID: survivor.ID, PausedUntil: paused.SlotDate}),
			tx.Classes().AddMember(ctx, domain.ClassMember{ClassID: c, PersonID: merged.ID}),
		)
	})

	gaveUp := w.booking(released, survivor, true) // canceled and released

	inPast := w.bookingAs(past, merged, domain.BookingSourceMember, domain.BookingAttended)
	inPaused := w.bookingAs(paused, merged, domain.BookingSourceMember, domain.BookingBooked)
	inReleased := w.bookingAs(released, merged, domain.BookingSourceMember, domain.BookingBooked)
	inActive := w.bookingAs(active, merged, domain.BookingSourceMember, domain.BookingBooked)
	self := w.bookingAs(ownPaused, merged, domain.BookingSourceSelf, domain.BookingBooked)

	req := service.MergeRequest{SurvivorID: survivor.ID, MergedID: merged.ID}

	sum, err := w.persons().Merge(ctx, w.office, req, true)
	if err != nil {
		t.Fatal(err)
	}

	canceled := mapIDs(sum.Canceled, func(c service.MergeCanceled) domain.LessonID { return c.Lesson.ID })
	if !slices.Equal(canceled, []domain.LessonID{paused.ID, released.ID}) ||
		sum.Canceled[0].Reason != service.MergeCancelMembership || sum.Canceled[1].Reason != service.MergeCancelMembership {
		t.Errorf("canceled %+v; want the paused and the released date, by membership", sum.Canceled)
	}

	if len(sum.MembershipsDropped) != 1 || sum.MembershipsMoved != 0 {
		t.Errorf("memberships: dropped %+v, moved %d", sum.MembershipsDropped, sum.MembershipsMoved)
	}

	got := w.bookingsByID(inPast, inPaused, inReleased, inActive, self, gaveUp)
	for id, want := range map[domain.BookingID]domain.BookingStatus{
		inPast.ID: domain.BookingAttended, inPaused.ID: domain.BookingCanceled, inReleased.ID: domain.BookingCanceled,
		inActive.ID: domain.BookingBooked, self.ID: domain.BookingBooked, gaveUp.ID: domain.BookingCanceled,
	} {
		if g := got[id]; g.Status != want || g.PersonID != survivor.ID || g.Late || (g.Released && id != gaveUp.ID) {
			t.Errorf("booking %+v; want %s, the survivor's", g, want)
		}
	}
}

// mergeMovesDismissals: the dismissals naming the duplicate go to the
// survivor, but not the pair of the two, nor a pair the survivor has.
func mergeMovesDismissals(t *testing.T, w *world) {
	ctx := t.Context()
	svc := w.persons()
	survivor, merged := w.person("Ida", "Example", ""), w.person("Ida", "Example", "")
	x, y := w.person("Ida", "Example", ""), w.person("Ida", "Example", "")

	for _, pair := range [][2]domain.Person{{merged, x}, {merged, survivor}, {survivor, y}, {merged, y}} {
		if err := svc.DismissDuplicate(ctx, w.office, pair[0].ID, pair[1].ID); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := svc.Merge(ctx, w.office, service.MergeRequest{SurvivorID: survivor.ID, MergedID: merged.ID}, true); err != nil {
		t.Fatal(err)
	}

	got := read(w, func(ctx context.Context, tx store.Tx) ([]domain.DuplicateDismissal, error) {
		return tx.DuplicateDismissals().List(ctx)
	})

	pairs := mapIDs(got, func(d domain.DuplicateDismissal) [2]domain.PersonID { return [2]domain.PersonID{d.PersonA, d.PersonB} })
	for _, want := range [][2]domain.PersonID{
		pairOf(survivor.ID, x.ID), pairOf(survivor.ID, y.ID),
	} {
		if !slices.Contains(pairs, want) {
			t.Errorf("dismissals %v lack %v", pairs, want)
		}
	}

	if len(pairs) != 2 {
		t.Errorf("dismissals %v; want the survivor's two only", pairs)
	}

	// The pairs stay dismissed: the survivor is paired with nobody; x and
	// y, never dismissed, are.
	dups, err := svc.Duplicates(ctx)
	if err != nil {
		t.Fatal(err)
	}

	for _, p := range dups {
		if p.A.Person.ID == survivor.ID || p.B.Person.ID == survivor.ID {
			t.Errorf("a dismissed pair is listed: %v / %v", p.A.Person.ID, p.B.Person.ID)
		}
	}

	if !slices.ContainsFunc(dups, func(p service.DuplicatePair) bool {
		return pairOf(p.A.Person.ID, p.B.Person.ID) == pairOf(x.ID, y.ID)
	}) {
		t.Error("x and y are not paired")
	}
}

func pairOf(a, b domain.PersonID) [2]domain.PersonID {
	d := domain.NewDuplicateDismissal(a, b, domain.AccountID{}, time.Time{})

	return [2]domain.PersonID{d.PersonA, d.PersonB}
}

// mergeNotesTooLong: notes of both beyond the limit refuse the preview
// and the merge on notes; the office's edited notes go instead.
func mergeNotesTooLong(t *testing.T, w *world) {
	ctx := t.Context()
	svc := w.persons()
	survivor, merged := w.person("Uwe", "Example", ""), w.person("Uwe", "Example", "")
	survivor.Notes, merged.Notes = strings.Repeat("s", 1200), strings.Repeat("m", 1200)
	w.update(survivor)
	w.update(merged)

	req := service.MergeRequest{SurvivorID: survivor.ID, MergedID: merged.ID}
	for _, apply := range []bool{false, true} {
		_, err := svc.Merge(ctx, w.office, req, apply)
		wantField(t, err, "notes")
	}

	tooLong := strings.Repeat("x", domain.MaxNotesLength+1)
	req.Notes = &tooLong

	_, err := svc.Merge(ctx, w.office, req, false)
	wantField(t, err, "notes")

	edited := " Both: see the files. "
	req.Notes = &edited

	preview, err := svc.Merge(ctx, w.office, req, false)
	if err != nil || preview.Survivor.Notes != "Both: see the files." {
		t.Fatalf("preview with edited notes: %q, %v", preview.Survivor.Notes, err)
	}

	if _, err := svc.Merge(ctx, w.office, req, true); err != nil {
		t.Fatal(err)
	}

	if got := w.get(survivor.ID).Notes; got != "Both: see the files." {
		t.Errorf("notes %q", got)
	}
}

// mergeTombstonesTakeNoWrites: nothing new refers to a merged person: a
// pass, a payer, an account, a class membership, a staff link, a booking by
// staff, a guardian link.
func mergeTombstonesTakeNoWrites(t *testing.T, w *world) {
	ctx := t.Context()
	clk := clock.NewFixed(now)
	persons := w.persons()
	survivor, merged := w.person("Tia", "Example", ""), w.person("Tia", "Example", "")

	if _, err := persons.Merge(ctx, w.office, service.MergeRequest{SurvivorID: survivor.ID, MergedID: merged.ID}, true); err != nil {
		t.Fatal(err)
	}

	passes := service.NewPassService(w.st, clk, w.seq, nil)

	_, err := passes.Sell(ctx, w.office, merged.ID, service.PassSale{PassTypeID: w.passType})
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("sell to a tombstone: %v", err)
	}

	_, err = passes.Sell(ctx, w.office, survivor.ID, service.PassSale{PassTypeID: w.passType, PayerID: merged.ID})
	wantField(t, err, "payerId")

	if _, err := passes.Assign(ctx, w.office, merged.ID, service.PassAssignment{PassTypeID: w.passType}); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("assign to a tombstone: %v", err)
	}

	auth := service.NewAuthService(w.st, clk, w.seq, nil, nil, "")
	if _, err := auth.CreateAccount(ctx, w.office, merged.ID, service.NewAccount{
		Email: "tia@example.org", Role: domain.RoleMember,
	}); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("an account for a tombstone: %v", err)
	}

	_, err = service.NewTimetableService(w.st, clk, w.seq, service.Mails{}).AddClassMember(ctx, w.office,
		domain.ClassMember{ClassID: w.classes[0], PersonID: merged.ID})
	wantField(t, err, "personId")

	_, err = service.NewCatalogService(w.st, w.seq).CreateStaff(ctx, service.StaffData{
		DisplayName: "Tia", Kind: domain.StaffInstructor, PersonID: merged.ID, Active: true,
	})
	wantField(t, err, "personId")

	_, err = service.NewParticipantService(w.st, clk, w.seq, service.Mails{}).Add(ctx, w.office, w.lessons[1].ID,
		service.NewParticipant{PersonID: merged.ID})
	wantField(t, err, "personId")

	_, err = persons.AddGuardian(ctx, survivor.ID, merged.ID)
	wantField(t, err, "guardianId")

	if _, err := persons.AddGuardian(ctx, merged.ID, survivor.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("a guardian for a tombstone: %v", err)
	}

	if err := persons.DismissDuplicate(ctx, w.office, survivor.ID, merged.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("dismiss a pair with a tombstone: %v", err)
	}

	if got := w.get(merged.ID); got.MergedInto != survivor.ID {
		t.Errorf("tombstone %+v", got)
	}
}
