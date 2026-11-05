package productui

import (
	"hash/fnv"
	"strconv"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// StatusProjection is an authorization-filtered presentation of the five
// canonical BusinessIntent lifecycle dimensions. Canonical enum types and
// explicit presence prevent free-form values and cross-dimension inference.
// Presence reasons are deliberately never rendered; they can contain protected
// authority or provenance details.
type StatusProjection struct {
	_           incomparable
	Available   bool
	Request     values.Presence[intentsv1.RequestState]
	Execution   values.Presence[intentsv1.ExecutionState]
	Business    values.Presence[intentsv1.BusinessState]
	Consistency values.Presence[intentsv1.ConsistencyState]
	Obligation  values.Presence[intentsv1.ObligationState]
}

type MultidimensionalStatus = StatusProjection
type StatusDimensions = StatusProjection

// CompleteStatusProjection adapts an already authorization-filtered canonical
// service projection. The renderer grants no authority and performs no masking.
func CompleteStatusProjection(d *intentsv1.LifecycleDimensions) StatusProjection {
	if d == nil {
		return StatusProjection{}
	}
	return StatusProjection{
		Available:   true,
		Request:     values.Value(d.GetRequest()),
		Execution:   values.Value(d.GetExecution()),
		Business:    values.Value(d.GetBusiness()),
		Consistency: values.Value(d.GetConsistency()),
		Obligation:  values.Value(d.GetObligation()),
	}
}

type StatusPresentationProps struct {
	I18nProps
	// IDSeed is hashed before emission so arbitrary service text and business
	// identifiers never become DOM IDs or CSS selectors.
	IDSeed     string
	Projection StatusProjection
}

type statusToken struct{ key, glyph, tone string }
type statusDimension struct{ kind, label, value, valueKey, glyph, tone string }

// StatusPresentation exposes no aggregate status. Legal lifecycle dimensions
// may disagree; tuple validity and repair remain service responsibilities.
func StatusPresentation(props StatusPresentationProps) ui.Node {
	groupLabel := props.Text("status.group")
	groupProps := html.Props{Class: "status-dimensions", Aria: map[string]string{"label": groupLabel}, Raw: map[string]any{"role": "group"}}
	if id := stableStatusID(props.IDSeed); id != "" {
		groupProps.ID = id
	}
	if !props.Projection.Available {
		groupProps.Class += " status-dimensions-unavailable"
		return html.Div(groupProps, html.Span(html.Props{Class: "status-projection-unavailable"}, ui.Text(props.Text("status.projection_unavailable"))))
	}

	p := props.Projection
	items := [5]statusDimension{
		statusItem(props.Locale, "request", props.Text("status.request_label"), p.Request, requestToken),
		statusItem(props.Locale, "execution", props.Text("status.execution_label"), p.Execution, executionToken),
		statusItem(props.Locale, "business", props.Text("status.business_label"), p.Business, businessToken),
		statusItem(props.Locale, "consistency", props.Text("status.consistency_label"), p.Consistency, consistencyToken),
		statusItem(props.Locale, "obligation", props.Text("status.obligation_label"), p.Obligation, obligationToken),
	}
	accessibleValues := make([]string, 1, len(items)+1)
	accessibleValues[0] = groupLabel
	for _, item := range items {
		accessibleValues = append(accessibleValues, item.label+": "+item.value)
	}
	groupProps.Aria["label"] = strings.Join(accessibleValues, "; ")
	children := make([]ui.Node, 0, len(items))
	for _, item := range items {
		children = append(children, html.Li(html.Props{Class: "status-dimension status-tone-" + item.tone, Raw: map[string]any{"data-status-dimension": item.kind, "data-status-value": item.valueKey}},
			html.Span(html.Props{Class: "status-dimension-glyph", Raw: map[string]any{"aria-hidden": "true"}}, ui.Text(item.glyph)),
			html.Span(html.Props{Class: "status-dimension-name"}, ui.Text(item.label)),
			html.Span(html.Props{Class: "status-dimension-value"}, ui.Text(item.value)),
		))
	}
	return html.Div(groupProps, html.Ul(html.Props{Class: "status-dimension-list"}, children...))
}

func statusItem[T any](locale LocaleContext, kind, label string, presence values.Presence[T], tokenFor func(T) (statusToken, bool)) statusDimension {
	if value, ok := presence.Get(); ok {
		token, valid := tokenFor(value)
		if !valid {
			return dimensionForValue(kind, label, locale.Text("status.value.invalid"), "invalid", "!", "invalid")
		}
		return dimensionForValue(kind, label, locale.Text("status."+kind+"."+token.key), token.key, token.glyph, token.tone)
	}
	key, glyph, tone := presenceToken(presence.State())
	return dimensionForValue(kind, label, locale.Text("status.value."+key), key, glyph, tone)
}

func dimensionForValue(kind, label, value, key, glyph, tone string) statusDimension {
	return statusDimension{kind: kind, label: label, value: value, valueKey: key, glyph: glyph, tone: tone}
}

func presenceToken(state values.PresenceState) (key, glyph, tone string) {
	switch state {
	case values.PresenceAbsent:
		return "not_supplied", "—", "muted"
	case values.PresenceUnknown:
		return "unknown", "?", "unknown"
	case values.PresenceRedacted:
		return "restricted", "◆", "muted"
	case values.PresenceUnavailable:
		return "unavailable", "…", "unknown"
	case values.PresenceNotApplicable:
		return "not_applicable", "–", "muted"
	default: // null, unspecified, VALUE without a valid enum, or an illegal state
		return "invalid", "!", "invalid"
	}
}

func stableStatusID(seed string) string {
	seed = strings.TrimSpace(seed)
	if seed == "" {
		return ""
	}
	h := fnv.New64a()
	_, _ = h.Write([]byte(seed))
	return "status-" + strconv.FormatUint(h.Sum64(), 36)
}

func requestToken(v intentsv1.RequestState) (statusToken, bool) {
	switch v {
	case intentsv1.RequestState_REQUEST_STATE_DRAFT:
		return statusToken{"draft", "○", "neutral"}, true
	case intentsv1.RequestState_REQUEST_STATE_PREFLIGHTED:
		return statusToken{"preflighted", "◇", "active"}, true
	case intentsv1.RequestState_REQUEST_STATE_SIMULATED:
		return statusToken{"simulated", "◎", "active"}, true
	case intentsv1.RequestState_REQUEST_STATE_SUBMITTED:
		return statusToken{"submitted", "↑", "active"}, true
	case intentsv1.RequestState_REQUEST_STATE_APPROVED:
		return statusToken{"approved", "✓", "positive"}, true
	case intentsv1.RequestState_REQUEST_STATE_REJECTED:
		return statusToken{"rejected", "×", "danger"}, true
	case intentsv1.RequestState_REQUEST_STATE_WITHDRAWN:
		return statusToken{"withdrawn", "←", "neutral"}, true
	case intentsv1.RequestState_REQUEST_STATE_CANCELLED:
		return statusToken{"cancelled", "∅", "neutral"}, true
	case intentsv1.RequestState_REQUEST_STATE_SUPERSEDED:
		return statusToken{"superseded", "⇢", "neutral"}, true
	case intentsv1.RequestState_REQUEST_STATE_CLOSED:
		return statusToken{"closed", "■", "neutral"}, true
	case intentsv1.RequestState_REQUEST_STATE_REOPENED:
		return statusToken{"reopened", "↻", "caution"}, true
	default:
		return statusToken{}, false
	}
}

func executionToken(v intentsv1.ExecutionState) (statusToken, bool) {
	switch v {
	case intentsv1.ExecutionState_EXECUTION_STATE_NOT_PLANNED:
		return statusToken{"not_planned", "–", "neutral"}, true
	case intentsv1.ExecutionState_EXECUTION_STATE_SCHEDULED:
		return statusToken{"scheduled", "◷", "active"}, true
	case intentsv1.ExecutionState_EXECUTION_STATE_REVALIDATING:
		return statusToken{"revalidating", "↻", "active"}, true
	case intentsv1.ExecutionState_EXECUTION_STATE_EXECUTING:
		return statusToken{"executing", "▶", "active"}, true
	case intentsv1.ExecutionState_EXECUTION_STATE_COMMITTED:
		return statusToken{"committed", "✓", "positive"}, true
	case intentsv1.ExecutionState_EXECUTION_STATE_BLOCKED:
		return statusToken{"blocked", "!", "danger"}, true
	case intentsv1.ExecutionState_EXECUTION_STATE_REPAIR_REQUIRED:
		return statusToken{"repair_required", "⚒", "danger"}, true
	default:
		return statusToken{}, false
	}
}

func businessToken(v intentsv1.BusinessState) (statusToken, bool) {
	switch v {
	case intentsv1.BusinessState_BUSINESS_STATE_NOT_STARTED:
		return statusToken{"not_started", "–", "neutral"}, true
	case intentsv1.BusinessState_BUSINESS_STATE_IN_PROGRESS:
		return statusToken{"in_progress", "▶", "active"}, true
	case intentsv1.BusinessState_BUSINESS_STATE_COMPLETED:
		return statusToken{"completed", "✓", "positive"}, true
	case intentsv1.BusinessState_BUSINESS_STATE_NOT_ACHIEVED:
		return statusToken{"not_achieved", "×", "danger"}, true
	case intentsv1.BusinessState_BUSINESS_STATE_CORRECTED:
		return statusToken{"corrected", "↻", "positive"}, true
	case intentsv1.BusinessState_BUSINESS_STATE_UNKNOWN:
		return statusToken{"unknown", "?", "unknown"}, true
	default:
		return statusToken{}, false
	}
}

func consistencyToken(v intentsv1.ConsistencyState) (statusToken, bool) {
	switch v {
	case intentsv1.ConsistencyState_CONSISTENCY_STATE_NOT_APPLICABLE:
		return statusToken{"not_applicable", "–", "muted"}, true
	case intentsv1.ConsistencyState_CONSISTENCY_STATE_PENDING_OBSERVATION:
		return statusToken{"pending_observation", "◷", "caution"}, true
	case intentsv1.ConsistencyState_CONSISTENCY_STATE_CONSISTENT:
		return statusToken{"consistent", "✓", "positive"}, true
	case intentsv1.ConsistencyState_CONSISTENCY_STATE_DEGRADED:
		return statusToken{"degraded", "!", "danger"}, true
	case intentsv1.ConsistencyState_CONSISTENCY_STATE_REPAIRING:
		return statusToken{"repairing", "↻", "caution"}, true
	case intentsv1.ConsistencyState_CONSISTENCY_STATE_UNKNOWN:
		return statusToken{"unknown", "?", "unknown"}, true
	default:
		return statusToken{}, false
	}
}

func obligationToken(v intentsv1.ObligationState) (statusToken, bool) {
	switch v {
	case intentsv1.ObligationState_OBLIGATION_STATE_NOT_APPLICABLE:
		return statusToken{"not_applicable", "–", "muted"}, true
	case intentsv1.ObligationState_OBLIGATION_STATE_PENDING:
		return statusToken{"pending", "◷", "caution"}, true
	case intentsv1.ObligationState_OBLIGATION_STATE_SATISFIED:
		return statusToken{"satisfied", "✓", "positive"}, true
	case intentsv1.ObligationState_OBLIGATION_STATE_OVERDUE:
		return statusToken{"overdue", "!", "danger"}, true
	case intentsv1.ObligationState_OBLIGATION_STATE_WAIVED:
		return statusToken{"waived", "◇", "neutral"}, true
	case intentsv1.ObligationState_OBLIGATION_STATE_UNKNOWN:
		return statusToken{"unknown", "?", "unknown"}, true
	default:
		return statusToken{}, false
	}
}
