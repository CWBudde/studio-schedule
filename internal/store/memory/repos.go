package memory

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"
	"time"
	"uuid"

	"github.com/cwbudde/studio-schedule/internal/domain"
)

func notFound(what string) error { return fmt.Errorf("%s: %w", what, domain.ErrNotFound) }

func conflict(format string, args ...any) error {
	return fmt.Errorf("%s: %w", fmt.Sprintf(format, args...), domain.ErrConflict)
}

// compareID orders IDs by their bytes, like PostgreSQL orders uuid values.
func compareID[I domain.AnyID](a, b I) int { return uuid.UUID(a).Compare(uuid.UUID(b)) }

// foldEmail lower-cases ASCII letters only, like PostgreSQL's lower() under
// collation "C" (the collation of the email column). Other letters
// keep their case, so "Ä" and "ä" are different emails in both stores.
func foldEmail(s string) string {
	return strings.Map(func(r rune) rune {
		if 'A' <= r && r <= 'Z' {
			return r + 'a' - 'A'
		}

		return r
	}, s)
}

// sorted sorts s in place and returns it.
func sorted[T any](s []T, cmpFn func(a, b T) int) []T {
	slices.SortFunc(s, cmpFn)

	return s
}

func all[V any](V) bool { return true }

// ---------------------------------------------------------------- persons

type personRepo struct{ tx *tx }

func comparePersons(a, b domain.Person) int {
	return cmp.Or(
		strings.Compare(a.FamilyName, b.FamilyName),
		strings.Compare(a.GivenName, b.GivenName),
		compareID(a.ID, b.ID),
	)
}

func (r personRepo) Get(_ context.Context, id domain.PersonID) (domain.Person, error) {
	p, ok := r.tx.state.persons.get(id)
	if !ok {
		return domain.Person{}, notFound("person")
	}

	return p, nil
}

func (r personRepo) Create(_ context.Context, p domain.Person) error {
	s := &r.tx.state
	if err := r.tx.checkWritable(); err != nil {
		return err
	}

	if s.persons.has(p.ID) {
		return conflict("person %s exists", p.ID)
	}

	s.persons.put(r.tx.gen, p.ID, p)

	return nil
}

func (r personRepo) Update(_ context.Context, p domain.Person) error {
	s := &r.tx.state
	if err := r.tx.checkWritable(); err != nil {
		return err
	}

	if !s.persons.has(p.ID) {
		return notFound("person")
	}

	s.persons.put(r.tx.gen, p.ID, p)

	return nil
}

func (r personRepo) Delete(_ context.Context, id domain.PersonID) error {
	s := &r.tx.state
	if err := r.tx.checkWritable(); err != nil {
		return err
	}

	if !s.persons.has(id) {
		return notFound("person")
	}

	referenced := s.accounts.exists(func(a domain.Account) bool { return a.PersonID == id }) ||
		s.staff.exists(func(st domain.Staff) bool { return st.PersonID == id }) ||
		s.classMembers.exists(func(m domain.ClassMember) bool { return m.PersonID == id }) ||
		s.bookings.exists(func(b domain.Booking) bool { return b.PersonID == id })
	for g := range s.guardians.rows {
		referenced = referenced || g.GuardianID == id || g.DependentID == id
	}

	if referenced {
		return conflict("person %s is referenced", id)
	}

	s.persons.delete(r.tx.gen, id)

	return nil
}

func (r personRepo) List(context.Context) ([]domain.Person, error) {
	return sorted(r.tx.state.persons.filter(all), comparePersons), nil
}

func (r personRepo) AddGuardian(_ context.Context, g domain.Guardian) error {
	s := &r.tx.state
	if err := r.tx.checkWritable(); err != nil {
		return err
	}

	switch {
	case !s.persons.has(g.GuardianID) || !s.persons.has(g.DependentID):
		return conflict("guardian link references a missing person")
	case s.guardians.has(g):
		return conflict("guardian link exists")
	}

	s.guardians.put(r.tx.gen, g, struct{}{})

	return nil
}

