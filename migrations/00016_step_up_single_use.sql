-- +goose Up
-- A weak step-up (identity provider without auth_time) unlocks one action only.
ALTER TABLE sessions ADD COLUMN stepup_single_use boolean NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE sessions DROP COLUMN stepup_single_use;
