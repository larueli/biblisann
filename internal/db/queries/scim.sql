-- name: ScimListUsers :many
SELECT * FROM users WHERE ($1::text = '' OR lower(email) = lower($1::text)) ORDER BY created_at, id LIMIT $2 OFFSET $3;

-- name: ScimCountUsers :one
SELECT count(*) FROM users WHERE ($1::text = '' OR lower(email) = lower($1::text));

-- name: ScimUpdateUser :one
UPDATE users SET email = $2, display_name = $3, disabled_at = $4 WHERE id = $1 RETURNING *;

-- name: ScimListGroups :many
SELECT * FROM groups WHERE ($1::text = '' OR lower(name) = lower($1::text)) ORDER BY created_at, id LIMIT $2 OFFSET $3;

-- name: ScimCountGroups :one
SELECT count(*) FROM groups WHERE ($1::text = '' OR lower(name) = lower($1::text));

-- name: ScimRenameGroup :one
UPDATE groups SET name = $2 WHERE id = $1 RETURNING *;

-- name: ScimSetGroupExternal :exec
UPDATE groups SET external_id = $2 WHERE id = $1;
