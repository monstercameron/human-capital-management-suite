-- +goose Up
-- Search prefilter trigram index (CHATBUG-096): the prefilter is a
-- leading-wildcard ILIKE that must stay a superset of substring Match, so
-- only a trigram GIN index can serve it; the chat_post_search tsvector
-- index cannot without changing search semantics.
-- Install into public and schema-qualify the opclass: test/tenant
-- connections run with search_path=<tenant schema> only, so an
-- unqualified install is invisible to every other schema once any one
-- schema (or the shared template) already has the extension.
CREATE EXTENSION IF NOT EXISTS pg_trgm SCHEMA public;
CREATE INDEX chat_post_body_trgm ON chat_post USING gin (body public.gin_trgm_ops);

-- +goose Down
DROP INDEX chat_post_body_trgm;