func (r personRepo) RemoveGuardian(_ context.Context, g domain.Guardian) error {
	s := &r.tx.state
	if err := r.tx.checkWritable(); err != nil {
		return err
	}

	if !s.guardians.has(g) {
		return notFound("guardian link")
	}

	s.guardians.delete(r.tx.gen, g)

	return nil
}

func (r personRepo) ListDependents(_ context.Context, guardian domain.PersonID) ([]domain.Person, error) {
	return r.linked(func(g domain.Guardian) (domain.PersonID, bool) {
		return g.DependentID, g.GuardianID == guardian
	}), nil
}

func (r personRepo) ListGuardians(_ context.Context, dependent domain.PersonID) ([]domain.Person, error) {
	return r.linked(func(g domain.Guardian) (domain.PersonID, bool) {
		return g.GuardianID, g.DependentID == dependent
	}), nil
}

// linked returns the persons that pick selects from the guardian links,
// ordered like List.
func (r personRepo) linked(pick func(domain.Guardian) (domain.PersonID, bool)) []domain.Person {
	var out []domain.Person

	for g := range r.tx.state.guardians.rows {
		if id, ok := pick(g); ok {
			p, _ := r.tx.state.persons.get(id)
			out = append(out, p)
		}
	}

	return sorted(out, comparePersons)
}

// ---------------------------------------------------------------- accounts

type accountRepo struct{ tx *tx }

func (r accountRepo) Get(_ context.Context, id domain.AccountID) (domain.Account, error) {
	a, ok := r.tx.state.accounts.get(id)
	if !ok {
		return domain.Account{}, notFound("account")
	}

	return a, nil
}

func (r accountRepo) GetByEmail(_ context.Context, email string) (domain.Account, error) {
	found := r.tx.state.accounts.filter(func(a domain.Account) bool { return foldEmail(a.Email) == foldEmail(email) })
	if len(found) == 0 {
		return domain.Account{}, notFound("account")
	}

	return found[0], nil
}

func (r accountRepo) Create(_ context.Context, a domain.Account) error {
	if err := r.tx.checkWritable(); err != nil {
		return err
	}

	if r.tx.state.accounts.has(a.ID) {
		return conflict("account %s exists", a.ID)
	}

	return r.put(a)
}

func (r accountRepo) Update(_ context.Context, a domain.Account) error {
	if err := r.tx.checkWritable(); err != nil {
		return err
	}

	if !r.tx.state.accounts.has(a.ID) {
		return notFound("account")
	}

	return r.put(a)
}

func (r accountRepo) put(a domain.Account) error {
	s := &r.tx.state

	switch {
	case !s.persons.has(a.PersonID):
		return conflict("account references a missing person")
	case s.accounts.exists(func(o domain.Account) bool { return o.ID != a.ID && foldEmail(o.Email) == foldEmail(a.Email) }):
		return conflict("account email is taken")
	case s.accounts.exists(func(o domain.Account) bool { return o.ID != a.ID && o.PersonID == a.PersonID }):
		return conflict("person already has an account")
	}

	a.ConfirmedAt = instant(a.ConfirmedAt)
	a.DisabledAt = instant(a.DisabledAt)
	s.accounts.put(r.tx.gen, a.ID, a)

	return nil
}

func (r accountRepo) List(context.Context) ([]domain.Account, error) {
	return sorted(r.tx.state.accounts.filter(all), func(a, b domain.Account) int {
		return cmp.Or(strings.Compare(foldEmail(a.Email), foldEmail(b.Email)), compareID(a.ID, b.ID))
	}), nil
}

func (r accountRepo) MaxVisibleDays(context.Context) (int, error) {
	n := 0
	for _, a := range r.tx.state.accounts.filter(all) {
		n = max(n, a.VisibleDays)
	}

	return n, nil
}

