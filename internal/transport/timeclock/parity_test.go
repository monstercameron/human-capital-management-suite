package timeclock

import (
	"context"
	"net"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	timev1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/time/v1"
	clockservice "github.com/monstercameron/human-capital-management-suite/internal/application/clockservice"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type parityApp struct{ calls int }

func (a *parityApp) CreateEnrollmentCode(context.Context, *trust.Principal, clockservice.EnrollmentRequest) (clockservice.EnrollmentResult, error) {
	return clockservice.EnrollmentResult{}, nil
}
func (a *parityApp) EnrollDevice(context.Context, *trust.Principal, clockservice.DeviceEnrollmentRequest) (clockservice.DeviceRecord, error) {
	return clockservice.DeviceRecord{}, nil
}
func (a *parityApp) RotateDeviceKey(context.Context, *trust.Principal, clockservice.DeviceKeyRotationRequest) (clockservice.DeviceRecord, error) {
	return clockservice.DeviceRecord{}, nil
}
func (a *parityApp) RevokeDevice(context.Context, *trust.Principal, clockservice.DeviceLifecycleRequest) (clockservice.DeviceRecord, error) {
	return clockservice.DeviceRecord{}, nil
}
func (a *parityApp) SyncRoster(context.Context, *trust.Principal, string, string) (clockservice.RosterDelta, error) {
	return clockservice.RosterDelta{}, nil
}
func (a *parityApp) IdentifyDevice(context.Context, *trust.Principal, IdentifyDeviceRequest) (clockservice.IdentifyResult, error) {
	return clockservice.IdentifyResult{}, nil
}
func (a *parityApp) SubmitPunches(_ context.Context, _ *trust.Principal, req clockservice.BatchRequest) (clockservice.BatchResponse, error) {
	a.calls++
	return clockservice.BatchResponse{HighestContiguous: 1, Receipts: []clockservice.BatchReceipt{{DeviceSequence: req.Punches[0].DeviceSequence, Status: clockservice.BatchAccepted, ObservationID: "receipt-1"}}}, nil
}
func (a *parityApp) Heartbeat(context.Context, *trust.Principal, clockservice.HeartbeatRequest) (clockservice.HeartbeatResult, error) {
	return clockservice.HeartbeatResult{}, nil
}
func (a *parityApp) GetWorkerStatus(context.Context, *trust.Principal, WorkerStatusRequest) (WorkerStatusResult, error) {
	return WorkerStatusResult{}, nil
}

func TestSubmitPunchesGRPCAndHTTPUseSamePort(t *testing.T) {
	app := &parityApp{}
	p := testPrincipal(t)
	srv := grpc.NewServer(grpc.UnaryInterceptor(func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		return handler(trust.WithPrincipal(ctx, p), req)
	}))
	Register(srv, app)
	lis := bufconn.Listen(1 << 20)
	go func() { _ = srv.Serve(lis) }()
	defer srv.Stop()
	ctx := context.Background()
	conn, err := grpc.NewClient("passthrough:///buf", grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return lis.Dial() }), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	client := timev1.NewClockDeviceServiceClient(conn)
	in := &timev1.SubmitPunchesRequest{DeviceId: "dev-1", Punches: []*timev1.DevicePunch{{DeviceSequence: 1, EventType: timev1.PunchEventType_PUNCH_EVENT_TYPE_CLOCK_IN, Worker: &timev1.WorkerCredentialRef{Ref: &timev1.WorkerCredentialRef_PunchToken{PunchToken: "token"}}, DeviceOccurredAt: timestamppb.New(time.Unix(1, 0))}}}
	got, err := client.SubmitPunches(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if got.GetHighestContiguousSequence() != 1 {
		t.Fatalf("grpc response=%v", got)
	}
	h := httptest.NewRequest("POST", "/v1/time/clock-device/SubmitPunches", strings.NewReader(string(mustJSON(t, in))))
	h = h.WithContext(trust.WithPrincipal(ctx, p))
	w := httptest.NewRecorder()
	New(app).HTTPHandler().ServeHTTP(w, h)
	if w.Code != 200 {
		t.Fatalf("http status=%d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "highest_contiguous_sequence") || app.calls != 2 {
		t.Fatalf("http=%s calls=%d", w.Body.String(), app.calls)
	}
}

func TestServerMapsCanonicalOperations(t *testing.T) {
	a := &parityApp{}
	s := New(a)
	p := testPrincipal(t)
	ctx := trust.WithPrincipal(context.Background(), p)
	if _, err := s.CreateEnrollmentCode(ctx, &timev1.CreateEnrollmentCodeRequest{SiteId: "site", ProfileRef: "profile", Timezone: "UTC", TtlSeconds: 60}); err != nil {
		t.Fatal(err)
	}
	key, sig := make([]byte, 32), make([]byte, 64)
	if _, err := s.EnrollDevice(ctx, &timev1.EnrollDeviceRequest{EnrollmentCode: "code", PublicKey: key, ChallengeSignature: sig}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RotateDeviceKey(ctx, &timev1.RotateDeviceKeyRequest{DeviceId: "dev", ExpectedRevision: 1, NewPublicKey: key, ChallengeSignature: sig}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RevokeDevice(ctx, &timev1.RevokeDeviceRequest{DeviceId: "dev", ExpectedRevision: 1, Reason: "retire"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SyncRoster(ctx, &timev1.SyncRosterRequest{DeviceId: "dev", MaxResults: 10}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.IdentifyWorker(ctx, &timev1.IdentifyWorkerRequest{DeviceId: "dev", Method: timev1.IdentificationMethod_IDENTIFICATION_METHOD_PIN, Credential: &timev1.IdentifyWorkerRequest_Pin{Pin: "1234"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Heartbeat(ctx, &timev1.HeartbeatRequest{DeviceId: "dev"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetWorkerStatus(ctx, &timev1.GetWorkerStatusRequest{DeviceId: "dev", PunchToken: "token"}); err != nil {
		t.Fatal(err)
	}
}

func TestHTTPBoundaryRejectsMalformedRequests(t *testing.T) {
	h := New(&parityApp{}).HTTPHandler()
	for _, tc := range []struct {
		name, method, path, body string
		status                   int
	}{
		{"method", "GET", "/v1/time/clock-device/Heartbeat", "{}", 405},
		{"unknown", "POST", "/v1/time/clock-device/Unknown", "{}", 404},
		{"json", "POST", "/v1/time/clock-device/Heartbeat", "{", 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			r = r.WithContext(trust.WithPrincipal(r.Context(), testPrincipal(t)))
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tc.status {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
		})
	}
}

func TestHTTPRoutesDelegateAllOperations(t *testing.T) {
	a := &parityApp{}
	h := New(a).HTTPHandler()
	cases := []struct {
		path string
		msg  interface{ ProtoReflect() protoreflect.Message }
	}{
		{"CreateEnrollmentCode", &timev1.CreateEnrollmentCodeRequest{SiteId: "site", ProfileRef: "profile", Timezone: "UTC", TtlSeconds: 60}},
		{"EnrollDevice", &timev1.EnrollDeviceRequest{EnrollmentCode: "code", PublicKey: make([]byte, 32), ChallengeSignature: make([]byte, 64)}},
		{"RotateDeviceKey", &timev1.RotateDeviceKeyRequest{DeviceId: "dev", ExpectedRevision: 1, NewPublicKey: make([]byte, 32), ChallengeSignature: make([]byte, 64)}},
		{"RevokeDevice", &timev1.RevokeDeviceRequest{DeviceId: "dev", ExpectedRevision: 1, Reason: "retire"}},
		{"SyncRoster", &timev1.SyncRosterRequest{DeviceId: "dev", MaxResults: 1}},
		{"IdentifyWorker", &timev1.IdentifyWorkerRequest{DeviceId: "dev", Method: timev1.IdentificationMethod_IDENTIFICATION_METHOD_PIN, Credential: &timev1.IdentifyWorkerRequest_Pin{Pin: "1234"}}},
		{"SubmitPunches", &timev1.SubmitPunchesRequest{DeviceId: "dev", Punches: []*timev1.DevicePunch{{DeviceSequence: 1, EventType: timev1.PunchEventType_PUNCH_EVENT_TYPE_CLOCK_IN, Worker: &timev1.WorkerCredentialRef{Ref: &timev1.WorkerCredentialRef_PunchToken{PunchToken: "t"}}, DeviceOccurredAt: timestamppb.New(time.Unix(1, 0))}}}},
		{"Heartbeat", &timev1.HeartbeatRequest{DeviceId: "dev"}},
		{"GetWorkerStatus", &timev1.GetWorkerStatusRequest{DeviceId: "dev", PunchToken: "t"}},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			body := mustJSON(t, tc.msg)
			r := httptest.NewRequest("POST", "/v1/time/clock-device/"+tc.path, strings.NewReader(string(body)))
			r = r.WithContext(trust.WithPrincipal(r.Context(), testPrincipal(t)))
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != 200 {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
		})
	}
}

func testPrincipal(t *testing.T) *trust.Principal {
	t.Helper()
	p, e := trust.NewPrincipal(trust.PrincipalSpec{Tenant: "tenant-a", Subject: "device", ClientID: "dev-1", SubjectKind: trust.SubjectKindService, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh, SessionRef: "s", IssuedAt: time.Now().Add(-time.Minute), ExpiresAt: time.Now().Add(time.Hour), CredentialDigest: "digest"})
	if e != nil {
		t.Fatal(e)
	}
	return p
}
func mustJSON(t *testing.T, m interface{ ProtoReflect() protoreflect.Message }) []byte {
	t.Helper()
	b, e := protojson.Marshal(m)
	if e != nil {
		t.Fatal(e)
	}
	return b
}
