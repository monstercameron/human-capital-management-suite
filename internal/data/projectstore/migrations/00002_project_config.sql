-- +goose Up
CREATE TABLE project_workflow_draft (
    tenant_id text NOT NULL,
    project_id text NOT NULL,
    revision bigint NOT NULL CHECK (revision > 0),
    config_json jsonb NOT NULL,
    config_digest text NOT NULL CHECK (length(config_digest) = 64),
    validation_json jsonb NOT NULL DEFAULT '[]'::jsonb,
    actor_id text NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, project_id),
    FOREIGN KEY (tenant_id, project_id) REFERENCES project(tenant_id, id)
);

CREATE TABLE project_workflow_version (
    tenant_id text NOT NULL,
    project_id text NOT NULL,
    version bigint NOT NULL CHECK (version > 0),
    source_revision bigint NOT NULL CHECK (source_revision > 0),
    config_json jsonb NOT NULL,
    digest text NOT NULL CHECK (length(digest) = 64),
    publisher_id text NOT NULL,
    reviewer_id text NOT NULL DEFAULT '',
    review_evidence jsonb NOT NULL DEFAULT '{}'::jsonb,
    published_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, project_id, version),
    FOREIGN KEY (tenant_id, project_id) REFERENCES project(tenant_id, id)
);
CREATE TRIGGER project_workflow_version_immutable BEFORE UPDATE OR DELETE ON project_workflow_version FOR EACH ROW EXECUTE FUNCTION project_forbid_mutation();

CREATE TABLE project_workflow_current (
    tenant_id text NOT NULL,
    project_id text NOT NULL,
    version bigint NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, project_id),
    FOREIGN KEY (tenant_id, project_id, version) REFERENCES project_workflow_version(tenant_id, project_id, version)
);

CREATE TABLE project_workflow_idempotency (
    tenant_id text NOT NULL,
    actor_id text NOT NULL,
    operation text NOT NULL,
    client_key text NOT NULL,
    fingerprint text NOT NULL,
    result_json jsonb NOT NULL DEFAULT 'null'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, actor_id, operation, client_key)
);

-- +goose StatementBegin
DO $$ DECLARE t text; BEGIN
  FOREACH t IN ARRAY ARRAY['project_workflow_draft','project_workflow_version','project_workflow_current','project_workflow_idempotency'] LOOP
    EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
    EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', t);
    EXECUTE format('CREATE POLICY tenant_isolation ON %I USING (tenant_id = current_setting(''hcmnext.tenant_id'', true)) WITH CHECK (tenant_id = current_setting(''hcmnext.tenant_id'', true))', t);
  END LOOP;
END $$;
-- +goose StatementEnd

-- +goose Down
DROP TABLE project_workflow_idempotency, project_workflow_current, project_workflow_version, project_workflow_draft CASCADE;
