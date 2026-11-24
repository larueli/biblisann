-- +goose Up
-- Step-up authentication (ADR 0026): a session can prove itself again for a short time before
-- sensitive actions. Expand-only and rolling-upgrade safe (nullable column, defaulted column).
ALTER TABLE sessions ADD COLUMN stepup_until timestamptz;
ALTER TABLE oidc_flows ADD COLUMN purpose text NOT NULL DEFAULT 'login' CHECK (purpose IN ('login', 'step_up'));

-- +goose Down
ALTER TABLE oidc_flows DROP COLUMN purpose;
ALTER TABLE sessions DROP COLUMN stepup_until;
