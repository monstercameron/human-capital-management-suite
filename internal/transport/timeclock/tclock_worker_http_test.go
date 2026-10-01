package timeclock

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	clockservice "github.com/monstercameron/human-capital-management-suite/internal/application/clockservice"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type workerHTTPFake struct{}

func (workerHTTPFake) GetSelfClock(context.Context, *trust.Principal) (clockservice.SelfClockStatus, error) {
	return clockservice.SelfClockStatus{WorkerLabel: "Taylor", ScheduleLabel: "Day", StatusLabel: "Clocked out", LastEventLabel: "No event", Revision: 3}, nil
}
func (workerHTTPFake) ExecuteSelfClockAction(_ context.Context, _ *trust.Principal, req clockservice.SelfClockActionRequest) (clockservice.SelfClockActionResult, error) {
	return clockservice.SelfClockActionResult{ReceiptID: req.IdempotencyKey, WorkerRef: "worker-1", AssignmentRef: "assignment-1", WorkflowInstanceRef: "00000000-0000-0000-0000-000000000001", PublishedPlanRef: "plan-1", WorkflowID: "hcmnext.workflows.time.clock_in_out", WorkflowTraceID: "trace-1", WorkflowNodeID: "commit_punch", WorkflowAttempt: 1, WorkflowInstanceVersion: 2, Status: clockservice.SelfClockStatus{Revision: 4}}, nil
}

func TestTodo_TCLOCK_WorkerSelfHTTPRequiresTrustedPrincipal(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/v1/time/self", nil)
	w := httptest.NewRecorder()
	WorkerSelfHTTPHandler(workerHTTPFake{}).ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestTodo_TCLOCK_WorkerSelfHTTPReadsAndExecutesActions(t *testing.T) {
	p := testPrincipal(t)
	h := WorkerSelfHTTPHandler(workerHTTPFake{})
	get := httptest.NewRequest(http.MethodGet, "/v1/time/self", nil).WithContext(trust.WithPrincipal(context.Background(), p))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, get)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Taylor") {
		t.Fatalf("GET status=%d body=%s", w.Code, w.Body.String())
	}
	post := httptest.NewRequest(http.MethodPost, "/v1/time/self/in", strings.NewReader(`{"expected_revision":3,"idempotency_key":"idem-1"}`)).WithContext(trust.WithPrincipal(context.Background(), p))
	w = httptest.NewRecorder()
	h.ServeHTTP(w, post)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "idem-1") {
		t.Fatalf("POST status=%d body=%s", w.Code, w.Body.String())
	}
}
