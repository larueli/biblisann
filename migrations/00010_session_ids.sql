-- +goose Up
-- A stable, non-secret handle so a session can be listed and revoked without exposing its token hash.
ALTER TABLE sessions ADD COLUMN id uuid NOT NULL DEFAULT gen_random_uuid();
CREATE UNIQUE INDEX sessions_id_key ON sessions (id);
