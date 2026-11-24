-- +goose Up
-- An administrator can exclude an edition from provider refreshes (a provider that went wrong).
ALTER TABLE editions ADD COLUMN sync_excluded boolean NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE editions DROP COLUMN sync_excluded;
