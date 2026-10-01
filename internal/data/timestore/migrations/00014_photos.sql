-- +goose Up
-- Punch photo artifact references, linked to the punch receipt they were
-- captured for. Every view is audited in an append-only log. Deletion
-- honors a legal-hold flag: a held photo is never swept by the retention
-- schedule.

CREATE TABLE time_punch_photo (
    tenant_id text NOT NULL,
    id uuid NOT NULL,
    punch_receipt_id text NOT NULL,
    artifact_ref text NOT NULL,
    site_id text NOT NULL,
    review_deadline timestamptz NOT NULL,
    legal_hold boolean NOT NULL DEFAULT false,
    captured_at timestamptz NOT NULL DEFAULT now(),
    deleted_at timestamptz,
    PRIMARY KEY (tenant_id, id)
);
CREATE INDEX time_punch_photo_receipt_idx ON time_punch_photo (tenant_id, punch_receipt_id);
CREATE INDEX time_punch_photo_deletion_idx ON time_punch_photo (tenant_id, review_deadline) WHERE deleted_at IS NULL AND legal_hold = false;
SELECT time_enable_tenant_isolation('time_punch_photo');

CREATE TABLE time_punch_photo_view (
    tenant_id text NOT NULL,
    id uuid NOT NULL,
    photo_id uuid NOT NULL,
    viewer_id text NOT NULL,
    viewer_scope text NOT NULL,
    viewed_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, id)
);
CREATE INDEX time_punch_photo_view_photo_idx ON time_punch_photo_view (tenant_id, photo_id);
SELECT time_enable_tenant_isolation('time_punch_photo_view');
CREATE TRIGGER time_punch_photo_view_immutable BEFORE UPDATE OR DELETE ON time_punch_photo_view FOR EACH ROW EXECUTE FUNCTION time_forbid_mutation();

-- +goose Down
DROP TABLE time_punch_photo_view;
DROP TABLE time_punch_photo;
