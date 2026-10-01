package promotioncommit

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/aggregates"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion/commit"
)

// ErrAssignmentWriteEvidenceMissing means no exact, non-legacy assignment
// write proof matched the requested proposal and committed aggregate row.
var ErrAssignmentWriteEvidenceMissing = errors.New("promotion commit: assignment write evidence is missing")

// AssignmentWriteEvidence is the immutable approval-to-successor proof read
// from the same transaction that wrote the assignment row.
type AssignmentWriteEvidence struct {
	TenantID, IntentID, ProposalRevisionID, ProposalDigest string
	ProposalRevisionNumber                                 int64
	WorkerID, AssignmentID, AssignmentRowID                string
	AssignmentDigest, FieldPath                            string
	CurrentValue, ProposedValue                            string
	EffectiveFrom, RecordedAt                              time.Time
	EffectiveTo                                            *time.Time
	ActorPrincipalID, AuthorityDecision                    string
	ExpectedSource                                         string
}

// AssignmentWriteEvidenceRequest identifies the exact approved material and
// committed aggregate successor whose provenance is required.
type AssignmentWriteEvidenceRequest struct {
	TenantID, IntentID, ProposalRevisionID, WorkerID, AssignmentID uuid.UUID
	ProposalRevisionNumber                                         int64
	ProposalDigest, AssignmentRowID, AssignmentDigest              string
}

func writeAssignmentEvidence(ctx context.Context, tx dbport.Tx, tenant, intentID, proposalID uuid.UUID,
	cmd commit.Command, before, after aggregates.Assignment, rowID uuid.UUID, proof commit.AssignmentWrite) error {
	var current, proposed string
	switch proof.FieldPath {
	case "assignment.grade":
		current, proposed = before.Grade, after.Grade
	case "assignment.job_code":
		current, proposed = before.JobCode, after.JobCode
	default:
		return fmt.Errorf("promotion commit: unsupported assignment proof field %q", proof.FieldPath)
	}
	if current != proof.CurrentValue || proposed != proof.ProposedValue {
		return fmt.Errorf("%w: approved %s values do not match the assignment transition", ErrAggregateBinding, proof.FieldPath)
	}
	_, err := tx.Exec(ctx, `INSERT INTO people_promotion_write_evidence (
		tenant_id, write_id, proposal_revision_id, proposal_digest, actor_principal_id,
		authority_decision, worker_id, assignment_id, field_path, current_value,
		proposed_value, expected_revision, effective_from, effective_to, recorded_at,
		intent_id, proposal_revision_number, assignment_row_id, assignment_digest)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19)`,
		tenant, uuid.New(), proposalID.String(), cmd.ProposalDigest, cmd.ActorPrincipalID,
		proof.AuthorityDecision, cmd.WorkerID, cmd.AssignmentID, proof.FieldPath, proof.CurrentValue,
		proof.ProposedValue, proof.ExpectedSource, after.EffectiveFrom, after.EffectiveTo, after.RecordedAt,
		intentID, int64(cmd.ProposalRevisionNumber), rowID, after.Digest)
	if err != nil {
		return fmt.Errorf("promotion commit: write assignment provenance: %w", err)
	}
	return nil
}

// ReadAssignmentWriteEvidence loads the exact tenant/intent/proposal/aggregate
// proof using the caller's executor, so a workflow can verify it in its
// existing transaction. Missing or legacy-null provenance fails closed.
func ReadAssignmentWriteEvidence(ctx context.Context, ex dbport.Querier, req AssignmentWriteEvidenceRequest) ([]AssignmentWriteEvidence, error) {
	if ex == nil {
		return nil, fmt.Errorf("promotion commit: a caller-owned query executor is required")
	}
	if req.TenantID == uuid.Nil || req.IntentID == uuid.Nil || req.ProposalRevisionID == uuid.Nil || req.WorkerID == uuid.Nil || req.AssignmentID == uuid.Nil ||
		req.ProposalRevisionNumber <= 0 || req.ProposalDigest == "" || req.AssignmentRowID == "" || req.AssignmentDigest == "" {
		return nil, fmt.Errorf("promotion commit: exact assignment provenance binding is required")
	}
	rows, err := ex.Query(ctx, `SELECT e.tenant_id::text, e.intent_id::text, e.proposal_revision_id, e.proposal_digest,
		e.proposal_revision_number, e.worker_id, e.assignment_id, e.assignment_row_id::text,
		e.assignment_digest, e.field_path, e.current_value, e.proposed_value, e.effective_from,
		e.effective_to, e.recorded_at, e.actor_principal_id, e.authority_decision, e.expected_revision
		FROM people_promotion_write_evidence e
		JOIN assignment a ON a.tenant_id=e.tenant_id AND a.row_id=e.assignment_row_id AND a.digest=e.assignment_digest
			AND a.effective_from=e.effective_from AND a.effective_to IS NOT DISTINCT FROM e.effective_to AND a.recorded_at=e.recorded_at
		WHERE e.tenant_id=$1 AND e.intent_id=$2 AND e.proposal_revision_id=$3 AND e.assignment_id=$4
		AND e.proposal_revision_number=$5 AND e.proposal_digest=$6 AND e.assignment_row_id=$7 AND e.assignment_digest=$8 AND e.worker_id=$9
		ORDER BY e.field_path`, req.TenantID, req.IntentID, req.ProposalRevisionID.String(), req.AssignmentID.String(),
		req.ProposalRevisionNumber, req.ProposalDigest, req.AssignmentRowID, req.AssignmentDigest, req.WorkerID.String())
	if err != nil {
		return nil, fmt.Errorf("promotion commit: read assignment provenance: %w", err)
	}
	defer rows.Close()
	var out []AssignmentWriteEvidence
	for rows.Next() {
		var item AssignmentWriteEvidence
		if err := rows.Scan(&item.TenantID, &item.IntentID, &item.ProposalRevisionID, &item.ProposalDigest, &item.ProposalRevisionNumber,
			&item.WorkerID, &item.AssignmentID, &item.AssignmentRowID, &item.AssignmentDigest, &item.FieldPath,
			&item.CurrentValue, &item.ProposedValue, &item.EffectiveFrom, &item.EffectiveTo, &item.RecordedAt,
			&item.ActorPrincipalID, &item.AuthorityDecision, &item.ExpectedSource); err != nil {
			return nil, fmt.Errorf("promotion commit: scan assignment provenance: %w", err)
		}
		if item.ProposalRevisionNumber <= 0 || item.AssignmentRowID == "" || item.AssignmentDigest == "" ||
			item.ProposalDigest == "" || item.ActorPrincipalID == "" || item.AuthorityDecision == "" || item.ExpectedSource == "" {
			return nil, fmt.Errorf("promotion commit: assignment provenance row is incomplete")
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("promotion commit: iterate assignment provenance: %w", err)
	}
	if len(out) == 0 {
		return nil, ErrAssignmentWriteEvidenceMissing
	}
	return out, nil
}
