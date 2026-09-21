-- Owner: data plane. Phase: P3.
-- The published career ladder, as rows instead of a Go map.
--
-- The demo company's promotion ladder was computed in Go
-- (internal/data/demoworkforce/promotion_paths.go) and never persisted, so
-- the served promotion-path catalog, the target-position picker and the
-- ladder gate all read a compiled-in table that no tenant could see, revise
-- or scope. This table is the tenant's own published ladder: one row per
-- (source profile, target profile) edge, effective-dated and append-only,
-- so a proposal made under one published edge can still be read back after
-- the ladder moves on.
--
-- An edge names the job architecture on both ends (source_profile_id /
-- target_profile_id are job_profile_revision.profile_id values from
-- migration 00048) and carries the display projections (job code, grade,
-- title) the catalog publishes. The projections are recorded rather than
-- joined because the edge is the published artifact: a later profile
-- revision must not silently retitle an edge a proposal already cited.
--
-- org_unit scopes the edge to the organization unit whose workers it
-- applies to; the same source job in a different unit is a different
-- published edge.
--
-- Storage disposition (STORE-001): permanent, append-only reference data,
-- tenant scoped by tenant_id, owned by internal/data/promotionladder, with
-- no rebuild source.

-- +goose Up

CREATE TABLE promotion_path_edge (
    row_id                uuid           NOT NULL,
    tenant_id             tenant_ref     NOT NULL REFERENCES tenant (tenant_id),
    path_id               semantic_key   NOT NULL,
    revision              semantic_key   NOT NULL,
    org_unit              semantic_key   NOT NULL,
    kind                  text           NOT NULL,
    -- ordinal is the edge's published rank among the targets its source job
    -- offers: 1 is the nearest step. It is stored because the order a ladder
    -- is read back in is part of what was published, and no column the rows
    -- already carry reproduces it.
    ordinal               integer        NOT NULL,
    source_profile_id     semantic_key   NOT NULL,
    source_job_code       semantic_key   NOT NULL,
    source_grade          semantic_key   NOT NULL,
    target_profile_id     semantic_key   NOT NULL,
    target_job_code       semantic_key   NOT NULL,
    target_grade          semantic_key   NOT NULL,
    target_title          text           NOT NULL,
    minimum_base_increase numeric(9, 4)  NOT NULL,
    maximum_base_increase numeric(9, 4)  NOT NULL,
    lifecycle             text           NOT NULL,
    policy_version        semantic_key   NOT NULL,
    effective_from        timestamptz    NOT NULL,
    effective_to          timestamptz,
    known_from            timestamptz    NOT NULL,
    recorded_at           timestamptz    NOT NULL,
    PRIMARY KEY (tenant_id, row_id),
    CONSTRAINT promotion_path_edge_one_per_revision UNIQUE (tenant_id, path_id, revision),
    CONSTRAINT promotion_path_edge_kind_allowed CHECK (kind IN ('UPWARD', 'LATERAL', 'CROSS_FAMILY')),
    CONSTRAINT promotion_path_edge_lifecycle_allowed CHECK (
        lifecycle IN ('DRAFT', 'PUBLISHED', 'RETIRED', 'SUPERSEDED')
    ),
    CONSTRAINT promotion_path_edge_distinct_ends CHECK (source_profile_id <> target_profile_id),
    CONSTRAINT promotion_path_edge_ordinal_positive CHECK (ordinal > 0),
    CONSTRAINT promotion_path_edge_title_present CHECK (length(btrim(target_title)) > 0),
    CONSTRAINT promotion_path_edge_increase_non_negative CHECK (minimum_base_increase >= 0),
    CONSTRAINT promotion_path_edge_increase_ordered CHECK (minimum_base_increase <= maximum_base_increase),
    CONSTRAINT promotion_path_edge_effective_half_open CHECK (
        effective_to IS NULL OR effective_from < effective_to
    )
);

CREATE INDEX promotion_path_edge_by_source
    ON promotion_path_edge (tenant_id, org_unit, source_job_code, source_grade, ordinal);

-- The published ladder is evidence a proposal cites: a row is never edited or
-- removed, only superseded by a later revision.
CREATE OR REPLACE TRIGGER promotion_path_edge_append_only
    BEFORE UPDATE OR DELETE ON promotion_path_edge
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

REVOKE UPDATE, DELETE ON promotion_path_edge FROM PUBLIC;

ALTER TABLE promotion_path_edge ENABLE ROW LEVEL SECURITY;
ALTER TABLE promotion_path_edge FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON promotion_path_edge
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

GRANT SELECT, INSERT ON promotion_path_edge TO hcmnext_app;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION '00317 is irreversible: migrations 00279-00316 already broke the rollback chain, and dropping the published ladder would discard the append-only promotion paths live proposals cite rather than restore a reachable earlier schema'; END $$;
-- +goose StatementEnd
