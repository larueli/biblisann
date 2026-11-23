-- +goose Up
-- A user's link to their own Hardcover account for two-way reading-status and rating sync.
CREATE TABLE hardcover_links (
    user_id       uuid        PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    token_enc     bytea       NOT NULL,                       -- sealed with BIBLISANN_SECRET_KEY
    mode          text        NOT NULL CHECK (mode IN ('pull', 'push', 'both')),
    pull_offset   int         NOT NULL DEFAULT 0,             -- where the next page of the remote library starts
    next_sync_at  timestamptz NOT NULL DEFAULT now(),
    last_sync_at  timestamptz,
    last_error    text        NOT NULL DEFAULT '',
    created_at    timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX hardcover_links_due_idx ON hardcover_links (next_sync_at);

-- What was equal on both sides at the last sync: the basis for telling who changed what.
CREATE TABLE hardcover_book_state (
    user_id         uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    work_id         uuid        NOT NULL REFERENCES works (id) ON DELETE CASCADE,
    hc_book_id      bigint      NOT NULL,
    hc_user_book_id bigint,
    status          text        NOT NULL DEFAULT '',          -- local status name at last sync
    grade           smallint,
    synced_at       timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, work_id)
);
CREATE INDEX hardcover_book_state_book_idx ON hardcover_book_state (user_id, hc_book_id);
