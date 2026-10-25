package service

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// Passes (ADR 0012): the office keeps the pass catalog, sells passes to
// persons, gives them away and voids them; members see their family's
// passes and what they paid for. Redemption itself is in redemption.go.

var (
	// ErrPassVoided refuses to void a pass that is voided already.
	ErrPassVoided = fmt.Errorf("%w: the pass is voided already", domain.ErrConflict)
	// errUnknownPassType is a sale of a pass type that does not exist.
	errUnknownPassType error = &domain.FieldError{Field: "passTypeId", Msg: "pass type does not exist"}
	// errRetiredPassType is a sale of a pass type that is no longer sold.
	errRetiredPassType error = &domain.FieldError{Field: "passTypeId", Msg: "the pass type is retired"}
	// errUnknownPayer is a payer who does not exist.
	errUnknownPayer error = &domain.FieldError{Field: "payerId", Msg: "person does not exist"}
)

// PassService is the office's pass administration and the members' view of
// their passes (ADR 0012).
type PassService struct {
	store    store.Store
	clock    clock.Clock
	ids      ids.Source
	receipts *ReceiptService
}

// NewPassService returns a PassService backed by st that issues the receipts
// of its sales with receipts.
func NewPassService(st store.Store, clk clock.Clock, src ids.Source, receipts *ReceiptService) *PassService {
	return &PassService{store: st, clock: clk, ids: src, receipts: receipts}
}

// PassTypeData are the fields the office edits on a pass type.
type PassTypeData struct {
	Name         string
	Kind         domain.PassKind
	Visits       int
	ValidityDays int
	Price        domain.Money
	VATRate      int
	Active       bool
}

func (d PassTypeData) apply(t *domain.PassType) {
	t.Name, t.Kind, t.Visits, t.ValidityDays = strings.TrimSpace(d.Name), d.Kind, d.Visits, d.ValidityDays
	t.Price, t.VATRate, t.Active = d.Price, d.VATRate, d.Active
}

// PassTypes returns every pass type, retired ones included, ordered by name.
func (s *PassService) PassTypes(ctx context.Context) ([]domain.PassType, error) {
	var out []domain.PassType

	err := s.store.InTx(ctx, store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		var err error
		out, err = tx.PassTypes().List(ctx)

		return err
	})
	if err != nil {
		return nil, fmt.Errorf("list pass types: %w", err)
	}

	return out, nil
}

// CreatePassType adds a pass type. A name another type has (ignoring case)
// is domain.ErrDuplicate; invalid fields are validation errors on them.
func (s *PassService) CreatePassType(ctx context.Context, d PassTypeData) (domain.PassType, error) {
	t := domain.PassType{ID: ids.New[domain.PassTypeID](s.ids), CreatedAt: s.clock.Now()}
	d.apply(&t)

	err := s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		if err := t.Validate(); err != nil {
			return err
		}

		return tx.PassTypes().Create(ctx, t)
	})
	if err != nil {
		return domain.PassType{}, fmt.Errorf("create pass type: %w", err)
	}

	return t, nil
}

// UpdatePassType replaces the fields of a pass type. Passes sold already
// keep what they were sold with (visits, days, price); retiring a type
// (Active false) stops its sale. A name another type has is
// domain.ErrDuplicate.
func (s *PassService) UpdatePassType(ctx context.Context, id domain.PassTypeID, d PassTypeData) (domain.PassType, error) {
	var t domain.PassType

	err := s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		var err error
		if t, err = tx.PassTypes().Get(ctx, id); err != nil {
			return err
		}

		d.apply(&t)

		if err := t.Validate(); err != nil {
			return err
		}

		return tx.PassTypes().Update(ctx, t)
	})
	if err != nil {
		return domain.PassType{}, fmt.Errorf("update pass type %v: %w", id, err)
	}

	return t, nil
}

