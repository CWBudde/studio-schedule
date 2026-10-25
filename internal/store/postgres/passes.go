package postgres

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/store/postgres/internal/sqlc"
)

// ---------------------------------------------------------------- pass types

type passTypeRepo struct{ q *sqlc.Queries }

func toPassType(row sqlc.PassType) domain.PassType {
	return domain.PassType{
		ID:           fromPgID[domain.PassTypeID](row.ID),
		Name:         row.Name,
		Kind:         domain.PassKind(row.Kind),
		Visits:       int(row.Visits.Int32),
		ValidityDays: int(row.ValidityDays),
		Price:        domain.Money(row.Price),
		VATRate:      int(row.VatRateBp),
		Active:       row.Active,
		CreatedAt:    fromPgInstant(row.CreatedAt),
	}
}

func passTypeParams(t domain.PassType) (sqlc.CreatePassTypeParams, error) {
	visits, err := int4Opt(t.Visits)
	if err != nil {
		return sqlc.CreatePassTypeParams{}, err
	}

	days, err := int4(t.ValidityDays)
	if err != nil {
		return sqlc.CreatePassTypeParams{}, err
	}

	vat, err := int4(t.VATRate)
	if err != nil {
		return sqlc.CreatePassTypeParams{}, err
	}

	return sqlc.CreatePassTypeParams{
		ID:           pgID(t.ID),
		Name:         t.Name,
		Kind:         string(t.Kind),
		Visits:       visits,
		ValidityDays: days,
		Price:        int64(t.Price),
		VatRateBp:    vat,
		Active:       t.Active,
		CreatedAt:    pgInstant(t.CreatedAt),
	}, nil
}

func (r passTypeRepo) Get(ctx context.Context, id domain.PassTypeID) (domain.PassType, error) {
	row, err := r.q.GetPassType(ctx, pgID(id))
	if err != nil {
		return domain.PassType{}, notFound("pass type", err)
	}

	return toPassType(row), nil
}

func (r passTypeRepo) Create(ctx context.Context, t domain.PassType) error {
	params, err := passTypeParams(t)
	if err != nil {
		return err
	}

	return mapErr(r.q.CreatePassType(ctx, params))
}

func (r passTypeRepo) Update(ctx context.Context, t domain.PassType) error {
	params, err := passTypeParams(t)
	if err != nil {
		return err
	}

	n, err := r.q.UpdatePassType(ctx, sqlc.UpdatePassTypeParams(params))

	return affected("pass type", n, err)
}

func (r passTypeRepo) Delete(ctx context.Context, id domain.PassTypeID) error {
	n, err := r.q.DeletePassType(ctx, pgID(id))

	return affected("pass type", n, err)
}

func (r passTypeRepo) List(ctx context.Context) ([]domain.PassType, error) {
	rows, err := r.q.ListPassTypes(ctx)

	return convertRows(rows, err, toPassType)
}

// ---------------------------------------------------------------- passes

type passRepo struct{ q *sqlc.Queries }

func toPass(row sqlc.Pass) domain.Pass {
	return domain.Pass{
		ID:              fromPgID[domain.PassID](row.ID),
		PassTypeID:      fromPgID[domain.PassTypeID](row.PassTypeID),
		PersonID:        fromPgID[domain.PersonID](row.PersonID),
		Kind:            domain.PassKind(row.Kind),
		VisitsTotal:     int(row.VisitsTotal.Int32),
		VisitsRemaining: int(row.VisitsRemaining.Int32),
		ValidFrom:       fromPgDate(row.ValidFrom),
		ValidUntil:      fromPgDate(row.ValidUntil),
		Status:          domain.PassStatus(row.Status),
		Origin:          domain.PassOrigin(row.Origin),
		SoldAt:          fromPgInstant(row.SoldAt),
		SoldBy:          fromPgID[domain.AccountID](row.SoldBy),
		PayerID:         fromPgID[domain.PersonID](row.PayerPersonID),
		ReceiptID:       fromPgID[domain.ReceiptID](row.ReceiptID),
		PricePaid:       domain.Money(row.PricePaid),
		Note:            row.Note.String,
		VoidedAt:        fromPgInstant(row.VoidedAt),
	}
}

// visitsColumn maps a visit count of a pass: NULL for a time pass, the
// number (zero included) for a visit pass.
func visitsColumn(p domain.Pass, n int) (pgtype.Int4, error) {
	if p.Kind != domain.PassVisits {
		return pgtype.Int4{}, nil
	}

	v, err := int4(n)

	return pgtype.Int4{Int32: v, Valid: err == nil}, err
}

func passParams(p domain.Pass) (sqlc.CreatePassParams, error) {
	total, err := visitsColumn(p, p.VisitsTotal)
	if err != nil {
		return sqlc.CreatePassParams{}, err
	}

	remaining, err := visitsColumn(p, p.VisitsRemaining)
	if err != nil {
		return sqlc.CreatePassParams{}, err
	}

	return sqlc.CreatePassParams{
		ID:              pgID(p.ID),
		PassTypeID:      pgID(p.PassTypeID),
		PersonID:        pgID(p.PersonID),
		Kind:            string(p.Kind),
		VisitsTotal:     total,
		VisitsRemaining: remaining,
		ValidFrom:       pgDate(p.ValidFrom),
		ValidUntil:      pgDate(p.ValidUntil),
		Status:          string(p.Status),
		Origin:          string(p.Origin),
		SoldAt:          pgInstant(p.SoldAt),
		SoldBy:          pgID(p.SoldBy),
		PayerPersonID:   pgID(p.PayerID),
		ReceiptID:       pgID(p.ReceiptID),
		PricePaid:       int64(p.PricePaid),
		Note:            pgText(p.Note),
		VoidedAt:        pgInstant(p.VoidedAt),
	}, nil
}

