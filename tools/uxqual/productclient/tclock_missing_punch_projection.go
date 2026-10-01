package productclient

import (
	"context"
	"crypto/rand"
	"encoding/hex"
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
	if strings.TrimSpace(req.SessionID) == "" || strings.TrimSpace(req.Reason) == "" || req.ProposedOutAt.IsZero() || req.ExpectedRevision == 0 {
		return errors.New("productclient: missing-punch submission is incomplete")
	}
	if strings.TrimSpace(req.IdempotencyKey) == "" {
		return errors.New("productclient: missing-punch idempotency key is required")
	}
	return nil
}

func validateDecision(req productui.MissingPunchDecision) error {
	if strings.TrimSpace(req.RequestID) == "" || strings.TrimSpace(req.DecisionNote) == "" || req.ExpectedRevision == 0 {
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
	if parsed, err := uuid.Parse(strings.TrimSpace(correction.GetRequestId())); err != nil || parsed == uuid.Nil {
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
		WorkflowTraceID: receipt.GetWorkflowTraceId(), WorkflowInstanceRef: receipt.GetWorkflowInstanceRef(),
		Attempt: int(receipt.GetWorkflowAttempt()), InstanceVersion: receipt.GetWorkflowInstanceVersion(),
		// A trace URI is not derivable from a receipt. Keep it empty rather than
		// manufacturing a fake href; the UI renders no link in that case.
	}, nil
}

// LoadMissingPunchProjection reads the authenticated correction context and
// supervisor queue through native gRPC. It copies only facts published by the
// server; missing observation IDs remain empty until the wire contract names
// one rather than being guessed from another workflow identifier.
func (t *MissingPunchTransport) LoadMissingPunchProjection(ctx context.Context, sessionID string) (productui.MissingPunchAdminProjection, error) {
	if t == nil || t.client == nil {
		return productui.MissingPunchAdminProjection{}, ErrMissingPunchTransportUnavailable
	}
	if ctx == nil {
		ctx = t.ctx
	}
	if strings.TrimSpace(sessionID) == "" {
		return productui.MissingPunchAdminProjection{}, errors.New("productclient: missing-punch session id is required")
	}
	response, err := t.client.GetCorrectionContext(ctx, &timev1.GetCorrectionContextRequest{SessionId: sessionID})
	if err != nil {
		return productui.MissingPunchAdminProjection{}, err
	}
	projection := productui.MissingPunchAdminProjection{State: productui.MissingPunchReady, Submitter: t, Decider: t}
	if err := projectCorrectionContext(response.GetContext(), sessionID, &projection); err != nil {
		return productui.MissingPunchAdminProjection{}, err
	}
	for _, correction := range response.GetPendingCorrections() {
		review, err := projectReview(correction)
		if err != nil {
			return productui.MissingPunchAdminProjection{}, err
		}
		projection.Pending = append(projection.Pending, review)
	}
	queue, err := t.client.ListPendingCorrections(ctx, &timev1.ListPendingCorrectionsRequest{PageSize: 100})
	if err != nil {
		return productui.MissingPunchAdminProjection{}, err
	}
	seen := make(map[string]struct{}, len(projection.Pending))
	for _, review := range projection.Pending {
		seen[review.RequestID] = struct{}{}
	}
	for _, correction := range queue.GetCorrections() {
		review, err := projectReview(correction)
		if err != nil {
			return productui.MissingPunchAdminProjection{}, err
		}
		if _, exists := seen[review.RequestID]; exists {
			continue
		}
		projection.Pending = append(projection.Pending, review)
		seen[review.RequestID] = struct{}{}
	}
	return projection, nil
}

func projectCorrectionContext(contextWire *timev1.CorrectionContext, requestedSession string, projection *productui.MissingPunchAdminProjection) error {
	if contextWire == nil || strings.TrimSpace(contextWire.GetSessionId()) != strings.TrimSpace(requestedSession) || strings.TrimSpace(contextWire.GetWorkerRef()) == "" || contextWire.GetRevision() == 0 {
		return errors.New("productclient: incomplete missing-punch correction context")
	}
	fact := contextWire.GetOriginalOut()
	if fact == nil {
		fact = contextWire.GetOriginalIn()
	}
	if fact == nil || strings.TrimSpace(fact.GetObservationId()) == "" || fact.GetOccurredAt() == nil || fact.GetOccurredAt().CheckValid() != nil || strings.TrimSpace(fact.GetEventType()) == "" {
		return errors.New("productclient: correction context omitted original punch fact")
	}
	key, err := newIdempotencyKey()
	if err != nil {
		return err
	}
	projection.Session = productui.MissingPunchSessionView{
		SessionID: contextWire.GetSessionId(), WorkerRef: contextWire.GetWorkerRef(), OriginalObservationID: fact.GetObservationId(), WorkerLabel: contextWire.GetWorkerRef(),
		SessionLabel: contextWire.GetPeriodRef(), OriginalEventLabel: fact.GetEventType(), WorkerTimezone: contextWire.GetTimezone(),
		OriginalAt: fact.GetOccurredAt().AsTime().UTC(), ExpectedRevision: contextWire.GetRevision(), PeriodClosed: contextWire.GetPeriodClosed(), IdempotencyKey: key,
	}
	return nil
}

func projectReview(correction *timev1.MissingPunchCorrection) (productui.MissingPunchReviewView, error) {
	if correction == nil || strings.TrimSpace(correction.GetWorkerRef()) == "" || correction.GetRevision() == 0 || correction.GetClaimedOccurredAt() == nil || correction.GetClaimedOccurredAt().CheckValid() != nil {
		return productui.MissingPunchReviewView{}, errors.New("productclient: incomplete missing-punch review projection")
	}
	if _, err := uuid.Parse(strings.TrimSpace(correction.GetRequestId())); err != nil {
		return productui.MissingPunchReviewView{}, fmt.Errorf("productclient: invalid pending correction id: %w", err)
	}
	key, err := newIdempotencyKey()
	if err != nil {
		return productui.MissingPunchReviewView{}, err
	}
	return productui.MissingPunchReviewView{
		RequestID: correction.GetRequestId(), WorkerRef: correction.GetWorkerRef(), WorkerLabel: correction.GetWorkerRef(),
		SessionLabel: correction.GetSessionId(), OriginalEventLabel: correction.GetClaimedEventType(), ProposedOutAt: correction.GetClaimedOccurredAt().AsTime().UTC(),
		Reason: correction.GetRequestReason(), RequestedBy: correction.GetWorkerRef(), Revision: correction.GetRevision(), IdempotencyKey: key,
	}, nil
}

func newIdempotencyKey() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("productclient: generate missing-punch idempotency key: %w", err)
	}
	return uuid.UUID(raw).String(), nil
}

