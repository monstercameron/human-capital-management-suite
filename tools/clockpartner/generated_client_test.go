package clockpartner

import (
	"context"
	"testing"

	timev1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/time/v1"
	"google.golang.org/grpc"
)

type generatedFake struct{ called bool }

func (f *generatedFake) CreateEnrollmentCode(context.Context, *timev1.CreateEnrollmentCodeRequest, ...grpc.CallOption) (*timev1.CreateEnrollmentCodeResponse, error) {
	f.called = true
	return &timev1.CreateEnrollmentCodeResponse{}, nil
}
func (*generatedFake) EnrollDevice(context.Context, *timev1.EnrollDeviceRequest, ...grpc.CallOption) (*timev1.EnrollDeviceResponse, error) {
	return nil, nil
}
func (*generatedFake) RotateDeviceKey(context.Context, *timev1.RotateDeviceKeyRequest, ...grpc.CallOption) (*timev1.RotateDeviceKeyResponse, error) {
	return nil, nil
}
func (*generatedFake) RevokeDevice(context.Context, *timev1.RevokeDeviceRequest, ...grpc.CallOption) (*timev1.RevokeDeviceResponse, error) {
	return nil, nil
}
func (*generatedFake) SyncRoster(context.Context, *timev1.SyncRosterRequest, ...grpc.CallOption) (*timev1.SyncRosterResponse, error) {
	return nil, nil
}
func (*generatedFake) IdentifyWorker(context.Context, *timev1.IdentifyWorkerRequest, ...grpc.CallOption) (*timev1.IdentifyWorkerResponse, error) {
	return nil, nil
}
func (*generatedFake) SubmitPunches(context.Context, *timev1.SubmitPunchesRequest, ...grpc.CallOption) (*timev1.SubmitPunchesResponse, error) {
	return nil, nil
}
func (*generatedFake) Heartbeat(context.Context, *timev1.HeartbeatRequest, ...grpc.CallOption) (*timev1.HeartbeatResponse, error) {
	return nil, nil
}
func (*generatedFake) GetWorkerStatus(context.Context, *timev1.GetWorkerStatusRequest, ...grpc.CallOption) (*timev1.GetWorkerStatusResponse, error) {
	return nil, nil
}

func TestGeneratedClient_ForwardsCanonicalMethod(t *testing.T) {
	f := &generatedFake{}
	_, err := GeneratedClient{Client: f}.CreateEnrollmentCode(context.Background(), &timev1.CreateEnrollmentCodeRequest{})
	if err != nil || !f.called {
		t.Fatalf("generated client did not forward call: err=%v called=%v", err, f.called)
	}
}
