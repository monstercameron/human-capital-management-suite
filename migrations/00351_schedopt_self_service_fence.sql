-- REV-045-03: durable schedule and offer compare-and-swap fences.

-- +goose Up
CREATE TABLE schedopt_self_service_state (
    tenant_id uuid NOT NULL REFERENCES tenant(tenant_id),
    schedule_id text NOT NULL CHECK (btrim(schedule_id) <> ''),
    publication_digest content_digest NOT NULL,
    fence bigint NOT NULL CHECK (fence > 0),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, schedule_id)
);
ALTER TABLE schedopt_self_service_state ENABLE ROW LEVEL SECURITY;
ALTER TABLE schedopt_self_service_state FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON schedopt_self_service_state
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
GRANT SELECT, INSERT, UPDATE ON schedopt_self_service_state TO hcmnext_app;

CREATE TABLE schedopt_self_service_offer (
    tenant_id uuid NOT NULL REFERENCES tenant(tenant_id),
    schedule_id text NOT NULL,
    offer_id text NOT NULL CHECK (btrim(offer_id) <> ''),
    offer_state text NOT NULL CHECK (offer_state IN ('OPEN','CLAIMED','TRADED','CLOSED')),
    offer_fence bigint NOT NULL CHECK (offer_fence > 0),
    publication_digest content_digest NOT NULL,
    payload jsonb NOT NULL CHECK (jsonb_typeof(payload) = 'object'),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, schedule_id, offer_id),
    FOREIGN KEY (tenant_id, schedule_id)
        REFERENCES schedopt_self_service_state(tenant_id, schedule_id)
);
ALTER TABLE schedopt_self_service_offer ENABLE ROW LEVEL SECURITY;
ALTER TABLE schedopt_self_service_offer FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON schedopt_self_service_offer
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
GRANT SELECT, INSERT, UPDATE ON schedopt_self_service_offer TO hcmnext_app;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM schedopt_self_service_offer) THEN
        RAISE EXCEPTION 'cannot remove durable shift offer history';
    END IF;
END $$;
-- +goose StatementEnd
DROP TABLE schedopt_self_service_offer;
DROP TABLE schedopt_self_service_state;
