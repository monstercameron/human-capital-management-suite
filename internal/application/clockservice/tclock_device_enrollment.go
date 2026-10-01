package clockservice

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"fmt"
	"strings"
	"time"

	clock "github.com/monstercameron/human-capital-management-suite/internal/domains/clock"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// MaxEnrollmentTTL bounds the lifetime of an enrollment code. Short-lived
// codes limit the value of a copied QR or MDM configuration.
const MaxEnrollmentTTL = 15 * time.Minute

// EnrollmentRequest asks an administrator to issue one pairing code.
type EnrollmentRequest struct {
	SiteID    string
	ProfileID string
	Timezone  string
	TTL       time.Duration
}

// EnrollmentResult contains the one-time code returned to an administrator.
// Code is never persisted in plaintext by the application service.
type EnrollmentResult struct {
	Code      string
	ExpiresAt time.Time
}

// DeviceEnrollmentRequest is the device proof submitted with a one-time code.
type DeviceEnrollmentRequest struct {
	Code      string
	DeviceID  string
	PublicKey ed25519.PublicKey
	Signature []byte
}

// DeviceKeyRotationRequest requests a possession-proven key rotation.
type DeviceKeyRotationRequest struct {
	DeviceID         string
	ExpectedRevision int64
	PublicKey        ed25519.PublicKey
	Challenge        []byte
	Signature        []byte
	Reason           string
}

// DeviceLifecycleRequest requests one revisioned state transition.
type DeviceLifecycleRequest struct {
	DeviceID         string
	ExpectedRevision int64
	Reason           string
}

// DeviceSiteReassignmentRequest moves a device and binds its new timezone.
type DeviceSiteReassignmentRequest struct {
	DeviceID         string
	ExpectedRevision int64
	SiteID           string
	Timezone         string
	Reason           string
}

// DeviceEnrollmentStore is the stronger TCLOCK-002 persistence seam. The
// older EnrollmentStore cannot verify a code challenge or resolve the signed
// profile registry, so using it alone would permit an arbitrary proof.
type DeviceEnrollmentStore interface {
	LoadEnrollment(ctx context.Context, tenant, code string) (clock.EnrollmentCode, clock.ProfileRegistry, error)
	RedeemEnrollment(ctx context.Context, tenant, code, deviceID string, proof clock.KeyPossessionProof, actorID string, now time.Time) (DeviceRecord, error)
}

// DeviceKeyRotationStore is the possession-bound extension of DeviceStore.
type DeviceKeyRotationStore interface {
	RotateDeviceKeyProof(ctx context.Context, tenant, id string, proof clock.KeyPossessionProof, actorID, reason string, expectedRevision int64) (DeviceRecord, error)
}

// CreateEnrollmentCode authorizes an administrator before creating a code.
func (s Service) CreateEnrollmentCode(ctx context.Context, p *trust.Principal, req EnrollmentRequest) (EnrollmentResult, error) {
	if err := s.validAdminPrincipal(p); err != nil {
		return EnrollmentResult{}, err
	}
	if s.Enrollments == nil || s.Auth == nil || s.IDs == nil {
		return EnrollmentResult{}, ErrUnavailable
	}
	if strings.TrimSpace(req.SiteID) == "" || strings.TrimSpace(req.ProfileID) == "" || strings.TrimSpace(req.Timezone) == "" {
		return EnrollmentResult{}, reject(ErrInvalidRequest, "enrollment", "INVALID", "site, profile and timezone are required")
	}
	if _, err := time.LoadLocation(req.Timezone); err != nil {
		return EnrollmentResult{}, reject(ErrInvalidRequest, "timezone", "INVALID", "timezone is not an IANA location")
	}
	if req.TTL <= 0 || req.TTL > MaxEnrollmentTTL {
		return EnrollmentResult{}, reject(ErrInvalidRequest, "ttl", "INVALID", "enrollment code lifetime must be positive and at most fifteen minutes")
	}
	tenant := tenantOf(p)
	if err := s.Auth.AuthorizeDeviceAdmin(ctx, p, tenant, req.SiteID); err != nil {
		return EnrollmentResult{}, err
	}
	now := s.now()
	code := s.IDs.Random()
	if strings.TrimSpace(code) == "" {
		return EnrollmentResult{}, reject(ErrUnavailable, "enrollment", "UNWIRED", "an enrollment-code generator is required")
	}
	expires := now.Add(req.TTL)
	if err := s.Enrollments.CreateEnrollmentCode(ctx, tenant, code, req.SiteID, req.ProfileID, req.Timezone, p.Subject(), expires); err != nil {
		return EnrollmentResult{}, fmt.Errorf("create enrollment code: %w", err)
	}
	return EnrollmentResult{Code: code, ExpiresAt: expires}, nil
}