// ---------------------------------------------------------------- catalog

type locationRepo struct{ tx *tx }

func (r locationRepo) Get(_ context.Context, id domain.LocationID) (domain.Location, error) {
	l, ok := r.tx.state.locations.get(id)
	if !ok {
		return domain.Location{}, notFound("location")
	}

	return l, nil
}

func (r locationRepo) Create(_ context.Context, l domain.Location) error {
	s := &r.tx.state
	if err := r.tx.checkWritable(); err != nil {
		return err
	}

	if s.locations.has(l.ID) {
		return conflict("location %s exists", l.ID)
	}

	s.locations.put(r.tx.gen, l.ID, l)

	return nil
}

func (r locationRepo) Update(_ context.Context, l domain.Location) error {
	s := &r.tx.state
	if err := r.tx.checkWritable(); err != nil {
		return err
	}

	if !s.locations.has(l.ID) {
		return notFound("location")
	}

	s.locations.put(r.tx.gen, l.ID, l)

	return nil
}

func (r locationRepo) Delete(_ context.Context, id domain.LocationID) error {
	s := &r.tx.state
	if err := r.tx.checkWritable(); err != nil {
		return err
	}

	switch {
	case !s.locations.has(id):
		return notFound("location")
	case s.rooms.exists(func(rm domain.Room) bool { return rm.LocationID == id }):
		return conflict("location %s is referenced", id)
	}

	s.locations.delete(r.tx.gen, id)

	return nil
}

func (r locationRepo) List(context.Context) ([]domain.Location, error) {
	return sorted(r.tx.state.locations.filter(all), func(a, b domain.Location) int {
		return cmp.Or(strings.Compare(a.Name, b.Name), compareID(a.ID, b.ID))
	}), nil
}

type roomRepo struct{ tx *tx }

func (r roomRepo) Get(_ context.Context, id domain.RoomID) (domain.Room, error) {
	rm, ok := r.tx.state.rooms.get(id)
	if !ok {
		return domain.Room{}, notFound("room")
	}

	return rm, nil
}

func (r roomRepo) Create(_ context.Context, rm domain.Room) error {
	if err := r.tx.checkWritable(); err != nil {
		return err
	}

	if r.tx.state.rooms.has(rm.ID) {
		return conflict("room %s exists", rm.ID)
	}

	return r.put(rm)
}

func (r roomRepo) Update(_ context.Context, rm domain.Room) error {
	if err := r.tx.checkWritable(); err != nil {
		return err
	}

	if !r.tx.state.rooms.has(rm.ID) {
		return notFound("room")
	}

	return r.put(rm)
}

func (r roomRepo) put(rm domain.Room) error {
	if !r.tx.state.locations.has(rm.LocationID) {
		return conflict("room references a missing location")
	}

	r.tx.state.rooms.put(r.tx.gen, rm.ID, rm)

	return nil
}

func (r roomRepo) Delete(_ context.Context, id domain.RoomID) error {
	s := &r.tx.state
	if err := r.tx.checkWritable(); err != nil {
		return err
	}

	switch {
	case !s.rooms.has(id):
		return notFound("room")
	case s.classes.exists(func(c domain.Class) bool { return c.RoomID == id }),
		s.lessons.exists(func(l domain.Lesson) bool { return l.RoomID == id }):
		return conflict("room %s is referenced", id)
	}

	s.rooms.delete(r.tx.gen, id)

	return nil
}

func (r roomRepo) List(_ context.Context, location domain.LocationID) ([]domain.Room, error) {
	rooms := r.tx.state.rooms.filter(func(rm domain.Room) bool {
		return location.IsZero() || rm.LocationID == location
	})

	return sorted(rooms, func(a, b domain.Room) int {
		return cmp.Or(strings.Compare(a.Name, b.Name), compareID(a.ID, b.ID))
	}), nil
}

