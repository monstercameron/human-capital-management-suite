-- +goose Up
CREATE TABLE persona_reply_receipt (
    tenant_id uuid NOT NULL REFERENCES tenant(tenant_id),
    invocation_id text NOT NULL CHECK (btrim(invocation_id) <> ''),
    output_id text NOT NULL CHECK (btrim(output_id) <> ''),
    invoker_id text NOT NULL CHECK (btrim(invoker_id) <> ''),
    conversation_id text NOT NULL CHECK (btrim(conversation_id) <> ''),
    public_post_id text NOT NULL,
    private_conversation_id text NOT NULL,
    receipt jsonb NOT NULL CHECK (jsonb_typeof(receipt) = 'object'),
    occurred_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, invocation_id),
    FOREIGN KEY (tenant_id, output_id) REFERENCES persona_final_outputs(tenant_id,output_id)
);
CREATE INDEX persona_reply_receipt_conversation ON persona_reply_receipt (tenant_id,conversation_id,occurred_at DESC,invocation_id);
CREATE INDEX persona_reply_receipt_owner ON persona_reply_receipt (tenant_id,invoker_id,private_conversation_id,occurred_at DESC,invocation_id);
ALTER TABLE persona_reply_receipt ENABLE ROW LEVEL SECURITY;
ALTER TABLE persona_reply_receipt FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON persona_reply_receipt USING (tenant_id = NULLIF(current_setting('app.tenant_id',true),'')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id',true),'')::uuid);
CREATE TRIGGER forbid_mutation BEFORE UPDATE OR DELETE ON persona_reply_receipt FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
GRANT SELECT, INSERT ON persona_reply_receipt TO hcmnext_agent_app;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM persona_reply_receipt) THEN
        RAISE EXCEPTION 'cannot remove retained persona reply receipts';
    END IF;
END $$;
-- +goose StatementEnd
DROP TABLE persona_reply_receipt;
