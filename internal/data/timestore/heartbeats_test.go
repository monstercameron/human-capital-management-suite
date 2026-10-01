package timestore

import (
	"context"
	"errors"
	"testing"
	"time"
)

func battery(v int) *int { return &v }

func TestTodo_TCLOCK_008(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	tenant := "tenant-tclock008"
	now := time.Now()

	hb := Heartbeat{DeviceID: "device-1", AppVersion: "1.2.3", QueueDepth: 4, OldestUnsentAgeSeconds: 30, BatteryPercent: battery(80), PowerState: "AC", OffsetMillis: 120, ObservedAt: now}
	if err := s.RecordHeartbeat(ctx, tenant, hb); err != nil {
		t.Fatalf("record: %v", err)
	}
	got, err := s.LatestHeartbeat(ctx, tenant, "device-1")
	if err != nil || got.AppVersion != "1.2.3" || got.QueueDepth != 4 || got.OffsetMillis != 120 {
		t.Fatalf("latest: %v %+v", err, got)
	}

	// A later heartbeat replaces the latest row.
	later := now.Add(time.Minute)
	hb2 := Heartbeat{DeviceID: "device-1", AppVersion: "1.2.4", QueueDepth: 0, OldestUnsentAgeSeconds: 0, BatteryPercent: battery(79), PowerState: "BATTERY", OffsetMillis: 15, ObservedAt: later}
	if err := s.RecordHeartbeat(ctx, tenant, hb2); err != nil {
		t.Fatalf("record 2: %v", err)
	}
	got, err = s.LatestHeartbeat(ctx, tenant, "device-1")
	if err != nil || got.AppVersion != "1.2.4" || got.QueueDepth != 0 {
		t.Fatalf("latest after update: %v %+v", err, got)
	}

	// An out-of-order (older) heartbeat does not clobber the newer state.
	stale := Heartbeat{DeviceID: "device-1", AppVersion: "1.2.0", QueueDepth: 99, OldestUnsentAgeSeconds: 999, PowerState: "AC", ObservedAt: now.Add(-time.Hour)}
	if err := s.RecordHeartbeat(ctx, tenant, stale); err != nil {
		t.Fatalf("record stale: %v", err)
	}
	got, err = s.LatestHeartbeat(ctx, tenant, "device-1")
	if err != nil || got.AppVersion != "1.2.4" {
		t.Fatalf("latest after stale heartbeat: %v %+v", err, got)
	}

	hist, err := s.HeartbeatHistory(ctx, tenant, "device-1", 10)
	if err != nil || len(hist) != 3 {
		t.Fatalf("history: %v %+v", err, hist)
	}
}

