-- name: GetInstanceSetting :one
SELECT key, value, updated_at FROM instance_settings WHERE key = $1;

-- name: SetInstanceSetting :one
INSERT INTO instance_settings (key, value)
VALUES ($1, $2)
ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = now()
RETURNING key, value, updated_at;

-- name: ListInstanceSettings :many
SELECT key, value, updated_at FROM instance_settings ORDER BY key;
