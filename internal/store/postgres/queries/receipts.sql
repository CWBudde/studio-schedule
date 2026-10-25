-- name: NextReceiptSequence :one
-- Locks the year's counter row until the transaction ends.
INSERT INTO receipt_counters (year, last) VALUES ($1, 1)
ON CONFLICT (year) DO UPDATE SET last = receipt_counters.last + 1
RETURNING last;

-- name: CreateReceipt :exec
INSERT INTO receipts (
    id, number, year, sequence, kind, cancels_receipt_id, issued_at, issue_date, locale, issuer_name,
    issuer_address, issuer_tax_id, issuer_email, issuer_phone, tax_mode, tax_note, footer, payer_person_id,
    recipient_name, recipient_address, recipient_email, payment_method, currency, net, vat, gross, reason,
    created_by
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23,
    $24, $25, $26, $27, $28
);

-- name: CreateReceiptLine :exec
INSERT INTO receipt_lines (receipt_id, position, description, quantity, unit_price, vat_rate_bp, net, vat, gross)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9);

-- name: GetReceipt :one
SELECT * FROM receipts WHERE id = $1;

-- name: GetReceiptByNumber :one
SELECT * FROM receipts WHERE number = $1;

-- name: ListReceiptsByIDs :many
SELECT * FROM receipts WHERE id = ANY(sqlc.arg(ids)::uuid[]) ORDER BY id;

-- name: GetReceiptCancellation :one
SELECT * FROM receipts WHERE cancels_receipt_id = $1;

-- name: ListReceipts :many
-- Year 0 lists every year. The query matches the number or the recipient's
-- name, ignoring ASCII case; empty matches all. The parameter needs
-- collation "C" as well, so that lower() folds ASCII only, like the columns'.
SELECT * FROM receipts
WHERE (sqlc.arg(year)::integer = 0 OR year = sqlc.arg(year)::integer)
    AND (
        sqlc.arg(query)::text = ''
        OR strpos(lower(number), lower(sqlc.arg(query)::text COLLATE "C")) > 0
        OR strpos(lower(recipient_name), lower(sqlc.arg(query)::text COLLATE "C")) > 0
    )
ORDER BY year DESC, sequence DESC, id
LIMIT sqlc.arg(max_rows) OFFSET sqlc.arg(skip_rows);

-- name: CountReceipts :one
SELECT count(*) FROM receipts
WHERE (sqlc.arg(year)::integer = 0 OR year = sqlc.arg(year)::integer)
    AND (
        sqlc.arg(query)::text = ''
        OR strpos(lower(number), lower(sqlc.arg(query)::text COLLATE "C")) > 0
        OR strpos(lower(recipient_name), lower(sqlc.arg(query)::text COLLATE "C")) > 0
    );

-- name: ListReceiptsByPayers :many
SELECT * FROM receipts WHERE payer_person_id = ANY(sqlc.arg(persons)::uuid[])
ORDER BY year DESC, sequence DESC, id;

-- name: ListReceiptCancellations :many
SELECT * FROM receipts WHERE cancels_receipt_id = ANY(sqlc.arg(receipts)::uuid[]) ORDER BY cancels_receipt_id;

-- name: ListReceiptLines :many
SELECT * FROM receipt_lines WHERE receipt_id = ANY(sqlc.arg(receipts)::uuid[]) ORDER BY receipt_id, position;

-- name: ListReceiptYears :many
SELECT year FROM receipt_counters ORDER BY year DESC;

-- name: CreateReceiptPDF :exec
INSERT INTO receipt_pdfs (receipt_id, pdf) VALUES ($1, $2);

-- name: GetReceiptPDF :one
SELECT pdf FROM receipt_pdfs WHERE receipt_id = $1;
