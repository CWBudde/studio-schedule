package service

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/seed"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// oneOff creates a one-off lesson on the school date at start for the given
// minutes, saved even in the past or in a taken room.
func (f *genFixture) oneOff(t *testing.T, svc *LessonService, date string, start domain.TimeOfDay, minutes int) domain.Lesson {
	t.Helper()

	d := f.lessonData()
	d.Date, d.StartTime, d.Duration = mustDate(t, date), start, time.Duration(minutes)*time.Minute
	d.AccompanistID, d.Visibility, d.Capacity = f.pianist, domain.VisibilityHidden, 7
	d.ConfirmPast, d.ConfirmRoomOverlap = true, true

	sl, err := svc.CreateLesson(t.Context(), NewLesson{LessonData: d, CourseTypeID: f.courseType})
	if err != nil {
		t.Fatal(err)
	}

	return sl.Lesson
}

// cloneSummary is a comparable view of a clone result without the copies' IDs.
type cloneSummary struct {
	created []string
	skipped []string
}

func summarize(r CloneResult) cloneSummary {
	var s cloneSummary

	for _, c := range r.Created {
		s.created = append(s.created, c.Source.Lesson.ID.String()+"@"+c.StartsAt.UTC().Format(time.RFC3339))
	}

	for _, k := range r.Skipped {
		e := k.Source.Lesson.ID.String() + "@" + k.StartsAt.UTC().Format(time.RFC3339) + ":" + string(k.Reason)
		if k.TakenBy != nil {
			e += ":"
			if !k.TakenBy.CopyOf.IsZero() {
				e += k.TakenBy.CopyOf.String()
			}

			e += "/" + k.TakenBy.Lesson.StartsAt.UTC().Format(time.RFC3339)
		}

		s.skipped = append(s.skipped, e)
	}

	return s
}

func (s cloneSummary) equal(o cloneSummary) bool {
	return slices.Equal(s.created, o.created) && slices.Equal(s.skipped, o.skipped)
}

