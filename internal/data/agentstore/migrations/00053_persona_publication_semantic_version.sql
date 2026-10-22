-- AGENTUX-065: a sealed semantic label on every persona publication.
-- persona_versions rows are strictly immutable, so the label seals onto the
-- publication event: the author's bump choice names the next label at
-- publish time, computed, never typed. Existing publications are labelled
-- once here: sequence n becomes n.0.0. The stored sequence stays the
-- immutable identity used in digests, seals and citations.
-- +goose Up
ALTER TABLE persona_lifecycle_events ADD COLUMN semantic_version text NOT NULL DEFAULT '';
UPDATE persona_lifecycle_events e SET semantic_version = pv.version || '.0.0'
FROM persona_versions pv
WHERE e.tenant_id = pv.tenant_id AND e.persona_id = pv.persona_id AND e.persona_version = pv.version AND e.to_state = 'PUBLISHED';

-- +goose Down
ALTER TABLE persona_lifecycle_events DROP COLUMN semantic_version;
