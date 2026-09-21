package workflow

import "sort"

// OutputBindingCandidate is one node output that the compiler permits as the
// source of a target input. It is an authoring projection of the same graph
// dominance and ValueType assignability rules used by checkMappings, not a
// second approximation of those rules.
type OutputBindingCandidate struct {
	NodeID string
	Path   string
	Type   ValueType
}

// OutputBindingCandidates returns only outputs whose producer is a strict
// dominator of targetNodeID and whose declared type is assignable to the
// target input. An invalid target identity or input path yields no choices.
func OutputBindingCandidates(def Definition, targetNodeID, targetPath string) []OutputBindingCandidate {
	target := def.nodeIndex()[targetNodeID]
	if target == nil {
		return nil
	}
	want, ok := fieldsByPath(target.Inputs)[targetPath]
	if !ok {
		return nil
	}
	c := &collector{}
	g := analyzeGraph(&def, c)
	result := make([]OutputBindingCandidate, 0)
	for _, nodeID := range g.sortedNodeIDs() {
		if !g.dominates(nodeID, targetNodeID) {
			continue
		}
		producer := g.nodes[nodeID]
		for _, output := range producer.Outputs {
			if output.Type.AssignableTo(want) != nil {
				continue
			}
			result = append(result, OutputBindingCandidate{NodeID: nodeID, Path: output.Path, Type: output.Type.clone()})
		}
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].NodeID == result[j].NodeID {
			return result[i].Path < result[j].Path
		}
		return result[i].NodeID < result[j].NodeID
	})
	return result
}

// OutcomeRoutes returns the exact routed outcomes the compiler expects for a
// node, including authored DECISION routes.
func OutcomeRoutes(node Node) []string {
	return append([]string(nil), expectedRoutes(&node)...)
}

// OutcomeRouteAllowsMultipleTargets reports whether route is the node type's
// declared fan-out route. Today this is used by PARALLEL authoring; every
// other outcome has exactly one continuation.
func OutcomeRouteAllowsMultipleTargets(node Node, route string) bool {
	conformance, ok := ConformanceFor(node.Type)
	return ok && conformance.FanOutRoute != "" && string(conformance.FanOutRoute) == route
}