// EnrollDevice verifies the server-issued code challenge before consuming it.
// A store without DeviceEnrollmentStore is rejected because the legacy port
// cannot make redemption and proof verification one trusted operation.
func (s Service) EnrollDevice(ctx context.Context, p *trust.Principal, req DeviceEnrollmentRequest) (DeviceRecord, error) {
	if err := s.validAdminPrincipal(p); err != nil {
		return DeviceRecord{}, err
	}
	if strings.TrimSpace(req.Code) == "" || strings.TrimSpace(req.DeviceID) == "" || len(req.PublicKey) != ed25519.PublicKeySize || len(req.Signature) != ed25519.SignatureSize {
		return DeviceRecord{}, reject(ErrInvalidRequest, "enrollment", "INVALID", "code, device id and Ed25519 proof are required")
	}
	store, ok := s.Enrollments.(DeviceEnrollmentStore)
	if !ok {
		return DeviceRecord{}, reject(ErrUnavailable, "enrollment", "UNWIRED", "proof-bound enrollment store is required")
	}
	code, registry, err := store.LoadEnrollment(ctx, tenantFromPrincipal(p), req.Code)
	if err != nil {
		return DeviceRecord{}, fmt.Errorf("load enrollment: %w", err)
	}
	if string(code.Tenant) != tenantOf(p) || code.CodeRef != req.Code {
		return DeviceRecord{}, reject(ErrInvalidRequest, "tenant", "MISMATCH", "enrollment code belongs to another tenant")
	}
	validatedRegistry, err := clock.NewProfileRegistry(registry.Profiles)
	if err != nil || validatedRegistry.Version != registry.Version || validatedRegistry.Digest != registry.Digest {
		return DeviceRecord{}, reject(ErrInvalidRequest, "profile_registry", "INVALID", "profile registry evidence is invalid")
	}
	now := s.now()
	proof := clock.KeyPossessionProof{PublicKey: append(ed25519.PublicKey(nil), req.PublicKey...), Challenge: []byte(req.Code), Signature: append([]byte(nil), req.Signature...)}
	if _, err := clock.Enroll(clock.EnrollmentRequest{Code: code, Proof: proof, DeviceRef: req.DeviceID, Registry: registry, Now: now}); err != nil {
		return DeviceRecord{}, err
	}
	actor := "device-enrollment"
	if p != nil && p.Subject() != "" {
		actor = p.Subject()
	}
	record, err := store.RedeemEnrollment(ctx, tenantOf(p), req.Code, req.DeviceID, proof, actor, now)
	if err != nil {
		return DeviceRecord{}, err
	}
	if record.TenantID != tenantOf(p) || record.ID != req.DeviceID || record.SiteID != string(code.SiteRef) || record.ProfileID != string(code.Profile) || record.Timezone != code.Timezone || record.State != "ACTIVE" || record.Revision < 1 || !equalBytes(record.PublicKey, req.PublicKey) {
		return DeviceRecord{}, reject(ErrUnavailable, "device", "MISMATCH", "enrollment store returned an unbound device record")
	}
	return record, nil
}

// RotationChallenge returns the server-derived key-rotation challenge. A
// device must sign this exact value; a caller cannot substitute an arbitrary
// challenge and thereby turn possession of an unrelated key into rotation.
func RotationChallenge(tenant, deviceID string, expectedRevision int64) []byte {
	input := fmt.Sprintf("tclock-device-rotation/v1\x00%s\x00%s\x00%d", tenant, deviceID, expectedRevision)
	sum := sha256.Sum256([]byte(input))
	return sum[:]
}

// RotateDeviceKey authorizes an administrator and commits a proof-bound key rotation.
func (s Service) RotateDeviceKey(ctx context.Context, p *trust.Principal, req DeviceKeyRotationRequest) (DeviceRecord, error) {
	if err := s.authorizeDeviceMutation(ctx, p, req.DeviceID, req.ExpectedRevision, req.Reason); err != nil {
		return DeviceRecord{}, err
	}
	store, ok := s.Devices.(DeviceKeyRotationStore)
	if !ok {
		return DeviceRecord{}, reject(ErrUnavailable, "device", "UNWIRED", "proof-bound key rotation store is required")
	}
	expectedChallenge := RotationChallenge(tenantOf(p), req.DeviceID, req.ExpectedRevision)
	if len(req.PublicKey) != ed25519.PublicKeySize || len(req.Signature) != ed25519.SignatureSize || !equalBytes(req.Challenge, expectedChallenge) {
		return DeviceRecord{}, reject(ErrInvalidRequest, "proof", "INVALID", "Ed25519 key rotation proof is required")
	}
	proof := clock.KeyPossessionProof{PublicKey: req.PublicKey, Challenge: req.Challenge, Signature: req.Signature}
	if err := proof.Verify(); err != nil {
		return DeviceRecord{}, err
	}
	return store.RotateDeviceKeyProof(ctx, tenantOf(p), req.DeviceID, proof, p.Subject(), req.Reason, req.ExpectedRevision)
}

