package cell

import (
	"context"
	"net"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	timev1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/time/v1"
	clockservice "github.com/monstercameron/human-capital-management-suite/internal/application/clockservice"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	transporthumanwork "github.com/monstercameron/human-capital-management-suite/internal/transport/humanwork"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/manifest"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/timeclock"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/transporttest"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestRegisterClockDevice_UsesApplicationPort(t *testing.T) {
	api := &recordingClockAPI{}
	p := testClockPrincipal(t)
	server := grpc.NewServer(grpc.UnaryInterceptor(func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		return handler(trust.WithPrincipal(ctx, p), req)
	}))
	registerClockDevice(server, api)
	lis := bufconn.Listen(1 << 20)
	go func() { _ = server.Serve(lis) }()
	t.Cleanup(server.Stop)
	conn, err := grpc.NewClient("passthrough:///clock", grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return lis.Dial() }), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	ctx := context.Background()
	client := timev1.NewClockDeviceServiceClient(conn)
	_, err = client.SubmitPunches(ctx, &timev1.SubmitPunchesRequest{DeviceId: "d", Punches: []*timev1.DevicePunch{{DeviceSequence: 1, EventType: timev1.PunchEventType_PUNCH_EVENT_TYPE_CLOCK_IN, Worker: &timev1.WorkerCredentialRef{Ref: &timev1.WorkerCredentialRef_PunchToken{PunchToken: "p"}}, DeviceOccurredAt: timestamppb.New(time.Unix(1, 0))}}})
	if err != nil {
		t.Fatal(err)
	}
	if api.submitCalls != 1 {
		t.Fatalf("application submit calls = %d, want 1", api.submitCalls)
	}
}

func TestClockDeviceHTTPMountsBothCanonicalPrefixes(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	verifier, err := transporttest.NewVerifier(func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	api := &recordingClockAPI{}
	config := transporttest.Config(verifier, func() time.Time { return now }, "clock-cell", nil)
	h, err := newEdgeHandlerWithDependenciesAndServices(&app.Cell{Config: config, Discovery: &manifest.DiscoveryDocument{}}, nil, nil, nil, nil, nil, nil, transporthumanwork.WritePorts{}, nil, ServiceHandlers{ClockDevice: api})
	if err != nil {
		t.Fatal(err)
	}
	claims := transporttest.DefaultClaims(now)
	token, err := transporttest.BearerToken(verifier, claims)
	if err != nil {
		t.Fatal(err)
	}
	body, err := protojson.Marshal(&timev1.HeartbeatRequest{DeviceId: "d"})
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/v1/time/clock-device/Heartbeat", "/hcmnext.time.v1.ClockDeviceService/Heartbeat"} {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest("POST", path, strings.NewReader(string(body)))
			req.Header.Set(transport.AuthorizationMetadataKey, token)
			req.Header.Set(transport.RequestIDMetadataKey, "clock-http")
			req.Header.Set("Content-Type", "application/json")
			res := httptest.NewRecorder()
			h.ServeHTTP(res, req)
			if res.Code != 200 {
				t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
			}
		})
	}
	if api.heartbeatCalls != 2 {
		t.Fatalf("application heartbeat calls = %d, want 2", api.heartbeatCalls)
	}
}

func TestClockDeviceHTTPRequiresCanonicalAdmission(t *testing.T) {
	verifier, err := transporttest.NewVerifier(time.Now)
	if err != nil {
		t.Fatal(err)
	}
	h, err := newEdgeHandlerWithDependenciesAndServices(&app.Cell{Config: transporttest.Config(verifier, time.Now, "clock-auth", nil), Discovery: &manifest.DiscoveryDocument{}}, nil, nil, nil, nil, nil, nil, transporthumanwork.WritePorts{}, nil, ServiceHandlers{ClockDevice: &recordingClockAPI{}})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("POST", "/v1/time/clock-device/Heartbeat", strings.NewReader(`{"device_id":"d"}`))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)
	if res.Code != 401 {
		t.Fatalf("unauthenticated status=%d body=%s", res.Code, res.Body.String())
	}
}

func TestClockDeviceHTTPPrefixesUseActualFacade(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	device := clockservice.DeviceRecord{TenantID: "tenant", ID: "device-1", PublicKey: make([]byte, 32), SiteID: "site", ProfileID: "profile", Timezone: "UTC", State: "ACTIVE", Revision: 1}
	store := &actualClockDeviceStore{device: device}
	facade := clockservice.DeviceFacade{
		Service:      clockservice.Service{Devices: store, Clock: func() time.Time { return now }},
		Tokens:       actualTokenVerifier{claims: clockservice.DeviceWorkerTokenClaims{TenantID: "tenant", DeviceID: "device-1", WorkerID: "worker-1", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Minute)}},
		WorkerStatus: actualWorkerStatus{},
		Clock:        func() time.Time { return now },
	}
	verifier, err := transporttest.NewVerifier(func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	h, err := newEdgeHandlerWithDependenciesAndServices(&app.Cell{Config: transporttest.Config(verifier, func() time.Time { return now }, "actual-clock", nil), Discovery: &manifest.DiscoveryDocument{}}, nil, nil, nil, nil, nil, nil, transporthumanwork.WritePorts{}, nil, ServiceHandlers{ClockDevice: facade})
	if err != nil {
		t.Fatal(err)
	}
	claims := transporttest.DefaultClaims(now)
	claims.Tenant, claims.ClientID, claims.SubjectKind = "tenant", "device-1", "service"
	token, err := transporttest.BearerToken(verifier, claims)
	if err != nil {
		t.Fatal(err)
	}
	body := `{"device_id":"device-1","punch_token":"token"}`
	for _, path := range []string{"/v1/time/clock-device/GetWorkerStatus", "/hcmnext.time.v1.ClockDeviceService/GetWorkerStatus"} {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest("POST", path, strings.NewReader(body))
			req.Header.Set(transport.AuthorizationMetadataKey, token)
			req.Header.Set(transport.RequestIDMetadataKey, "actual-clock")
			req.Header.Set("Content-Type", "application/json")
			res := httptest.NewRecorder()
			h.ServeHTTP(res, req)
			if res.Code != 200 || !strings.Contains(res.Body.String(), "worker-1") {
				t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
			}
		})
	}
	forged := httptest.NewRequest("POST", "/v1/time/clock-device/GetWorkerStatus", strings.NewReader(`{"device_id":"forged","punch_token":"token"}`))
	forged.Header.Set(transport.AuthorizationMetadataKey, token)
	forged.Header.Set(transport.RequestIDMetadataKey, "actual-clock-forged")
	forged.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	h.ServeHTTP(res, forged)
	if res.Code != 412 {
		t.Fatalf("forged device status=%d body=%s", res.Code, res.Body.String())
	}
}

