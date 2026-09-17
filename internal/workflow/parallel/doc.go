// Package parallel executes bounded PARALLEL branches and joins their
// outcomes (owner: workflow-runtime; phase: P1A/P1B boundary; WF-STEP-007,
// WF-STEP-008).
//
// # Admission runs before any branch does
//
// [Execute] validates the whole [Spec] first: a positive MaxBranches bound
// the branch count fits inside, unique non-empty branch identities and
// idempotency keys, every branch admitted with non-nil work, non-negative
// costs inside the budget, disjoint declared write keys and one declared
// failure policy. An unbounded count, a conflicting write, an exceeded
// budget or an undefined failure or cancel propagation is refused before
// any branch runs, so a rejected batch leaves no partial outcome behind.
//
// # Two failure policies, exact terminal vocabulary
//
// Admitted branches run concurrently under the caller's context. Under
// [FailFast] the first failure cancels its siblings, and a sibling that
// observes that cancellation reports [OutcomeCancelled] rather than
// manufacturing an outcome; under [CollectAll] every branch runs to its own
// terminal [OutcomeSucceeded], [OutcomeFailed] or [OutcomeCancelled]. The
// [Report] carries every branch result in branch-ID order with the policy
// that produced it.
//
// # Joins aggregate, never coerce
//
// [Join] aggregates branch results under a [JoinPlan] whose strategy and
// version are pinned -- strategy/version is part of the compiled plan, never
// a call-time surprise. ALL, ANY, QUORUM, REQUIRED_SET and BEST_EFFORT
// return a typed [JoinOutcome] with degraded and unknown dimensions: a
// missing mandatory branch or an impossible quorum never reports success,
// and an unknown branch outcome is named, never coerced false.
//
// # What this deliberately is not
//
// This package holds no durable state, no clock and no scheduler. Branches
// are caller-supplied work functions with caller-owned effects; the
// write-key disjointness this package checks is admission evidence, not a
// transaction. WF-COMP-004's compiler analysis owns the static fan-out
// proof; this package owns the run. Phase 1 callers pass only fixed
// compiled branches required by Promotion.
package parallel
