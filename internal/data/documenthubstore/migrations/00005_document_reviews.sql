-- HUB-008: independent review decisions. A decision pins the exact
-- version hash the reviewer saw, plus scope, reviewer and authority, so a
-- later deploy (HUB-009) can prove it publishes the reviewed bytes and
-- nothing else. Decisions are append-only; a re-review appends a new row.
-- +goose Up
CREATE TABLE document_review (
    id text PRIMARY KEY, tenant_id text NOT NULL, document_id text NOT NULL, version_id text NOT NULL,
    version_hash text NOT NULL, scope_kind text NOT NULL DEFAULT 'default', scope_id text NOT NULL DEFAULT '',
    reviewer_id text NOT NULL, authority text NOT NULL, decision text NOT NULL DEFAULT 'approved',
    note text NOT NULL DEFAULT '', decided_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, id),
    FOREIGN KEY (tenant_id, document_id) REFERENCES document(tenant_id, id) ON DELETE RESTRICT,
    FOREIGN KEY (tenant_id, version_id) REFERENCES document_version(tenant_id, id) ON DELETE RESTRICT,
    CONSTRAINT document_review_decision_check CHECK (decision IN ('approved','rejected','changes_requested'))
);
CREATE INDEX document_review_version ON document_review(tenant_id, document_id, version_id, scope_kind, scope_id, decided_at);

CREATE TRIGGER document_review_immutable BEFORE UPDATE OR DELETE ON document_review FOR EACH ROW EXECUTE FUNCTION document_forbid_mutation();

-- +goose StatementBegin
DO $$ BEGIN
    EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', 'document_review');
    EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', 'document_review');
    EXECUTE format('CREATE POLICY tenant_isolation ON %I USING (tenant_id = current_setting(''hcmnext.tenant_id'', true)) WITH CHECK (tenant_id = current_setting(''hcmnext.tenant_id'', true))', 'document_review');
END $$;
-- +goose StatementEnd

-- +goose Down
DROP TABLE document_review CASCADE;
