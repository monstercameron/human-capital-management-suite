-- +goose Up
-- Issue 3786: the personal digest. A person chooses whether they get a digest
-- of what was held back from them (mentions that arrived in quiet hours or in a
-- muted conversation, and followed threads that moved) and when. This table
-- holds only that choice and the moment the last digest was seen. It holds no
-- message text and no message ids: the digest itself is worked out when it is
-- read, from messages the person can read right then.
CREATE TABLE chat_digest_pref (
    tenant_id text NOT NULL,
    member_id text NOT NULL,
    cadence text NOT NULL DEFAULT 'off' CHECK (cadence IN ('off','daily','quiet_end')),
    minute integer NOT NULL DEFAULT 480 CHECK (minute BETWEEN 0 AND 1439),
    timezone text NOT NULL DEFAULT 'UTC',
    last_digest_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, member_id)
);
ALTER TABLE chat_digest_pref ENABLE ROW LEVEL SECURITY;
ALTER TABLE chat_digest_pref FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON chat_digest_pref
    USING (tenant_id = current_setting('hcmnext.tenant_id', true))
    WITH CHECK (tenant_id = current_setting('hcmnext.tenant_id', true));

-- +goose Down
DROP TABLE chat_digest_pref;
