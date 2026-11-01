package memory

import (
	"bytes"
	"cmp"
	"context"
	"slices"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// The reads of the integrity checks (db check). The memory store computes
// them from its tables; PostgreSQL answers each with one query.

func (r passRepo) ListUnbalanced(context.Context) ([]store.PassBalance, error) {
	s := &r.tx.state

	redeemed := map[domain.PassID]int{}
	for _, red := range s.redemptions.rows {
		redeemed[red.PassID]++
	}

	var out []store.PassBalance

	for _, p := range s.passes.rows {
		used := p.VisitsTotal - p.VisitsRemaining
		if p.Kind == domain.PassVisits && used != redeemed[p.ID] {
			out = append(out, store.PassBalance{Pass: p.ID, Used: used, Redeemed: redeemed[p.ID]})
		}
	}

	slices.SortFunc(out, func(a, b store.PassBalance) int { return bytes.Compare(a.Pass[:], b.Pass[:]) })

	return out, nil
}

func (r redemptionRepo) ListStray(context.Context) ([]store.StrayRedemption, error) {
	s := &r.tx.state

	var out []store.StrayRedemption

	for _, red := range s.redemptions.rows {
		b, _ := s.bookings.get(red.BookingID)
		l, _ := s.lessons.get(b.LessonID)

		stray := store.StrayRedemption{
			Redemption:     red,
			Lesson:         l.ID,
			CanceledInTime: b.Status == domain.BookingCanceled && !b.Late,
			LessonCanceled: l.Status == domain.LessonCanceled,
		}
		if stray.CanceledInTime || stray.LessonCanceled {
			out = append(out, stray)
		}
	}

	slices.SortFunc(out, func(a, b store.StrayRedemption) int {
		return bytes.Compare(a.Redemption.BookingID[:], b.Redemption.BookingID[:])
	})

	return out, nil
}

func (r bookingRepo) ListOverCapacity(context.Context) ([]store.LessonOccupancy, error) {
	s := &r.tx.state

	overridden := map[domain.BookingID]bool{}

	for _, e := range s.bookingEvents.rows {
		if e.Override {
			overridden[e.BookingID] = true
		}
	}

	byLesson := map[domain.LessonID]*store.LessonOccupancy{}

	for _, b := range s.bookings.rows {
		if !active(b) {
			continue
		}

		o := byLesson[b.LessonID]
		if o == nil {
			l, _ := s.lessons.get(b.LessonID)
			o = &store.LessonOccupancy{Lesson: l.ID, Capacity: l.Capacity}
			byLesson[b.LessonID] = o
		}

		o.Active++
		if overridden[b.ID] {
			o.Overrides++
		}
	}

	var out []store.LessonOccupancy

	for _, o := range byLesson {
		if o.Active > o.Capacity {
			out = append(out, *o)
		}
	}

	slices.SortFunc(out, func(a, b store.LessonOccupancy) int { return bytes.Compare(a.Lesson[:], b.Lesson[:]) })

	return out, nil
}

func (r receiptCounterRepo) ListStates(context.Context) ([]store.ReceiptCounterState, error) {
	s := &r.tx.state

	byYear := map[int]*store.ReceiptCounterState{}
	state := func(year int) *store.ReceiptCounterState {
		if byYear[year] == nil {
			byYear[year] = &store.ReceiptCounterState{Year: year}
		}

		return byYear[year]
	}

	for _, c := range s.receiptCounters.rows {
		state(c.Year).Last = c.Last
	}

	for _, rc := range s.receipts.rows {
		st := state(rc.Year)
		st.Highest = max(st.Highest, rc.Sequence)
	}

	out := make([]store.ReceiptCounterState, 0, len(byYear))
	for _, st := range byYear {
		out = append(out, *st)
	}

	slices.SortFunc(out, func(a, b store.ReceiptCounterState) int { return cmp.Compare(a.Year, b.Year) })

	return out, nil
}

func (r personRepo) ListTombstoneReferences(context.Context) ([]store.TombstoneReference, error) {
	s := &r.tx.state

	tombs := map[domain.PersonID]domain.Person{}

	for _, p := range s.persons.rows {
		if p.Merged() || p.Anonymized() {
			tombs[p.ID] = p
		}
	}

	var out []store.TombstoneReference

	// add adds the reference of the record ref to the person id if id is a
	// tombstone and keep accepts it.
	add := func(id domain.PersonID, kind store.TombstoneRefKind, ref string, keep func(t domain.Person) bool) {
		if t, ok := tombs[id]; ok && keep(t) {
			out = append(out, store.TombstoneReference{Person: id, Anonymized: !t.Merged(), Kind: kind, Ref: ref})
		}
	}
	always := func(domain.Person) bool { return true }
	// soldAfter: any pass of a merged person, one sold after the
	// anonymization of an anonymized one.
	soldAfter := func(p domain.Pass) func(domain.Person) bool {
		return func(t domain.Person) bool { return t.Merged() || p.SoldAt.After(t.AnonymizedAt) }
	}

	for _, b := range s.bookings.rows {
		l, _ := s.lessons.get(b.LessonID)
		add(b.PersonID, store.TombstoneBooking, b.ID.String(), func(t domain.Person) bool {
			return t.Merged() || (active(b) && l.StartsAt.After(t.AnonymizedAt))
		})
	}

	for _, e := range s.waitlist.rows {
		if e.Open() {
			add(e.PersonID, store.TombstoneWaitlist, e.ID.String(), always)
		}
	}

	for _, m := range s.classMembers.rows {
		add(m.PersonID, store.TombstoneMember, m.ClassID.String(), always)
	}

	for g := range s.guardians.rows {
		add(g.GuardianID, store.TombstoneGuardian, g.DependentID.String(), always)
		add(g.DependentID, store.TombstoneDependent, g.GuardianID.String(), always)
	}

	for _, p := range s.passes.rows {
		add(p.PersonID, store.TombstonePass, p.ID.String(), soldAfter(p))
		add(p.PayerID, store.TombstonePaidPass, p.ID.String(), soldAfter(p))
	}

	for _, st := range s.staff.rows {
		add(st.PersonID, store.TombstoneStaff, st.ID.String(), always)
	}

	for _, a := range s.accounts.rows {
		add(a.PersonID, store.TombstoneAccount, a.ID.String(), func(t domain.Person) bool {
			return !t.Merged() || a.DisabledAt.IsZero()
		})
	}

	for _, d := range s.dismissals.rows {
		add(d.PersonA, store.TombstoneDismissal, d.PersonB.String(), always)
		add(d.PersonB, store.TombstoneDismissal, d.PersonA.String(), always)
	}

	for _, p := range s.persons.rows {
		add(p.MergedInto, store.TombstoneMerged, p.ID.String(), domain.Person.Merged)
	}

	kinds := store.TombstoneRefKinds()

	slices.SortFunc(out, func(a, b store.TombstoneReference) int {
		return cmp.Or(
			bytes.Compare(a.Person[:], b.Person[:]),
			cmp.Compare(slices.Index(kinds, a.Kind), slices.Index(kinds, b.Kind)),
			cmp.Compare(a.Ref, b.Ref),
		)
	})

	return out, nil
}
