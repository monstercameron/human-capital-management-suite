package timeclockapp

import (
	"context"
	"errors"
	"fmt"

	timev1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/time/v1"
)

// ServicePath is the protojson route prefix of the public device API
// (POST {base}/hcmnext.time.v1.ClockDeviceService/{Method}).
const ServicePath = "/hcmnext.time.v1.ClockDeviceService/"

// The device-facing methods this kiosk calls. They are the complete list:
// the client has no other route, and kioskserver proxies nothing else.
const (
	MethodEnrollDevice   = "EnrollDevice"
	MethodIdentifyWorker = "IdentifyWorker"
	MethodSubmitPunches  = "SubmitPunches"
	MethodHeartbeat      = "Heartbeat"
	MethodSyncRoster     = "SyncRoster"
	MethodWorkerStatus   = "GetWorkerStatus"
)

// DeviceMethods returns the allow-list of ClockDeviceService methods the
// kiosk uses. Admin methods (CreateEnrollmentCode, RotateDeviceKey,
// RevokeDevice) are deliberately absent.
func DeviceMethods() []string {
	return []string{MethodEnrollDevice, MethodIdentifyWorker, MethodSubmitPunches, MethodHeartbeat, MethodSyncRoster, MethodWorkerStatus}
}

// IsDeviceMethod reports whether method is on the allow-list.
func IsDeviceMethod(method string) bool {
	for _, m := range DeviceMethods() {
		if m == method {
			return true
		}
	}
	return false
}

// DeviceAPI is the slice of ClockDeviceService the kiosk depends on. The
// view model talks only to this interface, so tests drive it with a fake
// and the browser drives it with HTTPClient.
type DeviceAPI interface {
	EnrollDevice(context.Context, *timev1.EnrollDeviceRequest) (*timev1.EnrollDeviceResponse, error)
	IdentifyWorker(context.Context, *timev1.IdentifyWorkerRequest) (*timev1.IdentifyWorkerResponse, error)
	SubmitPunches(context.Context, *timev1.SubmitPunchesRequest) (*timev1.SubmitPunchesResponse, error)
	Heartbeat(context.Context, *timev1.HeartbeatRequest) (*timev1.HeartbeatResponse, error)
}

// RosterAPI is the optional roster synchronization capability. Keeping it
// separate preserves compatibility with existing device fakes.
type RosterAPI interface {
	SyncRoster(context.Context, *timev1.SyncRosterRequest) (*timev1.SyncRosterResponse, error)
}

// WorkerStatusAPI is the optional status refresh capability.
type WorkerStatusAPI interface {
	GetWorkerStatus(context.Context, *timev1.GetWorkerStatusRequest) (*timev1.GetWorkerStatusResponse, error)
}

// ErrUnreachable marks a call that never got an answer from the HCM server:
// a network failure, or the kiosk server reporting its upstream down. The
// view model treats it as "offline" (queue and retry), never as a refusal.
var ErrUnreachable = errors.New("timeclockapp: device API unreachable")

// APIError is a refusal the server did answer with. Code is the connect
// error code ("not_found", "permission_denied", ...).
type APIError struct {
	Status  int
	Code    string
	Message string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("timeclockapp: device API refused (%d %s): %s", e.Status, e.Code, e.Message)
}
