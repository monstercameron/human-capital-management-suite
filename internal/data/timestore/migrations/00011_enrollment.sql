-- +goose Up
-- Enrollment codes are single-use and short-lived. Only the code's hash is
-- ever stored; redemption is one atomic UPDATE guarded by "used_at IS NULL"
-- so two concurrent redemptions of the same code cannot both win.

CREATE TABLE time_enrollment_code (
    tenant_id text NOT NULL,
    code_hash text NOT NULL,
    site_id text NOT NULL,
    profile_id text NOT NULL,
    timezone text NOT NULL,
    created_by text NOT NULL,
    expires_at timestamptz NOT NULL,
    used_at timestamptz,
    used_by_device_id text,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, code_hash)
);
CREATE INDEX time_enrollment_code_expiry_idx ON time_enrollment_code (tenant_id, expires_at) WHERE used_at IS NULL;
SELECT time_enable_tenant_isolation('time_enrollment_code');

-- +goose Down
DROP TABLE time_enrollment_code;
