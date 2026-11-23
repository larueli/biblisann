-- name: GetIdentity :one
SELECT user_id FROM identities WHERE issuer = $1 AND subject = $2;

-- name: CreateIdentity :exec
INSERT INTO identities (issuer, subject, user_id) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING;

-- name: CreateOIDCFlow :exec
INSERT INTO oidc_flows (state_hash, nonce, verifier, expires_at) VALUES ($1, $2, $3, $4);

-- name: ConsumeOIDCFlow :one
DELETE FROM oidc_flows WHERE state_hash = $1 AND expires_at > now() RETURNING nonce, verifier;

-- name: PurgeOIDCFlows :exec
DELETE FROM oidc_flows WHERE expires_at <= now();

-- name: CreateExternalUser :one
INSERT INTO users (email, display_name, password_hash, is_admin) VALUES ($1, $2, NULL, $3) RETURNING *;

-- name: SetUserAdmin :exec
UPDATE users SET is_admin = $2 WHERE id = $1;

-- name: UpsertExternalGroup :one
INSERT INTO groups (name, external_id) VALUES ($1, $2)
ON CONFLICT (lower(name)) DO UPDATE SET external_id = groups.external_id WHERE groups.external_id IS NOT NULL
RETURNING *;
-- A same-named group created by an administrator is never taken over: no row is returned for it.

-- name: ListExternalGroupsOfUser :many
SELECT g.id, g.name FROM groups g JOIN group_members gm ON gm.group_id = g.id
WHERE gm.user_id = $1 AND g.external_id IS NOT NULL;
