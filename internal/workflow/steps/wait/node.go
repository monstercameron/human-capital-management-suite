package wait

import (
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// WakeKind names which shape of wake condition a CompiledWaitNode declares.
type WakeKind string

// The declared wake-condition kinds.
const (
	// WakeAtInstant is a fixed, already-resolved instant. It carries no
	// timezone ambiguity of its own, but a TimerReference is still required
	// so the requirement preserves the calendar/tzdb identity in force when
	// it was minted (REFACTOR: preserve calendar/tzdb version and
	// calculation evidence).
	WakeAtInstant WakeKind = "AT_INSTANT"
	// WakeAtLocalDate fires at the start of a business local date in a zone.
	WakeAtLocalDate WakeKind = "AT_LOCAL_DATE"
	// WakeAtLocalDateTime fires at a specific local wall-clock time on a
	// business local date in a zone.
	WakeAtLocalDateTime WakeKind = "AT_LOCAL_DATETIME"
	// WakeAtTimeExpr resolves its wake instant from a time expression
	// against a per-instance anchor. The compiled node carries the
	// expression; the anchor arrives with the instance at requirement
	// time (ComputeTimerRequirementAtAnchor).
	WakeAtTimeExpr WakeKind = "AT_TIME_EXPR"
)

// CompiledWaitNode is the WF-STEP-005 view of a compiled WAIT node: the
// identity it belongs to, its declared wake condition, and the
// reference-update policy that governs what happens when the timezone or
// business-calendar dataset behind it is republished. See package doc for why
// this is this package's own type rather than a field on workflow.Node.
type CompiledWaitNode struct {
	WorkflowID      string
	WorkflowVersion uint32
	NodeID          string

	// WakeInstant, when set, is a fixed-instant wake condition. It is
	// mutually exclusive with WakeLocalDate and WakeTimeExpr.
	WakeInstant values.Instant

	// WakeTimeExpr, when its anchor path is set, is a deadline-expression
	// wake condition resolved against a per-instance anchor. It is mutually
	// exclusive with WakeInstant and WakeLocalDate.
	WakeTimeExpr values.TimeExpr

	// WakeLocalDate, when set, is a business wall-clock wake condition.
	// WakeLocalTime is optional; when unset the wake condition is the start
	// of WakeLocalDate in Zone.
	WakeLocalDate  values.LocalDate
	WakeLocalTime  values.LocalTime
	Disambiguation values.Disambiguation

	Zone     values.ZoneRef
	Calendar values.CalendarRef
	Policy   values.ReferenceUpdatePolicy
}

// Kind reports which wake-condition shape the node declares. It does not
// validate the node; call Validate first.
func (n CompiledWaitNode) Kind() WakeKind {
	if n.WakeInstant.IsSet() {
		return WakeAtInstant
	}
	if n.WakeTimeExpr.AnchorPath != "" {
		return WakeAtTimeExpr
	}
	if n.WakeLocalTime.IsSet() {
		return WakeAtLocalDateTime
	}
	return WakeAtLocalDate
}

// hasTimeExpr reports whether the node declares a time-expression wake.
func (n CompiledWaitNode) hasTimeExpr() bool { return n.WakeTimeExpr.AnchorPath != "" }

// Validate reports whether the node declares a complete, unambiguous shape.
// It does not resolve the wake condition to an instant — that is
// ComputeTimerRequirement's job, because resolution can legitimately refuse
// (DST gap/fold) without the node itself being malformed.
func (n CompiledWaitNode) Validate() error {
	if n.WorkflowID == "" || n.NodeID == "" {
		return ErrWorkflowIdentityRequired
	}
	shapes := 0
	if n.WakeInstant.IsSet() {
		shapes++
	}
	if n.WakeLocalDate.IsSet() {
		shapes++
	}
	if n.hasTimeExpr() {
		shapes++
	}
	if shapes == 0 {
		return ErrNoWakeCondition
	}
	if shapes > 1 {
		return ErrConflictingWakeCondition
	}
	if n.hasTimeExpr() {
		if err := n.WakeTimeExpr.Validate(); err != nil {
			return fmt.Errorf("wait: %w", err)
		}
		return n.reference().Validate()
	}
	if !n.WakeInstant.IsSet() {
		if !n.WakeLocalDate.IsSet() {
			return ErrNoWakeCondition
		}
		if n.Disambiguation == values.DisambiguationUnspecified {
			return values.ErrDisambiguationRequired
		}
		if !n.Disambiguation.Valid() {
			return fmt.Errorf("wait: %w: %d", values.ErrDisambiguation, uint8(n.Disambiguation))
		}
	}
	return n.reference().Validate()
}

// reference is the dataset context the node's wake condition must be
// replayable against.
func (n CompiledWaitNode) reference() values.TimerReference {
	return values.TimerReference{Zone: n.Zone, Calendar: n.Calendar, Policy: n.Policy}
}

// resolver builds a values.TimerResolver that recomputes the node's wake
// condition against an arbitrary dataset version pair. A fixed instant is
// dataset-independent and is returned unchanged; a local wall-clock condition
// is re-resolved with the zone/calendar's version fields substituted, so PIN
// replay against the historical dataset and RECALCULATE against the current
// one both go through the same arithmetic. A time-expression wake has no
// anchor here, so it resolves to an error naming the anchor requirement;
// resolverAtAnchor is the anchored form.
func (n CompiledWaitNode) resolver() values.TimerResolver {
	return func(ds values.DatasetVersions) (values.Instant, error) {
		if n.hasTimeExpr() {
			return values.Instant{}, ErrTimeExprAnchorRequired
		}
		if n.WakeInstant.IsSet() {
			return n.WakeInstant, nil
		}
		zone := n.Zone
		zone.TzdbVersion = ds.TzdbVersion
		var zdt values.ZonedDateTime
		var err error
		if n.WakeLocalTime.IsSet() {
			zdt, err = values.NewZonedDateTime(n.WakeLocalDate, n.WakeLocalTime, zone, n.Disambiguation)
		} else {
			zdt, err = n.WakeLocalDate.AtStartOfDay(zone, n.Disambiguation)
		}
		if err != nil {
			return values.Instant{}, err
		}
		return zdt.Instant(), nil
	}
}

// resolverAtAnchor is resolver with a per-instance anchor and calendar
// dataset for a time-expression wake: the expression resolves through the
// one shared values.TimeExpr.Resolver timers and SLA escalation use. For a
// fixed or wall-clock wake the anchor is ignored and this is resolver.
func (n CompiledWaitNode) resolverAtAnchor(anchor values.Instant, cals values.CalendarDataset) values.TimerResolver {
	if !n.hasTimeExpr() {
		return n.resolver()
	}
	return n.WakeTimeExpr.Resolver(anchor, cals)
}
