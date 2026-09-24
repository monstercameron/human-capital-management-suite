-- Passage-anchored comments, replies and resolution. Comment rows stay
-- immutable (HUB-018): the anchor (quote with prefix and suffix context,
-- and the server-located code-point offsets into the anchored version's
-- plain text) and the parent of a reply are fixed when the comment is
-- written. Resolving and reopening a thread is its own append-only event;
-- a thread's state is its latest event.
-- +goose Up
ALTER TABLE document_comment
    ADD COLUMN parent_id text NOT NULL DEFAULT '',
    ADD COLUMN anchor_prefix text NOT NULL DEFAULT '',
    ADD COLUMN anchor_suffix text NOT NULL DEFAULT '',
    ADD COLUMN anchor_start integer NOT NULL DEFAULT -1,
    ADD COLUMN anchor_end integer NOT NULL DEFAULT -1;
CREATE INDEX document_comment_document ON document_comment(tenant_id, document_id, created_at);

CREATE TABLE document_comment_resolution (
    id text PRIMARY KEY, tenant_id text NOT NULL, document_id text NOT NULL, comment_id text NOT NULL,
    resolved boolean NOT NULL, actor_id text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (tenant_id, id),
    FOREIGN KEY (tenant_id, comment_id) REFERENCES document_comment(tenant_id, id) ON DELETE CASCADE,
    FOREIGN KEY (tenant_id, document_id) REFERENCES document(tenant_id, id) ON DELETE CASCADE
);
CREATE INDEX document_comment_resolution_comment ON document_comment_resolution(tenant_id, comment_id, created_at);

CREATE TRIGGER document_comment_resolution_immutable BEFORE UPDATE OR DELETE ON document_comment_resolution FOR EACH ROW EXECUTE FUNCTION document_forbid_mutation();

-- +goose StatementBegin
DO $$ DECLARE t text; BEGIN
  FOREACH t IN ARRAY ARRAY['document_comment_resolution'] LOOP
    EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
    EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', t);
    EXECUTE format('CREATE POLICY tenant_isolation ON %I USING (tenant_id = current_setting(''hcmnext.tenant_id'', true)) WITH CHECK (tenant_id = current_setting(''hcmnext.tenant_id'', true))', t);
  END LOOP;
END $$;
-- +goose StatementEnd

-- +goose Down
DROP TABLE document_comment_resolution;
DROP INDEX document_comment_document;
ALTER TABLE document_comment
    DROP COLUMN anchor_end, DROP COLUMN anchor_start, DROP COLUMN anchor_suffix,
    DROP COLUMN anchor_prefix, DROP COLUMN parent_id;
