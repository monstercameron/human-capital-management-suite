-- +goose Up
ALTER TABLE project ADD COLUMN classification_level smallint NOT NULL DEFAULT 0 CHECK (classification_level BETWEEN 0 AND 255);

CREATE TABLE project_membership_clock (
    tenant_id text NOT NULL,
    project_id text NOT NULL,
    revision bigint NOT NULL CHECK (revision > 0),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, project_id),
    FOREIGN KEY (tenant_id, project_id) REFERENCES project(tenant_id, id)
);

CREATE TABLE project_membership (
    tenant_id text NOT NULL,
    project_id text NOT NULL,
    user_id text NOT NULL,
    role text NOT NULL CHECK (role IN ('OWNER','MANAGER','CONTRIBUTOR','VIEWER')),
    state text NOT NULL CHECK (state IN ('INVITED','ACTIVE','REVOKED')),
    revision bigint NOT NULL CHECK (revision > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, project_id, user_id),
    FOREIGN KEY (tenant_id, project_id) REFERENCES project(tenant_id, id)
);
CREATE INDEX project_membership_user_current ON project_membership(tenant_id, user_id, project_id) WHERE state='ACTIVE';

CREATE TABLE project_membership_event (
    tenant_id text NOT NULL,
    project_id text NOT NULL,
    revision bigint NOT NULL CHECK (revision > 0),
    actor_id text NOT NULL,
    event_type text NOT NULL CHECK (event_type IN ('membership.invited','membership.accepted','membership.role_changed','membership.revoked','membership.ownership_transferred')),
    payload jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, project_id, revision),
    FOREIGN KEY (tenant_id, project_id) REFERENCES project(tenant_id, id)
);
CREATE TRIGGER project_membership_event_immutable BEFORE UPDATE OR DELETE ON project_membership_event FOR EACH ROW EXECUTE FUNCTION project_forbid_mutation();

CREATE TABLE project_membership_idempotency (
    tenant_id text NOT NULL,
    actor_id text NOT NULL,
    operation text NOT NULL CHECK (operation IN ('invite','accept','role','revoke','transfer')),
    client_key text NOT NULL,
    fingerprint text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, actor_id, operation, client_key)
);

-- +goose StatementBegin
DO $$ DECLARE t text; BEGIN
  FOREACH t IN ARRAY ARRAY['project_membership_clock','project_membership','project_membership_event','project_membership_idempotency'] LOOP
    EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
    EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', t);
    EXECUTE format('CREATE POLICY tenant_isolation ON %I USING (tenant_id = current_setting(''hcmnext.tenant_id'', true)) WITH CHECK (tenant_id = current_setting(''hcmnext.tenant_id'', true))', t);
  END LOOP;
END $$;
-- +goose StatementEnd

-- +goose Down
DROP TABLE project_membership_idempotency, project_membership_event, project_membership, project_membership_clock CASCADE;
ALTER TABLE project DROP COLUMN classification_level;
