// Package recruit is REV-020-01's replacement for the standalone
// tools/conformance/recruit fixture: a real Recruit/Hire/Onboard reference
// workflow compiled under P1A and walked through the real SIMULATE-mode
// interpreter (internal/workflow/simulate).
//
// Scope note: the todo's GREEN names CAPABILITY, WAIT, SUBWORKFLOW and
// DOCUMENT step types, but the kernel refuses three of the four as node
// types today — simulate.dispatch answers WAIT and SUBWORKFLOW with
// CodeStepNotImplemented, and DOCUMENT is deliberately absent as a node type
// (internal/workflow/steptype.go) with WF-STEP-013 retired. This package
// therefore composes only interpreter-executed primitives (CAPABILITY,
// OBSERVE, TRANSFORM, DECISION, END) plus declared approval requirements,
// and carries the offer-window and document-evidence legs as OBSERVE and
// CAPABILITY state instead of unexecutable node types. Every blocked or
// degraded outcome below is derived from the interpreter's
// proposal/approval/reservation/observation state, never asserted directly.
package recruit