// TestCloneDay clones Thursday 2026-10-22 (summer time) to Thursday
// 2026-10-29 (winter time) and checks each skip reason, the preview, a
// repeated clone and that class edits and generation leave the copies alone.
func TestCloneDay(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newGenFixture(t, seed.DemoSettings())
	svc := f.lessonService(t, lessonNow)
	timetable := f.timetable(t, lessonNow)

	// A Thursday class at 19:00: its lesson is skipped, generation makes
	// the one of the target week.
	cd := f.classData()
	cd.Weekday, cd.StartTime = time.Thursday, domain.TimeOfDay{Hour: 19}

	class, err := timetable.CreateClass(ctx, f.term.ID, cd)
	if err != nil {
		t.Fatal(err)
	}

	workshop := f.oneOff(t, svc, "2026-10-22", domain.TimeOfDay{Hour: 17}, 90)
	f.bookFor(t, workshop, f.person(t, "Paula", "paula@example.org", "", ""))

	canceled := f.oneOff(t, svc, "2026-10-22", domain.TimeOfDay{Hour: 9}, 60)
	if _, err := svc.CancelLesson(ctx, Principal{}, canceled.ID, ""); err != nil {
		t.Fatal(err)
	}

	noon := f.oneOff(t, svc, "2026-10-22", domain.TimeOfDay{Hour: 12}, 60)
	blocker := f.oneOff(t, svc, "2026-10-29", domain.TimeOfDay{Hour: 12, Minute: 30}, 60)
	first := f.oneOff(t, svc, "2026-10-22", domain.TimeOfDay{Hour: 14}, 60)
	second := f.oneOff(t, svc, "2026-10-22", domain.TimeOfDay{Hour: 14, Minute: 30}, 60)
	classLesson := f.lessonAt(t, class.ID, "2026-10-22")

	req := CloneRequest{Unit: CloneDay, Source: mustDate(t, "2026-10-22"), Target: mustDate(t, "2026-10-29")}

	preview, err := svc.PreviewClone(ctx, req)
	if err != nil {
		t.Fatal(err)
	}

	before := len(f.lessons(t))

	res, err := svc.Clone(ctx, req)
	if err != nil {
		t.Fatal(err)
	}

	want := cloneSummary{
		created: []string{
			first.ID.String() + "@2026-10-29T13:00:00Z",
			workshop.ID.String() + "@2026-10-29T16:00:00Z",
		},
		skipped: []string{
			noon.ID.String() + "@2026-10-29T11:00:00Z:room-taken:/2026-10-29T11:30:00Z",
			second.ID.String() + "@2026-10-29T13:30:00Z:room-taken:" + first.ID.String() + "/2026-10-29T13:00:00Z",
			classLesson.ID.String() + "@2026-10-29T18:00:00Z:class-slot",
		},
	}

	if got := summarize(res); !got.equal(want) {
		t.Errorf("clone\n got %+v\nwant %+v", got, want)
	}

	if got := summarize(preview); !got.equal(want) {
		t.Errorf("preview\n got %+v\nwant %+v", got, want)
	}

	for _, c := range preview.Created {
		if !c.ID.IsZero() {
			t.Errorf("preview copy has ID %s", c.ID)
		}
	}

	if taken := res.Skipped[0].TakenBy; taken.Lesson.ID != blocker.ID || taken.CourseType.ID != f.courseType {
		t.Errorf("room taken by %+v, want %s", taken, blocker.ID)
	}

	if taken := res.Skipped[1].TakenBy; taken.Lesson.ID != res.Created[0].ID || taken.CourseType.ID != f.courseType {
		t.Errorf("room taken by %+v, want the copy %s", taken, res.Created[0].ID)
	}

	if after := len(f.lessons(t)); after != before+2 {
		t.Fatalf("%d lessons, want %d", after, before+2)
	}

	// The copy keeps everything but the time and has no bookings; 17:00
	// stays 17:00 across the change to winter time.
	cp := f.lesson(t, res.Created[1].ID)
	if !cp.ClassID.IsZero() || !cp.SlotDate.IsZero() || cp.Detached || cp.Status != domain.LessonScheduled ||
		cp.CourseTypeID != workshop.CourseTypeID || cp.RoomID != workshop.RoomID ||
		cp.InstructorID != workshop.InstructorID || cp.AccompanistID != f.pianist || cp.Capacity != 7 ||
		cp.Kind != domain.LessonWorkshop || cp.Visibility != domain.VisibilityHidden ||
		!cp.StartsAt.Equal(mustInstant(t, "2026-10-29T16:00:00Z")) ||
		!cp.EndsAt.Equal(mustInstant(t, "2026-10-29T17:30:00Z")) || cp.CancelNote != "" {
		t.Errorf("copy %+v", cp)
	}

	if local := cp.StartsAt.In(berlin(t)); local.Hour() != 17 || local.Minute() != 0 {
		t.Errorf("copy starts at %v local, want 17:00", local)
	}

	if booked, err := f.countActive(t, cp.ID); err != nil || booked != 0 {
		t.Errorf("copy has %d bookings (%v)", booked, err)
	}

	// A second clone finds every room taken and creates nothing.
	again, err := svc.Clone(ctx, req)
	if err != nil {
		t.Fatal(err)
	}

	if len(again.Created) != 0 || len(again.Skipped) != 5 {
		t.Fatalf("again: %+v", summarize(again))
	}

	for _, k := range again.Skipped {
		if k.Reason != SkipRoomTaken && k.Source.Lesson.ID != classLesson.ID {
			t.Errorf("again skips %s for %s", k.Source.Lesson.ID, k.Reason)
		}
	}

	if after := len(f.lessons(t)); after != before+2 {
		t.Fatalf("%d lessons after the second clone, want %d", after, before+2)
	}

	// Class edits and generation leave the copies alone.
	cd.StartTime = domain.TimeOfDay{Hour: 17}
	if _, err := timetable.UpdateClass(ctx, class.ID, cd); err != nil {
		t.Fatal(err)
	}

	if _, err := generatorAt(f.st, mustInstant(t, lessonNow)).GenerateAhead(ctx); err != nil {
		t.Fatal(err)
	}

	for _, c := range res.Created {
		if got := f.lesson(t, c.ID); !got.StartsAt.Equal(c.StartsAt) || !got.ClassID.IsZero() {
			t.Errorf("copy %s changed: %+v", c.ID, got)
		}
	}

	t.Run("started", func(t *testing.T) {
		// At 13:15 on the target day the 12:00 lesson has started; the
		// 14:00 one has not.
		late := f.lessonService(t, "2026-10-29T12:15:00Z")

		got, err := late.PreviewClone(ctx, req)
		if err != nil {
			t.Fatal(err)
		}

		reasons := map[domain.LessonID]SkipReason{}
		for _, k := range got.Skipped {
			reasons[k.Source.Lesson.ID] = k.Reason
		}

		if reasons[noon.ID] != SkipStarted || reasons[first.ID] != SkipRoomTaken {
			t.Errorf("skipped %+v", summarize(got))
		}
	})
}

