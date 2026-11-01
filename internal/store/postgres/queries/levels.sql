-- name: GetLevel :one
SELECT * FROM levels WHERE id = $1;

-- name: CreateLevel :exec
INSERT INTO levels (id, name, code, position) VALUES ($1, $2, $3, $4);

-- name: UpdateLevel :execrows
UPDATE levels SET name = $2, code = $3 WHERE id = $1;

-- name: DeleteLevel :execrows
DELETE FROM levels WHERE id = $1;

-- name: ListLevels :many
SELECT * FROM levels ORDER BY position, name, id;

-- name: LockLevels :many
SELECT * FROM levels ORDER BY id FOR UPDATE;

-- name: SetLevelPosition :execrows
UPDATE levels SET position = $2 WHERE id = $1;

-- name: LockLevelScale :one
-- A transaction-scoped advisory lock serializes adding a level and
-- reordering the scale (row locks cannot stop an insert). The key is
-- arbitrary but fixed: hashtext('level-scale'). A read-only transaction gets
-- false and no lock.
SELECT CASE WHEN current_setting('transaction_read_only')::boolean THEN false
    ELSE (SELECT true FROM (SELECT pg_advisory_xact_lock(hashtext('level-scale'))) AS l)
END::boolean AS locked;
