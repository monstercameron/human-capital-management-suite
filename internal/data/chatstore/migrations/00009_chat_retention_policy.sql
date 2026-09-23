-- +goose Up
CREATE TABLE chat_retention_policy (
    tenant_id text NOT NULL,
    conversation_kind text NOT NULL DEFAULT '',
    mode text NOT NULL,
    age_days integer NOT NULL DEFAULT 0,
    before_date timestamptz NOT NULL DEFAULT 'epoch',
    budget_bytes bigint NOT NULL DEFAULT 0,
    revision bigint NOT NULL,
    updated_by text NOT NULL,
    updated_at timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, conversation_kind),
    CONSTRAINT chat_retention_policy_mode CHECK (
      (mode = 'AGE' AND age_days > 0 AND age_days <= 36525 AND before_date = 'epoch' AND budget_bytes = 0) OR
      (mode = 'BEFORE_DATE' AND age_days = 0 AND before_date <> 'epoch' AND budget_bytes = 0) OR
      (mode = 'SIZE_BUDGET' AND conversation_kind = '' AND age_days = 0 AND before_date = 'epoch' AND budget_bytes > 0)
    )
);
ALTER TABLE chat_retention_policy ENABLE ROW LEVEL SECURITY;
ALTER TABLE chat_retention_policy FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON chat_retention_policy USING (tenant_id = current_setting('hcmnext.tenant_id', true)) WITH CHECK (tenant_id = current_setting('hcmnext.tenant_id', true));

-- +goose Down
DROP TABLE chat_retention_policy;
