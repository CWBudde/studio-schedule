-- name: CreateImportRun :exec
INSERT INTO import_runs (id, file_name, actor_id, started_at, finished_at, rows_total, created, updated, unchanged,
    failed, guardians_created, guardians_updated, links_added, errors)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14);

-- name: ListImportRuns :many
SELECT * FROM import_runs ORDER BY started_at DESC, id DESC LIMIT $1;

-- name: CreateDuplicateDismissal :exec
INSERT INTO person_duplicate_dismissals (person_a, person_b, dismissed_by, dismissed_at) VALUES ($1, $2, $3, $4)
ON CONFLICT (person_a, person_b) DO NOTHING;

-- name: ListDuplicateDismissals :many
SELECT * FROM person_duplicate_dismissals ORDER BY person_a, person_b;

-- name: ListDuplicateDismissalsByPerson :many
SELECT * FROM person_duplicate_dismissals WHERE person_a = $1 OR person_b = $1 ORDER BY person_a, person_b;

-- name: DeleteDuplicateDismissal :execrows
DELETE FROM person_duplicate_dismissals WHERE person_a = $1 AND person_b = $2;
