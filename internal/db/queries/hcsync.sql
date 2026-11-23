-- name: GetHardcoverLink :one
SELECT * FROM hardcover_links WHERE user_id = $1;

-- name: UpsertHardcoverLink :exec
INSERT INTO hardcover_links (user_id, token_enc, mode) VALUES ($1, $2, $3)
ON CONFLICT (user_id) DO UPDATE SET token_enc = EXCLUDED.token_enc, mode = EXCLUDED.mode, last_error = '', next_sync_at = now();

-- name: SetHardcoverMode :exec
UPDATE hardcover_links SET mode = $2, next_sync_at = now() WHERE user_id = $1;

-- name: DeleteHardcoverLink :exec
DELETE FROM hardcover_links WHERE user_id = $1;

-- name: DeleteHardcoverState :exec
DELETE FROM hardcover_book_state WHERE user_id = $1;

-- name: DueHardcoverLinks :many
SELECT user_id FROM hardcover_links WHERE next_sync_at <= now() ORDER BY next_sync_at LIMIT $1;

-- name: FinishHardcoverSync :exec
UPDATE hardcover_links SET pull_offset = $2, next_sync_at = $3, last_sync_at = CASE WHEN $4::text = '' THEN now() ELSE last_sync_at END, last_error = $4
WHERE user_id = $1;

-- name: HardcoverBookState :many
SELECT * FROM hardcover_book_state WHERE user_id = $1;

-- name: UpsertHardcoverBookState :exec
INSERT INTO hardcover_book_state (user_id, work_id, hc_book_id, hc_user_book_id, status, grade)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (user_id, work_id) DO UPDATE
SET hc_book_id = EXCLUDED.hc_book_id, hc_user_book_id = EXCLUDED.hc_user_book_id, status = EXCLUDED.status, grade = EXCLUDED.grade, synced_at = now();

-- name: LocalReadingForSync :many
-- Every reading state with its grade and the ISBN of one of the work's editions.
SELECT r.work_id, r.status, r.updated_at AS status_updated_at, g.grade, g.updated_at AS grade_updated_at,
       (SELECT e.isbn13 FROM editions e WHERE e.work_id = r.work_id AND e.isbn13 IS NOT NULL ORDER BY e.created_at LIMIT 1)::text AS isbn13
FROM reading_states r LEFT JOIN grades g ON g.user_id = r.user_id AND g.work_id = r.work_id
WHERE r.user_id = $1;

-- name: WorkIDByISBN :one
SELECT work_id FROM editions WHERE isbn13 = $1;

-- name: SealedHardcoverSamples :many
-- Sealed Hardcover tokens, to check at startup that the secret key still opens them.
SELECT token_enc FROM hardcover_links ORDER BY created_at DESC LIMIT 20;
