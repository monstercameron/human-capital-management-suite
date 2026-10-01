-- UXBLIND-081: author-owned deletion is limited to untouched workflow drafts.

-- +goose Up
-- +goose StatementBegin

CREATE FUNCTION hcmnext_delete_workflow_draft(
    p_tenant_id uuid,
    p_draft_id uuid,
    p_author_ref text
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
        RAISE EXCEPTION 'tenant scope does not authorize draft deletion'
            USING ERRCODE = '42501';
    END IF;
    IF p_draft_id IS NULL OR NULLIF(btrim(coalesce(p_author_ref, '')), '') IS NULL THEN
        RAISE EXCEPTION 'draft deletion identity is required'
            USING ERRCODE = '22023';
    END IF;

    DELETE FROM workflow_designer_draft
    WHERE tenant_id = p_tenant_id
      AND draft_id = p_draft_id
      AND author_ref = btrim(p_author_ref)
      AND jsonb_typeof(document->'nodes') = 'array'
      AND jsonb_array_length(document->'nodes') = 0;
    GET DIAGNOSTICS removed = ROW_COUNT;
    RETURN removed;
END;
$$;

REVOKE ALL ON FUNCTION hcmnext_delete_workflow_draft(uuid, uuid, text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION hcmnext_delete_workflow_draft(uuid, uuid, text) TO hcmnext_app;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP FUNCTION hcmnext_delete_workflow_draft(uuid, uuid, text);
-- +goose StatementEnd
