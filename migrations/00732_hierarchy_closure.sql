-- WF-DATA-020: effective-dated closure tables for every hierarchy.
-- +goose Up
CREATE TABLE hierarchy_type (
    tenant_id   tenant_ref NOT NULL REFERENCES tenant (tenant_id),
    hierarchy_key text     NOT NULL,
    depth_limit integer    NOT NULL,
    declared_at timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, hierarchy_key),
    CONSTRAINT hierarchy_type_key CHECK (hierarchy_key <> ''),
    CONSTRAINT hierarchy_type_depth_limit CHECK (depth_limit > 0)
);
CREATE TABLE hierarchy_edge (
    tenant_id     tenant_ref NOT NULL REFERENCES tenant (tenant_id),
    hierarchy_key text       NOT NULL,
    parent_id     text       NOT NULL,
    child_id      text       NOT NULL,
    effective_from date      NOT NULL,
    effective_to   date,
    known_at      timestamptz NOT NULL,
    event_id      text       NOT NULL,
    PRIMARY KEY (tenant_id, hierarchy_key, event_id),
    FOREIGN KEY (tenant_id, hierarchy_key) REFERENCES hierarchy_type (tenant_id, hierarchy_key),
    CONSTRAINT hierarchy_edge_identity CHECK (parent_id <> '' AND child_id <> '' AND parent_id <> child_id),
    CONSTRAINT hierarchy_edge_range CHECK (effective_to IS NULL OR effective_to > effective_from),
    CONSTRAINT hierarchy_edge_event CHECK (event_id <> '')
);
CREATE INDEX hierarchy_edge_by_identity
    ON hierarchy_edge (tenant_id, hierarchy_key, parent_id, child_id, known_at);
CREATE TABLE hierarchy_closure (
    tenant_id     tenant_ref NOT NULL REFERENCES tenant (tenant_id),
    hierarchy_key text       NOT NULL,
    ancestor_id   text       NOT NULL,
    descendant_id text       NOT NULL,
    depth         integer    NOT NULL,
    effective_from date      NOT NULL,
    effective_to   date,
    known_at      timestamptz NOT NULL,
    event_id      text       NOT NULL,
    PRIMARY KEY (tenant_id, hierarchy_key, event_id, ancestor_id, descendant_id),
    FOREIGN KEY (tenant_id, hierarchy_key) REFERENCES hierarchy_type (tenant_id, hierarchy_key),
    CONSTRAINT hierarchy_closure_identity CHECK (ancestor_id <> '' AND descendant_id <> '' AND ancestor_id <> descendant_id),
    CONSTRAINT hierarchy_closure_depth CHECK (depth > 0),
    CONSTRAINT hierarchy_closure_range CHECK (effective_to IS NULL OR effective_to > effective_from),
    CONSTRAINT hierarchy_closure_event CHECK (event_id <> '')
);
CREATE INDEX hierarchy_closure_by_descendant
    ON hierarchy_closure (tenant_id, hierarchy_key, descendant_id, known_at);
CREATE INDEX hierarchy_closure_by_ancestor
    ON hierarchy_closure (tenant_id, hierarchy_key, ancestor_id, known_at);
CREATE INDEX hierarchy_closure_by_pair
    ON hierarchy_closure (tenant_id, hierarchy_key, ancestor_id, descendant_id, known_at);
CREATE TRIGGER hierarchy_type_append_only
    BEFORE UPDATE OR DELETE ON hierarchy_type
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
CREATE TRIGGER hierarchy_edge_append_only
    BEFORE UPDATE OR DELETE ON hierarchy_edge
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
CREATE TRIGGER hierarchy_closure_append_only
    BEFORE UPDATE OR DELETE ON hierarchy_closure
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
ALTER TABLE hierarchy_type ENABLE ROW LEVEL SECURITY;
ALTER TABLE hierarchy_type FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON hierarchy_type
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE hierarchy_edge ENABLE ROW LEVEL SECURITY;
ALTER TABLE hierarchy_edge FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON hierarchy_edge
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE hierarchy_closure ENABLE ROW LEVEL SECURITY;
ALTER TABLE hierarchy_closure FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON hierarchy_closure
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
GRANT SELECT, INSERT ON hierarchy_type, hierarchy_edge, hierarchy_closure TO hcmnext_app;
-- +goose Down
DROP TABLE IF EXISTS hierarchy_closure;
DROP TABLE IF EXISTS hierarchy_edge;
DROP TABLE IF EXISTS hierarchy_type;
