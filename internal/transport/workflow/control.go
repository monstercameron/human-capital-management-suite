package workflow

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"connectrpc.com/connect"

	workflowv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/workflow/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/operator"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/operator/workflowcontrol"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// Workflow control procedures and authorization actions (EP-WF-002).
const (
	PauseWorkflowProcedure  = "/hcmnext.workflow.v1.WorkflowService/PauseWorkflow"
	ResumeWorkflowProcedure = "/hcmnext.workflow.v1.WorkflowService/ResumeWorkflow"
	CancelWorkflowProcedure = "/hcmnext.workflow.v1.WorkflowService/CancelWorkflow"
	RetryNodeProcedure      = "/hcmnext.workflow.v1.WorkflowService/RetryNode"
	ActionPauseWorkflow     = "pause_workflow"
	ActionResumeWorkflow    = "resume_workflow"
	ActionCancelWorkflow    = "cancel_workflow"
	ActionRetryNode         = "retry_node"
)

// ControlHandler runs one governed workflow control. It is satisfied by
// *workflowcontrol.Controller.
type ControlHandler interface {
	Handle(ctx context.Context, tenantIDs workflowcontrol.TenantIDs, req workflowcontrol.Request) (workflowcontrol.Response, error)
}

// registerControlHandlers adds the four control procedures to mux.
func registerControlHandlers(mux *http.ServeMux, s *server, opts ...connect.HandlerOption) {
	mux.Handle(PauseWorkflowProcedure, connect.NewUnaryHandler(PauseWorkflowProcedure, func(ctx context.Context, req *connect.Request[workflowv1.PauseWorkflowRequest]) (*connect.Response[workflowv1.PauseWorkflowResponse], error) {
		res, err := s.PauseWorkflow(ctx, req.Msg)
		if err != nil {
			return nil, err
		}
		return connect.NewResponse(res), nil
	}, opts...))
	mux.Handle(ResumeWorkflowProcedure, connect.NewUnaryHandler(ResumeWorkflowProcedure, func(ctx context.Context, req *connect.Request[workflowv1.ResumeWorkflowRequest]) (*connect.Response[workflowv1.ResumeWorkflowResponse], error) {
		res, err := s.ResumeWorkflow(ctx, req.Msg)
		if err != nil {
			return nil, err
		}
		return connect.NewResponse(res), nil
	}, opts...))
	mux.Handle(CancelWorkflowProcedure, connect.NewUnaryHandler(CancelWorkflowProcedure, func(ctx context.Context, req *connect.Request[workflowv1.CancelWorkflowRequest]) (*connect.Response[workflowv1.CancelWorkflowResponse], error) {
		res, err := s.CancelWorkflow(ctx, req.Msg)
		if err != nil {
			return nil, err
		}
		return connect.NewResponse(res), nil
	}, opts...))
	mux.Handle(RetryNodeProcedure, connect.NewUnaryHandler(RetryNodeProcedure, func(ctx context.Context, req *connect.Request[workflowv1.RetryNodeRequest]) (*connect.Response[workflowv1.RetryNodeResponse], error) {
		res, err := s.RetryNode(ctx, req.Msg)
		if err != nil {
			return nil, err
		}
		return connect.NewResponse(res), nil
	}, opts...))
}

func (s *server) PauseWorkflow(ctx context.Context, req *workflowv1.PauseWorkflowRequest) (*workflowv1.PauseWorkflowResponse, error) {
	receipt, instance, _, err := s.control(ctx, ActionPauseWorkflow, workflowcontrol.Request{
		Kind: operator.KindWorkflowPause, InstanceID: req.GetInstanceId(), ExpectedVersion: req.GetExpectedInstanceVersion(),
		IdempotencyKey: req.GetIdempotencyKey(), ReasonRef: req.GetReasonRef(),
	})
	if err != nil {
		return nil, err
	}
	return &workflowv1.PauseWorkflowResponse{Instance: instance, Receipt: receipt}, nil
}

