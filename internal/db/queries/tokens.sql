-- name: CreateToken :one
INSERT INTO access_tokens (user_id, name, kind, token_hash, scopes, library_ids, expires_at)
VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id, user_id, name, kind, scopes, library_ids, expires_at, created_at;

-- name: GetTokenByHash :one
SELECT t.id, t.user_id, t.kind, t.scopes, t.library_ids, u.email, u.is_admin, u.disabled_at
FROM access_tokens t JOIN users u ON u.id = t.user_id
WHERE t.token_hash = $1 AND (t.expires_at IS NULL OR t.expires_at > now());

-- name: TouchToken :exec
UPDATE access_tokens SET last_used = now() WHERE id = $1;

-- name: ListTokens :many
SELECT id, name, kind, scopes, library_ids, expires_at, last_used, created_at
FROM access_tokens WHERE user_id = $1 ORDER BY created_at DESC;

-- name: DeleteToken :execrows
DELETE FROM access_tokens WHERE id = $1 AND user_id = $2;

-- name: DeleteUserTokens :exec
DELETE FROM access_tokens WHERE user_id = $1 AND kind = 'pat';
