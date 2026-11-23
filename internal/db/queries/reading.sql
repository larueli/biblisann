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

-- name: GetAnyISBNOfWork :one
SELECT isbn13 FROM editions WHERE work_id = $1 AND isbn13 IS NOT NULL ORDER BY created_at LIMIT 1;

-- name: ExternalGradeCandidates :many
-- Works with an ISBN that are due for a (re)check at a rating source, never-checked ones first.
SELECT w.id AS work_id,
       (SELECT e.isbn13 FROM editions e WHERE e.work_id = w.id AND e.isbn13 IS NOT NULL ORDER BY e.created_at LIMIT 1)::text AS isbn13
FROM works w
LEFT JOIN external_grade_checks c ON c.work_id = w.id AND c.source = $1
WHERE EXISTS (SELECT 1 FROM editions e WHERE e.work_id = w.id AND e.isbn13 IS NOT NULL)
  AND (c.work_id IS NULL
    OR (c.status = 'ok'    AND c.checked_at < now() - make_interval(days => $2::int))
    OR (c.status = 'none'  AND c.checked_at < now() - make_interval(days => $3::int))
    OR (c.status = 'error' AND c.checked_at < now() - make_interval(mins => LEAST(c.attempts * 30, 1440))))
ORDER BY c.checked_at NULLS FIRST, w.created_at
LIMIT $4;

-- name: RecordExternalGradeCheck :exec
INSERT INTO external_grade_checks (work_id, source, status, attempts) VALUES ($1, $2, $3, CASE WHEN $3 = 'error' THEN 1 ELSE 0 END)
ON CONFLICT (work_id, source) DO UPDATE
SET checked_at = now(), status = EXCLUDED.status,
    attempts = CASE WHEN EXCLUDED.status = 'error' THEN external_grade_checks.attempts + 1 ELSE 0 END;

-- name: DeleteExternalGrade :exec
DELETE FROM external_grades WHERE work_id = $1 AND source = $2;

-- name: ExportReading :many
SELECT r.status, r.started_on, r.finished_on, e.isbn13, e.title, e.authors
FROM reading_states r JOIN LATERAL (SELECT * FROM editions WHERE work_id = r.work_id ORDER BY created_at LIMIT 1) e ON true
WHERE r.user_id = $1 ORDER BY r.updated_at, r.work_id;

-- name: ExportGrades :many
SELECT g.grade, g.comment, e.isbn13, e.title, e.authors
FROM grades g JOIN LATERAL (SELECT * FROM editions WHERE work_id = g.work_id ORDER BY created_at LIMIT 1) e ON true
WHERE g.user_id = $1 ORDER BY g.updated_at, g.work_id;
