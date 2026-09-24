-- REV-099-05: durable tenant-scoped SIEM feed and ordered event evidence.
-- Delivery intent is stored in the existing transactional outbox in the same
-- transaction as each feed event; cursor reads use the immutable event chain.
-- +goose Up

CREATE TABLE security_siem_feed_head (
    tenant_id uuid NOT NULL REFERENCES tenant(tenant_id),
    last_sequence bigint NOT NULL DEFAULT 0 CHECK (last_sequence >= 0),
    last_digest content_digest,
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id),
    CONSTRAINT security_siem_feed_head_digest_matches_sequence CHECK (
        (last_sequence = 0) = (last_digest IS NULL)
    )
);
ALTER TABLE security_siem_feed_head ENABLE ROW LEVEL SECURITY;
ALTER TABLE security_siem_feed_head FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON security_siem_feed_head
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
GRANT SELECT, INSERT, UPDATE ON security_siem_feed_head TO hcmnext_app;

CREATE TABLE security_siem_feed_event (
    tenant_id uuid NOT NULL REFERENCES tenant(tenant_id),
    sequence bigint NOT NULL CHECK (sequence > 0),
    source_identity semantic_key NOT NULL,
    event_type text NOT NULL CHECK (event_type IN (
        'SECURITY_ALERT_RULE', 'DLP_SIGNAL', 'ACCESS_SIGNAL', 'ADMIN_ACTION'
    )),
    occurred_at timestamptz NOT NULL,
    source_ref text NOT NULL CHECK (btrim(source_ref) <> ''),
    evidence_digest content_digest NOT NULL,
    rule_id semantic_key,
    rule_version integer,
    previous_digest content_digest,
    event_digest content_digest NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, sequence),
    CONSTRAINT security_siem_feed_event_identity_unique UNIQUE (tenant_id, source_identity),
    CONSTRAINT security_siem_feed_event_rule_fields CHECK (
        (event_type = 'SECURITY_ALERT_RULE' AND rule_id IS NOT NULL AND rule_version IS NOT NULL AND rule_version > 0)
        OR (event_type <> 'SECURITY_ALERT_RULE' AND rule_id IS NULL AND rule_version IS NULL)
    ),
    CONSTRAINT security_siem_feed_event_previous_digest CHECK (
        (sequence = 1 AND previous_digest IS NULL)
        OR (sequence > 1 AND previous_digest IS NOT NULL)
    ),
    FOREIGN KEY (tenant_id) REFERENCES security_siem_feed_head(tenant_id)
);
ALTER TABLE security_siem_feed_event ENABLE ROW LEVEL SECURITY;
ALTER TABLE security_siem_feed_event FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON security_siem_feed_event
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
CREATE TRIGGER security_siem_feed_event_forbid_mutation
    BEFORE UPDATE OR DELETE ON security_siem_feed_event
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
GRANT SELECT, INSERT ON security_siem_feed_event TO hcmnext_app;
CREATE INDEX security_siem_feed_event_page
    ON security_siem_feed_event (tenant_id, sequence);

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM security_siem_feed_event)
       OR EXISTS (SELECT 1 FROM security_siem_feed_head) THEN
        RAISE EXCEPTION 'cannot remove customer SIEM feed evidence';
    END IF;
END $$;
-- +goose StatementEnd
DROP TABLE security_siem_feed_event;
DROP TABLE security_siem_feed_head;