type staffRepo struct{ tx *tx }

func (r staffRepo) Get(_ context.Context, id domain.StaffID) (domain.Staff, error) {
	st, ok := r.tx.state.staff.get(id)
	if !ok {
		return domain.Staff{}, notFound("staff")
	}

	return st, nil
}

func (r staffRepo) Create(_ context.Context, st domain.Staff) error {
	if err := r.tx.checkWritable(); err != nil {
		return err
	}

	if r.tx.state.staff.has(st.ID) {
		return conflict("staff %s exists", st.ID)
	}

	return r.put(st)
}

func (r staffRepo) Update(_ context.Context, st domain.Staff) error {
	if err := r.tx.checkWritable(); err != nil {
		return err
	}

	if !r.tx.state.staff.has(st.ID) {
		return notFound("staff")
	}

	return r.put(st)
}

func (r staffRepo) put(st domain.Staff) error {
	if !st.PersonID.IsZero() && !r.tx.state.persons.has(st.PersonID) {
		return conflict("staff references a missing person")
	}

	r.tx.state.staff.put(r.tx.gen, st.ID, st)

	return nil
}

func (r staffRepo) Delete(_ context.Context, id domain.StaffID) error {
	s := &r.tx.state
	if err := r.tx.checkWritable(); err != nil {
		return err
	}

	switch {
	case !s.staff.has(id):
		return notFound("staff")
	case s.classes.exists(func(c domain.Class) bool { return c.InstructorID == id || c.AccompanistID == id }),
		s.lessons.exists(func(l domain.Lesson) bool { return l.InstructorID == id || l.AccompanistID == id }):
		return conflict("staff %s is referenced", id)
	}

	s.staff.delete(r.tx.gen, id)

	return nil
}

func (r staffRepo) List(context.Context) ([]domain.Staff, error) {
	return sorted(r.tx.state.staff.filter(all), func(a, b domain.Staff) int {
		return cmp.Or(strings.Compare(a.DisplayName, b.DisplayName), compareID(a.ID, b.ID))
	}), nil
}

type courseTypeRepo struct{ tx *tx }

func (r courseTypeRepo) Get(_ context.Context, id domain.CourseTypeID) (domain.CourseType, error) {
	c, ok := r.tx.state.courseTypes.get(id)
	if !ok {
		return domain.CourseType{}, notFound("course type")
	}

	return c, nil
}

func (r courseTypeRepo) Create(_ context.Context, c domain.CourseType) error {
	s := &r.tx.state
	if err := r.tx.checkWritable(); err != nil {
		return err
	}

	if s.courseTypes.has(c.ID) {
		return conflict("course type %s exists", c.ID)
	}

	s.courseTypes.put(r.tx.gen, c.ID, c)

	return nil
}

func (r courseTypeRepo) Update(_ context.Context, c domain.CourseType) error {
	s := &r.tx.state
	if err := r.tx.checkWritable(); err != nil {
		return err
	}

	if !s.courseTypes.has(c.ID) {
		return notFound("course type")
	}

	s.courseTypes.put(r.tx.gen, c.ID, c)

	return nil
}

func (r courseTypeRepo) Delete(_ context.Context, id domain.CourseTypeID) error {
	s := &r.tx.state
	if err := r.tx.checkWritable(); err != nil {
		return err
	}

	switch {
	case !s.courseTypes.has(id):
		return notFound("course type")
	case s.classes.exists(func(c domain.Class) bool { return c.CourseTypeID == id }),
		s.lessons.exists(func(l domain.Lesson) bool { return l.CourseTypeID == id }):
		return conflict("course type %s is referenced", id)
	}

	s.courseTypes.delete(r.tx.gen, id)

	return nil
}

func (r courseTypeRepo) List(context.Context) ([]domain.CourseType, error) {
	return sorted(r.tx.state.courseTypes.filter(all), func(a, b domain.CourseType) int {
		return cmp.Or(strings.Compare(a.Name, b.Name), compareID(a.ID, b.ID))
	}), nil
}

