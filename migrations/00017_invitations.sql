-- +goose Up
-- Invitations (ADR 0026): an instance administrator can invite an email address that has no
-- account yet. The secret link proves ownership of the mailbox; only its hash is stored.
CREATE TABLE library_invitations (
    id         uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    library_id uuid        NOT NULL REFERENCES libraries (id) ON DELETE CASCADE,
    email      text        NOT NULL CHECK (length(email) BETWEEN 3 AND 320),
    role       text        NOT NULL CHECK (role IN ('read', 'edit', 'admin', 'custom')),
    profile_id uuid        REFERENCES permission_profiles (id) ON DELETE CASCADE,
    invited_by uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    token_hash bytea       NOT NULL UNIQUE,
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX library_invitations_library_idx ON library_invitations (library_id);
CREATE INDEX library_invitations_expires_idx ON library_invitations (expires_at);

-- +goose Down
DROP TABLE library_invitations;
