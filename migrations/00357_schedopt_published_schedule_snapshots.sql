-- REV-045-03: durable immutable published schedule snapshots and current pointer.
-- +goose Up

CREATE TABLE schedopt_published_schedule_revision (
    tenant_id uuid NOT NULL REFERENCES tenant(tenant_id),
    schedule_id text NOT NULL CHECK (btrim(schedule_id) <> ''),
    revision text NOT NULL CHECK (btrim(revision) <> ''),
    approved_digest content_digest NOT NULL,
    publication_digest content_digest NOT NULL,
    problem_digest content_digest NOT NULL,
    rule_revision text NOT NULL CHECK (btrim(rule_revision) <> ''),
    rule_digest content_digest NOT NULL,
    payload_digest content_digest NOT NULL,
    payload text NOT NULL CHECK (jsonb_typeof(payload::jsonb) = 'object'),
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, schedule_id, revision),
    FOREIGN KEY (tenant_id, schedule_id)
        REFERENCES schedopt_self_service_state(tenant_id, schedule_id)
);
ALTER TABLE schedopt_published_schedule_revision ENABLE ROW LEVEL SECURITY;
ALTER TABLE schedopt_published_schedule_revision FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON schedopt_published_schedule_revision
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
CREATE TRIGGER schedopt_published_schedule_revision_immutable
    BEFORE UPDATE OR DELETE ON schedopt_published_schedule_revision
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
GRANT SELECT, INSERT ON schedopt_published_schedule_revision TO hcmnext_app;

CREATE TABLE schedopt_published_schedule_current (
    tenant_id uuid NOT NULL REFERENCES tenant(tenant_id),
    schedule_id text NOT NULL CHECK (btrim(schedule_id) <> ''),
    current_revision text NOT NULL,
    publication_digest content_digest NOT NULL,
    fence bigint NOT NULL CHECK (fence > 0),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, schedule_id),
    FOREIGN KEY (tenant_id, schedule_id, current_revision)
        REFERENCES schedopt_published_schedule_revision(tenant_id, schedule_id, revision),
    FOREIGN KEY (tenant_id, schedule_id)
        REFERENCES schedopt_self_service_state(tenant_id, schedule_id)
);
ALTER TABLE schedopt_published_schedule_current ENABLE ROW LEVEL SECURITY;
ALTER TABLE schedopt_published_schedule_current FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON schedopt_published_schedule_current
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
GRANT SELECT, INSERT, UPDATE ON schedopt_published_schedule_current TO hcmnext_app;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM schedopt_published_schedule_current)
       OR EXISTS (SELECT 1 FROM schedopt_published_schedule_revision) THEN
        RAISE EXCEPTION 'cannot remove published schedule snapshots';
    END IF;
END $$;
-- +goose StatementEnd
DROP TABLE schedopt_published_schedule_current;
DROP TABLE schedopt_published_schedule_revision;