func (r passRepo) Get(ctx context.Context, id domain.PassID) (domain.Pass, error) {
	row, err := r.q.GetPass(ctx, pgID(id))
	if err != nil {
		return domain.Pass{}, notFound("pass", err)
	}

	return toPass(row), nil
}

func (r passRepo) GetByReceipt(ctx context.Context, receipt domain.ReceiptID) (domain.Pass, error) {
	row, err := r.q.GetPassByReceipt(ctx, pgID(receipt))
	if err != nil {
		return domain.Pass{}, notFound("pass", err)
	}

	return toPass(row), nil
}

func (r passRepo) ListByReceipts(ctx context.Context, receipts []domain.ReceiptID) ([]domain.Pass, error) {
	rows, err := r.q.ListPassesByReceipts(ctx, pgIDs(receipts))

	return convertRows(rows, err, toPass)
}

func (r passRepo) Create(ctx context.Context, p domain.Pass) error {
	params, err := passParams(p)
	if err != nil {
		return err
	}

	return mapErr(r.q.CreatePass(ctx, params))
}

func (r passRepo) Update(ctx context.Context, p domain.Pass) error {
	params, err := passParams(p)
	if err != nil {
		return err
	}

	n, err := r.q.UpdatePass(ctx, sqlc.UpdatePassParams(params))

	return affected("pass", n, err)
}

func (r passRepo) ListByIDs(ctx context.Context, ids []domain.PassID) ([]domain.Pass, error) {
	rows, err := r.q.ListPassesByIDs(ctx, pgIDs(ids))

	return convertRows(rows, err, toPass)
}

func (r passRepo) ListByPersons(ctx context.Context, persons []domain.PersonID) ([]domain.Pass, error) {
	rows, err := r.q.ListPassesByPersons(ctx, pgIDs(persons))

	return convertRows(rows, err, toPass)
}

func (r passRepo) ListByPayers(ctx context.Context, persons []domain.PersonID) ([]domain.Pass, error) {
	rows, err := r.q.ListPassesByPayers(ctx, pgIDs(persons))

	return convertRows(rows, err, toPass)
}

// LockByPersons reads the passes with SELECT … FOR UPDATE, ordered by ID. In
// a read-only transaction PostgreSQL rejects the statement (SQLSTATE 25006,
// store.ErrReadOnly).
func (r passRepo) LockByPersons(ctx context.Context, persons ...domain.PersonID) ([]domain.Pass, error) {
	rows, err := r.q.LockPassesByPersons(ctx, pgIDs(persons))

	return convertRows(rows, err, toPass)
}

// ---------------------------------------------------------------- redemptions

type redemptionRepo struct{ q *sqlc.Queries }

func toRedemption(row sqlc.PassRedemption) domain.PassRedemption {
	return domain.PassRedemption{
		PassID:    fromPgID[domain.PassID](row.PassID),
		BookingID: fromPgID[domain.BookingID](row.BookingID),
		At:        fromPgInstant(row.At),
	}
}

func (r redemptionRepo) Create(ctx context.Context, red domain.PassRedemption) error {
	return mapErr(r.q.CreatePassRedemption(ctx, sqlc.CreatePassRedemptionParams{
		PassID: pgID(red.PassID), BookingID: pgID(red.BookingID), At: pgInstant(red.At),
	}))
}

func (r redemptionRepo) GetByBooking(ctx context.Context, booking domain.BookingID) (domain.PassRedemption, error) {
	row, err := r.q.GetPassRedemptionByBooking(ctx, pgID(booking))
	if err != nil {
		return domain.PassRedemption{}, notFound("redemption", err)
	}

	return toRedemption(row), nil
}

func (r redemptionRepo) Delete(ctx context.Context, booking domain.BookingID) error {
	n, err := r.q.DeletePassRedemption(ctx, pgID(booking))

	return affected("redemption", n, err)
}

func (r redemptionRepo) ListByPasses(
	ctx context.Context, passes []domain.PassID, latest int,
) ([]domain.PassRedemption, error) {
	if latest <= 0 {
		rows, err := r.q.ListPassRedemptionsByPasses(ctx, pgIDs(passes))

		return convertRows(rows, err, toRedemption)
	}

	n, err := int4(latest)
	if err != nil {
		return nil, err
	}

	rows, err := r.q.ListLatestPassRedemptionsByPasses(ctx, sqlc.ListLatestPassRedemptionsByPassesParams{
		Passes: pgIDs(passes), Latest: n,
	})

	return convertRows(rows, err, toRedemption)
}

func (r redemptionRepo) CountByPasses(ctx context.Context, passes []domain.PassID) (map[domain.PassID]int, error) {
	rows, err := r.q.CountPassRedemptionsByPasses(ctx, pgIDs(passes))
	if err != nil {
		return nil, mapErr(err)
	}

	out := make(map[domain.PassID]int, len(rows))
	for _, row := range rows {
		out[fromPgID[domain.PassID](row.PassID)] = int(row.Uses)
	}

	return out, nil
}

func (r redemptionRepo) ListByBookings(
	ctx context.Context, bookings []domain.BookingID,
) ([]domain.PassRedemption, error) {
	rows, err := r.q.ListPassRedemptionsByBookings(ctx, pgIDs(bookings))

	return convertRows(rows, err, toRedemption)
}
