package timeclock

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	commonv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/common/v1"
	timev1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/time/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type tclockT26App struct {
	operations []string
	principal  *trust.Principal
	request    TimeRequest
	result     TimeResult
}

func (a *tclockT26App) HandleTime(_ context.Context, p *trust.Principal, req TimeRequest) (TimeResult, error) {
	a.principal = p
	a.request = req
	a.operations = append(a.operations, req.Operation)
	if a.result.Session == nil {
		a.result.Session = &TimeSession{ID: "session-1", WorkerID: "worker-1", Status: "OPEN", Revision: 2, ETag: "etag-2", OpenedAt: time.Unix(10, 0).UTC()}
	}
	return a.result, nil
}

func tclockT26ClockInRequest(t *testing.T) *timev1.ClockInRequest {
	t.Helper()
	return &timev1.ClockInRequest{WorkerId: "worker-1", JobId: "job-1", OccurredAt: timestamppb.New(time.Unix(10, 0).UTC()), IdempotencyKey: "idem-1"}
}

func TestTodo_FTIME_008(t *testing.T) {
	app := &tclockT26App{}
	server := NewTimeServer(app)
	p := testPrincipal(t)
	got, err := server.ClockIn(trust.WithPrincipal(context.Background(), p), tclockT26ClockInRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	if got.GetSession().GetSessionId() != "session-1" || got.GetSession().GetWorkerId() != "worker-1" {
		t.Fatalf("projection=%v", got.GetSession())
	}
	if app.request.Operation != "ClockIn" || app.principal != p || app.request.WorkerID != "worker-1" {
		t.Fatalf("application call=%+v principal=%v", app.request, app.principal)
	}
}

func TestTodo_FTIME_008_Conformance(t *testing.T) {
	methods := []string{"ClockIn", "StartBreak", "EndBreak", "TransferJob", "ClockOut", "GetCurrentSession", "ListSessions", "GetTimecard", "ListTimecards", "SubmitTimecard", "AttestTimecard", "ApproveTimecard", "ReopenTimecard", "CorrectPunch", "RequestMissedPunch", "DecideMissedPunch", "CreateShift", "PublishShift", "CancelShift", "ListShifts", "GetTimeProfile", "AssignTimeProfile", "ListTimeEvents"}
	for _, method := range methods {
		if timeRequestMessage(method) == nil || len(httpRoutes("hcmnext.time.v1.TimeService", method)) != 1 {
			t.Errorf("method %s is not represented in both projections", method)
		}
	}
	document, err := OpenAPIDocument()
	if err != nil {
		t.Fatal(err)
	}
	for _, method := range methods {
		if !strings.Contains(string(document), "/v1/time/"+method) {
			t.Errorf("OpenAPI document missing TimeService/%s", method)
		}
	}
}

func TestTodo_FTIME_008_Security(t *testing.T) {
	server := NewTimeServer(&tclockT26App{})
	if _, err := server.ClockIn(context.Background(), tclockT26ClockInRequest(t)); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("missing principal code=%v err=%v", status.Code(err), err)
	}
	app := &tclockT26App{}
	server = NewTimeServer(app)
	request := tclockT26ClockInRequest(t)
	request.ScopeContext = &commonv1.ScopeContext{TenantId: "foreign-tenant", OrganizationScopeId: "foreign-org", Purpose: "impersonate"}
	// The request's scope field is not an authority source; the trusted
	// principal is the only identity passed to the application.
	if _, err := server.ClockIn(trust.WithPrincipal(context.Background(), testPrincipal(t)), request); err != nil {
		t.Fatal(err)
	}
	if app.principal == nil || app.principal.Tenant() != "tenant-a" || app.request.WorkerID != "worker-1" {
		t.Fatalf("untrusted scope altered application identity: principal=%v request=%+v", app.principal, app.request)
	}
}

func TestTodo_FTIME_008_Integration(t *testing.T) {
	app := &tclockT26App{}
	handler := NewTimeServer(app).HTTPHandler()
	r := httptest.NewRequest(http.MethodPost, "/v1/time/ClockIn", strings.NewReader(`{"worker_id":"worker-1","job_id":"job-1","occurred_at":"1970-01-01T00:00:10Z","idempotency_key":"idem-1"}`))
	r = r.WithContext(trust.WithPrincipal(r.Context(), testPrincipal(t)))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var got timev1.ClockInResponse
	if err := (protojson.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.GetSession().GetSessionId() != "session-1" || len(app.operations) != 1 || app.operations[0] != "ClockIn" {
		t.Fatalf("response=%v operations=%v", got.GetSession(), app.operations)
	}
	grpcServer := grpc.NewServer(grpc.UnaryInterceptor(func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		return handler(trust.WithPrincipal(ctx, testPrincipal(t)), req)
	}))
	RegisterTimeService(grpcServer, app)
	lis := bufconn.Listen(1 << 20)
	go func() { _ = grpcServer.Serve(lis) }()
	defer grpcServer.Stop()
	conn, err := grpc.NewClient("passthrough:///tclock", grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return lis.Dial() }), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	grpcResult, err := timev1.NewTimeServiceClient(conn).ClockIn(context.Background(), tclockT26ClockInRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	if grpcResult.GetSession().GetSessionId() != got.GetSession().GetSessionId() || app.request.IdempotencyKey != "idem-1" {
		t.Fatalf("HTTP/gRPC parity mismatch: http=%v grpc=%v request=%+v", got.GetSession(), grpcResult.GetSession(), app.request)
	}
}
