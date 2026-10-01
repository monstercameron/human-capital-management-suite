package timeclockkit

import (
	"context"
	"crypto/ed25519"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	timev1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/time/v1"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type tclockT14Adapter struct {
	tenant string
	first  bool
}

func (a *tclockT14Adapter) CreateEnrollmentCode(context.Context, *timev1.CreateEnrollmentCodeRequest) (*timev1.CreateEnrollmentCodeResponse, error) {
	return &timev1.CreateEnrollmentCodeResponse{Code: "sandbox-code"}, nil
}

func (a *tclockT14Adapter) EnrollDevice(context.Context, *timev1.EnrollDeviceRequest) (*timev1.EnrollDeviceResponse, error) {
	return &timev1.EnrollDeviceResponse{Device: &timev1.ClockDevice{DeviceId: "device-1", TenantId: a.tenant, SiteId: "site-1", Revision: 1}, MachineClientCredentialRef: "machine-ref"}, nil
}

func (a *tclockT14Adapter) RevokeDevice(context.Context, *timev1.RevokeDeviceRequest) (*timev1.RevokeDeviceResponse, error) {
	return &timev1.RevokeDeviceResponse{Device: &timev1.ClockDevice{DeviceId: "device-1", TenantId: a.tenant, State: timev1.ClockDeviceState_CLOCK_DEVICE_STATE_REVOKED}}, nil
}

func (*tclockT14Adapter) SyncRoster(context.Context, *timev1.SyncRosterRequest) (*timev1.SyncRosterResponse, error) {
	return &timev1.SyncRosterResponse{Snapshot: &timev1.RosterSnapshot{SnapshotRevision: 2, MaxOfflineAgeSeconds: 3600}}, nil
}

func (*tclockT14Adapter) IdentifyWorker(context.Context, *timev1.IdentifyWorkerRequest) (*timev1.IdentifyWorkerResponse, error) {
	return &timev1.IdentifyWorkerResponse{PunchToken: "worker-token", Status: &timev1.WorkerPunchStatus{WorkerId: "worker-1"}}, nil
}

func (a *tclockT14Adapter) SubmitPunches(_ context.Context, request *timev1.SubmitPunchesRequest) (*timev1.SubmitPunchesResponse, error) {
	sequence := request.GetPunches()[0].GetDeviceSequence()
	if sequence == 3 {
		return &timev1.SubmitPunchesResponse{Receipts: []*timev1.PunchReceipt{{DeviceSequence: sequence, RejectionReason: timev1.PunchRejectionReason_PUNCH_REJECTION_REASON_SEQUENCE_GAP}}, HighestContiguousSequence: 1}, nil
	}
	status := timev1.PunchReceiptStatus_PUNCH_RECEIPT_STATUS_ACCEPTED
	original := ""
	if a.first {
		status = timev1.PunchReceiptStatus_PUNCH_RECEIPT_STATUS_DUPLICATE
		original = "receipt-1"
	}
	a.first = true
	return &timev1.SubmitPunchesResponse{Receipts: []*timev1.PunchReceipt{{DeviceSequence: sequence, ReceiptId: "receipt-1", OriginalReceiptId: original, Status: status}}, HighestContiguousSequence: 1}, nil
}

func (*tclockT14Adapter) Heartbeat(context.Context, *timev1.HeartbeatRequest) (*timev1.HeartbeatResponse, error) {
	return &timev1.HeartbeatResponse{ServerTime: timestamppb.New(time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)), Action: timev1.HeartbeatAction_HEARTBEAT_ACTION_NONE}, nil
}

type tclockT14Webhook struct{ event WebhookEvent }

func (w *tclockT14Webhook) Receive(_ context.Context, event WebhookEvent) error {
	w.event = event
	return nil
}

type tclockT14GeneratedFake struct{}

func (*tclockT14GeneratedFake) CreateEnrollmentCode(context.Context, *timev1.CreateEnrollmentCodeRequest, ...grpc.CallOption) (*timev1.CreateEnrollmentCodeResponse, error) {
	return &timev1.CreateEnrollmentCodeResponse{Code: "forwarded"}, nil
}
func (*tclockT14GeneratedFake) EnrollDevice(context.Context, *timev1.EnrollDeviceRequest, ...grpc.CallOption) (*timev1.EnrollDeviceResponse, error) {
	return nil, nil
}
func (*tclockT14GeneratedFake) RotateDeviceKey(context.Context, *timev1.RotateDeviceKeyRequest, ...grpc.CallOption) (*timev1.RotateDeviceKeyResponse, error) {
	return nil, nil
}
func (*tclockT14GeneratedFake) RevokeDevice(context.Context, *timev1.RevokeDeviceRequest, ...grpc.CallOption) (*timev1.RevokeDeviceResponse, error) {
	return nil, nil
}
func (*tclockT14GeneratedFake) SyncRoster(context.Context, *timev1.SyncRosterRequest, ...grpc.CallOption) (*timev1.SyncRosterResponse, error) {
	return nil, nil
}
func (*tclockT14GeneratedFake) IdentifyWorker(context.Context, *timev1.IdentifyWorkerRequest, ...grpc.CallOption) (*timev1.IdentifyWorkerResponse, error) {
	return nil, nil
}
func (*tclockT14GeneratedFake) SubmitPunches(context.Context, *timev1.SubmitPunchesRequest, ...grpc.CallOption) (*timev1.SubmitPunchesResponse, error) {
	return nil, nil
}
func (*tclockT14GeneratedFake) Heartbeat(context.Context, *timev1.HeartbeatRequest, ...grpc.CallOption) (*timev1.HeartbeatResponse, error) {
	return nil, nil
}
func (*tclockT14GeneratedFake) GetWorkerStatus(context.Context, *timev1.GetWorkerStatusRequest, ...grpc.CallOption) (*timev1.GetWorkerStatusResponse, error) {
	return nil, nil
}

