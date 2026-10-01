package timeclockstore

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/application/clockservice"
	"github.com/monstercameron/human-capital-management-suite/internal/data/timestore"
	clock "github.com/monstercameron/human-capital-management-suite/internal/domains/clock"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/partnerapp"
)

// MachineClientRegistrar is the core machine-client registry with governed
// registration operations. It stores public keys only.
type MachineClientRegistrar interface {
	partnerapp.ClientRegistry
	RegisterClient(context.Context, string, partnerapp.MachineClient) error
	RegisterClientKey(context.Context, string, partnerapp.MachineClientKey) error
}

// MachineEnrollmentAdapter bridges the durable time enrollment saga to the
// existing core machine-client registry.
type MachineEnrollmentAdapter struct {
	Store    *timestore.Store
	Registry MachineClientRegistrar
	Profiles VerifiedRegistrySource
	Clock    func() time.Time
}

var _ clockservice.MachineClientEnrollmentStore = MachineEnrollmentAdapter{}

// LoadEnrollment resolves the durable code and authenticated profile registry.
func (a MachineEnrollmentAdapter) LoadEnrollment(ctx context.Context, tenant, code string) (clock.EnrollmentCode, clock.ProfileRegistry, error) {
	if a.Store == nil || a.Profiles == nil || a.Clock == nil {
		return clock.EnrollmentCode{}, clock.ProfileRegistry{}, clockservice.ErrUnavailable
	}
	return (EnrollmentAdapter{Store: a.Store, Registry: a.Profiles, Clock: a.Clock}).LoadEnrollment(ctx, tenant, code)
}

// PrepareMachineEnrollment consumes the code and writes the pending saga row.
func (a MachineEnrollmentAdapter) PrepareMachineEnrollment(ctx context.Context, in clockservice.MachineClientEnrollment) (clockservice.MachineBootstrapPending, error) {
	if a.Store == nil {
		return clockservice.MachineBootstrapPending{}, clockservice.ErrUnavailable
	}
	p, err := a.Store.PrepareMachineEnrollment(ctx, timestore.MachineEnrollmentInput{Tenant: in.Tenant, EnrollmentCode: in.EnrollmentCode, DeviceID: in.DeviceID, MachineClientID: in.MachineClientID, Owner: in.Owner, SiteID: in.SiteID, ProfileID: in.ProfileID, Timezone: in.Timezone, PublicKey: in.PublicKey, Challenge: in.Proof.Challenge, Signature: in.Proof.Signature, KeyID: []byte(in.KeyID), Scopes: in.Scopes, Purpose: in.Purpose, ExpiresAt: in.ExpiresAt, IdempotencyKey: in.IdempotencyKey, Now: in.Now})
	if err != nil {
		return clockservice.MachineBootstrapPending{}, err
	}
	return clockservice.MachineBootstrapPending{Enrollment: in, Device: deviceRecord(timestore.Device{TenantID: p.Tenant, ID: p.DeviceID, PublicKey: p.PublicKey, SiteID: p.SiteID, ProfileID: p.ProfileID, Timezone: p.Timezone, State: p.State, Revision: 1})}, nil
}

// PendingMachineEnrollmentFor recovers a pending saga by canonical device ID.
func (a MachineEnrollmentAdapter) PendingMachineEnrollmentFor(ctx context.Context, tenant, deviceID string) (clockservice.MachineBootstrapPending, bool, error) {
	if a.Store == nil {
		return clockservice.MachineBootstrapPending{}, false, clockservice.ErrUnavailable
	}
	p, err := a.Store.PendingMachineEnrollmentFor(ctx, tenant, deviceID)
	if err != nil {
		if err == timestore.ErrNotFound {
			return clockservice.MachineBootstrapPending{}, false, nil
		}
		return clockservice.MachineBootstrapPending{}, false, err
	}
	e := clockservice.MachineClientEnrollment{Tenant: p.Tenant, DeviceID: p.DeviceID, MachineClientID: p.MachineClientID, Owner: p.Owner, SiteID: p.SiteID, ProfileID: p.ProfileID, Timezone: p.Timezone, PublicKey: append([]byte(nil), p.PublicKey...), KeyID: string(p.KeyID), Proof: clock.KeyPossessionProof{PublicKey: append([]byte(nil), p.PublicKey...), Challenge: append([]byte(nil), p.Challenge...), Signature: append([]byte(nil), p.Signature...)}, Scopes: append([]string(nil), p.Scopes...), Purpose: p.Purpose, ExpiresAt: p.ExpiresAt, IdempotencyKey: p.IdempotencyKey, Now: p.Now}
	return clockservice.MachineBootstrapPending{Enrollment: e, Device: deviceRecord(timestore.Device{TenantID: p.Tenant, ID: p.DeviceID, PublicKey: p.PublicKey, SiteID: p.SiteID, ProfileID: p.ProfileID, Timezone: p.Timezone, State: p.State, Revision: 1})}, true, nil
}