// RetirePassType stops the sale of a pass type (domain.ErrNotFound for an
// unknown one) and changes nothing else; a retired type stays retired.
func (s *PassService) RetirePassType(ctx context.Context, id domain.PassTypeID) (domain.PassType, error) {
	var t domain.PassType

	err := s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		var err error
		if t, err = tx.PassTypes().Get(ctx, id); err != nil || !t.Active {
			return err
		}

		t.Active = false

		return tx.PassTypes().Update(ctx, t)
	})
	if err != nil {
		return domain.PassType{}, fmt.Errorf("retire pass type %v: %w", id, err)
	}

	return t, nil
}

// DeletePassType deletes a pass type no pass is of; a sold one is
// domain.ErrInUse and is retired instead.
func (s *PassService) DeletePassType(ctx context.Context, id domain.PassTypeID) error {
	err := s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		return tx.PassTypes().Delete(ctx, id)
	})
	if errors.Is(err, domain.ErrConflict) {
		err = fmt.Errorf("%w (%w)", domain.ErrInUse, err)
	}

	if err != nil {
		return fmt.Errorf("delete pass type %v: %w", id, err)
	}

	return nil
}

// PassSale is what Sell records. A zero ValidFrom is today; a nil Price is
// the type's price; a zero PayerID, or the owner, means the owner paid.
// PaymentMethod (cash if zero) and Recipient go on the receipt.
type PassSale struct {
	PassTypeID    domain.PassTypeID
	PayerID       domain.PersonID
	ValidFrom     domain.Date
	Price         *domain.Money
	Note          string
	PaymentMethod domain.PaymentMethod
	Recipient     ReceiptRecipient
}

// PassAssignment is what Assign records: a pass given without payment.
type PassAssignment struct {
	PassTypeID domain.PassTypeID
	ValidFrom  domain.Date
	Note       string
}

// Sell sells a pass of an active type to person (domain.ErrNotFound for an
// unknown person) as the office account p: the pass copies the type's
// visits and days from ValidFrom on and records the price paid and the
// payer. An unknown or retired type is a validation error on passTypeId,
// an unknown payer on payerId. A new pass pays for nothing booked before;
// the office settles those at the desk.
//
// The sale issues its receipt in the same transaction (ADR 0012): numbered,
// rendered and mailed to the payer, with the payment method and recipient
// of d. Incomplete receipt settings refuse the sale
// (ErrReceiptSettingsIncomplete): every sale has a receipt.
func (s *PassService) Sell(ctx context.Context, p Principal, person domain.PersonID, d PassSale) (PassView, error) {
	var out PassView

	err := s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		settings, loc, err := loadSchool(ctx, tx)
		if err != nil {
			return err
		}

		// The payer is checked first: newPass reports any missing person as
		// the owner.
		var payer domain.Person

		paidByOther := !d.PayerID.IsZero() && d.PayerID != person
		if paidByOther {
			if payer, err = tx.Persons().Get(ctx, d.PayerID); errors.Is(err, domain.ErrNotFound) {
				return errUnknownPayer
			} else if err != nil {
				return err
			}
		}

		var also []domain.PersonID
		if paidByOther {
			also = append(also, payer.ID)
		}

		pass, t, err := s.newPass(ctx, tx, loc, p, person, d.PassTypeID, d.ValidFrom, d.Note, also...)
		if err != nil {
			return err
		}

		pass.Origin, pass.PricePaid = domain.PassSold, t.Price
		if d.Price != nil {
			pass.PricePaid = *d.Price
		}

		owner, err := tx.Persons().Get(ctx, person)
		if err != nil {
			return err
		}

		if paidByOther {
			pass.PayerID = payer.ID
		} else {
			payer = owner
		}

		// The pass is checked before a number is taken; the receipt comes
		// first, as the pass refers to it.
		if err := pass.Validate(); err != nil {
			return err
		}

		if s.receipts == nil {
			return ErrNoPDFRenderer
		}

		method := cmp.Or(d.PaymentMethod, domain.PaymentCash)

		receipt, err := s.receipts.issueSale(ctx, tx, settings, loc, p, pass, t, owner, payer, method, d.Recipient)
		if err != nil {
			return err
		}

		pass.ReceiptID = receipt.ID

		if err := s.create(ctx, tx, pass); err != nil {
			return err
		}

		out, err = s.view(ctx, tx, settings, loc, pass)

		return err
	})
	if err != nil {
		return PassView{}, fmt.Errorf("sell pass to person %v: %w", person, err)
	}

	return out, nil
}