func validateWorkflowReceipt(receipt *timev1.WorkflowTrackedReceipt) error {
	for name, value := range map[string]string{"receipt_id": receipt.GetReceiptId(), "workflow_instance_ref": receipt.GetWorkflowInstanceRef()} {
		if parsed, err := uuid.Parse(strings.TrimSpace(value)); err != nil || parsed == uuid.Nil {
			return fmt.Errorf("productclient: invalid workflow %s: %w", name, err)
		}
	}
	traceID := strings.TrimSpace(receipt.GetWorkflowTraceId())
	if len(traceID) != 32 {
		return fmt.Errorf("productclient: invalid workflow trace id %q", traceID)
	}
	if _, err := hex.DecodeString(traceID); err != nil {
		return fmt.Errorf("productclient: invalid workflow trace id: %w", err)
	}
	if receipt.GetWorkflowId() != "hcmnext.workflows.time.fix_missing_punch" {
		return fmt.Errorf("productclient: unexpected workflow id %q", receipt.GetWorkflowId())
	}
	if strings.TrimSpace(receipt.GetWorkflowNodeId()) == "" || receipt.GetWorkflowAttempt() < 1 || receipt.GetWorkflowInstanceVersion() < 1 || strings.TrimSpace(receipt.GetWorkflowPlanDigest()) == "" {
		return errors.New("productclient: incomplete workflow receipt")
	}
	switch strings.ToUpper(strings.TrimSpace(receipt.GetWorkflowNodeId())) {
	case "COMMIT_MISSING_PUNCH_REQUEST", "APPEND_CORRECTION", "APPROVED", "REJECTED":
	default:
		return fmt.Errorf("productclient: unexpected workflow node %q", receipt.GetWorkflowNodeId())
	}
	return nil
}
