-- name: CountUsers :one
SELECT count(*) FROM users;

-- name: CreateUser :one
INSERT INTO users (email, display_name, password_hash, is_admin, locale)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetUserByEmail :one
SELECT * FROM users WHERE lower(email) = lower($1);

-- name: GetUser :one
SELECT * FROM users WHERE id = $1;

-- name: UpdateUserPassword :exec
UPDATE users SET password_hash = $2 WHERE id = $1;

-- name: CreateSession :exec
INSERT INTO sessions (token_hash, user_id, csrf_token, expires_at, user_agent)
VALUES ($1, $2, $3, $4, $5);

-- name: GetSession :one
SELECT s.token_hash, s.user_id, s.csrf_token, s.expires_at, s.stepup_until, s.stepup_single_use,
       u.email, u.display_name, u.is_admin, u.locale, u.disabled_at
FROM sessions s JOIN users u ON u.id = s.user_id
WHERE s.token_hash = $1 AND s.expires_at > now();

-- name: MarkSessionStepUp :exec
UPDATE sessions SET stepup_until = $2, stepup_single_use = $3 WHERE token_hash = $1;

-- name: ConsumeSessionStepUp :exec
UPDATE sessions SET stepup_until = NULL, stepup_single_use = false WHERE token_hash = $1 AND stepup_single_use;

-- name: TouchSession :exec
UPDATE sessions SET last_seen = now() WHERE token_hash = $1;

-- name: DeleteSession :exec
DELETE FROM sessions WHERE token_hash = $1;

-- name: DeleteUserSessions :exec
DELETE FROM sessions WHERE user_id = $1;

-- name: DeleteExpiredSessions :execrows
DELETE FROM sessions WHERE expires_at <= now();

-- name: ListUserSessions :many
SELECT id, token_hash, created_at, last_seen, expires_at, user_agent FROM sessions
WHERE user_id = $1 AND expires_at > now() ORDER BY last_seen DESC;

-- name: DeleteSessionByID :execrows
DELETE FROM sessions WHERE id = $1 AND user_id = $2;

-- name: DeleteOtherSessions :execrows
DELETE FROM sessions WHERE user_id = $1 AND token_hash <> $2;

-- name: SetUserDisabled :exec
UPDATE users SET disabled_at = $2 WHERE id = $1;

-- name: ListUsersAdmin :many
SELECT id, email, display_name, is_admin, disabled_at, created_at FROM users ORDER BY created_at, id LIMIT $1 OFFSET $2;

-- name: CountAdminUsers :one
SELECT count(*)::int FROM users WHERE is_admin AND disabled_at IS NULL;

-- name: DeleteUser :exec
DELETE FROM users WHERE id = $1;
