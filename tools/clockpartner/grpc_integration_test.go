package clockpartner

import (
	"context"
	"net"
	"testing"
	"time"

	timev1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/time/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type loopbackService struct {
	timev1.UnimplementedClockDeviceServiceServer
	first bool
}

func (*loopbackService) CreateEnrollmentCode(context.Context, *timev1.CreateEnrollmentCodeRequest) (*timev1.CreateEnrollmentCodeResponse, error) {
	return &timev1.CreateEnrollmentCodeResponse{Code: "enroll"}, nil
}
func (*loopbackService) EnrollDevice(context.Context, *timev1.EnrollDeviceRequest) (*timev1.EnrollDeviceResponse, error) {
	return &timev1.EnrollDeviceResponse{Device: &timev1.ClockDevice{DeviceId: "loop-device", TenantId: "tenant-a", SiteId: "site-1", Revision: 1}, MachineClientCredentialRef: "machine"}, nil
}
func (*loopbackService) RevokeDevice(context.Context, *timev1.RevokeDeviceRequest) (*timev1.RevokeDeviceResponse, error) {
	return &timev1.RevokeDeviceResponse{Device: &timev1.ClockDevice{DeviceId: "loop-device", State: timev1.ClockDeviceState_CLOCK_DEVICE_STATE_REVOKED}}, nil
}
func (*loopbackService) SyncRoster(context.Context, *timev1.SyncRosterRequest) (*timev1.SyncRosterResponse, error) {
	return &timev1.SyncRosterResponse{Snapshot: &timev1.RosterSnapshot{SnapshotRevision: 1, MaxOfflineAgeSeconds: 3600}}, nil
}
func (*loopbackService) IdentifyWorker(context.Context, *timev1.IdentifyWorkerRequest) (*timev1.IdentifyWorkerResponse, error) {
	return &timev1.IdentifyWorkerResponse{PunchToken: "loop-token", Status: &timev1.WorkerPunchStatus{WorkerId: "worker-1"}}, nil
}
func (s *loopbackService) SubmitPunches(_ context.Context, req *timev1.SubmitPunchesRequest) (*timev1.SubmitPunchesResponse, error) {
	seq := req.GetPunches()[0].GetDeviceSequence()
	if seq == 3 {
		return &timev1.SubmitPunchesResponse{Receipts: []*timev1.PunchReceipt{{DeviceSequence: 3, RejectionReason: timev1.PunchRejectionReason_PUNCH_REJECTION_REASON_SEQUENCE_GAP}}, HighestContiguousSequence: 1}, nil
	}
	status := timev1.PunchReceiptStatus_PUNCH_RECEIPT_STATUS_ACCEPTED
	original := ""
	if s.first {
		status = timev1.PunchReceiptStatus_PUNCH_RECEIPT_STATUS_DUPLICATE
		original = "loop-receipt"
	}
	s.first = true
	return &timev1.SubmitPunchesResponse{Receipts: []*timev1.PunchReceipt{{DeviceSequence: seq, Status: status, ReceiptId: "loop-receipt", OriginalReceiptId: original}}, HighestContiguousSequence: 1}, nil
}
func (*loopbackService) Heartbeat(context.Context, *timev1.HeartbeatRequest) (*timev1.HeartbeatResponse, error) {
	return &timev1.HeartbeatResponse{ServerTime: timestamppb.New(testInstant()), Action: timev1.HeartbeatAction_HEARTBEAT_ACTION_NONE}, nil
}
func (*loopbackService) GetWorkerStatus(context.Context, *timev1.GetWorkerStatusRequest) (*timev1.GetWorkerStatusResponse, error) {
	return &timev1.GetWorkerStatusResponse{}, nil
}
func (*loopbackService) RotateDeviceKey(context.Context, *timev1.RotateDeviceKeyRequest) (*timev1.RotateDeviceKeyResponse, error) {
	return &timev1.RotateDeviceKeyResponse{}, nil
}
func testInstant() time.Time { return time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC) }

func TestTodo_TCLOCK_018_Integration_GeneratedGRPCLoopback(t *testing.T) {
	service := &loopbackService{}
	server := grpc.NewServer()
	timev1.RegisterClockDeviceServiceServer(server, service)
	listener := bufconn.Listen(1 << 20)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	conn, err := grpc.DialContext(context.Background(), "bufnet", grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	client := GeneratedClient{Client: timev1.NewClockDeviceServiceClient(conn)}
	result, err := Run(context.Background(), client, options(&webhook{}))
	if err != nil || !result.Passed {
		t.Fatalf("generated loopback conformance failed: %v %#v", err, result)
	}
}
