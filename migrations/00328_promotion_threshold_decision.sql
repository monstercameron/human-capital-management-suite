-- 00328: durable RULE-003 threshold decisions (REV-010-01).
--
-- RULE-004's re-evaluation compares the live rule inputs against the exact
-- inputs, tier and row the approval was granted under. Nothing recorded
-- those: the threshold node's GovernanceRefs persist the matched row and the
-- table digest, but the tier needs its table and the inputs were discarded
-- after evaluation. This table freezes the threshold decision per proposal
-- revision and node attempt, keyed the way the served RuleFacts reads it
-- back (tenant, intent, revision; latest attempt wins). A second, different
-- decision for the same key is refused rather than overwritten: one revision
-- is evaluated against one set of inputs, and moved inputs need a new
-- revision, not a rewritten past.
--
-- The writer is the raise_threshold branch inside the advancement
-- transaction, so the decision and the outcome it produced commit or roll
-- back together. Only EXECUTE advances record; simulate/replay/shadow runs
-- never freeze approval-time evidence.

-- +goose Up

CREATE TABLE IF NOT EXISTS promotion_threshold_decision (
    tenant_id            tenant_ref NOT NULL REFERENCES tenant (tenant_id),
    intent_id            uuid       NOT NULL,
    revision             bigint     NOT NULL CHECK (revision >= 0),
    attempt              integer    NOT NULL CHECK (attempt >= 1),
    instance_id          uuid       NOT NULL,

    tier                 text NOT NULL CHECK (tier <> ''),
    matched_row          text NOT NULL CHECK (matched_row <> ''),
    table_id             text NOT NULL CHECK (table_id <> ''),
    table_version        text NOT NULL CHECK (table_version <> ''),
    table_digest         text NOT NULL CHECK (table_digest LIKE 'sha256:%'),
    input_digest         text NOT NULL CHECK (input_digest LIKE 'sha256:%'),
    inputs               jsonb NOT NULL CHECK (jsonb_typeof(inputs) = 'object'),

    recorded_at          timestamptz NOT NULL DEFAULT clock_timestamp(),

    PRIMARY KEY (tenant_id, intent_id, revision, attempt)
);

ALTER TABLE promotion_threshold_decision ENABLE ROW LEVEL SECURITY;
ALTER TABLE promotion_threshold_decision FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON promotion_threshold_decision USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

CREATE TRIGGER promotion_threshold_decision_append_only
    BEFORE UPDATE OR DELETE ON promotion_threshold_decision
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

GRANT SELECT, INSERT ON promotion_threshold_decision TO hcmnext_app;

-- +goose Down

-- Threshold decisions are the approval-time evidence RULE-004 re-evaluates
-- against; dropping them would orphan every frozen approval they cite.
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION '00328 is irreversible: threshold decisions are append-only approval evidence and cannot be discarded'; END $$;
-- +goose StatementEnd
