-- +goose Up
CREATE TABLE oauth_clients (
    id            text        PRIMARY KEY,
    name          text        NOT NULL CHECK (length(name) BETWEEN 1 AND 200),
    redirect_uris text[]      NOT NULL CHECK (cardinality(redirect_uris) BETWEEN 1 AND 10),
    created_at    timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE oauth_codes (
    code_hash      bytea       PRIMARY KEY,
    client_id      text        NOT NULL REFERENCES oauth_clients (id) ON DELETE CASCADE,
    user_id        uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    redirect_uri   text        NOT NULL,
    code_challenge text        NOT NULL,
    scopes         text[]      NOT NULL,
    library_ids    uuid[],
    resource       text        NOT NULL,
    expires_at     timestamptz NOT NULL
);

CREATE TABLE oauth_refresh_tokens (
    token_hash  bytea       PRIMARY KEY,
    client_id   text        NOT NULL REFERENCES oauth_clients (id) ON DELETE CASCADE,
    user_id     uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    scopes      text[]      NOT NULL,
    library_ids uuid[],
    resource    text        NOT NULL,
    expires_at  timestamptz NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX oauth_refresh_user_idx ON oauth_refresh_tokens (user_id);

-- Short-lived access tokens issued by the authorization server.
CREATE TABLE oauth_access_tokens (
    token_hash  bytea       PRIMARY KEY,
    client_id   text        NOT NULL REFERENCES oauth_clients (id) ON DELETE CASCADE,
    user_id     uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    scopes      text[]      NOT NULL,
    library_ids uuid[],
    resource    text        NOT NULL,
    expires_at  timestamptz NOT NULL
);
CREATE INDEX oauth_access_expires_idx ON oauth_access_tokens (expires_at);
