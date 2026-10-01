package clockservice

import (
	"context"
	"math"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/clock"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// FleetOperatingHours is the site-owned schedule seam used by fleet
// monitoring. The monitor never derives working hours from a timezone or a
// device-provided timestamp.
type FleetOperatingHours interface {
	WithinOperatingHours(context.Context, string, string, time.Time) (bool, error)
}

// HeartbeatPresenceStore is an optional extension of HeartbeatStore that
// distinguishes a device with no heartbeat from a store failure. Existing
// stores may continue to implement LatestHeartbeat; production adapters can
// add this extension without changing the heartbeat write contract.
type HeartbeatPresenceStore interface {
	LatestHeartbeatState(context.Context, string, string) (HeartbeatRecord, bool, error)
}

// FleetDeviceStatus is the bounded admin projection for one enrolled device.
// Heartbeat carries operational facts already accepted by the service; it
// never contains credentials or raw device assertions.
type FleetDeviceStatus struct {
	Device             DeviceRecord
	Heartbeat          *HeartbeatRecord
	Status             string
	DriftExceeded      bool
	VersionSupported   bool
	MissingHeartbeat   bool
	WithinOperatingHrs bool
}

const (
	FleetStatusOnline             = "ONLINE"
	FleetStatusOffline            = "OFFLINE"
	FleetStatusNeverSeen          = "NEVER_SEEN"
	FleetStatusSuspended          = "SUSPENDED"
	FleetStatusRevoked            = "REVOKED"
	FleetStatusDrifting           = "DRIFTING"
	FleetStatusUnsupportedVersion = "UNSUPPORTED_VERSION"
)

// DeviceHealth evaluates one enrolled device against its server-side fleet
// policy and emits a missing-heartbeat alert when the site's schedule says
// the device should be operating. It is intentionally an application
// projection: heartbeat persistence and shared alerting remain separate
// ports.
func (s FleetService) DeviceHealth(ctx context.Context, p *trust.Principal, deviceID string) (FleetDeviceStatus, error) {
	if err := s.Devices.validAdminPrincipal(p); err != nil {
		return FleetDeviceStatus{}, err
	}
	if strings.TrimSpace(deviceID) == "" || s.Devices.Devices == nil || s.Devices.Auth == nil {
		return FleetDeviceStatus{}, ErrUnavailable
	}
	device, err := s.Devices.Devices.GetDevice(ctx, tenantOf(p), deviceID)
	if err != nil {
		return FleetDeviceStatus{}, err
	}
	if device.TenantID != tenantOf(p) || device.ID != deviceID || strings.TrimSpace(device.SiteID) == "" {
		return FleetDeviceStatus{}, ErrDeviceNotEligible
	}
	if err := s.Devices.Auth.AuthorizeDeviceAdmin(ctx, p, tenantOf(p), device.SiteID); err != nil {
		return FleetDeviceStatus{}, err
	}
	return s.evaluateDeviceHealth(ctx, device)
}

// ListDeviceStatus returns the site-scoped fleet projection used by an admin
// view. Filtering is performed after server-side evaluation so a caller
// cannot make an unhealthy device appear healthy by selecting a status.
func (s FleetService) ListDeviceStatus(ctx context.Context, p *trust.Principal, siteID, status string) ([]FleetDeviceStatus, error) {
	if err := s.Devices.validAdminPrincipal(p); err != nil {
		return nil, err
	}
	if strings.TrimSpace(siteID) == "" || s.Devices.Devices == nil || s.Devices.Auth == nil {
		return nil, ErrUnavailable
	}
	if err := s.Devices.Auth.AuthorizeDeviceAdmin(ctx, p, tenantOf(p), siteID); err != nil {
		return nil, err
	}
	devices, err := s.Devices.Devices.DevicesBySite(ctx, tenantOf(p), siteID, 1000)
	if err != nil {
		return nil, err
	}
	out := make([]FleetDeviceStatus, 0, len(devices))
	for _, device := range devices {
		if device.TenantID != tenantOf(p) || device.SiteID != siteID {
			return nil, ErrDeviceNotEligible
		}
		health, err := s.evaluateDeviceHealth(ctx, device)
		if err != nil {
			return nil, err
		}
		if status == "" || health.Status == status {
			out = append(out, health)
		}
	}
	return out, nil
}

func (s FleetService) evaluateDeviceHealth(ctx context.Context, device DeviceRecord) (FleetDeviceStatus, error) {
	if device.State == "SUSPENDED" {
		return FleetDeviceStatus{Device: device, Status: FleetStatusSuspended}, nil
	}
	if device.State == "REVOKED" {
		return FleetDeviceStatus{Device: device, Status: FleetStatusRevoked}, nil
	}
	if s.Policies == nil || s.Devices.Heartbeats == nil || s.Hours == nil {
		return FleetDeviceStatus{}, ErrUnavailable
	}
	policy, err := s.Policies.FleetHealthPolicy(ctx, device.TenantID, device.SiteID, device.ProfileID)
	if err != nil {
		return FleetDeviceStatus{}, err
	}
	within, err := s.Hours.WithinOperatingHours(ctx, device.TenantID, device.SiteID, s.Devices.now())
	if err != nil {
		return FleetDeviceStatus{}, err
	}
	latest, present, err := latestHeartbeatState(ctx, s.Devices.Heartbeats, device.TenantID, device.ID)
	if err != nil {
		return FleetDeviceStatus{}, err
	}
	health := FleetDeviceStatus{Device: device, WithinOperatingHrs: within}
	if !present {
		health.Status = FleetStatusNeverSeen
		health.MissingHeartbeat = within
		if within && s.Alerts != nil {
			if err := s.Alerts.EmitFleetAlert(ctx, device.TenantID, device.SiteID, device.ID, "MISSING_HEARTBEAT", s.Devices.now()); err != nil {
				return FleetDeviceStatus{}, err
			}
		}
		return health, nil
	}
	health.Heartbeat = &latest
	hb, err := heartbeatFromRecord(latest)
	if err != nil {
		return FleetDeviceStatus{}, err
	}
	evaluation, err := clock.EvaluateHeartbeat(policy, hb, s.Devices.now())
	if err != nil {
		return FleetDeviceStatus{}, err
	}
	health.DriftExceeded = evaluation.DriftExceeded
	health.VersionSupported = evaluation.VersionSupported
	missing, err := clock.MissingHeartbeatAlert(policy, latest.ObservedAt, s.Devices.now(), within)
	if err != nil {
		return FleetDeviceStatus{}, err
	}
	health.MissingHeartbeat = missing
	if missing {
		health.Status = FleetStatusOffline
		if s.Alerts != nil {
			if err := s.Alerts.EmitFleetAlert(ctx, device.TenantID, device.SiteID, device.ID, "MISSING_HEARTBEAT", s.Devices.now()); err != nil {
				return FleetDeviceStatus{}, err
			}
		}
	} else if !evaluation.VersionSupported {
		health.Status = FleetStatusUnsupportedVersion
	} else if evaluation.DriftExceeded {
		health.Status = FleetStatusDrifting
	} else {
		health.Status = FleetStatusOnline
	}
	return health, nil
}

func latestHeartbeatState(ctx context.Context, store HeartbeatStore, tenant, deviceID string) (HeartbeatRecord, bool, error) {
	if state, ok := store.(HeartbeatPresenceStore); ok {
		return state.LatestHeartbeatState(ctx, tenant, deviceID)
	}
	record, err := store.LatestHeartbeat(ctx, tenant, deviceID)
	if err != nil {
		return HeartbeatRecord{}, false, err
	}
	return record, !record.ObservedAt.IsZero(), nil
}

func heartbeatFromRecord(record HeartbeatRecord) (clock.Heartbeat, error) {
	if record.OldestUnsentAgeSeconds < 0 || int64(record.OldestUnsentAgeSeconds) > math.MaxInt64/int64(time.Second) {
		return clock.Heartbeat{}, reject(ErrInvalidRequest, "oldest_unsent_age_seconds", "REJECTED", "duration overflows server range")
	}
	if record.OffsetMillis > math.MaxInt64/int64(time.Millisecond) || record.OffsetMillis < -math.MaxInt64/int64(time.Millisecond) {
		return clock.Heartbeat{}, reject(ErrInvalidRequest, "offset_millis", "REJECTED", "duration overflows server range")
	}
	battery := record.BatteryPercent
	if !record.HasBatteryPercent {
		battery = -1
	}
	return clock.Heartbeat{
		DeviceRef: record.DeviceID, AppVersion: record.AppVersion,
		QueueDepth: record.QueueDepth, OldestUnsentAge: time.Duration(record.OldestUnsentAgeSeconds) * time.Second,
		BatteryPercent: battery, Power: clock.PowerState(record.PowerState),
		MeasuredOffset: time.Duration(record.OffsetMillis) * time.Millisecond, SentAt: record.ObservedAt,
	}, nil
}
