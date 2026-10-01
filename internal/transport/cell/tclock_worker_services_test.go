package cell

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	clockservice "github.com/monstercameron/human-capital-management-suite/internal/application/clockservice"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/timeclock"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/transporttest"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type workerClockCellFake struct{}

func (workerClockCellFake) GetSelfClock(context.Context, *trust.Principal) (clockservice.SelfClockStatus, error) {
	return clockservice.SelfClockStatus{WorkerLabel: "Ada", ScheduleLabel: "Day", StatusLabel: "Clocked out", LastEventLabel: "None", Revision: 4}, nil
}

func (workerClockCellFake) ExecuteSelfClockAction(context.Context, *trust.Principal, clockservice.SelfClockActionRequest) (clockservice.SelfClockActionResult, error) {
	return clockservice.SelfClockActionResult{ReceiptID: "receipt-1", WorkerRef: "worker-1", AssignmentRef: "assignment-1", WorkflowInstanceRef: "00000000-0000-0000-0000-000000000001", PublishedPlanRef: "plan-1", WorkflowID: "hcmnext.workflows.time.clock_in_out", WorkflowTraceID: "trace-1", WorkflowNodeID: "commit_punch", WorkflowAttempt: 1, WorkflowInstanceVersion: 2, Status: clockservice.SelfClockStatus{WorkerLabel: "Ada", StatusLabel: "Clocked in", Revision: 5}}, nil
}

func TestRegisterWorkerClockPublishesNativeService(t *testing.T) {
	server := grpc.NewServer()
	registerWorkerClock(server, workerClockCellFake{})
	if _, ok := server.GetServiceInfo()["hcmnext.time.v1.WorkerClockService"]; !ok {
		t.Fatal("worker clock service was not registered")
	}
	registerWorkerClock(nil, workerClockCellFake{})
	registerWorkerClock(server, nil)
}

func TestWorkerClockHTTPUsesCanonicalHumanAdmission(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	verifier, err := transporttest.NewVerifier(func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	cfg := transporttest.Config(verifier, func() time.Time { return now }, "worker-clock-cell", nil)
	h := workerClockHTTP(cfg, workerClockCellFake{})

	unauthenticated := httptest.NewRecorder()
	h.ServeHTTP(unauthenticated, httptest.NewRequest(http.MethodGet, "/v1/time/self", nil))
	if unauthenticated.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status=%d", unauthenticated.Code)
	}

	claims := transporttest.DefaultClaims(now)
	token, err := transporttest.BearerToken(verifier, claims)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/v1/time/self", nil)
	req.Header.Set(transport.AuthorizationMetadataKey, token)
	req.Header.Set(transport.RequestIDMetadataKey, "worker-clock-http")
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)
	if res.Code != http.StatusOK || !strings.Contains(res.Body.String(), "worker_label") {
		t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
	}
}

func TestWorkerClockHTTPDoesNotTurnWorkflowFailureIntoReceipt(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	verifier, err := transporttest.NewVerifier(func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	failing := workerClockFailureApp{}
	h := workerClockHTTP(transporttest.Config(verifier, func() time.Time { return now }, "worker-clock-failure", nil), failing)
	token, err := transporttest.BearerToken(verifier, transporttest.DefaultClaims(now))
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/time/self/in", strings.NewReader(`{"expected_revision":4,"idempotency_key":"idem-1"}`))
	req.Header.Set(transport.AuthorizationMetadataKey, token)
	req.Header.Set(transport.RequestIDMetadataKey, "worker-clock-failure")
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)
	if res.Code != http.StatusServiceUnavailable || strings.Contains(res.Body.String(), "receipt_id") {
		t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
	}
}

type workerClockFailureApp struct{}

func (workerClockFailureApp) GetSelfClock(context.Context, *trust.Principal) (clockservice.SelfClockStatus, error) {
	return clockservice.SelfClockStatus{WorkerLabel: "Ada", StatusLabel: "Clocked out", Revision: 4}, nil
}

func (workerClockFailureApp) ExecuteSelfClockAction(context.Context, *trust.Principal, clockservice.SelfClockActionRequest) (clockservice.SelfClockActionResult, error) {
	return clockservice.SelfClockActionResult{}, status.Error(codes.Unavailable, "workflow execution unavailable")
}

var _ timeclock.WorkerSelfService = workerClockCellFake{}
