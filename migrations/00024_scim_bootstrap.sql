-- +goose Up
-- Only accounts newly created by trusted SCIM provisioning may bootstrap their first identity.
ALTER TABLE users ADD COLUMN scim_bootstrap boolean NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE users DROP COLUMN scim_bootstrap;
