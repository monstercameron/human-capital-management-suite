package wait

import (
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

// FromCompiled builds this package's own [CompiledWaitNode] view from a
// compiled WAIT node.
//
// internal/workflow's compiler now carries a WAIT-specific binding
// (internal/workflow/definition.go's WaitSpec, compiled onto
// [workflow.CompiledNode.Wait] by internal/workflow/compile.go); this is the
// one place that translates its plain wire strings into the typed
// internal/kernel/values this package's pure functions consume, through the
// exact parsers those values expose for their own wire vocabulary. It reads
// no ambient state and no clock.
//
// A [workflow.CompiledNode] carries no workflow identity of its own — that
// lives on the [workflow.CompiledWorkflow] plan it came from — so the
// returned node's WorkflowID and WorkflowVersion are left zero; a caller that
// has the plan in hand sets them before calling [ComputeTimerRequirement],
// whose own [CompiledWaitNode.Validate] enforces they are present.
func FromCompiled(node *workflow.CompiledNode) (CompiledWaitNode, error) {
	if node == nil {
		return CompiledWaitNode{}, fmt.Errorf("wait: FromCompiled: node is nil")
	}
	if node.Type != workflow.StepWait {
		return CompiledWaitNode{}, fmt.Errorf("wait: FromCompiled: node %q is %s, not WAIT", node.ID, node.Type)
	}
	w := node.Wait
	if w == nil {
		return CompiledWaitNode{}, fmt.Errorf("wait: FromCompiled: node %q carries no WAIT binding", node.ID)
	}

	out := CompiledWaitNode{
		NodeID:   node.ID,
		Zone:     values.ZoneRef{ID: w.ZoneID, TzdbVersion: w.ZoneTzdbVersion},
		Calendar: values.CalendarRef{Ref: w.CalendarRef, Version: w.CalendarVersion},
	}

	policy, err := values.ParseReferenceUpdatePolicy(w.ReferenceUpdatePolicy)
	if err != nil {
		return CompiledWaitNode{}, fmt.Errorf("wait: FromCompiled: node %q: reference_update_policy: %w", node.ID, err)
	}
	out.Policy = policy

	switch w.WakeKind {
	case workflow.WaitWakeAtInstant:
		var inst values.Instant
		if err := inst.UnmarshalText([]byte(w.WakeInstant)); err != nil {
			return CompiledWaitNode{}, fmt.Errorf("wait: FromCompiled: node %q: wake_instant: %w", node.ID, err)
		}
		out.WakeInstant = inst

	case workflow.WaitWakeAtLocalDate, workflow.WaitWakeAtLocalDateTime:
		date, err := values.ParseLocalDate(w.WakeLocalDate)
		if err != nil {
			return CompiledWaitNode{}, fmt.Errorf("wait: FromCompiled: node %q: wake_local_date: %w", node.ID, err)
		}
		out.WakeLocalDate = date

		if w.WakeKind == workflow.WaitWakeAtLocalDateTime {
			t, err := values.ParseLocalTime(w.WakeLocalTime)
			if err != nil {
				return CompiledWaitNode{}, fmt.Errorf("wait: FromCompiled: node %q: wake_local_time: %w", node.ID, err)
			}
			out.WakeLocalTime = t
		}

		disamb, err := values.ParseDisambiguation(w.Disambiguation)
		if err != nil {
			return CompiledWaitNode{}, fmt.Errorf("wait: FromCompiled: node %q: disambiguation: %w", node.ID, err)
		}
		out.Disambiguation = disamb

	case workflow.WaitWakeAtTimeExpr:
		if w.TimeExpr == nil {
			return CompiledWaitNode{}, fmt.Errorf("wait: FromCompiled: node %q: AT_TIME_EXPR carries no time expression", node.ID)
		}
		expr, err := translateTimeExpr(w.TimeExpr, out.Zone, out.Calendar)
		if err != nil {
			return CompiledWaitNode{}, fmt.Errorf("wait: FromCompiled: node %q: %w", node.ID, err)
		}
		out.WakeTimeExpr = expr

	default:
		return CompiledWaitNode{}, fmt.Errorf("wait: FromCompiled: node %q: wake_kind %q is not declared", node.ID, w.WakeKind)
	}

	// reference() and its Validate() are the same dataset-identity proof
	// CompiledWaitNode.Validate runs; called directly (this file is in
	// package wait) because the WorkflowID half of that method's own check
	// cannot be satisfied from a lone CompiledNode.
	if err := out.reference().Validate(); err != nil {
		return CompiledWaitNode{}, fmt.Errorf("wait: FromCompiled: node %q: %w", node.ID, err)
	}

	return out, nil
}

// translateTimeExpr builds this package”s typed time expression from its
// wire form. Zone and calendar are the node-level halves a WAIT wake
// inherits; the spec”s own halves stay empty by validation and are ignored
// here. Shape validation stays downstream in CompiledWaitNode.Validate;
// only wire parsing failures surface here.
func translateTimeExpr(spec *workflow.TimeExprSpec, zone values.ZoneRef, cal values.CalendarRef) (values.TimeExpr, error) {
	kind, err := values.ParseTimeExprOffsetKind(spec.OffsetKind)
	if err != nil {
		return values.TimeExpr{}, fmt.Errorf("time_expr: %w", err)
	}
	expr := values.TimeExpr{
		AnchorPath:    spec.AnchorPath,
		OffsetKind:    kind,
		OffsetDays:    spec.OffsetDays,
		OffsetSeconds: spec.OffsetSeconds,
		Calendar:      cal,
		Zone:          zone,
	}
	if spec.LocalTime != "" {
		lt, err := values.ParseLocalTime(spec.LocalTime)
		if err != nil {
			return values.TimeExpr{}, fmt.Errorf("time_expr: local_time: %w", err)
		}
		expr.WakeLocalTime = lt
	}
	if spec.Disambiguation != "" {
		disamb, err := values.ParseDisambiguation(spec.Disambiguation)
		if err != nil {
			return values.TimeExpr{}, fmt.Errorf("time_expr: disambiguation: %w", err)
		}
		expr.Disambiguation = disamb
	}
	if spec.StatuteRulePack != "" || spec.Statute != "" {
		expr.Statute = &values.StatuteRef{
			RulePack: spec.StatuteRulePack,
			Release:  spec.StatuteRelease,
			Statute:  spec.Statute,
		}
	}
	return expr, nil
}
