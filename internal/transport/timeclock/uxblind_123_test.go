package timeclock

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	commonv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/common/v1"
	timev1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/time/v1"
	clockservice "github.com/monstercameron/human-capital-management-suite/internal/application/clockservice"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// refusalReasonRef is the reason reference a client reads from a refusal's
// canonical error detail.
func refusalReasonRef(err error) string {
	for _, raw := range status.Convert(err).Details() {
		if detail, ok := raw.(*commonv1.ErrorDetail); ok {
			return detail.GetReasonRef()
		}
	}
	return ""
}

// refusingSelfClock answers every call with one error.
type refusingSelfClock struct{ err error }

func (r refusingSelfClock) GetSelfClock(context.Context, *trust.Principal) (clockservice.SelfClockStatus, error) {
	return clockservice.SelfClockStatus{}, r.err
}

func (r refusingSelfClock) ExecuteSelfClockAction(context.Context, *trust.Principal, clockservice.SelfClockActionRequest) (clockservice.SelfClockActionResult, error) {
	return clockservice.SelfClockActionResult{}, r.err
}

// TestTodo_UXBLIND_123_SelfClockRefusalsCarryTheirReason proves an
// ineligibility decision reaches the browser as a closed reason over both
// boundaries, without any identity, and that an outage never does.
func TestTodo_UXBLIND_123_SelfClockRefusalsCarryTheirReason(t *testing.T) {
	principal := workerClockGRPCPrincipal(t, trust.SubjectKindHuman)
	ctx := trust.WithPrincipal(context.Background(), principal)
	for _, reason := range []clockservice.SelfClockReason{clockservice.ReasonNoWorkerRecord, clockservice.ReasonNoAssignment, clockservice.ReasonNoTimeProfile, clockservice.ReasonExempt, clockservice.ReasonCaptureNotPunch} {
		decision := clockservice.NotEligible(reason, "worker-secret-ref assignment-secret-ref")
		server := NewWorkerClockServer(refusingSelfClock{err: decision})

		_, err := server.GetSelfClock(ctx, &timev1.GetSelfClockRequest{})
		if status.Code(err) != codes.FailedPrecondition || refusalReasonRef(err) != SelfClockReasonRefPrefix+strings.ToLower(string(reason)) {
			t.Fatalf("%s: gRPC read refusal = %v (ref %q)", reason, err, refusalReasonRef(err))
		}
		_, err = server.ExecuteSelfClockAction(ctx, &timev1.ExecuteSelfClockActionRequest{Action: timev1.ExecuteSelfClockActionRequest_ACTION_IN, ExpectedRevision: 1, IdempotencyKey: "k"})
		if status.Code(err) != codes.FailedPrecondition || refusalReasonRef(err) != SelfClockReasonRefPrefix+strings.ToLower(string(reason)) {
			t.Fatalf("%s: gRPC action refusal = %v (ref %q)", reason, err, refusalReasonRef(err))
		}
		if strings.Contains(status.Convert(err).Message(), "secret") {
			t.Fatalf("%s: the gRPC refusal leaked detail: %v", reason, err)
		}

		for _, request := range []*http.Request{
			httptest.NewRequest(http.MethodGet, "/v1/time/self", nil),
			httptest.NewRequest(http.MethodPost, "/v1/time/self/in", strings.NewReader(`{"expected_revision":1,"idempotency_key":"k"}`)),
		} {
			recorder := httptest.NewRecorder()
			WorkerSelfHTTPHandler(refusingSelfClock{err: decision}).ServeHTTP(recorder, request.WithContext(ctx))
			var body struct {
				Code, Message, Reason string
			}
			if recorder.Code != http.StatusPreconditionFailed || json.Unmarshal(recorder.Body.Bytes(), &body) != nil || body.Reason != string(reason) || body.Code != "failed_precondition" {
				t.Fatalf("%s: HTTP refusal = %d %s", reason, recorder.Code, recorder.Body.String())
			}
			if strings.Contains(recorder.Body.String(), "secret") {
				t.Fatalf("%s: the refusal leaked detail: %s", reason, recorder.Body.String())
			}
		}
	}

	// Not decisions: an outage, a foreign rejection and a forged state.
	for name, refusal := range map[string]error{
		"outage":           clockservice.ErrUnavailable,
		"unknown":          errors.New("boom"),
		"plain ineligible": clockservice.ErrWorkerNotEligible,
	} {
		_, err := NewWorkerClockServer(refusingSelfClock{err: refusal}).GetSelfClock(ctx, &timev1.GetSelfClockRequest{})
		if err == nil || strings.HasPrefix(refusalReasonRef(err), SelfClockReasonRefPrefix) {
			t.Fatalf("%s: was presented as a decision: %v", name, err)
		}
		recorder := httptest.NewRecorder()
		WorkerSelfHTTPHandler(refusingSelfClock{err: refusal}).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/v1/time/self", nil).WithContext(ctx))
		if strings.Contains(recorder.Body.String(), `"reason"`) {
			t.Fatalf("%s: HTTP outage carried a reason: %s", name, recorder.Body.String())
		}
	}
}

