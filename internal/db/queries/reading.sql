-- name: UpsertReadingState :one
INSERT INTO reading_states (user_id, work_id, status, started_on, finished_on)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (user_id, work_id) DO UPDATE
SET status = EXCLUDED.status, started_on = EXCLUDED.started_on, finished_on = EXCLUDED.finished_on, updated_at = now()
RETURNING *;

-- name: GetReadingState :one
SELECT * FROM reading_states WHERE user_id = $1 AND work_id = $2;

-- name: DeleteReadingState :exec
DELETE FROM reading_states WHERE user_id = $1 AND work_id = $2;

-- name: ListReadingStates :many
SELECT r.work_id, r.status, r.started_on, r.finished_on, w.title, w.authors
FROM reading_states r JOIN works w ON w.id = r.work_id
WHERE r.user_id = $1 AND ($2::text = '' OR r.status = $2::text)
ORDER BY r.updated_at DESC LIMIT $3;

-- name: UpsertGrade :one
INSERT INTO grades (user_id, work_id, grade, comment) VALUES ($1, $2, $3, $4)
ON CONFLICT (user_id, work_id) DO UPDATE SET grade = EXCLUDED.grade, comment = EXCLUDED.comment, updated_at = now()
RETURNING *;

-- name: GetGrade :one
SELECT * FROM grades WHERE user_id = $1 AND work_id = $2;

-- name: DeleteGrade :exec
DELETE FROM grades WHERE user_id = $1 AND work_id = $2;

-- name: ListExternalGrades :many
SELECT * FROM external_grades WHERE work_id = $1 ORDER BY source;

-- name: UpsertExternalGrade :exec
INSERT INTO external_grades (work_id, source, grade, scale, votes, url) VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (work_id, source) DO UPDATE SET grade = EXCLUDED.grade, scale = EXCLUDED.scale, votes = EXCLUDED.votes,
  url = EXCLUDED.url, fetched_at = now();

-- name: ReadingCounts :many
SELECT status, count(*)::int AS n FROM reading_states WHERE user_id = $1 GROUP BY status;

-- name: ReadPerYear :many
SELECT extract(year FROM finished_on)::int AS year, count(*)::int AS n
FROM reading_states WHERE user_id = $1 AND status = 'read' AND finished_on IS NOT NULL
GROUP BY 1 ORDER BY 1 DESC;

-- name: AverageGrade :one
SELECT coalesce(avg(grade), 0)::float8 AS average, count(grade)::int AS graded FROM grades WHERE user_id = $1 AND grade IS NOT NULL;
