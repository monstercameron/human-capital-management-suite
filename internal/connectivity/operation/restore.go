// Restored-state transition rules: CONN-RT-009 preserves terminal
// ambiguity and no-redrive fencing across crash and restore.
//
// Connector runtime centralizes these rules; provider adapters only
// supply observation capability. A restored lease epoch fences stale
// workers, terminal and ambiguous operations are durably NO_REDRIVE,
// observation evidence resolves state without a second effect, and only
// an approved RepairPlan creates an explicitly related new attempt with
// a distinct effect identity.
package operation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

// RestoredDisposition is the closed post-restore fate of one operation.
type RestoredDisposition string

// The restored dispositions.
const (
	// RestoredNoRedrive is durable: the operation never dispatches again.
	RestoredNoRedrive RestoredDisposition = "NO_REDRIVE"
	// RestoredObservationRequired waits on typed provider observation.
	RestoredObservationRequired RestoredDisposition = "OBSERVATION_REQUIRED"
	// RestoredRepairOnly admits only a governed repair attempt.
	RestoredRepairOnly RestoredDisposition = "REPAIR_ONLY"
	// RestoredRedrivable keeps the governed failed-operation path.
	RestoredRedrivable RestoredDisposition = "REDRIVABLE"
)

// RestoredVerdict classifies one operation under a restored epoch.
type RestoredVerdict struct {
	Disposition RestoredDisposition
	StaleWorker bool
	Reason      string
}

// ClassifyRestored maps one journaled operation to its post-restore
// fate under the current lease epoch.
func ClassifyRestored(op Operation, currentEpoch uint64) RestoredVerdict {
	verdict := RestoredVerdict{StaleWorker: op.WriterFenceEpoch < currentEpoch}
	switch op.State {
	case StateReconciled, StateDeadLetter, StateRejected:
		verdict.Disposition = RestoredNoRedrive
		verdict.Reason = "terminal operation " + string(op.State) + " is durable NO_REDRIVE"
	case StateAmbiguous, StateSent, StateSending, StateProviderAccepted, StateObserving:
		verdict.Disposition = RestoredNoRedrive
		verdict.Reason = "ambiguous operation " + string(op.State) + " resolves by observation only, never redrive"
	case StateRepairRequired:
		verdict.Disposition = RestoredRepairOnly
		verdict.Reason = "repair-required operation admits only a governed repair attempt"
	case StateFailed, StateRetryable:
		verdict.Disposition = RestoredRedrivable
		verdict.Reason = "failed operation keeps the governed redrive path"
	default:
		verdict.Disposition = RestoredRedrivable
		verdict.Reason = "unstarted operation " + string(op.State) + " resumes under the restored epoch"
	}
	return verdict
}

// CheckRestoredLease fences a worker presenting a stale lease epoch
// after restore. The epoch moves forward only; presenting the future
// is a malformed claim, not authority.
func CheckRestoredLease(op Operation, currentEpoch uint64, workerID string, workerEpoch uint64) error {
	if strings.TrimSpace(workerID) == "" {
		return fmt.Errorf("%w: worker identity is required", ErrLeaseRequired)
	}
	if workerEpoch < currentEpoch {
		return fmt.Errorf("%w: worker %s epoch %d is stale at restored epoch %d",
			ErrLeaseFenced, workerID, workerEpoch, currentEpoch)
	}
	if workerEpoch > currentEpoch {
		return fmt.Errorf("%w: worker %s claims future epoch %d past restored epoch %d",
			ErrLeaseRequired, workerID, workerEpoch, currentEpoch)
	}
	if op.WriterFenceEpoch != currentEpoch && op.State != StatePlanned && op.State != StateQueued {
		return fmt.Errorf("%w: operation fenced at epoch %d, restored epoch is %d",
			ErrLeaseFenced, op.WriterFenceEpoch, currentEpoch)
	}
	return nil
}

