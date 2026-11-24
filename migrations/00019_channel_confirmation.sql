-- +goose Up
-- Email channels must be confirmed by a code sent to the address (ADR 0026). Channels that exist
-- today keep working: they count as confirmed.
ALTER TABLE notification_channels
    ADD COLUMN confirmed_at      timestamptz,
    ADD COLUMN confirm_hash      bytea,
    ADD COLUMN confirm_expires   timestamptz,
    ADD COLUMN confirm_attempts  int NOT NULL DEFAULT 0;
UPDATE notification_channels SET confirmed_at = created_at;

-- Mail sent per user and day (notifications and confirmation codes), for the daily cap.
CREATE TABLE email_quota (
    user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    day     date NOT NULL,
    sent    int  NOT NULL DEFAULT 0,
    PRIMARY KEY (user_id, day)
);

-- +goose Down
DROP TABLE email_quota;
ALTER TABLE notification_channels DROP COLUMN confirm_attempts, DROP COLUMN confirm_expires,
    DROP COLUMN confirm_hash, DROP COLUMN confirmed_at;
