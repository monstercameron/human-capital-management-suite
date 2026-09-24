-- Owner: intelligence data adapter. REV-026-02 persists typed metric output
-- and its decision-to-outcome link as one tenant-scoped immutable publication.
-- +goose Up

CREATE TABLE intelligence_metric_publication (
    tenant_id          tenant_ref NOT NULL REFERENCES tenant (tenant_id),
    publication_id     uuid NOT NULL,
    metric_id          semantic_key NOT NULL,
    metric_version     semantic_key NOT NULL,
    decision_id        uuid NOT NULL,
    calculation_digest text NOT NULL,
    outcome_digest     text NOT NULL,
    metric_result      jsonb NOT NULL,
    outcome_link       jsonb NOT NULL,
    published_at       timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, publication_id),
    UNIQUE (tenant_id, calculation_digest, outcome_digest),
    FOREIGN KEY (tenant_id, decision_id) REFERENCES intent_decision (tenant_id, decision_id)
);

CREATE TRIGGER intelligence_metric_publication_forbid_mutation
    BEFORE UPDATE OR DELETE ON intelligence_metric_publication
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
REVOKE UPDATE, DELETE ON intelligence_metric_publication FROM PUBLIC;

ALTER TABLE intelligence_metric_publication ENABLE ROW LEVEL SECURITY;
ALTER TABLE intelligence_metric_publication FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON intelligence_metric_publication
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
GRANT SELECT, INSERT ON intelligence_metric_publication TO hcmnext_app;

-- +goose Down

-- Published metric results are immutable decision evidence. Removing the table
-- would erase that evidence, so rollback must stop and use a governed forward
-- correction instead.
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION '00343 is irreversible: intelligence_metric_publication stores immutable metric results and decision-to-outcome evidence; dropping it would erase durable published records, so corrections must be appended through the governed correction path'; END $$;
-- +goose StatementEnd
