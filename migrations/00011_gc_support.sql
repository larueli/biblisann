-- +goose Up
-- Change-log compaction forgets old tombstones: a client whose cursor is older than this horizon
-- must resynchronize from scratch (the API tells it to).
ALTER TABLE libraries ADD COLUMN changes_pruned_through bigint NOT NULL DEFAULT 0;
