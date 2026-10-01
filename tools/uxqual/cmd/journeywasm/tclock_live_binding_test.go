package main

import (
	"context"
	"os"
	"strings"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	commonv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/common/v1"
	timev1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/time/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/taskmux"
)

// fakeWorkerClock stands in for the workspace tunnel's worker clock service.
type fakeWorkerClock struct {
	get       func(context.Context, *timev1.GetSelfClockRequest) (*timev1.GetSelfClockResponse, error)
	execute   func(context.Context, *timev1.ExecuteSelfClockActionRequest) (*timev1.ExecuteSelfClockActionResponse, error)
	lastMeta  metadata.MD
	lastExec  *timev1.ExecuteSelfClockActionRequest
	execCalls int
}

func (f *fakeWorkerClock) GetSelfClock(ctx context.Context, in *timev1.GetSelfClockRequest, _ ...grpc.CallOption) (*timev1.GetSelfClockResponse, error) {
	f.lastMeta, _ = metadata.FromOutgoingContext(ctx)
	return f.get(ctx, in)
}

func (f *fakeWorkerClock) ExecuteSelfClockAction(ctx context.Context, in *timev1.ExecuteSelfClockActionRequest, _ ...grpc.CallOption) (*timev1.ExecuteSelfClockActionResponse, error) {
	f.lastMeta, _ = metadata.FromOutgoingContext(ctx)
	f.lastExec = in
	f.execCalls++
	return f.execute(ctx, in)
}

// refusalWithRef is a refusal as the server sends one: a status carrying the
// platform's canonical error detail with a reason reference.
func refusalWithRef(code codes.Code, ref string) error {
	st, err := status.New(code, "the time clock is not available for this worker").WithDetails(&commonv1.ErrorDetail{ReasonRef: ref})
	if err != nil {
		panic(err)
	}
	return st.Err()
}

func newFakeBinding(fake *fakeWorkerClock) *clockLiveBinding {
	binding := newClockLiveBinding(journeyclient.Config{Bearer: "secret-token"}, nil)
	binding.client = fake
	return binding
}

func TestTodoTClockLiveBinding_UsesWorkspaceTunnelNotHTTP(t *testing.T) {
	body, err := os.ReadFile("tclock_live_binding.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(body)
	for _, want := range []string{"NewWorkerClockServiceClient", "GetSelfClock", "ExecuteSelfClockAction", "ExpectedRevision", "IdempotencyKey", "AuthorizationHeader", "BearerScheme", "ClockProjectionReady", "ClockActionState"} {
		if !strings.Contains(source, want) {
			t.Fatalf("clock binding missing %q", want)
		}
	}
	// The page's connect-src allows the workspace tunnel and nothing else; a
	// direct HTTP call is blocked by the browser and leaves the page blank.
	for _, forbidden := range []string{"fetch", "/v1/time/self", "GetWorkerStatus", "XMLHttpRequest", "syscall/js"} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("clock binding must not use %q", forbidden)
		}
	}
	for _, forbidden := range []string{"Label: \"Clock in\"", "Label: \"Clock out\""} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("binding must not hardcode localized action label %q", forbidden)
		}
	}
	// The reason prefix is a contract with the server: pin the same literal.
	transport, err := os.ReadFile("../../../../internal/transport/timeclock/tclock_worker_grpc.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(transport), `SelfClockReasonRefPrefix = "`+clockSelfReasonRefPrefix+`"`) {
		t.Fatal("the client and server disagree on the self-clock reason prefix")
	}
}

