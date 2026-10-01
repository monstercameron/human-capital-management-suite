package clockpartner

import (
	"context"
	"crypto/ed25519"
	"errors"
	"testing"
	"time"

	timev1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/time/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type fakeAdapter struct {
	revoked   bool
	duplicate bool
	first     bool
	tenant    string
}

func (f *fakeAdapter) CreateEnrollmentCode(context.Context, *timev1.CreateEnrollmentCodeRequest) (*timev1.CreateEnrollmentCodeResponse, error) {
	return &timev1.CreateEnrollmentCodeResponse{Code: "enroll"}, nil
}
func (f *fakeAdapter) EnrollDevice(context.Context, *timev1.EnrollDeviceRequest) (*timev1.EnrollDeviceResponse, error) {
	return &timev1.EnrollDeviceResponse{Device: &timev1.ClockDevice{DeviceId: "d1", TenantId: f.tenant, SiteId: "site-1", Revision: 1}, MachineClientCredentialRef: "m1"}, nil
}
func (f *fakeAdapter) RevokeDevice(context.Context, *timev1.RevokeDeviceRequest) (*timev1.RevokeDeviceResponse, error) {
	f.revoked = true
	return &timev1.RevokeDeviceResponse{Device: &timev1.ClockDevice{DeviceId: "d1", State: timev1.ClockDeviceState_CLOCK_DEVICE_STATE_REVOKED}}, nil
}
func (f *fakeAdapter) SyncRoster(context.Context, *timev1.SyncRosterRequest) (*timev1.SyncRosterResponse, error) {
	return &timev1.SyncRosterResponse{Snapshot: &timev1.RosterSnapshot{SnapshotRevision: 2, MaxOfflineAgeSeconds: 3600}}, nil
}
func (f *fakeAdapter) IdentifyWorker(context.Context, *timev1.IdentifyWorkerRequest) (*timev1.IdentifyWorkerResponse, error) {
	return &timev1.IdentifyWorkerResponse{PunchToken: "token", Status: &timev1.WorkerPunchStatus{WorkerId: "worker-1"}}, nil
}
func (f *fakeAdapter) SubmitPunches(_ context.Context, req *timev1.SubmitPunchesRequest) (*timev1.SubmitPunchesResponse, error) {
	seq := req.GetPunches()[0].GetDeviceSequence()
	if seq == 3 {
		return &timev1.SubmitPunchesResponse{Receipts: []*timev1.PunchReceipt{{DeviceSequence: seq, RejectionReason: timev1.PunchRejectionReason_PUNCH_REJECTION_REASON_SEQUENCE_GAP}}, HighestContiguousSequence: 1}, nil
	}
	status := timev1.PunchReceiptStatus_PUNCH_RECEIPT_STATUS_ACCEPTED
	if f.first {
		status = timev1.PunchReceiptStatus_PUNCH_RECEIPT_STATUS_DUPLICATE
	}
	f.first = true
	original := ""
	if status == timev1.PunchReceiptStatus_PUNCH_RECEIPT_STATUS_DUPLICATE {
		original = "r1"
	}
	return &timev1.SubmitPunchesResponse{Receipts: []*timev1.PunchReceipt{{DeviceSequence: seq, Status: status, ReceiptId: "r1", OriginalReceiptId: original}}, HighestContiguousSequence: 1}, nil
}
func (f *fakeAdapter) Heartbeat(context.Context, *timev1.HeartbeatRequest) (*timev1.HeartbeatResponse, error) {
	return &timev1.HeartbeatResponse{ServerTime: timestamppb.Now(), Action: timev1.HeartbeatAction_HEARTBEAT_ACTION_NONE}, nil
}

type webhook struct{ got WebhookEvent }

func (w *webhook) Receive(_ context.Context, e WebhookEvent) error { w.got = e; return nil }

func options(w WebhookReceiver) Options {
	return Options{TenantID: "tenant-a", SiteID: "site-1", ProfileRef: "profile-a", WorkerID: "worker-1", EnrollmentTTL: time.Hour, Now: time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC), Webhook: w}
}

func TestTodo_TCLOCK_018_Conformance(t *testing.T) {
	w := &webhook{}
	result, err := Run(context.Background(), &fakeAdapter{tenant: "tenant-a"}, options(w))
	if err != nil || !result.Passed {
		t.Fatalf("run failed: %v %#v", err, result)
	}
	if w.got.Type != "clock.punch.accepted" || w.got.TenantID != "tenant-a" {
		t.Fatalf("webhook receipt lost scope: %#v", w.got)
	}
	pub, key, _ := ed25519.GenerateKey(nil)
	_ = pub
	report, err := SignPassReport(result, "profile-a", key, options(w).Now)
	if err != nil {
		t.Fatal(err)
	}
	if err := report.Verify(pub); err != nil {
		t.Fatal(err)
	}
}

func TestTodo_TCLOCK_018_Security_TenantIsolation(t *testing.T) {
	result, err := Run(context.Background(), &fakeAdapter{tenant: "tenant-b"}, options(&webhook{}))
	if err == nil || result.Passed || !errors.Is(err, ErrConformance) {
		t.Fatalf("foreign tenant passed: result=%#v err=%v", result, err)
	}
}

func TestTodo_TCLOCK_018_Conformance_FailedRunCannotSign(t *testing.T) {
	result, err := Run(context.Background(), &fakeAdapter{tenant: "tenant-a"}, Options{TenantID: "tenant-a", SiteID: "site-1", ProfileRef: "profile-a", WorkerID: "worker-1", EnrollmentTTL: time.Hour, Now: time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC), Webhook: &webhook{}})
	if err != nil {
		t.Fatal(err)
	}
	result.Passed = false
	_, key, _ := ed25519.GenerateKey(nil)
	if _, err := SignPassReport(result, "profile-a", key, time.Now()); err == nil {
		t.Fatal("failed result was signed")
	}
}

func TestTodo_TCLOCK_018_Security_FabricatedResultAndMalformedReport(t *testing.T) {
	_, key, _ := ed25519.GenerateKey(nil)
	fabricated := Result{Profile: "profile-a", TenantID: "tenant-a", Passed: true}
	if _, err := SignPassReport(fabricated, "profile-a", key, options(&webhook{}).Now); err == nil {
		t.Fatal("fabricated exported result was signed")
	}
	report := PassReport{Version: 1, Profile: "profile-a", TenantID: "tenant-a", Passed: true, ResultDigest: "x"}
	if err := report.Verify(key.Public().(ed25519.PublicKey)); err == nil {
		t.Fatal("malformed report verified")
	}
}

func TestTodo_TCLOCK_018_Security_InvalidOptions(t *testing.T) {
	_, err := Run(context.Background(), &fakeAdapter{tenant: "tenant-a"}, Options{})
	if !errors.Is(err, ErrInvalidOptions) {
		t.Fatalf("want invalid options, got %v", err)
	}
}
