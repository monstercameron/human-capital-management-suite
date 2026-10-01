-- AGENT-050: retain delegated budget lineage across a ledger restart.
-- +goose Up
ALTER TABLE agent_budget_task
    ADD COLUMN parent_task_id text,
    ADD COLUMN root_task_id text,
    ADD COLUMN delegation_depth integer NOT NULL DEFAULT 0;

ALTER TABLE agent_budget_task
    ADD CONSTRAINT agent_budget_task_lineage_shape CHECK (
        (parent_task_id IS NULL AND root_task_id IS NULL AND delegation_depth = 0)
        OR (parent_task_id IS NOT NULL AND root_task_id IS NOT NULL
            AND parent_task_id <> task_id AND root_task_id <> task_id
            AND delegation_depth BETWEEN 1 AND 16)
    );

CREATE INDEX agent_budget_task_parent ON agent_budget_task (tenant_id, parent_task_id);

-- +goose Down
DROP INDEX agent_budget_task_parent;
ALTER TABLE agent_budget_task DROP CONSTRAINT agent_budget_task_lineage_shape;
ALTER TABLE agent_budget_task DROP COLUMN delegation_depth, DROP COLUMN root_task_id, DROP COLUMN parent_task_id;
