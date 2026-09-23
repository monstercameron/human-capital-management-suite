package workflow

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/google/uuid"

	workflowv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/workflow/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/operator"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/operator/workflowcontrol"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/transporttest"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type fakeControl struct {
	mu   sync.Mutex
	reqs []workflowcontrol.Request
	res  workflowcontrol.Response
	err  error
}

func (f *fakeControl) Handle(_ context.Context, ids workflowcontrol.TenantIDs, req workflowcontrol.Request) (workflowcontrol.Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, err := ids(req.Tenant); err != nil {
		return workflowcontrol.Response{}, err
	}
	f.reqs = append(f.reqs, req)
	return f.res, f.err
}

func tenantIDs(values.TenantId) (uuid.UUID, error) { return uuid.New(), nil }

// TestTodo_EP_WF_002_Integration drives the four RPCs through the transport
// with a trusted principal: tenant and operator come from the principal, never
// from the request, and each governed outcome projects onto the wire receipt
// together with the instance (or retried node) read back afterwards.
func TestTodo_EP_WF_002_TransportIntegration(t *testing.T) {
	reader := &workflowTestReader{record: Record{
		Instance: Instance{InstanceID: "workflow-1", TenantID: transporttest.Tenant, RuntimeStatus: "PAUSED", InstanceVersion: 9},
		Nodes:    []NodeExecution{{NodeExecutionID: "n-2", WorkflowInstanceID: "workflow-1", NodeID: "execute_promotion", Attempt: 2, Status: "READY"}},
	}}
	ctl := &fakeControl{res: workflowcontrol.Response{Outcome: workflowcontrol.OutcomeApplied, IntentInstanceID: "intent:operator:x",
		ReceiptDigest: "sha256:r", InstanceVersion: 9, InstanceStatus: "PAUSED", NodeID: "execute_promotion", Attempt: 2}}
	srv := &server{deps: Dependencies{Instances: reader, Control: ctl, TenantIDs: tenantIDs, Authorize: allowWorkflowCalls}}

	pause, err := srv.PauseWorkflow(workflowTestContext(t, PauseWorkflowProcedure), &workflowv1.PauseWorkflowRequest{
		IdempotencyKey: "k1", InstanceId: "workflow-1", ExpectedInstanceVersion: 8, ReasonRef: "INC-1"})
	if err != nil {
		t.Fatalf("PauseWorkflow: %v", err)
	}
	if pause.GetReceipt().GetOutcome() != workflowv1.WorkflowControlOutcome_WORKFLOW_CONTROL_OUTCOME_APPLIED ||
		pause.GetReceipt().GetIntentInstanceId() != "intent:operator:x" || pause.GetInstance().GetInstanceVersion() != 9 {
		t.Fatalf("pause response = %v", pause)
	}
	got := ctl.reqs[0]
	if got.Kind != operator.KindWorkflowPause || string(got.Tenant) != transporttest.Tenant || got.Operator == "" || got.ExpectedVersion != 8 {
		t.Fatalf("control request = %+v, want tenant and operator from the principal", got)
	}
	if _, err := srv.ResumeWorkflow(workflowTestContext(t, ResumeWorkflowProcedure), &workflowv1.ResumeWorkflowRequest{
		IdempotencyKey: "k2", InstanceId: "workflow-1", ExpectedInstanceVersion: 9, ReasonRef: "INC-1"}); err != nil || ctl.reqs[1].Kind != operator.KindWorkflowResume {
		t.Fatalf("ResumeWorkflow: %v", err)
	}
	if _, err := srv.CancelWorkflow(workflowTestContext(t, CancelWorkflowProcedure), &workflowv1.CancelWorkflowRequest{
		IdempotencyKey: "k3", InstanceId: "workflow-1", ExpectedInstanceVersion: 9, ReasonRef: "INC-1"}); err != nil || ctl.reqs[2].Kind != operator.KindWorkflowCancel {
		t.Fatalf("CancelWorkflow: %v", err)
	}
	retry, err := srv.RetryNode(workflowTestContext(t, RetryNodeProcedure), &workflowv1.RetryNodeRequest{
		IdempotencyKey: "k4", InstanceId: "workflow-1", NodeId: "execute_promotion", ExpectedAttempt: 1, ReasonRef: "INC-1"})
	if err != nil || ctl.reqs[3].Kind != operator.KindWorkflowRetryNode || retry.GetNodeExecution().GetAttempt() != 2 {
		t.Fatalf("RetryNode = %v, %v", retry, err)
	}
	if ctl.reqs[3].ReasonRef != "INC-1" || ctl.reqs[3].IdempotencyKey != "k4" {
		t.Fatalf("retry control request = %+v, want the caller's reason, not the idempotency key", ctl.reqs[3])
	}

	for outcome, want := range map[workflowcontrol.Outcome]workflowv1.WorkflowControlOutcome{
		workflowcontrol.OutcomePendingSafePoint: workflowv1.WorkflowControlOutcome_WORKFLOW_CONTROL_OUTCOME_PENDING_SAFE_POINT,
		workflowcontrol.OutcomeDenied:           workflowv1.WorkflowControlOutcome_WORKFLOW_CONTROL_OUTCOME_DENIED,
		workflowcontrol.OutcomeTooLate:          workflowv1.WorkflowControlOutcome_WORKFLOW_CONTROL_OUTCOME_TOO_LATE,
		workflowcontrol.OutcomeRepairRequired:   workflowv1.WorkflowControlOutcome_WORKFLOW_CONTROL_OUTCOME_REPAIR_REQUIRED,
		"BOGUS":                                 workflowv1.WorkflowControlOutcome_WORKFLOW_CONTROL_OUTCOME_UNSPECIFIED,
	} {
		if controlOutcome(outcome) != want {
			t.Errorf("outcome %s projects to %v", outcome, controlOutcome(outcome))
		}
	}

	// Without an instance reader the receipt still returns.
	bare := &server{deps: Dependencies{Control: ctl, TenantIDs: tenantIDs, Authorize: allowWorkflowCalls}}
	if res, err := bare.CancelWorkflow(workflowTestContext(t, CancelWorkflowProcedure), &workflowv1.CancelWorkflowRequest{
		IdempotencyKey: "k5", InstanceId: "workflow-1", ExpectedInstanceVersion: 9, ReasonRef: "r"}); err != nil || res.GetInstance() != nil || res.GetReceipt() == nil {
		t.Fatalf("bare cancel = %v, %v", res, err)
	}
	// An unreadable instance projection does not turn an outcome into an error.
	unreadable := &server{deps: Dependencies{Instances: &workflowTestReader{}, Control: ctl, TenantIDs: tenantIDs, Authorize: allowWorkflowCalls}}
	if res, err := unreadable.PauseWorkflow(workflowTestContext(t, PauseWorkflowProcedure), &workflowv1.PauseWorkflowRequest{
		IdempotencyKey: "k6", InstanceId: "workflow-1", ExpectedInstanceVersion: 9, ReasonRef: "r"}); err != nil || res.GetReceipt() == nil {
		t.Fatalf("unreadable projection = %v, %v", res, err)
	}

	// The connect handler exposes every control procedure.
	h := NewHandler(Dependencies{Control: ctl, TenantIDs: tenantIDs})
	for _, proc := range []string{PauseWorkflowProcedure, ResumeWorkflowProcedure, CancelWorkflowProcedure, RetryNodeProcedure} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, proc, nil))
		if rec.Code == http.StatusNotFound {
			t.Errorf("%s is not routed", proc)
		}
	}
}