func TestTodoTClockLiveBinding_ReadsProjectionAndCarriesBearer(t *testing.T) {
	fake := &fakeWorkerClock{get: func(context.Context, *timev1.GetSelfClockRequest) (*timev1.GetSelfClockResponse, error) {
		return &timev1.GetSelfClockResponse{WorkerLabel: "Taylor", ScheduleLabel: "Day", StatusLabel: "Clocked in", LastEventLabel: "2026-09-29T14:00:00Z", StatusCode: "CLOCKED_IN", Revision: 8}, nil
	}}
	binding := newFakeBinding(fake)
	projection, err := binding.ReadClockProjection(context.Background())
	if err != nil || projection.State != productui.ClockProjectionReady || projection.WorkerLabel != "Taylor" || binding.ClockRevision() != 8 {
		t.Fatalf("projection=%+v err=%v revision=%d", projection, err, binding.ClockRevision())
	}
	if projection.Phase != productui.ClockPhaseIn || clockActionForPhase(projection.Phase) != "out" {
		t.Fatalf("clocked-in worker got phase %v and action %q, want out", projection.Phase, clockActionForPhase(projection.Phase))
	}
	if got := fake.lastMeta.Get(journeyclient.AuthorizationHeader); len(got) != 1 || got[0] != "Bearer secret-token" {
		t.Fatalf("authorization metadata = %v", got)
	}
}

func TestTodoTClockLiveBinding_MapsPrimaryActionForEachPhase(t *testing.T) {
	for _, tc := range []struct {
		code   string
		phase  productui.ClockPhase
		action string
	}{
		{"CLOCKED_OUT", productui.ClockPhaseOut, "in"},
		{"CLOCKED_IN", productui.ClockPhaseIn, "out"},
		{"ON_BREAK", productui.ClockPhaseBreak, "end_break"},
		{"", productui.ClockPhaseUnknown, ""},
		{"SOMETHING_ELSE", productui.ClockPhaseUnknown, ""},
	} {
		if got := clockPhaseFromCode(tc.code); got != tc.phase {
			t.Fatalf("phase for %q = %v, want %v", tc.code, got, tc.phase)
		}
		if got := clockActionForPhase(clockPhaseFromCode(tc.code)); got != tc.action {
			t.Fatalf("action for %q = %q, want %q", tc.code, got, tc.action)
		}
	}
}

func TestTodoTClockLiveBinding_RefusalsBecomeReasonsOrOutagesNeverTheWrongOne(t *testing.T) {
	for _, reason := range []string{"NOT_ENABLED", "NO_WORKER_RECORD", "NO_ASSIGNMENT", "NO_TIME_PROFILE", "EXEMPT", "CAPTURE_NOT_PUNCH"} {
		fake := &fakeWorkerClock{get: func(context.Context, *timev1.GetSelfClockRequest) (*timev1.GetSelfClockResponse, error) {
			return nil, refusalWithRef(codes.FailedPrecondition, clockSelfReasonRefPrefix+strings.ToLower(reason))
		}}
		binding := newFakeBinding(fake)
		projection, err := binding.ReadClockProjection(context.Background())
		if err != nil || projection.State != productui.ClockProjectionUnavailable || string(projection.Reason) != reason {
			t.Fatalf("%s: projection=%+v err=%v", reason, projection, err)
		}
		if projection.WorkerLabel != "" || projection.ClockIn != nil || projection.ClockOut != nil {
			t.Fatalf("%s: a refusal carried data or an action: %+v", reason, projection)
		}
	}
	// A service that is not registered is the workspace not running the clock.
	binding := newFakeBinding(&fakeWorkerClock{get: func(context.Context, *timev1.GetSelfClockRequest) (*timev1.GetSelfClockResponse, error) {
		return nil, status.Error(codes.Unimplemented, "unknown service")
	}})
	if projection, err := binding.ReadClockProjection(context.Background()); err != nil || projection.Reason != productui.ClockReasonNotEnabled {
		t.Fatalf("unimplemented projection=%+v err=%v", projection, err)
	}
	// Outages, forged reasons and other preconditions are never a verdict.
	for name, refusal := range map[string]error{
		"unavailable":     status.Error(codes.Unavailable, "down"),
		"internal":        status.Error(codes.Internal, "boom"),
		"unknown reason":  refusalWithRef(codes.FailedPrecondition, clockSelfReasonRefPrefix+"ask_your_supervisor"),
		"message only":    status.Error(codes.FailedPrecondition, clockSelfReasonRefPrefix+"exempt"),
		"other precond":   status.Error(codes.FailedPrecondition, "clockservice: worker is not eligible"),
		"wrong code":      refusalWithRef(codes.PermissionDenied, clockSelfReasonRefPrefix+"exempt"),
		"plain error":     context.DeadlineExceeded,
		"prefix only":     refusalWithRef(codes.FailedPrecondition, clockSelfReasonRefPrefix),
		"embedded prefix": refusalWithRef(codes.FailedPrecondition, "x "+clockSelfReasonRefPrefix+"exempt"),
	} {
		refusal := refusal
		binding := newFakeBinding(&fakeWorkerClock{get: func(context.Context, *timev1.GetSelfClockRequest) (*timev1.GetSelfClockResponse, error) {
			return nil, refusal
		}})
		projection, err := binding.ReadClockProjection(context.Background())
		if err == nil || projection.Reason != productui.ClockReasonNone {
			t.Fatalf("%s: a failed read was presented as a decision: %+v err=%v", name, projection, err)
		}
	}
}

