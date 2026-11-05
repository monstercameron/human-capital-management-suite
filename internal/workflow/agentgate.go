package workflow

// The side-effecting agent gate (WF-CTRL-005-F1).
//
// An agent-backed capability whose skills can change records compiles only
// when every path to its node passes a gate. A gate is an APPROVAL node (a
// person authorizes the path) or a DECISION node (a rule or model decides
// it); RULE is deliberately not a step type, so a rule gate is a DECISION
// carrying the rule reference. TASK, WAIT, SIGNAL and the structural
// primitives never gate: they perform, pause or route work rather than
// authorize the path to a side effect.
//
// The compiler cannot name the agent registry: it lives in
// internal/workflow/execute/agentcapability, which imports this package.
// Compositions that serve agent-backed capabilities therefore pass an
// [AgentGatePolicy] (adapted from the published agent bindings) in
// [Options.AgentGate]. A nil policy skips the check: without the policy the
// compiler cannot tell an agent-backed capability from any other, so there
// is nothing it could faithfully refuse.

// AgentGatePolicy answers one question per capability node: is this an
// agent-backed capability, and if so, does it clear the compile-time gate?
// Unknown capabilities report a nil error: they are not agent-backed, so
// the gate does not apply to them.
type AgentGatePolicy interface {
	CheckAgentGate(id string, version uint32, pathPassesGate bool) error
}

// isGateNode reports whether n authorizes the paths through it.
func isGateNode(n *Node) bool {
	return n.Type == StepApproval || n.Type == StepDecision
}

// pathPassesGate reports whether every path from the start node to id runs
// through a gate. It reads the compiler's own dominator sets: a node that
// dominates the agent node on every path, excluding the agent node itself,
// is exactly a gate on its path. A gate downstream of the node, or on only
// some paths, does not pass.
func pathPassesGate(g *graph, id string) bool {
	for dom := range g.dominators[id] {
		if dom == id {
			continue
		}
		if n, ok := g.nodes[dom]; ok && isGateNode(n) {
			return true
		}
	}
	return false
}

// checkAgentGates refuses every agent-backed capability node whose path
// passes no gate. It runs after graph analysis, so the dominator sets are
// available, and only when the graph is sound: an unsound graph already
// fails compilation, and partial dominators must not invent gate verdicts.
func checkAgentGates(def *Definition, g *graph, opts Options, c *collector) {
	if opts.AgentGate == nil || !g.sound {
		return
	}
	for i := range def.Nodes {
		n := &def.Nodes[i]
		if n.Type != StepCapability || n.Capability == nil {
			continue
		}
		if err := opts.AgentGate.CheckAgentGate(n.Capability.ID, n.Capability.Version, pathPassesGate(g, n.ID)); err != nil {
			c.add(CodeAgentGateRequired, Location{NodeID: n.ID}, "%v", err)
		}
	}
}
