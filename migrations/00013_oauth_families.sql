-- +goose Up
-- A "family" is one authorization and every token rotated from it. A refresh token that is presented
-- again after it was used reveals a copy: the whole family is revoked.
ALTER TABLE oauth_refresh_tokens ADD COLUMN family_id uuid NOT NULL DEFAULT gen_random_uuid();
ALTER TABLE oauth_refresh_tokens ADD COLUMN used_at timestamptz;
ALTER TABLE oauth_access_tokens  ADD COLUMN family_id uuid NOT NULL DEFAULT gen_random_uuid();
CREATE INDEX oauth_refresh_family_idx ON oauth_refresh_tokens (family_id);
CREATE INDEX oauth_access_family_idx ON oauth_access_tokens (family_id);
