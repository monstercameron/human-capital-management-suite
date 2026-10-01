package clockservice

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	clock "github.com/monstercameron/human-capital-management-suite/internal/domains/clock"
)

const (
	maxBootstrapCodeBytes = 256
	maxBootstrapScopes    = 16
	maxBootstrapScopeSize = 128
	maxBootstrapIDSize    = 128
)

// MachineBootstrapRequest is the transport-neutral input for the first
// device call. A device has no trusted principal yet; its identity is derived
// from the enrollment code and the key it proves possession of.
type MachineBootstrapRequest struct {
	Tenant         string
	EnrollmentCode string
	PublicKey      ed25519.PublicKey
	Challenge      []byte
	Signature      []byte
	IdempotencyKey string
}

// MachineBootstrapScopePolicy resolves the scopes authorized for a newly
// enrolled device. The policy is server-owned; callers cannot widen it by
// putting scopes in the enrollment request.
type MachineBootstrapScopePolicy func(context.Context, string, string, string) ([]string, error)

// MachineBootstrapResult is the non-secret result of a successful bootstrap.
// CredentialRef identifies the machine-client registration; it is not a
// bearer credential and no private key or token is returned here.
type MachineBootstrapResult struct {
	Device          DeviceRecord
	DeviceID        string
	MachineClientID string
	CredentialRef   string
}

// MachineClientEnrollment is the atomic command handed to the machine-client
// registry. The registry must consume the code and create the device and
// machine-client rows in one transaction.
type MachineClientEnrollment struct {
	Tenant         string
	EnrollmentCode string
	// EnrollmentCodeHash is the storage-safe binding of the enrollment code.
	// Recovered pending records carry this hash and never carry the raw code.
	EnrollmentCodeHash string
	DeviceID           string
	MachineClientID    string
	Owner              string
	SiteID             string
	ProfileID          string
	Timezone           string
	PublicKey          ed25519.PublicKey
	KeyID              string
	Proof              clock.KeyPossessionProof
	Scopes             []string
	Purpose            string
	ExpiresAt          time.Time
	IdempotencyKey     string
	Now                time.Time
}

// MachineBootstrapPending is the durable handoff between the time-store
// transaction and the machine-client registry. Its outbox record makes a
// registry outage retryable without replaying the enrollment code.
type MachineBootstrapPending struct {
	Enrollment MachineClientEnrollment
	Device     DeviceRecord
}

// MachineClientEnrollmentStore is the narrow saga seam for bootstrap
// persistence. PrepareMachineEnrollment must atomically consume the code,
// write a pending device and append an outbox item. EnsureMachineClient is an
// idempotent registry transaction, and ActivateMachineEnrollment is an
// idempotent state transition. This avoids claiming one transaction across
// the separate time and trust databases.
type MachineClientEnrollmentStore interface {
	LoadEnrollment(ctx context.Context, tenant, code string) (clock.EnrollmentCode, clock.ProfileRegistry, error)
	PrepareMachineEnrollment(ctx context.Context, in MachineClientEnrollment) (MachineBootstrapPending, error)
	EnsureMachineClient(ctx context.Context, in MachineBootstrapPending) (string, error)
	ActivateMachineEnrollment(ctx context.Context, tenant, deviceID, credentialRef string) (DeviceRecord, error)
}

// MachineBootstrapPendingRecovery is implemented by the time-store adapter
// backed by time_machine_enrollment_pending. Found is distinct from an error
// so a consumed code is never retried through LoadEnrollment after a partial
// saga failure.
type MachineBootstrapPendingRecovery interface {
	PendingMachineEnrollmentFor(context.Context, string, string) (MachineBootstrapPending, bool, error)
}

// MachineBootstrapService performs the pre-device half of TCLOCK-002.
type MachineBootstrapService struct {
	Store       MachineClientEnrollmentStore
	ScopePolicy MachineBootstrapScopePolicy
	Clock       func() time.Time
}

func (s MachineBootstrapService) now() (time.Time, error) {
	if s.Clock == nil {
		return time.Time{}, ErrUnavailable
	}
	now := s.Clock().UTC()
	if now.IsZero() {
		return time.Time{}, reject(ErrUnavailable, "clock", "UNWIRED", "a trusted server clock is required")
	}
	return now, nil
}