// Assign gives person (domain.ErrNotFound for an unknown person) a pass of
// an active type without payment, as the office account p, with an
// optional note why; it fails like Sell. No receipt is issued.
func (s *PassService) Assign(
	ctx context.Context, p Principal, person domain.PersonID, d PassAssignment,
) (PassView, error) {
	var out PassView

	err := s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		settings, loc, err := loadSchool(ctx, tx)
		if err != nil {
			return err
		}

		pass, _, err := s.newPass(ctx, tx, loc, p, person, d.PassTypeID, d.ValidFrom, d.Note)
		if err != nil {
			return err
		}

		pass.Origin = domain.PassAssigned

		if err := s.create(ctx, tx, pass); err != nil {
			return err
		}

		out, err = s.view(ctx, tx, settings, loc, pass)

		return err
	})
	if err != nil {
		return PassView{}, fmt.Errorf("assign pass to person %v: %w", person, err)
	}

	return out, nil
}

// newPass returns an active pass of person of the active type typeID from
// from (today in the school's time zone loc if zero), sold now by p,
// without its origin and price, and its type. It locks person and the
// persons in also (the payer), so that none is deleted before the pass
// refers to them: a missing one is domain.ErrNotFound.
func (s *PassService) newPass(
	ctx context.Context, tx store.Tx, loc *time.Location, p Principal, person domain.PersonID,
	typeID domain.PassTypeID, from domain.Date, note string, also ...domain.PersonID,
) (domain.Pass, domain.PassType, error) {
	if err := tx.Persons().Lock(ctx, append([]domain.PersonID{person}, also...)...); err != nil {
		return domain.Pass{}, domain.PassType{}, fmt.Errorf("person: %w", err)
	}

	t, err := tx.PassTypes().Get(ctx, typeID)

	switch {
	case errors.Is(err, domain.ErrNotFound):
		return domain.Pass{}, domain.PassType{}, errUnknownPassType
	case err != nil:
		return domain.Pass{}, domain.PassType{}, err
	case !t.Active:
		return domain.Pass{}, domain.PassType{}, errRetiredPassType
	}

	now := s.clock.Now()
	if from.IsZero() {
		from = domain.DateOf(now, loc)
	}

	pass := domain.Pass{
		ID: ids.New[domain.PassID](s.ids), PassTypeID: t.ID, PersonID: person, Kind: t.Kind,
		ValidFrom: from, Status: domain.PassActive, SoldAt: now, SoldBy: actorOf(p), Note: strings.TrimSpace(note),
	}
	if t.Kind == domain.PassVisits {
		pass.VisitsTotal, pass.VisitsRemaining = t.Visits, t.Visits
	}

	if from.Valid() {
		pass.ValidUntil = t.ValidUntil(from)
	}

	return pass, t, nil
}

// create validates and stores a new pass. Its persons are locked
// (newPass), so a conflict of the store is about something else, e.g. its
// receipt, and is returned as is.
func (s *PassService) create(ctx context.Context, tx store.Tx, pass domain.Pass) error {
	if err := pass.Validate(); err != nil {
		return err
	}

	return tx.Passes().Create(ctx, pass)
}