// TestCloneWeek clones the week of 2026-11-02 to the week of 2026-11-16 and
// to a week after the term, where the class has no slot.
func TestCloneWeek(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newGenFixture(t, seed.DemoSettings())
	svc := f.lessonService(t, lessonNow)

	class, err := f.timetable(t, lessonNow).CreateClass(ctx, f.term.ID, f.classData())
	if err != nil {
		t.Fatal(err)
	}

	monday := f.oneOff(t, svc, "2026-11-02", domain.TimeOfDay{Hour: 9}, 60)
	sunday := f.oneOff(t, svc, "2026-11-08", domain.TimeOfDay{Hour: 20}, 60)
	f.oneOff(t, svc, "2026-11-09", domain.TimeOfDay{Hour: 9}, 60) // the next week
	tuesday := f.lessonAt(t, class.ID, "2026-11-03")

	res, err := svc.Clone(ctx, CloneRequest{
		Unit: CloneWeek, Source: mustDate(t, "2026-11-02"), Target: mustDate(t, "2026-11-16"),
	})
	if err != nil {
		t.Fatal(err)
	}

	want := cloneSummary{
		created: []string{
			monday.ID.String() + "@2026-11-16T08:00:00Z",
			sunday.ID.String() + "@2026-11-22T19:00:00Z",
		},
		skipped: []string{tuesday.ID.String() + "@2026-11-17T17:00:00Z:class-slot"},
	}
	if got := summarize(res); !got.equal(want) {
		t.Errorf("clone\n got %+v\nwant %+v", got, want)
	}

	// After the term (it ends on Saturday 2027-07-31) the class has no
	// slot: its lesson is copied as a one-off lesson.
	res, err = svc.PreviewClone(ctx, CloneRequest{
		Unit: CloneWeek, Source: mustDate(t, "2026-11-02"), Target: mustDate(t, "2027-08-02"),
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(res.Created) != 3 || len(res.Skipped) != 0 ||
		!res.Created[1].StartsAt.Equal(mustInstant(t, "2027-08-03T16:00:00Z")) {
		t.Errorf("after the term: %+v", summarize(res))
	}
}

func TestCloneValidation(t *testing.T) {
	t.Parallel()

	f := newGenFixture(t, seed.DemoSettings())
	svc := f.lessonService(t, lessonNow) // Wednesday 2026-10-21

	for _, tc := range []struct {
		name  string
		req   CloneRequest
		field string
	}{
		{"unit", CloneRequest{Unit: "month", Source: mustDate(t, "2026-10-19"), Target: mustDate(t, "2026-10-26")}, "unit"},
		{"source not a Monday", CloneRequest{Unit: CloneWeek, Source: mustDate(t, "2026-10-20"), Target: mustDate(t, "2026-10-26")}, "source"},
		{"target not a Monday", CloneRequest{Unit: CloneWeek, Source: mustDate(t, "2026-10-19"), Target: mustDate(t, "2026-10-27")}, "target"},
		{"same week", CloneRequest{Unit: CloneWeek, Source: mustDate(t, "2026-10-26"), Target: mustDate(t, "2026-10-26")}, "target"},
		{"same day", CloneRequest{Unit: CloneDay, Source: mustDate(t, "2026-10-22"), Target: mustDate(t, "2026-10-22")}, "target"},
		{"past day", CloneRequest{Unit: CloneDay, Source: mustDate(t, "2026-10-13"), Target: mustDate(t, "2026-10-20")}, "target"},
		{"current week", CloneRequest{Unit: CloneWeek, Source: mustDate(t, "2026-10-12"), Target: mustDate(t, "2026-10-19")}, "target"},
		{"invalid target", CloneRequest{Unit: CloneDay, Source: mustDate(t, "2026-10-12")}, "target"},
		{"invalid source", CloneRequest{Unit: CloneDay, Target: mustDate(t, "2026-10-29")}, "source"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := svc.PreviewClone(t.Context(), tc.req)
			wantFieldError(t, err, tc.field)

			_, err = svc.Clone(t.Context(), tc.req)
			wantFieldError(t, err, tc.field)
		})
	}

	// Today is a valid target, and any day may be the source.
	if _, err := svc.Clone(t.Context(), CloneRequest{
		Unit: CloneDay, Source: mustDate(t, "2026-01-01"), Target: mustDate(t, "2026-10-21"),
	}); err != nil {
		t.Fatal(err)
	}
}

// countActive returns the active bookings of lesson id.
func (f *genFixture) countActive(t *testing.T, id domain.LessonID) (int, error) {
	t.Helper()

	var n int

	err := f.st.InTx(t.Context(), store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		var err error
		n, err = tx.Bookings().CountActive(ctx, id)

		return err
	})

	return n, err
}

