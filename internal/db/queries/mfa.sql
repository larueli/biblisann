-- name: GetTOTP :one
SELECT * FROM user_totp WHERE user_id = $1;

-- name: UpsertTOTP :exec
INSERT INTO user_totp (user_id, secret_enc) VALUES ($1, $2)
ON CONFLICT (user_id) DO UPDATE SET secret_enc = EXCLUDED.secret_enc, confirmed_at = NULL, last_step = 0;

-- name: ConfirmTOTP :exec
UPDATE user_totp SET confirmed_at = now() WHERE user_id = $1;

-- name: SetTOTPStep :execrows
UPDATE user_totp SET last_step = $2 WHERE user_id = $1 AND last_step < $2;

-- name: DeleteTOTP :exec
DELETE FROM user_totp WHERE user_id = $1;

-- name: DeleteRecoveryCodes :exec
DELETE FROM recovery_codes WHERE user_id = $1;

-- name: AddRecoveryCode :exec
INSERT INTO recovery_codes (user_id, code_hash) VALUES ($1, $2);

-- name: UseRecoveryCode :execrows
UPDATE recovery_codes SET used_at = now() WHERE user_id = $1 AND code_hash = $2 AND used_at IS NULL;

-- name: CountRecoveryCodes :one
SELECT count(*)::int FROM recovery_codes WHERE user_id = $1 AND used_at IS NULL;

-- name: CreateMFAChallenge :exec
INSERT INTO mfa_challenges (token_hash, user_id, expires_at) VALUES ($1, $2, $3);

-- name: GetMFAChallenge :one
SELECT * FROM mfa_challenges WHERE token_hash = $1 AND expires_at > now();

-- name: BumpMFAAttempts :one
UPDATE mfa_challenges SET attempts = attempts + 1 WHERE token_hash = $1 RETURNING attempts;

-- name: DeleteMFAChallenge :exec
DELETE FROM mfa_challenges WHERE token_hash = $1;

-- name: CreatePasswordReset :exec
INSERT INTO password_resets (token_hash, user_id, expires_at) VALUES ($1, $2, $3);

-- name: UsePasswordReset :one
UPDATE password_resets SET used_at = now()
WHERE token_hash = $1 AND used_at IS NULL AND expires_at > now() RETURNING user_id;