func (s *server) ResumeWorkflow(ctx context.Context, req *workflowv1.ResumeWorkflowRequest) (*workflowv1.ResumeWorkflowResponse, error) {
	receipt, instance, _, err := s.control(ctx, ActionResumeWorkflow, workflowcontrol.Request{
		Kind: operator.KindWorkflowResume, InstanceID: req.GetInstanceId(), ExpectedVersion: req.GetExpectedInstanceVersion(),
		IdempotencyKey: req.GetIdempotencyKey(), ReasonRef: req.GetReasonRef(),
	})
	if err != nil {
		return nil, err
	}
	return &workflowv1.ResumeWorkflowResponse{Instance: instance, Receipt: receipt}, nil
}

func (s *server) CancelWorkflow(ctx context.Context, req *workflowv1.CancelWorkflowRequest) (*workflowv1.CancelWorkflowResponse, error) {
	receipt, instance, _, err := s.control(ctx, ActionCancelWorkflow, workflowcontrol.Request{
		Kind: operator.KindWorkflowCancel, InstanceID: req.GetInstanceId(), ExpectedVersion: req.GetExpectedInstanceVersion(),
		IdempotencyKey: req.GetIdempotencyKey(), ReasonRef: req.GetReasonRef(),
	})
	if err != nil {
		return nil, err
	}
	return &workflowv1.CancelWorkflowResponse{Instance: instance, Receipt: receipt}, nil
}

func (s *server) RetryNode(ctx context.Context, req *workflowv1.RetryNodeRequest) (*workflowv1.RetryNodeResponse, error) {
	receipt, _, node, err := s.control(ctx, ActionRetryNode, workflowcontrol.Request{
		Kind: operator.KindWorkflowRetryNode, InstanceID: req.GetInstanceId(), NodeID: req.GetNodeId(),
		ExpectedAttempt: req.GetExpectedAttempt(), IdempotencyKey: req.GetIdempotencyKey(), ReasonRef: req.GetIdempotencyKey(),
	})
	if err != nil {
		return nil, err
	}
	return &workflowv1.RetryNodeResponse{NodeExecution: node, Receipt: receipt}, nil
}

// control authenticates, authorizes and validates one control, runs it
// through the governed controller and projects the receipt plus the
// instance (or retried node) read back afterwards.
func (s *server) control(ctx context.Context, action string, req workflowcontrol.Request) (*workflowv1.WorkflowControlReceipt, *workflowv1.WorkflowInstance, *workflowv1.NodeExecution, error) {
	p, inv, err := trustedContext(ctx)
	if err != nil {
		return nil, nil, nil, err
	}
	switch {
	case strings.TrimSpace(req.InstanceID) == "":
		return nil, nil, nil, invalid(inv, "instance_id")
	case strings.TrimSpace(req.IdempotencyKey) == "":
		return nil, nil, nil, invalid(inv, "idempotency_key")
	case req.Kind == operator.KindWorkflowRetryNode && strings.TrimSpace(req.NodeID) == "":
		return nil, nil, nil, invalid(inv, "node_id")
	case req.Kind == operator.KindWorkflowRetryNode && req.ExpectedAttempt == 0:
		return nil, nil, nil, invalid(inv, "expected_attempt")
	case req.Kind != operator.KindWorkflowRetryNode && req.ExpectedVersion == 0:
		return nil, nil, nil, invalid(inv, "expected_instance_version")
	case req.Kind != operator.KindWorkflowRetryNode && strings.TrimSpace(req.ReasonRef) == "":
		return nil, nil, nil, invalid(inv, "reason_ref")
	}
	if !s.authorized(p, action) {
		return nil, nil, nil, controlDenied(inv, p)
	}
	if s.deps.Control == nil || s.deps.TenantIDs == nil {
		return nil, nil, nil, controlUnavailable(inv, p)
	}
	req.Tenant, req.Operator = p.Tenant(), p.Subject()
	res, runErr := s.deps.Control.Handle(ctx, s.deps.TenantIDs, req)
	if runErr != nil {
		return nil, nil, nil, projectControlError(runErr, inv, p)
	}
	receipt := &workflowv1.WorkflowControlReceipt{
		Outcome: controlOutcome(res.Outcome), ResultCode: res.Code, IntentInstanceId: res.IntentInstanceID,
		ReceiptDigest: res.ReceiptDigest, Replayed: res.Replayed, InstanceVersion: res.InstanceVersion,
		InstanceStatus: res.InstanceStatus,
	}
	if s.deps.Instances == nil {
		return receipt, nil, nil, nil
	}
	record, readErr := s.read(ctx, p.Tenant().String(), req.InstanceID)
	if readErr != nil || validateRecord(record, p.Tenant().String(), req.InstanceID) != nil {
		// The control is committed; a projection that cannot be read back is
		// omitted rather than turning a governed outcome into an error.
		return receipt, nil, nil, nil
	}
	var node *workflowv1.NodeExecution
	for _, n := range record.Nodes {
		if n.NodeID == req.NodeID && res.Attempt > 0 && n.Attempt == res.Attempt {
			node = ProjectNodeExecution(n)
		}
	}
	return receipt, ProjectInstance(record.Instance), node, nil
}

