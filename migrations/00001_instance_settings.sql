-- +goose Up
-- Instance-wide settings written by an administrator (defaults layered under library and user
-- settings, see ADR 0013). Values are JSON documents.
CREATE TABLE instance_settings (
    key        text        PRIMARY KEY CHECK (key <> '' AND length(key) <= 128),
    value      jsonb       NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);
