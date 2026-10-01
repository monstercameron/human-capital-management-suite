package cell

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoGRPCBridge/pkg/grpctunnel"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	commonv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/common/v1"
	timev1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/time/v1"
	clockservice "github.com/monstercameron/human-capital-management-suite/internal/application/clockservice"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	transporthumanwork "github.com/monstercameron/human-capital-management-suite/internal/transport/humanwork"
	transportposition "github.com/monstercameron/human-capital-management-suite/internal/transport/position"
	transporttimeclock "github.com/monstercameron/human-capital-management-suite/internal/transport/timeclock"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// reasonRefOf reads the reason reference from a refusal's canonical error
// detail, as the browser client does.
func reasonRefOf(err error) string {
	for _, raw := range status.Convert(err).Details() {
		if detail, ok := raw.(*commonv1.ErrorDetail); ok {
			return detail.GetReasonRef()
		}
	}
	return ""
}

// tunnelWorkerClock is a self clock that serves one worker and refuses another
// with an ineligibility decision.
type tunnelWorkerClock struct{ decision error }

func (c tunnelWorkerClock) GetSelfClock(context.Context, *trust.Principal) (clockservice.SelfClockStatus, error) {
	if c.decision != nil {
		return clockservice.SelfClockStatus{}, c.decision
	}
	return clockservice.SelfClockStatus{WorkerLabel: "Ben", ScheduleLabel: "Journeyman Carpenter", StatusLabel: "Clocked out", LastEventLabel: "No event", StatusCode: "CLOCKED_OUT", Revision: 4}, nil
}

func (c tunnelWorkerClock) ExecuteSelfClockAction(context.Context, *trust.Principal, clockservice.SelfClockActionRequest) (clockservice.SelfClockActionResult, error) {
	return clockservice.SelfClockActionResult{}, c.decision
}

func tunnelClient(t *testing.T, workerClock transporttimeclock.WorkerSelfService) (*grpc.ClientConn, context.Context) {
	t.Helper()
	c, token := tunnelBridgeCell(t)
	srv, err := NewTunnelGRPCServerWithWorkerClock(c, nil, nil, nil, nil, transporthumanwork.WritePorts{}, nil, nil, nil, nil, transportposition.Dependencies{}, nil, nil, nil, nil, workerClock)
	if err != nil {
		t.Fatalf("NewTunnelGRPCServerWithWorkerClock: %v", err)
	}
	t.Cleanup(srv.Stop)
	bridge, err := newTunnelHandler(c, srv, "")
	if err != nil {
		t.Fatalf("newTunnelHandler: %v", err)
	}
	edge := httptest.NewServer(bridge)
	t.Cleanup(edge.Close)
	host := edge.URL[len("http://"):]
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	conn, err := grpctunnel.BuildTunnelConn(ctx, grpctunnel.TunnelConfig{
		Target:      "ws://" + host + TunnelPath,
		Headers:     http.Header{"Authorization": []string{token}, "Origin": []string{"http://" + host}},
		GRPCOptions: []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())},
	})
	if err != nil {
		t.Fatalf("BuildTunnelConn: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn, metadata.AppendToOutgoingContext(ctx, transport.AuthorizationMetadataKey, token)
}

// TestTodo_UXBLIND_123_TunnelCarriesTheWorkerClockAndNothingAroundIt proves the
// browser reads the clock over the workspace tunnel, that an ineligibility
// decision arrives with its reason reference intact, that a workspace with no
// clock answers UNIMPLEMENTED, and that the device service stays off the
// tunnel.
func TestTodo_UXBLIND_123_TunnelCarriesTheWorkerClockAndNothingAroundIt(t *testing.T) {
	for path, want := range map[string]bool{
		"/hcmnext.time.v1.WorkerClockService/GetSelfClock":           true,
		"/hcmnext.time.v1.WorkerClockService/ExecuteSelfClockAction": true,
		"/hcmnext.time.v1.ClockDeviceService/SyncRoster":             false,
		"/hcmnext.time.v1.ClockDeviceService/SubmitPunches":          false,
	} {
		if got := tunnelAllowsService(path); got != want {
			t.Errorf("tunnelAllowsService(%q) = %v, want %v", path, got, want)
		}
	}

	conn, ctx := tunnelClient(t, tunnelWorkerClock{})
	read, err := timev1.NewWorkerClockServiceClient(conn).GetSelfClock(ctx, &timev1.GetSelfClockRequest{})
	if err != nil || read.GetWorkerLabel() != "Ben" || read.GetStatusCode() != "CLOCKED_OUT" || read.GetRevision() != 4 {
		t.Fatalf("GetSelfClock over the tunnel = %v err=%v", read, err)
	}
	if _, err := timev1.NewClockDeviceServiceClient(conn).SyncRoster(ctx, &timev1.SyncRosterRequest{}); status.Code(err) != codes.Unimplemented {
		t.Fatalf("ClockDeviceService over the tunnel = %v, want UNIMPLEMENTED", err)
	}

	decided, callCtx := tunnelClient(t, tunnelWorkerClock{decision: clockservice.NotEligible(clockservice.ReasonExempt, "worker-secret-ref")})
	_, err = timev1.NewWorkerClockServiceClient(decided).GetSelfClock(callCtx, &timev1.GetSelfClockRequest{})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("ineligible worker over the tunnel = %v, want FAILED_PRECONDITION", err)
	}
	ref := reasonRefOf(err)
	if ref != transporttimeclock.SelfClockReasonRefPrefix+"exempt" || strings.Contains(err.Error(), "secret") {
		t.Fatalf("reason reference = %q, error = %v", ref, err)
	}

	none, noneCtx := tunnelClient(t, nil)
	_, err = timev1.NewWorkerClockServiceClient(none).GetSelfClock(noneCtx, &timev1.GetSelfClockRequest{})
	if status.Code(err) != codes.FailedPrecondition || reasonRefOf(err) != transporttimeclock.SelfClockReasonRefPrefix+"not_enabled" {
		t.Fatalf("a workspace with no clock answered %v (ref %q), want FAILED_PRECONDITION not_enabled", err, reasonRefOf(err))
	}
	_, err = timev1.NewWorkerClockServiceClient(none).ExecuteSelfClockAction(noneCtx, &timev1.ExecuteSelfClockActionRequest{Action: timev1.ExecuteSelfClockActionRequest_ACTION_IN, ExpectedRevision: 1, IdempotencyKey: "k"})
	if status.Code(err) != codes.FailedPrecondition || reasonRefOf(err) != transporttimeclock.SelfClockReasonRefPrefix+"not_enabled" {
		t.Fatalf("an action on a workspace with no clock answered %v", err)
	}
}
