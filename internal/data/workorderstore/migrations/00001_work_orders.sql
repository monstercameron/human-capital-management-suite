-- +goose Up
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION workorder_forbid_mutation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'work order append-only relation % cannot be changed', TG_TABLE_NAME; END $$;
-- +goose StatementEnd

CREATE TABLE work_order (
 tenant_id text NOT NULL, id text NOT NULL, project_id text NOT NULL,
 status text NOT NULL, phase_id text NOT NULL, revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
 template_id text NOT NULL, template_version text NOT NULL CHECK (template_version <> ''),
 payload jsonb NOT NULL DEFAULT '{}'::jsonb, created_by text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY (tenant_id,id)
);
CREATE INDEX work_order_project_page ON work_order(tenant_id, project_id, created_at, id);

CREATE TABLE work_order_record (
 tenant_id text NOT NULL, id text NOT NULL, work_order_id text NOT NULL,
 sequence bigint NOT NULL CHECK (sequence > 0), revision bigint NOT NULL CHECK (revision > 0),
 kind text NOT NULL,
 actor_id text NOT NULL, idempotency_key text NOT NULL, payload jsonb NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,id), UNIQUE(tenant_id,work_order_id,sequence),
 UNIQUE(tenant_id,work_order_id,kind,actor_id,idempotency_key),
 FOREIGN KEY(tenant_id,work_order_id) REFERENCES work_order(tenant_id,id)
);
CREATE INDEX work_order_record_history ON work_order_record(tenant_id,work_order_id,sequence);
CREATE TRIGGER work_order_record_immutable BEFORE UPDATE OR DELETE ON work_order_record FOR EACH ROW EXECUTE FUNCTION workorder_forbid_mutation();

CREATE TABLE work_order_template_version (
 tenant_id text NOT NULL, template_id text NOT NULL, version text NOT NULL CHECK(version <> ''),
 digest text NOT NULL, payload jsonb NOT NULL, published_by text NOT NULL,
 published_at timestamptz NOT NULL DEFAULT now(), PRIMARY KEY(tenant_id,template_id,version)
);
CREATE TRIGGER work_order_template_immutable BEFORE UPDATE OR DELETE ON work_order_template_version FOR EACH ROW EXECUTE FUNCTION workorder_forbid_mutation();

CREATE TABLE work_order_outbox (
 tenant_id text NOT NULL, id text NOT NULL, work_order_id text NOT NULL, sequence bigint NOT NULL,
 event_type text NOT NULL, schema_version integer NOT NULL CHECK(schema_version > 0), payload jsonb NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(), PRIMARY KEY(tenant_id,id),
 UNIQUE(tenant_id,work_order_id,sequence), FOREIGN KEY(tenant_id,work_order_id) REFERENCES work_order(tenant_id,id)
);
CREATE TRIGGER work_order_outbox_immutable BEFORE UPDATE OR DELETE ON work_order_outbox FOR EACH ROW EXECUTE FUNCTION workorder_forbid_mutation();

CREATE TABLE work_order_idempotency (
 tenant_id text NOT NULL, work_order_id text NOT NULL, actor_id text NOT NULL,
 client_key text NOT NULL, command_digest text NOT NULL, result_snapshot jsonb NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,work_order_id,actor_id,client_key),
 FOREIGN KEY(tenant_id,work_order_id) REFERENCES work_order(tenant_id,id)
);
CREATE TRIGGER work_order_idempotency_immutable BEFORE UPDATE OR DELETE ON work_order_idempotency FOR EACH ROW EXECUTE FUNCTION workorder_forbid_mutation();

CREATE TABLE work_order_artifact (
 tenant_id text NOT NULL, id text NOT NULL, work_order_id text NOT NULL, project_id text NOT NULL,
 kind text NOT NULL CHECK(kind IN ('REPORT','BILLING')), actor_id text NOT NULL,
 idempotency_key text NOT NULL, command_digest text NOT NULL,
 source_revision bigint NOT NULL CHECK(source_revision > 0), payload jsonb NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(), PRIMARY KEY(tenant_id,id),
 UNIQUE(tenant_id,work_order_id,kind,actor_id,idempotency_key),
 FOREIGN KEY(tenant_id,work_order_id) REFERENCES work_order(tenant_id,id)
);
CREATE TRIGGER work_order_artifact_immutable BEFORE UPDATE OR DELETE ON work_order_artifact FOR EACH ROW EXECUTE FUNCTION workorder_forbid_mutation();

-- +goose StatementBegin
DO $$ DECLARE t text; BEGIN
 FOREACH t IN ARRAY ARRAY['work_order','work_order_record','work_order_template_version','work_order_outbox','work_order_idempotency','work_order_artifact'] LOOP
  EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY',t);
  EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY',t);
  EXECUTE format('CREATE POLICY tenant_isolation ON %I USING (tenant_id = current_setting(''hcmnext.tenant_id'', true)) WITH CHECK (tenant_id = current_setting(''hcmnext.tenant_id'', true))',t);
 END LOOP;
END $$;
-- +goose StatementEnd

-- +goose Down
DROP TABLE work_order_artifact, work_order_idempotency, work_order_outbox, work_order_template_version, work_order_record, work_order CASCADE;
DROP FUNCTION workorder_forbid_mutation();
