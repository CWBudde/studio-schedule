-- name: GetPerson :one
SELECT * FROM persons WHERE id = $1;

-- name: CreatePerson :exec
INSERT INTO persons (id, given_name, family_name, email, phone, birth_date, notes)
VALUES ($1, $2, $3, $4, $5, $6, $7);

-- name: UpdatePerson :execrows
UPDATE persons
SET given_name = $2, family_name = $3, email = $4, phone = $5, birth_date = $6, notes = $7
WHERE id = $1;

-- name: DeletePerson :execrows
DELETE FROM persons WHERE id = $1;

-- name: ListPersons :many
SELECT * FROM persons ORDER BY family_name, given_name, id;

-- name: AddGuardian :exec
INSERT INTO person_guardians (guardian_id, dependent_id) VALUES ($1, $2);

-- name: RemoveGuardian :execrows
DELETE FROM person_guardians WHERE guardian_id = $1 AND dependent_id = $2;

-- name: ListDependents :many
SELECT p.*
FROM person_guardians g
JOIN persons p ON p.id = g.dependent_id
WHERE g.guardian_id = $1
ORDER BY p.family_name, p.given_name, p.id;

-- name: ListGuardians :many
SELECT p.*
FROM person_guardians g
JOIN persons p ON p.id = g.guardian_id
WHERE g.dependent_id = $1
ORDER BY p.family_name, p.given_name, p.id;

-- name: GetAccount :one
SELECT * FROM accounts WHERE id = $1;

-- name: GetAccountByEmail :one
-- The parameter needs collation "C" as well, so that lower() folds ASCII
-- letters only and the expression matches accounts_email_key.
SELECT * FROM accounts WHERE lower(email) = lower(sqlc.arg(email)::text COLLATE "C");

-- name: CreateAccount :exec
INSERT INTO accounts (id, person_id, email, role, visible_days, confirmed_at, disabled_at, locale)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8);

-- name: UpdateAccount :execrows
UPDATE accounts
SET person_id = $2, email = $3, role = $4, visible_days = $5, confirmed_at = $6, disabled_at = $7,
    locale = $8
WHERE id = $1;

-- name: ListAccounts :many
SELECT * FROM accounts ORDER BY lower(email), id;

-- name: MaxAccountVisibleDays :one
SELECT coalesce(max(visible_days), 0)::integer FROM accounts;
