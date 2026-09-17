// Package workflow implements the P1A workflow definition model, compiler and
// step-conformance harness (owner: workflow-runtime; phase: P1A; WF-COMP-001,
// WF-COMP-002, WF-COMP-003, WF-COMP-005, WF-STEP-001, WF-STEP-002,
// WF-STEP-010, WF-STEP-014, WF-STEP-017).
//
// # Boundary
//
// planning/specs/workflow-runtime.md draws the governing line:
//
//	The workflow kernel coordinates typed behavior. Capability services own
//	domain semantics and deterministic effects.
//
// This package therefore never interprets payroll, compensation, legal or IAM
// semantics. It resolves references, proves the graph, classifies effects and
// binds governance insertion points; the meaning behind each
// [capability.Definition] stays with its owning domain, and the manifest — not
// the workflow author — is the source of effect truth.
//
// # What is here and what is not
//
// This is the definition/compiler half of the kernel. The durable runtime
// (instances, nodes, leases, timers, signals, the scheduler) is P1B and is
// gated behind the build-or-adopt decision WF-RUN-000; nothing in this package
// executes a node. [Compile] turns a draft [Definition] into an immutable
// [CompiledWorkflow] whose [CompiledWorkflow.Digest] is stable for identical
// input, so a runtime can pin exactly what it is running.
//
// # Kernel vocabulary
//
// The runtime has ten core primitives and three structural ones. P1A compiles
// five of them — CAPABILITY, DECISION, TRANSFORM, OBSERVE and END — and binds
// WAIT and SIGNAL structurally (`WaitSpec`/`SignalSpec` with explicit routes;
// durable suspension stays P1B, see WF-STEP-005). [Options.Phase] refuses the
// rest rather than pretending to support them.
// CHECKPOINT, RULE, AGENT and DOCUMENT are not step types: a safe point is a
// node attribute the compiler places, a rule is a DECISION with a rule
// reference, an agent is a CAPABILITY whose manifest declares agent
// eligibility, and a document is a capability in the documents namespace.
//
// # Failing closed
//
// Compilation reports every diagnostic it finds as a [Diagnostics] set with
// stable [Error] codes and node/edge/mapping source locations, and produces no
// plan at all when any diagnostic is present. There is no partially valid
// plan: an unreachable node, an implicit first edge, a retried mutation
// without compatible idempotency, an irreversible effect nobody observes, a
// terminal that collapses a degraded observation into success, or a sixth
// lifecycle dimension all stop publication.
package workflow
