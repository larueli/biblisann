-- name: CreateInstance :one
INSERT INTO instances (library_id, edition_id, place_id, kind, condition, notes, acquired_on, created_by)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING *;

-- name: GetInstance :one
SELECT * FROM instances WHERE id = $1 AND deleted_at IS NULL;

-- name: UpdateInstance :one
UPDATE instances SET place_id = $2, kind = $3, condition = $4, notes = $5, acquired_on = $6, updated_at = now()
WHERE id = $1 AND deleted_at IS NULL RETURNING *;

-- name: SoftDeleteInstance :execrows
UPDATE instances SET deleted_at = now(), updated_at = now() WHERE id = $1 AND deleted_at IS NULL;

-- name: RestoreInstance :execrows
UPDATE instances SET deleted_at = NULL, updated_at = now() WHERE id = $1 AND deleted_at IS NOT NULL;

-- name: ListInstances :many
SELECT i.id, i.library_id, i.edition_id, i.place_id, i.kind, i.condition, i.notes, i.acquired_on,
       i.created_at, i.updated_at,
       e.title, e.authors, e.isbn13, e.work_id, e.cover_id
FROM instances i JOIN editions e ON e.id = i.edition_id
WHERE i.library_id = $1 AND i.deleted_at IS NULL
  AND (i.created_at, i.id) < ($2::timestamptz, $3::uuid)
ORDER BY i.created_at DESC, i.id DESC
LIMIT $4;

-- name: SearchInstances :many
SELECT i.id, i.library_id, i.edition_id, i.place_id, i.kind, i.condition, i.notes, i.acquired_on,
       i.created_at, i.updated_at,
       e.title, e.authors, e.isbn13, e.work_id, e.cover_id
FROM instances i JOIN editions e ON e.id = i.edition_id
WHERE i.library_id = $1 AND i.deleted_at IS NULL
  AND (to_tsvector('simple', e.search_text) @@ websearch_to_tsquery('simple', lower(immutable_unaccent($2::text)))
       OR e.search_text ILIKE '%' || lower(immutable_unaccent($2::text)) || '%'
       OR word_similarity(lower(immutable_unaccent($2::text)), e.search_text) >= 0.4)
ORDER BY similarity(e.search_text, lower(immutable_unaccent($2::text))) DESC, e.title
LIMIT $3;

-- name: OwnedByWorkOrISBN :many
-- "Do I own this?" across every library the user can read: same ISBN, or another edition of the work.
SELECT i.id AS instance_id, i.library_id, l.name AS library_name, e.id AS edition_id, e.isbn13, e.title,
       (e.isbn13 = $2::text) AS same_edition
FROM instances i
JOIN editions e ON e.id = i.edition_id
JOIN libraries l ON l.id = i.library_id
JOIN user_library_access m ON m.library_id = i.library_id AND m.user_id = $1
WHERE i.deleted_at IS NULL AND (e.isbn13 = $2::text OR e.work_id = $3::uuid)
ORDER BY same_edition DESC, l.name;

-- name: AddChange :exec
INSERT INTO changes (library_id, entity, entity_id, op) VALUES ($1, $2, $3, $4);

-- name: ListChanges :many
SELECT seq, library_id, entity, entity_id, op FROM changes
WHERE library_id = $1 AND seq > $2 ORDER BY seq LIMIT $3;

-- name: LatestChangeSeq :one
SELECT coalesce(max(seq), 0)::bigint FROM changes WHERE library_id = $1;

-- name: SyncInstanceRows :many
SELECT i.id, i.edition_id, i.place_id, (i.deleted_at IS NOT NULL)::boolean AS deleted,
       e.title, e.authors, e.isbn13, e.work_id
FROM instances i JOIN editions e ON e.id = i.edition_id
WHERE i.id = ANY($1::uuid[]);

-- name: GetIdempotency :one
SELECT request_hash, response_status, response_body FROM idempotency_keys WHERE user_id = $1 AND key = $2;

-- name: PutIdempotency :exec
INSERT INTO idempotency_keys (user_id, key, request_hash, response_status, response_body)
VALUES ($1, $2, $3, $4, $5) ON CONFLICT DO NOTHING;

-- name: DeleteOldIdempotency :execrows
DELETE FROM idempotency_keys WHERE created_at < now() - interval '7 days';

-- name: CountInstancesOfEdition :one
SELECT count(*) FROM instances WHERE library_id = $1 AND edition_id = $2 AND deleted_at IS NULL;

-- name: ExportInstances :many
SELECT i.id, i.edition_id, i.place_id, i.kind, i.condition, i.notes, i.acquired_on
FROM instances i WHERE i.library_id = $1 AND i.deleted_at IS NULL ORDER BY i.created_at, i.id;
