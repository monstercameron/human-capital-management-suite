-- +goose Up
-- TCLOCK-004/009/010: one immutable, effective-dated site policy snapshot.
-- The payload is strict JSON validated by the timestore writer before insert;
-- the columns make tenant/site/profile/version selection indexable and keep
-- the published revision visible without interpreting policy in SQL.
CREATE TABLE time_site_policy_version (
    tenant_id text NOT NULL,
    site_id text NOT NULL,
    profile_id text NOT NULL DEFAULT '',
    policy_id text NOT NULL,
    version bigint NOT NULL CHECK (version > 0),
    effective_from timestamptz NOT NULL,
    effective_to timestamptz,
    snapshot_revision text NOT NULL CHECK (snapshot_revision <> ''),
    max_offline_age_seconds bigint NOT NULL CHECK (max_offline_age_seconds > 0),
    payload jsonb NOT NULL,
    digest text NOT NULL CHECK (digest <> ''),
    published_by text NOT NULL CHECK (published_by <> ''),
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, site_id, profile_id, policy_id, version),
    CHECK (effective_to IS NULL OR effective_to > effective_from)
);
CREATE INDEX time_site_policy_current_idx
    ON time_site_policy_version (tenant_id, site_id, profile_id, effective_from, version DESC);
CREATE TRIGGER time_site_policy_version_immutable
    BEFORE UPDATE OR DELETE ON time_site_policy_version
    FOR EACH ROW EXECUTE FUNCTION time_forbid_mutation();
SELECT time_enable_tenant_isolation('time_site_policy_version');

-- +goose Down
DROP TABLE time_site_policy_version CASCADE;
