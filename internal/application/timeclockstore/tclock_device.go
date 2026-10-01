package timeclockstore

import (
	"bytes"
	"context"

	"github.com/monstercameron/human-capital-management-suite/internal/application/clockservice"
	"github.com/monstercameron/human-capital-management-suite/internal/data/timestore"
	clock "github.com/monstercameron/human-capital-management-suite/internal/domains/clock"
)

// DeviceAdapter maps revisioned device lifecycle operations to timestore.
type DeviceAdapter struct{ Store *timestore.Store }

var _ clockservice.DeviceStore = DeviceAdapter{}

// GetDevice loads one tenant-scoped device.
func (a DeviceAdapter) GetDevice(ctx context.Context, tenant, id string) (clockservice.DeviceRecord, error) {
	if a.Store == nil {
		return clockservice.DeviceRecord{}, clockservice.ErrUnavailable
	}
	d, err := a.Store.GetDevice(ctx, tenant, id)
	return deviceRecord(d), err
}

// DevicesBySite lists tenant devices for a site.
func (a DeviceAdapter) DevicesBySite(ctx context.Context, tenant, siteID string, limit int) ([]clockservice.DeviceRecord, error) {
	if a.Store == nil {
		return nil, clockservice.ErrUnavailable
	}
	rows, err := a.Store.DevicesBySite(ctx, tenant, siteID, limit)
	if err != nil {
		return nil, err
	}
	out := make([]clockservice.DeviceRecord, len(rows))
	for i := range rows {
		out[i] = deviceRecord(rows[i])
	}
	return out, nil
}

// RotateDeviceKey records a revisioned key rotation.
func (a DeviceAdapter) RotateDeviceKey(ctx context.Context, tenant, id string, key []byte, actor, reason string, rev int64) (clockservice.DeviceRecord, error) {
	if a.Store == nil {
		return clockservice.DeviceRecord{}, clockservice.ErrUnavailable
	}
	d, err := a.Store.RotateDeviceKey(ctx, tenant, id, key, actor, reason, rev)
	return deviceRecord(d), err
}

// RotateDeviceKeyProof verifies possession over the server-derived challenge
// before applying timestore's revision-checked key rotation.
func (a DeviceAdapter) RotateDeviceKeyProof(ctx context.Context, tenant, id string, proof clock.KeyPossessionProof, actor, reason string, rev int64) (clockservice.DeviceRecord, error) {
	if a.Store == nil {
		return clockservice.DeviceRecord{}, clockservice.ErrUnavailable
	}
	challenge := clockservice.RotationChallenge(tenant, id, rev)
	if !bytes.Equal(proof.Challenge, challenge) {
		return clockservice.DeviceRecord{}, clock.ErrEnrollmentRejected
	}
	if err := proof.Verify(); err != nil {
		return clockservice.DeviceRecord{}, err
	}
	d, err := a.Store.RotateDeviceKey(ctx, tenant, id, proof.PublicKey, actor, reason, rev)
	return deviceRecord(d), err
}

// SuspendDevice records a revisioned suspension.
func (a DeviceAdapter) SuspendDevice(ctx context.Context, tenant, id, actor, reason string, rev int64) (clockservice.DeviceRecord, error) {
	if a.Store == nil {
		return clockservice.DeviceRecord{}, clockservice.ErrUnavailable
	}
	d, err := a.Store.SuspendDevice(ctx, tenant, id, actor, reason, rev)
	return deviceRecord(d), err
}

// ResumeDevice records a revisioned resumption.
func (a DeviceAdapter) ResumeDevice(ctx context.Context, tenant, id, actor, reason string, rev int64) (clockservice.DeviceRecord, error) {
	if a.Store == nil {
		return clockservice.DeviceRecord{}, clockservice.ErrUnavailable
	}
	d, err := a.Store.ResumeDevice(ctx, tenant, id, actor, reason, rev)
	return deviceRecord(d), err
}

// RevokeDevice records a revisioned revocation.
func (a DeviceAdapter) RevokeDevice(ctx context.Context, tenant, id, actor, reason string, rev int64) (clockservice.DeviceRecord, error) {
	if a.Store == nil {
		return clockservice.DeviceRecord{}, clockservice.ErrUnavailable
	}
	d, err := a.Store.RevokeDevice(ctx, tenant, id, actor, reason, rev)
	return deviceRecord(d), err
}

// ReassignDeviceSite records a revisioned site and timezone move.
func (a DeviceAdapter) ReassignDeviceSite(ctx context.Context, tenant, id, site, timezone, actor, reason string, rev int64) (clockservice.DeviceRecord, error) {
	if a.Store == nil {
		return clockservice.DeviceRecord{}, clockservice.ErrUnavailable
	}
	d, err := a.Store.ReassignDeviceSite(ctx, tenant, id, site, timezone, actor, reason, rev)
	return deviceRecord(d), err
}
