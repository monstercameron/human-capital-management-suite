package productui

import "strings"

// UXBLIND-123: one availability projection decides whether the Time clock
// destination is advertised and what its page says when the clock cannot be
// offered. The server composes the workspace half (is the clock service
// running here, is the viewer an administrator); the clock service supplies
// the per-worker half as a closed reason. The browser only renders both.

// ClockReason is why a viewer's own clock is not offered. The values are the
// closed vocabulary the clock service publishes on the wire; ClockReasonNone
// means no reason is known, and an unknown value is never trusted.
type ClockReason string

const (
	// ClockReasonNone is no known reason: the clock is offered, or the read
	// failed for a transient cause the viewer can retry.
	ClockReasonNone ClockReason = ""
	// ClockReasonNotEnabled means the workspace does not run the time clock.
	ClockReasonNotEnabled ClockReason = "NOT_ENABLED"
	// ClockReasonNoWorkerRecord means the sign-in has no active worker record.
	ClockReasonNoWorkerRecord ClockReason = "NO_WORKER_RECORD"
	// ClockReasonNoAssignment means the worker has no current assignment.
	ClockReasonNoAssignment ClockReason = "NO_ASSIGNMENT"
	// ClockReasonNoTimeProfile means no published time profile is pinned to
	// the worker's assignment.
	ClockReasonNoTimeProfile ClockReason = "NO_TIME_PROFILE"
	// ClockReasonExempt means the worker's profile is exempt from time
	// recording by clock.
	ClockReasonExempt ClockReason = "EXEMPT"
	// ClockReasonCaptureNotPunch means the worker's profile records time by
	// another method.
	ClockReasonCaptureNotPunch ClockReason = "CAPTURE_NOT_PUNCH"
)

// NormalizeClockReason bounds an untrusted string to the known vocabulary.
func NormalizeClockReason(value string) ClockReason {
	switch reason := ClockReason(strings.TrimSpace(value)); reason {
	case ClockReasonNotEnabled, ClockReasonNoWorkerRecord, ClockReasonNoAssignment, ClockReasonNoTimeProfile, ClockReasonExempt, ClockReasonCaptureNotPunch:
		return reason
	}
	return ClockReasonNone
}

// ClockAvailabilityProjection is the workspace-level answer for one viewer.
type ClockAvailabilityProjection struct {
	// Enabled reports whether this workspace runs the time clock service.
	Enabled bool
	// ViewerIsAdmin reports whether the viewer administers access; only an
	// administrator is told how to turn the clock on.
	ViewerIsAdmin bool
}

// ClockSurface is the decision shared by navigation and the page.
type ClockSurface struct {
	// NavVisible reports whether the Time clock destination is advertised.
	NavVisible bool
	// AdminHelp reports whether the page adds the administrator's guidance.
	AdminHelp bool
	// Reason is set when the workspace itself rules the clock out.
	Reason ClockReason
}

// ResolveClockSurface is the single decision for the navigation entry and the
// page state. A nil projection is a component preview and leaves the registry
// navigation untouched. Where the service is not running, only an
// administrator is shown the entry: nobody else can act on it, and a direct
// visit still states the reason.
func ResolveClockSurface(projection *ClockAvailabilityProjection) ClockSurface {
	switch {
	case projection == nil:
		return ClockSurface{NavVisible: true}
	case !projection.Enabled:
		return ClockSurface{NavVisible: projection.ViewerIsAdmin, AdminHelp: projection.ViewerIsAdmin, Reason: ClockReasonNotEnabled}
	default:
		return ClockSurface{NavVisible: true, AdminHelp: projection.ViewerIsAdmin}
	}
}

// ApplyClockAvailability installs the projection and re-derives navigation
// from it, so the menu, global search and the page agree.
func ApplyClockAvailability(view View, projection ClockAvailabilityProjection) View {
	installed := projection
	view.ClockAvailability = &installed
	if view.NavigationProjection != nil {
		return ApplyNavigationProjection(view, *view.NavigationProjection)
	}
	if !ResolveClockSurface(view.ClockAvailability).NavVisible {
		view.Navigation = withoutPageNavItems(view.Navigation, PageClock)
	}
	return view
}

// clockNavigationFilter removes the Time clock destination from an authorized
// navigation answer when the projection says it is not advertised.
func clockNavigationFilter(view View, projection AuthorizedNavigationProjection) AuthorizedNavigationProjection {
	if ResolveClockSurface(view.ClockAvailability).NavVisible {
		return projection
	}
	projection.Items = withoutPageAuthorizedItems(projection.Items, PageClock)
	projection.Support = withoutPageAuthorizedItems(projection.Support, PageClock)
	return projection
}

func withoutPageAuthorizedItems(items []AuthorizedNavigationItem, page PageID) []AuthorizedNavigationItem {
	result := make([]AuthorizedNavigationItem, 0, len(items))
	for _, item := range items {
		if item.Page == page {
			continue
		}
		if len(item.Children) > 0 {
			item.Children = withoutPageAuthorizedItems(item.Children, page)
			if len(item.Children) == 1 && item.Children[0].Page == item.Page {
				item.Children = nil
			}
		}
		result = append(result, item)
	}
	return result
}

func withoutPageNavItems(items []NavItem, page PageID) []NavItem {
	result := make([]NavItem, 0, len(items))
	for _, item := range items {
		if item.Page == page {
			continue
		}
		if len(item.Children) > 0 {
			item.Children = withoutPageNavItems(item.Children, page)
			if len(item.Children) == 1 && item.Children[0].Page == item.Page {
				item.Children = nil
			}
		}
		result = append(result, item)
	}
	return result
}

// clockUnavailableReason is the reason the page states: the workspace's own
// answer first, then the clock service's answer about this worker.
func clockUnavailableReason(view View) ClockReason {
	if surface := ResolveClockSurface(view.ClockAvailability); surface.Reason != ClockReasonNone {
		return surface.Reason
	}
	return NormalizeClockReason(string(view.ClockProjection.Reason))
}
