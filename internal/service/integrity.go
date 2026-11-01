package service

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// The integrity checks (db check, ADR 0020) compare what the services keep
// consistent and the database cannot enforce: the visits of a pass with its
// redemptions (ADR 0012), the active bookings of a lesson with its places
// (ADR 0009), merged and anonymized persons with what still refers to them
// (ADR 0015, ADR 0016), and the receipt counters with the numbers issued.
// They only read, in one read-only transaction, and find what a bug, a
// manual change in the database or a restore of a partial backup leaves
// behind. A school whose data only the services wrote has no finding.

// IntegrityCheck names a check.
type IntegrityCheck string

// The checks, in the order they run.
const (
	// CheckPasses: a visit pass's used visits equal its redemptions, and no
	// redemption pays for a booking that costs no visit: one canceled in
	// time or one of a lesson the school canceled (charges.due).
	CheckPasses IntegrityCheck = "passes"
	// CheckCapacity: no lesson has more active bookings than places, except
	// for bookings whose event records an override (ADR 0009).
	CheckCapacity IntegrityCheck = "capacity"
	// CheckTombstones: nothing refers to a merged person that the merge
	// moves, and nothing to an anonymized person that anonymizing ends or
	// the services refuse afterwards (store.TombstoneRefKind).
	CheckTombstones IntegrityCheck = "tombstones"
	// CheckReceiptCounters: each year's receipt counter is at least the
	// highest number issued that year, so the next receipt gets a new one.
	CheckReceiptCounters IntegrityCheck = "receipt-counters"
)

// IntegrityChecks returns the checks in their order.
func IntegrityChecks() []IntegrityCheck {
	return []IntegrityCheck{CheckPasses, CheckCapacity, CheckTombstones, CheckReceiptCounters}
}

// IntegrityEntity is the kind of record a finding is about.
type IntegrityEntity string

// The entities of findings.
const (
	EntityPass        IntegrityEntity = "pass"
	EntityBooking     IntegrityEntity = "booking"
	EntityLesson      IntegrityEntity = "lesson"
	EntityPerson      IntegrityEntity = "person"
	EntityReceiptYear IntegrityEntity = "receipt-year"
)

// IntegrityFinding is an inconsistency a check found. It names records by
// their IDs only, never by personal data.
type IntegrityFinding struct {
	Check IntegrityCheck
	// Entity and ID name the record: ID is the text form of its typed ID,
	// or the year of a receipt counter.
	Entity IntegrityEntity
	ID     string
	// Message says what is inconsistent.
	Message string
}

// IntegrityService runs the integrity checks.
type IntegrityService struct {
	store store.Store
}

// NewIntegrityService returns the integrity checks of st.
func NewIntegrityService(st store.Store) *IntegrityService {
	return &IntegrityService{store: st}
}