func controlOutcome(o workflowcontrol.Outcome) workflowv1.WorkflowControlOutcome {
	switch o {
	case workflowcontrol.OutcomeApplied:
		return workflowv1.WorkflowControlOutcome_WORKFLOW_CONTROL_OUTCOME_APPLIED
	case workflowcontrol.OutcomePendingSafePoint:
		return workflowv1.WorkflowControlOutcome_WORKFLOW_CONTROL_OUTCOME_PENDING_SAFE_POINT
	case workflowcontrol.OutcomeDenied:
		return workflowv1.WorkflowControlOutcome_WORKFLOW_CONTROL_OUTCOME_DENIED
	case workflowcontrol.OutcomeTooLate:
		return workflowv1.WorkflowControlOutcome_WORKFLOW_CONTROL_OUTCOME_TOO_LATE
	case workflowcontrol.OutcomeRepairRequired:
		return workflowv1.WorkflowControlOutcome_WORKFLOW_CONTROL_OUTCOME_REPAIR_REQUIRED
	default:
		return workflowv1.WorkflowControlOutcome_WORKFLOW_CONTROL_OUTCOME_UNSPECIFIED
	}
}

func controlDenied(inv *transport.Invocation, p *trust.Principal) *envelope.Error {
	err := envelope.New(envelope.CodePermissionDenied, "workflow.control_denied", "the caller is not authorized to control this workflow")
	if inv != nil {
		err.WithCorrelation(inv.RequestID())
	}
	if p != nil {
		err.WithEvidence(envelope.Evidence{ID: p.EvidenceID(), Kind: "authentication"})
	}
	return err
}

func controlUnavailable(inv *transport.Invocation, p *trust.Principal) *envelope.Error {
	err := envelope.New(envelope.CodeFailedPrecondition, "workflow.control_not_governed",
		"workflow controls require governed operator wiring; no transition was performed")
	if inv != nil {
		err.WithCorrelation(inv.RequestID())
	}
	if p != nil {
		err.WithEvidence(envelope.Evidence{ID: p.EvidenceID(), Kind: "authentication"})
	}
	return err
}

func projectControlError(err error, inv *transport.Invocation, p *trust.Principal) *envelope.Error {
	code, reason, message := envelope.CodeUnavailable, "workflow.control_unavailable", "the workflow control could not be completed"
	if errors.Is(err, workflowcontrol.ErrInvalidCommand) {
		code, reason, message = envelope.CodeInvalidArgument, "workflow.invalid_request", "the request is invalid"
	}
	out := envelope.New(code, reason, message).WithDiagnostic(err)
	if inv != nil {
		out.WithCorrelation(inv.RequestID())
	}
	if p != nil {
		out.WithEvidence(envelope.Evidence{ID: p.EvidenceID(), Kind: "authentication"})
	}
	return out
}
