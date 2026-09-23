-- HUB-007: deployment records and scoped active pointers. A deployment is
-- the immutable fact that a version went live for one scope; the active
-- pointer names the current deployment per scope. Pointer moves never
-- rewrite deployment history. Review binding (HUB-008) and atomic
-- publish (HUB-009) build on these tables.
-- +goose Up
CREATE TABLE document_deployment (
    id text PRIMARY KEY, tenant_id text NOT NULL, document_id text NOT NULL, version_id text NOT NULL,
    scope_kind text NOT NULL DEFAULT 'default', scope_id text NOT NULL DEFAULT '',
    deployer_id text NOT NULL, effective_at timestamptz NOT NULL DEFAULT now(),
    review_due_at timestamptz,
    prior_deployment_id text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, id),
    FOREIGN KEY (tenant_id, document_id) REFERENCES document(tenant_id, id) ON DELETE RESTRICT,
    FOREIGN KEY (tenant_id, version_id) REFERENCES document_version(tenant_id, id) ON DELETE RESTRICT
);
CREATE TABLE document_active_pointer (
    tenant_id text NOT NULL, document_id text NOT NULL,
    scope_kind text NOT NULL DEFAULT 'default', scope_id text NOT NULL DEFAULT '',
    deployment_id text NOT NULL, version_id text NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, document_id, scope_kind, scope_id),
    FOREIGN KEY (tenant_id, deployment_id) REFERENCES document_deployment(tenant_id, id) ON DELETE RESTRICT
);
CREATE INDEX document_deployment_document ON document_deployment(tenant_id, document_id, created_at);

CREATE TRIGGER document_deployment_immutable BEFORE UPDATE OR DELETE ON document_deployment FOR EACH ROW EXECUTE FUNCTION document_forbid_mutation();

-- +goose StatementBegin
DO $$ DECLARE t text; BEGIN
  FOREACH t IN ARRAY ARRAY['document_deployment','document_active_pointer'] LOOP
    EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
    EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', t);
    EXECUTE format('CREATE POLICY tenant_isolation ON %I USING (tenant_id = current_setting(''hcmnext.tenant_id'', true)) WITH CHECK (tenant_id = current_setting(''hcmnext.tenant_id'', true))', t);
  END LOOP;
END $$;
-- +goose StatementEnd

-- +goose Down
DROP TABLE document_active_pointer, document_deployment CASCADE;