// TestTodo_UXBLIND_123_SelfClockStatusCodeReachesTheClient proves the server's
// position in the work session travels with the projection, because the
// localized label is copy and cannot decide which action a client offers.
func TestTodo_UXBLIND_123_SelfClockStatusCodeReachesTheClient(t *testing.T) {
	projection := clockservice.SelfClockStatus{WorkerLabel: "Ada", ScheduleLabel: "Day", StatusLabel: "Eingestempelt", LastEventLabel: "2026-09-29T14:00:00Z", StatusCode: "CLOCKED_IN", Revision: 7}
	app := &workerClockGRPCApp{projection: projection, result: clockservice.SelfClockActionResult{Status: projection}}
	ctx := trust.WithPrincipal(context.Background(), workerClockGRPCPrincipal(t, trust.SubjectKindHuman))
	read, err := NewWorkerClockServer(app).GetSelfClock(ctx, &timev1.GetSelfClockRequest{})
	if err != nil || read.GetStatusCode() != "CLOCKED_IN" {
		t.Fatalf("read = %v err=%v", read, err)
	}
	acted, err := NewWorkerClockServer(app).ExecuteSelfClockAction(ctx, &timev1.ExecuteSelfClockActionRequest{Action: timev1.ExecuteSelfClockActionRequest_ACTION_OUT, ExpectedRevision: 7, IdempotencyKey: "k"})
	if err != nil || acted.GetStatus().GetStatusCode() != "CLOCKED_IN" {
		t.Fatalf("action = %v err=%v", acted, err)
	}
	recorder := httptest.NewRecorder()
	WorkerSelfHTTPHandler(app).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/v1/time/self", nil).WithContext(ctx))
	if !strings.Contains(recorder.Body.String(), `"status_code":"CLOCKED_IN"`) {
		t.Fatalf("HTTP projection = %s", recorder.Body.String())
	}
}

// TestTodo_UXBLIND_123_NotEnabledWorkerClockNamesTheReason proves a workspace
// with no clock answers with the closed reason rather than a generic outage.
func TestTodo_UXBLIND_123_NotEnabledWorkerClockNamesTheReason(t *testing.T) {
	server := NotEnabledWorkerClock{}
	_, readErr := server.GetSelfClock(context.Background(), &timev1.GetSelfClockRequest{})
	_, actErr := server.ExecuteSelfClockAction(context.Background(), &timev1.ExecuteSelfClockActionRequest{})
	for name, err := range map[string]error{"read": readErr, "action": actErr} {
		if status.Code(err) != codes.FailedPrecondition || refusalReasonRef(err) != SelfClockReasonRefPrefix+"not_enabled" {
			t.Fatalf("%s: err = %v (ref %q)", name, err, refusalReasonRef(err))
		}
	}
	RegisterNotEnabledWorkerClock(nil)
	registered := grpc.NewServer()
	RegisterNotEnabledWorkerClock(registered)
	if _, ok := registered.GetServiceInfo()["hcmnext.time.v1.WorkerClockService"]; !ok {
		t.Fatal("the not-enabled worker clock was not registered")
	}
}
