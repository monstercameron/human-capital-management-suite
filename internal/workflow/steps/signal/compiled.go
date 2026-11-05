package signal

import (
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

// InstanceContext carries the per-workflow-instance values a compiled SIGNAL
// node needs but a definition cannot know: which tenant and running instance
// is waiting, the correlation value the node's declared correlation key
// expression resolved to for that instance, and when the subscription
// closes. A compiled plan is shared across every instance of a workflow
// version; these values are not.
type InstanceContext struct {
	Tenant             values.TenantId
	WorkflowInstanceID string
	// CorrelationValue is the value an inbound signal must carry on
	// [SignalSubscription.CorrelationKey] to match this instance. Evaluating
	// the node's declared correlation key expression against the instance's
	// data is the caller's job; this package only carries the result.
	CorrelationValue string
	// ClosesAt, when set, is the instant after which a signal is late. Unset
	// means the subscription never closes on its own. A caller that has
	// [workflow.CompiledSignal.CloseAfterSeconds] and the node's entry
	// instant computes this before calling [FromCompiled]; resolving it here
	// would mean reading the clock, which this package's conformance
	// forbids. It is mutually exclusive with a node close time expression.
	ClosesAt values.Instant
	// Anchor, Calendars and Dataset resolve a node close time expression.
	// They are required exactly when the compiled node carries
	// CloseTimeExpr and ignored otherwise. Anchor is the evaluated anchor
	// path for this instance; Calendars is the pinned calendar dataset.
	Anchor    values.Instant
	Calendars values.CalendarDataset
	Dataset   values.DatasetVersions
}

// FromCompiled builds a [SignalSubscription] from a compiled SIGNAL node and
// the per-instance context a definition cannot carry on its own.
//
// internal/workflow's compiler now carries a SIGNAL-specific binding
// (internal/workflow/definition.go's SignalSpec, compiled onto
// [workflow.CompiledNode.Signal] by internal/workflow/compile.go); this is
// the one place that combines it with instanceCtx into the durable
// subscription record [Accept] evaluates signals against.
func FromCompiled(node *workflow.CompiledNode, instanceCtx InstanceContext) (SignalSubscription, error) {
	if node == nil {
		return SignalSubscription{}, fmt.Errorf("signal: FromCompiled: node is nil")
	}
	if node.Type != workflow.StepSignal {
		return SignalSubscription{}, fmt.Errorf("signal: FromCompiled: node %q is %s, not SIGNAL", node.ID, node.Type)
	}
	s := node.Signal
	if s == nil {
		return SignalSubscription{}, fmt.Errorf("signal: FromCompiled: node %q carries no SIGNAL binding", node.ID)
	}

	sub := SignalSubscription{
		Tenant:             instanceCtx.Tenant,
		WorkflowInstanceID: instanceCtx.WorkflowInstanceID,
		NodeID:             node.ID,
		EventType:          s.EventType,
		CorrelationKey:     s.CorrelationKeyExpression,
		CorrelationValue:   instanceCtx.CorrelationValue,
		ExpectedSchemaRef:  s.ExpectedSchemaRef.String(),
		AcceptedSources:    append([]string(nil), s.AcceptedSources...),
		Ordering:           OrderingExpectation(s.Ordering),
		ClosesAt:           instanceCtx.ClosesAt,
	}
	if s.CloseTimeExpr != nil {
		if instanceCtx.ClosesAt.IsSet() {
			return SignalSubscription{}, fmt.Errorf("signal: FromCompiled: node %q: a close time expression and a context close instant are mutually exclusive", node.ID)
		}
		expr, err := translateTimeExpr(s.CloseTimeExpr)
		if err != nil {
			return SignalSubscription{}, fmt.Errorf("signal: FromCompiled: node %q: %w", node.ID, err)
		}
		res, err := values.ResolveTimeExpr(expr, instanceCtx.Anchor, instanceCtx.Calendars, instanceCtx.Dataset)
		if err != nil {
			return SignalSubscription{}, fmt.Errorf("signal: FromCompiled: node %q: close time expression: %v", node.ID, err)
		}
		sub.ClosesAt = res.FireAt
	}
	if s.TimeoutDefault != nil {
		sub.DeadlineDefault = values.DeadlineDefault{Schema: s.TimeoutDefault.Schema, CanonicalJSON: s.TimeoutDefault.CanonicalJSON}
	}
	if err := sub.Validate(); err != nil {
		return SignalSubscription{}, fmt.Errorf("signal: FromCompiled: node %q: %w", node.ID, err)
	}
	return sub, nil
}

// translateTimeExpr builds the typed close expression from its wire form.
// Zone and calendar come from the spec itself: unlike WAIT, a SIGNAL node
// pins no zone or calendar at node level.
func translateTimeExpr(spec *workflow.TimeExprSpec) (values.TimeExpr, error) {
	kind, err := values.ParseTimeExprOffsetKind(spec.OffsetKind)
	if err != nil {
		return values.TimeExpr{}, fmt.Errorf("close_time_expr: %w", err)
	}
	expr := values.TimeExpr{
		AnchorPath:    spec.AnchorPath,
		OffsetKind:    kind,
		OffsetDays:    spec.OffsetDays,
		OffsetSeconds: spec.OffsetSeconds,
		Calendar:      values.CalendarRef{Ref: spec.CalendarRef, Version: spec.CalendarVersion},
		Zone:          values.ZoneRef{ID: spec.ZoneID, TzdbVersion: spec.ZoneTzdbVersion},
	}
	if spec.LocalTime != "" {
		lt, err := values.ParseLocalTime(spec.LocalTime)
		if err != nil {
			return values.TimeExpr{}, fmt.Errorf("close_time_expr: local_time: %w", err)
		}
		expr.WakeLocalTime = lt
	}
	if spec.Disambiguation != "" {
		disamb, err := values.ParseDisambiguation(spec.Disambiguation)
		if err != nil {
			return values.TimeExpr{}, fmt.Errorf("close_time_expr: disambiguation: %w", err)
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
	if err := expr.Validate(); err != nil {
		return values.TimeExpr{}, fmt.Errorf("close_time_expr: %w", err)
	}
	return expr, nil
}
