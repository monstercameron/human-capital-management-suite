-- +goose Up
-- Issue 3805: an ambient agent may offer to start a governed workflow from a
-- message. The offer is one more row of agentux_ambient_offer (private to the
-- message's author; its title is the workflow's own catalog name, never the
-- message text), so the kind check admits it. A person's "Not now" and "Never
-- offer this" answers are remembered per person per workflow: muted_until is
-- the end of a "Not now" pause, and NULL means never offer it again. The row
-- is read and written only for its own person and carries no message content.
ALTER TABLE agentux_ambient_offer DROP CONSTRAINT agentux_ambient_offer_kind_check;
ALTER TABLE agentux_ambient_offer ADD CONSTRAINT agentux_ambient_offer_kind_check CHECK (kind IN ('TASK','REMINDER','WORKFLOW_START'));
CREATE TABLE agentux_ambient_workflow_mute (
    tenant_id text NOT NULL,
    person_id text NOT NULL,
    workflow_id text NOT NULL CHECK (btrim(workflow_id) <> '' AND char_length(workflow_id) <= 200),
    muted_until timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, person_id, workflow_id)
);
ALTER TABLE agentux_ambient_workflow_mute ENABLE ROW LEVEL SECURITY;
ALTER TABLE agentux_ambient_workflow_mute FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON agentux_ambient_workflow_mute
    USING (tenant_id = current_setting('hcmnext.tenant_id', true))
    WITH CHECK (tenant_id = current_setting('hcmnext.tenant_id', true));

-- +goose Down
DROP TABLE agentux_ambient_workflow_mute;
DELETE FROM agentux_ambient_offer WHERE kind = 'WORKFLOW_START';
ALTER TABLE agentux_ambient_offer DROP CONSTRAINT agentux_ambient_offer_kind_check;
ALTER TABLE agentux_ambient_offer ADD CONSTRAINT agentux_ambient_offer_kind_check CHECK (kind IN ('TASK','REMINDER'));
