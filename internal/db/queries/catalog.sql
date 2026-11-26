-- name: GetEditionByISBN13 :one
SELECT * FROM editions WHERE isbn13 = $1;

-- name: GetEdition :one
SELECT * FROM editions WHERE id = $1;

-- name: CreateWork :one
INSERT INTO works (title, authors) VALUES ($1, $2) RETURNING *;

-- name: FindWorkByTitleAuthors :one
SELECT * FROM works
WHERE lower(immutable_unaccent(title)) = lower(immutable_unaccent($1::text)) AND authors = $2::text[]
ORDER BY created_at LIMIT 1;

-- name: CreateEdition :one
INSERT INTO editions (work_id, isbn13, isbn10, ean, title, subtitle, authors, publisher, published_year,
                      language, pages, description, series_name, series_position, cover_id, provenance,
                      search_text, created_by)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16,
        lower(immutable_unaccent(concat_ws(' ', $5::text, $6::text, array_to_string($7::text[], ' '), $8::text, $2::text, $10::text))),
        $17)
RETURNING *;

-- name: UpdateEditionMetadata :one
UPDATE editions SET
    title = $2, subtitle = $3, authors = $4, publisher = $5, published_year = $6, language = $7,
    pages = $8, description = $9, series_name = $10, series_position = $11, cover_id = $12,
    provenance = $13, locked_fields = $14,
    search_text = lower(immutable_unaccent(concat_ws(' ', $2::text, $3::text, array_to_string($4::text[], ' '), $5::text, isbn13, $7::text))),
    updated_at = now()
WHERE id = $1 RETURNING *;

-- name: InsertCover :one
INSERT INTO covers (sha256, width, height) VALUES ($1, $2, $3)
ON CONFLICT (sha256) DO UPDATE SET sha256 = EXCLUDED.sha256
RETURNING *;

-- name: GetCover :one
SELECT * FROM covers WHERE id = $1;

-- name: GetProviderCache :one
SELECT * FROM provider_cache WHERE provider = $1 AND key = $2;

-- name: PutProviderCache :exec
INSERT INTO provider_cache (provider, key, found, payload) VALUES ($1, $2, $3, $4)
ON CONFLICT (provider, key) DO UPDATE SET found = EXCLUDED.found, payload = EXCLUDED.payload, fetched_at = now();

-- name: SearchEditions :many
SELECT e.* FROM editions e
WHERE to_tsvector('simple', e.search_text) @@ websearch_to_tsquery('simple', lower(immutable_unaccent($1::text)))
   OR e.search_text ILIKE '%' || lower(immutable_unaccent($1::text)) || '%'
   OR word_similarity(lower(immutable_unaccent($1::text)), e.search_text) >= 0.4
ORDER BY similarity(e.search_text, lower(immutable_unaccent($1::text))) DESC, e.title
LIMIT $2;

-- name: FindIsbnlessEdition :one
SELECT * FROM editions
WHERE work_id = $1 AND isbn13 IS NULL AND publisher = $2 AND language = $3
  AND published_year IS NOT DISTINCT FROM sqlc.narg('year')::int
ORDER BY created_at LIMIT 1;

-- name: SetEditionCoverIfMissing :exec
UPDATE editions SET cover_id = $2 WHERE id = $1 AND cover_id IS NULL;

-- name: GetCoverSHA :one
SELECT sha256 FROM covers WHERE id = $1;

-- name: SetEditionSyncExcluded :one
UPDATE editions SET sync_excluded = $2, updated_at = now() WHERE id = $1 RETURNING *;

-- name: CountEditionInstances :one
SELECT count(*) FROM instances WHERE edition_id = $1;

-- name: DeleteEdition :execrows
DELETE FROM editions WHERE id = $1;

-- name: DeleteProviderCacheForKey :exec
DELETE FROM provider_cache WHERE key = $1;

-- name: SetEditionSyncResult :one
UPDATE editions SET sync_status = $2, synced_at = now() WHERE id = $1 RETURNING *;

-- name: ListEditionsToRefresh :many
-- Editions a provider refresh should look at: not excluded, with an ISBN to look up. By default only
-- "unknown" books (no provider knew them, or never looked up and no provider data), waiting a week
-- between attempts. include_known adds every edition; ignore_wait drops the one-week pause.
SELECT e.id FROM editions e
WHERE NOT e.sync_excluded AND e.isbn13 IS NOT NULL
  AND (sqlc.narg('library_id')::uuid IS NULL OR EXISTS (SELECT 1 FROM instances i WHERE i.edition_id = e.id AND i.library_id = sqlc.narg('library_id')::uuid))
  AND (sqlc.arg('include_known')::boolean OR e.sync_status IN ('not_found', 'skipped') OR (e.sync_status = '' AND e.provenance = '{}'::jsonb))
  AND (sqlc.arg('ignore_wait')::boolean OR e.synced_at IS NULL OR e.synced_at < now() - interval '7 days')
ORDER BY e.synced_at NULLS FIRST, e.id LIMIT sqlc.arg('max_rows')::int;

-- name: ListEditionLibraries :many
SELECT DISTINCT library_id FROM instances WHERE edition_id = $1 AND deleted_at IS NULL;

-- name: CoversForFileCheck :many
-- Covers old enough that their files must exist, with the number of editions that show them.
SELECT c.id, c.sha256, (SELECT count(*) FROM editions e WHERE e.cover_id = c.id)::bigint AS editions
FROM covers c
WHERE c.created_at < now() - make_interval(hours => $1::int)
ORDER BY c.created_at;
