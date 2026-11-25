-- +goose Up
-- Existing addresses are unverified until the mailbox or a trusted identity provider proves them.
ALTER TABLE users ADD COLUMN email_verified boolean NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE users DROP COLUMN email_verified;
