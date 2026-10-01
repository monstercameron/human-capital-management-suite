package clockservice

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/clock"
)

func TestTodo_TCLOCK_008_Integration(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	devices := fleetStatusDevices{devices: []DeviceRecord{
		{TenantID: "tenant-a", ID: "device-drift", SiteID: "site-a", ProfileID: "profile-a", Timezone: "UTC", State: "ACTIVE", Revision: 1},
		{TenantID: "tenant-a", ID: "device-new", SiteID: "site-a", ProfileID: "profile-a", Timezone: "UTC", State: "ACTIVE", Revision: 1},
	}}
	heartbeats := &fleetPresenceFake{records: map[string]HeartbeatRecord{
		"device-drift": {DeviceID: "device-drift", AppVersion: "2.3.0", PowerState: "MAINS", OffsetMillis: 61_000, ObservedAt: now.Add(-time.Minute)},
	}}
	alerts := &fleetAlertFake{}
	service := FleetService{
		Devices:  Service{Devices: devices, Auth: authStub{}, Heartbeats: heartbeats, Clock: func() time.Time { return now }},
		Policies: fleetPolicyFake{policy: clock.FleetHealthPolicy{MaxDrift: time.Minute, MinimumVersion: "2.4.0", MissingHeartbeatLimit: 15 * time.Minute}},
		Alerts:   alerts, Hours: fleetHoursFake{within: true},
	}
	got, err := service.ListDeviceStatus(context.Background(), fleetPrincipal(t, "tenant-a", "admin", now), "site-a", "")
	if err != nil {
		t.Fatalf("ListDeviceStatus: %v", err)
	}
	if len(got) != 2 || got[0].Status != FleetStatusUnsupportedVersion || !got[0].DriftExceeded || got[1].Status != FleetStatusNeverSeen || !got[1].MissingHeartbeat {
		t.Fatalf("fleet status = %+v", got)
	}
	if len(alerts.kinds) != 1 || alerts.kinds[0] != "MISSING_HEARTBEAT" {
		t.Fatalf("alerts = %v", alerts.kinds)
	}
}

func TestTodo_TCLOCK_008_Security(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	device := DeviceRecord{TenantID: "tenant-a", ID: "device-a", SiteID: "site-a", ProfileID: "profile-a", State: "ACTIVE", Revision: 1}
	service := FleetService{
		Devices:  Service{Devices: fleetStatusDevices{devices: []DeviceRecord{device}}, Auth: authStub{}, Heartbeats: &fleetPresenceFake{records: map[string]HeartbeatRecord{}}, Clock: func() time.Time { return now }},
		Policies: fleetPolicyFake{policy: clock.FleetHealthPolicy{MaxDrift: time.Minute, MinimumVersion: "1.0.0", MissingHeartbeatLimit: time.Minute}},
		Hours:    fleetHoursFake{within: true},
	}
	if got, err := service.ListDeviceStatus(context.Background(), fleetPrincipal(t, "tenant-b", "admin", now), "site-a", ""); err != nil || len(got) != 0 {
		t.Fatalf("cross-tenant status got=%+v err=%v, want empty projection", got, err)
	}
	if _, err := service.DeviceHealth(context.Background(), fleetPrincipal(t, "tenant-a", "admin", now), ""); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("empty device error = %v", err)
	}
}

func TestTodo_TCLOCK_008_Fault(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	device := DeviceRecord{TenantID: "tenant-a", ID: "device-a", SiteID: "site-a", ProfileID: "profile-a", State: "ACTIVE", Revision: 1}
	service := FleetService{
		Devices:  Service{Devices: fleetStatusDevices{devices: []DeviceRecord{device}}, Auth: authStub{}, Heartbeats: &fleetPresenceFake{records: map[string]HeartbeatRecord{"device-a": {DeviceID: "device-a", AppVersion: "not-a-version", PowerState: "MAINS", ObservedAt: now}}}, Clock: func() time.Time { return now }},
		Policies: fleetPolicyFake{policy: clock.FleetHealthPolicy{MaxDrift: time.Minute, MinimumVersion: "1.0.0", MissingHeartbeatLimit: time.Minute}},
		Hours:    fleetHoursFake{within: false},
	}
	if _, err := service.DeviceHealth(context.Background(), fleetPrincipal(t, "tenant-a", "admin", now), "device-a"); !errors.Is(err, clock.ErrHeartbeatRejected) {
		t.Fatalf("malformed heartbeat error = %v", err)
	}
	service.Devices.Heartbeats.(*fleetPresenceFake).records["device-a"] = HeartbeatRecord{DeviceID: "device-a", AppVersion: "1.0.0", PowerState: "MAINS", OffsetMillis: math.MaxInt64, ObservedAt: now}
	if _, err := service.DeviceHealth(context.Background(), fleetPrincipal(t, "tenant-a", "admin", now), "device-a"); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("overflow heartbeat error = %v", err)
	}
	service.Hours = nil
	if _, err := service.DeviceHealth(context.Background(), fleetPrincipal(t, "tenant-a", "admin", now), "device-a"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("missing schedule error = %v", err)
	}
}

type fleetStatusDevices struct{ devices []DeviceRecord }

func (f fleetStatusDevices) GetDevice(_ context.Context, tenant, id string) (DeviceRecord, error) {
	for _, device := range f.devices {
		if device.TenantID == tenant && device.ID == id {
			return device, nil
		}
	}
	return DeviceRecord{}, ErrDeviceNotEligible
}

func (f fleetStatusDevices) DevicesBySite(_ context.Context, tenant, site string, _ int) ([]DeviceRecord, error) {
	out := make([]DeviceRecord, 0, len(f.devices))
	for _, device := range f.devices {
		if device.TenantID == tenant && device.SiteID == site {
			out = append(out, device)
		}
	}
	return out, nil
}
func (fleetStatusDevices) RotateDeviceKey(context.Context, string, string, []byte, string, string, int64) (DeviceRecord, error) {
	return DeviceRecord{}, nil
}
func (fleetStatusDevices) SuspendDevice(context.Context, string, string, string, string, int64) (DeviceRecord, error) {
	return DeviceRecord{}, nil
}
func (fleetStatusDevices) ResumeDevice(context.Context, string, string, string, string, int64) (DeviceRecord, error) {
	return DeviceRecord{}, nil
}
func (fleetStatusDevices) RevokeDevice(context.Context, string, string, string, string, int64) (DeviceRecord, error) {
	return DeviceRecord{}, nil
}
func (fleetStatusDevices) ReassignDeviceSite(context.Context, string, string, string, string, string, string, int64) (DeviceRecord, error) {
	return DeviceRecord{}, nil
}

type fleetPresenceFake struct{ records map[string]HeartbeatRecord }

func (f *fleetPresenceFake) RecordHeartbeat(_ context.Context, _ string, record HeartbeatRecord) error {
	f.records[record.DeviceID] = record
	return nil
}
func (f *fleetPresenceFake) LatestHeartbeat(_ context.Context, _ string, deviceID string) (HeartbeatRecord, error) {
	record, ok := f.records[deviceID]
	if !ok {
		return HeartbeatRecord{}, nil
	}
	return record, nil
}
func (f *fleetPresenceFake) LatestHeartbeatState(_ context.Context, _ string, deviceID string) (HeartbeatRecord, bool, error) {
	record, ok := f.records[deviceID]
	return record, ok, nil
}

type fleetHoursFake struct{ within bool }

func (f fleetHoursFake) WithinOperatingHours(context.Context, string, string, time.Time) (bool, error) {
	return f.within, nil
}
