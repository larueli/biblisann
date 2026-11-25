-- +goose Up
-- What the last provider refresh found, so "no provider knows this book" is visible and the
-- exclusion from refreshes has a clear meaning for such books.
ALTER TABLE editions ADD COLUMN sync_status text NOT NULL DEFAULT '' CHECK (sync_status IN ('', 'updated', 'not_found', 'no_isbn')),
                     ADD COLUMN synced_at timestamptz;

-- +goose Down
ALTER TABLE editions DROP COLUMN synced_at, DROP COLUMN sync_status;
