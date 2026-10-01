-- +goose Up
-- WTIME-001/002 persistence: versioned, effective-dated time profile
-- definitions and eligibility rules. Publishing a new version is an INSERT
-- only; time_forbid_mutation (00001) blocks every later UPDATE or DELETE so
-- an old version can never be mutated by a later publish.

CREATE TABLE time_profile_version (
 tenant_id text NOT NULL, profile_id text NOT NULL,
 version bigint NOT NULL CHECK (version > 0),
 effective_from timestamptz NOT NULL, effective_to timestamptz,
 payload jsonb NOT NULL, digest text NOT NULL CHECK (digest <> ''),
 published_by text NOT NULL CHECK (published_by <> ''),
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY (tenant_id, profile_id, version),
 CHECK (effective_to IS NULL OR effective_to > effective_from)
);
CREATE INDEX time_profile_version_window ON time_profile_version(tenant_id, profile_id, effective_from);
CREATE TRIGGER time_profile_version_immutable BEFORE UPDATE OR DELETE ON time_profile_version
 FOR EACH ROW EXECUTE FUNCTION time_forbid_mutation();

CREATE TABLE time_eligibility_rule_version (
 tenant_id text NOT NULL, rule_id text NOT NULL,
 version bigint NOT NULL CHECK (version > 0),
 payload jsonb NOT NULL, digest text NOT NULL CHECK (digest <> ''),
 published_by text NOT NULL CHECK (published_by <> ''),
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY (tenant_id, rule_id, version)
);
CREATE TRIGGER time_eligibility_rule_version_immutable BEFORE UPDATE OR DELETE ON time_eligibility_rule_version
 FOR EACH ROW EXECUTE FUNCTION time_forbid_mutation();

-- The per-assignment resolved profile pin. Unlike the version tables this
-- row is mutated in place under an optimistic revision guard: a repin never
-- rewrites the pin an open session or period already read, because the
-- caller must present the revision it last observed.
CREATE TABLE time_assignment_profile_pin (
 tenant_id text NOT NULL, assignment_ref text NOT NULL,
 revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
 profile_id text NOT NULL, profile_version bigint NOT NULL CHECK (profile_version > 0),
 resolved_at timestamptz NOT NULL, updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY (tenant_id, assignment_ref)
);

SELECT time_enable_tenant_isolation('time_profile_version');
SELECT time_enable_tenant_isolation('time_eligibility_rule_version');
SELECT time_enable_tenant_isolation('time_assignment_profile_pin');

-- +goose Down
DROP TABLE time_assignment_profile_pin, time_eligibility_rule_version, time_profile_version CASCADE;
