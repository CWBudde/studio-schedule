package postgres

import (
	"fmt"
	"math"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/cwbudde/studio-schedule/internal/domain"
)

// Conversions between domain values and their column types. Optional values
// map their zero value to NULL and back (ADR 0002).

func pgID[I domain.AnyID](id I) pgtype.UUID {
	return pgtype.UUID{Bytes: [16]byte(id), Valid: !id.IsZero()}
}

func pgIDs[I domain.AnyID](ids []I) []pgtype.UUID {
	out := make([]pgtype.UUID, len(ids))
	for i, id := range ids {
		out[i] = pgID(id)
	}

	return out
}

func fromPgID[I domain.AnyID](u pgtype.UUID) I {
	if !u.Valid {
		return I{}
	}

	return I(u.Bytes)
}

// pgText maps an optional string; "" is NULL.
func pgText(s string) pgtype.Text {
	return pgtype.Text{String: s, Valid: s != ""}
}

// pgInstant maps an optional instant; the zero time is NULL. It truncates to
// microseconds, the precision of timestamptz, as PostgreSQL would.
func pgInstant(t time.Time) pgtype.Timestamptz {
	if t.IsZero() {
		return pgtype.Timestamptz{}
	}

	return pgtype.Timestamptz{Time: instant(t), Valid: true}
}

// fromPgInstant returns the instant in UTC (pgx decodes into time.Local).
func fromPgInstant(ts pgtype.Timestamptz) time.Time {
	if !ts.Valid {
		return time.Time{}
	}

	return ts.Time.UTC()
}

// instant normalizes t like a timestamptz round trip: UTC, microseconds.
func instant(t time.Time) time.Time {
	return t.UTC().Truncate(time.Microsecond)
}

// pgDate maps an optional date; the zero Date is NULL.
func pgDate(d domain.Date) pgtype.Date {
	if d.IsZero() {
		return pgtype.Date{}
	}

	return pgtype.Date{Time: time.Date(d.Year, d.Month, d.Day, 0, 0, 0, 0, time.UTC), Valid: true}
}

func fromPgDate(d pgtype.Date) domain.Date {
	if !d.Valid {
		return domain.Date{}
	}

	return domain.Date{Year: d.Time.Year(), Month: d.Time.Month(), Day: d.Time.Day()}
}

func pgTimeOfDay(t domain.TimeOfDay) pgtype.Time {
	return pgtype.Time{Microseconds: t.SinceMidnight().Microseconds(), Valid: true}
}

func fromPgTimeOfDay(t pgtype.Time) domain.TimeOfDay {
	d := time.Duration(t.Microseconds) * time.Microsecond

	return domain.TimeOfDay{Hour: int(d / time.Hour), Minute: int(d % time.Hour / time.Minute)}
}

// pgInterval stores a duration as microseconds only, so that it compares
// exactly (no day or month parts).
func pgInterval(d time.Duration) pgtype.Interval {
	return pgtype.Interval{Microseconds: d.Microseconds(), Valid: true}
}

// fromPgInterval also accepts day and month parts written by hand, counting
// them like PostgreSQL compares intervals (1 day = 24 h, 1 month = 30 days).
func fromPgInterval(iv pgtype.Interval) time.Duration {
	const day = 24 * time.Hour

	return time.Duration(iv.Months)*30*day + time.Duration(iv.Days)*day +
		time.Duration(iv.Microseconds)*time.Microsecond
}

// int4 converts n for an integer column. Validate keeps the numbers small, so
// an overflow is a bug and fails the write.
func int4(n int) (int32, error) {
	if n < math.MinInt32 || n > math.MaxInt32 {
		return 0, fmt.Errorf("postgres: %d does not fit an integer column: %w", n, domain.ErrValidation)
	}

	return int32(n), nil
}

// int4Opt is int4 for an optional number; 0 is NULL.
func int4Opt(n int) (pgtype.Int4, error) {
	if n == 0 {
		return pgtype.Int4{}, nil
	}

	v, err := int4(n)

	return pgtype.Int4{Int32: v, Valid: err == nil}, err
}

// weekday converts d for a smallint column.
func weekday(d time.Weekday) (int16, error) {
	if d < 0 || d > time.Saturday {
		return 0, fmt.Errorf("postgres: invalid weekday %d: %w", d, domain.ErrValidation)
	}

	return int16(d), nil
}
