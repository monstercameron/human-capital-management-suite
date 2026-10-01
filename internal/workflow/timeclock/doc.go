// Package timeclock defines the product time workflows (WTIME-002, WTIME-003,
// WTIME-005, WTIME-006, WTIME-008): one template per timeprofile.Template plus
// the shared period timecard, each an ordinary workflow.Definition built from
// the closed kernel's existing primitives. No node type is added.
//
// # The six plans
//
//	time.punch_session       one run per clocking session (WTIME-003)
//	time.duration_timesheet  daily duration lines for one period
//	time.exception_period    scheduled pattern plus reported deviations
//	time.contractor_invoice  contractor time to invoice; no control classes
//	time.agency_vms          agency temp time to the hirer's VMS
//	time.period_timecard     folds sessions, lines or exceptions and hands off
//	                         by destination (WTIME-005)
//
// The first three end by emitting the period-collect obligation that the
// period timecard run collects. Contractor and agency templates carry their
// own client or hirer approval and delivery, because their approval and
// destination semantics differ and because a contractor plan may not carry
// the break attestation the period timecard asks an employee for.
//
// # Where the engine is short
//
// The engine lacks several features the planning text assumes. Each template
// expresses the intended behaviour in the closest supported form and names the
// gap with a constant (see gaps.go): typed multi-accept SIGNAL correlation
// (WF-EXT-014) is a SIGNAL inside a declared, DECISION-guarded cycle; TimeExpr
// WAIT (WF-EXT-012) is a fixed placeholder wake condition the timer factory
// substitutes; spawn and reducing joins (WF-EXT-019/020) are a collect
// CAPABILITY and a folding TRANSFORM; the vendor round trip (WF-EXT-022) is
// dispatch, correlated SIGNAL and bounded OBSERVE in sequence; and plan
// resolution by profile (WF-EXT-008) is [ResolvePlan] plus the candidate
// registration's match predicate.
//
// # Simulation
//
// internal/workflow/simulate refuses SIGNAL, WAIT and write-effect nodes (they
// are gated behind WF-RUN-000), so it cannot walk these plans. [Simulate]
// walks a compiled plan through the engine's own pure frontier arithmetic
// (internal/workflow/frontier.Advance) with zero effects: DECISION and
// TRANSFORM nodes run the real evaluators in this package, and every other
// outcome comes from a data fixture. The frontier refuses any route the plan
// did not declare, so a walk that completes is a walk the runtime would take.
package timeclock