// TestCloneUngeneratedClassLesson checks that a lesson a class has not
// generated yet, beyond the horizon, takes its room like one that exists,
// and that a canceled one does not (generation does not make it again).
func TestCloneUngeneratedClassLesson(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newGenFixture(t, seed.DemoSettings())
	svc := f.lessonService(t, lessonNow)

	// A Thursday class at 17:00; generation reaches 2026-11-19, so it has
	// no lesson on 2026-12-10 yet.
	cd := f.classData()
	cd.Weekday, cd.StartTime = time.Thursday, domain.TimeOfDay{Hour: 17}

	class, err := f.timetable(t, lessonNow).CreateClass(ctx, f.term.ID, cd)
	if err != nil {
		t.Fatal(err)
	}

	evening := f.oneOff(t, svc, "2026-10-22", domain.TimeOfDay{Hour: 17, Minute: 30}, 60)
	classLesson := f.lessonAt(t, class.ID, "2026-10-22")
	req := CloneRequest{Unit: CloneDay, Source: mustDate(t, "2026-10-22"), Target: mustDate(t, "2026-12-10")}

	res, err := svc.PreviewClone(ctx, req)
	if err != nil {
		t.Fatal(err)
	}

	want := cloneSummary{skipped: []string{
		classLesson.ID.String() + "@2026-12-10T16:00:00Z:class-slot",
		evening.ID.String() + "@2026-12-10T16:30:00Z:room-taken:/2026-12-10T16:00:00Z",
	}}
	if got := summarize(res); !got.equal(want) {
		t.Fatalf("preview\n got %+v\nwant %+v", got, want)
	}

	if by := res.Skipped[1].TakenBy; !by.Lesson.ID.IsZero() || !by.CopyOf.IsZero() || by.Lesson.ClassID != class.ID ||
		!by.Lesson.EndsAt.Equal(mustInstant(t, "2026-12-10T17:00:00Z")) || by.CourseType.ID != f.courseType {
		t.Errorf("room taken by %+v, want the ungenerated lesson of class %s", by, class.ID)
	}

	// Once generated, the lesson exists and takes the room with its ID.
	if _, err := generatorAt(f.st, mustInstant(t, lessonNow)).Generate(ctx, req.Target, req.Target.AddDays(1)); err != nil {
		t.Fatal(err)
	}

	generated := f.lessonAt(t, class.ID, "2026-12-10")

	res, err = svc.PreviewClone(ctx, req)
	if err != nil {
		t.Fatal(err)
	}

	if len(res.Skipped) != 2 || res.Skipped[1].TakenBy == nil || res.Skipped[1].TakenBy.Lesson.ID != generated.ID {
		t.Fatalf("after generation: %+v", summarize(res))
	}

	// A canceled lesson frees the room, and generation does not refill it.
	if _, err := svc.CancelLesson(ctx, Principal{}, generated.ID, ""); err != nil {
		t.Fatal(err)
	}

	res, err = svc.Clone(ctx, req)
	if err != nil {
		t.Fatal(err)
	}

	want = cloneSummary{
		created: []string{evening.ID.String() + "@2026-12-10T16:30:00Z"},
		skipped: []string{classLesson.ID.String() + "@2026-12-10T16:00:00Z:class-slot"},
	}
	if got := summarize(res); !got.equal(want) {
		t.Errorf("after the cancellation\n got %+v\nwant %+v", got, want)
	}
}

