-- name: CreateLibrary :one
INSERT INTO libraries (name) VALUES ($1) RETURNING *;

-- name: AddLibraryMember :exec
INSERT INTO library_members (library_id, user_id, role) VALUES ($1, $2, $3)
ON CONFLICT (library_id, user_id) DO UPDATE SET role = EXCLUDED.role;

-- name: GetLibraryRole :one
SELECT role FROM library_members WHERE library_id = $1 AND user_id = $2;

-- name: ListLibrariesForUser :many
SELECT l.id, l.name, l.settings, l.created_at, m.role
FROM libraries l JOIN library_members m ON m.library_id = l.id
WHERE m.user_id = $1 ORDER BY l.name, l.id;

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
