-- +goose Up
-- Issue 3787: a person's own decision about one follow-up row (a message that
-- is waiting on them, or one they are waiting on). The follow-up list itself is
-- computed from existing posts at read time; this table only remembers what
-- the person chose, so a choice survives a refresh. It holds no message text,
-- and a row is shown only while the person can still read the message.
CREATE TABLE chat_followup_state (
    tenant_id text NOT NULL,
    home_tenant_id text NOT NULL,
    member_id text NOT NULL,
    conversation_id text NOT NULL,
    post_id text NOT NULL,
    direction text NOT NULL CHECK (direction IN ('owed','waiting')),
    status text NOT NULL CHECK (status IN ('dismissed','resolved','snoozed','kept')),
    snooze_until timestamptz,
    revision bigint NOT NULL DEFAULT 1,
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, home_tenant_id, member_id, conversation_id, post_id, direction),
    CHECK ((status = 'snoozed') = (snooze_until IS NOT NULL))
);
ALTER TABLE chat_followup_state ENABLE ROW LEVEL SECURITY;
ALTER TABLE chat_followup_state FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON chat_followup_state
    USING (tenant_id = current_setting('hcmnext.tenant_id', true))
    WITH CHECK (tenant_id = current_setting('hcmnext.tenant_id', true));

-- +goose Down
DROP TABLE chat_followup_state;
