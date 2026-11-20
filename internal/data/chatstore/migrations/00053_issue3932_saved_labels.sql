-- +goose Up
-- Issue 3932: a person's private labels for their own saved items. The labels
-- live on the saved row, so the row's tenant_isolation policy (tenant, home
-- tenant, person) and EraseSavedPerson cover them without a second table.
ALTER TABLE chat_saved_item ADD COLUMN labels text[] NOT NULL DEFAULT '{}'
    CHECK (cardinality(labels) <= 10);

-- +goose Down
ALTER TABLE chat_saved_item DROP COLUMN labels;
