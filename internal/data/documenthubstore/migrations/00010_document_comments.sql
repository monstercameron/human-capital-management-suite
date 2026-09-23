-- HUB-018: version-anchored comments and mention consent. Comment rows are
-- immutable and bind the version hash they discuss, so replacing content
-- never invalidates discussion. Mentions start unconsented; only the
-- mentioned user flips their own row, which is how participants join.
-- +goose Up
CREATE TABLE document_comment (
    id text PRIMARY KEY, tenant_id text NOT NULL, document_id text NOT NULL, version_id text NOT NULL,
    version_hash text NOT NULL, author_id text NOT NULL,
    anchor_block text NOT NULL DEFAULT '', quote text NOT NULL DEFAULT '', body text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, id),
    FOREIGN KEY (tenant_id, document_id) REFERENCES document(tenant_id, id) ON DELETE CASCADE,
    FOREIGN KEY (tenant_id, version_id) REFERENCES document_version(tenant_id, id) ON DELETE CASCADE
);
CREATE TABLE document_mention (
    tenant_id text NOT NULL, comment_id text NOT NULL, mentioned_id text NOT NULL,
    consented boolean NOT NULL DEFAULT false, consented_at timestamptz,
    PRIMARY KEY (tenant_id, comment_id, mentioned_id),
    FOREIGN KEY (tenant_id, comment_id) REFERENCES document_comment(tenant_id, id) ON DELETE CASCADE
);
CREATE INDEX document_comment_version ON document_comment(tenant_id, document_id, version_id, created_at);

CREATE TRIGGER document_comment_immutable BEFORE UPDATE OR DELETE ON document_comment FOR EACH ROW EXECUTE FUNCTION document_forbid_mutation();

-- +goose StatementBegin
DO $$ DECLARE t text; BEGIN
  FOREACH t IN ARRAY ARRAY['document_comment','document_mention'] LOOP
    EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
    EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', t);
    EXECUTE format('CREATE POLICY tenant_isolation ON %I USING (tenant_id = current_setting(''hcmnext.tenant_id'', true)) WITH CHECK (tenant_id = current_setting(''hcmnext.tenant_id'', true))', t);
  END LOOP;
END $$;
-- +goose StatementEnd

-- +goose Down
DROP TABLE document_mention, document_comment CASCADE;
