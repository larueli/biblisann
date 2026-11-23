-- name: AddCredential :exec
INSERT INTO webauthn_credentials (id, user_id, name, credential) VALUES ($1, $2, $3, $4);

-- name: ListCredentials :many
SELECT * FROM webauthn_credentials WHERE user_id = $1 ORDER BY created_at;

-- name: GetCredential :one
SELECT * FROM webauthn_credentials WHERE id = $1;

-- name: UpdateCredential :exec
UPDATE webauthn_credentials SET credential = $2, last_used = now() WHERE id = $1;

-- name: DeleteCredential :execrows
DELETE FROM webauthn_credentials WHERE id = $1 AND user_id = $2;

-- name: CreateWebAuthnSession :exec
INSERT INTO webauthn_sessions (id, user_id, kind, data, expires_at) VALUES ($1, $2, $3, $4, $5);

-- name: ConsumeWebAuthnSession :one
DELETE FROM webauthn_sessions WHERE id = $1 AND kind = $2 AND expires_at > now() RETURNING *;

-- name: PurgeWebAuthnSessions :exec
DELETE FROM webauthn_sessions WHERE expires_at <= now();
