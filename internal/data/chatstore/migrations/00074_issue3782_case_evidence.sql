-- +goose Up
-- Issue 3782: reviewed conversation evidence attached to a governed case. One
-- row freezes one message of a case-bound conversation against the case the
-- conversation was bound to (an opaque reference; the case lives outside Chat)
-- and, when the person named one, the revision of the case's proposal. Rows
-- sharing an excerpt_id were attached together. The table is append-only: the
-- text of a copy never changes, and nothing here claims an approval, a policy
-- version or an outcome. Each attachment is also a DERIVED_COPY row of
-- chat_record_inventory, so retention and legal holds see the copy.
CREATE TABLE chat_case_evidence (
    tenant_id text NOT NULL,
    id text NOT NULL CHECK (btrim(id) <> ''),
    excerpt_id text NOT NULL CHECK (btrim(excerpt_id) <> ''),
    ordinal integer NOT NULL CHECK (ordinal >= 0),
    conversation_id text NOT NULL CHECK (btrim(conversation_id) <> ''),
    case_ref text NOT NULL CHECK (btrim(case_ref) <> '' AND char_length(case_ref) <= 120),
    case_revision text NOT NULL DEFAULT '' CHECK (char_length(case_revision) <= 80),
    message_id text NOT NULL CHECK (btrim(message_id) <> ''),
    source_revision bigint NOT NULL CHECK (source_revision >= 0),
    author_id text NOT NULL CHECK (btrim(author_id) <> ''),
    authored_at timestamptz NOT NULL,
    body text NOT NULL,
    content_hash text NOT NULL CHECK (content_hash ~ '^[0-9a-f]{64}$'),
    note text NOT NULL DEFAULT '' CHECK (char_length(note) <= 500),
    attached_by text NOT NULL CHECK (btrim(attached_by) <> ''),
    attached_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, id),
    UNIQUE (tenant_id, excerpt_id, ordinal)
);
CREATE INDEX chat_case_evidence_case ON chat_case_evidence (tenant_id, conversation_id, case_ref, attached_at, excerpt_id, ordinal);
ALTER TABLE chat_case_evidence ENABLE ROW LEVEL SECURITY;
ALTER TABLE chat_case_evidence FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON chat_case_evidence
    USING (tenant_id = current_setting('hcmnext.tenant_id', true))
    WITH CHECK (tenant_id = current_setting('hcmnext.tenant_id', true));
CREATE TRIGGER chat_case_evidence_immutable BEFORE UPDATE OR DELETE ON chat_case_evidence FOR EACH ROW EXECUTE FUNCTION chat_forbid_mutation();

-- +goose Down
DROP TABLE chat_case_evidence;
