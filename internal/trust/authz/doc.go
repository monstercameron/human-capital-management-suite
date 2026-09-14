// Package authz implements the P1A bootstrap authorization plane described in
// [the organization scope and authorization contract]: tenant and
// organization scope resolution, record/population/relationship
// authorization, field-level and purpose-bound authorization, and the
// explainable decision that ties them together.
//
// Semantic owner: governance-and-trust. Phase: P1A. Todo: TRUST-008,
// TRUST-009, TRUST-010, TRUST-011.
//
// This package consumes a [trust.Principal] and caller-supplied projections
// (organization edges, sharing grants, relationship facts). It does not read
// a database, call a network service, or own the lifecycle of the
// organization graph, the relationship graph, or the field classification
// registry that a later policy engine will source from MODEL-023: those
// remain someone else's domain, and this package would rather fail closed on
// a stale or missing projection than invent one.
//
// Policy is the [PolicyTable], a compiled-in table for six P1A role
// templates (worker self, manager, HR partner, compensation admin, auditor).
// A later policy engine plugs in behind the same [Evaluate] entry point;
// nothing downstream of this package should need to change when it does.
//
// [Evaluate] composes three independently testable stages, in order:
//
//   - [ResolveTenantScope] (TRUST-008): is the resource's tenant and
//     organization reachable from the principal at all, and under what
//     mandatory, non-delegable restrictions.
//   - [ResolveAuthorizationScope] (TRUST-009): does a bitemporal,
//     source-attributed relationship (self, manager chain, HR partner
//     population, or an administrative role grant) authorize this specific
//     record.
//   - [ResolveFields] (TRUST-010): which of the requested fields may be
//     read under the declared purpose, and whether a restricted value is
//     denied outright, redacted, or withheld because the record itself is
//     not disclosable.
//
// Every decision returned by [Evaluate] is a [Decision] (TRUST-011): it
// carries the rule IDs that fired, the policy versions evaluated, a digest of
// its inputs, and a durable evidence identifier, and [Decision.Explain]
// renders a redaction-safe summary that never reproduces a protected value or
// otherwise discloses whether a denied subject exists. [Enforce] and
// [Simulate] are the same pure evaluation under two names: there is exactly
// one evaluator, so a policy simulation can never drift from what enforcement
// actually does.
package authz