// Void voids a pass (domain.ErrNotFound for an unknown one): it pays for
// nothing more. What it paid for in lessons that have started stays paid;
// its bookings of lessons ahead give their visits back and use another pass
// of the person that covers their dates, if any (charges.recheck). A voided
// pass is ErrPassVoided. It locks those lessons in ID order, then the
// owner's passes (lesson → passes). A booking that the pass pays for while
// it is being voided stays paid by it, like one paid before.
func (s *PassService) Void(ctx context.Context, id domain.PassID) (PassView, error) {
	var out PassView

	err := s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		settings, loc, err := loadSchool(ctx, tx)
		if err != nil {
			return err
		}

		pass, err := tx.Passes().Get(ctx, id)
		if err != nil {
			return err
		}

		if pass.Status == domain.PassVoided {
			return ErrPassVoided
		}

		now := s.clock.Now()

		ahead, paid, err := lessonsAheadPaidBy(ctx, tx, now, id)
		if err != nil {
			return err
		}

		locked, err := tx.Passes().LockByPersons(ctx, pass.PersonID)
		if err != nil {
			return err
		}

		i := slices.IndexFunc(locked, func(p domain.Pass) bool { return p.ID == id })
		if i < 0 {
			return fmt.Errorf("pass %v: %w", id, domain.ErrNotFound)
		}

		pass = locked[i]
		if pass.Status == domain.PassVoided {
			return ErrPassVoided
		}

		pass.Status, pass.VoidedAt = domain.PassVoided, now
		if err := pass.Validate(); err != nil {
			return err
		}

		if err := tx.Passes().Update(ctx, pass); err != nil {
			return err
		}

		c := chargesOf(settings, loc)
		for _, l := range ahead {
			if err := c.recheck(ctx, tx, now, l, paid[l.ID]); err != nil {
				return err
			}
		}

		if pass, err = tx.Passes().Get(ctx, id); err != nil {
			return err
		}

		out, err = s.view(ctx, tx, settings, loc, pass)

		return err
	})
	if err != nil {
		return PassView{}, fmt.Errorf("void pass %v: %w", id, err)
	}

	return out, nil
}

// lessonsAheadPaidBy locks, in the order of their IDs, the lessons that have
// not started at now with bookings pass paid for, and returns them, as read
// under the locks, with those bookings by lesson.
func lessonsAheadPaidBy(
	ctx context.Context, tx store.Tx, now time.Time, pass domain.PassID,
) ([]domain.Lesson, map[domain.LessonID][]domain.Booking, error) {
	redemptions, err := tx.Redemptions().ListByPasses(ctx, []domain.PassID{pass}, 0)
	if err != nil || len(redemptions) == 0 {
		return nil, nil, err
	}

	bookingIDs := make([]domain.BookingID, len(redemptions))
	for i, r := range redemptions {
		bookingIDs[i] = r.BookingID
	}

	bookings, err := tx.Bookings().ListByIDs(ctx, bookingIDs)
	if err != nil {
		return nil, nil, fmt.Errorf("bookings: %w", err)
	}

	paid := map[domain.LessonID][]domain.Booking{}
	for _, b := range bookings {
		paid[b.LessonID] = append(paid[b.LessonID], b)
	}

	lessons, err := tx.Lessons().ListByIDs(ctx, slices.Collect(maps.Keys(paid)))
	if err != nil {
		return nil, nil, fmt.Errorf("lessons: %w", err)
	}

	lessons = slices.DeleteFunc(lessons, func(l domain.Lesson) bool { return !l.StartsAt.After(now) })
	slices.SortFunc(lessons, func(a, b domain.Lesson) int { return bytes.Compare(a.ID[:], b.ID[:]) })

	ahead := lessons[:0]

	for _, l := range lessons {
		// A single lesson edit may have moved it meanwhile: decide from the
		// locked row.
		if l, err = tx.Lessons().Lock(ctx, l.ID); err != nil {
			return nil, nil, err
		}

		if l.StartsAt.After(now) {
			ahead = append(ahead, l)
		}
	}

	return ahead, paid, nil
}

// MaxPassUsesShown is the most uses of a pass a PassView lists: the
// latest; UseCount counts them all.
const MaxPassUsesShown = 10

// PassView is a pass with what it refers to and what it paid for.
type PassView struct {
	Pass      domain.Pass
	PassType  domain.PassType
	OwnerName string
	// PayerName is the full name of the person who paid, if not the owner;
	// empty otherwise.
	PayerName string
	// Usable tells that the pass pays for a visit today (domain.Pass.Covers
	// on the school's date).
	Usable bool
	// Uses are the latest MaxPassUsesShown bookings the pass paid for (by
	// when it paid), the latest lesson first; UseCount counts every one.
	Uses     []PassUse
	UseCount int
}

// PassUse is a booking a pass paid for.
type PassUse struct {
	Redemption domain.PassRedemption
	Booking    domain.Booking
	// Lesson is the lesson with its references.
	Lesson ScheduledLesson
}

