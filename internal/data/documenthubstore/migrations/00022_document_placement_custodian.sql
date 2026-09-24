-- HUB-014: official placement custodian. document_deployment already
-- carries review_due_at (added empty by HUB-007's 00004 migration, never
-- populated by any writer yet); this migration adds the paired custodian
-- column so PlaceDocument (HUB-014) can bind both to one placement
-- scope_kind='placement' deployment row. The default '' preserves the
-- previous generic placement behaviour, which never set either column, so
-- no existing deployment row or caller is affected.
-- +goose Up
ALTER TABLE document_deployment ADD COLUMN custodian_id text NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE document_deployment DROP COLUMN custodian_id;