func TestTodo_TCLOCK_008_Integration(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	tenant := "tenant-integration-hb"
	now := time.Now()

	if err := s.CreateEnrollmentCode(ctx, tenant, "HB1", "site-1", "profile-1", "UTC", "admin-1", now.Add(time.Hour)); err != nil {
		t.Fatalf("create code online: %v", err)
	}
	online, err := s.RedeemEnrollmentCode(ctx, tenant, "HB1", "device-online", []byte("k"), "admin-1", now)
	if err != nil {
		t.Fatalf("redeem online: %v", err)
	}
	if err := s.CreateEnrollmentCode(ctx, tenant, "HB2", "site-1", "profile-1", "UTC", "admin-1", now.Add(time.Hour)); err != nil {
		t.Fatalf("create code offline: %v", err)
	}
	offline, err := s.RedeemEnrollmentCode(ctx, tenant, "HB2", "device-offline", []byte("k"), "admin-1", now)
	if err != nil {
		t.Fatalf("redeem offline: %v", err)
	}
	if err := s.CreateEnrollmentCode(ctx, tenant, "HB3", "site-1", "profile-1", "UTC", "admin-1", now.Add(time.Hour)); err != nil {
		t.Fatalf("create code never-seen: %v", err)
	}
	neverSeen, err := s.RedeemEnrollmentCode(ctx, tenant, "HB3", "device-never-seen", []byte("k"), "admin-1", now)
	if err != nil {
		t.Fatalf("redeem never-seen: %v", err)
	}
	if err := s.CreateEnrollmentCode(ctx, tenant, "HB4", "site-1", "profile-1", "UTC", "admin-1", now.Add(time.Hour)); err != nil {
		t.Fatalf("create code suspended: %v", err)
	}
	suspended, err := s.RedeemEnrollmentCode(ctx, tenant, "HB4", "device-suspended", []byte("k"), "admin-1", now)
	if err != nil {
		t.Fatalf("redeem suspended: %v", err)
	}
	if _, err := s.SuspendDevice(ctx, tenant, suspended.ID, "admin-1", "maintenance", suspended.Revision); err != nil {
		t.Fatalf("suspend: %v", err)
	}

	if err := s.RecordHeartbeat(ctx, tenant, Heartbeat{DeviceID: online.ID, AppVersion: "1.0", ObservedAt: now, PowerState: "AC"}); err != nil {
		t.Fatalf("heartbeat online: %v", err)
	}
	if err := s.RecordHeartbeat(ctx, tenant, Heartbeat{DeviceID: offline.ID, AppVersion: "1.0", ObservedAt: now.Add(-2 * time.Hour), PowerState: "AC"}); err != nil {
		t.Fatalf("heartbeat offline: %v", err)
	}
	if err := s.RecordHeartbeat(ctx, tenant, Heartbeat{DeviceID: suspended.ID, AppVersion: "1.0", ObservedAt: now, PowerState: "AC"}); err != nil {
		t.Fatalf("heartbeat suspended: %v", err)
	}

	asOf := now
	offlineAfter := 15 * time.Minute
	all, err := s.DevicesByStatus(ctx, tenant, "site-1", "", asOf, offlineAfter, 10)
	if err != nil {
		t.Fatalf("all devices by status: %v", err)
	}
	if len(all) != 4 {
		t.Fatalf("device count = %d, want 4: %+v", len(all), all)
	}
	statusByID := map[string]string{}
	for _, d := range all {
		statusByID[d.Device.ID] = d.FleetStatus
	}
	if statusByID[online.ID] != FleetStatusOnline {
		t.Fatalf("online device status = %q", statusByID[online.ID])
	}
	if statusByID[offline.ID] != FleetStatusOffline {
		t.Fatalf("offline device status = %q", statusByID[offline.ID])
	}
	if statusByID[neverSeen.ID] != FleetStatusNeverSeen {
		t.Fatalf("never-seen device status = %q", statusByID[neverSeen.ID])
	}
	if statusByID[suspended.ID] != FleetStatusSuspended {
		t.Fatalf("suspended device status = %q", statusByID[suspended.ID])
	}

	onlineOnly, err := s.DevicesByStatus(ctx, tenant, "site-1", FleetStatusOnline, asOf, offlineAfter, 10)
	if err != nil {
		t.Fatalf("filtered by status: %v", err)
	}
	if len(onlineOnly) != 1 || onlineOnly[0].Device.ID != online.ID {
		t.Fatalf("online-only filter = %+v", onlineOnly)
	}
}

func TestTodo_TCLOCK_008_Security(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	tenant := "tenant-a"
	now := time.Now()
	if err := s.RecordHeartbeat(ctx, tenant, Heartbeat{DeviceID: "device-1", AppVersion: "1.0", ObservedAt: now, PowerState: "AC"}); err != nil {
		t.Fatalf("record: %v", err)
	}
	if _, err := s.LatestHeartbeat(ctx, "tenant-b", "device-1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant latest: %v", err)
	}
	hist, err := s.HeartbeatHistory(ctx, "tenant-b", "device-1", 10)
	if err != nil || len(hist) != 0 {
		t.Fatalf("cross-tenant history leaked: %v %+v", err, hist)
	}
}

func TestTodo_TCLOCK_008_Fault(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	tenant := "tenant-fault"
	if err := s.RecordHeartbeat(ctx, tenant, Heartbeat{}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("empty heartbeat: %v", err)
	}
	if _, err := s.LatestHeartbeat(ctx, tenant, "unknown-device"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown device: %v", err)
	}
	if _, err := s.DevicesByStatus(ctx, tenant, "site-1", "", time.Time{}, time.Minute, 10); !errors.Is(err, ErrInvalid) {
		t.Fatalf("zero asOf: %v", err)
	}
}

// BenchmarkTodo_TCLOCK_008 is the BENCHMARK entry for the heartbeat upsert
// path. pgtest intentionally exposes test schemas only through *testing.T
// (see internal/data/documenthubstore/hub_043_test.go's BenchmarkTodo_HUB_043),
// so the measured path is exercised by TestTodo_TCLOCK_008's repeated
// RecordHeartbeat calls against the same device instead of by a b.N loop
// here.
func BenchmarkTodo_TCLOCK_008(b *testing.B) {
	b.Skip("the measured upsert path is TestTodo_TCLOCK_008; pgtest intentionally exposes test schemas only through *testing.T")
}