// TestTodo_EP_WF_002_TransportSecurity refuses unauthenticated, unauthorized,
// malformed and ungoverned controls before any control runs.
func TestTodo_EP_WF_002_TransportSecurity(t *testing.T) {
	ctl := &fakeControl{}
	deny := &server{deps: Dependencies{Control: ctl, TenantIDs: tenantIDs, Authorize: func(context.Context, *trust.Principal, string) bool { return false }}}
	_, err := deny.PauseWorkflow(workflowTestContext(t, PauseWorkflowProcedure), &workflowv1.PauseWorkflowRequest{
		IdempotencyKey: "k", InstanceId: "workflow-1", ExpectedInstanceVersion: 1, ReasonRef: "r"})
	assertCode(t, "unauthorized", err, envelope.CodePermissionDenied)

	if _, err := deny.PauseWorkflow(context.Background(), &workflowv1.PauseWorkflowRequest{}); err == nil {
		t.Fatal("an unauthenticated control was accepted")
	}

	srv := &server{deps: Dependencies{Control: ctl, TenantIDs: tenantIDs}}
	for name, call := range map[string]func() error{
		"no instance": func() error {
			_, err := srv.PauseWorkflow(workflowTestContext(t, PauseWorkflowProcedure), &workflowv1.PauseWorkflowRequest{IdempotencyKey: "k", ExpectedInstanceVersion: 1, ReasonRef: "r"})
			return err
		},
		"no key": func() error {
			_, err := srv.ResumeWorkflow(workflowTestContext(t, ResumeWorkflowProcedure), &workflowv1.ResumeWorkflowRequest{InstanceId: "w", ExpectedInstanceVersion: 1, ReasonRef: "r"})
			return err
		},
		"no version": func() error {
			_, err := srv.CancelWorkflow(workflowTestContext(t, CancelWorkflowProcedure), &workflowv1.CancelWorkflowRequest{InstanceId: "w", IdempotencyKey: "k", ReasonRef: "r"})
			return err
		},
		"no reason": func() error {
			_, err := srv.CancelWorkflow(workflowTestContext(t, CancelWorkflowProcedure), &workflowv1.CancelWorkflowRequest{InstanceId: "w", IdempotencyKey: "k", ExpectedInstanceVersion: 1})
			return err
		},
		"no node": func() error {
			_, err := srv.RetryNode(workflowTestContext(t, RetryNodeProcedure), &workflowv1.RetryNodeRequest{InstanceId: "w", IdempotencyKey: "k", ExpectedAttempt: 1, ReasonRef: "r"})
			return err
		},
		"no attempt": func() error {
			_, err := srv.RetryNode(workflowTestContext(t, RetryNodeProcedure), &workflowv1.RetryNodeRequest{InstanceId: "w", IdempotencyKey: "k", NodeId: "n", ReasonRef: "r"})
			return err
		},
		"no retry reason": func() error {
			_, err := srv.RetryNode(workflowTestContext(t, RetryNodeProcedure), &workflowv1.RetryNodeRequest{InstanceId: "w", IdempotencyKey: "k", NodeId: "n", ExpectedAttempt: 1})
			return err
		},
	} {
		assertCode(t, name, call(), envelope.CodeInvalidArgument)
	}
	if len(ctl.reqs) != 0 {
		t.Fatalf("refused controls reached the controller %d times", len(ctl.reqs))
	}

	// An unwired service refuses before it checks governance wiring:
	// authorization runs before availability, so a missing hook denies
	// rather than reporting the unwired control surface.
	ungoverned := &server{deps: Dependencies{}}
	_, err = ungoverned.CancelWorkflow(workflowTestContext(t, CancelWorkflowProcedure), &workflowv1.CancelWorkflowRequest{
		InstanceId: "w", IdempotencyKey: "k", ExpectedInstanceVersion: 1, ReasonRef: "r"})
	assertCode(t, "ungoverned", err, envelope.CodePermissionDenied)

	for name, tc := range map[string]struct {
		err  error
		code envelope.Code
	}{
		"invalid command": {workflowcontrol.ErrInvalidCommand, envelope.CodeInvalidArgument},
		"dependency down": {errors.New("db down"), envelope.CodeUnavailable},
	} {
		failing := &server{deps: Dependencies{Control: &fakeControl{err: tc.err}, TenantIDs: tenantIDs, Authorize: allowWorkflowCalls}}
		_, err := failing.PauseWorkflow(workflowTestContext(t, PauseWorkflowProcedure), &workflowv1.PauseWorkflowRequest{
			InstanceId: "w", IdempotencyKey: "k", ExpectedInstanceVersion: 1, ReasonRef: "r"})
		assertCode(t, name, err, tc.code)
	}
	if controlDenied(nil, nil) == nil || controlUnavailable(nil, nil) == nil || projectControlError(errors.New("x"), nil, nil) == nil {
		t.Fatal("envelope constructors must tolerate a missing invocation or principal")
	}
}