type recordingClockAPI struct {
	submitCalls, heartbeatCalls int
}

func (a *recordingClockAPI) CreateEnrollmentCode(context.Context, *trust.Principal, clockservice.EnrollmentRequest) (clockservice.EnrollmentResult, error) {
	return clockservice.EnrollmentResult{}, nil
}
func (a *recordingClockAPI) EnrollDevice(context.Context, *trust.Principal, clockservice.DeviceEnrollmentRequest) (clockservice.DeviceRecord, error) {
	return clockservice.DeviceRecord{}, nil
}
func (a *recordingClockAPI) RotateDeviceKey(context.Context, *trust.Principal, clockservice.DeviceKeyRotationRequest) (clockservice.DeviceRecord, error) {
	return clockservice.DeviceRecord{}, nil
}
func (a *recordingClockAPI) RevokeDevice(context.Context, *trust.Principal, clockservice.DeviceLifecycleRequest) (clockservice.DeviceRecord, error) {
	return clockservice.DeviceRecord{}, nil
}
func (a *recordingClockAPI) SyncRoster(context.Context, *trust.Principal, string, string) (clockservice.RosterDelta, error) {
	return clockservice.RosterDelta{}, nil
}
func (a *recordingClockAPI) IdentifyDevice(context.Context, *trust.Principal, clockservice.IdentifyDeviceRequest) (clockservice.IdentifyResult, error) {
	return clockservice.IdentifyResult{}, nil
}
func (a *recordingClockAPI) SubmitPunches(context.Context, *trust.Principal, clockservice.BatchRequest) (clockservice.BatchResponse, error) {
	a.submitCalls++
	return clockservice.BatchResponse{HighestContiguous: 1}, nil
}
func (a *recordingClockAPI) Heartbeat(context.Context, *trust.Principal, clockservice.HeartbeatRequest) (clockservice.HeartbeatResult, error) {
	a.heartbeatCalls++
	return clockservice.HeartbeatResult{RecordedAt: time.Unix(100, 0)}, nil
}
func (a *recordingClockAPI) GetWorkerStatus(context.Context, *trust.Principal, clockservice.WorkerStatusRequest) (clockservice.WorkerStatusResult, error) {
	return clockservice.WorkerStatusResult{}, nil
}

func testClockPrincipal(t *testing.T) *trust.Principal {
	t.Helper()
	now := time.Unix(100, 0)
	p, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: "tenant", Subject: "subject", SubjectKind: trust.SubjectKindService, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh, SessionRef: "session", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Minute), CredentialDigest: "digest"})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

var _ clockservice.DeviceAPI = (*recordingClockAPI)(nil)
var _ timeclock.Service = clockDeviceAdapter{}

type actualClockDeviceStore struct{ device clockservice.DeviceRecord }

func (s *actualClockDeviceStore) GetDevice(context.Context, string, string) (clockservice.DeviceRecord, error) {
	return s.device, nil
}
func (s *actualClockDeviceStore) DevicesBySite(context.Context, string, string, int) ([]clockservice.DeviceRecord, error) {
	return []clockservice.DeviceRecord{s.device}, nil
}
func (s *actualClockDeviceStore) RotateDeviceKey(context.Context, string, string, []byte, string, string, int64) (clockservice.DeviceRecord, error) {
	return s.device, nil
}
func (s *actualClockDeviceStore) SuspendDevice(context.Context, string, string, string, string, int64) (clockservice.DeviceRecord, error) {
	return s.device, nil
}
func (s *actualClockDeviceStore) ResumeDevice(context.Context, string, string, string, string, int64) (clockservice.DeviceRecord, error) {
	return s.device, nil
}
func (s *actualClockDeviceStore) RevokeDevice(context.Context, string, string, string, string, int64) (clockservice.DeviceRecord, error) {
	return s.device, nil
}
func (s *actualClockDeviceStore) ReassignDeviceSite(context.Context, string, string, string, string, string, string, int64) (clockservice.DeviceRecord, error) {
	return s.device, nil
}

type actualTokenVerifier struct {
	claims clockservice.DeviceWorkerTokenClaims
}

func (v actualTokenVerifier) VerifyDeviceWorkerToken(context.Context, string) (clockservice.DeviceWorkerTokenClaims, error) {
	return v.claims, nil
}

type actualWorkerStatus struct{}

func (actualWorkerStatus) ResolveWorkerStatus(context.Context, clockservice.DeviceWorkerTokenClaims) (clockservice.WorkerStatusResult, error) {
	return clockservice.WorkerStatusResult{WorkerID: "worker-1", DisplayName: "Ada"}, nil
}
