package workflow

import (
	"sort"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
)

// effectClassOf returns the node's effect class. A bound capability manifest
// is the source of effect truth; only a node with no capability falls back to
// the author's declaration, and then to the step type's own nature
// ([declaredEffectClass]).
func effectClassOf(n *Node, records map[string]capability.Record) capability.EffectClass {
	if rec, ok := records[n.ID]; ok {
		return rec.Definition.EffectClass
	}
	return declaredEffectClass(n)
}

// allowedModesFor returns the execution modes a node with this effect class
// may run in, in canonical order.
func allowedModesFor(class capability.EffectClass) []ExecutionMode {
	switch class {
	case capability.EffectPure, capability.EffectReadOnly:
		return []ExecutionMode{ModeSimulate, ModeExecute, ModeReplay, ModeRepair, ModeShadow}
	case capability.EffectInternalMutation, capability.EffectExternalMutation:
		return []ExecutionMode{ModeExecute, ModeRepair}
	case capability.EffectIrreversibleExternalMutation:
		return []ExecutionMode{ModeExecute}
	default:
		return nil
	}
}

// AdmitsMode reports whether the node's compiled effect class admits mode. A
// node with no compiled modes admits nothing, so every caller's gate -- the
// execute driver, runtime advancement and the simulator -- fails closed on the
// same rule (WF-RUN-040).
func (n CompiledNode) AdmitsMode(mode ExecutionMode) bool {
	return mode != "" && modeAllowed(n.AllowedModes, mode)
}

func modeAllowed(modes []ExecutionMode, want ExecutionMode) bool {
	for _, m := range modes {
		if m == want {
			return true
		}
	}
	return false
}

// effectKeyOf returns the logical effect identity a mutating node produces.
// A read produces none: there is nothing to deduplicate.
func effectKeyOf(n *Node, records map[string]capability.Record) string {
	class := effectClassOf(n, records)
	if !class.IsWrite() || n.Capability == nil {
		return ""
	}
	key := n.Capability.Key().String()
	if n.Capability.EffectBinding != "" {
		key += "#" + n.Capability.EffectBinding
	}
	if n.Capability.IdempotencyKeyMapping != "" {
		key += "@" + n.Capability.IdempotencyKeyMapping
	}
	return key
}

// analyzeEffects classifies every planned action, proves retried mutations are
// idempotent, proves irreversible effects are observed and repairable, and
// computes the execution modes the plan actually supports (WF-COMP-003).
func analyzeEffects(
	def *Definition,
	g *graph,
	records map[string]capability.Record,
	opts Options,
	overlaid map[string]bool,
	c *collector,
) EffectSummary {
	summary := EffectSummary{
		ZeroEffect:   true,
		NodesByClass: map[string][]string{},
	}
	planModes := allowedModesFor(capability.EffectPure)

	for _, id := range g.sortedNodeIDs() {
		n := g.nodes[id]
		loc := Location{NodeID: id}
		class := effectClassOf(n, records)
		if !class.Valid() {
			c.add(CodeEffectDeclarationConflict, loc,
				"effect class %q is not one of the five declared classes", string(class))
			continue
		}
		// An overlay on a node that mutates nothing is a declaration without a
		// subject. The resolved class decides: the manifest is the source of
		// effect truth, so a read-by-manifest capability needs no overlay
		// even when its step type defaults to a write (WF-EXT-003). Nodes the
		// SIMULATE projection suppressed read as reads by design and are not
		// dead declarations.
		if n.ModeOverlay != nil && !class.IsWrite() && !overlaid[id] {
			c.add(CodeInvalidDefinition, Location{NodeID: id, Field: "mode_overlay"},
				"a SIMULATE overlay on %s resolves to %s, which mutates nothing; only a write effect declares one", n.Type, class)
		}
		summary.NodesByClass[string(class)] = append(summary.NodesByClass[string(class)], id)

		if rec, ok := records[id]; ok && n.DeclaredEffect != "" && n.DeclaredEffect != rec.Definition.EffectClass {
			c.add(CodeEffectDeclarationConflict, loc,
				"node declares %s but capability %s declares %s; the manifest is the source of effect truth",
				n.DeclaredEffect, rec.Definition.Key(), rec.Definition.EffectClass)
		}
		if conf, ok := ConformanceFor(n.Type); ok && !conf.MayCarryEffect && class.IsWrite() {
			c.add(CodeEffectDeclarationConflict, loc,
				"%s never carries a write effect, but this node resolves to %s", n.Type, class)
		}

		if class.IsWrite() {
			summary.ZeroEffect = false
			if key := effectKeyOf(n, records); key != "" {
				summary.EffectKeys = append(summary.EffectKeys, key)
			}
			if opts.requiresZeroEffect() {
				c.add(CodeWriteEffectRefusedP1A, loc,
					"%s declares %s; a %s plan compiles to zero effect",
					n.Type, class, opts.phase())
			}
		}
		if class == capability.EffectIrreversibleExternalMutation {
			summary.IrreversibleNodes = append(summary.IrreversibleNodes, id)
		}

		checkRetrySafety(n, records, class, c)
		checkEffectObservation(g, n, class, c)

		modes := allowedModesFor(class)
		// A write with a suppressing mode overlay declares SIMULATE support
		// honestly: the SIMULATE projection applies the overlay, so the
		// EXECUTE compilation has nothing to refuse (WF-EXT-003). The
		// projection pass owns SIMULATE-mode diagnostics and reports a write
		// it cannot suppress itself.
		if def.declaresMode(ModeSimulate) && !opts.SimulateProjection && !modeAllowed(modes, ModeSimulate) && !n.ModeOverlay.suppresses() {
			c.add(CodeMutationInSimulation, loc,
				"definition declares SIMULATE support but %s carries %s, which cannot be suppressed",
				n.Type, class)
		}
		planModes = intersectModes(planModes, modes)
	}

	summary.AllowedModes = planModes
	summary.EffectKeys = sortedStrings(summary.EffectKeys)
	sort.Strings(summary.IrreversibleNodes)
	for k := range summary.NodesByClass {
		sort.Strings(summary.NodesByClass[k])
	}
	return summary
}

