package postgres

import (
	"context"
	"uuid"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/store"
	"github.com/cwbudde/studio-schedule/internal/store/postgres/internal/sqlc"
)

// The reads of the integrity checks (db check), one query each
// (queries/integrity.sql).

func (r passRepo) ListUnbalanced(ctx context.Context) ([]store.PassBalance, error) {
	rows, err := r.q.ListUnbalancedPasses(ctx)

	return convertRows(rows, err, func(row sqlc.ListUnbalancedPassesRow) store.PassBalance {
		return store.PassBalance{Pass: fromPgID[domain.PassID](row.ID), Used: int(row.Used), Redeemed: int(row.Redeemed)}
	})
}

func (r redemptionRepo) ListStray(ctx context.Context) ([]store.StrayRedemption, error) {
	rows, err := r.q.ListStrayRedemptions(ctx)

	return convertRows(rows, err, func(row sqlc.ListStrayRedemptionsRow) store.StrayRedemption {
		return store.StrayRedemption{
			Redemption: domain.PassRedemption{
				PassID: fromPgID[domain.PassID](row.PassID), BookingID: fromPgID[domain.BookingID](row.BookingID),
				At: fromPgInstant(row.At),
			},
			Lesson:         fromPgID[domain.LessonID](row.LessonID),
			CanceledInTime: row.CanceledInTime,
			LessonCanceled: row.LessonCanceled,
		}
	})
}

func (r bookingRepo) ListOverCapacity(ctx context.Context) ([]store.LessonOccupancy, error) {
	rows, err := r.q.ListLessonsOverCapacity(ctx)

	return convertRows(rows, err, func(row sqlc.ListLessonsOverCapacityRow) store.LessonOccupancy {
		return store.LessonOccupancy{
			Lesson: fromPgID[domain.LessonID](row.ID), Capacity: int(row.Capacity), Active: int(row.Active),
			Overrides: int(row.Overrides),
		}
	})
}

func (r receiptCounterRepo) ListStates(ctx context.Context) ([]store.ReceiptCounterState, error) {
	rows, err := r.q.ListReceiptCounterStates(ctx)

	return convertRows(rows, err, func(row sqlc.ListReceiptCounterStatesRow) store.ReceiptCounterState {
		return store.ReceiptCounterState{Year: int(row.Year), Last: int(row.Last), Highest: int(row.Highest)}
	})
}

func (r personRepo) ListTombstoneReferences(ctx context.Context) ([]store.TombstoneReference, error) {
	rows, err := r.q.ListTombstoneReferences(ctx)

	return convertRows(rows, err, func(row sqlc.ListTombstoneReferencesRow) store.TombstoneReference {
		return store.TombstoneReference{
			Person: fromPgID[domain.PersonID](row.PersonID), Anonymized: row.Anonymized,
			Kind: store.TombstoneRefKind(row.Kind), Ref: uuidText(row.Ref),
		}
	})
}

// uuidText is the canonical text form of a UUID, like a domain ID's.
func uuidText(u pgtype.UUID) string { return uuid.UUID(u.Bytes).String() }
