package clockservice

import (
	"context"
	"crypto/sha256"
	"strings"
	"time"

	clockdomain "github.com/monstercameron/human-capital-management-suite/internal/domains/clock"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// IdentifyRequest contains one shared-device identification attempt. The
// device identity is taken from the authenticated machine principal; the
// request cannot select a different tenant or device.
type IdentifyRequest struct {
	DeviceID                string
	WorkerID                string
	Method                  clockdomain.IdentificationMethod
	PIN                     string
	BadgeID                 string
	QRKeyID                 string
	SupervisorCredentialRef string
	SupervisorReason        string
}

// DeviceWorkerTokenClaims is the complete scope of a token issued after a
// successful identification. Issuers must sign it with a configured server
// key; this service supplies no development or zero-value key.
type DeviceWorkerTokenClaims struct {
	TenantID  string
	DeviceID  string
	WorkerID  string
	Method    clockdomain.IdentificationMethod
	IssuedAt  time.Time
	ExpiresAt time.Time
}

// DeviceWorkerToken is the signed credential returned to a kiosk client.
type DeviceWorkerToken struct {
	Value     string
	IssuedAt  time.Time
	ExpiresAt time.Time
}

// DeviceWorkerTokenIssuer is implemented by the configured server-side
// HMAC or Ed25519 signer. Implementations must reject missing key material.
type DeviceWorkerTokenIssuer interface {
	IssueDeviceWorkerToken(context.Context, DeviceWorkerTokenClaims) (DeviceWorkerToken, error)
}

// CredentialLookup resolves a current credential registry row. It must apply
// tenant, worker, kind, value, rotation and revocation checks atomically.
type CredentialLookup interface {
	LookupCredential(context.Context, string, string, string, string) (CredentialRecord, error)
}

// SupervisorCredentialVerifier authenticates the supervisor's own credential
// and returns its verified principal. Opaque client references are not enough.
type SupervisorCredentialVerifier interface {
	VerifySupervisorCredential(context.Context, string, string, string) (*trust.Principal, error)
}

// IdentificationPolicySource resolves the pinned server-side rate-limit
// policy. A kiosk cannot choose its own attempt threshold or lockout window.
type IdentificationPolicySource interface {
	IdentificationPolicy(context.Context, string, string, string) (clockdomain.RateLimitPolicy, error)
}

// Identify authenticates a worker at an enrolled shared device, applies both
// device and worker lockouts, and mints a short-lived scoped device token.
func (s Service) Identify(ctx context.Context, p *trust.Principal, req IdentifyRequest) (IdentifyResult, error) {
	if err := validPrincipal(p); err != nil {
		return IdentifyResult{}, err
	}
	if strings.TrimSpace(req.DeviceID) == "" || strings.TrimSpace(req.WorkerID) == "" {
		return IdentifyResult{}, reject(ErrInvalidRequest, "device_id", "", "device and worker are required")
	}
	if s.Credentials == nil {
		return IdentifyResult{}, ErrUnavailable
	}
	device, err := s.authenticatedRosterDevice(ctx, p, req.DeviceID)
	if err != nil {
		return IdentifyResult{}, err
	}
	if strings.TrimSpace(device.SiteID) == "" || strings.TrimSpace(device.ProfileID) == "" || device.State != "ACTIVE" {
		return IdentifyResult{}, ErrDeviceNotEligible
	}
	if err := s.ensureWorkerOnRoster(ctx, device, req.WorkerID); err != nil {
		return IdentifyResult{}, err
	}
	policySource, ok := s.Credentials.(IdentificationPolicySource)
	if !ok {
		return IdentifyResult{}, ErrUnavailable
	}
	policy, err := policySource.IdentificationPolicy(ctx, string(p.Tenant()), device.SiteID, device.ID)
	if err != nil {
		return IdentifyResult{}, err
	}
	if err := policy.Validate(); err != nil {
		return IdentifyResult{}, reject(ErrInvalidRequest, "rate_limit", "", err.Error())
	}
	if err := s.ensureUnlocked(ctx, string(p.Tenant()), req.DeviceID, req.WorkerID); err != nil {
		return IdentifyResult{}, err
	}

	now := s.now()
	evidence, verified, err := s.identifyCredential(ctx, p, device, req, now)
	if err != nil {
		return IdentifyResult{}, err
	}
	if !verified {
		locked, recordErr := s.recordFailed(ctx, string(p.Tenant()), req.DeviceID, req.WorkerID, policy, now)
		if recordErr != nil {
			return IdentifyResult{}, recordErr
		}
		if locked {
			return IdentifyResult{}, ErrLockedOut
		}
		return IdentifyResult{}, reject(ErrInvalidRequest, "credential", "REJECTED", "credential was not accepted")
	}
	issuer, ok := s.Credentials.(DeviceWorkerTokenIssuer)
	if !ok {
		return IdentifyResult{}, ErrUnavailable
	}
	if err := s.Credentials.ResetDeviceAttempts(ctx, string(p.Tenant()), req.DeviceID); err != nil {
		return IdentifyResult{}, err
	}
	if err := s.Credentials.ResetWorkerAttempts(ctx, string(p.Tenant()), req.WorkerID); err != nil {
		return IdentifyResult{}, err
	}
	token, err := issuer.IssueDeviceWorkerToken(ctx, DeviceWorkerTokenClaims{
		TenantID: string(p.Tenant()), DeviceID: device.ID, WorkerID: evidence.WorkerRef,
		Method: evidence.Method, IssuedAt: now, ExpiresAt: now.Add(5 * time.Minute),
	})
	if err != nil {
		return IdentifyResult{}, err
	}
	if token.Value == "" || !token.IssuedAt.Equal(now) || !token.ExpiresAt.Equal(now.Add(5*time.Minute)) {
		return IdentifyResult{}, reject(ErrUnavailable, "token", "INVALID", "configured signer returned an invalid token")
	}
	return IdentifyResult{Evidence: evidence, Device: device, Token: token}, nil
}

func (s Service) ensureWorkerOnRoster(ctx context.Context, device DeviceRecord, workerID string) error {
	if s.Roster == nil {
		return ErrUnavailable
	}
	cursor := ""
	for page := 0; page < 100; page++ {
		delta, err := s.Roster.Delta(ctx, device.TenantID, device.SiteID, device.ProfileID, cursor, s.now())
		if err != nil {
			return err
		}
		if err := validateRosterDelta(delta); err != nil {
			return err
		}
		for _, worker := range delta.Workers {
			if worker.WorkerRef == workerID && !worker.Terminated {
				return nil
			}
		}
		if !delta.HasMore {
			return ErrWorkerNotOnRoster
		}
		if delta.NextCursor == "" || delta.NextCursor == cursor {
			return ErrInvalidRequest
		}
		cursor = delta.NextCursor
	}
	return ErrInvalidRequest
}

// IdentifyResult is the auditable identification and its scoped credential.
type IdentifyResult struct {
	Evidence clockdomain.IdentificationEvidence
	Device   DeviceRecord
	Token    DeviceWorkerToken
}

func (s Service) ensureUnlocked(ctx context.Context, tenant, deviceID, workerID string) error {
	device, err := s.Credentials.DeviceLockout(ctx, tenant, deviceID)
	if err != nil {
		return err
	}
	worker, err := s.Credentials.WorkerLockout(ctx, tenant, workerID)
	if err != nil {
		return err
	}
	now := s.now()
	if (!device.LockedUntil.IsZero() && now.Before(device.LockedUntil)) ||
		(!worker.LockedUntil.IsZero() && now.Before(worker.LockedUntil)) {
		return ErrLockedOut
	}
	return nil
}

func (s Service) identifyCredential(ctx context.Context, p *trust.Principal, device DeviceRecord, req IdentifyRequest, now time.Time) (clockdomain.IdentificationEvidence, bool, error) {
	tenant := string(p.Tenant())
	switch req.Method {
	case clockdomain.MethodPIN:
		ok, err := s.Credentials.VerifyPIN(ctx, tenant, req.WorkerID, req.PIN)
		if err != nil {
			return clockdomain.IdentificationEvidence{}, false, err
		}
		if !ok {
			return clockdomain.IdentificationEvidence{}, false, nil
		}
		saltDigest := sha256.Sum256([]byte(tenant + "\x00" + device.ID + "\x00" + req.WorkerID))
		pinCredential, hashErr := clockdomain.HashPIN(req.WorkerID, req.PIN, saltDigest[:])
		if hashErr != nil {
			return clockdomain.IdentificationEvidence{}, false, hashErr
		}
		evidence, domainErr := clockdomain.IdentifyWorker(clockdomain.IdentificationRequest{Method: req.Method, DeviceRef: device.ID, WorkerRef: req.WorkerID, Now: now, PINCredential: pinCredential, PINEntered: req.PIN, DeviceOutcome: clockdomain.AttemptAccepted, WorkerOutcome: clockdomain.AttemptAccepted})
		if domainErr != nil {
			return clockdomain.IdentificationEvidence{}, false, domainErr
		}
		return evidence, ok, nil
	case clockdomain.MethodBadge, clockdomain.MethodQR:
		lookup, ok := s.Credentials.(CredentialLookup)
		if !ok {
			return clockdomain.IdentificationEvidence{}, false, ErrUnavailable
		}
		kind, value := string(req.Method), req.BadgeID
		if req.Method == clockdomain.MethodQR {
			value = req.QRKeyID
		}
		rec, err := lookup.LookupCredential(ctx, tenant, req.WorkerID, kind, value)
		if err != nil {
			return clockdomain.IdentificationEvidence{}, false, err
		}
		if rec.WorkerID != req.WorkerID || rec.ExternalID != value || rec.State != "ACTIVE" {
			return clockdomain.IdentificationEvidence{}, false, nil
		}
		evidence, err := clockdomain.IdentifyWorker(clockdomain.IdentificationRequest{Method: req.Method, DeviceRef: device.ID, WorkerRef: req.WorkerID, Now: now, Badge: clockdomain.BadgeCredential{WorkerRef: req.WorkerID, BadgeID: value, IssuedAt: now.Add(-time.Nanosecond)}, QR: clockdomain.QRCredential{WorkerRef: req.WorkerID, KeyID: value, IssuedAt: now.Add(-time.Nanosecond)}, DeviceOutcome: clockdomain.AttemptAccepted, WorkerOutcome: clockdomain.AttemptAccepted})
		return evidence, err == nil, err
	case clockdomain.MethodSupervisorOverride:
		verifier, ok := s.Credentials.(SupervisorCredentialVerifier)
		if !ok || s.Auth == nil {
			return clockdomain.IdentificationEvidence{}, false, ErrUnavailable
		}
		supervisor, err := verifier.VerifySupervisorCredential(ctx, tenant, device.ID, req.SupervisorCredentialRef)
		if err != nil {
			return clockdomain.IdentificationEvidence{}, false, err
		}
		if supervisor == nil || string(supervisor.Tenant()) != tenant || supervisor.SubjectKind() != trust.SubjectKindHuman ||
			supervisor.Subject() == req.WorkerID || !now.After(supervisor.IssuedAt()) || !now.Before(supervisor.ExpiresAt()) {
			return clockdomain.IdentificationEvidence{}, false, ErrSelfApproval
		}
		if err := s.Auth.AuthorizeSupervisorOverride(ctx, supervisor, tenant, device.SiteID); err != nil {
			return clockdomain.IdentificationEvidence{}, false, err
		}
		override := clockdomain.SupervisorOverride{SupervisorRef: supervisor.Subject(), EvidenceRef: req.SupervisorCredentialRef, Reason: req.SupervisorReason, At: now}
		evidence, err := clockdomain.IdentifyWorker(clockdomain.IdentificationRequest{Method: req.Method, DeviceRef: device.ID, WorkerRef: req.WorkerID, Now: now, Override: override, DeviceOutcome: clockdomain.AttemptAccepted, WorkerOutcome: clockdomain.AttemptAccepted})
		if err != nil {
			return clockdomain.IdentificationEvidence{}, false, err
		}
		if _, err := s.Credentials.RecordSupervisorOverride(ctx, tenant, device.ID, req.WorkerID, req.SupervisorCredentialRef, req.SupervisorReason); err != nil {
			return clockdomain.IdentificationEvidence{}, false, err
		}
		return evidence, true, nil
	default:
		return clockdomain.IdentificationEvidence{}, false, reject(ErrInvalidRequest, "method", "", "unsupported shared-device identification method")
	}
}

func (s Service) recordFailed(ctx context.Context, tenant, deviceID, workerID string, policy clockdomain.RateLimitPolicy, now time.Time) (bool, error) {
	lockUntil := now.Add(policy.LockoutDuration)
	deviceCount, err := s.Credentials.RecordFailedDeviceAttempt(ctx, tenant, deviceID, policy.MaxAttempts, lockUntil)
	if err != nil {
		return false, err
	}
	workerCount, err := s.Credentials.RecordFailedWorkerAttempt(ctx, tenant, workerID, policy.MaxAttempts, lockUntil)
	if err != nil {
		return false, err
	}
	return deviceCount >= policy.MaxAttempts || workerCount >= policy.MaxAttempts, nil
}
