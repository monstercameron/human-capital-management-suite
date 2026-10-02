-- +goose Up
-- Ambient agents have no read authority until a channel administrator opts in.
CREATE TABLE agentux_ambient_grant (
    tenant_id text NOT NULL, conversation_id text NOT NULL, agent_id text NOT NULL,
    enabled boolean NOT NULL DEFAULT false, automatic_public boolean NOT NULL DEFAULT false,
    installation_version bigint NOT NULL DEFAULT 0,
    conversation_limit integer NOT NULL DEFAULT 25 CHECK(conversation_limit BETWEEN 1 AND 500),
    daily_limit integer NOT NULL DEFAULT 100 CHECK(daily_limit BETWEEN 1 AND 2000),
    updated_by text NOT NULL, enabled_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY(tenant_id,conversation_id,agent_id),
    FOREIGN KEY(tenant_id,conversation_id) REFERENCES chat_conversation(tenant_id,id)
);
CREATE TABLE agentux_ambient_optout (
    tenant_id text NOT NULL, conversation_id text NOT NULL, person_id text NOT NULL,
    opted_out boolean NOT NULL DEFAULT false,
    PRIMARY KEY(tenant_id,conversation_id,person_id)
);
-- This run journal records exactly which source revision and parent were read.
CREATE TABLE agentux_ambient_read (
    tenant_id text NOT NULL, conversation_id text NOT NULL, agent_id text NOT NULL,
    post_id text NOT NULL, revision bigint NOT NULL, parent_id text NOT NULL DEFAULT '',
    stage text NOT NULL DEFAULT 'READ' CHECK(stage IN ('READ','RESULT')),
    source_kind text NOT NULL DEFAULT 'CHAT_MESSAGE' CHECK(source_kind='CHAT_MESSAGE'),
    at timestamptz NOT NULL, outcome text NOT NULL,
    PRIMARY KEY(tenant_id,agent_id,post_id,revision,stage)
);
CREATE TABLE agentux_ambient_offer (
    tenant_id text NOT NULL, id text NOT NULL, conversation_id text NOT NULL,
    source_id text NOT NULL, source_revision bigint NOT NULL, agent_id text NOT NULL,
    kind text NOT NULL CHECK(kind IN ('TASK','REMINDER')),
    person_id text NOT NULL DEFAULT '',
    scope text NOT NULL CHECK(scope IN ('PRIVATE','PUBLIC')),
    state text NOT NULL DEFAULT 'OFFERED', reason text NOT NULL,
    title text NOT NULL, data jsonb NOT NULL, revision bigint NOT NULL DEFAULT 1,
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY(tenant_id,id), UNIQUE(tenant_id,source_id,agent_id),
    CHECK((scope='PRIVATE' AND person_id<>'') OR (scope='PUBLIC' AND person_id=''))
);
CREATE TABLE agentux_ambient_task (
    tenant_id text NOT NULL, id text NOT NULL, person_id text NOT NULL,
    conversation_id text NOT NULL, source_id text NOT NULL,
    title text NOT NULL, due_at timestamptz, completed boolean NOT NULL DEFAULT false,
    PRIMARY KEY(tenant_id,id)
);
-- +goose StatementBegin
DO $$ DECLARE t text; role_name text; BEGIN
    FOREACH t IN ARRAY ARRAY['agentux_ambient_grant','agentux_ambient_optout','agentux_ambient_read','agentux_ambient_offer','agentux_ambient_task'] LOOP
        EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY',t);
        EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY',t);
        EXECUTE format('CREATE POLICY tenant_isolation ON %I USING (tenant_id=current_setting(''hcmnext.tenant_id'',true)) WITH CHECK (tenant_id=current_setting(''hcmnext.tenant_id'',true))',t);
        FOR role_name IN SELECT DISTINCT grantee FROM information_schema.role_table_grants WHERE table_schema=current_schema() AND table_name='chat_post' AND privilege_type='SELECT' AND grantee<>'PUBLIC' LOOP
            IF t='agentux_ambient_read' THEN EXECUTE format('GRANT SELECT,INSERT ON %I TO %I',t,role_name);
            ELSE EXECUTE format('GRANT SELECT,INSERT,UPDATE,DELETE ON %I TO %I',t,role_name); END IF;
        END LOOP;
    END LOOP;
END $$;
-- +goose StatementEnd
CREATE TRIGGER forbid_mutation BEFORE UPDATE OR DELETE ON agentux_ambient_read FOR EACH ROW EXECUTE FUNCTION chat_forbid_mutation();
CREATE INDEX agentux_ambient_read_budget ON agentux_ambient_read(tenant_id,agent_id,at);
CREATE INDEX agentux_ambient_offer_reader ON agentux_ambient_offer(tenant_id,conversation_id,person_id,state);
-- +goose Down
DROP TABLE agentux_ambient_task;
DROP TABLE agentux_ambient_offer;
DROP TABLE agentux_ambient_read;
DROP TABLE agentux_ambient_optout;
DROP TABLE agentux_ambient_grant;
