// Package recruit is REV-020-01's replacement for the standalone
// tools/conformance/recruit fixture: a real Recruit/Hire/Onboard reference
// workflow compiled under P1A and walked through the real SIMULATE-mode
// interpreter (internal/workflow/simulate).
//
// Scope note: the REV-020-01 GREEN clause asks for CAPABILITY, WAIT,
// SUBWORKFLOW and DOCUMENT nodes in SIMULATE. The plan authority conflicts
// with three of those requirements: planning/specs/workflow-runtime.md maps
// DOCUMENT to a documents.* CAPABILITY (the mapping exercised by this
// fixture's work-authorization evidence node); planning/todos.md retires
// WF-STEP-013; planning/specs/workflow-runtime.md schedules WAIT for P1B and
// SUBWORKFLOW after P1B evidence; planning/plan.md defers subworkflows. The
// P1A SIMULATE interpreter deliberately refuses WAIT and SUBWORKFLOW. The
// fixture therefore cannot claim the literal REV-020-01 GREEN until the
// conflicting plan clauses are reconciled. Its executable cases use
// CAPABILITY, OBSERVE, TRANSFORM, DECISION and END with declared approval
// requirements. Every blocked or degraded outcome is derived from the
// interpreter's proposal/approval/reservation/observation state, never
// asserted directly.
package recruit
