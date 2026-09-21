-- DB-017 follow-up: the application role never receives table-level DELETE.
-- Operational cleanup remains available through tenant-fenced, purpose-built
-- functions whose predicates cannot be widened by callers.

-- +goose Up
-- +goose StatementBegin

CREATE FUNCTION hcmnext_replace_worker_role_assignments(
    p_tenant_id uuid,
    p_worker_ref text
) RETURNS bigint
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path FROM CURRENT
AS $$
DECLARE
    removed bigint;
BEGIN
    IF p_tenant_id IS NULL OR p_tenant_id IS DISTINCT FROM
        NULLIF(current_setting('app.tenant_id', true), '')::uuid THEN
        RAISE EXCEPTION 'tenant scope does not authorize assignment replacement'
            USING ERRCODE = '42501';
    END IF;
    DELETE FROM worker_access_role_assignment
    WHERE tenant_id = p_tenant_id AND worker_ref = p_worker_ref;
    GET DIAGNOSTICS removed = ROW_COUNT;
    RETURN removed;
END;
$$;

CREATE FUNCTION hcmnext_release_conflict_scope_fence(
    p_tenant_id uuid,
    p_intent_id text,
    p_fence bigint
) RETURNS bigint
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path FROM CURRENT
AS $$
DECLARE
    removed bigint;
BEGIN
    IF p_tenant_id IS NULL OR p_tenant_id IS DISTINCT FROM
        NULLIF(current_setting('app.tenant_id', true), '')::uuid THEN
        RAISE EXCEPTION 'tenant scope does not authorize conflict-fence release'
            USING ERRCODE = '42501';
    END IF;
    DELETE FROM conflict_scope_fence
    WHERE tenant_id = p_tenant_id AND intent_id = p_intent_id AND fence = p_fence;
    GET DIAGNOSTICS removed = ROW_COUNT;
    RETURN removed;
END;
$$;

CREATE FUNCTION hcmnext_prune_expired_job_trace_links(
    p_tenant_id uuid,
    p_before timestamptz,
    p_limit integer
) RETURNS bigint
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path FROM CURRENT
AS $$
DECLARE
    affected bigint;
BEGIN
    IF p_tenant_id IS NULL OR p_tenant_id IS DISTINCT FROM
        NULLIF(current_setting('app.tenant_id', true), '')::uuid THEN
        RAISE EXCEPTION 'tenant scope does not authorize trace-link pruning'
            USING ERRCODE = '42501';
    END IF;
    IF p_before IS NULL OR p_limit < 1 OR p_limit > 1000 THEN
        RAISE EXCEPTION 'invalid trace-link pruning boundary'
            USING ERRCODE = '22023';
    END IF;

    WITH candidates AS (
        SELECT 'run' AS kind, run_id AS owner_id, 0::bigint AS sequence,
               trace_link_expires_at AS expires_at
        FROM job_run
        WHERE tenant_id = p_tenant_id AND trace_link_expires_at <= p_before
        UNION ALL
        SELECT 'partition', partition_id, 0::bigint, trace_link_expires_at
        FROM job_partition
        WHERE tenant_id = p_tenant_id AND trace_link_expires_at <= p_before
        UNION ALL
        SELECT 'checkpoint', partition_id, checkpoint_sequence, expires_at
        FROM job_checkpoint_trace_link
        WHERE tenant_id = p_tenant_id AND expires_at <= p_before
        ORDER BY expires_at, kind, owner_id, sequence
        LIMIT p_limit
    ), pruned_runs AS (
        UPDATE job_run r
        SET trace_id = NULL, trace_span_id = NULL, trace_flags = NULL,
            trace_state = NULL, trace_link_expires_at = NULL
        FROM candidates c
        WHERE c.kind = 'run' AND r.tenant_id = p_tenant_id
          AND r.run_id = c.owner_id
          AND r.trace_link_expires_at <= p_before
          AND r.trace_link_expires_at = c.expires_at
        RETURNING 1
    ), pruned_partitions AS (
        UPDATE job_partition p
        SET trace_id = NULL, trace_span_id = NULL, trace_flags = NULL,
            trace_state = NULL, trace_link_expires_at = NULL
        FROM candidates c
        WHERE c.kind = 'partition' AND p.tenant_id = p_tenant_id
          AND p.partition_id = c.owner_id
          AND p.trace_link_expires_at <= p_before
          AND p.trace_link_expires_at = c.expires_at
        RETURNING 1
    ), pruned_checkpoints AS (
        DELETE FROM job_checkpoint_trace_link l USING candidates c
        WHERE c.kind = 'checkpoint' AND l.tenant_id = p_tenant_id
          AND l.partition_id = c.owner_id
          AND l.checkpoint_sequence = c.sequence
        RETURNING 1
    )
    SELECT (SELECT count(*) FROM pruned_runs)
         + (SELECT count(*) FROM pruned_partitions)
         + (SELECT count(*) FROM pruned_checkpoints)
    INTO affected;
    RETURN affected;
