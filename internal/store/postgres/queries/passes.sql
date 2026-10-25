-- name: GetPassType :one
SELECT * FROM pass_types WHERE id = $1;

-- name: CreatePassType :exec
INSERT INTO pass_types (id, name, kind, visits, validity_days, price, vat_rate_bp, active, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9);

-- name: UpdatePassType :execrows
UPDATE pass_types
SET name = $2, kind = $3, visits = $4, validity_days = $5, price = $6, vat_rate_bp = $7, active = $8, created_at = $9
WHERE id = $1;

-- name: DeletePassType :execrows
DELETE FROM pass_types WHERE id = $1;

-- name: ListPassTypes :many
SELECT * FROM pass_types ORDER BY name, id;

-- name: GetPass :one
SELECT * FROM passes WHERE id = $1;

-- name: GetPassByReceipt :one
SELECT * FROM passes WHERE receipt_id = $1;

-- name: ListPassesByReceipts :many
SELECT * FROM passes WHERE receipt_id = ANY(sqlc.arg(receipts)::uuid[]) ORDER BY id;

-- name: CreatePass :exec
INSERT INTO passes (
    id, pass_type_id, person_id, kind, visits_total, visits_remaining, valid_from, valid_until, status, origin,
    sold_at, sold_by, payer_person_id, receipt_id, price_paid, note, voided_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17
);

-- name: UpdatePass :execrows
UPDATE passes
SET pass_type_id = $2, person_id = $3, kind = $4, visits_total = $5, visits_remaining = $6, valid_from = $7,
    valid_until = $8, status = $9, origin = $10, sold_at = $11, sold_by = $12, payer_person_id = $13,
    receipt_id = $14, price_paid = $15, note = $16, voided_at = $17
WHERE id = $1;

-- name: ListPassesByIDs :many
SELECT * FROM passes WHERE id = ANY(sqlc.arg(ids)::uuid[]) ORDER BY id;

-- name: ListPassesByPersons :many
SELECT * FROM passes WHERE person_id = ANY(sqlc.arg(persons)::uuid[]) ORDER BY sold_at, id;

-- name: ListPassesByPayers :many
SELECT * FROM passes WHERE payer_person_id = ANY(sqlc.arg(persons)::uuid[]) ORDER BY id;

-- name: LockPassesByPersons :many
-- In ID order, so that concurrent transactions lock the same passes in the
-- same order.
SELECT * FROM passes WHERE person_id = ANY(sqlc.arg(persons)::uuid[]) ORDER BY id FOR UPDATE;

-- name: CreatePassRedemption :exec
INSERT INTO pass_redemptions (pass_id, booking_id, at) VALUES ($1, $2, $3);

-- name: GetPassRedemptionByBooking :one
SELECT * FROM pass_redemptions WHERE booking_id = $1;

-- name: DeletePassRedemption :execrows
DELETE FROM pass_redemptions WHERE booking_id = $1;

-- name: ListPassRedemptionsByPasses :many
SELECT * FROM pass_redemptions WHERE pass_id = ANY(sqlc.arg(passes)::uuid[]) ORDER BY pass_id, at, booking_id;

-- name: ListLatestPassRedemptionsByPasses :many
SELECT pass_id, booking_id, at FROM (
    SELECT r.pass_id, r.booking_id, r.at,
        row_number() OVER (PARTITION BY r.pass_id ORDER BY r.at DESC, r.booking_id DESC) AS n
    FROM pass_redemptions r
    WHERE r.pass_id = ANY(sqlc.arg(passes)::uuid[])
) latest
WHERE n <= sqlc.arg(latest)::integer
ORDER BY pass_id, at, booking_id;

-- name: CountPassRedemptionsByPasses :many
SELECT pass_id, count(*) AS uses
FROM pass_redemptions
WHERE pass_id = ANY(sqlc.arg(passes)::uuid[])
GROUP BY pass_id;

-- name: ListPassRedemptionsByBookings :many
SELECT * FROM pass_redemptions WHERE booking_id = ANY(sqlc.arg(bookings)::uuid[]) ORDER BY booking_id;