func tclockT14Options(webhook WebhookReceiver) Options {
	return Options{TenantID: "sandbox-tenant", SiteID: "site-1", ProfileRef: "profile-1", WorkerID: "worker-1", EnrollmentTTL: 10 * time.Minute, Now: time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC), Webhook: webhook}
}

func TestTodo_TCLOCK_018(t *testing.T) {
	webhook := &tclockT14Webhook{}
	result, err := Run(context.Background(), &tclockT14Adapter{tenant: "sandbox-tenant"}, tclockT14Options(webhook))
	if err != nil || !result.Passed {
		t.Fatalf("partner run failed: %v %#v", err, result)
	}
	if webhook.event.TenantID != "sandbox-tenant" || webhook.event.Type != "clock.punch.accepted" || webhook.event.ReceiptID == "" {
		t.Fatalf("webhook receipt was not scoped: %#v", webhook.event)
	}
	_, private, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	report, err := SignPassReport(result, "profile-1", private, tclockT14Options(webhook).Now)
	if err != nil {
		t.Fatal(err)
	}
	if err := report.Verify(private.Public().(ed25519.PublicKey)); err != nil {
		t.Fatalf("signed report did not verify: %v", err)
	}
}

func TestTodo_TCLOCK_018_Conformance(t *testing.T) {
	result, err := Run(context.Background(), &tclockT14Adapter{tenant: "sandbox-tenant"}, tclockT14Options(&tclockT14Webhook{}))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Observations) != len(mandatoryObservations) {
		t.Fatalf("observation count=%d, want %d", len(result.Observations), len(mandatoryObservations))
	}
	for _, name := range mandatoryObservations {
		found := false
		for _, observation := range result.Observations {
			if observation.Name == name && observation.Passed {
				found = true
			}
		}
		if !found {
			t.Errorf("missing passing criterion %q", name)
		}
	}
	if _, err := OpenAPI(); err != nil {
		t.Fatalf("generated contract failed validation: %v", err)
	}
}

func TestTodo_TCLOCK_018_Integration(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != OpenAPIPath {
			http.NotFound(w, r)
			return
		}
		document, documentErr := OpenAPI()
		if documentErr != nil {
			http.Error(w, documentErr.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/yaml")
		_, _ = w.Write(document)
	}))
	t.Cleanup(server.Close)
	document, err := FetchClockOpenAPI(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("served contract was not conformant: %v", err)
	}
	if len(document) == 0 || !strings.Contains(string(document), "ClockDeviceService") {
		t.Fatal("served contract did not contain the device service")
	}
	if _, err := FetchClockOpenAPI(context.Background(), server.URL+OpenAPIPath); err != nil {
		t.Fatalf("explicit contract path failed: %v", err)
	}
}

func TestTodo_TCLOCK_018_Security(t *testing.T) {
	result, err := Run(context.Background(), &tclockT14Adapter{tenant: "production-tenant"}, tclockT14Options(&tclockT14Webhook{}))
	if err == nil || !errors.Is(err, ErrConformance) || result.Passed {
		t.Fatalf("foreign tenant passed certification: result=%#v err=%v", result, err)
	}
	_, private, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := SignPassReport(Result{Profile: "profile-1", TenantID: "sandbox-tenant", Passed: true}, "profile-1", private, time.Now().UTC()); err == nil {
		t.Fatal("fabricated result was signed")
	}
	malformed := PassReport{Version: 1, Profile: "profile-1", TenantID: "sandbox-tenant", Passed: true, ResultDigest: "sha256:" + strings.Repeat("0", 64), SignedAt: time.Now().UTC(), SignerPublicKey: "bad", Signature: "bad"}
	if err := malformed.Verify(private.Public().(ed25519.PublicKey)); err == nil {
		t.Fatal("malformed report verified")
	}
}

func TestTodo_TCLOCK_018_InvalidSetup(t *testing.T) {
	if _, err := Run(context.Background(), &tclockT14Adapter{tenant: "sandbox-tenant"}, Options{}); !errors.Is(err, ErrInvalidOptions) {
		t.Fatalf("want invalid options, got %v", err)
	}
}

func TestTodo_TCLOCK_018_GeneratedClientForwardsCanonicalRequest(t *testing.T) {
	client := GeneratedClient{Client: &tclockT14GeneratedFake{}}
	response, err := client.CreateEnrollmentCode(context.Background(), &timev1.CreateEnrollmentCodeRequest{})
	if err != nil || response.GetCode() != "forwarded" {
		t.Fatalf("generated client did not forward: response=%v err=%v", response, err)
	}
}
