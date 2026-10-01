package main

import (
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// projectClock converts the island's workspace-level clock availability into
// the page contract. A missing island fails closed: the clock is reported as
// not running here rather than assumed to be.
func projectClock(value *journeyclient.Clock) *productui.ClockAvailabilityProjection {
	projection := productui.ClockAvailabilityProjection{}
	if value != nil {
		projection.Enabled, projection.ViewerIsAdmin = value.Enabled, value.ViewerIsAdmin
	}
	return &projection
}

// clockUnavailableProjection turns a refusal into the page state that says why.
// Only a decision about the worker, or a service that is not registered at all,
// becomes a reason; a failed or malformed read stays a retryable outage, so an
// outage is never presented as a verdict on the worker.
func clockUnavailableProjection(err error) (productui.ClockProjection, bool) {
	if reason, ok := clockRefusalReason(err); ok {
		return productui.ClockProjection{State: productui.ClockProjectionUnavailable, Reason: productui.NormalizeClockReason(reason)}, true
	}
	if status.Code(err) == codes.Unimplemented {
		return productui.ClockProjection{State: productui.ClockProjectionUnavailable, Reason: productui.ClockReasonNotEnabled}, true
	}
	return productui.ClockProjection{}, false
}

// clockPhaseFromCode maps the server-owned status code to the page phase. An
// unknown code stays unknown, so no action is offered on a guess.
func clockPhaseFromCode(code string) productui.ClockPhase {
	switch code {
	case "CLOCKED_OUT":
		return productui.ClockPhaseOut
	case "CLOCKED_IN", "OPEN":
		return productui.ClockPhaseIn
	case "ON_BREAK":
		return productui.ClockPhaseBreak
	}
	return productui.ClockPhaseUnknown
}

// clockActionForPhase names the primary action offered in a phase. Secondary
// actions are bound alongside it by the page composer.
func clockActionForPhase(phase productui.ClockPhase) string {
	switch phase {
	case productui.ClockPhaseOut:
		return "in"
	case productui.ClockPhaseIn:
		return "out"
	case productui.ClockPhaseBreak:
		return "end_break"
	}
	return ""
}

// clockLastEventTime parses the server's last-event label when it is an
// instant; "No event" and anything else are shown as the server wrote them.
func clockLastEventTime(label string) (time.Time, bool) {
	at, err := time.Parse(time.RFC3339, strings.TrimSpace(label))
	return at, err == nil
}

// clockRelatedLinks are the destinations the clock page offers beside the
// clock: the worker's own timecard and the way to put a missing punch right.
// A link is offered only when the viewer may open the page it names, so the
// clock never advertises a destination that would refuse them.
func clockRelatedLinks(view productui.View) (timecard, fixPunch *productui.ActionLinkProps) {
	link := func(page productui.PageID) *productui.ActionLinkProps {
		definition, ok := productui.LookupPage(page)
		if !ok || !view.CanFeature(page, productui.FeatureContent, "view") {
			return nil
		}
		return &productui.ActionLinkProps{Label: view.Locale.Text(definition.LabelKey), Href: definition.Route}
	}
	return link(productui.PageTimecard), link(productui.PageMissingPunch)
}