// MyPasses are the passes of a signed-in account's family.
type MyPasses struct {
	// Timezone is the IANA time zone of the school; Today is the school's
	// date.
	Timezone string
	Today    domain.Date
	// RedemptionMode is when bookings use a pass (ADR 0012).
	RedemptionMode domain.RedemptionMode
	// Passes are the family's passes: the usable ones first, then the
	// others, each newest sale first.
	Passes []PassView
}

// PersonPasses returns the passes of a person (domain.ErrNotFound for an
// unknown one) for the office, ordered like MyPasses.
func (s *PassService) PersonPasses(ctx context.Context, person domain.PersonID) ([]PassView, error) {
	var out []PassView

	err := s.store.InTx(ctx, store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		settings, loc, err := loadSchool(ctx, tx)
		if err != nil {
			return err
		}

		if _, err := tx.Persons().Get(ctx, person); err != nil {
			return err
		}

		out, err = s.views(ctx, tx, settings, loc, []domain.PersonID{person})

		return err
	})
	if err != nil {
		return nil, fmt.Errorf("passes of person %v: %w", person, err)
	}

	return out, nil
}

// Mine returns the passes of p's family: its own person and its
// dependents, managed ones included (ADR 0008). A zero p has none.
func (s *PassService) Mine(ctx context.Context, p Principal) (MyPasses, error) {
	var out MyPasses

	err := s.store.InTx(ctx, store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		settings, loc, err := loadSchool(ctx, tx)
		if err != nil {
			return err
		}

		out = MyPasses{
			Timezone: settings.Timezone, Today: domain.DateOf(s.clock.Now(), loc), RedemptionMode: settings.RedemptionMode,
		}

		fam, err := familyOf(ctx, tx, p)
		if err != nil || len(fam.persons) == 0 {
			return err
		}

		out.Passes, err = s.views(ctx, tx, settings, loc, fam.ids())

		return err
	})
	if err != nil {
		return MyPasses{}, fmt.Errorf("my passes: %w", err)
	}

	return out, nil
}

// view returns the PassView of pass.
func (s *PassService) view(
	ctx context.Context, tx store.Tx, settings domain.Settings, loc *time.Location, pass domain.Pass,
) (PassView, error) {
	views, err := s.build(ctx, tx, settings, loc, []domain.Pass{pass})
	if err != nil {
		return PassView{}, err
	}

	return views[0], nil
}

// views returns the PassViews of the passes the persons own, the usable
// ones first, then the others, each newest sale first (the ID breaks ties).
func (s *PassService) views(
	ctx context.Context, tx store.Tx, settings domain.Settings, loc *time.Location, persons []domain.PersonID,
) ([]PassView, error) {
	passes, err := tx.Passes().ListByPersons(ctx, persons)
	if err != nil {
		return nil, fmt.Errorf("passes: %w", err)
	}

	out, err := s.build(ctx, tx, settings, loc, passes)
	if err != nil {
		return nil, err
	}

	slices.SortStableFunc(out, func(a, b PassView) int {
		rank := func(v PassView) int {
			if v.Usable {
				return 0
			}

			return 1
		}

		return cmp.Or(
			cmp.Compare(rank(a), rank(b)), b.Pass.SoldAt.Compare(a.Pass.SoldAt), bytes.Compare(b.Pass.ID[:], a.Pass.ID[:]),
		)
	})

	return out, nil
}

