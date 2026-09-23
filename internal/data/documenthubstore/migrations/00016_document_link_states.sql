-- HUB-022: the link checker marks broken or stale targets, so the
-- extraction-time state set grows beyond valid/malformed.
-- +goose Up
ALTER TABLE document_link DROP CONSTRAINT document_link_state_check;
ALTER TABLE document_link ADD CONSTRAINT document_link_state_check CHECK (state IN ('valid','malformed','broken','stale'));

-- +goose Down
ALTER TABLE document_link DROP CONSTRAINT document_link_state_check;
ALTER TABLE document_link ADD CONSTRAINT document_link_state_check CHECK (state IN ('valid','malformed'));