// ---------------------------------------------------------------- timetable

type termRepo struct{ tx *tx }

func (r termRepo) Get(_ context.Context, id domain.TermID) (domain.Term, error) {
	t, ok := r.tx.state.terms.get(id)
	if !ok {
		return domain.Term{}, notFound("term")
	}

	return t, nil
}

func (r termRepo) Create(_ context.Context, t domain.Term) error {
	s := &r.tx.state
	if err := r.tx.checkWritable(); err != nil {
		return err
	}

	if s.terms.has(t.ID) {
		return conflict("term %s exists", t.ID)
	}

	s.terms.put(r.tx.gen, t.ID, t)

	return nil
}

func (r termRepo) Update(_ context.Context, t domain.Term) error {
	s := &r.tx.state
	if err := r.tx.checkWritable(); err != nil {
		return err
	}

	if !s.terms.has(t.ID) {
		return notFound("term")
	}

	s.terms.put(r.tx.gen, t.ID, t)

	return nil
}

func (r termRepo) Delete(_ context.Context, id domain.TermID) error {
	s := &r.tx.state
	if err := r.tx.checkWritable(); err != nil {
		return err
	}

	switch {
	case !s.terms.has(id):
		return notFound("term")
	case s.classes.exists(func(c domain.Class) bool { return c.TermID == id }):
		return conflict("term %s is referenced", id)
	}

	s.terms.delete(r.tx.gen, id)

	return nil
}

func (r termRepo) List(context.Context) ([]domain.Term, error) {
	return sorted(r.tx.state.terms.filter(all), func(a, b domain.Term) int {
		return cmp.Or(a.StartsOn.Compare(b.StartsOn), compareID(a.ID, b.ID))
	}), nil
}

type classRepo struct{ tx *tx }

func (r classRepo) Get(_ context.Context, id domain.ClassID) (domain.Class, error) {
	c, ok := r.tx.state.classes.get(id)
	if !ok {
		return domain.Class{}, notFound("class")
	}

	return c, nil
}

func (r classRepo) Create(_ context.Context, c domain.Class) error {
	if err := r.tx.checkWritable(); err != nil {
		return err
	}

	if r.tx.state.classes.has(c.ID) {
		return conflict("class %s exists", c.ID)
	}

	return r.put(c)
}

func (r classRepo) Update(_ context.Context, c domain.Class) error {
	if err := r.tx.checkWritable(); err != nil {
		return err
	}

	if !r.tx.state.classes.has(c.ID) {
		return notFound("class")
	}

	return r.put(c)
}

func (r classRepo) put(c domain.Class) error {
	s := &r.tx.state
	if !s.terms.has(c.TermID) || !s.courseTypes.has(c.CourseTypeID) || !s.rooms.has(c.RoomID) ||
		!s.staff.has(c.InstructorID) || (!c.AccompanistID.IsZero() && !s.staff.has(c.AccompanistID)) {
		return conflict("class references a missing record")
	}

	s.classes.put(r.tx.gen, c.ID, c)

	return nil
}

func (r classRepo) Delete(_ context.Context, id domain.ClassID) error {
	s := &r.tx.state
	if err := r.tx.checkWritable(); err != nil {
		return err
	}

	switch {
	case !s.classes.has(id):
		return notFound("class")
	case s.classMembers.exists(func(m domain.ClassMember) bool { return m.ClassID == id }),
		s.lessons.exists(func(l domain.Lesson) bool { return l.ClassID == id }):
		return conflict("class %s is referenced", id)
	}

	s.classes.delete(r.tx.gen, id)

	return nil
}

