-- +goose Up
-- Issue 3749: which messages mention a document. One row says that a message
-- (post_id) names a document (target_id) in its text, either as a doc:<id>
-- token or as a Documents address. It holds no text and no title: whether a
-- reader may see the conversation, the message or the document is decided when
-- the row is read, never from this table. The rows are kept current by a
-- trigger on chat_post, so every way a message is written (posted, edited,
-- removed, restored) updates them in the same transaction, and a removed or
-- deleted message leaves no row.
CREATE TABLE chat_post_reference (
    tenant_id text NOT NULL,
    post_id text NOT NULL REFERENCES chat_post(id) ON DELETE CASCADE,
    conversation_id text NOT NULL,
    target_kind text NOT NULL CHECK (target_kind IN ('DOCUMENT')),
    target_id text NOT NULL CHECK (btrim(target_id) <> ''),
    posted_at timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, post_id, target_kind, target_id)
);
CREATE INDEX chat_post_reference_target ON chat_post_reference(tenant_id, target_kind, target_id, posted_at DESC);

-- The document ids a message body names, at most sixteen, in the same two
-- shapes the chat client turns into document links.
-- +goose StatementBegin
CREATE FUNCTION chat_post_document_ids(body text) RETURNS SETOF text LANGUAGE sql IMMUTABLE AS $$
    SELECT found.id FROM (
        SELECT btrim(m[1], '.') AS id FROM regexp_matches(left(body, 32768), '(?:^|[^A-Za-z0-9])doc:([A-Za-z0-9._~-]{1,255})', 'g') AS m
        UNION
        SELECT btrim(m[1], '.') AS id FROM regexp_matches(left(body, 32768), '/workspace/app/docs\?(?:[^\s<>"'']*&)?document=([A-Za-z0-9._~-]{1,255})', 'g') AS m
    ) AS found
    WHERE found.id <> '' AND position('..' IN found.id) = 0
    ORDER BY found.id
    LIMIT 16
$$;
-- +goose StatementEnd

INSERT INTO chat_post_reference(tenant_id, post_id, conversation_id, target_kind, target_id, posted_at)
SELECT p.tenant_id, p.id, p.conversation_id, 'DOCUMENT', d, p.created_at
FROM chat_post p, LATERAL chat_post_document_ids(p.body) AS d
WHERE NOT p.tombstoned AND (p.body LIKE '%doc:%' OR p.body LIKE '%/workspace/app/docs?%')
ON CONFLICT DO NOTHING;

ALTER TABLE chat_post_reference ENABLE ROW LEVEL SECURITY;
ALTER TABLE chat_post_reference FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON chat_post_reference
    USING (tenant_id = current_setting('hcmnext.tenant_id', true))
    WITH CHECK (tenant_id = current_setting('hcmnext.tenant_id', true));

-- +goose StatementBegin
CREATE FUNCTION chat_post_reference_sync() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'UPDATE' THEN
        IF NEW.body IS NOT DISTINCT FROM OLD.body AND NEW.tombstoned = OLD.tombstoned THEN
            RETURN NEW;
        END IF;
        DELETE FROM chat_post_reference WHERE tenant_id = NEW.tenant_id AND post_id = NEW.id;
    END IF;
    IF NOT NEW.tombstoned THEN
        INSERT INTO chat_post_reference(tenant_id, post_id, conversation_id, target_kind, target_id, posted_at)
        SELECT NEW.tenant_id, NEW.id, NEW.conversation_id, 'DOCUMENT', d, NEW.created_at
        FROM chat_post_document_ids(NEW.body) AS d
        ON CONFLICT DO NOTHING;
    END IF;
    RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER chat_post_reference_sync AFTER INSERT OR UPDATE OF body, tombstoned ON chat_post
    FOR EACH ROW EXECUTE FUNCTION chat_post_reference_sync();

-- +goose Down
DROP TRIGGER chat_post_reference_sync ON chat_post;
DROP FUNCTION chat_post_reference_sync();
DROP TABLE chat_post_reference;
DROP FUNCTION chat_post_document_ids(text);