// build combines passes with their types, owners, payers and latest uses,
// with a fixed number of queries for any number of passes.
func (s *PassService) build(
	ctx context.Context, tx store.Tx, settings domain.Settings, loc *time.Location, passes []domain.Pass,
) ([]PassView, error) {
	if len(passes) == 0 {
		return []PassView{}, nil
	}

	today := domain.DateOf(s.clock.Now(), loc)

	types, err := passTypesByID(ctx, tx)
	if err != nil {
		return nil, err
	}

	personIDs := make([]domain.PersonID, 0, 2*len(passes))
	passIDs := make([]domain.PassID, 0, len(passes))

	for _, p := range passes {
		personIDs = append(personIDs, p.PersonID)
		if !p.PayerID.IsZero() {
			personIDs = append(personIDs, p.PayerID)
		}

		passIDs = append(passIDs, p.ID)
	}

	persons, err := tx.Persons().ListByIDs(ctx, personIDs)
	if err != nil {
		return nil, fmt.Errorf("persons: %w", err)
	}

	names := byID(persons)

	uses, err := s.uses(ctx, tx, settings, passIDs)
	if err != nil {
		return nil, err
	}

	counts, err := tx.Redemptions().CountByPasses(ctx, passIDs)
	if err != nil {
		return nil, fmt.Errorf("count uses: %w", err)
	}

	out := make([]PassView, 0, len(passes))

	for _, p := range passes {
		v := PassView{
			Pass: p, PassType: types[p.PassTypeID], OwnerName: fullName(names[p.PersonID]), Usable: p.Covers(today),
			Uses: uses[p.ID], UseCount: counts[p.ID],
		}
		if !p.PayerID.IsZero() {
			v.PayerName = fullName(names[p.PayerID])
		}

		out = append(out, v)
	}

	return out, nil
}

// uses returns, by pass, the latest MaxPassUsesShown bookings each pass
// paid for with their lessons, the latest lesson first.
func (s *PassService) uses(
	ctx context.Context, tx store.Tx, settings domain.Settings, passes []domain.PassID,
) (map[domain.PassID][]PassUse, error) {
	redemptions, err := tx.Redemptions().ListByPasses(ctx, passes, MaxPassUsesShown)
	if err != nil || len(redemptions) == 0 {
		return map[domain.PassID][]PassUse{}, err
	}

	bookingIDs := make([]domain.BookingID, len(redemptions))
	for i, r := range redemptions {
		bookingIDs[i] = r.BookingID
	}

	bookings, err := tx.Bookings().ListByIDs(ctx, bookingIDs)
	if err != nil {
		return nil, fmt.Errorf("bookings: %w", err)
	}

	byBooking := make(map[domain.BookingID]domain.Booking, len(bookings))
	for _, b := range bookings {
		byBooking[b.ID] = b
	}

	lessons, err := lessonsOf(ctx, tx, settings, bookings, nil)
	if err != nil {
		return nil, err
	}

	out := map[domain.PassID][]PassUse{}

	for _, r := range redemptions {
		b, ok := byBooking[r.BookingID]
		if !ok {
			return nil, fmt.Errorf("booking %v: %w", r.BookingID, errBrokenReference)
		}

		l, ok := lessons[b.LessonID]
		if !ok {
			return nil, fmt.Errorf("lesson %v: %w", b.LessonID, errBrokenReference)
		}

		out[r.PassID] = append(out[r.PassID], PassUse{Redemption: r, Booking: b, Lesson: l})
	}

	for _, list := range out {
		slices.SortStableFunc(list, func(a, b PassUse) int { return byStart(b.Lesson.Lesson, a.Lesson.Lesson) })
	}

	return out, nil
}

// RedemptionMode returns when bookings use a pass (ADR 0012).
func (s *PassService) RedemptionMode(ctx context.Context) (domain.RedemptionMode, error) {
	var mode domain.RedemptionMode

	err := s.store.InTx(ctx, store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		settings, err := tx.Settings().Get(ctx)
		mode = settings.RedemptionMode

		return err
	})
	if err != nil {
		return "", fmt.Errorf("redemption mode: %w", err)
	}

	return mode, nil
}

// SetRedemptionMode changes when bookings use a pass from now on; an
// unknown mode is a validation error on redemptionMode. What was redeemed
// before stays: a booking is settled by the new mode the next time it
// changes (ADR 0012).
func (s *PassService) SetRedemptionMode(ctx context.Context, mode domain.RedemptionMode) error {
	err := s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		settings, err := tx.Settings().Get(ctx)
		if err != nil {
			return err
		}

		settings.RedemptionMode = mode
		if err := settings.Validate(); err != nil {
			return err
		}

		return tx.Settings().Put(ctx, settings)
	})
	if err != nil {
		return fmt.Errorf("set redemption mode: %w", err)
	}

	return nil
}
