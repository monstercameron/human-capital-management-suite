-- +goose NO TRANSACTION
-- +goose Up
-- The receipt foreign key references outbox_id alone. Its tenant-leading
-- primary key cannot bound PostgreSQL's cascade lookup when a parent event is
-- pruned. A bounded parent batch otherwise scans all receipts for each event.
DROP INDEX CONCURRENTLY IF EXISTS chatscale_receipt_parent;
CREATE INDEX CONCURRENTLY chatscale_receipt_parent ON chat_outbox_receipt(outbox_id);

-- +goose Down
DROP INDEX CONCURRENTLY IF EXISTS chatscale_receipt_parent;
