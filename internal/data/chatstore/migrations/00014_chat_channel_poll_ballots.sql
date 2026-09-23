-- +goose Up
CREATE TABLE chat_channel_poll_vote (
    tenant_id text NOT NULL,
    conversation_id text NOT NULL,
    home_tenant_id text NOT NULL,
    subject_id text NOT NULL,
    option_id text NOT NULL,
    voted_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, conversation_id, home_tenant_id, subject_id),
    FOREIGN KEY (tenant_id, conversation_id) REFERENCES chat_channel_poll(tenant_id, conversation_id) ON DELETE CASCADE
);
CREATE INDEX chat_channel_poll_vote_option_idx ON chat_channel_poll_vote(tenant_id, conversation_id, option_id);
ALTER TABLE chat_channel_poll_vote ENABLE ROW LEVEL SECURITY;
ALTER TABLE chat_channel_poll_vote FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON chat_channel_poll_vote
    USING (tenant_id = current_setting('hcmnext.tenant_id', true))
    WITH CHECK (tenant_id = current_setting('hcmnext.tenant_id', true));

-- Upgrade ballots already written by migration 00013 before dropping their
-- serialized representation. A duplicate voter fails the primary key rather
-- than silently losing a ballot.
INSERT INTO chat_channel_poll_vote(tenant_id,conversation_id,home_tenant_id,subject_id,option_id)
SELECT p.tenant_id,p.conversation_id,v.home_tenant_id,v.subject_id,v.option_id
FROM chat_channel_poll p
CROSS JOIN LATERAL jsonb_to_recordset(p.votes_json) AS v(home_tenant_id text,subject_id text,option_id text);

-- Replace full prior/current ballots in each immutable history row with the
-- one vote delta performed by the recorded actor. The DDL and backfill run in
-- Goose's transaction, so the append-only trigger is restored atomically.
DROP TRIGGER chat_channel_poll_revision_immutable ON chat_channel_poll_revision;
ALTER TABLE chat_channel_poll_revision
    ADD COLUMN vote_home_tenant_id text,
    ADD COLUMN vote_subject_id text,
    ADD COLUMN prior_option_id text NOT NULL DEFAULT '',
    ADD COLUMN option_id text NOT NULL DEFAULT '';
UPDATE chat_channel_poll_revision AS r
SET vote_home_tenant_id = r.actor_home_tenant_id,
    vote_subject_id = r.actor_id,
    prior_option_id = COALESCE((
        SELECT v.option_id
        FROM jsonb_to_recordset(r.prior_votes_json) AS v(home_tenant_id text,subject_id text,option_id text)
        WHERE v.home_tenant_id = r.actor_home_tenant_id AND v.subject_id = r.actor_id
        LIMIT 1
    ), ''),
    option_id = COALESCE((
        SELECT v.option_id
        FROM jsonb_to_recordset(r.votes_json) AS v(home_tenant_id text,subject_id text,option_id text)
        WHERE v.home_tenant_id = r.actor_home_tenant_id AND v.subject_id = r.actor_id
        LIMIT 1
    ), '')
WHERE r.operation = 'VOTE';
ALTER TABLE chat_channel_poll_revision
    ADD CONSTRAINT chat_channel_poll_revision_vote_delta_check CHECK (
        (operation = 'CREATE' AND vote_home_tenant_id IS NULL AND vote_subject_id IS NULL AND prior_option_id = '' AND option_id = '') OR
        (operation = 'VOTE' AND vote_home_tenant_id <> '' AND vote_subject_id <> '' AND option_id <> '')
    ),
    DROP COLUMN prior_votes_json,
    DROP COLUMN votes_json;
CREATE TRIGGER chat_channel_poll_revision_immutable BEFORE UPDATE OR DELETE ON chat_channel_poll_revision
    FOR EACH ROW EXECUTE FUNCTION chat_forbid_mutation();

ALTER TABLE chat_channel_poll DROP COLUMN votes_json;

