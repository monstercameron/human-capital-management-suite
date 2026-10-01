// Package agentcontext assembles server-resolved authority and provenance for
// agent runs, and requires a final owner recheck before protected delivery.
// Callers supply typed snapshots from owning services; chat claims are never
// accepted as tenant, role, purpose, policy, or grant authority.
package agentcontext
