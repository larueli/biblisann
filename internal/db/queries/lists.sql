-- name: CreateList :one
INSERT INTO lists (owner_id, library_id, name, kind) VALUES ($1, $2, $3, $4) RETURNING *;

-- name: GetList :one
SELECT * FROM lists WHERE id = $1;

-- name: GetWishlist :one
SELECT * FROM lists WHERE owner_id = $1 AND kind = 'wishlist';

-- name: ListListsForUser :many
SELECT l.* FROM lists l
WHERE l.owner_id = $1
   OR l.library_id IN (SELECT library_id FROM user_library_access WHERE user_id = $1)
ORDER BY (l.kind = 'wishlist') DESC, l.name;

-- name: RenameList :one
UPDATE lists SET name = $2 WHERE id = $1 RETURNING *;

-- name: DeleteList :execrows
DELETE FROM lists WHERE id = $1 AND kind <> 'wishlist';

-- name: AddListItem :one
INSERT INTO list_items (list_id, edition_id, title, authors, isbn13, series_name, series_position, note)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING *;

-- name: GetListItem :one
SELECT * FROM list_items WHERE id = $1;

-- name: DeleteListItem :exec
DELETE FROM list_items WHERE id = $1;

-- name: ListListItems :many
-- in_library: I own this edition (or another edition of the same work) in a library I can read.
SELECT li.*,
  EXISTS (
    SELECT 1 FROM instances i
    JOIN editions e ON e.id = i.edition_id
    WHERE i.deleted_at IS NULL
      AND i.library_id = ANY(sqlc.arg(readable_libraries)::uuid[])
      AND ((li.isbn13 IS NOT NULL AND e.isbn13 = li.isbn13)
        OR (li.edition_id IS NOT NULL AND (e.id = li.edition_id
            OR e.work_id = (SELECT work_id FROM editions WHERE id = li.edition_id))))
  ) AS in_library
FROM list_items li WHERE li.list_id = $1 ORDER BY li.added_at, li.id;

-- name: SeriesInLibrary :many
SELECT e.series_name, coalesce(array_agg(DISTINCT e.series_position ORDER BY e.series_position) FILTER (WHERE e.series_position IS NOT NULL), ARRAY[]::float8[])::float8[] AS positions,
       min(e.title) AS sample_title, min(e.authors[1]) AS sample_author
FROM instances i JOIN editions e ON e.id = i.edition_id
WHERE i.library_id = $1 AND i.deleted_at IS NULL AND e.series_name <> ''
GROUP BY e.series_name ORDER BY e.series_name;

-- name: ListPersonalLists :many
SELECT * FROM lists WHERE owner_id = $1 ORDER BY (kind = 'wishlist') DESC, name, id;

-- name: ListLibraryLists :many
SELECT * FROM lists WHERE library_id = $1 ORDER BY name, id;

-- name: ListItemsOfList :many
SELECT li.*, e.isbn13 AS edition_isbn13 FROM list_items li LEFT JOIN editions e ON e.id = li.edition_id WHERE li.list_id = $1 ORDER BY li.added_at, li.id;
