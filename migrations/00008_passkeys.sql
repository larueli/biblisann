-- +goose Up
CREATE TABLE webauthn_credentials (
    id         bytea       PRIMARY KEY,           -- credential ID
    user_id    uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name       text        NOT NULL CHECK (length(name) BETWEEN 1 AND 100),
    credential jsonb       NOT NULL,              -- go-webauthn Credential (public key, sign count, flags)
    created_at timestamptz NOT NULL DEFAULT now(),
    last_used  timestamptz
);
CREATE INDEX webauthn_credentials_user_idx ON webauthn_credentials (user_id);

-- Ceremonies in flight (challenge and options); single use.
CREATE TABLE webauthn_sessions (
    id         text        PRIMARY KEY,
    user_id    uuid        REFERENCES users (id) ON DELETE CASCADE,
    kind       text        NOT NULL CHECK (kind IN ('register', 'login')),
    data       jsonb       NOT NULL,
    expires_at timestamptz NOT NULL
);
