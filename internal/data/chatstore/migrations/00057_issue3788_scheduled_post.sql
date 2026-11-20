-- +goose Up
-- Issue 3788: a message its author chose to send later. The row is the
-- author's private draft until delivery: it is listed only to the author, it
-- is deleted when it is cancelled or sent, and delivery goes through the
-- ordinary Send service as the author, so membership, history, filter, DLP and
-- channel status are checked again at the moment of sending. A refused row stays
-- (state 'failed') with a short reason so only the author can read why.
CREATE TABLE chat_scheduled_post (
    tenant_id text NOT NULL,
    id text NOT NULL,
    home_tenant_id text NOT NULL,
    author_id text NOT NULL,
    org_scope_id text NOT NULL DEFAULT '',
    conversation_id text NOT NULL,
    body text NOT NULL CHECK (btrim(body) <> ''),
    send_at timestamptz NOT NULL,
    zone text NOT NULL DEFAULT 'UTC',
    state text NOT NULL DEFAULT 'pending' CHECK (state IN ('pending','sending','failed')),
    attempts integer NOT NULL DEFAULT 0,
    claim_until timestamptz,
    refusal text NOT NULL DEFAULT '',
    revision bigint NOT NULL DEFAULT 1,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, id)
);
CREATE INDEX chat_scheduled_post_author ON chat_scheduled_post (tenant_id, home_tenant_id, author_id, send_at);
CREATE INDEX chat_scheduled_post_due ON chat_scheduled_post (tenant_id, send_at) WHERE state IN ('pending','sending');
ALTER TABLE chat_scheduled_post ENABLE ROW LEVEL SECURITY;
ALTER TABLE chat_scheduled_post FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON chat_scheduled_post
    USING (tenant_id = current_setting('hcmnext.tenant_id', true))
    WITH CHECK (tenant_id = current_setting('hcmnext.tenant_id', true));

-- +goose Down
DROP TABLE chat_scheduled_post;
