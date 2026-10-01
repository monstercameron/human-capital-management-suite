package cell

import (
	"context"
	"net/http"
	"strings"

	clockservice "github.com/monstercameron/human-capital-management-suite/internal/application/clockservice"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/timeclock"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"google.golang.org/grpc"
)

// registerClockDevice mounts the clock-device service on the canonical local
// gRPC server. The application contract remains the only dependency of the
// composition root; timeclock owns wire conversion at this boundary.
func registerClockDevice(server *grpc.Server, api clockservice.DeviceAPI) {
	if server == nil || api == nil {
		return
	}
	timeclock.Register(server, clockDeviceAdapter{api: api})
}

func clockDeviceHTTP(api clockservice.DeviceAPI) http.Handler {
	canonical := timeclock.New(clockDeviceAdapter{api: api}).HTTPHandler()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/hcmnext.time.v1.ClockDeviceService/") {
			clone := r.Clone(r.Context())
			urlCopy := *r.URL
			urlCopy.Path = "/v1/time/clock-device/" + strings.TrimPrefix(r.URL.Path, "/hcmnext.time.v1.ClockDeviceService/")
			clone.URL = &urlCopy
			canonical.ServeHTTP(w, clone)
			return
		}
		canonical.ServeHTTP(w, r)
	})
}

type clockDeviceAdapter struct{ api clockservice.DeviceAPI }

func (a clockDeviceAdapter) CreateEnrollmentCode(ctx context.Context, p *trust.Principal, req clockservice.EnrollmentRequest) (clockservice.EnrollmentResult, error) {
	return a.api.CreateEnrollmentCode(ctx, p, req)
}
func (a clockDeviceAdapter) EnrollDevice(ctx context.Context, p *trust.Principal, req clockservice.DeviceEnrollmentRequest) (clockservice.DeviceRecord, error) {
	return a.api.EnrollDevice(ctx, p, req)
}
func (a clockDeviceAdapter) RotateDeviceKey(ctx context.Context, p *trust.Principal, req clockservice.DeviceKeyRotationRequest) (clockservice.DeviceRecord, error) {
	return a.api.RotateDeviceKey(ctx, p, req)
}
func (a clockDeviceAdapter) RevokeDevice(ctx context.Context, p *trust.Principal, req clockservice.DeviceLifecycleRequest) (clockservice.DeviceRecord, error) {
	return a.api.RevokeDevice(ctx, p, req)
}
func (a clockDeviceAdapter) SyncRoster(ctx context.Context, p *trust.Principal, deviceID, cursor string) (clockservice.RosterDelta, error) {
	return a.api.SyncRoster(ctx, p, deviceID, cursor)
}
func (a clockDeviceAdapter) IdentifyDevice(ctx context.Context, p *trust.Principal, req timeclock.IdentifyDeviceRequest) (clockservice.IdentifyResult, error) {
	return a.api.IdentifyDevice(ctx, p, clockservice.IdentifyDeviceRequest{DeviceID: req.DeviceID, Method: req.Method, Credential: req.Credential, OverrideReason: req.OverrideReason})
}
func (a clockDeviceAdapter) SubmitPunches(ctx context.Context, p *trust.Principal, req clockservice.BatchRequest) (clockservice.BatchResponse, error) {
	return a.api.SubmitPunches(ctx, p, req)
}
func (a clockDeviceAdapter) Heartbeat(ctx context.Context, p *trust.Principal, req clockservice.HeartbeatRequest) (clockservice.HeartbeatResult, error) {
	return a.api.Heartbeat(ctx, p, req)
}
func (a clockDeviceAdapter) GetWorkerStatus(ctx context.Context, p *trust.Principal, req timeclock.WorkerStatusRequest) (timeclock.WorkerStatusResult, error) {
	got, err := a.api.GetWorkerStatus(ctx, p, clockservice.WorkerStatusRequest{DeviceID: req.DeviceID, PunchToken: req.PunchToken})
	return timeclock.WorkerStatusResult{WorkerID: got.WorkerID, DisplayName: got.DisplayName, SessionStatus: got.SessionStatus, CurrentJobID: got.CurrentJobID, ActiveShiftID: got.ActiveShiftID, AllowedNextEvents: got.AllowedNextEvents}, err
}

var _ timeclock.Service = clockDeviceAdapter{}