// TestCloneLocksAndReads checks what a clone locks and reads: all terms
// first in the order of TermRepo.List, then the classes term by term in
// the order of ClassRepo.ListByTerm, as generation does; and it takes the
// classes and course types it has loaded instead of getting them again.
func TestCloneLocksAndReads(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newGenFixture(t, seed.DemoSettings())
	timetable := f.timetable(t, lessonNow)

	// A second term that starts after the fixture's, so it comes second.
	autumn := domain.Term{
		ID: ids.New[domain.TermID](f.seq), Name: "Fictional Autumn",
		StartsOn: mustDate(t, "2026-10-01"), EndsOn: mustDate(t, "2026-12-31"),
	}
	f.write(t, func(ctx context.Context, tx store.Tx) error { return tx.Terms().Create(ctx, autumn) })

	// On Thursday the autumn class comes first, the fixture term's later.
	cd := f.classData()
	cd.Weekday, cd.StartTime = time.Thursday, domain.TimeOfDay{Hour: 10}

	early, err := timetable.CreateClass(ctx, autumn.ID, cd)
	if err != nil {
		t.Fatal(err)
	}

	cd.StartTime = domain.TimeOfDay{Hour: 19}

	late, err := timetable.CreateClass(ctx, f.term.ID, cd)
	if err != nil {
		t.Fatal(err)
	}

	// A one-off lesson whose room is taken in the target week.
	svc := f.lessonService(t, lessonNow)
	f.oneOff(t, svc, "2026-10-22", domain.TimeOfDay{Hour: 12}, 60)
	blocker := f.oneOff(t, svc, "2026-10-29", domain.TimeOfDay{Hour: 12, Minute: 30}, 60)

	rec := &recordingStore{Store: f.st}
	recorded := NewLessonService(rec, clock.NewFixed(mustInstant(t, lessonNow)), f.seq, Mails{})

	res, err := recorded.Clone(ctx, CloneRequest{
		Unit: CloneDay, Source: mustDate(t, "2026-10-22"), Target: mustDate(t, "2026-10-29"),
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(res.Skipped) != 3 || res.Skipped[1].TakenBy == nil || res.Skipped[1].TakenBy.Lesson.ID != blocker.ID ||
		res.Skipped[1].TakenBy.CourseType.Name != "Imaginary Ballet" {
		t.Fatalf("clone: %+v", summarize(res))
	}

	want := []string{
		"lock term " + f.term.ID.String(), "lock term " + autumn.ID.String(),
		"lock class " + late.ID.String(), "lock class " + early.ID.String(),
	}
	if !slices.Equal(rec.calls, want) {
		t.Errorf("calls\n got %v\nwant %v", rec.calls, want)
	}
}

// recordingStore records the locks of terms and classes and the reads of
// single classes and course types.
type recordingStore struct {
	store.Store

	calls []string
}

func (s *recordingStore) InTx(ctx context.Context, mode store.TxMode, fn func(context.Context, store.Tx) error) error {
	return s.Store.InTx(ctx, mode, func(ctx context.Context, tx store.Tx) error {
		return fn(ctx, recordingTx{Tx: tx, s: s})
	})
}

type recordingTx struct {
	store.Tx

	s *recordingStore
}

func (t recordingTx) Terms() store.TermRepo { return recordingTerms{t.Tx.Terms(), t.s} }

func (t recordingTx) Classes() store.ClassRepo { return recordingClasses{t.Tx.Classes(), t.s} }

func (t recordingTx) CourseTypes() store.CourseTypeRepo {
	return recordingCourseTypes{t.Tx.CourseTypes(), t.s}
}

type recordingTerms struct {
	store.TermRepo

	s *recordingStore
}

func (r recordingTerms) Lock(ctx context.Context, id domain.TermID) (domain.Term, error) {
	r.s.calls = append(r.s.calls, "lock term "+id.String())

	return r.TermRepo.Lock(ctx, id)
}

type recordingClasses struct {
	store.ClassRepo

	s *recordingStore
}

func (r recordingClasses) Lock(ctx context.Context, id domain.ClassID) (domain.Class, error) {
	r.s.calls = append(r.s.calls, "lock class "+id.String())

	return r.ClassRepo.Lock(ctx, id)
}

func (r recordingClasses) Get(ctx context.Context, id domain.ClassID) (domain.Class, error) {
	r.s.calls = append(r.s.calls, "get class "+id.String())

	return r.ClassRepo.Get(ctx, id)
}

type recordingCourseTypes struct {
	store.CourseTypeRepo

	s *recordingStore
}

func (r recordingCourseTypes) Get(ctx context.Context, id domain.CourseTypeID) (domain.CourseType, error) {
	r.s.calls = append(r.s.calls, "get course type "+id.String())

	return r.CourseTypeRepo.Get(ctx, id)
}

// TestCloneStaffInactive skips a lesson whose instructor or accompanist is
// no longer active, before checking its room.
func TestCloneStaffInactive(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newGenFixture(t, seed.DemoSettings())
	svc := f.lessonService(t, lessonNow)

	cleo := domain.Staff{ID: ids.New[domain.StaffID](f.seq), DisplayName: "Cleo Example", Kind: domain.StaffInstructor, Active: true}
	dora := domain.Staff{ID: ids.New[domain.StaffID](f.seq), DisplayName: "Dora Example", Kind: domain.StaffAccompanist, Active: true}
	f.write(t, func(ctx context.Context, tx store.Tx) error {
		return errors.Join(tx.Staff().Create(ctx, cleo), tx.Staff().Create(ctx, dora))
	})

	at := func(hour int, instructor, accompanist domain.StaffID) domain.Lesson {
		l := f.oneOff(t, svc, "2026-10-22", domain.TimeOfDay{Hour: hour}, 60)

		d := dataOf(t, l)
		d.InstructorID, d.AccompanistID, d.ConfirmRoomOverlap = instructor, accompanist, true

		sl, err := svc.UpdateLesson(ctx, l.ID, d)
		if err != nil {
			t.Fatal(err)
		}

		return sl.Lesson
	}

	// Ada's lesson would also find its room taken: the staff come first.
	ada := at(9, f.instructor, dora.ID)
	f.oneOff(t, svc, "2026-10-29", domain.TimeOfDay{Hour: 9, Minute: 30}, 60)
	ben := at(11, cleo.ID, f.pianist)
	both := at(13, cleo.ID, dora.ID)
	alone := at(15, cleo.ID, domain.StaffID{})

	// Ada and Ben leave the school.
	f.write(t, func(ctx context.Context, tx store.Tx) error {
		for _, id := range []domain.StaffID{f.instructor, f.pianist} {
			s, err := tx.Staff().Get(ctx, id)
			if err != nil {
				return err
			}

			s.Active = false
			if err := tx.Staff().Update(ctx, s); err != nil {
				return err
			}
		}

		return nil
	})

	req := CloneRequest{Unit: CloneDay, Source: mustDate(t, "2026-10-22"), Target: mustDate(t, "2026-10-29")}
	want := cloneSummary{
		created: []string{both.ID.String() + "@2026-10-29T12:00:00Z", alone.ID.String() + "@2026-10-29T14:00:00Z"},
		skipped: []string{
			ada.ID.String() + "@2026-10-29T08:00:00Z:staff-inactive",
			ben.ID.String() + "@2026-10-29T10:00:00Z:staff-inactive",
		},
	}

	preview, err := svc.PreviewClone(ctx, req)
	if err != nil {
		t.Fatal(err)
	}

	res, err := svc.Clone(ctx, req)
	if err != nil {
		t.Fatal(err)
	}

	for name, r := range map[string]CloneResult{"preview": preview, "clone": res} {
		if got := summarize(r); !got.equal(want) {
			t.Errorf("%s\n got %+v\nwant %+v", name, got, want)
		}
	}
}