// SummarizeRestored renders the deterministic disposition table for one
// restored set: per-state dispositions with staleness, plus a digest.
// Evidence reviewers pin this string in golden files.
func SummarizeRestored(ops []Operation, currentEpoch uint64) string {
	lines := []string{"conn-rt-009-restored"}
	for _, op := range ops {
		verdict := ClassifyRestored(op, currentEpoch)
		lines = append(lines, string(op.State)+"\x00"+string(verdict.Disposition)+"\x00"+fmt.Sprint(verdict.StaleWorker))
	}
	sort.Strings(lines[1:])
	body := strings.Join(lines, "\n")
	sum := sha256.Sum256([]byte(body))
	return body + "\ndigest: sha256:" + hex.EncodeToString(sum[:])
}

// RepairPlan is the governed approval a repair attempt requires.
type RepairPlan struct {
	ID         string
	Approved   bool
	ApprovedBy string
}

// PlanRepairAttempt creates the explicitly related new attempt an
// approved RepairPlan authorizes. The original operation is never
// mutated: the attempt carries a distinct operation and effect
// identity linked by the repair plan ID.
func PlanRepairAttempt(ctx context.Context, j *MemoryJournal, operationID uuid.UUID, plan RepairPlan, at time.Time) (Operation, error) {
	if err := contextError(ctx); err != nil {
		return Operation{}, err
	}
	if strings.TrimSpace(plan.ID) == "" || !plan.Approved || strings.TrimSpace(plan.ApprovedBy) == "" {
		return Operation{}, fmt.Errorf("%w: approved repair plan with approver is required", ErrRepairPlanRequired)
	}
	j.mu.RLock()
	op, ok := j.operations[operationID]
	j.mu.RUnlock()
	if !ok {
		return Operation{}, fmt.Errorf("%w: %s", ErrNotFound, operationID)
	}
	// Terminal operations admit no repair attempt: reconciliation already
	// closed them. Every other state resolves through observation or the
	// governed redrive path; the repair attempt is the explicitly related
	// new effect identity, never a mutation of the original.
	if op.State == StateReconciled || op.State == StateDeadLetter || op.State == StateRejected {
		return Operation{}, fmt.Errorf("%w: terminal operation %s admits no repair attempt", ErrRedriveNotAllowed, op.State)
	}
	if at.IsZero() {
		at = j.now().UTC()
	}
	req := PlanRequest{
		OperationID: uuid.New(), TenantID: op.TenantID, ConnectionID: op.ConnectionID,
		ConnectorVersion: op.ConnectorVersion, BusinessTransactionID: op.BusinessTransactionID,
		WorkflowInstanceID: op.WorkflowInstanceID, RepairPlanID: plan.ID,
		SemanticOperation: op.SemanticOperation, Direction: op.Direction, Criticality: op.Criticality,
		ExternalResourceKey: op.ExternalResourceKey, OrderingClass: op.OrderingClass,
		CausalPredecessorID: op.OperationID, ResourceSequence: op.ResourceSequence,
		ExpectedExternalVersion:    op.ExpectedExternalVersion,
		SourceAuthorityDecisionRef: op.SourceAuthorityDecisionRef,
		AuthorityPolicyFingerprint: op.AuthorityPolicyFingerprint,
		WriterFenceEpoch:           op.WriterFenceEpoch,
		CanonicalInputRef:          op.CanonicalInputRef, CanonicalInputDigest: op.CanonicalInputDigest,
		MappingProfileVersion: op.MappingProfileVersion,
		MappedPayloadRef:      op.MappedPayloadRef, MappedPayloadDigest: op.MappedPayloadDigest, MappedPayload: op.MappedPayload,
		Classification: op.Classification, Purpose: op.Purpose, DestinationRef: op.DestinationRef,
		CredentialRef:          op.CredentialRef,
		IdempotencyKey:         op.IdempotencyKey + ":repair:" + plan.ID,
		ObservationRequirement: op.ObservationRequirement,
		CreatedAt:              at, DeadlineAt: at.Add(time.Hour),
	}
	return j.Plan(ctx, req)
}
