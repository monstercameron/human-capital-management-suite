-- REV-030-01: durable, immutable DataOps import staging receipts.
--
-- The payload is source material only. Staging never writes a business fact;
-- later mapping/validation/commit capabilities own those effects.
--
-- +goose Up

CREATE TABLE IF NOT EXISTS dataops_import_batch (
    tenant_id          tenant_ref NOT NULL REFERENCES tenant (tenant_id),
    batch_id           uuid NOT NULL DEFAULT gen_random_uuid(),
    idempotency_key    semantic_key NOT NULL,
    request_digest     text NOT NULL,
    source_uri         text NOT NULL,
    source_hash        text NOT NULL,
    batch_digest       text NOT NULL,
    header             jsonb NOT NULL,
    row_count          bigint NOT NULL,
    classification     text NOT NULL,
    schema_candidate   text NOT NULL,
    payload            bytea NOT NULL,
    retrieved_at       timestamptz NOT NULL,
    created_at         timestamptz NOT NULL DEFAULT now(),

    PRIMARY KEY (tenant_id, batch_id),
    CONSTRAINT dataops_import_batch_key_unique UNIQUE (tenant_id, idempotency_key),
    CONSTRAINT dataops_import_batch_key_not_blank CHECK (idempotency_key <> ''),
    CONSTRAINT dataops_import_batch_request_digest_shape CHECK (request_digest ~ '^sha256:[0-9a-f]{64}$'),
    CONSTRAINT dataops_import_batch_source_hash_shape CHECK (source_hash ~ '^sha256:[0-9a-f]{64}$'),
    CONSTRAINT dataops_import_batch_digest_shape CHECK (batch_digest <> ''),
    CONSTRAINT dataops_import_batch_uri_not_blank CHECK (source_uri <> ''),
    CONSTRAINT dataops_import_batch_row_count_valid CHECK (row_count >= 0),
    CONSTRAINT dataops_import_batch_classification_not_blank CHECK (classification <> ''),
    CONSTRAINT dataops_import_batch_schema_not_blank CHECK (schema_candidate <> ''),
    CONSTRAINT dataops_import_batch_payload_bound CHECK (octet_length(payload) <= 67108864)
);

CREATE INDEX IF NOT EXISTS dataops_import_batch_created
    ON dataops_import_batch (tenant_id, created_at DESC, batch_id DESC);

ALTER TABLE dataops_import_batch ENABLE ROW LEVEL SECURITY;
ALTER TABLE dataops_import_batch FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON dataops_import_batch
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
CREATE TRIGGER dataops_import_batch_forbid_mutation
    BEFORE UPDATE OR DELETE ON dataops_import_batch
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
REVOKE UPDATE, DELETE ON dataops_import_batch FROM PUBLIC;
REVOKE UPDATE, DELETE ON dataops_import_batch FROM hcmnext_app;
GRANT SELECT, INSERT ON dataops_import_batch TO hcmnext_app;

-- +goose Down

REVOKE ALL ON dataops_import_batch FROM hcmnext_app;
DROP POLICY tenant_isolation ON dataops_import_batch;
ALTER TABLE dataops_import_batch NO FORCE ROW LEVEL SECURITY;
ALTER TABLE dataops_import_batch DISABLE ROW LEVEL SECURITY;
DROP TRIGGER dataops_import_batch_forbid_mutation ON dataops_import_batch;
DROP TABLE dataops_import_batch;
