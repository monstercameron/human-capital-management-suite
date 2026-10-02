-- +goose Up
-- A poll posted as a message keeps its ballots beside the post. Counts shown to
-- readers are always computed from these rows, never from the numbers written
-- in the message body, so a body sent by hand cannot claim votes nobody cast.
--
-- A named poll has one row per voter and chosen option. An anonymous poll has
-- one row per voter with an empty option: it records that the person voted and
-- nothing else, and the choice is added to chat_post_card_tally, which holds no
-- voter. Nobody, including someone reading the database, can tie an anonymous
-- choice to a person.
CREATE TABLE chat_post_card_vote (
    tenant_id text NOT NULL,
    conversation_id text NOT NULL,
    post_id text NOT NULL REFERENCES chat_post(id) ON DELETE CASCADE,
    home_tenant_id text NOT NULL,
    subject_id text NOT NULL,
    option_id text NOT NULL DEFAULT '',
    voted_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, post_id, home_tenant_id, subject_id, option_id)
);
CREATE INDEX chat_post_card_vote_option_idx ON chat_post_card_vote(tenant_id, post_id, option_id);
ALTER TABLE chat_post_card_vote ENABLE ROW LEVEL SECURITY;
ALTER TABLE chat_post_card_vote FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON chat_post_card_vote
    USING (tenant_id = current_setting('hcmnext.tenant_id', true))
    WITH CHECK (tenant_id = current_setting('hcmnext.tenant_id', true));

CREATE TABLE chat_post_card_tally (
    tenant_id text NOT NULL,
    conversation_id text NOT NULL,
    post_id text NOT NULL REFERENCES chat_post(id) ON DELETE CASCADE,
    option_id text NOT NULL,
    votes integer NOT NULL DEFAULT 0 CHECK (votes >= 0),
    PRIMARY KEY (tenant_id, post_id, option_id)
);
ALTER TABLE chat_post_card_tally ENABLE ROW LEVEL SECURITY;
ALTER TABLE chat_post_card_tally FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON chat_post_card_tally
    USING (tenant_id = current_setting('hcmnext.tenant_id', true))
    WITH CHECK (tenant_id = current_setting('hcmnext.tenant_id', true));

-- The ballots are part of the message: deleting the message removes them in the
-- same statement. (A purged post row removes them through the foreign key.)
-- +goose StatementBegin
CREATE FUNCTION chat_post_card_remove_votes() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 DELETE FROM chat_post_card_vote WHERE tenant_id=OLD.tenant_id AND post_id=OLD.id;
 DELETE FROM chat_post_card_tally WHERE tenant_id=OLD.tenant_id AND post_id=OLD.id;
 RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER chat_post_card_remove_votes AFTER UPDATE OF tombstoned ON chat_post
    FOR EACH ROW WHEN (NEW.tombstoned AND NOT OLD.tombstoned) EXECUTE FUNCTION chat_post_card_remove_votes();

-- The channel's standing poll can be closed by the person who started it or a
-- channel manager, which ends it and lets the channel hold a new one. The
-- close is recorded in the poll's audit log like a create: it names no voter.
ALTER TABLE chat_channel_poll_revision
    DROP CONSTRAINT chat_channel_poll_revision_operation_check,
    DROP CONSTRAINT chat_channel_poll_revision_vote_delta_check,
    ADD CONSTRAINT chat_channel_poll_revision_operation_check CHECK (operation IN ('CREATE', 'VOTE', 'CLOSE')),
    ADD CONSTRAINT chat_channel_poll_revision_vote_delta_check CHECK (
        (operation IN ('CREATE', 'CLOSE') AND vote_home_tenant_id IS NULL AND vote_subject_id IS NULL AND prior_option_id = '' AND option_id = '') OR
        (operation = 'VOTE' AND vote_home_tenant_id <> '' AND vote_subject_id <> '' AND option_id <> '')
    );

-- Changing a vote deletes the choice it replaces, closing the channel poll
-- deletes its ballots, and the trigger above deletes as the caller. A serving
-- role holds only the schema's default table privileges (SELECT, INSERT,
-- UPDATE), so each role that may write posts is given what these statements
-- need (row level security still scopes them to the caller's tenant).
-- +goose StatementBegin
DO $$ DECLARE role_name text; card_table text; BEGIN
    FOR role_name IN SELECT DISTINCT grantee FROM information_schema.role_table_grants
      WHERE table_schema=current_schema() AND table_name='chat_post' AND privilege_type='UPDATE' AND grantee<>'PUBLIC'
    LOOP
      FOREACH card_table IN ARRAY ARRAY['chat_post_card_vote','chat_post_card_tally'] LOOP
        EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON %I TO %I',card_table,role_name);
      END LOOP;
      EXECUTE format('GRANT DELETE ON chat_channel_poll_vote TO %I',role_name);
    END LOOP;
END $$;
-- +goose StatementEnd

-- +goose Down
-- The widened checks on chat_channel_poll_revision stay: its rows cannot be
-- removed, and a recorded close would fail the narrower ones.
DROP TRIGGER chat_post_card_remove_votes ON chat_post;
DROP FUNCTION chat_post_card_remove_votes();
DROP TABLE chat_post_card_tally;
DROP TABLE chat_post_card_vote;
