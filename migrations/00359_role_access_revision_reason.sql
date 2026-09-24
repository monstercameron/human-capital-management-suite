-- Owner: trust and product experience (RBAC-RT-021). Phase: production frontend.
-- storage-disposition: role authorization | append-only permission revision ledger | local PostgreSQL | tenant-local ACID/CAS | admin governed.
--
-- 00327 introduced the append-only role revision ledger. Keep a reason for
-- every new revision and identify organization visibility scope explicitly so
-- historical permission snapshots can be reconstructed without ambiguity.

-- +goose Up

ALTER TABLE access_role_revision
    ADD COLUMN change_reason text NOT NULL DEFAULT 'Legacy revision predates required reason capture',
    ADD COLUMN organization_scope_id text NOT NULL DEFAULT '';

ALTER TABLE access_role_revision
    ADD CONSTRAINT access_role_revision_reason_nonblank
        CHECK (change_reason = btrim(change_reason) AND change_reason <> '' AND length(change_reason) <= 500),
    ADD CONSTRAINT access_role_revision_reason_single_line
        CHECK (position(chr(10) in change_reason) = 0 AND position(chr(13) in change_reason) = 0);

-- Establish a truthful history boundary for permissions that existed before
-- reason capture was introduced. The baseline says when the state was
-- observed, while explicitly stating that the original rationale is unknown.
INSERT INTO access_role_revision (
    tenant_id, revision_id, actor_ref, change_reason, change_kind, role_id,
    organization_scope_id, before_row, after_row
)
SELECT r.tenant_id, gen_random_uuid(), coalesce(nullif(r.updated_by, ''), 'system:history-baseline'),
       'Historical baseline captured by migration; original rationale predates reason capture',
       'ROLE', r.role_id, '', NULL,
       jsonb_build_object('Version',r.version,'ID',r.role_id,'Name',r.name,'Description',r.description,'System',r.system_role,'Active',r.active)
FROM access_role r
WHERE NOT EXISTS (
    SELECT 1 FROM access_role_revision h WHERE h.tenant_id=r.tenant_id AND h.change_kind='ROLE' AND h.role_id=r.role_id
);

INSERT INTO access_role_revision (
    tenant_id, revision_id, actor_ref, change_reason, change_kind, role_id,
    organization_scope_id, page_id, before_row, after_row
)
SELECT p.tenant_id, gen_random_uuid(), coalesce(nullif(p.updated_by, ''), 'system:history-baseline'),
       'Historical baseline captured by migration; original rationale predates reason capture',
       'PAGE_PERMISSION', p.role_id, '', p.page_id, NULL,
       jsonb_build_object('Version',p.version,'RoleID',p.role_id,'PageID',p.page_id,'View',p.can_view,'Create',p.can_create,'Update',p.can_update,'Delete',p.can_delete)
FROM role_page_permission p
WHERE NOT EXISTS (
    SELECT 1 FROM access_role_revision h WHERE h.tenant_id=p.tenant_id AND h.change_kind='PAGE_PERMISSION' AND h.role_id=p.role_id AND h.page_id=p.page_id
);

INSERT INTO access_role_revision (
    tenant_id, revision_id, actor_ref, change_reason, change_kind, role_id,
    organization_scope_id, page_id, feature_id, before_row, after_row
)
SELECT f.tenant_id, gen_random_uuid(), coalesce(nullif(f.updated_by, ''), 'system:history-baseline'),
       'Historical baseline captured by migration; original rationale predates reason capture',
       'FEATURE_PERMISSION', f.role_id, '', f.page_id, f.feature_id, NULL,
       jsonb_build_object('Version',f.version,'RoleID',f.role_id,'PageID',f.page_id,'FeatureID',f.feature_id,'View',f.can_view,'Create',f.can_create,'Update',f.can_update,'Delete',f.can_delete)
FROM role_page_feature_permission f
WHERE NOT EXISTS (
    SELECT 1 FROM access_role_revision h WHERE h.tenant_id=f.tenant_id AND h.change_kind='FEATURE_PERMISSION' AND h.role_id=f.role_id AND h.page_id=f.page_id AND h.feature_id=f.feature_id
);

INSERT INTO access_role_revision (
    tenant_id, revision_id, actor_ref, change_reason, change_kind, role_id,
    organization_scope_id, before_row, after_row
)
SELECT v.tenant_id, gen_random_uuid(), coalesce(nullif(v.updated_by, ''), 'system:history-baseline'),
       'Historical baseline captured by migration; original rationale predates reason capture',
       'VISIBILITY', v.role_id, v.organization_scope_id, NULL,
       jsonb_build_object('Version',v.version,'RoleID',v.role_id,'Mode',v.mode,'OrganizationUnits',v.organization_units)
FROM role_organization_visibility v
WHERE NOT EXISTS (
    SELECT 1 FROM access_role_revision h WHERE h.tenant_id=v.tenant_id AND h.change_kind='VISIBILITY' AND h.role_id=v.role_id AND h.organization_scope_id=v.organization_scope_id
);

INSERT INTO access_role_revision (
    tenant_id, revision_id, actor_ref, change_reason, change_kind, role_id,
    worker_ref, organization_scope_id, before_row, after_row
)
SELECT a.tenant_id, gen_random_uuid(), coalesce(nullif(s.updated_by, ''), 'system:history-baseline'),
       'Historical baseline captured by migration; original rationale predates reason capture',
       'ASSIGNMENT', a.role_id, a.worker_ref, '', NULL,
       jsonb_build_object('version',s.version,'present',true)
FROM worker_access_role_assignment a
JOIN worker_access_role_set s USING (tenant_id,worker_ref)
WHERE NOT EXISTS (
    SELECT 1 FROM access_role_revision h WHERE h.tenant_id=a.tenant_id AND h.change_kind='ASSIGNMENT' AND h.role_id=a.role_id AND h.worker_ref=a.worker_ref
);

CREATE INDEX access_role_revision_identity_history
    ON access_role_revision (tenant_id, change_kind, role_id, worker_ref, organization_scope_id, page_id, feature_id, recorded_at DESC, revision_id DESC);

-- +goose Down

-- Reasons and visibility scope are required to reconstruct authorization
-- history; dropping either would destroy durable evidence.
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION '00359 is irreversible: role authorization revision reasons and scope are durable evidence and cannot be discarded'; END $$;
-- +goose StatementEnd