func TestTodoTClockLiveBinding_RejectsIncompleteProjection(t *testing.T) {
	binding := newFakeBinding(&fakeWorkerClock{get: func(context.Context, *timev1.GetSelfClockRequest) (*timev1.GetSelfClockResponse, error) {
		return &timev1.GetSelfClockResponse{WorkerLabel: "Taylor", Revision: 3}, nil
	}})
	if _, err := binding.ReadClockProjection(context.Background()); err == nil {
		t.Fatal("an incomplete projection was accepted")
	}
	if binding.ClockRevision() != 0 {
		t.Fatal("an incomplete projection moved the action revision")
	}
}

func TestTodoTClockLiveBinding_RejectsForgedReceiptAndUsesActionRevision(t *testing.T) {
	fake := &fakeWorkerClock{execute: func(context.Context, *timev1.ExecuteSelfClockActionRequest) (*timev1.ExecuteSelfClockActionResponse, error) {
		return &timev1.ExecuteSelfClockActionResponse{ReceiptId: "not-a-uuid", WorkflowInstanceRef: "bad", PublishedPlanRef: "plan", WorkflowId: "hcmnext.workflows.time.clock_in_out", WorkflowTraceId: "trace", WorkflowNodeId: "commit_punch", WorkflowAttempt: 1, WorkflowInstanceVersion: 1, Status: &timev1.SelfClockProjection{Revision: 9}}, nil
	}}
	binding := newFakeBinding(fake)
	binding.mu.Lock()
	binding.revision = 8
	binding.mu.Unlock()
	if _, err := binding.ExecuteClockAction(context.Background(), "in", binding.ClockRevision()); err == nil || !strings.Contains(err.Error(), "incomplete workflow receipt") {
		t.Fatalf("forged receipt error=%v", err)
	}
	if fake.lastExec.GetExpectedRevision() != 8 || fake.lastExec.GetIdempotencyKey() == "" || fake.lastExec.GetAction() != timev1.ExecuteSelfClockActionRequest_ACTION_IN {
		t.Fatalf("request=%+v", fake.lastExec)
	}
	first := fake.lastExec.GetIdempotencyKey()
	_, _ = binding.ExecuteClockAction(context.Background(), "in", 8)
	if fake.lastExec.GetIdempotencyKey() != first {
		t.Fatal("a retry of the same action and revision must reuse its idempotency key")
	}
	_, _ = binding.ExecuteClockAction(context.Background(), "out", 8)
	if fake.lastExec.GetAction() != timev1.ExecuteSelfClockActionRequest_ACTION_OUT || fake.lastExec.GetIdempotencyKey() == first {
		t.Fatalf("clock out reused the clock-in key or action: %+v", fake.lastExec)
	}
	for _, action := range []string{"", "break", "IN"} {
		if _, err := binding.ExecuteClockAction(context.Background(), action, 8); err == nil {
			t.Fatalf("action %q was sent", action)
		}
	}
	if _, err := binding.ExecuteClockAction(context.Background(), "in", 0); err == nil {
		t.Fatal("an action without a revision was sent")
	}
}

