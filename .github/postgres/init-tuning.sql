-- CI-only PostgreSQL tuning for the go-core service container.
--
-- The root-module race run migrates hundreds of per-test schemas, each
-- through the full ~280-migration chain, against one shared server. The
-- stock PostgreSQL defaults (max_locks_per_transaction = 64) exhaust
-- shared lock memory (SQLSTATE 53200) under that fan-out. These settings
-- cost ~15MB of shared memory and change nothing about test semantics.
-- Applied via the /docker-entrypoint-initdb.d mount in tests.yml, which
-- the entrypoint picks up before the server starts serving.
ALTER SYSTEM SET max_locks_per_transaction = 512;
ALTER SYSTEM SET max_connections = 200;
