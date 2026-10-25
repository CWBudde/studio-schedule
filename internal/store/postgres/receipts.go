package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/store"
	"github.com/cwbudde/studio-schedule/internal/store/postgres/internal/sqlc"
)

// ---------------------------------------------------------------- receipts

type receiptRepo struct{ q *sqlc.Queries }

func toReceipt(row sqlc.Receipt) domain.Receipt {
	return domain.Receipt{
		ID:               fromPgID[domain.ReceiptID](row.ID),
		Number:           row.Number,
		Year:             int(row.Year),
		Sequence:         int(row.Sequence),
		Kind:             domain.ReceiptKind(row.Kind),
		CancelsReceiptID: fromPgID[domain.ReceiptID](row.CancelsReceiptID),
		IssuedAt:         fromPgInstant(row.IssuedAt),
		IssueDate:        fromPgDate(row.IssueDate),
		Locale:           row.Locale,
		Issuer: domain.ReceiptIssuer{
			Name: row.IssuerName, Address: row.IssuerAddress, TaxID: row.IssuerTaxID,
			Email: row.IssuerEmail.String, Phone: row.IssuerPhone.String,
		},
		TaxMode:          domain.TaxMode(row.TaxMode),
		TaxNote:          row.TaxNote.String,
		Footer:           row.Footer.String,
		PayerID:          fromPgID[domain.PersonID](row.PayerPersonID),
		RecipientName:    row.RecipientName,
		RecipientAddress: row.RecipientAddress.String,
		RecipientEmail:   row.RecipientEmail.String,
		PaymentMethod:    domain.PaymentMethod(row.PaymentMethod),
		Currency:         row.Currency,
		Net:              domain.Money(row.Net),
		VAT:              domain.Money(row.Vat),
		Gross:            domain.Money(row.Gross),
		Reason:           row.Reason.String,
		CreatedBy:        fromPgID[domain.AccountID](row.CreatedBy),
	}
}

func toReceiptLine(row sqlc.ReceiptLine) domain.ReceiptLine {
	return domain.ReceiptLine{
		Position:    int(row.Position),
		Description: row.Description,
		Quantity:    int(row.Quantity),
		UnitPrice:   domain.Money(row.UnitPrice),
		VATRate:     int(row.VatRateBp),
		Net:         domain.Money(row.Net),
		VAT:         domain.Money(row.Vat),
		Gross:       domain.Money(row.Gross),
	}
}

func (r receiptRepo) Create(ctx context.Context, rc domain.Receipt) error {
	year, err := int4(rc.Year)
	if err != nil {
		return err
	}

	seq, err := int4(rc.Sequence)
	if err != nil {
		return err
	}

	err = r.q.CreateReceipt(ctx, sqlc.CreateReceiptParams{
		ID:               pgID(rc.ID),
		Number:           rc.Number,
		Year:             year,
		Sequence:         seq,
		Kind:             string(rc.Kind),
		CancelsReceiptID: pgID(rc.CancelsReceiptID),
		IssuedAt:         pgInstant(rc.IssuedAt),
		IssueDate:        pgDate(rc.IssueDate),
		Locale:           rc.Locale,
		IssuerName:       rc.Issuer.Name,
		IssuerAddress:    rc.Issuer.Address,
		IssuerTaxID:      rc.Issuer.TaxID,
		IssuerEmail:      pgText(rc.Issuer.Email),
		IssuerPhone:      pgText(rc.Issuer.Phone),
		TaxMode:          string(rc.TaxMode),
		TaxNote:          pgText(rc.TaxNote),
		Footer:           pgText(rc.Footer),
		PayerPersonID:    pgID(rc.PayerID),
		RecipientName:    rc.RecipientName,
		RecipientAddress: pgText(rc.RecipientAddress),
		RecipientEmail:   pgText(rc.RecipientEmail),
		PaymentMethod:    string(rc.PaymentMethod),
		Currency:         rc.Currency,
		Net:              int64(rc.Net),
		Vat:              int64(rc.VAT),
		Gross:            int64(rc.Gross),
		Reason:           pgText(rc.Reason),
		CreatedBy:        pgID(rc.CreatedBy),
	})
	if err != nil {
		return mapErr(err)
	}

	for _, l := range rc.Lines {
		params, err := lineParams(rc.ID, l)
		if err != nil {
			return err
		}

		if err := r.q.CreateReceiptLine(ctx, params); err != nil {
			return mapErr(err)
		}
	}

	return nil
}

func lineParams(id domain.ReceiptID, l domain.ReceiptLine) (sqlc.CreateReceiptLineParams, error) {
	pos, err := int4(l.Position)
	if err != nil {
		return sqlc.CreateReceiptLineParams{}, err
	}

	qty, err := int4(l.Quantity)
	if err != nil {
		return sqlc.CreateReceiptLineParams{}, err
	}

	rate, err := int4(l.VATRate)
	if err != nil {
		return sqlc.CreateReceiptLineParams{}, err
	}

	return sqlc.CreateReceiptLineParams{
		ReceiptID: pgID(id), Position: pos, Description: l.Description, Quantity: qty,
		UnitPrice: int64(l.UnitPrice), VatRateBp: rate, Net: int64(l.Net), Vat: int64(l.VAT), Gross: int64(l.Gross),
	}, nil
}