// TestRetryNodeRecordsCallerReason is INTAPI-006's RED for the RetryNode
// audit defect: the transport sent the idempotency key as the governed
// ReasonRef, so every retry receipt cited the deduplication key instead of
// the caller's ticket, and a retry without a reason passed validation the
// controller itself would refuse.
func TestRetryNodeRecordsCallerReason(t *testing.T) {
	reader := &workflowTestReader{record: Record{
		Instance: Instance{InstanceID: "workflow-1", TenantID: transporttest.Tenant, RuntimeStatus: "PAUSED", InstanceVersion: 9},
		Nodes:    []NodeExecution{{NodeExecutionID: "n-2", WorkflowInstanceID: "workflow-1", NodeID: "execute_promotion", Attempt: 2, Status: "READY"}},
	}}
	ctl := &fakeControl{res: workflowcontrol.Response{Outcome: workflowcontrol.OutcomeApplied, IntentInstanceID: "intent:operator:x",
		ReceiptDigest: "sha256:r", InstanceVersion: 9, InstanceStatus: "PAUSED", NodeID: "execute_promotion", Attempt: 2}}
	srv := &server{deps: Dependencies{Instances: reader, Control: ctl, TenantIDs: tenantIDs, Authorize: allowWorkflowCalls}}

	if _, err := srv.RetryNode(workflowTestContext(t, RetryNodeProcedure), &workflowv1.RetryNodeRequest{
		IdempotencyKey: "k4", InstanceId: "workflow-1", NodeId: "execute_promotion", ExpectedAttempt: 1, ReasonRef: "INC-9"}); err != nil {
		t.Fatalf("RetryNode: %v", err)
	}
	got := ctl.reqs[0]
	if got.IdempotencyKey != "k4" {
		t.Fatalf("idempotency key = %q, want k4", got.IdempotencyKey)
	}
	if got.ReasonRef != "INC-9" {
		t.Fatalf("ReasonRef = %q, want the caller's reason INC-9", got.ReasonRef)
	}

	if _, err := srv.RetryNode(workflowTestContext(t, RetryNodeProcedure), &workflowv1.RetryNodeRequest{
		IdempotencyKey: "k5", InstanceId: "workflow-1", NodeId: "execute_promotion", ExpectedAttempt: 1}); err == nil {
		t.Fatal("a retry without a reason_ref was accepted")
	} else {
		assertCode(t, "retry without reason", err, envelope.CodeInvalidArgument)
	}
	if len(ctl.reqs) != 1 {
		t.Fatalf("refused retry reached the controller %d times", len(ctl.reqs))
	}
}

func assertCode(t *testing.T, name string, err error, want envelope.Code) {
	t.Helper()
	owned, ok := envelope.As(err)
	if !ok || owned.Code() != want {
		t.Errorf("%s: error = %v, want %v", name, err, want)
	}
}
