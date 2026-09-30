package productclient

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	timev1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/time/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// ErrMissingPunchTransportUnavailable means the command transport was not
// composed. It is deliberately distinct from an RPC refusal.
var ErrMissingPunchTransportUnavailable = errors.New("productclient: missing-punch transport unavailable")

// MissingPunchTransport is the authenticated native gRPC command adapter for
// the missing-punch surface. Authentication and principal metadata belong to
// the context/connection supplied by the composition root; this adapter never
// accepts worker or tenant identity from the browser.
type MissingPunchTransport struct {
	ctx    context.Context
	client timev1.MissingPunchServiceClient
}

var _ productui.MissingPunchSubmitter = (*MissingPunchTransport)(nil)
var _ productui.MissingPunchDecider = (*MissingPunchTransport)(nil)

// NewMissingPunchTransport binds an authenticated gRPC client to the UI
// command ports. The context is retained because the current UI ports are
// synchronous; composition should invoke them from its existing async event
// path so a WASM event handler is not blocked.
func NewMissingPunchTransport(ctx context.Context, client timev1.MissingPunchServiceClient) *MissingPunchTransport {
	if ctx == nil {
		ctx = context.Background()
	}
	return &MissingPunchTransport{ctx: ctx, client: client}
}

// SubmitMissingPunch sends the worker's typed claimed occurrence through the
// generated MissingPunchService. The service resolves principal and tenant.
func (t *MissingPunchTransport) SubmitMissingPunch(req productui.MissingPunchSubmission) (productui.MissingPunchReceipt, error) {
	if t == nil || t.client == nil {
		return productui.MissingPunchReceipt{}, ErrMissingPunchTransportUnavailable
	}
	if err := validateSubmission(req); err != nil {
		return productui.MissingPunchReceipt{}, err
	}
	response, err := t.client.SubmitCorrection(t.ctx, &timev1.SubmitCorrectionRequest{
		SessionId: req.SessionID, ClaimedEventType: "CLOCK_OUT", ClaimedOccurredAt: timestamppb.New(req.ProposedOutAt.UTC()),
		Reason: req.Reason, IdempotencyKey: req.IdempotencyKey, ExpectedRevision: req.ExpectedRevision,
	})
	if err != nil {
		return productui.MissingPunchReceipt{}, err
	}
	return missingPunchReceipt(response.GetCorrection())
}

// DecideMissingPunch sends an independent supervisor decision through the
// generated MissingPunchService. Approval is represented only by the typed
// protobuf enum and cannot alter the original observation in the browser.
func (t *MissingPunchTransport) DecideMissingPunch(req productui.MissingPunchDecision) (productui.MissingPunchReceipt, error) {
	if t == nil || t.client == nil {
		return productui.MissingPunchReceipt{}, ErrMissingPunchTransportUnavailable
	}
	if err := validateDecision(req); err != nil {
		return productui.MissingPunchReceipt{}, err
	}
	decision := timev1.MissingPunchDecision_MISSING_PUNCH_DECISION_REJECTED
	if req.Approve {
		decision = timev1.MissingPunchDecision_MISSING_PUNCH_DECISION_APPROVED
	}
	response, err := t.client.ReviewCorrection(t.ctx, &timev1.ReviewCorrectionRequest{
		RequestId: req.RequestID, ExpectedRevision: req.ExpectedRevision, Decision: decision,
		Reason: req.DecisionNote, IdempotencyKey: req.IdempotencyKey,
	})
	if err != nil {
		return productui.MissingPunchReceipt{}, err
	}
	return missingPunchReceipt(response.GetCorrection())
}

func validateSubmission(req productui.MissingPunchSubmission) error {
	if strings.TrimSpace(req.SessionID) == "" || strings.TrimSpace(req.Reason) == "" || req.ProposedOutAt.IsZero() {
		return errors.New("productclient: missing-punch submission is incomplete")
	}
	if strings.TrimSpace(req.IdempotencyKey) == "" {
		return errors.New("productclient: missing-punch idempotency key is required")
	}
	return nil
}

func validateDecision(req productui.MissingPunchDecision) error {
	if strings.TrimSpace(req.RequestID) == "" || strings.TrimSpace(req.DecisionNote) == "" {
		return errors.New("productclient: missing-punch decision is incomplete")
	}
	if strings.TrimSpace(req.IdempotencyKey) == "" {
		return errors.New("productclient: missing-punch idempotency key is required")
	}
	return nil
}

func missingPunchReceipt(correction *timev1.MissingPunchCorrection) (productui.MissingPunchReceipt, error) {
	if correction == nil {
		return productui.MissingPunchReceipt{}, errors.New("productclient: missing-punch response omitted correction")
	}
	if _, err := uuid.Parse(strings.TrimSpace(correction.GetRequestId())); err != nil {
		return productui.MissingPunchReceipt{}, fmt.Errorf("productclient: invalid missing-punch request id: %w", err)
	}
	if correction.GetRevision() == 0 {
		return productui.MissingPunchReceipt{}, errors.New("productclient: missing-punch response omitted revision")
	}
	receipt := correction.GetWorkflowReceipt()
	if receipt == nil {
		return productui.MissingPunchReceipt{}, errors.New("productclient: missing-punch response omitted workflow receipt")
	}
	if err := validateWorkflowReceipt(receipt); err != nil {
		return productui.MissingPunchReceipt{}, err
	}
	return productui.MissingPunchReceipt{
		RequestID: correction.GetRequestId(), Status: correction.GetStatus(), Revision: correction.GetRevision(),
		WorkflowID: receipt.GetWorkflowId(), PlanDigest: receipt.GetWorkflowPlanDigest(), NodeID: receipt.GetWorkflowNodeId(),
		Attempt: int(receipt.GetWorkflowAttempt()), InstanceVersion: receipt.GetWorkflowInstanceVersion(),
		// A trace URI is not derivable from a receipt. Keep it empty rather than
		// manufacturing a fake href; the UI renders no link in that case.
	}, nil
}

func validateWorkflowReceipt(receipt *timev1.WorkflowTrackedReceipt) error {
	for name, value := range map[string]string{"receipt_id": receipt.GetReceiptId(), "workflow_instance_ref": receipt.GetWorkflowInstanceRef(), "workflow_trace_id": receipt.GetWorkflowTraceId()} {
		if _, err := uuid.Parse(strings.TrimSpace(value)); err != nil {
			return fmt.Errorf("productclient: invalid workflow %s: %w", name, err)
		}
	}
	if receipt.GetWorkflowId() != "hcmnext.workflows.time.fix_missing_punch" {
		return fmt.Errorf("productclient: unexpected workflow id %q", receipt.GetWorkflowId())
	}
	if strings.TrimSpace(receipt.GetWorkflowNodeId()) == "" || receipt.GetWorkflowAttempt() < 1 || receipt.GetWorkflowInstanceVersion() < 1 || strings.TrimSpace(receipt.GetWorkflowPlanDigest()) == "" {
		return errors.New("productclient: incomplete workflow receipt")
	}
	return nil
}
