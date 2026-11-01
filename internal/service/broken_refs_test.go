package service

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"

	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/store"
	"github.com/cwbudde/studio-schedule/internal/store/memory"
)

// dropRows deletes the rows with the given IDs from a table of the memory
// store, bypassing its reference checks, through a snapshot: broken data
// that the stores' foreign keys otherwise rule out.
func dropRows(t *testing.T, st *memory.Store, table string, ids ...string) {
	t.Helper()

	const build = "broken-refs"

	b, err := st.Snapshot(build)
	if err != nil {
		t.Fatal(err)
	}

	var doc map[string]json.RawMessage
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}

	var tables map[string]json.RawMessage
	if err := json.Unmarshal(doc["tables"], &tables); err != nil {
		t.Fatal(err)
	}

	var rows []json.RawMessage
	if err := json.Unmarshal(tables[table], &rows); err != nil {
		t.Fatalf("table %s: %v", table, err)
	}

	kept := slices.DeleteFunc(slices.Clone(rows), func(r json.RawMessage) bool {
		var row struct{ ID string }
		if err := json.Unmarshal(r, &row); err != nil {
			t.Fatal(err)
		}

		return slices.Contains(ids, row.ID)
	})
	if len(kept) != len(rows)-len(ids) {
		t.Fatalf("table %s: dropped %d rows, want %d", table, len(rows)-len(kept), len(ids))
	}

	marshal := func(v any) json.RawMessage {
		out, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}

		return out
	}

	tables[table] = marshal(kept)
	doc["tables"] = marshal(tables)

	if err := st.Restore(marshal(doc), build); err != nil {
		t.Fatal(err)
	}
}

// TestBrokenReferences: a lesson whose room, location, course type,
// instructor, accompanist or class is missing, or a booking whose person
// is, is broken data. Every read path reports it as such, never as a 404
// that would tell the caller the lesson does not exist.
func TestBrokenReferences(t *testing.T) {
	t.Parallel()

	for name, drop := range map[string]func(f *staffFixture, s scene) (string, []string){
		"room": func(f *staffFixture, _ scene) (string, []string) { return "rooms", []string{f.room.String()} },
		"course type": func(f *staffFixture, _ scene) (string, []string) {
			return "courseTypes", []string{f.courseType.String()}
		},
		"instructor":  func(f *staffFixture, _ scene) (string, []string) { return "staff", []string{f.instructor.String()} },
		"accompanist": func(f *staffFixture, _ scene) (string, []string) { return "staff", []string{f.pianist.String()} },
		"location": func(f *staffFixture, _ scene) (string, []string) {
			return "locations", []string{f.roomLocation(t).String()}
		},
		"class": func(_ *staffFixture, s scene) (string, []string) {
			return "classes", []string{s.class.String(), s.stray.ClassID.String()}
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			ctx := t.Context()
			f := newStaffFixture(t)
			s := f.newScene(t)
			table, ids := drop(f, s)
			dropRows(t, f.st, table, ids...)

			anna, olga := f.earlyAnna(t), f.principal(t, f.olga)
			sched := NewScheduleService(f.st, clock.NewFixed(mustInstant(t, bookingNow)))
			q := ScheduleQuery{From: mustDate(t, "2026-10-21"), To: mustDate(t, "2026-10-28")}

			_, err := sched.Schedule(ctx, anna, q)
			wantBroken(t, "schedule", err)

			_, err = sched.Lesson(ctx, anna, s.stray.ID)
			wantBroken(t, "lesson", err)

			_, err = f.teach(t, bookingNow).Lessons(ctx, olga, TeachQuery{})
			wantBroken(t, "teach", err)

			_, err = f.myLessons(t, bookingNow).Upcoming(ctx, f.principal(t, f.anna))
			wantBroken(t, "my lessons", err)
		})
	}

	// The stray class is for the level Advanced (strayClassLesson).
	t.Run("level", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		f := newStaffFixture(t)
		s := f.newScene(t)

		var stray domain.Class

		f.write(t, func(ctx context.Context, tx store.Tx) error {
			var err error
			stray, err = tx.Classes().Get(ctx, s.stray.ClassID)

			return err
		})
		dropRows(t, f.st, "levels", stray.Levels.From.String())

		anna, olga := f.earlyAnna(t), f.principal(t, f.olga)
		sched := NewScheduleService(f.st, clock.NewFixed(mustInstant(t, bookingNow)))
		q := ScheduleQuery{From: mustDate(t, "2026-10-21"), To: mustDate(t, "2026-10-28")}

		_, err := sched.Schedule(ctx, anna, q)
		wantBroken(t, "schedule", err)

		_, err = sched.Lesson(ctx, anna, s.stray.ID)
		wantBroken(t, "lesson", err)

		_, err = f.teach(t, bookingNow).Lessons(ctx, olga, TeachQuery{})
		wantBroken(t, "teach", err)

		_, err = f.timetable(t, bookingNow).AllClasses(ctx, stray.TermID)
		wantBroken(t, "classes", err)
	})

	t.Run("booked person", func(t *testing.T) {
		t.Parallel()

		f := newStaffFixture(t)
		l := f.lessonWithPianist(t, 5)
		f.bookAs(t, f.sam, l)
		dropRows(t, f.st, "persons", f.sam.ID.String())

		_, err := f.participants(t, bookingNow).Participants(t.Context(), f.principal(t, f.ivy), l.ID)
		wantBroken(t, "participants", err)
	})
}

// roomLocation returns the location of the fixture's room.
func (f *staffFixture) roomLocation(t *testing.T) domain.LocationID {
	t.Helper()

	var room domain.Room

	err := f.st.InTx(t.Context(), store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		var err error
		room, err = tx.Rooms().Get(ctx, f.room)

		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	return room.LocationID
}

func wantBroken(t *testing.T, what string, err error) {
	t.Helper()

	if !errors.Is(err, errBrokenReference) || errors.Is(err, domain.ErrNotFound) {
		t.Errorf("%s: want the broken reference, not a 404, got %v", what, err)
	}
}
