-- Project owns its schema, role and bounded pool while sharing the core
-- PostgreSQL instance. Membership and richer task/configuration state are
-- added by their owning project todos.
-- +goose Up
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION project_forbid_mutation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'project append-only relation % cannot be changed', TG_TABLE_NAME; END $$;
-- +goose StatementEnd

CREATE TABLE project (
    tenant_id text NOT NULL,
    id text NOT NULL,
    owner_id text NOT NULL,
    name text NOT NULL,
    project_timezone text NOT NULL,
    lifecycle text NOT NULL DEFAULT 'ACTIVE' CHECK (lifecycle IN ('ACTIVE','SUSPENDED','ARCHIVED')),
    revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
    event_sequence bigint NOT NULL DEFAULT 0 CHECK (event_sequence >= 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, id)
);

CREATE TABLE project_task (
    tenant_id text NOT NULL,
    id text NOT NULL,
    project_id text NOT NULL,
    title text NOT NULL,
    description text NOT NULL DEFAULT '',
    status_id text NOT NULL,
    type_id text NOT NULL DEFAULT 'task_default',
    priority text NOT NULL DEFAULT 'NORMAL',
    fields_json jsonb NOT NULL DEFAULT '{}'::jsonb,
    assignee_id text NOT NULL DEFAULT '',
    due_date date,
    revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
    archived boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, id),
    FOREIGN KEY (tenant_id, project_id) REFERENCES project(tenant_id, id)
);
CREATE INDEX project_task_project_page ON project_task(tenant_id, project_id, archived, created_at, id);

CREATE TABLE project_activity (
    tenant_id text NOT NULL,
    id text NOT NULL,
    project_id text NOT NULL,
    aggregate_id text NOT NULL,
    actor_id text NOT NULL,
    origin text NOT NULL CHECK (origin IN ('HUMAN','APP','AGENT')),
    event_type text NOT NULL,
    prior_revision bigint NOT NULL DEFAULT 0 CHECK (prior_revision >= 0),
    new_revision bigint NOT NULL CHECK (new_revision > 0),
    config_version bigint NOT NULL DEFAULT 0 CHECK (config_version >= 0),
    classification text NOT NULL DEFAULT 'INTERNAL',
    payload jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, id),
    FOREIGN KEY (tenant_id, project_id) REFERENCES project(tenant_id, id)
);
CREATE INDEX project_activity_history ON project_activity(tenant_id, project_id, created_at, id);
CREATE TRIGGER project_activity_immutable BEFORE UPDATE OR DELETE ON project_activity FOR EACH ROW EXECUTE FUNCTION project_forbid_mutation();

CREATE TABLE project_outbox (
    id bigserial PRIMARY KEY,
    tenant_id text NOT NULL,
    project_id text NOT NULL,
    event_id text NOT NULL,
    project_sequence bigint NOT NULL CHECK (project_sequence > 0),
    event_type text NOT NULL,
    schema_version integer NOT NULL CHECK (schema_version > 0),
    source_revision bigint NOT NULL CHECK (source_revision > 0),
    classification text NOT NULL DEFAULT 'INTERNAL',
    payload jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, event_id),
    UNIQUE (tenant_id, project_id, project_sequence),
    FOREIGN KEY (tenant_id, project_id) REFERENCES project(tenant_id, id)
);
CREATE INDEX project_outbox_pending ON project_outbox(tenant_id, id);
CREATE INDEX project_outbox_retention ON project_outbox(tenant_id, project_id, created_at, project_sequence);

CREATE TABLE project_outbox_receipt (
    tenant_id text NOT NULL,
    outbox_id bigint NOT NULL,
    published_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, outbox_id),
    FOREIGN KEY (outbox_id) REFERENCES project_outbox(id) ON DELETE CASCADE
);

CREATE TABLE project_outbox_cursor (
    tenant_id text NOT NULL,
    project_id text NOT NULL,
    consumer text NOT NULL,
    last_sequence bigint NOT NULL DEFAULT 0 CHECK (last_sequence >= 0),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, project_id, consumer),
    FOREIGN KEY (tenant_id, project_id) REFERENCES project(tenant_id, id)
);

CREATE TABLE project_idempotency (
    tenant_id text NOT NULL,
    actor_id text NOT NULL,
    operation text NOT NULL,
    client_key text NOT NULL,
    fingerprint text NOT NULL,
    result_json jsonb NOT NULL DEFAULT 'null'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, actor_id, operation, client_key)
);

-- Outbox rows are immutable after insertion. The later retention todo may
-- delete only published events behind every consumer's cursor.
CREATE TRIGGER project_outbox_immutable BEFORE UPDATE OR DELETE ON project_outbox FOR EACH ROW EXECUTE FUNCTION project_forbid_mutation();
CREATE TRIGGER project_outbox_receipt_immutable BEFORE UPDATE OR DELETE ON project_outbox_receipt FOR EACH ROW EXECUTE FUNCTION project_forbid_mutation();

-- +goose StatementBegin
DO $$ DECLARE t text; BEGIN
  FOREACH t IN ARRAY ARRAY['project','project_task','project_activity','project_outbox','project_outbox_receipt','project_outbox_cursor','project_idempotency'] LOOP
    EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
    EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', t);
    EXECUTE format('CREATE POLICY tenant_isolation ON %I USING (tenant_id = current_setting(''hcmnext.tenant_id'', true)) WITH CHECK (tenant_id = current_setting(''hcmnext.tenant_id'', true))', t);
  END LOOP;
END $$;
-- +goose StatementEnd

-- +goose Down
DROP TABLE project_idempotency, project_outbox_cursor, project_outbox_receipt, project_outbox, project_activity, project_task, project CASCADE;
DROP FUNCTION project_forbid_mutation();
