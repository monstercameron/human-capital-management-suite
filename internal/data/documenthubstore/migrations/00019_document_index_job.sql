-- Queued meaning-search indexing. A write that makes a version searchable
-- (an owner's new candidate, a deployment) enqueues one job per version and
-- embedding model in its own transaction; a background indexer claims jobs
-- under a lease with FOR UPDATE SKIP LOCKED, embeds the version's sections
-- and records the outcome. Nothing embeds inside a request. A lease that
-- runs out returns the job to any worker; failures back off and stop at a
-- capped attempt count with the error kept; a version that stopped being
-- searchable before its turn is skipped, not embedded.
-- +goose Up
CREATE TABLE document_index_job (
    id bigserial PRIMARY KEY,
    tenant_id text NOT NULL, document_id text NOT NULL, version_id text NOT NULL, model_id text NOT NULL,
    status text NOT NULL DEFAULT 'queued',
    attempts integer NOT NULL DEFAULT 0,
    next_attempt_at timestamptz NOT NULL DEFAULT now(),
    last_error text NOT NULL DEFAULT '',
    lease_owner text NOT NULL DEFAULT '', lease_until timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, document_id, version_id, model_id),
    FOREIGN KEY (tenant_id, document_id) REFERENCES document(tenant_id, id) ON DELETE CASCADE,
    FOREIGN KEY (tenant_id, version_id) REFERENCES document_version(tenant_id, id) ON DELETE CASCADE,
    CONSTRAINT document_index_job_status_check CHECK (status IN ('queued','running','done','failed','skipped'))
);
CREATE INDEX document_index_job_due ON document_index_job(tenant_id, model_id, next_attempt_at, id) WHERE status IN ('queued','running');

-- +goose StatementBegin
DO $$ DECLARE t text; BEGIN
  FOREACH t IN ARRAY ARRAY['document_index_job'] LOOP
    EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
    EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', t);
    EXECUTE format('CREATE POLICY tenant_isolation ON %I USING (tenant_id = current_setting(''hcmnext.tenant_id'', true)) WITH CHECK (tenant_id = current_setting(''hcmnext.tenant_id'', true))', t);
  END LOOP;
END $$;
-- +goose StatementEnd

-- +goose Down
DROP TABLE document_index_job;
