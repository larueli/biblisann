-- name: CreateLibrary :one
INSERT INTO libraries (name) VALUES ($1) RETURNING *;

-- name: AddLibraryMember :exec
INSERT INTO library_members (library_id, user_id, role) VALUES ($1, $2, $3)
ON CONFLICT (library_id, user_id) DO UPDATE SET role = EXCLUDED.role;

-- name: GetLibraryRole :one
SELECT role FROM library_members WHERE library_id = $1 AND user_id = $2;

-- name: ListLibrariesForUser :many
-- role: the most privileged of the direct membership and the group shares.
SELECT l.id, l.name, l.settings, l.created_at,
       (array_agg(a.role ORDER BY CASE a.role WHEN 'admin' THEN 0 WHEN 'edit' THEN 1 WHEN 'read' THEN 2 ELSE 3 END))[1]::text AS role
FROM libraries l
JOIN (
    SELECT lm.library_id, lm.role FROM library_members lm WHERE lm.user_id = $1
    UNION ALL
    SELECT s.library_id, s.role FROM library_group_shares s JOIN group_members gm ON gm.group_id = s.group_id AND gm.user_id = $1
) a ON a.library_id = l.id
GROUP BY l.id ORDER BY l.name, l.id;

-- name: LibraryAccess :many
-- One row per grant (direct or through a group); permissions is set for custom profiles.
SELECT m.role, p.permissions FROM library_members m
LEFT JOIN permission_profiles p ON p.id = m.profile_id
WHERE m.library_id = $1 AND m.user_id = $2
UNION ALL
SELECT s.role, p.permissions FROM library_group_shares s
JOIN group_members gm ON gm.group_id = s.group_id AND gm.user_id = $2
LEFT JOIN permission_profiles p ON p.id = s.profile_id
WHERE s.library_id = $1;

-- name: GetLibrary :one
SELECT * FROM libraries WHERE id = $1;

-- name: UpdateLibrary :one
UPDATE libraries SET name = $2, settings = $3 WHERE id = $1 RETURNING *;

-- name: CreatePlace :one
INSERT INTO places (library_id, parent_id, name, kind, position)
VALUES ($1, $2, $3, $4, $5) RETURNING *;

-- name: ListPlaces :many
SELECT * FROM places WHERE library_id = $1 ORDER BY parent_id NULLS FIRST, position, name;

-- name: GetPlace :one
SELECT * FROM places WHERE id = $1;

-- name: UpdatePlace :one
UPDATE places SET name = $2, kind = $3, position = $4, parent_id = $5
WHERE id = $1 RETURNING *;

-- name: DeletePlace :exec
DELETE FROM places WHERE id = $1;

-- name: PlaceSubtreeInstances :many
-- Copies expected in a place and in every place below it.
WITH RECURSIVE tree AS (
    SELECT pl.id FROM places pl WHERE pl.id = $1
    UNION ALL
    SELECT p.id FROM places p JOIN tree t ON p.parent_id = t.id
)
SELECT i.id, i.edition_id, e.title, e.isbn13
FROM instances i JOIN editions e ON e.id = i.edition_id
WHERE i.place_id IN (SELECT tree.id FROM tree) AND i.deleted_at IS NULL
ORDER BY e.title;
