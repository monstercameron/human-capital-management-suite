// Package testcontainerskit records and checks LIB-009's test-only
// Testcontainers admission boundary. The current decision is REJECT, so no
// runtime or test module graph is changed. Its Integration matrix test proves
// only release dependency graph exclusion; actual digest-pinned PostgreSQL,
// S3, SMTP, and provider-fake executions remain explicitly unproven.
package testcontainerskit
