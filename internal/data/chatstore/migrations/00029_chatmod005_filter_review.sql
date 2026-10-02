-- +goose Up
-- CHATMOD-005: a filter hit flagged for review is an open item in the moderation
-- queue. chat_filter_hit is immutable history, so the decision on a hit lives in
-- this table: one row per decided hit, written once, with who decided, what and
-- why. A hit with no row is still open.
CREATE TABLE chat_filter_hit_review (
    tenant_id text NOT NULL,
    hit_id bigint NOT NULL,
    conversation_id text NOT NULL,
    post_id text NOT NULL DEFAULT '',
    decision text NOT NULL CHECK (decision IN ('remove','dismiss','restore','message_author')),
    reason text NOT NULL,
    decided_by text NOT NULL,
    decided_at timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, hit_id)
);
ALTER TABLE chat_filter_hit_review ENABLE ROW LEVEL SECURITY;
ALTER TABLE chat_filter_hit_review FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON chat_filter_hit_review USING (tenant_id=current_setting('hcmnext.tenant_id',true)) WITH CHECK (tenant_id=current_setting('hcmnext.tenant_id',true));
CREATE TRIGGER forbid_mutation BEFORE UPDATE OR DELETE ON chat_filter_hit_review FOR EACH ROW EXECUTE FUNCTION chat_forbid_mutation();
-- +goose StatementBegin
DO $$ DECLARE role_name text; BEGIN
  FOR role_name IN SELECT DISTINCT grantee FROM information_schema.role_table_grants
    WHERE table_schema=current_schema() AND table_name='chat_filter_hit' AND privilege_type='SELECT' AND grantee<>'PUBLIC'
  LOOP
    EXECUTE format('GRANT SELECT,INSERT ON chat_filter_hit_review TO %I',role_name);
  END LOOP;
END $$;
-- +goose StatementEnd

-- +goose Down
DROP TABLE chat_filter_hit_review;
