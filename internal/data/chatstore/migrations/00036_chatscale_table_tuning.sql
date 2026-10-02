-- +goose NO TRANSACTION
-- +goose Up
-- Metadata-only changes, with a short lock deadline rather than waiting behind
-- a live send. Fillfactor reserves space for the fixture's one-in-seventeen
-- edits; it applies as pages are written, without rewriting existing pages.
SET lock_timeout = '2s';
ALTER TABLE chat_post SET (fillfactor=90,autovacuum_vacuum_scale_factor=0.02,autovacuum_vacuum_threshold=1000,autovacuum_analyze_scale_factor=0.01,autovacuum_analyze_threshold=1000);
ALTER TABLE chat_conversation SET (fillfactor=80,autovacuum_vacuum_scale_factor=0.02,autovacuum_vacuum_threshold=100,autovacuum_analyze_scale_factor=0.02,autovacuum_analyze_threshold=100);
ALTER TABLE chatscale_read_state SET (fillfactor=80,autovacuum_vacuum_scale_factor=0.02,autovacuum_vacuum_threshold=100,autovacuum_analyze_scale_factor=0.02,autovacuum_analyze_threshold=100);
-- Absolute thresholds keep mutable heads and badges from waiting for twenty
-- percent of a very large relation to die. Append-only revisions need no
-- fillfactor reserve. Receipt churn follows the existing bounded outbox pruner.
ALTER TABLE chat_outbox_receipt SET (autovacuum_vacuum_scale_factor=0.02,autovacuum_vacuum_threshold=1000,autovacuum_analyze_scale_factor=0.01,autovacuum_analyze_threshold=1000);
RESET lock_timeout;

-- +goose Down
SET lock_timeout = '2s';
ALTER TABLE chat_outbox_receipt RESET (autovacuum_vacuum_scale_factor,autovacuum_vacuum_threshold,autovacuum_analyze_scale_factor,autovacuum_analyze_threshold);
ALTER TABLE chatscale_read_state RESET (fillfactor,autovacuum_vacuum_scale_factor,autovacuum_vacuum_threshold,autovacuum_analyze_scale_factor,autovacuum_analyze_threshold);
ALTER TABLE chat_conversation RESET (fillfactor,autovacuum_vacuum_scale_factor,autovacuum_vacuum_threshold,autovacuum_analyze_scale_factor,autovacuum_analyze_threshold);
ALTER TABLE chat_post RESET (fillfactor,autovacuum_vacuum_scale_factor,autovacuum_vacuum_threshold,autovacuum_analyze_scale_factor,autovacuum_analyze_threshold);
RESET lock_timeout;
