-- AGENTP-015 durable tenant-scoped persona mention admission buckets.
-- +goose Up

CREATE TABLE persona_limit_buckets (
    tenant_id       uuid        NOT NULL REFERENCES tenant (tenant_id),
    bucket_kind     text        NOT NULL CHECK (bucket_kind IN ('INVOKER', 'CONVERSATION', 'PERSONA')),
    bucket_key      text        NOT NULL CHECK (btrim(bucket_key) <> ''),
    window_start    timestamptz NOT NULL,
    admission_count bigint      NOT NULL DEFAULT 0 CHECK (admission_count >= 0),
    active_count    bigint      NOT NULL DEFAULT 0 CHECK (active_count >= 0),
    used_spend      bigint      NOT NULL DEFAULT 0 CHECK (used_spend >= 0),
    reserved_spend  bigint      NOT NULL DEFAULT 0 CHECK (reserved_spend >= 0),
    revision        bigint      NOT NULL DEFAULT 1 CHECK (revision > 0),
    PRIMARY KEY (tenant_id, bucket_kind, bucket_key, window_start)
);

CREATE TABLE persona_limit_reservations (
    tenant_id             uuid        NOT NULL REFERENCES tenant (tenant_id),
    reservation_id        text        NOT NULL CHECK (btrim(reservation_id) <> ''),
    invoker_bucket_key    text        NOT NULL,
    invoker_window_start  timestamptz NOT NULL,
    conversation_bucket_key   text        NOT NULL,
    conversation_window_start timestamptz NOT NULL,
    persona_bucket_key    text        NOT NULL,
    persona_window_start  timestamptz NOT NULL,
    policy_version        text        NOT NULL CHECK (btrim(policy_version) <> ''),
    estimate              bigint      NOT NULL CHECK (estimate > 0),
    actual                bigint      NOT NULL DEFAULT 0 CHECK (actual >= 0),
    state                 text        NOT NULL CHECK (state IN ('OPEN', 'SETTLED', 'RELEASED')),
    revision              bigint      NOT NULL DEFAULT 1 CHECK (revision > 0),
    created_at            timestamptz NOT NULL,
    closed_at             timestamptz,
    PRIMARY KEY (tenant_id, reservation_id),
    CHECK ((state = 'OPEN' AND closed_at IS NULL AND actual = 0) OR
           (state IN ('SETTLED', 'RELEASED') AND closed_at IS NOT NULL)),
    CHECK (actual <= estimate)
);
CREATE INDEX persona_limit_reservations_state
    ON persona_limit_reservations (tenant_id, state, created_at);

ALTER TABLE persona_limit_buckets ENABLE ROW LEVEL SECURITY;
ALTER TABLE persona_limit_buckets FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON persona_limit_buckets
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE persona_limit_reservations ENABLE ROW LEVEL SECURITY;
ALTER TABLE persona_limit_reservations FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON persona_limit_reservations
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

GRANT SELECT, INSERT, UPDATE ON persona_limit_buckets TO hcmnext_agent_app;
GRANT SELECT, INSERT, UPDATE ON persona_limit_reservations TO hcmnext_agent_app;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM persona_limit_reservations) OR EXISTS (SELECT 1 FROM persona_limit_buckets) THEN
        RAISE EXCEPTION 'cannot remove retained persona budget reservations';
    END IF;
END $$;
-- +goose StatementEnd
DROP TABLE persona_limit_reservations;
DROP TABLE persona_limit_buckets;
