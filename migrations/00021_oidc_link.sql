-- +goose Up
-- Bind an explicit account-link flow to the account that initiated it.
ALTER TABLE oidc_flows DROP CONSTRAINT oidc_flows_purpose_check;
ALTER TABLE oidc_flows ADD CONSTRAINT oidc_flows_purpose_check CHECK (purpose IN ('login', 'step_up', 'link'));
ALTER TABLE oidc_flows ADD COLUMN link_user_id uuid REFERENCES users(id) ON DELETE CASCADE;
ALTER TABLE oidc_flows ADD COLUMN link_session_hash bytea;

-- +goose Down
ALTER TABLE oidc_flows DROP COLUMN link_session_hash;
ALTER TABLE oidc_flows DROP COLUMN link_user_id;
DELETE FROM oidc_flows WHERE purpose = 'link';
ALTER TABLE oidc_flows DROP CONSTRAINT oidc_flows_purpose_check;
ALTER TABLE oidc_flows ADD CONSTRAINT oidc_flows_purpose_check CHECK (purpose IN ('login', 'step_up'));
