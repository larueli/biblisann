-- +goose Up
-- Record that privacy settings prevented a lookup, independently of a provider miss.
ALTER TABLE editions DROP CONSTRAINT editions_sync_status_check;
ALTER TABLE editions ADD CONSTRAINT editions_sync_status_check
    CHECK (sync_status IN ('', 'updated', 'not_found', 'no_isbn', 'skipped'));
