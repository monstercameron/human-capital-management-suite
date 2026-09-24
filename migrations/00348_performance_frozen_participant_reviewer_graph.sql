-- REV-075-02: persist the exact participant/reviewer graph snapshot frozen
-- for a performance-cycle revision. The graph JSON is immutable evidence;
-- amendments use a later graph revision and retain the prior row.

-- +goose Up

CREATE TABLE performance_participant_reviewer_graph (
    tenant_id                 tenant_ref NOT NULL REFERENCES tenant (tenant_id),
    row_id                    uuid NOT NULL DEFAULT gen_random_uuid(),
    cycle_id                  text NOT NULL,
    cycle_revision            bigint NOT NULL CHECK (cycle_revision >= 1),
    graph_revision            bigint NOT NULL CHECK (graph_revision >= 1),
    supersedes_graph_revision bigint NOT NULL DEFAULT 0,
    graph_digest              content_digest NOT NULL,
    graph                     jsonb NOT NULL CHECK (jsonb_typeof(graph) = 'object'),
    recorded_at               timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (tenant_id, row_id),
    UNIQUE (tenant_id, cycle_id, cycle_revision, graph_revision),
    FOREIGN KEY (tenant_id, cycle_id, cycle_revision)
        REFERENCES performance_cycle (tenant_id, cycle_id, revision),
    CONSTRAINT performance_participant_reviewer_graph_revision_lineage CHECK (
        (graph_revision = 1 AND supersedes_graph_revision = 0)
        OR (graph_revision > 1 AND supersedes_graph_revision = graph_revision - 1)
    )
);

ALTER TABLE performance_participant_reviewer_graph ENABLE ROW LEVEL SECURITY;
ALTER TABLE performance_participant_reviewer_graph FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON performance_participant_reviewer_graph
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

CREATE TRIGGER performance_participant_reviewer_graph_forbid_mutation
    BEFORE UPDATE OR DELETE ON performance_participant_reviewer_graph
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON performance_participant_reviewer_graph FROM PUBLIC;
REVOKE UPDATE, DELETE ON performance_participant_reviewer_graph FROM hcmnext_app;
GRANT SELECT, INSERT ON performance_participant_reviewer_graph TO hcmnext_app;

-- +goose Down

REVOKE ALL ON performance_participant_reviewer_graph FROM hcmnext_app;
DROP POLICY tenant_isolation ON performance_participant_reviewer_graph;
ALTER TABLE performance_participant_reviewer_graph NO FORCE ROW LEVEL SECURITY;
ALTER TABLE performance_participant_reviewer_graph DISABLE ROW LEVEL SECURITY;
DROP TRIGGER performance_participant_reviewer_graph_forbid_mutation ON performance_participant_reviewer_graph;
DROP TABLE performance_participant_reviewer_graph;
