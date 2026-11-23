-- +goose Up
-- When each work was last looked up at an external rating source, and what came of it. It drives
-- the periodic refresh: rated books are rechecked every few days, unknown ones less often, failed
-- ones with a growing delay.
CREATE TABLE external_grade_checks (
    work_id    uuid        NOT NULL REFERENCES works (id) ON DELETE CASCADE,
    source     text        NOT NULL,
    checked_at timestamptz NOT NULL DEFAULT now(),
    status     text        NOT NULL CHECK (status IN ('ok', 'none', 'error')),
    attempts   int         NOT NULL DEFAULT 0,
    PRIMARY KEY (work_id, source)
);
CREATE INDEX external_grade_checks_due_idx ON external_grade_checks (source, checked_at);
