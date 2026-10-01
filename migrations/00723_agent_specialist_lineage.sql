-- +goose Up
ALTER TABLE agent_task ADD COLUMN task_lineage jsonb;
ALTER TABLE agent_task ADD CONSTRAINT agent_task_lineage_shape CHECK (
    task_lineage IS NULL OR (
        jsonb_typeof(task_lineage) = 'object'
        AND task_lineage ?& ARRAY['parent_task_id', 'root_task_id', 'budget_task_id', 'delegation_depth']
        AND task_lineage->>'parent_task_id' <> task_id
        AND task_lineage->>'root_task_id' <> task_id
        AND task_lineage->>'budget_task_id' = task_lineage->>'root_task_id'
        AND (task_lineage->>'delegation_depth')::integer BETWEEN 1 AND 16
    )
);
ALTER TABLE agent_delegation_grant ADD COLUMN parent_grant_id text;
ALTER TABLE agent_delegation_grant ADD COLUMN parent_actor jsonb;
ALTER TABLE agent_delegation_grant ADD CONSTRAINT agent_delegation_parent_shape CHECK (
    (parent_grant_id IS NULL AND parent_actor IS NULL) OR (
        parent_grant_id IS NOT NULL AND parent_grant_id <> grant_id
        AND parent_actor IS NOT NULL AND jsonb_typeof(parent_actor) = 'object'
    )
);
ALTER TABLE agent_delegation_grant ADD CONSTRAINT agent_delegation_parent_fk
    FOREIGN KEY (tenant_id, parent_grant_id) REFERENCES agent_delegation_grant (tenant_id, grant_id);

-- +goose Down
ALTER TABLE agent_delegation_grant DROP CONSTRAINT agent_delegation_parent_fk;
ALTER TABLE agent_delegation_grant DROP CONSTRAINT agent_delegation_parent_shape;
ALTER TABLE agent_delegation_grant DROP COLUMN parent_actor;
ALTER TABLE agent_delegation_grant DROP COLUMN parent_grant_id;
ALTER TABLE agent_task DROP CONSTRAINT agent_task_lineage_shape;
ALTER TABLE agent_task DROP COLUMN task_lineage;