-- +goose Down
ALTER TABLE chat_channel_poll ADD COLUMN votes_json jsonb NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(votes_json) = 'array');
UPDATE chat_channel_poll p SET votes_json = COALESCE((
    SELECT jsonb_agg(jsonb_build_object('home_tenant_id',v.home_tenant_id,'subject_id',v.subject_id,'option_id',v.option_id))
    FROM chat_channel_poll_vote v
    WHERE v.tenant_id=p.tenant_id AND v.conversation_id=p.conversation_id
), '[]'::jsonb);

DROP TRIGGER chat_channel_poll_revision_immutable ON chat_channel_poll_revision;
ALTER TABLE chat_channel_poll_revision
    ADD COLUMN prior_votes_json jsonb NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN votes_json jsonb NOT NULL DEFAULT '[]'::jsonb;
-- Reconstruct the legacy snapshots from the compact ordered vote deltas. This
-- can reproduce the old (larger) format without making the up migration retain
-- quadratic data indefinitely.
-- +goose StatementBegin
DO $$
DECLARE
    audit_row record;
    prior_ballot jsonb;
    current_ballot jsonb;
BEGIN
    CREATE TEMP TABLE chat_channel_poll_rollback_state (
        tenant_id text NOT NULL,
        conversation_id text NOT NULL,
        votes_json jsonb NOT NULL,
        PRIMARY KEY (tenant_id,conversation_id)
    ) ON COMMIT DROP;

    FOR audit_row IN
        SELECT * FROM chat_channel_poll_revision ORDER BY tenant_id,conversation_id,revision
    LOOP
        IF audit_row.operation = 'CREATE' THEN
            prior_ballot := '[]'::jsonb;
            current_ballot := '[]'::jsonb;
        ELSE
            SELECT votes_json INTO prior_ballot
            FROM chat_channel_poll_rollback_state
            WHERE tenant_id=audit_row.tenant_id AND conversation_id=audit_row.conversation_id;
            IF NOT FOUND THEN
                prior_ballot := '[]'::jsonb;
            END IF;
            SELECT COALESCE(jsonb_agg(v.value),'[]'::jsonb) INTO current_ballot
            FROM jsonb_array_elements(prior_ballot) AS v(value)
            WHERE v.value->>'home_tenant_id' <> audit_row.vote_home_tenant_id
               OR v.value->>'subject_id' <> audit_row.vote_subject_id;
            current_ballot := current_ballot || jsonb_build_array(jsonb_build_object(
                'home_tenant_id',audit_row.vote_home_tenant_id,
                'subject_id',audit_row.vote_subject_id,
                'option_id',audit_row.option_id
            ));
        END IF;

        UPDATE chat_channel_poll_revision
        SET prior_votes_json=prior_ballot,votes_json=current_ballot
        WHERE tenant_id=audit_row.tenant_id AND conversation_id=audit_row.conversation_id AND revision=audit_row.revision;
        INSERT INTO chat_channel_poll_rollback_state(tenant_id,conversation_id,votes_json)
        VALUES(audit_row.tenant_id,audit_row.conversation_id,current_ballot)
        ON CONFLICT(tenant_id,conversation_id) DO UPDATE SET votes_json=EXCLUDED.votes_json;
    END LOOP;
END $$;
-- +goose StatementEnd
ALTER TABLE chat_channel_poll_revision
    DROP CONSTRAINT chat_channel_poll_revision_vote_delta_check,
    DROP COLUMN vote_home_tenant_id,
    DROP COLUMN vote_subject_id,
    DROP COLUMN prior_option_id,
    DROP COLUMN option_id;
CREATE TRIGGER chat_channel_poll_revision_immutable BEFORE UPDATE OR DELETE ON chat_channel_poll_revision
    FOR EACH ROW EXECUTE FUNCTION chat_forbid_mutation();

DROP POLICY tenant_isolation ON chat_channel_poll_vote;
DROP TABLE chat_channel_poll_vote;
