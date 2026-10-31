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

// TestStatisticsStoreFaults: a store failure in any read of the statistics
// of the current term is returned. The term has a booked lesson, a
// receipt, and a lesson of a class of last summer's term moved into it,
// whose class is read on its own.
func TestStatisticsStoreFaults(t *testing.T) {
	t.Parallel()

	SweepFaults(t, FaultSweep{Setup: func(t *testing.T) (*FaultStore, func() error) {
		f := newGenFixture(t, seed.DemoSettings())
		f.statBooking(t, f.statLesson(t, "2026-10-20T16:00:00Z", 8, domain.LessonScheduled), domain.BookingBooked)
		f.statReceipt(t, 1, "2026-10-21", 9900, domain.ReceiptID{})
		f.summerClassLesson(t, "2026-10-22T16:00:00Z")

		fs := NewFaultStore(f.st)
		svc := NewStatisticsService(fs, clock.NewFixed(mustInstant(t, "2026-10-24T22:30:00Z")))

		return fs, func() error {
			_, err := svc.Statistics(t.Context(), StatisticsQuery{Range: StatisticsTerm})

			return err
		}
	}})
}

// summerClassLesson stores a class of a summer term that ended before the
// fixture's term and a lesson of it moved to start, and returns the class.
func (f *genFixture) summerClassLesson(t *testing.T, start string) domain.Class {
	t.Helper()

	summer := domain.Term{
		ID: ids.New[domain.TermID](f.seq), Name: "Fictional Summer",
		StartsOn: mustDate(t, "2026-07-01"), EndsOn: mustDate(t, "2026-08-31"),
	}
	c := domain.Class{
		ID: ids.New[domain.ClassID](f.seq), TermID: summer.ID, CourseTypeID: f.courseType, RoomID: f.room,
		InstructorID: f.instructor, Level: domain.LevelBeginner, Weekday: time.Wednesday,
		StartTime: domain.TimeOfDay{Hour: 18}, Duration: time.Hour, Capacity: 6,
	}
	at := mustInstant(t, start)
	l := domain.Lesson{
		ID: ids.New[domain.LessonID](f.seq), ClassID: c.ID, SlotDate: mustDate(t, "2026-08-26"),
		CourseTypeID: f.courseType, RoomID: f.room, InstructorID: f.instructor, StartsAt: at, EndsAt: at.Add(time.Hour),
		Status: domain.LessonScheduled, Visibility: domain.VisibilityPublic, Kind: domain.LessonRegular, Capacity: 6,
	}
	f.write(t, func(ctx context.Context, tx store.Tx) error {
		return errors.Join(tx.Terms().Create(ctx, summer), tx.Classes().Create(ctx, c), tx.Lessons().Create(ctx, l))
	})

	return c
}

// TestStatisticsMovedClassLesson counts a lesson moved out of its class's
// term for its class all the same.
func TestStatisticsMovedClassLesson(t *testing.T) {
	t.Parallel()

	f := newGenFixture(t, seed.DemoSettings())
	c := f.summerClassLesson(t, "2026-10-22T16:00:00Z")

	got := statistics(t, f.st, "2026-10-24T22:30:00Z", StatisticsQuery{Range: StatisticsLast4Weeks})
	if len(got.Classes) != 1 || got.Classes[0].Class.ID != c.ID || got.Classes[0].Lessons != 1 {
		t.Fatalf("classes %+v, want the summer class with its lesson", got.Classes)
	}
}

// TestStatisticsLongTerm: a term of more than a year shows its latest year
// up to today.
func TestStatisticsLongTerm(t *testing.T) {
	t.Parallel()

	f := newGenFixture(t, seed.DemoSettings())
	f.write(t, func(ctx context.Context, tx store.Tx) error {
		f.term.StartsOn = mustDate(t, "2025-01-01")

		return tx.Terms().Update(ctx, f.term)
	})

	got := statistics(t, f.st, "2026-10-24T22:30:00Z", StatisticsQuery{Range: StatisticsTerm})
	if got.Term.ID != f.term.ID || got.To != mustDate(t, "2026-10-25") || got.From != mustDate(t, "2025-10-25") ||
		got.From.DaysUntil(got.To) != MaxStatisticsDays-1 || got.Granularity != GranularityMonth {
		t.Fatalf("term range %v to %v by %s, want 2025-10-25 to 2026-10-25 by month", got.From, got.To, got.Granularity)
	}
}

// TestStatisticsOtherCurrency leaves receipts in a currency other than the
// school's out of the sales. The currency is locked once a receipt exists
// (ADR 0012), but a currency change that reads the usage while the first
// sale is not yet committed can still leave a receipt in the old one.
func TestStatisticsOtherCurrency(t *testing.T) {
	t.Parallel()

	f := newGenFixture(t, seed.DemoSettings())
	f.statReceipt(t, 1, "2026-10-20", 9900, domain.ReceiptID{})
	f.write(t, func(ctx context.Context, tx store.Tx) error {
		s, err := tx.Settings().Get(ctx)
		if err != nil {
			return err
		}

		s.Currency = "CHF"

		return tx.Settings().Put(ctx, s)
	})

	got := statistics(t, f.st, "2026-10-24T22:30:00Z", StatisticsQuery{Range: StatisticsLast4Weeks})
	if got.Currency != "CHF" || got.Totals.PassesSold != 0 || got.Totals.Gross != 0 {
		t.Fatalf("sales %+v in %s, want none in CHF", got.Totals.SalesCounts, got.Currency)
	}
}

// TestStatisticsRangeList lists every range the statistics accept.
func TestStatisticsRangeList(t *testing.T) {
	t.Parallel()

	f := newGenFixture(t, seed.DemoSettings())
	svc := NewStatisticsService(f.st, clock.NewFixed(mustInstant(t, "2026-10-24T22:30:00Z")))
	ranges := StatisticsRanges()

	for _, r := range ranges {
		q := StatisticsQuery{Range: r, From: mustDate(t, "2026-10-01"), To: mustDate(t, "2026-10-24")}
		if _, err := svc.Statistics(t.Context(), q); err != nil {
			t.Errorf("range %s: %v", r, err)
		}
	}

	if !slices.Equal(ranges, []StatisticsRange{StatisticsLast4Weeks, StatisticsLast12Weeks, StatisticsTerm, StatisticsCustom}) {
		t.Errorf("ranges %v", ranges)
	}
}