func (r classRepo) ListByTerm(_ context.Context, term domain.TermID) ([]domain.Class, error) {
	classes := r.tx.state.classes.filter(func(c domain.Class) bool { return c.TermID == term })

	return sorted(classes, func(a, b domain.Class) int {
		return cmp.Or(cmp.Compare(a.Weekday, b.Weekday), a.StartTime.Compare(b.StartTime), compareID(a.ID, b.ID))
	}), nil
}

func (r classRepo) AddMember(_ context.Context, m domain.ClassMember) error {
	s := &r.tx.state
	if err := r.tx.checkWritable(); err != nil {
		return err
	}

	key := memberKey{m.ClassID, m.PersonID}

	switch {
	case !s.classes.has(m.ClassID) || !s.persons.has(m.PersonID):
		return conflict("class member references a missing record")
	case s.classMembers.has(key):
		return conflict("person is already a member")
	}

	s.classMembers.put(r.tx.gen, key, m)

	return nil
}

func (r classRepo) RemoveMember(_ context.Context, class domain.ClassID, person domain.PersonID) error {
	s := &r.tx.state
	if err := r.tx.checkWritable(); err != nil {
		return err
	}

	key := memberKey{class, person}
	if !s.classMembers.has(key) {
		return notFound("class member")
	}

	s.classMembers.delete(r.tx.gen, key)

	return nil
}

func (r classRepo) ListMembers(_ context.Context, class domain.ClassID) ([]domain.ClassMember, error) {
	members := r.tx.state.classMembers.filter(func(m domain.ClassMember) bool { return m.ClassID == class })

	return sorted(members, func(a, b domain.ClassMember) int { return compareID(a.PersonID, b.PersonID) }), nil
}

// ---------------------------------------------------------------- lessons

type lessonRepo struct{ tx *tx }

func (r lessonRepo) Get(_ context.Context, id domain.LessonID) (domain.Lesson, error) {
	l, ok := r.tx.state.lessons.get(id)
	if !ok {
		return domain.Lesson{}, notFound("lesson")
	}

	return l, nil
}

func (r lessonRepo) Create(_ context.Context, l domain.Lesson) error {
	if err := r.tx.checkWritable(); err != nil {
		return err
	}

	if r.tx.state.lessons.has(l.ID) {
		return conflict("lesson %s exists", l.ID)
	}

	return r.put(l)
}

func (r lessonRepo) Update(_ context.Context, l domain.Lesson) error {
	if err := r.tx.checkWritable(); err != nil {
		return err
	}

	if !r.tx.state.lessons.has(l.ID) {
		return notFound("lesson")
	}

	return r.put(l)
}

func (r lessonRepo) put(l domain.Lesson) error {
	s := &r.tx.state
	l.StartsAt, l.EndsAt = instant(l.StartsAt), instant(l.EndsAt)

	switch {
	case (!l.ClassID.IsZero() && !s.classes.has(l.ClassID)) || !s.courseTypes.has(l.CourseTypeID) ||
		!s.rooms.has(l.RoomID) || !s.staff.has(l.InstructorID) ||
		(!l.AccompanistID.IsZero() && !s.staff.has(l.AccompanistID)):
		return conflict("lesson references a missing record")
	case !l.ClassID.IsZero() && s.lessons.exists(func(o domain.Lesson) bool {
		return o.ID != l.ID && o.ClassID == l.ClassID && o.StartsAt.Equal(l.StartsAt)
	}):
		return conflict("class already has a lesson at %s", l.StartsAt)
	}

	s.lessons.put(r.tx.gen, l.ID, l)

	return nil
}

func (r lessonRepo) ListRange(_ context.Context, from, to time.Time) ([]domain.Lesson, error) {
	// Bounds get the precision of stored instants, as in PostgreSQL.
	from, to = instant(from), instant(to)
	lessons := r.tx.state.lessons.filter(func(l domain.Lesson) bool {
		return !l.StartsAt.Before(from) && l.StartsAt.Before(to)
	})

	return sorted(lessons, func(a, b domain.Lesson) int {
		return cmp.Or(a.StartsAt.Compare(b.StartsAt), compareID(a.ID, b.ID))
	}), nil
}

