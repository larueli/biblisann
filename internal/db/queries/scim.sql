-- name: ScimListUsers :many
SELECT * FROM users WHERE ($1::text = '' OR lower(email) = lower($1::text)) ORDER BY created_at, id LIMIT $2 OFFSET $3;

-- name: ScimCountUsers :one
SELECT count(*) FROM users WHERE ($1::text = '' OR lower(email) = lower($1::text));

-- name: CreateScimUser :one
INSERT INTO users (email, display_name, password_hash, is_admin, scim_bootstrap, disabled_at)
VALUES ($1, $2, NULL, false, true, $3) RETURNING *;

-- name: ClaimScimBootstrap :one
UPDATE users SET scim_bootstrap = false, email_verified = true
WHERE users.id = sqlc.arg(user_id) AND scim_bootstrap AND disabled_at IS NULL AND password_hash IS NULL
  AND lower(email) = lower(sqlc.arg(email)::text)
  AND NOT EXISTS (SELECT 1 FROM sessions WHERE user_id = users.id)
  AND NOT EXISTS (SELECT 1 FROM identities WHERE user_id = users.id)
  AND NOT EXISTS (SELECT 1 FROM webauthn_credentials WHERE user_id = users.id)
  AND NOT EXISTS (SELECT 1 FROM user_totp WHERE user_id = users.id)
  AND NOT EXISTS (SELECT 1 FROM access_tokens WHERE user_id = users.id)
  AND NOT EXISTS (SELECT 1 FROM oauth_codes WHERE user_id = users.id)
  AND NOT EXISTS (SELECT 1 FROM oauth_refresh_tokens WHERE user_id = users.id)
  AND NOT EXISTS (SELECT 1 FROM oauth_access_tokens WHERE user_id = users.id)
RETURNING users.id;

-- name: ScimUpdateUser :one
UPDATE users SET email_verified = CASE WHEN lower(email) = lower($2) THEN email_verified ELSE false END,
  email = $2, display_name = $3, disabled_at = $4 WHERE id = $1 RETURNING *;

-- name: ScimListGroups :many
SELECT * FROM groups WHERE ($1::text = '' OR lower(name) = lower($1::text)) ORDER BY created_at, id LIMIT $2 OFFSET $3;

-- name: ScimCountGroups :one
SELECT count(*) FROM groups WHERE ($1::text = '' OR lower(name) = lower($1::text));

-- name: ScimRenameGroup :one
UPDATE groups SET name = $2 WHERE id = $1 RETURNING *;

-- name: ScimSetGroupExternal :exec
UPDATE groups SET external_id = $2 WHERE id = $1;
