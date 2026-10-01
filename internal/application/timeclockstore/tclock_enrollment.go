package timeclockstore

import (
	"bytes"
	"context"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/application/clockservice"
	"github.com/monstercameron/human-capital-management-suite/internal/data/timestore"
	clock "github.com/monstercameron/human-capital-management-suite/internal/domains/clock"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// VerifiedRegistrySource supplies a registry that was authenticated and
// pinned by the composition root. Implementations must reject stale or
// unverified registry material before returning it.
type VerifiedRegistrySource interface {
	Resolve(ctx context.Context, tenant string, now time.Time) (clock.VerifiedProfileRegistry, error)
}

// EnrollmentAdapter maps the legacy enrollment port to timestore's atomic
// single-use code redemption. Proof-bound enrollment remains unavailable
// until the data package persists and validates profile-registry evidence.
type EnrollmentAdapter struct {
	Store    *timestore.Store
	Registry VerifiedRegistrySource
	Clock    func() time.Time
}

var _ clockservice.EnrollmentStore = EnrollmentAdapter{}
var _ clockservice.DeviceEnrollmentStore = EnrollmentAdapter{}

// CreateEnrollmentCode stores only the hash of the bearer code.
func (a EnrollmentAdapter) CreateEnrollmentCode(ctx context.Context, tenant, code, siteID, profileID, timezone, createdBy string, expiresAt time.Time) error {
	if a.Store == nil {
		return clockservice.ErrUnavailable
	}
	return a.Store.CreateEnrollmentCode(ctx, tenant, code, siteID, profileID, timezone, createdBy, expiresAt)
}

// RedeemEnrollmentCode atomically consumes the code and creates the device.
func (a EnrollmentAdapter) RedeemEnrollmentCode(ctx context.Context, tenant, code, deviceID string, key []byte, actorID string, now time.Time) (clockservice.DeviceRecord, error) {
	if a.Store == nil {
		return clockservice.DeviceRecord{}, clockservice.ErrUnavailable
	}
	d, err := a.Store.RedeemEnrollmentCode(ctx, tenant, code, deviceID, key, actorID, now)
	return deviceRecord(d), err
}

// LoadEnrollment reads the durable code and binds its stored profile ID to
// the exact source class present in the authenticated registry. Unknown IDs
// are rejected rather than guessed from a vendor or transport string.
func (a EnrollmentAdapter) LoadEnrollment(ctx context.Context, tenant, code string) (clock.EnrollmentCode, clock.ProfileRegistry, error) {
	if a.Clock == nil {
		return clock.EnrollmentCode{}, clock.ProfileRegistry{}, clockservice.ErrUnavailable
	}
	return a.LoadEnrollmentAt(ctx, tenant, code, a.Clock().UTC())
}

// RedeemEnrollment verifies the supplied key proof and registry binding
// before delegating code consumption and device creation to timestore's
// atomic update transaction.
func (a EnrollmentAdapter) RedeemEnrollment(ctx context.Context, tenant, code, deviceID string, proof clock.KeyPossessionProof, actorID string, now time.Time) (clockservice.DeviceRecord, error) {
	if a.Store == nil || a.Registry == nil {
		return clockservice.DeviceRecord{}, clockservice.ErrUnavailable
	}
	codeRow, registry, err := a.LoadEnrollmentAt(ctx, tenant, code, now)
	if err != nil {
		return clockservice.DeviceRecord{}, err
	}
	if !bytes.Equal(proof.Challenge, []byte(code)) {
		return clockservice.DeviceRecord{}, clock.ErrEnrollmentRejected
	}
	if err := proof.Verify(); err != nil {
		return clockservice.DeviceRecord{}, err
	}
	if _, err := clock.Enroll(clock.EnrollmentRequest{Code: codeRow, Proof: proof, DeviceRef: deviceID, Registry: registry, Now: now}); err != nil {
		return clockservice.DeviceRecord{}, err
	}
	d, err := a.Store.RedeemEnrollmentCode(ctx, tenant, code, deviceID, proof.PublicKey, actorID, now)
	return deviceRecord(d), err
}

func (a EnrollmentAdapter) LoadEnrollmentAt(ctx context.Context, tenant, code string, now time.Time) (clock.EnrollmentCode, clock.ProfileRegistry, error) {
	if a.Store == nil || a.Registry == nil {
		return clock.EnrollmentCode{}, clock.ProfileRegistry{}, clockservice.ErrUnavailable
	}
	row, err := a.Store.GetEnrollmentCode(ctx, tenant, code)
	if err != nil {
		return clock.EnrollmentCode{}, clock.ProfileRegistry{}, err
	}
	verified, err := a.Registry.Resolve(ctx, tenant, now)
	if err != nil {
		return clock.EnrollmentCode{}, clock.ProfileRegistry{}, err
	}
	if err := clock.ValidateProfileRegistry(verified.Registry); err != nil || now.Before(verified.ValidFrom) || !now.Before(verified.ValidUntil) {
		return clock.EnrollmentCode{}, clock.ProfileRegistry{}, clock.ErrEnrollmentRejected
	}
	profile := clock.EnrollmentCode{CodeRef: code, Tenant: values.TenantId(tenant), SiteRef: row.SiteID, Profile: clock.SourceClass(row.ProfileID), Timezone: row.Timezone, IssuedAt: row.CreatedAt, ExpiresAt: row.ExpiresAt, Consumed: row.UsedAt != nil}
	if err := profile.Validate(); err != nil {
		return clock.EnrollmentCode{}, clock.ProfileRegistry{}, err
	}
	if err := verified.Registry.Accepts(profile.Profile, ""); err != nil {
		return clock.EnrollmentCode{}, clock.ProfileRegistry{}, err
	}
	return profile, verified.Registry, nil
}
