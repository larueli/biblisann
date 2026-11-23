-- +goose Up
-- A lent loan used to require its copy. When a soft-deleted copy was purged, ON DELETE SET NULL
-- violated that rule and the purge (and with it the whole garbage collection) failed forever.
-- The loan keeps its title, so history survives without the copy. Relaxing a constraint is safe
-- for a rolling upgrade: older code never writes a lent loan without a copy.
ALTER TABLE loans DROP CONSTRAINT IF EXISTS loans_check;

-- +goose Down
-- Forward-only: the constraint is not restored.
SELECT 1;
