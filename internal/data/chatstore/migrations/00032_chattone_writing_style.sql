-- +goose Up
-- CHATTONE-004: what a workspace administrator chooses about the message-box
-- writing-style controls, and one usage line per call. Neither table holds a
-- draft, a rewrite or any message text.

-- One row per workspace. A workspace with no row has the controls off (the
-- served composition may turn a development workspace on without writing a
-- row; an administrator's save always writes one). styles is the registry the
-- workspace chose: [{"id","label","instruction","register"}], at most twelve,
-- validated by the service before it is written. An instruction is
-- administration data and is never returned to a member.
CREATE TABLE chattone_workspace_setting (
    tenant_id text PRIMARY KEY,
    enabled boolean NOT NULL,
    styles jsonb NOT NULL CHECK (jsonb_typeof(styles) = 'array' AND jsonb_array_length(styles) BETWEEN 1 AND 12),
    revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
    updated_by text NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);

-- One line per reservation and per operation of a writing-style call, written
-- in the same step as the call's outcome: identifiers, the operation, the
-- attempt and whether it succeeded. A "reserve" line is what the per-person
-- daily limit counts. Append-only: a correction is a later line.
CREATE TABLE chattone_usage (
    tenant_id text NOT NULL,
    line_id bigint GENERATED ALWAYS AS IDENTITY,
    person_id text NOT NULL,
    day date NOT NULL,
    operation text NOT NULL CHECK (operation IN ('reserve','rewrite','meaning')),
    attempt integer NOT NULL DEFAULT 0 CHECK (attempt BETWEEN 0 AND 2),
    succeeded boolean NOT NULL DEFAULT true,
    occurred_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, line_id)
);
CREATE INDEX chattone_usage_person_day ON chattone_usage (tenant_id, person_id, day, operation);

ALTER TABLE chattone_workspace_setting ENABLE ROW LEVEL SECURITY;
ALTER TABLE chattone_workspace_setting FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON chattone_workspace_setting USING (tenant_id=current_setting('hcmnext.tenant_id',true)) WITH CHECK (tenant_id=current_setting('hcmnext.tenant_id',true));
ALTER TABLE chattone_usage ENABLE ROW LEVEL SECURITY;
ALTER TABLE chattone_usage FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON chattone_usage USING (tenant_id=current_setting('hcmnext.tenant_id',true)) WITH CHECK (tenant_id=current_setting('hcmnext.tenant_id',true));

-- A setting is revised in place and its revision only moves forward; a usage
-- line is never changed or removed.
-- +goose StatementBegin
CREATE FUNCTION chattone_setting_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'UPDATE' THEN
        NEW.revision := OLD.revision + 1;
        NEW.updated_at := now();
    END IF;
    RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER chattone_setting_guard BEFORE UPDATE ON chattone_workspace_setting FOR EACH ROW EXECUTE FUNCTION chattone_setting_guard();

-- +goose StatementBegin
CREATE FUNCTION chattone_usage_forbid_mutation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'writing style usage lines are immutable';
END $$;
-- +goose StatementEnd
CREATE TRIGGER forbid_mutation BEFORE UPDATE OR DELETE ON chattone_usage FOR EACH ROW EXECUTE FUNCTION chattone_usage_forbid_mutation();

-- Neighbouring chat migrations use the role's existing default privileges
-- (SELECT, INSERT and UPDATE in a cell whose serving roles are separated); this
-- migration needs no more.

-- +goose Down
DROP TABLE chattone_usage;
DROP TABLE chattone_workspace_setting;
DROP FUNCTION chattone_usage_forbid_mutation();
DROP FUNCTION chattone_setting_guard();