// EnrollDevice derives the canonical device identity, verifies the signed
// enrollment evidence, and delegates the single-use write to the registry.
func (s MachineBootstrapService) EnrollDevice(ctx context.Context, req MachineBootstrapRequest) (MachineBootstrapResult, error) {
	if s.Store == nil {
		return MachineBootstrapResult{}, ErrUnavailable
	}
	if s.ScopePolicy == nil {
		return MachineBootstrapResult{}, reject(ErrUnavailable, "scopes", "UNWIRED", "a server-owned scope policy is required")
	}
	if err := validateBootstrapRequest(req); err != nil {
		return MachineBootstrapResult{}, err
	}
	now, err := s.now()
	if err != nil {
		return MachineBootstrapResult{}, err
	}
	deviceID := canonicalBootstrapDeviceID(req.EnrollmentCode, req.PublicKey)
	if recovery, ok := s.Store.(MachineBootstrapPendingRecovery); ok {
		pending, found, recoveryErr := recovery.PendingMachineEnrollmentFor(ctx, strings.TrimSpace(req.Tenant), deviceID)
		if recoveryErr != nil {
			return MachineBootstrapResult{}, fmt.Errorf("load pending machine enrollment: %w", recoveryErr)
		}
		if found {
			return s.recoverMachineBootstrap(ctx, req, pending, now)
		}
	}
	code, registry, err := s.Store.LoadEnrollment(ctx, strings.TrimSpace(req.Tenant), strings.TrimSpace(req.EnrollmentCode))
	if err != nil {
		return MachineBootstrapResult{}, fmt.Errorf("load enrollment: %w", err)
	}
	if string(code.Tenant) != strings.TrimSpace(req.Tenant) {
		return MachineBootstrapResult{}, reject(ErrInvalidRequest, "tenant", "MISMATCH", "enrollment code belongs to another tenant")
	}
	validated, err := clock.NewProfileRegistry(registry.Profiles)
	if err != nil || validated.Version != registry.Version || validated.Digest != registry.Digest {
		return MachineBootstrapResult{}, reject(ErrInvalidRequest, "profile_registry", "INVALID", "profile registry evidence is invalid")
	}
	if err := code.Validate(); err != nil {
		return MachineBootstrapResult{}, err
	}
	if err := registry.Accepts(code.Profile, ""); err != nil {
		return MachineBootstrapResult{}, err
	}
	scopes, err := s.ScopePolicy(ctx, strings.TrimSpace(req.Tenant), code.SiteRef, string(code.Profile))
	if err != nil {
		return MachineBootstrapResult{}, fmt.Errorf("resolve machine scopes: %w", err)
	}
	if err := validateBootstrapScopes(scopes); err != nil {
		return MachineBootstrapResult{}, err
	}
	keyID := canonicalBootstrapKeyID(req.PublicKey)
	proof := clock.KeyPossessionProof{
		PublicKey: append(ed25519.PublicKey(nil), req.PublicKey...),
		Challenge: append([]byte(nil), req.Challenge...),
		Signature: append([]byte(nil), req.Signature...),
	}
	if err := proof.Verify(); err != nil {
		return MachineBootstrapResult{}, err
	}
	if !equalBytes(req.Challenge, []byte(req.EnrollmentCode)) {
		return MachineBootstrapResult{}, reject(ErrInvalidRequest, "challenge", "MISMATCH", "proof challenge must be the enrollment code")
	}
	identity, err := clock.Enroll(clock.EnrollmentRequest{Code: code, Proof: proof, DeviceRef: deviceID, Registry: registry, Now: now})
	if err != nil {
		return MachineBootstrapResult{}, err
	}
	commit := MachineClientEnrollment{
		Tenant: strings.TrimSpace(req.Tenant), EnrollmentCode: strings.TrimSpace(req.EnrollmentCode),
		DeviceID: identity.DeviceRef, MachineClientID: identity.DeviceRef,
		Owner: "clock-device:" + identity.DeviceRef, SiteID: string(code.SiteRef),
		ProfileID: string(code.Profile), Timezone: code.Timezone,
		PublicKey: append(ed25519.PublicKey(nil), req.PublicKey...), KeyID: keyID,
		Proof: proof, Scopes: append([]string(nil), scopes...),
		Purpose: "time.clock.device", ExpiresAt: code.ExpiresAt.UTC(),
		IdempotencyKey: strings.TrimSpace(req.IdempotencyKey), Now: now,
		EnrollmentCodeHash: hashBootstrapEnrollmentCode(strings.TrimSpace(req.EnrollmentCode)),
	}
	pending, err := s.Store.PrepareMachineEnrollment(ctx, commit)
	if err != nil {
		return MachineBootstrapResult{}, fmt.Errorf("prepare machine enrollment: %w", err)
	}
	if pending.Enrollment.DeviceID != identity.DeviceRef || pending.Enrollment.MachineClientID != identity.DeviceRef {
		return MachineBootstrapResult{}, reject(ErrUnavailable, "device", "MISMATCH", "pending machine enrollment is not bound to the canonical device")
	}
	credentialRef, err := s.Store.EnsureMachineClient(ctx, pending)
	if err != nil {
		return MachineBootstrapResult{}, fmt.Errorf("ensure machine client: %w", err)
	}
	if err := validateCredentialRef(credentialRef); err != nil {
		return MachineBootstrapResult{}, reject(ErrUnavailable, "credential_ref", "UNWIRED", "machine-client registry returned no bounded credential reference")
	}
	record, err := s.Store.ActivateMachineEnrollment(ctx, strings.TrimSpace(req.Tenant), identity.DeviceRef, credentialRef)
	if err != nil {
		return MachineBootstrapResult{}, fmt.Errorf("activate machine enrollment: %w", err)
	}
	if record.TenantID != strings.TrimSpace(req.Tenant) || record.ID != identity.DeviceRef || record.SiteID != code.SiteRef || record.ProfileID != string(code.Profile) || record.Timezone != code.Timezone || record.State != "ACTIVE" || record.Revision <= 0 || !equalBytes(record.PublicKey, req.PublicKey) {
		return MachineBootstrapResult{}, reject(ErrUnavailable, "device", "MISMATCH", "machine-client transaction returned an unbound device record")
	}
	return MachineBootstrapResult{Device: record, DeviceID: identity.DeviceRef, MachineClientID: identity.DeviceRef, CredentialRef: credentialRef}, nil
}