// EnsureMachineClient persists the real core identity and public assertion key.
func (a MachineEnrollmentAdapter) EnsureMachineClient(ctx context.Context, in clockservice.MachineBootstrapPending) (string, error) {
	if a.Registry == nil {
		return "", clockservice.ErrUnavailable
	}
	e := in.Enrollment
	client := partnerapp.MachineClient{Tenant: e.Tenant, ClientID: e.MachineClientID, Owner: e.Owner, Status: partnerapp.MachineClientActive, Scopes: append([]string(nil), e.Scopes...), Purpose: e.Purpose, ExpiresAt: e.ExpiresAt}
	if err := a.Registry.RegisterClient(ctx, e.Tenant, client); err != nil {
		existing, loadErr := a.Registry.LoadClient(ctx, e.Tenant, e.MachineClientID)
		if loadErr != nil || !sameCoreClient(existing, client) {
			return "", err
		}
	}
	key := partnerapp.MachineClientKey{ClientID: e.MachineClientID, KID: e.KeyID, Alg: "EdDSA", JWK: ed25519JWK(e.PublicKey), NotBefore: e.Now, ExpiresAt: e.ExpiresAt}
	if err := a.Registry.RegisterClientKey(ctx, e.Tenant, key); err != nil {
		keys, loadErr := a.Registry.LoadClientKeys(ctx, e.Tenant, e.MachineClientID)
		if loadErr != nil || !hasCoreKey(keys, key) {
			return "", err
		}
	}
	verified, err := a.Registry.LoadClient(ctx, e.Tenant, e.MachineClientID)
	if err != nil || !sameCoreClient(verified, client) {
		return "", clockservice.ErrUnavailable
	}
	return verified.ClientID, nil
}

// ActivateMachineEnrollment activates only after a real registry reference exists.
func (a MachineEnrollmentAdapter) ActivateMachineEnrollment(ctx context.Context, tenant, deviceID, credentialRef string) (clockservice.DeviceRecord, error) {
	if a.Store == nil || a.Registry == nil || strings.TrimSpace(credentialRef) == "" {
		return clockservice.DeviceRecord{}, clockservice.ErrUnavailable
	}
	client, err := a.Registry.LoadClient(ctx, tenant, credentialRef)
	now := time.Now().UTC()
	if a.Clock != nil {
		now = a.Clock().UTC()
	}
	if err != nil || client.ClientID != credentialRef || client.Tenant != tenant || client.Status != partnerapp.MachineClientActive || client.Revoked || (!client.ExpiresAt.IsZero() && !now.Before(client.ExpiresAt)) {
		return clockservice.DeviceRecord{}, clockservice.ErrUnavailable
	}
	keys, err := a.Registry.LoadClientKeys(ctx, tenant, credentialRef)
	pending, pendingErr := a.Store.PendingMachineEnrollmentFor(ctx, tenant, deviceID)
	if err != nil || pendingErr != nil || len(keys) != 1 || keys[0].Revoked || keys[0].Alg != "EdDSA" || keys[0].ClientID != pending.MachineClientID || keys[0].KID != string(pending.KeyID) || !sameJWK(keys[0].JWK, pending.PublicKey) {
		return clockservice.DeviceRecord{}, clockservice.ErrUnavailable
	}
	d, err := a.Store.ActivateMachineEnrollment(ctx, tenant, deviceID, credentialRef)
	return deviceRecord(d), err
}

func sameCoreClient(a, b partnerapp.MachineClient) bool {
	return a.Tenant == b.Tenant && a.ClientID == b.ClientID && a.Owner == b.Owner && a.Status == b.Status && a.Purpose == b.Purpose && a.ExpiresAt.Equal(b.ExpiresAt) && sameStrings(a.Scopes, b.Scopes)
}
func hasCoreKey(keys []partnerapp.MachineClientKey, want partnerapp.MachineClientKey) bool {
	for _, k := range keys {
		if k.ClientID == want.ClientID && k.KID == want.KID && k.Alg == want.Alg && string(k.JWK) == string(want.JWK) && k.NotBefore.Equal(want.NotBefore) {
			return true
		}
	}
	return false
}
func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
func ed25519JWK(key []byte) []byte {
	b, _ := json.Marshal(map[string]string{"kty": "OKP", "crv": "Ed25519", "x": base64.RawURLEncoding.EncodeToString(key)})
	return b
}

func sameJWK(raw []byte, key []byte) bool {
	var got map[string]string
	if json.Unmarshal(raw, &got) != nil {
		return false
	}
	return got["kty"] == "OKP" && got["crv"] == "Ed25519" && got["x"] == base64.RawURLEncoding.EncodeToString(key)
}