// Check runs the given checks, all of them without any, in their order
// (IntegrityChecks) and in one read-only transaction, and returns their
// findings, by check and then by record. An unknown check is a field error
// on checks, before anything is read.
func (s *IntegrityService) Check(ctx context.Context, checks ...IntegrityCheck) ([]IntegrityFinding, error) {
	run, err := SelectIntegrityChecks(checks...)
	if err != nil {
		return nil, err
	}

	var findings []IntegrityFinding

	err = s.store.InTx(ctx, store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		findings = nil

		for _, c := range run {
			found, err := integrityChecks[c](ctx, tx)
			if err != nil {
				return fmt.Errorf("check %s: %w", c, err)
			}

			findings = append(findings, found...)
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return findings, nil
}

// SelectIntegrityChecks returns the checks Check runs for checks, in their
// order: those given, each once, or all without any. An unknown check is a
// field error on checks, so a caller can refuse it before it opens the
// database.
func SelectIntegrityChecks(checks ...IntegrityCheck) ([]IntegrityCheck, error) {
	all := IntegrityChecks()
	if len(checks) == 0 {
		return all, nil
	}

	for _, c := range checks {
		if !slices.Contains(all, c) {
			names := make([]string, len(all))
			for i, a := range all {
				names[i] = string(a)
			}

			return nil, &domain.FieldError{
				Field: "checks", Msg: fmt.Sprintf("unknown check %q; the checks are %s", c, strings.Join(names, ", ")),
			}
		}
	}

	return slices.DeleteFunc(all, func(c IntegrityCheck) bool { return !slices.Contains(checks, c) }), nil
}

// integrityChecks are the checks by name.
var integrityChecks = map[IntegrityCheck]func(ctx context.Context, tx store.Tx) ([]IntegrityFinding, error){
	CheckPasses:          checkPasses,
	CheckCapacity:        checkCapacity,
	CheckTombstones:      checkTombstones,
	CheckReceiptCounters: checkReceiptCounters,
}

// finding is a finding of check about the record id of kind entity.
func finding[I domain.AnyID](check IntegrityCheck, entity IntegrityEntity, id I, format string, args ...any) IntegrityFinding {
	return IntegrityFinding{Check: check, Entity: entity, ID: id.String(), Message: fmt.Sprintf(format, args...)}
}

// checkPasses compares the visit passes' used visits with their
// redemptions, and finds redemptions of bookings that cost no visit.
func checkPasses(ctx context.Context, tx store.Tx) ([]IntegrityFinding, error) {
	unbalanced, err := tx.Passes().ListUnbalanced(ctx)
	if err != nil {
		return nil, err
	}

	stray, err := tx.Redemptions().ListStray(ctx)
	if err != nil {
		return nil, err
	}

	out := make([]IntegrityFinding, 0, len(unbalanced)+len(stray))

	for _, b := range unbalanced {
		out = append(out, finding(CheckPasses, EntityPass, b.Pass,
			"%d of its visits are used, but it paid for %d bookings", b.Used, b.Redeemed))
	}

	for _, r := range stray {
		var why []string
		if r.CanceledInTime {
			why = append(why, "the booking was canceled in time")
		}

		if r.LessonCanceled {
			why = append(why, "the school canceled lesson "+r.Lesson.String())
		}

		out = append(out, finding(CheckPasses, EntityBooking, r.Redemption.BookingID,
			"pass %s paid for it, but it costs no visit: %s", r.Redemption.PassID, strings.Join(why, ", and ")))
	}

	return out, nil
}

// checkCapacity finds the lessons with more active bookings than places
// than the overrides explain.
func checkCapacity(ctx context.Context, tx store.Tx) ([]IntegrityFinding, error) {
	lessons, err := tx.Bookings().ListOverCapacity(ctx)
	if err != nil {
		return nil, err
	}

	var out []IntegrityFinding

	for _, l := range lessons {
		if l.Active-l.Overrides <= l.Capacity {
			continue // booked beyond the capacity on purpose
		}

		out = append(out, finding(CheckCapacity, EntityLesson, l.Lesson,
			"%d active bookings for %d places, and only %d of them were booked beyond the capacity on purpose",
			l.Active, l.Capacity, l.Overrides))
	}

	return out, nil
}

// tombstoneRefs describe the references to a tombstone, by kind.
var tombstoneRefs = map[store.TombstoneRefKind]string{
	store.TombstoneBooking:   "booking %s",
	store.TombstoneWaitlist:  "open waiting-list entry %s",
	store.TombstoneMember:    "membership of class %s",
	store.TombstoneGuardian:  "guardian link to dependent %s",
	store.TombstoneDependent: "guardian link from guardian %s",
	store.TombstonePass:      "pass %s it owns",
	store.TombstonePaidPass:  "pass %s it paid for",
	store.TombstoneStaff:     "staff record %s",
	store.TombstoneAccount:   "account %s",
	store.TombstoneDismissal: "duplicate dismissal with person %s",
	store.TombstoneMerged:    "person %s merged into it",
}

// checkTombstones finds what still refers to merged and anonymized persons.
func checkTombstones(ctx context.Context, tx store.Tx) ([]IntegrityFinding, error) {
	refs, err := tx.Persons().ListTombstoneReferences(ctx)
	if err != nil {
		return nil, err
	}

	out := make([]IntegrityFinding, len(refs))

	for i, r := range refs {
		what := "merged"
		if r.Anonymized {
			what = "anonymized"
		}

		out[i] = finding(CheckTombstones, EntityPerson, r.Person,
			"the person is %s, but %s still refers to it", what, fmt.Sprintf(tombstoneRefs[r.Kind], r.Ref))
	}

	return out, nil
}

// checkReceiptCounters finds the years whose counter is behind the numbers
// issued.
func checkReceiptCounters(ctx context.Context, tx store.Tx) ([]IntegrityFinding, error) {
	states, err := tx.ReceiptCounters().ListStates(ctx)
	if err != nil {
		return nil, err
	}

	var out []IntegrityFinding

	for _, st := range states {
		if st.Last >= st.Highest {
			continue
		}

		out = append(out, IntegrityFinding{
			Check: CheckReceiptCounters, Entity: EntityReceiptYear, ID: strconv.Itoa(st.Year),
			Message: fmt.Sprintf("the counter is at %d, but receipt number %d was issued: the next receipt would repeat a number",
				st.Last, st.Highest),
		})
	}

	return out, nil
}
