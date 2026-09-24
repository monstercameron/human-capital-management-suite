// Package capabilityrunner is WF-EXT-005's generic CAPABILITY and OBSERVE
// runner for the durable path: it resolves a compiled node's input mappings,
// invokes the capability gateway with the resolved mapping as the typed
// request, and returns the typed response for durable output-artifact storage.
//
// It mirrors internal/workflow/simulate's runCapability admission path
// (version resolution, authorization decision, required scope) without
// importing the simulator: the durable driver consumes this runner through
// execute.StepRunner. Required attestation obligations are revalidated before
// gateway invocation, and context-kind mappings fail closed when the durable
// context artifact is unavailable.
package capabilityrunner