// SuspendDevice records an audited, revision-checked suspension.
func (s Service) SuspendDevice(ctx context.Context, p *trust.Principal, req DeviceLifecycleRequest) (DeviceRecord, error) {
	if err := s.authorizeDeviceMutation(ctx, p, req.DeviceID, req.ExpectedRevision, req.Reason); err != nil {
		return DeviceRecord{}, err
	}
	return s.Devices.SuspendDevice(ctx, tenantOf(p), req.DeviceID, p.Subject(), req.Reason, req.ExpectedRevision)
}

// ResumeDevice records an audited, revision-checked resumption.
func (s Service) ResumeDevice(ctx context.Context, p *trust.Principal, req DeviceLifecycleRequest) (DeviceRecord, error) {
	if err := s.authorizeDeviceMutation(ctx, p, req.DeviceID, req.ExpectedRevision, req.Reason); err != nil {
		return DeviceRecord{}, err
	}
	return s.Devices.ResumeDevice(ctx, tenantOf(p), req.DeviceID, p.Subject(), req.Reason, req.ExpectedRevision)
}

// RevokeDevice records an audited, revision-checked revocation.
func (s Service) RevokeDevice(ctx context.Context, p *trust.Principal, req DeviceLifecycleRequest) (DeviceRecord, error) {
	if err := s.authorizeDeviceMutation(ctx, p, req.DeviceID, req.ExpectedRevision, req.Reason); err != nil {
		return DeviceRecord{}, err
	}
	return s.Devices.RevokeDevice(ctx, tenantOf(p), req.DeviceID, p.Subject(), req.Reason, req.ExpectedRevision)
}

// ReassignDeviceSite records a revisioned site and timezone change.
func (s Service) ReassignDeviceSite(ctx context.Context, p *trust.Principal, req DeviceSiteReassignmentRequest) (DeviceRecord, error) {
	if err := s.authorizeDeviceMutation(ctx, p, req.DeviceID, req.ExpectedRevision, req.Reason); err != nil {
		return DeviceRecord{}, err
	}
	if strings.TrimSpace(req.SiteID) == "" || strings.TrimSpace(req.Timezone) == "" {
		return DeviceRecord{}, reject(ErrInvalidRequest, "site", "INVALID", "site and timezone are required")
	}
	if _, err := time.LoadLocation(req.Timezone); err != nil {
		return DeviceRecord{}, reject(ErrInvalidRequest, "timezone", "INVALID", "timezone is not an IANA location")
	}
	if err := s.Auth.AuthorizeDeviceAdmin(ctx, p, tenantOf(p), req.SiteID); err != nil {
		return DeviceRecord{}, err
	}
	return s.Devices.ReassignDeviceSite(ctx, tenantOf(p), req.DeviceID, req.SiteID, req.Timezone, p.Subject(), req.Reason, req.ExpectedRevision)
}

func (s Service) authorizeDeviceMutation(ctx context.Context, p *trust.Principal, deviceID string, revision int64, reason string) error {
	if err := s.validAdminPrincipal(p); err != nil {
		return err
	}
	if s.Devices == nil || s.Auth == nil {
		return ErrUnavailable
	}
	if strings.TrimSpace(deviceID) == "" || revision <= 0 || strings.TrimSpace(reason) == "" {
		return reject(ErrInvalidRequest, "device", "INVALID", "device id, positive revision and reason are required")
	}
	device, err := s.Devices.GetDevice(ctx, tenantOf(p), deviceID)
	if err != nil {
		return fmt.Errorf("load device: %w", err)
	}
	if device.TenantID != tenantOf(p) || device.ID != deviceID || device.Revision != revision {
		return reject(ErrInvalidRequest, "device", "REVISION_MISMATCH", "device identity or expected revision does not match")
	}
	if err := s.Auth.AuthorizeDeviceAdmin(ctx, p, tenantOf(p), device.SiteID); err != nil {
		return err
	}
	return nil
}

func (s Service) validAdminPrincipal(p *trust.Principal) error {
	if err := validPrincipal(p); err != nil {
		return err
	}
	now := s.now()
	if now.Before(p.IssuedAt()) || !now.Before(p.ExpiresAt()) {
		return ErrInvalidPrincipal
	}
	return nil
}

func equalBytes(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	var diff byte
	for i := range a {
		diff |= a[i] ^ b[i]
	}
	return diff == 0
}

func tenantFromPrincipal(p *trust.Principal) string {
	if p == nil {
		return ""
	}
	return string(p.Tenant())
}
