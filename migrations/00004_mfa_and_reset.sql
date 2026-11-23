-- +goose Up
CREATE TABLE user_totp (
    user_id      uuid        PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    secret_enc   bytea       NOT NULL, -- AES-GCM sealed with BIBLISANN_SECRET_KEY
    confirmed_at timestamptz,
    last_step    bigint      NOT NULL DEFAULT 0 -- last accepted time step: a code is valid once
);

CREATE TABLE recovery_codes (
    user_id   uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    code_hash bytea       NOT NULL,
    used_at   timestamptz,
    PRIMARY KEY (user_id, code_hash)
);

-- Between the password step and the second factor.
CREATE TABLE mfa_challenges (
    token_hash bytea       PRIMARY KEY,
    user_id    uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    expires_at timestamptz NOT NULL,
    attempts   int         NOT NULL DEFAULT 0
);

CREATE TABLE password_resets (
    token_hash bytea       PRIMARY KEY,
    user_id    uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    expires_at timestamptz NOT NULL,
    used_at    timestamptz
);
