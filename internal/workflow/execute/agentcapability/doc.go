// Package agentcapability runs a published, evaluated agent version as an
// agent-eligible capability inside a workflow (WF-CTRL-005).
//
// A PublishedAgent's own bindings - input and output schemas, data scope,
// tools, budget, effect class and service principal - generate the registry
// manifest, so a CAPABILITY node can name an agent version exactly like any
// other capability. The handler starts one agent run per node cause: retries
// of the node share one cause identifier, hence one run and one charge. The
// run's access is the intersection of the agent's bindings and the workflow
// run's access policy, the result is schema-validated, grounded and
// taint-marked before it becomes the node's output, and the agent verdict
// maps to the capability outcomes (succeeded, rejected, unknown, ambiguous).
//
// By default an agent capability is read and draft only. An agent with
// side-effecting skills states that in its manifest and the compiler refuses
// it unless the path to the node passes a gate.
package agentcapability