func TestTodoTClockLiveBinding_SendsBreakActionsWithWorkflowReceipts(t *testing.T) {
	fake := &fakeWorkerClock{execute: func(_ context.Context, request *timev1.ExecuteSelfClockActionRequest) (*timev1.ExecuteSelfClockActionResponse, error) {
		return &timev1.ExecuteSelfClockActionResponse{ReceiptId: "00000000-0000-0000-0000-000000000001", WorkflowInstanceRef: "00000000-0000-0000-0000-000000000002", PublishedPlanRef: "plan", WorkflowId: "hcmnext.workflows.time.clock_in_out", WorkflowTraceId: "trace", WorkflowNodeId: "commit_punch", WorkflowAttempt: 1, WorkflowInstanceVersion: 1, Status: &timev1.SelfClockProjection{Revision: request.GetExpectedRevision() + 1}}, nil
	}}
	binding := newFakeBinding(fake)
	for _, tc := range []struct {
		action string
		wire   timev1.ExecuteSelfClockActionRequest_Action
	}{{"start_break", timev1.ExecuteSelfClockActionRequest_ACTION_START_BREAK}, {"end_break", timev1.ExecuteSelfClockActionRequest_ACTION_END_BREAK}} {
		result, err := binding.ExecuteClockAction(context.Background(), tc.action, 12)
		if err != nil || fake.lastExec.GetAction() != tc.wire || fake.lastExec.GetExpectedRevision() != 12 || result.WorkflowNodeID != "commit_punch" {
			t.Fatalf("action %s request=%+v result=%+v err=%v", tc.action, fake.lastExec, result, err)
		}
	}
}

func TestTodoTClockLiveBinding_ProjectionRefreshPreservesPendingActionRefusal(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	fake := &fakeWorkerClock{
		get: func(context.Context, *timev1.GetSelfClockRequest) (*timev1.GetSelfClockResponse, error) {
			return &timev1.GetSelfClockResponse{WorkerLabel: "Taylor", ScheduleLabel: "Day", StatusLabel: "Clocked in", LastEventLabel: "now", StatusCode: "CLOCKED_IN", Revision: 8}, nil
		},
		execute: func(context.Context, *timev1.ExecuteSelfClockActionRequest) (*timev1.ExecuteSelfClockActionResponse, error) {
			close(started)
			<-release
			return nil, status.Error(codes.FailedPrecondition, "break is not available")
		},
	}
	binding := newFakeBinding(fake)
	binding.revision = 8
	scheduler := taskmux.New(taskmux.Options{MaxRunning: 1})
	finished := make(chan error, 1)
	handle, err := binding.SubmitClockAction(context.Background(), scheduler, "start_break", func(_ clockActionWire, actionErr error) {
		finished <- actionErr
	})
	if err != nil {
		t.Fatal(err)
	}
	<-started
	if _, _, diagnostic := binding.ClockActionState(); diagnostic != "" {
		t.Fatalf("pending action diagnostic = %q, want none", diagnostic)
	}
	if _, err := binding.ReadClockProjection(context.Background()); err != nil {
		t.Fatal(err)
	}
	if busy, _, diagnostic := binding.ClockActionState(); !busy || diagnostic != "" {
		t.Fatalf("projection refresh changed pending action state: busy=%v diagnostic=%q", busy, diagnostic)
	}
	close(release)
	<-handle.Done()
	if actionErr := <-finished; actionErr == nil {
		t.Fatal("break refusal was not returned to completion callback")
	}
	if busy, _, diagnostic := binding.ClockActionState(); busy || diagnostic != "clock action could not be completed" {
		t.Fatalf("final action state busy=%v diagnostic=%q", busy, diagnostic)
	}
}
