-- Independent external-cost journal, applied by deployment only.
-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION extcost_forbid_mutation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'external usage journal is append-only'; END $$;
-- +goose StatementEnd
CREATE TABLE extcost_usage (
 tenant_id text NOT NULL, attempt_key text NOT NULL, cause_id text NOT NULL,
 feature text NOT NULL, provider text NOT NULL, operation text NOT NULL,
 actor text NOT NULL, agent text NOT NULL, workflow text NOT NULL,
 currency text NOT NULL CHECK(length(currency)=3), cost_micros bigint NOT NULL,
 occurred_at timestamptz NOT NULL, budget_at timestamptz NOT NULL, line jsonb NOT NULL,
 PRIMARY KEY(tenant_id,attempt_key)
);
CREATE INDEX extcost_usage_period ON extcost_usage(tenant_id,occurred_at,currency);
CREATE INDEX extcost_usage_cause ON extcost_usage(tenant_id,cause_id);
CREATE TABLE extcost_reservation (
 tenant_id text NOT NULL, attempt_key text NOT NULL, fingerprint text NOT NULL,
 maximum_micros bigint NOT NULL CHECK(maximum_micros>=0), currency text NOT NULL,
 state text NOT NULL CHECK(state IN ('reserved','sent','settled','released')),
 reservation jsonb NOT NULL, PRIMARY KEY(tenant_id,attempt_key)
);
CREATE TABLE extcost_budget (
 tenant_id text NOT NULL, kind text NOT NULL, scope_id text NOT NULL, period text NOT NULL,
 currency text NOT NULL, limit_micros bigint NOT NULL CHECK(limit_micros>=0),
 warning_basis_points bigint NOT NULL CHECK(warning_basis_points BETWEEN 1 AND 10000),
 PRIMARY KEY(tenant_id,kind,scope_id,period,currency)
);
CREATE TABLE extcost_budget_audit (
 audit_id bigserial PRIMARY KEY, tenant_id text NOT NULL, actor text NOT NULL,
 reason text NOT NULL, occurred_at timestamptz NOT NULL, change jsonb NOT NULL
);
CREATE TABLE extcost_reconciliation (
 tenant_id text NOT NULL, report_id text NOT NULL, occurred_at timestamptz NOT NULL,
 report jsonb NOT NULL, PRIMARY KEY(tenant_id,report_id)
);
CREATE TABLE extcost_price_schedule (
 tenant_id text NOT NULL, provider text NOT NULL, operation text NOT NULL,
 model text NOT NULL, model_version text NOT NULL, version text NOT NULL, digest text NOT NULL,
 effective_from timestamptz NOT NULL, schedule jsonb NOT NULL,
 PRIMARY KEY(tenant_id,provider,operation,model,model_version,version)
);
CREATE TRIGGER forbid_mutation BEFORE UPDATE OR DELETE ON extcost_usage FOR EACH ROW EXECUTE FUNCTION extcost_forbid_mutation();
CREATE TRIGGER forbid_mutation BEFORE UPDATE OR DELETE ON extcost_budget_audit FOR EACH ROW EXECUTE FUNCTION extcost_forbid_mutation();
CREATE TRIGGER forbid_mutation BEFORE UPDATE OR DELETE ON extcost_reconciliation FOR EACH ROW EXECUTE FUNCTION extcost_forbid_mutation();
CREATE TRIGGER forbid_mutation BEFORE UPDATE OR DELETE ON extcost_price_schedule FOR EACH ROW EXECUTE FUNCTION extcost_forbid_mutation();
-- +goose StatementBegin
DO $$ DECLARE t text; BEGIN
 FOREACH t IN ARRAY ARRAY['extcost_usage','extcost_reservation','extcost_budget','extcost_budget_audit','extcost_reconciliation','extcost_price_schedule'] LOOP
  EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY',t);
  EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY',t);
  EXECUTE format('CREATE POLICY tenant_isolation ON %I USING (tenant_id = current_setting(''hcmnext.tenant_id'',true)) WITH CHECK (tenant_id = current_setting(''hcmnext.tenant_id'',true))',t);
  EXECUTE format('GRANT SELECT,INSERT ON %I TO hcmnext_app',t);
 END LOOP;
END $$;
-- +goose StatementEnd
GRANT UPDATE ON extcost_budget,extcost_reservation TO hcmnext_app;
GRANT USAGE,SELECT ON SEQUENCE extcost_budget_audit_audit_id_seq TO hcmnext_app;
-- +goose Down
DROP TABLE extcost_price_schedule,extcost_reconciliation,extcost_budget_audit,extcost_budget,extcost_reservation,extcost_usage;
DROP FUNCTION extcost_forbid_mutation();
