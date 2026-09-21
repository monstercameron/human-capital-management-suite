// Package promotionexec defines the bounded P1B executable promotion
// workflow (owner: workflow-runtime; phase: P1B; PROMO-009).
//
// # The shipped definition
//
// [Definition] is the promotion reference,
// hcmnext.workflows.promotion.execute 1.1.0 (definition version 2): snapshot
// and compensation simulation, band evaluation and the raise threshold,
// finance-partner and current-manager approvals, the effective-date WAIT
// resolved in America/New_York ([EffectiveDateZoneID]), revalidation with a
// re-approval loop, promotion execution, a SIGNAL wait for each provider's
// confirmation (payroll, then identity) before its observation, the
// reconciliation observation, the acknowledgement gate and typed terminals.
// [DefinitionV1_0] is the frozen 1.0.0 graph without the provider waits;
// instances pinned to it keep resuming on it ([CompileV1_0]).
// [HasProviderWaits] tells a step runner which of the two a plan is. [Compile] projects the two
// aliases the kernel compiler does not admit (FIRED and
// REAPPROVED/WITHDRAWN) onto the fixed WAIT and TASK route vocabulary
// before calling workflow.Compile; [CompileSimulation] compiles the same
// graph for the simulate profile. [NodeOrder] and [CapabilityIDs] enumerate
// the graph so callers and tests name nodes and capabilities without
// re-listing them.
//
// # Approval requirements are pinned, authority is rechecked
//
// requirement.go compiles each approval requirement against pinned policy
// and governance refs with an authority floor, quorum one, a decide-by
// deadline with block-on-expiry escalation, separation (neither requester
// nor subject may approve; one requirement per principal) and invalidators
// for material proposal change, revoked authority and expired deadline.
// lifecycle.go keeps the other half of that promise: [ValidateCurrentAuthority]
// compares the authority captured at routing time with the server-resolved
// authority at decision time (WF-STEP-003, PROMOUX-003). A principal
// identity is not an authority class: one person must not satisfy two
// independent classes merely by holding both roles.
//
// # Capabilities are named here, invoked elsewhere
//
// capabilities.go names the capability identities the graph invokes and
// [GovernedReadDefinitions] returns the EXECUTE-mode definitions of the
// read-only capabilities (revalidation and the three observations) a
// composition binds to governed invocations (WF-RUN-034).
//
// # What this deliberately is not
//
// This package defines the plan; it never executes it. Execution belongs to
// internal/workflow/execute composed by internal/platform/execution, and
// domain meaning behind each capability stays with its owning domain.
package promotionexec
