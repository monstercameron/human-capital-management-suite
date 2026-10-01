package clockservice

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/clock"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

func TestTodo_TCLOCK_008(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	device := DeviceRecord{TenantID: "tenant-a", ID: "device-a", SiteID: "site-a", ProfileID: "profile-a", Timezone: "UTC", State: "ACTIVE", Revision: 1, PublicKey: make([]byte, 32)}
	policies := fleetPolicyFake{policy: clock.FleetHealthPolicy{MaxDrift: time.Minute, MinimumVersion: "2.4.0", MissingHeartbeatLimit: 15 * time.Minute}}
	store := &fleetHeartbeatFake{}
	sink := &fleetAlertFake{}
	s := FleetService{Devices: Service{Devices: fleetDeviceFake{device: device}, Heartbeats: store, Clock: func() time.Time { return now }}, Policies: policies, Alerts: sink}
	p := fleetPrincipal(t, "tenant-a", "device-a", now)

	t.Run("records server last seen and emits bounded decisions", func(t *testing.T) {
		got, err := s.RecordFleetHeartbeat(context.Background(), p, HeartbeatRequest{
			DeviceID: "device-a", AppVersion: "2.3.0", PowerState: "BATTERY", QueueDepth: 2,
			OldestUnsentAgeSeconds: 10, BatteryPercent: 20, HasBatteryPercent: true,
			OffsetMillis: 61_000, ObservedAt: now.Add(-time.Minute),
		})
		if err != nil {
			t.Fatalf("RecordFleetHeartbeat: %v", err)
		}
		if !got.Evaluation.DriftExceeded || got.Evaluation.VersionSupported {
			t.Fatalf("evaluation = %+v", got.Evaluation)
		}
		if !store.last.ObservedAt.Equal(now) {
			t.Fatalf("last seen = %v, want server time %v", store.last.ObservedAt, now)
		}
		if len(sink.kinds) != 2 || sink.kinds[0] != "DRIFT_EXCEEDED" || sink.kinds[1] != "UNSUPPORTED_VERSION" {
			t.Fatalf("alerts = %v", sink.kinds)
		}
	})

	t.Run("rejects malformed queue age and forged identity", func(t *testing.T) {
		_, err := s.RecordFleetHeartbeat(context.Background(), p, HeartbeatRequest{DeviceID: "device-a", AppVersion: "2.4.0", PowerState: "MAINS", OldestUnsentAgeSeconds: -1, ObservedAt: now})
		if !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("negative age error = %v", err)
		}
		_, err = s.RecordFleetHeartbeat(context.Background(), p, HeartbeatRequest{DeviceID: "device-other", AppVersion: "2.4.0", PowerState: "MAINS", ObservedAt: now})
		if !errors.Is(err, ErrDeviceNotEligible) {
			t.Fatalf("forged identity error = %v", err)
		}
	})

	t.Run("blocks roster sync for unsupported version", func(t *testing.T) {
		_, err := s.SyncRoster(context.Background(), p, "device-a", "")
		if !errors.Is(err, ErrDeviceNotEligible) {
			t.Fatalf("unsupported sync error = %v", err)
		}
	})

	t.Run("rejects offset overflow and preserves store failure", func(t *testing.T) {
		_, err := s.RecordFleetHeartbeat(context.Background(), p, HeartbeatRequest{DeviceID: "device-a", AppVersion: "2.4.0", PowerState: "MAINS", OffsetMillis: math.MaxInt64, ObservedAt: now})
		if !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("overflow error = %v", err)
		}
		store.err = errors.New("store unavailable")
		_, err = s.RecordFleetHeartbeat(context.Background(), p, HeartbeatRequest{DeviceID: "device-a", AppVersion: "2.4.0", PowerState: "MAINS", ObservedAt: now})
		if !errors.Is(err, store.err) {
			t.Fatalf("store error = %v", err)
		}
	})
}

func fleetPrincipal(t *testing.T, tenant, client string, now time.Time) *trust.Principal {
	t.Helper()
	p, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: values.TenantId(tenant), Subject: "service-subject", ClientID: client, SubjectKind: trust.SubjectKindService, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceSubstantial, SessionRef: "session", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Minute), CredentialDigest: "digest"})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

type fleetDeviceFake struct{ device DeviceRecord }

func (f fleetDeviceFake) GetDevice(context.Context, string, string) (DeviceRecord, error) {
	return f.device, nil
}
func (f fleetDeviceFake) DevicesBySite(context.Context, string, string, int) ([]DeviceRecord, error) {
	return nil, nil
}
func (f fleetDeviceFake) RotateDeviceKey(context.Context, string, string, []byte, string, string, int64) (DeviceRecord, error) {
	return DeviceRecord{}, nil
}
func (f fleetDeviceFake) SuspendDevice(context.Context, string, string, string, string, int64) (DeviceRecord, error) {
	return DeviceRecord{}, nil
}
func (f fleetDeviceFake) ResumeDevice(context.Context, string, string, string, string, int64) (DeviceRecord, error) {
	return DeviceRecord{}, nil
}
func (f fleetDeviceFake) RevokeDevice(context.Context, string, string, string, string, int64) (DeviceRecord, error) {
	return DeviceRecord{}, nil
}
func (f fleetDeviceFake) ReassignDeviceSite(context.Context, string, string, string, string, string, string, int64) (DeviceRecord, error) {
	return DeviceRecord{}, nil
}

type fleetPolicyFake struct{ policy clock.FleetHealthPolicy }

func (f fleetPolicyFake) FleetHealthPolicy(context.Context, string, string, string) (clock.FleetHealthPolicy, error) {
	return f.policy, nil
}

type fleetHeartbeatFake struct {
	last HeartbeatRecord
	err  error
}

func (f *fleetHeartbeatFake) RecordHeartbeat(_ context.Context, _ string, hb HeartbeatRecord) error {
	if f.err != nil {
		return f.err
	}
	f.last = hb
	return nil
}
func (f *fleetHeartbeatFake) LatestHeartbeat(context.Context, string, string) (HeartbeatRecord, error) {
	return f.last, nil
}

type fleetAlertFake struct{ kinds []string }

func (f *fleetAlertFake) EmitFleetAlert(_ context.Context, _, _, _, kind string, _ time.Time) error {
	f.kinds = append(f.kinds, kind)
	return nil
}
