-- +goose Up
CREATE TABLE chat_ephemeral_post (
    id text NOT NULL,
    tenant_id text NOT NULL,
    conversation_id text NOT NULL,
    stream_offset bigint NOT NULL UNIQUE,
    thread_id text NOT NULL,
    recipient_home_tenant_id text NOT NULL,
    recipient_subject_id text NOT NULL,
    body text NOT NULL,
    only_visible_to_you boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL,
    durable_copy_conversation_id text NOT NULL,
    durable_copy_post_id text NOT NULL,
    thread_link text NOT NULL,
    fingerprint text NOT NULL,
    PRIMARY KEY (tenant_id, conversation_id, id),
    FOREIGN KEY (tenant_id, conversation_id) REFERENCES chat_conversation(tenant_id, id) ON DELETE CASCADE,
    CONSTRAINT chat_ephemeral_post_expiry CHECK (
        only_visible_to_you AND expires_at > created_at AND expires_at <= created_at + interval '24 hours'
    ),
    CONSTRAINT chat_ephemeral_post_required CHECK (
        length(id) > 0 AND length(thread_id) > 0 AND length(recipient_home_tenant_id) > 0 AND
        length(recipient_subject_id) > 0 AND length(body) BETWEEN 1 AND 4000 AND
        length(durable_copy_conversation_id) > 0 AND length(durable_copy_post_id) > 0 AND
        length(thread_link) > 0 AND length(fingerprint) > 0
    )
);
CREATE INDEX chat_ephemeral_post_replay ON chat_ephemeral_post(tenant_id, conversation_id, stream_offset);
CREATE INDEX chat_ephemeral_post_expiry ON chat_ephemeral_post(tenant_id, expires_at);
ALTER TABLE chat_ephemeral_post ENABLE ROW LEVEL SECURITY;
ALTER TABLE chat_ephemeral_post FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON chat_ephemeral_post
    USING (tenant_id = current_setting('hcmnext.tenant_id', true))
    WITH CHECK (tenant_id = current_setting('hcmnext.tenant_id', true));

-- +goose Down
DROP TABLE chat_ephemeral_post;
