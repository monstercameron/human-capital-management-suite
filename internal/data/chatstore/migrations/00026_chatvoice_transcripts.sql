-- +goose Up
CREATE TABLE chat_voice_transcript (
    tenant_id text NOT NULL,
    conversation_id text NOT NULL,
    post_id text NOT NULL REFERENCES chat_post(id) ON DELETE CASCADE,
    artifact_id text NOT NULL,
    duration_ms bigint NOT NULL CHECK (duration_ms BETWEEN 1 AND 120000),
    attachment jsonb NOT NULL,
    transcript jsonb NOT NULL,
    search_text text NOT NULL DEFAULT '',
    state text NOT NULL CHECK (state IN ('none','pending','ready','unavailable','failed')),
    revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
    corrections jsonb NOT NULL DEFAULT '[]',
    reports jsonb NOT NULL DEFAULT '[]',
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, conversation_id, post_id, artifact_id),
    FOREIGN KEY (tenant_id, conversation_id) REFERENCES chat_conversation(tenant_id, id) ON DELETE CASCADE
);
CREATE INDEX chat_voice_pending ON chat_voice_transcript(tenant_id, conversation_id, state, updated_at);
CREATE INDEX chat_voice_search ON chat_voice_transcript USING gin(to_tsvector('simple', search_text));
ALTER TABLE chat_voice_transcript ENABLE ROW LEVEL SECURITY;
ALTER TABLE chat_voice_transcript FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON chat_voice_transcript
    USING (tenant_id = current_setting('hcmnext.tenant_id', true))
    WITH CHECK (tenant_id = current_setting('hcmnext.tenant_id', true));

-- +goose StatementBegin
DO $$ DECLARE role_name text; BEGIN
    FOR role_name IN SELECT grantee FROM information_schema.role_table_grants
      WHERE table_schema=current_schema() AND table_name='chat_ephemeral_post' AND privilege_type='SELECT'
    LOOP
      EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON chat_voice_transcript TO %I',role_name);
    END LOOP;
END $$;
-- +goose StatementEnd

-- A tombstone hides held content through every reader. Without an active hold,
-- the request, segments, correction history and search text are erased together.
-- +goose StatementBegin
CREATE FUNCTION chatvoice_erase_transcript() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.tombstoned AND NOT OLD.tombstoned AND NOT EXISTS (
        SELECT 1 FROM chat_record_inventory i JOIN chat_record_hold h
          ON h.tenant_id=i.tenant_id AND h.hold_id IN (SELECT jsonb_array_elements_text(i.hold_ids))
        WHERE i.tenant_id=NEW.tenant_id AND i.record_id='post:' || NEW.id AND h.released_at IS NULL
    ) THEN
        DELETE FROM chat_voice_transcript WHERE tenant_id=NEW.tenant_id AND post_id=NEW.id;
    END IF;
    RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER chatvoice_post_erasure AFTER UPDATE ON chat_post FOR EACH ROW EXECUTE FUNCTION chatvoice_erase_transcript();

-- +goose Down
DROP TRIGGER chatvoice_post_erasure ON chat_post;
DROP FUNCTION chatvoice_erase_transcript();
DROP TABLE chat_voice_transcript;
