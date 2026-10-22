-- +goose Up
-- Agent reactions to questions (AGENTUX-052): whether agents may react.
-- conversation_id is empty for the workspace row; a conversation row, when
-- present, wins over the workspace row. Absent rows mean enabled.
CREATE TABLE chat_agent_reaction_policy (
 tenant_id text NOT NULL,
 conversation_id text NOT NULL DEFAULT '',
 reactions_enabled boolean NOT NULL DEFAULT true,
 updated_by text NOT NULL DEFAULT '',
 updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY (tenant_id, conversation_id)
);

ALTER TABLE chat_agent_reaction_policy ENABLE ROW LEVEL SECURITY;
ALTER TABLE chat_agent_reaction_policy FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON chat_agent_reaction_policy
 USING (tenant_id=current_setting('hcmnext.tenant_id',true))
 WITH CHECK (tenant_id=current_setting('hcmnext.tenant_id',true));

-- The new table gets the same grants as the neighboring settings tables
-- (pattern: migrations 00028, 00039 and 00045).
-- +goose StatementBegin
DO $$ DECLARE grant_row record; BEGIN
 FOR grant_row IN SELECT grantee,privilege_type FROM information_schema.role_table_grants
  WHERE table_schema=current_schema() AND table_name='chat_location_policy' AND privilege_type IN ('SELECT','INSERT','UPDATE','DELETE') AND grantee<>'PUBLIC'
 LOOP
  EXECUTE format('GRANT %s ON chat_agent_reaction_policy TO %I',grant_row.privilege_type,grant_row.grantee);
 END LOOP;
END $$;
-- +goose StatementEnd

-- +goose Down
DROP TABLE chat_agent_reaction_policy;
