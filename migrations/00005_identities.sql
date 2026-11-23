-- +goose Up
CREATE TABLE identities (
    issuer     text        NOT NULL,
    subject    text        NOT NULL,
    user_id    uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (issuer, subject)
);
CREATE INDEX identities_user_idx ON identities (user_id);

-- Authorization requests in flight (state, nonce, PKCE verifier); single use.
CREATE TABLE oidc_flows (
    state_hash bytea       PRIMARY KEY,
    nonce      text        NOT NULL,
    verifier   text        NOT NULL,
    expires_at timestamptz NOT NULL
);

-- Groups managed by an external identity provider carry an external_id.
CREATE INDEX groups_external_idx ON groups (external_id) WHERE external_id IS NOT NULL;
