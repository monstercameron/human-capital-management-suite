package timeclockkit

import (
	"context"

	timev1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/time/v1"
)

// GeneratedClient adapts the canonical generated gRPC client to Adapter.
// Requests and responses are forwarded unchanged, so the simulator cannot
// accidentally certify a private wire model.
type GeneratedClient struct {
	Client timev1.ClockDeviceServiceClient
}

func (c GeneratedClient) CreateEnrollmentCode(ctx context.Context, in *timev1.CreateEnrollmentCodeRequest) (*timev1.CreateEnrollmentCodeResponse, error) {
	return c.Client.CreateEnrollmentCode(ctx, in)
}
func (c GeneratedClient) EnrollDevice(ctx context.Context, in *timev1.EnrollDeviceRequest) (*timev1.EnrollDeviceResponse, error) {
	return c.Client.EnrollDevice(ctx, in)
}
func (c GeneratedClient) RevokeDevice(ctx context.Context, in *timev1.RevokeDeviceRequest) (*timev1.RevokeDeviceResponse, error) {
	return c.Client.RevokeDevice(ctx, in)
}
func (c GeneratedClient) SyncRoster(ctx context.Context, in *timev1.SyncRosterRequest) (*timev1.SyncRosterResponse, error) {
	return c.Client.SyncRoster(ctx, in)
}
func (c GeneratedClient) IdentifyWorker(ctx context.Context, in *timev1.IdentifyWorkerRequest) (*timev1.IdentifyWorkerResponse, error) {
	return c.Client.IdentifyWorker(ctx, in)
}
func (c GeneratedClient) SubmitPunches(ctx context.Context, in *timev1.SubmitPunchesRequest) (*timev1.SubmitPunchesResponse, error) {
	return c.Client.SubmitPunches(ctx, in)
}
func (c GeneratedClient) Heartbeat(ctx context.Context, in *timev1.HeartbeatRequest) (*timev1.HeartbeatResponse, error) {
	return c.Client.Heartbeat(ctx, in)
}
