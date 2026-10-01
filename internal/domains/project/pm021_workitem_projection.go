package project

import (
	"errors"
	"strings"
	"time"
)

const (
	WorkItemFreshnessCurrent      = "CURRENT"
	WorkItemFreshnessStale        = "STALE"
	WorkItemFreshnessUnavailable  = "UNAVAILABLE"
	WorkItemSafeStatusUnavailable = "UNAVAILABLE"
)

var (
	ErrInvalidWorkItemEvent = errors.New("project: invalid WorkItem projection event")
	ErrStaleWorkItemEvent   = errors.New("project: stale WorkItem projection event")
	ErrWorkItemReplay       = errors.New("project: WorkItem projection replay conflicts")
)

// WorkItemProjectionEvent is the only input accepted by the project-side
// WorkItem projection. It carries a monotonic owner version and an
// authorization decision made by Human Work; it contains no command fields.
type WorkItemProjectionEvent struct {
	EventID         string
	TenantID        string
	WorkItemID      string
	ObservedVersion uint64
	SafeStatus      string
	Freshness       string
	Authorized      bool
	OccurredAt      time.Time
}

// ApplyWorkItemProjection applies one owner-authorized event without exposing
// a project operation that can mutate the Human Work item. Replays of the
// exact event are no-ops. A stale or revoked event deliberately replaces the
// prior detail with a neutral unavailable projection.
func ApplyWorkItemProjection(current WorkItemProjection, event WorkItemProjectionEvent) (WorkItemProjection, bool, error) {
	if strings.TrimSpace(event.EventID) == "" || strings.TrimSpace(event.TenantID) == "" || strings.TrimSpace(event.WorkItemID) == "" || event.ObservedVersion == 0 || event.OccurredAt.IsZero() {
		return WorkItemProjection{}, false, ErrInvalidWorkItemEvent
	}
	if event.Freshness != WorkItemFreshnessCurrent && event.Freshness != WorkItemFreshnessStale && event.Freshness != WorkItemFreshnessUnavailable {
		return WorkItemProjection{}, false, ErrInvalidWorkItemEvent
	}
	if current.ID != "" {
		if current.ID != event.WorkItemID || current.TenantID != "" && current.TenantID != event.TenantID {
			return WorkItemProjection{}, false, ErrInvalidWorkItemEvent
		}
		if current.LastEventID == event.EventID {
			if current.ObservedVersion != event.ObservedVersion {
				return WorkItemProjection{}, false, ErrWorkItemReplay
			}
			return current, false, nil
		}
		if event.ObservedVersion <= current.ObservedVersion {
			return WorkItemProjection{}, false, ErrStaleWorkItemEvent
		}
	}
	projection := WorkItemProjection{
		ID: event.WorkItemID, TenantID: event.TenantID, ObservedVersion: event.ObservedVersion,
		Freshness: event.Freshness, LastEventID: event.EventID, ObservedAt: event.OccurredAt.UTC(),
		Available: event.Authorized && event.Freshness == WorkItemFreshnessCurrent,
	}
	if projection.Available && strings.TrimSpace(event.SafeStatus) != "" {
		projection.SafeStatus = strings.TrimSpace(event.SafeStatus)
	} else {
		projection.SafeStatus = WorkItemSafeStatusUnavailable
		projection.Available = false
		if projection.Freshness == WorkItemFreshnessCurrent {
			projection.Freshness = WorkItemFreshnessUnavailable
		}
	}
	return projection, true, nil
}
