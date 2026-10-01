package timeclock

import (
	"context"

	clockservice "github.com/monstercameron/human-capital-management-suite/internal/application/clockservice"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// IdentifyDeviceRequest is the transport-independent credential envelope for
// worker identification. Worker identity is resolved by the application port.
type IdentifyDeviceRequest struct {
	DeviceID, Method, Credential, OverrideReason string
}

// WorkerStatusRequest asks the application facade to resolve a previously
// issued worker token. The transport never decodes or trusts the token.
type WorkerStatusRequest struct{ DeviceID, PunchToken string }

// WorkerStatusResult is the wire-neutral worker status projection.
type WorkerStatusResult struct {
	WorkerID, DisplayName, SessionStatus, CurrentJobID, ActiveShiftID string
	AllowedNextEvents                                                 []string
}

// Service is the application port consumed by both HTTP and gRPC. Implementors
// own authorization, tenant scoping, credential resolution, and persistence.
type Service interface {
	CreateEnrollmentCode(context.Context, *trust.Principal, clockservice.EnrollmentRequest) (clockservice.EnrollmentResult, error)
	EnrollDevice(context.Context, *trust.Principal, clockservice.DeviceEnrollmentRequest) (clockservice.DeviceRecord, error)
	RotateDeviceKey(context.Context, *trust.Principal, clockservice.DeviceKeyRotationRequest) (clockservice.DeviceRecord, error)
	RevokeDevice(context.Context, *trust.Principal, clockservice.DeviceLifecycleRequest) (clockservice.DeviceRecord, error)
	SyncRoster(context.Context, *trust.Principal, string, string) (clockservice.RosterDelta, error)
	IdentifyDevice(context.Context, *trust.Principal, IdentifyDeviceRequest) (clockservice.IdentifyResult, error)
	SubmitPunches(context.Context, *trust.Principal, clockservice.BatchRequest) (clockservice.BatchResponse, error)
	Heartbeat(context.Context, *trust.Principal, clockservice.HeartbeatRequest) (clockservice.HeartbeatResult, error)
	GetWorkerStatus(context.Context, *trust.Principal, WorkerStatusRequest) (WorkerStatusResult, error)
}
