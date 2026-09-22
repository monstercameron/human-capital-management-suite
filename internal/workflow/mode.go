package workflow

import (
	"sort"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
)

// This file owns the two WF-EXT-003 compiler behaviors that used to live in
// promotionexec: outcome-alias canonicalization (canonicalEdges) and the
// SIMULATE projection (compilerDefinition/ProjectMode). A node declares
// business outcome aliases and a SIMULATE mode overlay; the compiler applies
// both, so every definition reuses them instead of re-implementing Promotion
// by hand.

// declaredEffectClass returns the effect class a node declares before any
// capability manifest is consulted: the author's declaration, else the step
// type's own nature. Overlay declarations are validated against it; the
// SIMULATE projection itself reads the resolved class, where the manifest is
// the source of effect truth.
func declaredEffectClass(n *Node) capability.EffectClass {
	if n.DeclaredEffect != "" {
		return n.DeclaredEffect
	}
	switch n.Type {
	case StepDecision, StepTransform, StepEnd, StepWait, StepSignal:
		return capability.EffectPure
	case StepObserve:
		return capability.EffectReadOnly
	default:
		return capability.EffectInternalMutation
	}
}

// checkOutcomeAliases proves a node's alias declarations are well formed: a
// business name maps onto exactly one kernel outcome of its own step type,
// never onto nothingness by accident and never over a route the node already
// produces. The graph pass reports what an alias rewrites to; this reports
// what it may rewrite to.
func checkOutcomeAliases(n *Node, conf Conformance, c *collector) {
	if len(n.OutcomeAliases) == 0 {
		return
	}
	loc := Location{NodeID: n.ID, Field: "outcome_aliases"}
	kernel := map[string]bool{}
	for _, o := range conf.Outcomes {
		kernel[string(o)] = true
	}
	routable := map[string]bool{}
	for _, key := range expectedRoutes(n) {
		routable[key] = true
	}
	aliases := make([]string, 0, len(n.OutcomeAliases))
	for alias := range n.OutcomeAliases {
		aliases = append(aliases, alias)
	}
	// sort.Strings, not sortedStrings: an empty alias name is itself the
	// defect this reports, and sortedStrings silently drops it.
	sort.Strings(aliases)
	for _, alias := range aliases {
		target := n.OutcomeAliases[alias]
		switch {
		case alias == "":
			c.add(CodeInvalidDefinition, loc, "an outcome alias has no business name")
		case alias == target:
			c.add(CodeInvalidDefinition, loc, "outcome alias %q maps to itself; a kernel outcome needs no alias", alias)
		case routable[alias]:
			c.add(CodeInvalidDefinition, loc, "outcome alias %q shadows the %s outcome %q; alias only business names", alias, n.Type, alias)
		case target != "" && !kernel[target]:
			c.add(CodeInvalidDefinition, loc, "outcome alias %q maps to %q, which is not a %s outcome", alias, target, n.Type)
		}
	}
}

// checkModeOverlay proves a node's SIMULATE overlay target is well formed:
// a declared write suppresses to a read. Whether the node actually mutates
// is judged on its resolved class in the effects pass, where the capability
// manifest is available; this reports only a target that suppresses nothing,
// in every mode including EXECUTE.
func checkModeOverlay(n *Node, c *collector) {
	if n.ModeOverlay == nil {
		return
	}
	if declaredEffectClass(n).IsWrite() && !n.ModeOverlay.suppresses() {
		c.add(CodeInvalidDefinition, Location{NodeID: n.ID, Field: "mode_overlay"},
			"a SIMULATE overlay suppresses to PURE or READ_ONLY, got %q", string(n.ModeOverlay.SimulateEffect))
	}
}

// canonicalizeEdges rewrites every edge through its source node's declared
// outcome aliases and drops alias-to-nothing edges, deduplicating exact
// repeats. A definition without aliases compiles exactly as authored: this is
// a no-op for every graph that never needed Promotion's helpers.
func canonicalizeEdges(def *Definition) []Edge {
	byID := def.nodeIndex()
	seen := map[string]bool{}
	out := make([]Edge, 0, len(def.Edges))
	for _, edge := range def.Edges {
		key := edge.RouteKey
		if n := byID[edge.From]; n != nil && len(n.OutcomeAliases) > 0 {
			if target, ok := n.OutcomeAliases[key]; ok {
				if target == "" {
					continue
				}
				key = target
			}
		}
		edge.RouteKey = key
		dedupe := edge.From + "\x00" + edge.To + "\x00" + edge.RouteKey
		if seen[dedupe] {
			continue
		}
		seen[dedupe] = true
		out = append(out, edge)
	}
	return out
}

// projectSimulate derives the SIMULATE projection from declared effect
// classes: every node whose resolved class writes must carry a suppressing
// mode overlay, which is applied — cleared effect role and SIMULATE operation
// mode — while reads pass through untouched. The manifest is the source of
// effect truth, so a capability that resolves to a read needs no overlay
// even when its step type defaults to a write. It returns the ids it
// suppressed so capability records can follow. The definition's own nodes
// are never mutated: suppression works on a copy.
func projectSimulate(def *Definition, records map[string]capability.Record, c *collector) map[string]bool {
	def.Nodes = append([]Node(nil), def.Nodes...)
	overlaid := map[string]bool{}
	for i := range def.Nodes {
		n := &def.Nodes[i]
		class := effectClassOf(n, records)
		if !class.IsWrite() {
			continue
		}
		if !n.ModeOverlay.suppresses() {
			loc := Location{NodeID: n.ID, Field: "mode_overlay"}
			if n.ModeOverlay == nil {
				c.add(CodeMutationInSimulation, loc,
					"%s carries %s with no SIMULATE mode overlay; a write without an overlay cannot be suppressed",
					n.Type, class)
			} else {
				c.add(CodeMutationInSimulation, loc,
					"%s carries %s but its overlay suppresses to %q, not to a read",
					n.Type, class, string(n.ModeOverlay.SimulateEffect))
			}
			continue
		}
		n.DeclaredEffect = n.ModeOverlay.SimulateEffect
		// A read-only projection mutates nothing, so it has no core to
		// classify (WF-RUN-037).
		n.EffectRole = ""
		if n.Capability != nil {
			ref := *n.Capability
			ref.OperationMode = ModeSimulate
			n.Capability = &ref
		}
		overlaid[n.ID] = true
	}
	return overlaid
}

// downgradeSimulateRecords follows a SIMULATE projection into the capability
// manifests: a suppressed node's record reports the read it became, so the
// manifest stays the source of effect truth instead of contradicting the
// projection. Records of nodes the projection did not suppress are untouched,
// so their diagnostics still fire.
func downgradeSimulateRecords(def *Definition, overlaid map[string]bool, records map[string]capability.Record) {
	if len(overlaid) == 0 {
		return
	}
	for i := range def.Nodes {
		n := &def.Nodes[i]
		if !overlaid[n.ID] {
			continue
		}
		rec, ok := records[n.ID]
		if !ok {
			continue
		}
		if rec.Definition.EffectClass.IsWrite() {
			rec.Definition.EffectClass = n.DeclaredEffect
			records[n.ID] = rec
		}
	}
}
