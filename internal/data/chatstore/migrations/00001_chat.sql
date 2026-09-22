-- Chat owns this migration tree. It is intentionally not part of /migrations.
-- +goose Up
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION chat_forbid_mutation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'chat append-only relation % cannot be changed', TG_TABLE_NAME; END $$;
-- +goose StatementEnd

CREATE TABLE chat_conversation (
    id text PRIMARY KEY, tenant_id text NOT NULL, kind text NOT NULL, name text NOT NULL DEFAULT '',
    description text NOT NULL DEFAULT '', owner_id text NOT NULL, settings_revision bigint NOT NULL DEFAULT 1,
    lifecycle text NOT NULL DEFAULT 'ACTIVE', route_shard text NOT NULL DEFAULT '', route_epoch bigint NOT NULL DEFAULT 1, route_state text NOT NULL DEFAULT 'ACTIVE', created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, id)
);
CREATE TABLE chat_membership (
    tenant_id text NOT NULL, conversation_id text NOT NULL, member_id text NOT NULL, home_tenant_id text NOT NULL DEFAULT '',
    role text NOT NULL DEFAULT 'member', state text NOT NULL DEFAULT 'active', history_visibility text NOT NULL DEFAULT 'FULL_HISTORY',
    revision bigint NOT NULL DEFAULT 1, joined_at timestamptz NOT NULL DEFAULT now(), left_at timestamptz,
    PRIMARY KEY (tenant_id, conversation_id, home_tenant_id, member_id),
    FOREIGN KEY (tenant_id, conversation_id) REFERENCES chat_conversation(tenant_id, id) ON DELETE CASCADE
);
CREATE TABLE chat_conversation_idempotency (
    tenant_id text NOT NULL, owner_id text NOT NULL, client_key text NOT NULL,
    conversation_id text NOT NULL, fingerprint text NOT NULL, created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, owner_id, client_key),
    FOREIGN KEY (tenant_id, conversation_id) REFERENCES chat_conversation(tenant_id, id) ON DELETE CASCADE
);
CREATE TABLE chat_post (
    id text PRIMARY KEY, tenant_id text NOT NULL, conversation_id text NOT NULL, author_id text NOT NULL, author_home_tenant_id text NOT NULL DEFAULT '',
    sequence bigint NOT NULL, body text NOT NULL, parent_id text NOT NULL DEFAULT '', references_json jsonb NOT NULL DEFAULT '[]'::jsonb, source_attribution jsonb, client_key text NOT NULL DEFAULT '',
    revision bigint NOT NULL DEFAULT 1, tombstoned boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, conversation_id, sequence),
    FOREIGN KEY (tenant_id, conversation_id) REFERENCES chat_conversation(tenant_id, id) ON DELETE CASCADE
);
CREATE TABLE chat_post_revision (
    id bigserial PRIMARY KEY, tenant_id text NOT NULL, post_id text NOT NULL, revision bigint NOT NULL,
    author_id text NOT NULL, body text NOT NULL, parent_id text NOT NULL DEFAULT '', references_json jsonb NOT NULL DEFAULT '[]'::jsonb, source_attribution jsonb, tombstoned boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now(), UNIQUE (tenant_id, post_id, revision)
);
CREATE TABLE chat_idempotency (
    tenant_id text NOT NULL, conversation_id text NOT NULL, client_key text NOT NULL,
    fingerprint text NOT NULL, post_id text NOT NULL, sequence bigint NOT NULL, created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, conversation_id, client_key)
);
CREATE TABLE chat_outbox (
    id bigserial PRIMARY KEY, tenant_id text NOT NULL, aggregate_id text NOT NULL, event_type text NOT NULL,
    payload jsonb NOT NULL, created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, aggregate_id, event_type, created_at)
);
CREATE TABLE chat_outbox_receipt (
    tenant_id text NOT NULL, outbox_id bigint NOT NULL, published_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, outbox_id), FOREIGN KEY (outbox_id) REFERENCES chat_outbox(id) ON DELETE CASCADE
);
CREATE TABLE chat_preference (
    tenant_id text NOT NULL, home_tenant_id text NOT NULL DEFAULT '', member_id text NOT NULL, conversation_id text NOT NULL,
    marker text NOT NULL DEFAULT '', value jsonb NOT NULL DEFAULT '{}'::jsonb, revision bigint NOT NULL DEFAULT 1,
    updated_at timestamptz NOT NULL DEFAULT now(), PRIMARY KEY (tenant_id, home_tenant_id, member_id, conversation_id, marker)
);
CREATE TABLE chat_reaction (
    tenant_id text NOT NULL, home_tenant_id text NOT NULL DEFAULT '', post_id text NOT NULL, member_id text NOT NULL, emoji text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(), PRIMARY KEY (tenant_id, home_tenant_id, post_id, member_id, emoji)
);
CREATE TABLE chat_pin (
    tenant_id text NOT NULL, home_tenant_id text NOT NULL DEFAULT '', conversation_id text NOT NULL, post_id text NOT NULL, member_id text NOT NULL,
    revision bigint NOT NULL DEFAULT 1, created_at timestamptz NOT NULL DEFAULT now(), PRIMARY KEY (tenant_id, home_tenant_id, conversation_id, post_id, member_id)
);
CREATE TABLE chat_cursor (
    tenant_id text NOT NULL, home_tenant_id text NOT NULL DEFAULT '', member_id text NOT NULL, conversation_id text NOT NULL, last_sequence bigint NOT NULL DEFAULT 0,
    revision bigint NOT NULL DEFAULT 1, updated_at timestamptz NOT NULL DEFAULT now(), PRIMARY KEY (tenant_id, home_tenant_id, member_id, conversation_id)
);

CREATE INDEX chat_post_history ON chat_post(tenant_id, conversation_id, sequence DESC);
CREATE INDEX chat_outbox_pending ON chat_outbox(tenant_id, id);
CREATE INDEX chat_post_search ON chat_post USING gin (to_tsvector('simple', body));

CREATE TRIGGER chat_post_revision_immutable BEFORE UPDATE OR DELETE ON chat_post_revision FOR EACH ROW EXECUTE FUNCTION chat_forbid_mutation();
CREATE TRIGGER chat_outbox_immutable BEFORE UPDATE OR DELETE ON chat_outbox FOR EACH ROW EXECUTE FUNCTION chat_forbid_mutation();
CREATE TRIGGER chat_outbox_receipt_immutable BEFORE UPDATE OR DELETE ON chat_outbox_receipt FOR EACH ROW EXECUTE FUNCTION chat_forbid_mutation();

-- +goose StatementBegin
DO $$ DECLARE t text; BEGIN
  FOREACH t IN ARRAY ARRAY['chat_conversation','chat_conversation_idempotency','chat_membership','chat_post','chat_post_revision','chat_idempotency','chat_outbox','chat_outbox_receipt','chat_preference','chat_reaction','chat_pin','chat_cursor'] LOOP
    EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
    EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', t);
    EXECUTE format('CREATE POLICY tenant_isolation ON %I USING (tenant_id = current_setting(''hcmnext.tenant_id'', true)) WITH CHECK (tenant_id = current_setting(''hcmnext.tenant_id'', true))', t);
  END LOOP;
END $$;
-- +goose StatementEnd

-- +goose Down
DROP TABLE chat_cursor, chat_pin, chat_reaction, chat_preference, chat_outbox_receipt, chat_outbox, chat_idempotency, chat_conversation_idempotency,
 chat_post_revision, chat_post, chat_membership, chat_conversation CASCADE;
DROP FUNCTION chat_forbid_mutation();
