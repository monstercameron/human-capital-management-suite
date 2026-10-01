package project

import (
	"errors"
	"testing"
	"time"
)

func TestTodo_PM_021(t *testing.T) {
	at := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	got, applied, err := ApplyWorkItemProjection(WorkItemProjection{}, WorkItemProjectionEvent{
		EventID: "event-1", TenantID: "tenant-a", WorkItemID: "work-1", ObservedVersion: 7,
		SafeStatus: "ASSIGNED", Freshness: WorkItemFreshnessCurrent, Authorized: true, OccurredAt: at,
	})
	if err != nil || !applied || !got.Available || got.SafeStatus != "ASSIGNED" || got.ObservedVersion != 7 || got.LastEventID != "event-1" {
		t.Fatalf("authorized projection = %+v applied=%v err=%v", got, applied, err)
	}
	if got.TenantID != "tenant-a" || !got.ObservedAt.Equal(at) {
		t.Fatalf("projection lost event scope/time: %+v", got)
	}
	if replay, applied, err := ApplyWorkItemProjection(got, WorkItemProjectionEvent{
		EventID: "event-1", TenantID: "tenant-a", WorkItemID: "work-1", ObservedVersion: 7,
		SafeStatus: "ASSIGNED", Freshness: WorkItemFreshnessCurrent, Authorized: true, OccurredAt: at,
	}); err != nil || applied || replay.SafeStatus != "ASSIGNED" {
		t.Fatalf("exact replay = %+v applied=%v err=%v", replay, applied, err)
	}
}

func TestTodo_PM_021_Security(t *testing.T) {
	at := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	current, _, err := ApplyWorkItemProjection(WorkItemProjection{}, WorkItemProjectionEvent{
		EventID: "event-1", TenantID: "tenant-a", WorkItemID: "work-1", ObservedVersion: 7,
		SafeStatus: "IN_PROGRESS", Freshness: WorkItemFreshnessCurrent, Authorized: true, OccurredAt: at,
	})
	if err != nil {
		t.Fatal(err)
	}
	neutral, applied, err := ApplyWorkItemProjection(current, WorkItemProjectionEvent{
		EventID: "event-2", TenantID: "tenant-a", WorkItemID: "work-1", ObservedVersion: 8,
		SafeStatus: "PRIVATE_APPROVAL_DETAIL", Freshness: WorkItemFreshnessStale, Authorized: true, OccurredAt: at.Add(time.Minute),
	})
	if err != nil || !applied || neutral.Available || neutral.SafeStatus != WorkItemSafeStatusUnavailable || neutral.Freshness != WorkItemFreshnessStale {
		t.Fatalf("stale projection retained protected detail: %+v applied=%v err=%v", neutral, applied, err)
	}
	if _, _, err := ApplyWorkItemProjection(current, WorkItemProjectionEvent{EventID: "event-x", TenantID: "tenant-b", WorkItemID: "work-1", ObservedVersion: 8, Freshness: WorkItemFreshnessCurrent, Authorized: true, OccurredAt: at}); !errors.Is(err, ErrInvalidWorkItemEvent) {
		t.Fatalf("cross-tenant event error=%v", err)
	}
}

func TestTodo_PM_021_Integration(t *testing.T) {
	at := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	projection, _, err := ApplyWorkItemProjection(WorkItemProjection{}, WorkItemProjectionEvent{EventID: "event-2", TenantID: "tenant-a", WorkItemID: "work-1", ObservedVersion: 2, SafeStatus: "DONE", Freshness: WorkItemFreshnessCurrent, Authorized: true, OccurredAt: at})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := ApplyWorkItemProjection(projection, WorkItemProjectionEvent{EventID: "event-1", TenantID: "tenant-a", WorkItemID: "work-1", ObservedVersion: 1, SafeStatus: "ASSIGNED", Freshness: WorkItemFreshnessCurrent, Authorized: true, OccurredAt: at}); !errors.Is(err, ErrStaleWorkItemEvent) {
		t.Fatalf("out-of-order event error=%v", err)
	}
	if _, _, err := ApplyWorkItemProjection(projection, WorkItemProjectionEvent{EventID: "event-2", TenantID: "tenant-a", WorkItemID: "work-1", ObservedVersion: 3, SafeStatus: "DONE", Freshness: WorkItemFreshnessCurrent, Authorized: true, OccurredAt: at}); !errors.Is(err, ErrWorkItemReplay) {
		t.Fatalf("conflicting replay error=%v", err)
	}
}