END;
$$;

CREATE FUNCTION hcmnext_discard_workflow_draft_redo(
    p_tenant_id uuid,
    p_draft_id uuid,
    p_history_position bigint
) RETURNS bigint
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path FROM CURRENT
AS $$
DECLARE
    removed bigint;
BEGIN
    IF p_tenant_id IS NULL OR p_tenant_id IS DISTINCT FROM
        NULLIF(current_setting('app.tenant_id', true), '')::uuid THEN
        RAISE EXCEPTION 'tenant scope does not authorize draft-history cleanup'
            USING ERRCODE = '42501';
    END IF;
    DELETE FROM workflow_designer_draft_history
    WHERE tenant_id = p_tenant_id AND draft_id = p_draft_id
      AND history_position > p_history_position;
    GET DIAGNOSTICS removed = ROW_COUNT;
    RETURN removed;
END;
$$;

CREATE FUNCTION hcmnext_purge_expired_workflow_drafts(
    p_tenant_id uuid,
    p_at timestamptz
) RETURNS bigint
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path FROM CURRENT
AS $$
DECLARE
    removed bigint;
BEGIN
    IF p_tenant_id IS NULL OR p_tenant_id IS DISTINCT FROM
        NULLIF(current_setting('app.tenant_id', true), '')::uuid THEN
        RAISE EXCEPTION 'tenant scope does not authorize draft cleanup'
            USING ERRCODE = '42501';
    END IF;
    IF p_at IS NULL THEN
        RAISE EXCEPTION 'draft cleanup boundary is required'
            USING ERRCODE = '22023';
    END IF;
    DELETE FROM workflow_designer_draft
    WHERE tenant_id = p_tenant_id AND expires_at <= p_at;
    GET DIAGNOSTICS removed = ROW_COUNT;
    RETURN removed;
END;
$$;

REVOKE DELETE ON worker_access_role_assignment FROM hcmnext_app;
REVOKE DELETE ON conflict_scope_fence FROM hcmnext_app;
REVOKE DELETE ON job_checkpoint_trace_link FROM hcmnext_app;
REVOKE DELETE ON workflow_designer_draft FROM hcmnext_app;
REVOKE DELETE ON workflow_designer_draft_history FROM hcmnext_app;

REVOKE ALL ON FUNCTION hcmnext_replace_worker_role_assignments(uuid, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION hcmnext_release_conflict_scope_fence(uuid, text, bigint) FROM PUBLIC;
REVOKE ALL ON FUNCTION hcmnext_prune_expired_job_trace_links(uuid, timestamptz, integer) FROM PUBLIC;
REVOKE ALL ON FUNCTION hcmnext_discard_workflow_draft_redo(uuid, uuid, bigint) FROM PUBLIC;
REVOKE ALL ON FUNCTION hcmnext_purge_expired_workflow_drafts(uuid, timestamptz) FROM PUBLIC;

GRANT EXECUTE ON FUNCTION hcmnext_replace_worker_role_assignments(uuid, text) TO hcmnext_app;
GRANT EXECUTE ON FUNCTION hcmnext_release_conflict_scope_fence(uuid, text, bigint) TO hcmnext_app;
GRANT EXECUTE ON FUNCTION hcmnext_prune_expired_job_trace_links(uuid, timestamptz, integer) TO hcmnext_app;
GRANT EXECUTE ON FUNCTION hcmnext_discard_workflow_draft_redo(uuid, uuid, bigint) TO hcmnext_app;
GRANT EXECUTE ON FUNCTION hcmnext_purge_expired_workflow_drafts(uuid, timestamptz) TO hcmnext_app;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION '00319 is irreversible: restoring direct application-role DELETE would violate DB-017'; END $$;
-- +goose StatementEnd
