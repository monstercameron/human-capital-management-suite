package timeclockstore

import (
	"context"

	"github.com/monstercameron/human-capital-management-suite/internal/application/clockservice"
	"github.com/monstercameron/human-capital-management-suite/internal/data/timestore"
)

// HeartbeatAdapter maps fleet-health reports to timestore.
type HeartbeatAdapter struct{ Store *timestore.Store }

var _ clockservice.HeartbeatStore = HeartbeatAdapter{}

// RecordHeartbeat atomically updates latest state and appends history.
func (a HeartbeatAdapter) RecordHeartbeat(ctx context.Context, tenant string, hb clockservice.HeartbeatRecord) error {
	if a.Store == nil {
		return clockservice.ErrUnavailable
	}
	var battery *int
	if hb.HasBatteryPercent {
		battery = &hb.BatteryPercent
	}
	return a.Store.RecordHeartbeat(ctx, tenant, timestore.Heartbeat{TenantID: tenant, DeviceID: hb.DeviceID, AppVersion: hb.AppVersion, PowerState: hb.PowerState, QueueDepth: hb.QueueDepth, OldestUnsentAgeSeconds: hb.OldestUnsentAgeSeconds, BatteryPercent: battery, OffsetMillis: hb.OffsetMillis, ObservedAt: hb.ObservedAt})
}

// LatestHeartbeat returns the latest tenant-scoped report.
func (a HeartbeatAdapter) LatestHeartbeat(ctx context.Context, tenant, deviceID string) (clockservice.HeartbeatRecord, error) {
	if a.Store == nil {
		return clockservice.HeartbeatRecord{}, clockservice.ErrUnavailable
	}
	h, err := a.Store.LatestHeartbeat(ctx, tenant, deviceID)
	if err != nil {
		return clockservice.HeartbeatRecord{}, err
	}
	out := clockservice.HeartbeatRecord{DeviceID: h.DeviceID, AppVersion: h.AppVersion, PowerState: h.PowerState, QueueDepth: h.QueueDepth, OldestUnsentAgeSeconds: h.OldestUnsentAgeSeconds, OffsetMillis: h.OffsetMillis, ObservedAt: h.ObservedAt}
	if h.BatteryPercent != nil {
		out.BatteryPercent = *h.BatteryPercent
		out.HasBatteryPercent = true
	}
	return out, nil
}
