package clockservice

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/punchpolicy"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// TipAdjustmentRequest is a later, append-only correction to a worker's tip
// statement. DeclarationObservationID links the correction to the immutable
// clock-out evidence; the original declaration is never edited.
type TipAdjustmentRequest struct {
	WorkerRef                string
	AssignmentRef            string
	DeclarationObservationID string
	Amount                   string
	Reason                   string
	IdempotencyKey           string
}

type tipAdjustmentEvidence struct {
	SchemaVersion            int       `json:"schema_version"`
	DeclarationObservationID string    `json:"declaration_observation_id"`
	WorkerID                 string    `json:"worker_id"`
	AdjustedBy               string    `json:"adjusted_by"`
	AdjustedAt               time.Time `json:"adjusted_at"`
	Amount                   string    `json:"amount"`
	Reason                   string    `json:"reason"`
}

func newTipDeclarationEvidence(receipt PunchReceipt, amount string, at time.Time) (*TipDeclarationEvidence, error) {
	declared, err := values.NewDecimal(amount, 2, values.RoundingHalfEven)
	if err != nil {
		return nil, err
	}
	record, err := punchpolicy.NewTipDeclaration(punchpolicy.TipDeclaration{
		WorkerID: receipt.WorkerRef, ShiftID: receipt.SessionID, DeclaredBy: receipt.WorkerRef,
		DeclaredAt: at, Amount: declared,
	})
	if err != nil {
		return nil, err
	}
	return &TipDeclarationEvidence{WorkerID: record.Declaration.WorkerID, ShiftID: record.Declaration.ShiftID, DeclaredBy: record.Declaration.DeclaredBy, DeclaredAt: record.Declaration.DeclaredAt, Amount: record.Declaration.Amount.String()}, nil
}

// AppendTipAdjustment validates authority and appends one immutable tip
// adjustment observation. Reusing an idempotency key with different content
// is rejected by the observation store; a retry with the same content replays
// the original evidence row.
func (s Service) AppendTipAdjustment(ctx context.Context, p *trust.Principal, req TipAdjustmentRequest) (ObservationRecord, error) {
	if err := validPrincipal(p); err != nil {
		return ObservationRecord{}, err
	}
	if s.Observations == nil || s.IDs == nil || s.Workers == nil || s.Auth == nil {
		return ObservationRecord{}, ErrUnavailable
	}
	if !requireNonEmpty(req.WorkerRef, req.AssignmentRef, req.DeclarationObservationID, req.Amount, req.Reason, req.IdempotencyKey) {
		return ObservationRecord{}, reject(ErrInvalidRequest, "tip_adjustment", "", "worker, assignment, declaration, amount, reason and idempotency key are required")
	}
	worker, _, _, err := s.resolveWorkerAndAssignment(ctx, p, tenantOf(p), req.WorkerRef, req.AssignmentRef)
	if err != nil {
		return ObservationRecord{}, err
	}
	amount, err := values.NewDecimal(strings.TrimSpace(req.Amount), 2, values.RoundingHalfEven)
	if err != nil {
		return ObservationRecord{}, reject(ErrInvalidRequest, "amount", "", err.Error())
	}
	at := s.now()
	if at.IsZero() {
		return ObservationRecord{}, ErrUnavailable
	}
	evidence := tipAdjustmentEvidence{
		SchemaVersion: 1, DeclarationObservationID: req.DeclarationObservationID, WorkerID: worker,
		AdjustedBy: p.Subject(), AdjustedAt: at, Amount: amount.String(), Reason: strings.TrimSpace(req.Reason),
	}
	payload, err := json.Marshal(evidence)
	if err != nil {
		return ObservationRecord{}, err
	}
	digestBytes := sha256.Sum256(payload)
	digest := "sha256:" + hex.EncodeToString(digestBytes[:])
	id := s.IDs.Deterministic(tenantOf(p), "tip-adjustment", req.DeclarationObservationID, req.IdempotencyKey)
	if strings.TrimSpace(id) == "" {
		return ObservationRecord{}, ErrUnavailable
	}
	row := ObservationRecord{
		ID: id, TenantID: tenantOf(p), WorkerRef: worker, AssignmentRef: req.AssignmentRef,
		Source: clockOutAttestationSource, EventType: tipAdjustmentEvent,
		IdempotencyKey: "tip-adjustment:" + req.IdempotencyKey, Digest: digest,
		CorrectsID: req.DeclarationObservationID, OccurredAt: at, ReceivedAt: at, Payload: payload,
	}
	result, _, err := s.Observations.AppendObservation(ctx, tenantOf(p), row)
	if err != nil {
		return ObservationRecord{}, err
	}
	return result, nil
}
