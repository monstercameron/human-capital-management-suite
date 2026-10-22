-- +goose Up
-- CHATGATE-010 and CHATGATE-011: content addressed to a segment of a gate.
--
-- chat_post_audience says who a post is addressed to. A restricted post is read
-- only by the people in member_ids, which is the segment's members at the moment
-- the post was sent plus its author: the list is frozen with the post, so a
-- later change to a segment (someone joins it, rules change, someone withdraws)
-- never widens or narrows who may read it. An addressed post that is not
-- restricted is readable by every member and only names the segments it is for.
CREATE TABLE chat_post_audience (
    tenant_id text NOT NULL,
    conversation_id text NOT NULL,
    post_id text NOT NULL REFERENCES chat_post(id) ON DELETE CASCADE,
    author_id text NOT NULL,
    segments text[] NOT NULL CHECK (cardinality(segments) BETWEEN 1 AND 10),
    restricted boolean NOT NULL DEFAULT false,
    member_ids text[] NOT NULL DEFAULT '{}',
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, post_id)
);
CREATE INDEX chat_post_audience_restricted_idx ON chat_post_audience(tenant_id, conversation_id) WHERE restricted;
ALTER TABLE chat_post_audience ENABLE ROW LEVEL SECURITY;
ALTER TABLE chat_post_audience FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON chat_post_audience
    USING (tenant_id = current_setting('hcmnext.tenant_id', true))
    WITH CHECK (tenant_id = current_setting('hcmnext.tenant_id', true));
-- A post's audience is fixed when it is sent.
-- +goose StatementBegin
CREATE FUNCTION chat_post_audience_forbid_change() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 RAISE EXCEPTION 'a post audience is fixed when the post is sent' USING ERRCODE='42501';
END $$;
-- +goose StatementEnd
CREATE TRIGGER chat_post_audience_forbid_change BEFORE UPDATE ON chat_post_audience
    FOR EACH ROW EXECUTE FUNCTION chat_post_audience_forbid_change();

-- A poll addressed to segments keeps its tally by cell: the exact set of the
-- poll's segments the voter was in (signature, segment names joined by '|'), the
-- option chosen, and how many. An empty option_id row counts the voters of the
-- cell. It names nobody, and the overlap of segments is known exactly, which is
-- what lets a report by segment refuse a combination that would isolate a vote.
CREATE TABLE chat_post_card_segment_tally (
    tenant_id text NOT NULL,
    conversation_id text NOT NULL,
    post_id text NOT NULL REFERENCES chat_post(id) ON DELETE CASCADE,
    signature text NOT NULL,
    option_id text NOT NULL,
    votes integer NOT NULL DEFAULT 0 CHECK (votes >= 0),
    PRIMARY KEY (tenant_id, post_id, signature, option_id)
);
ALTER TABLE chat_post_card_segment_tally ENABLE ROW LEVEL SECURITY;
ALTER TABLE chat_post_card_segment_tally FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON chat_post_card_segment_tally
    USING (tenant_id = current_setting('hcmnext.tenant_id', true))
    WITH CHECK (tenant_id = current_setting('hcmnext.tenant_id', true));

-- Deleting a message removes its segment tally with the other ballots.
-- +goose StatementBegin
CREATE FUNCTION chat_post_card_remove_segment_tally() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 DELETE FROM chat_post_card_segment_tally WHERE tenant_id=OLD.tenant_id AND post_id=OLD.id;
 RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER chat_post_card_remove_segment_tally AFTER UPDATE OF tombstoned ON chat_post
    FOR EACH ROW WHEN (NEW.tombstoned AND NOT OLD.tombstoned) EXECUTE FUNCTION chat_post_card_remove_segment_tally();

-- The serving role holds only the schema's default table privileges (SELECT,
-- INSERT, UPDATE); the removal above deletes as the caller and the tally is
-- written by upsert, so every role that may write posts is given what these
-- statements need (row level security still scopes them to the caller's tenant).
-- +goose StatementBegin
DO $$ DECLARE role_name text; BEGIN
    FOR role_name IN SELECT DISTINCT grantee FROM information_schema.role_table_grants
      WHERE table_schema=current_schema() AND table_name='chat_post' AND privilege_type='UPDATE' AND grantee<>'PUBLIC'
    LOOP
      EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON chat_post_card_segment_tally TO %I',role_name);
    END LOOP;
END $$;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER chat_post_card_remove_segment_tally ON chat_post;
DROP FUNCTION chat_post_card_remove_segment_tally();
DROP TABLE chat_post_card_segment_tally;
DROP TRIGGER chat_post_audience_forbid_change ON chat_post_audience;
DROP FUNCTION chat_post_audience_forbid_change();
DROP TABLE chat_post_audience;
