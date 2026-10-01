package clockservice

import (
	"context"
	"strings"
	"time"

	clockdomain "github.com/monstercameron/human-capital-management-suite/internal/domains/clock"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// IdentifyDeviceRequest is the transport-neutral credential envelope used by
// a shared clock. The credential is resolved by the application, never by a
// transport adapter.
type IdentifyDeviceRequest struct {
	DeviceID, Method, Credential, OverrideReason string
}

// WorkerStatusRequest asks the application to resolve a previously issued
// worker token for one enrolled device.
type WorkerStatusRequest struct {
	DeviceID, PunchToken string
}

// WorkerStatusResult is the transport-neutral current worker projection.
type WorkerStatusResult struct {
	WorkerID, DisplayName, SessionStatus, CurrentJobID, ActiveShiftID string
	AllowedNextEvents                                                 []string
}

// CredentialResolver resolves an opaque shared-device credential to the
// worker it identifies. Implementations must apply tenant, device, method,
// rotation and revocation checks atomically.
type CredentialResolver interface {
	ResolveDeviceCredential(context.Context, string, string, clockdomain.IdentificationMethod, string) (string, error)
}

// TokenVerifier verifies the signed token issued after shared-device
// identification and returns its complete scope.
type TokenVerifier interface {
	VerifyDeviceWorkerToken(context.Context, string) (DeviceWorkerTokenClaims, error)
}

// PunchContextSource resolves the worker and assignment bound to a token at
// the instant a buffered punch is accepted. Client supplied assignment ids
// are not authoritative.
type PunchContextSource interface {
	ResolvePunchContext(context.Context, string, string, string, time.Time) (workerID, assignmentID string, err error)
}

// WorkerStatusSource resolves the current status after token scope has been
// checked by the facade.
type WorkerStatusSource interface {
	ResolveWorkerStatus(context.Context, DeviceWorkerTokenClaims) (WorkerStatusResult, error)
}

// DeviceAPI is the application-owned contract consumed by the cell
// composition root. Wire transports adapt their protobuf and JSON messages to
// these types; no application package depends on a transport package.
type DeviceAPI interface {
	CreateEnrollmentCode(context.Context, *trust.Principal, EnrollmentRequest) (EnrollmentResult, error)
	EnrollDevice(context.Context, *trust.Principal, DeviceEnrollmentRequest) (DeviceRecord, error)
	RotateDeviceKey(context.Context, *trust.Principal, DeviceKeyRotationRequest) (DeviceRecord, error)
	RevokeDevice(context.Context, *trust.Principal, DeviceLifecycleRequest) (DeviceRecord, error)
	SyncRoster(context.Context, *trust.Principal, string, string) (RosterDelta, error)
	IdentifyDevice(context.Context, *trust.Principal, IdentifyDeviceRequest) (IdentifyResult, error)
	SubmitPunches(context.Context, *trust.Principal, BatchRequest) (BatchResponse, error)
	Heartbeat(context.Context, *trust.Principal, HeartbeatRequest) (HeartbeatResult, error)
	GetWorkerStatus(context.Context, *trust.Principal, WorkerStatusRequest) (WorkerStatusResult, error)
}

// DeviceFacade is the application boundary for gRPC and HTTP clock devices.
// It owns all decisions that must be shared by both transports.
type DeviceFacade struct {
	Service
	Fleet        FleetService
	Credentials  CredentialResolver
	Tokens       TokenVerifier
	PunchContext PunchContextSource
	WorkerStatus WorkerStatusSource
	Clock        func() time.Time
}

var _ DeviceAPI = DeviceFacade{}

func (f DeviceFacade) now() time.Time {
	if f.Clock != nil {
		return f.Clock().UTC()
	}
	if f.Service.Clock != nil {
		return f.Service.Clock().UTC()
	}
	return time.Now().UTC()
}

// IdentifyDevice resolves a PIN, badge or QR credential and mints a scoped
// worker token through the existing clock service.
func (f DeviceFacade) IdentifyDevice(ctx context.Context, p *trust.Principal, req IdentifyDeviceRequest) (IdentifyResult, error) {
	if err := validPrincipal(p); err != nil {
		return IdentifyResult{}, err
	}
	if strings.TrimSpace(req.DeviceID) == "" || strings.TrimSpace(req.Credential) == "" {
		return IdentifyResult{}, reject(ErrInvalidRequest, "credential", "MISSING", "device and credential are required")
	}
	if _, err := f.Service.authenticatedRosterDevice(ctx, p, req.DeviceID); err != nil {
		return IdentifyResult{}, err
	}
	if f.Credentials == nil {
		return IdentifyResult{}, ErrUnavailable
	}
	method, err := identificationMethod(req.Method)
	if err != nil {
		return IdentifyResult{}, err
	}
	worker, err := f.Credentials.ResolveDeviceCredential(ctx, string(p.Tenant()), req.DeviceID, method, req.Credential)
	if err != nil {
		return IdentifyResult{}, err
	}
	if strings.TrimSpace(worker) == "" {
		return IdentifyResult{}, ErrWorkerNotEligible
	}
	identify := IdentifyRequest{DeviceID: req.DeviceID, WorkerID: worker, Method: method, SupervisorReason: req.OverrideReason}
	switch method {
	case clockdomain.MethodPIN:
		identify.PIN = req.Credential
	case clockdomain.MethodBadge:
		identify.BadgeID = req.Credential
	case clockdomain.MethodQR:
		identify.QRKeyID = req.Credential
	case clockdomain.MethodSupervisorOverride:
		identify.SupervisorCredentialRef = req.Credential
	}
	return f.Service.Identify(ctx, p, identify)
}

func identificationMethod(method string) (clockdomain.IdentificationMethod, error) {
	switch strings.ToUpper(strings.TrimSpace(method)) {
	case string(clockdomain.MethodPIN):
		return clockdomain.MethodPIN, nil
	case string(clockdomain.MethodBadge):
		return clockdomain.MethodBadge, nil
	case string(clockdomain.MethodQR):
		return clockdomain.MethodQR, nil
	case string(clockdomain.MethodSupervisorOverride):
		return clockdomain.MethodSupervisorOverride, nil
	default:
		return "", reject(ErrInvalidRequest, "method", "INVALID", "unsupported identification method")
	}
}

// SubmitPunches resolves every opaque worker token and assignment before the
// existing atomic batch service prepares or commits any entry.
func (f DeviceFacade) SubmitPunches(ctx context.Context, p *trust.Principal, req BatchRequest) (BatchResponse, error) {
	if err := validPrincipal(p); err != nil {
		return BatchResponse{}, err
	}
	if _, err := f.Service.authenticatedRosterDevice(ctx, p, req.DeviceID); err != nil {
		return BatchResponse{}, err
	}
	if len(req.Punches) == 0 || len(req.Punches) > maxPunchBatch {
		return BatchResponse{}, reject(ErrInvalidRequest, "punches", "BOUNDS", "batch must contain between one and 200 punches")
	}
	if f.PunchContext == nil {
		return BatchResponse{}, ErrUnavailable
	}
	now := f.now()
	resolved := req
	resolved.Punches = append([]BatchPunch(nil), req.Punches...)
	for i := range resolved.Punches {
		punch := &resolved.Punches[i]
		if lookup, ok := f.Service.Work.(BatchReceiptLookup); ok {
			_, found, err := lookup.LookupPunchReceipt(ctx, string(p.Tenant()), req.DeviceID, punch.DeviceSequence)
			if err != nil {
				return BatchResponse{}, err
			}
			if found {
				continue
			}
		}
		worker, assignment, err := f.PunchContext.ResolvePunchContext(ctx, string(p.Tenant()), req.DeviceID, punch.WorkerCredentialRef, now)
		if err != nil {
			return BatchResponse{}, err
		}
		// Keep the opaque credential reference immutable for idempotency and
		// audit digests. The batch service must carry the resolved worker in its
		// prepared context rather than replacing this wire value.
		punch.resolvedWorkerRef = worker
		punch.resolvedAssignmentRef = assignment
	}
	return f.Service.SubmitPunches(ctx, p, resolved)
}

// Heartbeat records a fleet heartbeat using the server clock as its receipt
// time, regardless of any timestamp supplied by the device.
func (f DeviceFacade) Heartbeat(ctx context.Context, p *trust.Principal, req HeartbeatRequest) (HeartbeatResult, error) {
	req.ObservedAt = f.now()
	return f.Fleet.RecordFleetHeartbeat(ctx, p, req)
}

// GetWorkerStatus verifies token scope before asking the status source for a
// current projection. Tenant and device are always checked against claims.
func (f DeviceFacade) GetWorkerStatus(ctx context.Context, p *trust.Principal, req WorkerStatusRequest) (WorkerStatusResult, error) {
	if err := validPrincipal(p); err != nil {
		return WorkerStatusResult{}, err
	}
	if f.Tokens == nil || f.WorkerStatus == nil || strings.TrimSpace(req.DeviceID) == "" || strings.TrimSpace(req.PunchToken) == "" {
		return WorkerStatusResult{}, ErrUnavailable
	}
	claims, err := f.Tokens.VerifyDeviceWorkerToken(ctx, req.PunchToken)
	if err != nil {
		return WorkerStatusResult{}, err
	}
	if _, err := f.Service.authenticatedRosterDevice(ctx, p, req.DeviceID); err != nil {
		return WorkerStatusResult{}, err
	}
	now := f.now()
	if claims.TenantID != string(p.Tenant()) || claims.DeviceID != req.DeviceID || claims.WorkerID == "" || !now.Before(claims.ExpiresAt) {
		return WorkerStatusResult{}, ErrInvalidPrincipal
	}
	if !validTokenTimes(claims, now, false) {
		return WorkerStatusResult{}, ErrInvalidDeviceWorkerToken
	}
	status, err := f.WorkerStatus.ResolveWorkerStatus(ctx, claims)
	if err != nil {
		return WorkerStatusResult{}, err
	}
	if status.WorkerID != claims.WorkerID || len(status.AllowedNextEvents) > 16 {
		return WorkerStatusResult{}, ErrInvalidDeviceWorkerToken
	}
	for _, event := range status.AllowedNextEvents {
		if !allowedWorkerEvent(event) {
			return WorkerStatusResult{}, ErrInvalidDeviceWorkerToken
		}
	}
	return status, nil
}

func allowedWorkerEvent(event string) bool {
	switch event {
	case "CLOCK_IN", "START_BREAK", "END_BREAK", "TRANSFER_JOB", "CLOCK_OUT":
		return true
	default:
		return false
	}
}
