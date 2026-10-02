-- +goose Up
CREATE TABLE support_inbox_message (
    tenant_id uuid NOT NULL REFERENCES tenant(tenant_id),
    message_id text NOT NULL CHECK (char_length(message_id) BETWEEN 1 AND 128 AND btrim(message_id) = message_id),
    content_ref text NOT NULL CHECK (char_length(content_ref) BETWEEN 1 AND 512),
    content_digest text NOT NULL CHECK (content_digest ~ '^sha256:[0-9a-f]{64}$'),
    claimed_sender_name text NOT NULL CHECK (char_length(claimed_sender_name) BETWEEN 1 AND 200),
    claimed_sender_address text NOT NULL CHECK (char_length(claimed_sender_address) BETWEEN 1 AND 320),
    sender_verified boolean NOT NULL DEFAULT false CHECK (NOT sender_verified),
    received_at timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, message_id)
);
CREATE TRIGGER forbid_mutation BEFORE UPDATE OR DELETE ON support_inbox_message
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
ALTER TABLE support_inbox_message ENABLE ROW LEVEL SECURITY;
ALTER TABLE support_inbox_message FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON support_inbox_message
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
GRANT SELECT, INSERT ON support_inbox_message TO hcmnext_agent_app;

CREATE TABLE agent_profile_birthday_preference (
    tenant_id uuid NOT NULL REFERENCES tenant(tenant_id),
    worker_key text NOT NULL CHECK (char_length(worker_key) BETWEEN 1 AND 256 AND btrim(worker_key) = worker_key),
    share_birthday boolean NOT NULL,
    revision bigint NOT NULL CHECK (revision > 0),
    updated_at timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, worker_key)
);
ALTER TABLE agent_profile_birthday_preference ENABLE ROW LEVEL SECURITY;
ALTER TABLE agent_profile_birthday_preference FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON agent_profile_birthday_preference
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
GRANT SELECT, INSERT, UPDATE ON agent_profile_birthday_preference TO hcmnext_agent_app;

-- +goose Down
DROP TABLE agent_profile_birthday_preference;
DROP TABLE support_inbox_message;