// checkRetrySafety refuses a retried mutation that has no compatible
// idempotency. A retry without stable effect identity is a duplicate payment,
// not a resilience feature.
func checkRetrySafety(n *Node, records map[string]capability.Record, class capability.EffectClass, c *collector) {
	if n.Retry == nil || n.Retry.MaxAttempts <= 1 || !class.IsWrite() {
		return
	}
	loc := Location{NodeID: n.ID, Field: "retry"}
	if class == capability.EffectIrreversibleExternalMutation {
		c.add(CodeNonIdempotentRetry, loc,
			"an irreversible external mutation is never retried automatically; route ambiguity to observation and repair")
		return
	}
	rec, ok := records[n.ID]
	if !ok || rec.Definition.IdempotencyPolicyRef == "" {
		c.add(CodeNonIdempotentRetry, loc,
			"retried mutation has no capability idempotency policy")
		return
	}
	if n.Capability == nil || n.Capability.IdempotencyKeyMapping == "" {
		c.add(CodeNonIdempotentRetry, loc,
			"retried mutation declares no idempotency key mapping, so retries cannot share one effect identity")
	}
}

// checkEffectObservation refuses an external or irreversible effect nobody
// observes and nothing repairs. An HTTP 200 is not an outcome.
func checkEffectObservation(g *graph, n *Node, class capability.EffectClass, c *collector) {
	if class != capability.EffectExternalMutation && class != capability.EffectIrreversibleExternalMutation {
		return
	}
	loc := Location{NodeID: n.ID}
	observed := false
	for _, id := range g.sortedNodeIDs() {
		if g.nodes[id].Type == StepObserve && g.reaches(n.ID, id) {
			observed = true
			break
		}
	}
	if !observed {
		c.add(CodeUnobservedEffect, loc,
			"%s produces %s but no OBSERVE node is reachable from it", n.Type, class)
	}
	if n.FailureRoute == "" {
		c.add(CodeUnobservedEffect, loc,
			"%s produces %s and must declare an explicit failure/repair route", n.Type, class)
	}
}

func intersectModes(a, b []ExecutionMode) []ExecutionMode {
	if b == nil {
		return nil
	}
	keep := map[ExecutionMode]bool{}
	for _, m := range b {
		keep[m] = true
	}
	out := make([]ExecutionMode, 0, len(a))
	for _, m := range a {
		if keep[m] {
			out = append(out, m)
		}
	}
	return out
}
