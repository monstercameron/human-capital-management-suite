// Package capabilityrunner is WF-EXT-005's generic CAPABILITY and OBSERVE
// runner for the durable path: it resolves a compiled node's input mappings,
// invokes the capability gateway with the resolved mapping as the typed
// request, and stores the typed response as the node's output digest.
//
// It mirrors internal/workflow/simulate's runCapability admission path
// (version resolution, authorization decision, required scope) without
// importing the simulator: the durable driver consumes this runner through
// execute.StepRunner. Write-effect admission through a governed envelope and
// durable artifact persistence of typed outputs remain follow-ups owned by
// the gateway write policy and WF-EXT-004; context-kind mappings fail closed
// naming that todo.
package capabilityrunner
