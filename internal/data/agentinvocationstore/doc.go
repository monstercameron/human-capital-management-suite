// Package agentinvocationstore persists tenant-isolated persona mention
// invocations in the independent agent database. Claims are idempotent by
// (tenant, post, persona), and authority fields are immutable after claim.
package agentinvocationstore
