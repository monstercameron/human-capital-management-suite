package clockservice

import (
	"context"
	"math"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/clock"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// HeartbeatRequest is the untrusted payload sent by an enrolled device.
// Thresholds and policy values are deliberately absent: they are resolved
// from the server-side profile configuration.
type HeartbeatRequest struct {
	DeviceID, AppVersion, PowerState   string
	QueueDepth, OldestUnsentAgeSeconds int
	BatteryPercent                     int
	HasBatteryPercent                  bool
	OffsetMillis                       int64
	ObservedAt                         time.Time
}

// HeartbeatResult is the server-side decision and receipt for a heartbeat.
type HeartbeatResult struct {
	Device     DeviceRecord
	Evaluation clock.HeartbeatEvaluation
	RecordedAt time.Time
}

// HeartbeatPolicySource resolves the pinned fleet policy for a device
// profile. Implementations must source these values from authenticated,
// tenant-scoped configuration rather than request data.
type HeartbeatPolicySource interface {
	FleetHealthPolicy(ctx context.Context, tenant, siteID, profileID string) (clock.FleetHealthPolicy, error)
}

// FleetAlertSink receives bounded fleet-health decisions. It carries no raw
// heartbeat payload, credentials, or client-controlled text.
type FleetAlertSink interface {
	EmitFleetAlert(ctx context.Context, tenant, siteID, deviceID, kind string, at time.Time) error
}

// FleetService composes the existing clock service with the pinned heartbeat
// policy and the shared fleet alerting port.
type FleetService struct {
	Devices  Service
	Policies HeartbeatPolicySource
	Alerts   FleetAlertSink
	Hours    FleetOperatingHours
}

// RecordFleetHeartbeat authenticates the enrolled device, evaluates its
// heartbeat against the server-side profile policy, records server receipt
// time as last-seen, and emits bounded drift/version decisions.
func (s FleetService) RecordFleetHeartbeat(ctx context.Context, p *trust.Principal, req HeartbeatRequest) (HeartbeatResult, error) {
	device, err := s.Devices.authenticatedRosterDevice(ctx, p, req.DeviceID)
	if err != nil {
		return HeartbeatResult{}, err
	}
	if s.Policies == nil {
		return HeartbeatResult{}, ErrUnavailable
	}
	policy, err := s.Policies.FleetHealthPolicy(ctx, device.TenantID, device.SiteID, device.ProfileID)
	if err != nil {
		return HeartbeatResult{}, err
	}
	hb, err := heartbeatDomainValue(req)
	if err != nil {
		return HeartbeatResult{}, err
	}
	now := s.Devices.now()
	evaluation, err := clock.EvaluateHeartbeat(policy, hb, now)
	if err != nil {
		return HeartbeatResult{}, reject(ErrInvalidRequest, "heartbeat", "REJECTED", err.Error())
	}
	if s.Devices.Heartbeats == nil {
		return HeartbeatResult{}, ErrUnavailable
	}
	stored := HeartbeatRecord{
		DeviceID: req.DeviceID, AppVersion: req.AppVersion, PowerState: req.PowerState,
		QueueDepth: req.QueueDepth, OldestUnsentAgeSeconds: req.OldestUnsentAgeSeconds,
		BatteryPercent: req.BatteryPercent, HasBatteryPercent: req.HasBatteryPercent,
		OffsetMillis: req.OffsetMillis, ObservedAt: now,
	}
	if err := s.Devices.Heartbeats.RecordHeartbeat(ctx, device.TenantID, stored); err != nil {
		return HeartbeatResult{}, err
	}
	if s.Alerts != nil {
		if evaluation.DriftExceeded {
			if err := s.Alerts.EmitFleetAlert(ctx, device.TenantID, device.SiteID, device.ID, "DRIFT_EXCEEDED", now); err != nil {
				return HeartbeatResult{}, err
			}
		}
		if !evaluation.VersionSupported {
			if err := s.Alerts.EmitFleetAlert(ctx, device.TenantID, device.SiteID, device.ID, "UNSUPPORTED_VERSION", now); err != nil {
				return HeartbeatResult{}, err
			}
		}
	}
	return HeartbeatResult{Device: device, Evaluation: evaluation, RecordedAt: now}, nil
}

func heartbeatDomainValue(req HeartbeatRequest) (clock.Heartbeat, error) {
	if req.OldestUnsentAgeSeconds < 0 || int64(req.OldestUnsentAgeSeconds) > math.MaxInt64/int64(time.Second) {
		return clock.Heartbeat{}, reject(ErrInvalidRequest, "oldest_unsent_age_seconds", "REJECTED", "duration overflows server range")
	}
	if req.OffsetMillis > math.MaxInt64/int64(time.Millisecond) || req.OffsetMillis < -math.MaxInt64/int64(time.Millisecond) {
		return clock.Heartbeat{}, reject(ErrInvalidRequest, "offset_millis", "REJECTED", "duration overflows server range")
	}
	battery := req.BatteryPercent
	if !req.HasBatteryPercent {
		battery = -1
	}
	hb := clock.Heartbeat{
		DeviceRef: req.DeviceID, AppVersion: req.AppVersion,
		QueueDepth: req.QueueDepth, OldestUnsentAge: time.Duration(req.OldestUnsentAgeSeconds) * time.Second,
		BatteryPercent: battery, Power: clock.PowerState(req.PowerState),
		MeasuredOffset: time.Duration(req.OffsetMillis) * time.Millisecond, SentAt: req.ObservedAt,
	}
	if err := hb.Validate(); err != nil {
		return clock.Heartbeat{}, reject(ErrInvalidRequest, "heartbeat", "REJECTED", err.Error())
	}
	return hb, nil
}

// SyncRoster authenticates the enrolled device and refuses to publish a
// roster to a device below the pinned minimum version. The ordinary roster
// implementation remains on Service; this wrapper owns the fleet gate.
func (s FleetService) SyncRoster(ctx context.Context, p *trust.Principal, deviceID, cursor string) (RosterDelta, error) {
	device, err := s.Devices.authenticatedRosterDevice(ctx, p, deviceID)
	if err != nil {
		return RosterDelta{}, err
	}
	if s.Policies == nil || s.Devices.Heartbeats == nil {
		return RosterDelta{}, ErrUnavailable
	}
	policy, err := s.Policies.FleetHealthPolicy(ctx, device.TenantID, device.SiteID, device.ProfileID)
	if err != nil {
		return RosterDelta{}, err
	}
	latest, err := s.Devices.Heartbeats.LatestHeartbeat(ctx, device.TenantID, device.ID)
	if err != nil {
		return RosterDelta{}, err
	}
	supported, err := clock.VersionAtLeast(latest.AppVersion, policy.MinimumVersion)
	if err != nil {
		return RosterDelta{}, reject(ErrInvalidRequest, "app_version", "REJECTED", err.Error())
	}
	if !supported {
		return RosterDelta{}, reject(ErrDeviceNotEligible, "app_version", "UNSUPPORTED", "device version is below the pinned minimum")
	}
	return s.Devices.SyncRoster(ctx, p, deviceID, cursor)
}
