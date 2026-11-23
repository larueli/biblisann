-- +goose Up
ALTER TABLE access_tokens DROP CONSTRAINT access_tokens_kind_check;
ALTER TABLE access_tokens ADD CONSTRAINT access_tokens_kind_check CHECK (kind IN ('pat', 'feed', 'scim'));
