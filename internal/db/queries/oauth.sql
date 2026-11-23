-- name: CreateOAuthClient :exec
INSERT INTO oauth_clients (id, name, redirect_uris) VALUES ($1, $2, $3);

-- name: GetOAuthClient :one
SELECT * FROM oauth_clients WHERE id = $1;

-- name: CountOAuthClients :one
SELECT count(*) FROM oauth_clients;

-- name: CreateOAuthCode :exec
INSERT INTO oauth_codes (code_hash, client_id, user_id, redirect_uri, code_challenge, scopes, library_ids, resource, expires_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9);

-- name: ConsumeOAuthCode :one
DELETE FROM oauth_codes WHERE code_hash = $1 AND expires_at > now() RETURNING *;

-- name: CreateOAuthRefresh :exec
INSERT INTO oauth_refresh_tokens (token_hash, client_id, user_id, scopes, library_ids, resource, expires_at, family_id)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8);

-- name: LockOAuthRefresh :one
SELECT * FROM oauth_refresh_tokens WHERE token_hash = $1 AND expires_at > now() FOR UPDATE;

-- name: MarkOAuthRefreshUsed :exec
UPDATE oauth_refresh_tokens SET used_at = now() WHERE token_hash = $1;

-- name: RevokeOAuthFamily :exec
DELETE FROM oauth_refresh_tokens WHERE family_id = $1;

-- name: RevokeOAuthFamilyAccess :exec
DELETE FROM oauth_access_tokens WHERE family_id = $1;

-- name: CreateOAuthAccess :exec
INSERT INTO oauth_access_tokens (token_hash, client_id, user_id, scopes, library_ids, resource, expires_at, family_id)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8);

-- name: GetOAuthAccess :one
SELECT t.user_id, t.scopes, t.library_ids, t.resource, u.email, u.is_admin, u.disabled_at
FROM oauth_access_tokens t JOIN users u ON u.id = t.user_id
WHERE t.token_hash = $1 AND t.expires_at > now();

-- name: PurgeOAuth :exec
DELETE FROM oauth_access_tokens WHERE expires_at <= now();

-- name: RevokeOAuthForClient :exec
DELETE FROM oauth_refresh_tokens WHERE user_id = $1 AND client_id = $2;

-- name: ListOAuthGrants :many
SELECT c.id AS client_id, c.name, array_agg(DISTINCT s ORDER BY s)::text[] AS scopes, min(r.created_at)::timestamptz AS since
FROM oauth_refresh_tokens r JOIN oauth_clients c ON c.id = r.client_id, unnest(r.scopes) AS s
WHERE r.user_id = $1 AND r.used_at IS NULL GROUP BY c.id, c.name ORDER BY since;

-- name: RevokeOAuthRefreshForClient :execrows
DELETE FROM oauth_refresh_tokens WHERE user_id = $1 AND client_id = $2;

-- name: RevokeOAuthAccessForClient :exec
DELETE FROM oauth_access_tokens WHERE user_id = $1 AND client_id = $2;

-- name: RevokeAllOAuthRefreshForUser :exec
DELETE FROM oauth_refresh_tokens WHERE user_id = $1;

-- name: RevokeAllOAuthAccessForUser :exec
DELETE FROM oauth_access_tokens WHERE user_id = $1;