func (s MachineBootstrapService) recoverMachineBootstrap(ctx context.Context, req MachineBootstrapRequest, pending MachineBootstrapPending, now time.Time) (MachineBootstrapResult, error) {
	e := pending.Enrollment
	// Validate the persisted proof before comparing it with the retry. A
	// tampered stored proof is a cryptographic rejection, while a valid proof
	// that belongs to a different request remains an invalid retry.
	if err := e.Proof.Verify(); err != nil {
		return MachineBootstrapResult{}, err
	}
	if e.Tenant != strings.TrimSpace(req.Tenant) || (e.EnrollmentCodeHash != "" && e.EnrollmentCodeHash != hashBootstrapEnrollmentCode(strings.TrimSpace(req.EnrollmentCode))) || e.DeviceID != canonicalBootstrapDeviceID(req.EnrollmentCode, req.PublicKey) || e.MachineClientID != e.DeviceID || !equalBytes(e.PublicKey, req.PublicKey) || !equalBytes(e.Proof.PublicKey, e.PublicKey) || e.IdempotencyKey != strings.TrimSpace(req.IdempotencyKey) || !equalBytes(e.Proof.Challenge, req.Challenge) || !equalBytes(e.Proof.Signature, req.Signature) || !equalBytes(req.Challenge, []byte(req.EnrollmentCode)) {
		return MachineBootstrapResult{}, reject(ErrInvalidRequest, "enrollment", "MISMATCH", "pending enrollment does not match the retry")
	}
	if e.ExpiresAt.IsZero() || !now.Before(e.ExpiresAt) {
		return MachineBootstrapResult{}, clock.ErrEnrollmentRejected
	}
	scopes, err := s.ScopePolicy(ctx, e.Tenant, e.SiteID, e.ProfileID)
	if err != nil {
		return MachineBootstrapResult{}, fmt.Errorf("resolve recovery scopes: %w", err)
	}
	if err := validateBootstrapScopes(scopes); err != nil || !sameStrings(scopes, e.Scopes) {
		return MachineBootstrapResult{}, reject(ErrInvalidRequest, "scopes", "MISMATCH", "server scope policy changed since enrollment")
	}
	if pending.Device.ID != e.DeviceID || pending.Device.TenantID != e.Tenant || pending.Device.SiteID != e.SiteID || pending.Device.ProfileID != e.ProfileID || pending.Device.Timezone != e.Timezone || pending.Device.State != "PENDING" || pending.Device.Revision < 1 || !equalBytes(pending.Device.PublicKey, e.PublicKey) {
		return MachineBootstrapResult{}, reject(ErrInvalidRequest, "device", "MISMATCH", "pending device is not tenant bound")
	}
	if now.IsZero() {
		return MachineBootstrapResult{}, ErrUnavailable
	}
	credentialRef, err := s.Store.EnsureMachineClient(ctx, pending)
	if err != nil {
		return MachineBootstrapResult{}, fmt.Errorf("ensure recovered machine client: %w", err)
	}
	if err := validateCredentialRef(credentialRef); err != nil {
		return MachineBootstrapResult{}, reject(ErrUnavailable, "credential_ref", "UNWIRED", "machine-client registry returned no bounded credential reference")
	}
	record, err := s.Store.ActivateMachineEnrollment(ctx, e.Tenant, e.DeviceID, credentialRef)
	if err != nil {
		return MachineBootstrapResult{}, fmt.Errorf("activate recovered machine enrollment: %w", err)
	}
	if record.TenantID != e.Tenant || record.ID != e.DeviceID || record.SiteID != e.SiteID || record.ProfileID != e.ProfileID || record.Timezone != e.Timezone || record.State != "ACTIVE" || record.Revision <= 0 || !equalBytes(record.PublicKey, e.PublicKey) {
		return MachineBootstrapResult{}, reject(ErrUnavailable, "device", "MISMATCH", "recovered device record is not bound")
	}
	return MachineBootstrapResult{Device: record, DeviceID: e.DeviceID, MachineClientID: e.MachineClientID, CredentialRef: credentialRef}, nil
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

func validateBootstrapRequest(req MachineBootstrapRequest) error {
	if strings.TrimSpace(req.Tenant) == "" || strings.TrimSpace(req.EnrollmentCode) == "" || len(req.EnrollmentCode) > maxBootstrapCodeBytes {
		return reject(ErrInvalidRequest, "enrollment", "INVALID", "tenant and a bounded enrollment code are required")
	}
	if len(req.PublicKey) != ed25519.PublicKeySize || len(req.Signature) != ed25519.SignatureSize {
		return reject(ErrInvalidRequest, "proof", "INVALID", "Ed25519 public key and signature are required")
	}
	if len(req.Challenge) == 0 || len(req.Challenge) > maxBootstrapCodeBytes {
		return reject(ErrInvalidRequest, "challenge", "INVALID", "a bounded proof challenge is required")
	}
	if strings.TrimSpace(req.IdempotencyKey) == "" {
		return reject(ErrInvalidRequest, "idempotency_key", "INVALID", "idempotency key is required")
	}
	return nil
}

func validateBootstrapScopes(scopes []string) error {
	if len(scopes) == 0 || len(scopes) > maxBootstrapScopes {
		return reject(ErrInvalidRequest, "scopes", "INVALID", "explicit machine-client scopes are required")
	}
	seen := make(map[string]struct{}, len(scopes))
	for _, scope := range scopes {
		scope = strings.TrimSpace(scope)
		if scope == "" || len(scope) > maxBootstrapScopeSize || scope == "*" {
			return reject(ErrInvalidRequest, "scopes", "INVALID", "machine-client scopes must be explicit and bounded")
		}
		if _, ok := seen[scope]; ok {
			return reject(ErrInvalidRequest, "scopes", "INVALID", "machine-client scopes must be unique")
		}
		seen[scope] = struct{}{}
	}
	return nil
}

func validateCredentialRef(ref string) error {
	ref = strings.TrimSpace(ref)
	if ref == "" || len(ref) > 256 {
		return ErrInvalidRequest
	}
	for _, r := range ref {
		if r < 0x20 || r == 0x7f {
			return ErrInvalidRequest
		}
	}
	return nil
}

func canonicalBootstrapDeviceID(code string, pub ed25519.PublicKey) string {
	h := sha256.New()
	h.Write([]byte("hcmnext/tclock-device/v1\x00"))
	h.Write(pub)
	h.Write([]byte("\x00"))
	h.Write([]byte(code))
	return "clock_" + hex.EncodeToString(h.Sum(nil))
}

func hashBootstrapEnrollmentCode(code string) string {
	sum := sha256.Sum256([]byte(code))
	return hex.EncodeToString(sum[:])
}

func canonicalBootstrapKeyID(pub ed25519.PublicKey) string {
	sum := sha256.Sum256(pub)
	return "key_" + hex.EncodeToString(sum[:])
}
