-- HUB-025: lexical search terms per version. Derived evidence rebuilt from
-- version bytes, so rows carry no audience: deployment liveness and the
-- reader's current grants are evaluated per query, and stale rows never
-- surface because only pointer-live versions qualify.
-- +goose Up
CREATE TABLE document_search_term (
    tenant_id text NOT NULL, document_id text NOT NULL, version_id text NOT NULL,
    term text NOT NULL, field text NOT NULL, hits integer NOT NULL,
    PRIMARY KEY (tenant_id, version_id, term, field),
    FOREIGN KEY (tenant_id, document_id) REFERENCES document(tenant_id, id) ON DELETE CASCADE,
    FOREIGN KEY (tenant_id, version_id) REFERENCES document_version(tenant_id, id) ON DELETE CASCADE
);
CREATE INDEX document_search_term_lookup ON document_search_term(tenant_id, term);

-- +goose StatementBegin
DO $$ DECLARE t text; BEGIN
  FOREACH t IN ARRAY ARRAY['document_search_term'] LOOP
    EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
    EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', t);
    EXECUTE format('CREATE POLICY tenant_isolation ON %I USING (tenant_id = current_setting(''hcmnext.tenant_id'', true)) WITH CHECK (tenant_id = current_setting(''hcmnext.tenant_id'', true))', t);
  END LOOP;
END $$;
-- +goose StatementEnd

-- +goose Down
DROP TABLE document_search_term;