// Lock needs no lock of its own: the store mutex already serialises every
// transaction.
func (r lessonRepo) Lock(ctx context.Context, id domain.LessonID) (domain.Lesson, error) {
	if err := r.tx.checkWritable(); err != nil {
		return domain.Lesson{}, err
	}

	return r.Get(ctx, id)
}

// ---------------------------------------------------------------- bookings

type bookingRepo struct{ tx *tx }

func active(b domain.Booking) bool { return b.Status != domain.BookingCanceled }

func (r bookingRepo) Get(_ context.Context, id domain.BookingID) (domain.Booking, error) {
	b, ok := r.tx.state.bookings.get(id)
	if !ok {
		return domain.Booking{}, notFound("booking")
	}

	return b, nil
}

func (r bookingRepo) Create(_ context.Context, b domain.Booking) error {
	if err := r.tx.checkWritable(); err != nil {
		return err
	}

	if r.tx.state.bookings.has(b.ID) {
		return conflict("booking %s exists", b.ID)
	}

	return r.put(b)
}

func (r bookingRepo) Update(_ context.Context, b domain.Booking) error {
	if err := r.tx.checkWritable(); err != nil {
		return err
	}

	if !r.tx.state.bookings.has(b.ID) {
		return notFound("booking")
	}

	return r.put(b)
}

func (r bookingRepo) put(b domain.Booking) error {
	s := &r.tx.state
	b.BookedAt, b.CanceledAt = instant(b.BookedAt), instant(b.CanceledAt)

	switch {
	case !s.lessons.has(b.LessonID) || (!b.PersonID.IsZero() && !s.persons.has(b.PersonID)):
		return conflict("booking references a missing record")
	case !b.PersonID.IsZero() && active(b) && s.bookings.exists(func(o domain.Booking) bool {
		return o.ID != b.ID && o.LessonID == b.LessonID && o.PersonID == b.PersonID && active(o)
	}):
		return conflict("person already has an active booking for the lesson")
	}

	s.bookings.put(r.tx.gen, b.ID, b)

	return nil
}

func (r bookingRepo) ListByLesson(_ context.Context, lesson domain.LessonID) ([]domain.Booking, error) {
	bookings := r.tx.state.bookings.filter(func(b domain.Booking) bool { return b.LessonID == lesson })

	return sorted(bookings, func(a, b domain.Booking) int {
		return cmp.Or(a.BookedAt.Compare(b.BookedAt), compareID(a.ID, b.ID))
	}), nil
}

func (r bookingRepo) CountActive(_ context.Context, lesson domain.LessonID) (int, error) {
	return len(r.tx.state.bookings.filter(func(b domain.Booking) bool { return b.LessonID == lesson && active(b) })), nil
}

func (r bookingRepo) AddEvent(_ context.Context, e domain.BookingEvent) error {
	s := &r.tx.state
	if err := r.tx.checkWritable(); err != nil {
		return err
	}

	switch {
	case s.bookingEvents.has(e.ID):
		return conflict("booking event %s exists", e.ID)
	case !s.bookings.has(e.BookingID) || (!e.ActorAccountID.IsZero() && !s.accounts.has(e.ActorAccountID)):
		return conflict("booking event references a missing record")
	}

	e.At = instant(e.At)
	s.bookingEvents.put(r.tx.gen, e.ID, e)

	return nil
}

func (r bookingRepo) ListEvents(_ context.Context, booking domain.BookingID) ([]domain.BookingEvent, error) {
	events := r.tx.state.bookingEvents.filter(func(e domain.BookingEvent) bool { return e.BookingID == booking })

	return sorted(events, func(a, b domain.BookingEvent) int {
		return cmp.Or(a.At.Compare(b.At), compareID(a.ID, b.ID))
	}), nil
}