// withLines converts receipt rows and adds their lines, with one query.
func (r receiptRepo) withLines(ctx context.Context, rows []sqlc.Receipt, err error) ([]domain.Receipt, error) {
	if err != nil {
		return nil, mapErr(err)
	}

	out := make([]domain.Receipt, len(rows))
	if len(rows) == 0 {
		return out, nil
	}

	ids := make([]domain.ReceiptID, len(rows))
	at := make(map[domain.ReceiptID]int, len(rows))

	for i, row := range rows {
		out[i] = toReceipt(row)
		ids[i] = out[i].ID
		at[out[i].ID] = i
	}

	lines, err := r.q.ListReceiptLines(ctx, pgIDs(ids))
	if err != nil {
		return nil, mapErr(err)
	}

	for _, l := range lines {
		i := at[fromPgID[domain.ReceiptID](l.ReceiptID)]
		out[i].Lines = append(out[i].Lines, toReceiptLine(l))
	}

	return out, nil
}

// one returns the receipt of a single-row query with its lines.
func (r receiptRepo) one(ctx context.Context, row sqlc.Receipt, err error) (domain.Receipt, error) {
	if err != nil {
		return domain.Receipt{}, notFound("receipt", err)
	}

	out, err := r.withLines(ctx, []sqlc.Receipt{row}, nil)
	if err != nil {
		return domain.Receipt{}, err
	}

	return out[0], nil
}

func (r receiptRepo) Get(ctx context.Context, id domain.ReceiptID) (domain.Receipt, error) {
	row, err := r.q.GetReceipt(ctx, pgID(id))

	return r.one(ctx, row, err)
}

func (r receiptRepo) GetByNumber(ctx context.Context, number string) (domain.Receipt, error) {
	row, err := r.q.GetReceiptByNumber(ctx, number)

	return r.one(ctx, row, err)
}

func (r receiptRepo) ListByIDs(ctx context.Context, ids []domain.ReceiptID) ([]domain.Receipt, error) {
	rows, err := r.q.ListReceiptsByIDs(ctx, pgIDs(ids))

	return r.withLines(ctx, rows, err)
}

func (r receiptRepo) GetCancellation(ctx context.Context, id domain.ReceiptID) (domain.Receipt, error) {
	row, err := r.q.GetReceiptCancellation(ctx, pgID(id))

	return r.one(ctx, row, err)
}

func (r receiptRepo) List(ctx context.Context, f store.ReceiptFilter) ([]domain.Receipt, int, error) {
	year, err := int4(max(f.Year, 0))
	if err != nil {
		return nil, 0, err
	}

	total, err := r.q.CountReceipts(ctx, sqlc.CountReceiptsParams{Year: year, Query: f.Query})
	if err != nil {
		return nil, 0, mapErr(err)
	}

	rows, err := r.q.ListReceipts(ctx, sqlc.ListReceiptsParams{
		Year: year, Query: f.Query, SkipRows: rowLimit(f.Offset), MaxRows: rowLimit(f.Limit),
	})

	out, err := r.withLines(ctx, rows, err)

	return out, int(total), err
}

func (r receiptRepo) ListByPayers(ctx context.Context, persons []domain.PersonID) ([]domain.Receipt, error) {
	rows, err := r.q.ListReceiptsByPayers(ctx, pgIDs(persons))

	return r.withLines(ctx, rows, err)
}

func (r receiptRepo) ListCancellations(ctx context.Context, ids []domain.ReceiptID) ([]domain.Receipt, error) {
	rows, err := r.q.ListReceiptCancellations(ctx, pgIDs(ids))

	return r.withLines(ctx, rows, err)
}

func (r receiptRepo) PutPDF(ctx context.Context, id domain.ReceiptID, pdf []byte) error {
	if len(pdf) == 0 {
		return fmt.Errorf("%w: empty PDF", domain.ErrValidation)
	}

	return mapErr(r.q.CreateReceiptPDF(ctx, sqlc.CreateReceiptPDFParams{ReceiptID: pgID(id), Pdf: pdf}))
}

func (r receiptRepo) PDF(ctx context.Context, id domain.ReceiptID) ([]byte, error) {
	pdf, err := r.q.GetReceiptPDF(ctx, pgID(id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("receipt PDF: %w", domain.ErrNotFound)
	}

	if err != nil {
		return nil, mapErr(err)
	}

	return pdf, nil
}

// ---------------------------------------------------------------- counters

type receiptCounterRepo struct{ q *sqlc.Queries }

func (r receiptCounterRepo) Next(ctx context.Context, year int) (int, error) {
	y, err := int4(year)
	if err != nil {
		return 0, err
	}

	n, err := r.q.NextReceiptSequence(ctx, y)
	if err != nil {
		return 0, mapErr(err)
	}

	return int(n), nil
}

func (r receiptCounterRepo) Years(ctx context.Context) ([]int, error) {
	rows, err := r.q.ListReceiptYears(ctx)

	return convertRows(rows, err, func(y int32) int { return int(y) })
}
