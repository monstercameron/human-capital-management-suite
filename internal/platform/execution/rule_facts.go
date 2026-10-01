package execution

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/aggregates"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/promotioncommit"
	"github.com/monstercameron/human-capital-management-suite/internal/data/rulethreshold"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/rules"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// composeCurrencyGuard honors a unit composition's guard override and
// defaults every other composition to the served RULE-004 guard. The
// override exists for harnesses that exercise non-currency concerns with
// synthetic intents the durable tables cannot key on; traffic-serving
// compositions leave PromotionExecutionConfig.Currency nil.
func composeCurrencyGuard(cfg PromotionExecutionConfig, steps *promotionStepPorts) *execute.CurrencyGuard {
	if cfg.Currency != nil {
		return cfg.Currency
	}
	return servedCurrencyGuard(steps)
}

// servedCurrencyGuard is the served driver's currency check (REV-010-01):
// every served advance revalidates the pinned proposal's supersession and
// standing approval, and re-runs the decision-table approval that produced
// its tier against the currently published threshold table (RULE-004). The
// frozen half comes from the threshold decisions the raise_threshold branch
// freezes in the advancement transaction; the live half re-derives through
// the same governed reads the threshold port uses. Revisions that never ran
// the threshold node, or that carry no standing approval, resolve RULE-004
// to silence and keep the WF-RUN-029 verdict exactly.
func servedCurrencyGuard(steps *promotionStepPorts) *execute.CurrencyGuard {
	return &execute.CurrencyGuard{
		Proposal: app.DurableProposalFacts{},
		Approval: app.DurableProposalFacts{},
		Rules:    &ServedRuleFacts{Thresholds: steps, Approval: app.DurableProposalFacts{}},
	}
}

// ServedRuleFacts resolves the RULE-004 re-evaluation facts for one served
// proposal revision (REV-010-01). The frozen half comes from the threshold
// decision the raise_threshold branch froze in the advancement transaction;
// the live half re-derives the threshold inputs through the same governed
// reads the threshold port uses; the standing approval digest comes from
// the recorded approval decisions. A revision that never ran the threshold
// node, or that carries no standing approval, resolves to the zero value:
// the guard's own approval checks then refuse with their typed errors, and
// RULE-004 simply has nothing to re-evaluate.
type ServedRuleFacts struct {
	// Thresholds derives the live threshold inputs. Nil is a composition
	// mistake, reported loudly rather than read as unchanged inputs.
	Thresholds ruleInputDeriver
	// Approval reads the standing approval decisions. Nil is a composition
	// mistake, reported loudly rather than read as no approval.
	Approval runtime.ApprovalFacts
}

// ruleInputDeriver is the one governed read the served facts need.
// *promotionStepPorts is the production implementation; tests substitute a
// func-backed fake without standing up step services.
type ruleInputDeriver interface {
	thresholdInputs(ctx context.Context, ex runtime.Executor, req execute.StepRequest) (rules.PromotionApprovalInput, error)
}

// currentThresholdInputs re-derives RULE-003's inputs for the recorded
// instance through the governed threshold-inputs path, so the derivation
// the guard compares against is the same one the threshold port evaluated.
func (s *ServedRuleFacts) currentThresholdInputs(ctx context.Context, ex runtime.Executor, record rulethreshold.Decision, tenantID uuid.UUID, rev intent.ProposalRevision, checkedAt time.Time) (rules.PromotionApprovalInput, error) {
	if checkedAt.IsZero() {
		return rules.PromotionApprovalInput{}, fmt.Errorf("platform execution: RULE-004 threshold revalidation has no checked_at instant")
	}
	req := execute.StepRequest{
		TenantID:   tenantID,
		InstanceID: record.InstanceID,
		Attempt:    record.Attempt,
		Node:       workflow.CompiledNode{ID: promotionexec.NodeRaiseThreshold, Type: workflow.StepDecision},
		Proposal:   runtime.ProposalBinding{Revision: rev},
		RecordedAt: checkedAt.UTC(),
	}
	return s.Thresholds.thresholdInputs(ctx, ex, req)
}

// Lookup implements [execute.RuleFacts].
func (s *ServedRuleFacts) Lookup(ctx context.Context, ex runtime.Executor, tenantID uuid.UUID, rev intent.ProposalRevision, checkedAt time.Time) (execute.RuleApproval, error) {
	if s.Thresholds == nil || s.Approval == nil {
		return execute.RuleApproval{}, fmt.Errorf("platform execution: served rule facts need threshold inputs and approval facts ports")
	}
	intentID, err := uuid.Parse(rev.IntentID)
	if err != nil {
		return execute.RuleApproval{}, fmt.Errorf("platform execution: threshold lookup needs an intent-keyed revision: %w", err)
	}
	record, found, err := rulethreshold.Latest(ctx, ex, tenantID, intentID, rev.Revision)
	if err != nil {
		return execute.RuleApproval{}, err
	}
	if !found {
		return execute.RuleApproval{}, nil
	}
	decisions, err := s.Approval.Decisions(ctx, ex, tenantID, rev)
	if err != nil {
		return execute.RuleApproval{}, err
	}
	var approvalDigest string
	for _, d := range decisions {
		if d.ProposalDigest != rev.MaterialDigest.Digest || d.Invalidated || d.Outcome != runtime.ApprovalOutcomeApproved {
			continue
		}
		// ApprovalDecisionFact carries no digest column; the decision's
		// own content identity stands in. Minting a sha256: label for it
		// would be theater, and the verdict never compares this value --
		// it cites the approval the re-evaluation ran under.
		approvalDigest = d.DecisionID
		break
	}
	if approvalDigest == "" {
		return execute.RuleApproval{}, nil
	}
	current, err := s.currentThresholdInputs(ctx, ex, record, tenantID, rev, checkedAt)
	if err != nil {
		return execute.RuleApproval{}, err
	}
	if record.InstanceID != uuid.Nil && (len(rev.CurrentState) > 0 || len(rev.ProposedState) > 0 || len(rev.Writes) > 0) {
		if err := verifyAssignmentPhase(ctx, ex, tenantID, record.InstanceID, rev, checkedAt); err != nil {
			return execute.RuleApproval{}, err
		}
	}
	return execute.RuleApproval{
		Resolved: true,
		Approved: rules.ApprovedPlan{
			Input: record.Input, InputDigest: record.InputDigest,
			Tier: rules.ApprovalTier(record.Tier), MatchedRowID: record.MatchedRow,
			TableID: record.TableID, TableVersion: record.TableVersion, TableDigest: record.TableDigest,
			ApprovalDigest: approvalDigest,
		},
		Current: current,
	}, nil
}

// verifyAssignmentPhase binds every served RULE-004 lookup to the assignment
// state at the proposal's effective coordinate. Before this proposal commits,
// the row must still equal CurrentState. After commit, only the exact writer
// evidence may authorize ProposedState; a later row with the same grade is
// rejected by row and digest identity.
func verifyAssignmentPhase(ctx context.Context, ex runtime.Executor, tenantID, instanceID uuid.UUID, rev intent.ProposalRevision, checkedAt time.Time) error {
	workerID, currentGrade, proposedGrade, err := assignmentGrades(rev, rev.Tenant)
	if err != nil {
		return err
	}
	effectiveAt, err := phaseEffectiveAt(rev)
	if err != nil {
		return err
	}
	phase, err := promotionCommitPhase(ctx, ex, tenantID, instanceID)
	if err != nil {
		return err
	}
	var assignmentID, rowID, digest, liveGrade string
	var effectiveFrom, recordedAt time.Time
	err = ex.QueryRow(ctx, `SELECT a.entity_id, a.row_id, a.digest, a.grade, a.effective_from, a.recorded_at
		FROM assignment a JOIN employment e ON e.tenant_id=a.tenant_id AND e.entity_id=a.employment_ref
		WHERE a.tenant_id=$1 AND e.worker_ref=$2 AND a.effective_from <= $3
		AND (a.effective_to IS NULL OR a.effective_to > $3) AND a.superseded_at IS NULL
		ORDER BY a.effective_from DESC, a.recorded_at DESC, a.row_id DESC LIMIT 1`, tenantID, workerID, effectiveAt).
		Scan(&assignmentID, &rowID, &digest, &liveGrade, &effectiveFrom, &recordedAt)
	if err != nil {
		return fmt.Errorf("platform execution: assignment phase read: %w", err)
	}
	assignmentUUID, parseErr := uuid.Parse(assignmentID)
	if parseErr != nil {
		return fmt.Errorf("platform execution: assignment phase identity: %w", parseErr)
	}
	live, err := (aggregates.PeopleStore{}).CurrentAssignment(ctx, ex, tenantID, assignmentUUID, effectiveAt)
	if err != nil {
		return fmt.Errorf("platform execution: current assignment phase: %w", err)
	}
	baseline, err := (aggregates.PeopleStore{}).KnownAsOfAssignment(ctx, ex, tenantID, assignmentUUID, effectiveAt, rev.CreatedAt.Time())
	if err != nil {
		return fmt.Errorf("platform execution: known assignment baseline: %w", err)
	}
	rowID, digest, liveGrade, effectiveFrom, recordedAt = live.RowID.String(), live.Digest, live.Grade, live.EffectiveFrom, live.RecordedAt
	if effectiveFrom.After(effectiveAt) || recordedAt.After(checkedAt.UTC()) {
		return fmt.Errorf("platform execution: assignment phase is not known at checked_at")
	}
	proofs, err := readAssignmentProof(ctx, ex, tenantID, rev, workerID, assignmentUUID, rowID, digest)
	if err == nil {
		if liveGrade != proposedGrade {
			return fmt.Errorf("platform execution: committed assignment grade is not the approved proposed grade")
		}
		for _, proof := range proofs {
			wantCurrent, wantProposed, wantAuthority, wantSource := proposalWriteValues(rev, proof.FieldPath)
			if proof.WorkerID != workerID.String() || proof.AssignmentID != assignmentID || proof.AssignmentRowID != rowID || proof.AssignmentDigest != digest ||
				!proof.EffectiveFrom.Equal(effectiveFrom) || proof.RecordedAt.After(checkedAt.UTC()) || proof.CurrentValue == proof.ProposedValue ||
				wantCurrent == "" || proof.CurrentValue != wantCurrent || proof.ProposedValue != wantProposed || strings.TrimSpace(proof.ActorPrincipalID) == "" ||
				proof.AuthorityDecision != wantAuthority || proof.ExpectedSource != wantSource {
				return fmt.Errorf("platform execution: assignment proof does not bind the live successor")
			}
		}
		return nil
	}
	if !errors.Is(err, promotioncommit.ErrAssignmentWriteEvidenceMissing) {
		return fmt.Errorf("platform execution: assignment proof read: %w", err)
	}
	if phase == "POSTCOMMIT" {
		return fmt.Errorf("platform execution: committed assignment has no exact provenance")
	}
	if rowID != baseline.RowID.String() || digest != baseline.Digest {
		return fmt.Errorf("platform execution: assignment successor has no matching commit proof")
	}
	if liveGrade != currentGrade {
		return fmt.Errorf("platform execution: assignment grade is neither the approved baseline nor a proven successor")
	}
	return nil
}

func promotionCommitPhase(ctx context.Context, ex runtime.Executor, tenantID, instanceID uuid.UUID) (string, error) {
	// A later failed/retrying attempt cannot reopen a commit that already
	// succeeded. The durable history, rather than the latest attempt, owns the
	// phase transition.
	var succeeded bool
	if err := ex.QueryRow(ctx, `SELECT EXISTS (
		SELECT 1 FROM workflow_node_execution
		WHERE tenant_id=$1 AND instance_id=$2 AND node_id=$3 AND status=$4)`, tenantID, instanceID,
		promotionexec.NodeExecutePromotion, string(runtime.NodeSucceeded)).Scan(&succeeded); err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return "PRECOMMIT", nil
		}
		return "", fmt.Errorf("platform execution: promotion commit phase: %w", err)
	}
	if succeeded {
		return "POSTCOMMIT", nil
	}
	return "PRECOMMIT", nil
}

func proposalWriteValues(rev intent.ProposalRevision, field string) (string, string, string, string) {
	for _, write := range rev.Writes {
		mapped := write.FieldPath
		if mapped == "assignment.assignment.grade" {
			mapped = "assignment.grade"
		}
		if mapped == field {
			return write.CurrentCanonicalText, write.ProposedCanonicalText, write.SourceAuthorityDecision, write.ExpectedRevision.String()
		}
	}
	return "", "", "", ""
}

func assignmentGrades(rev intent.ProposalRevision, tenantKey values.TenantId) (uuid.UUID, string, string, error) {
	var workerID uuid.UUID
	var current, proposed string
	var key values.ResourceKey
	if strings.TrimSpace(string(tenantKey)) == "" || rev.Tenant != tenantKey {
		return uuid.Nil, "", "", fmt.Errorf("platform execution: assignment grade proposal tenant is invalid")
	}
	for _, assertion := range rev.CurrentState {
		if assertion.FieldPath != "assignment.grade" {
			continue
		}
		if assertion.ResourceKey.Tenant != tenantKey || assertion.ResourceKey.ResourceType != values.Kind("assignment") ||
			len(assertion.ResourceKey.Segments) != 2 || assertion.ResourceKey.Segments[0] != "worker" || assertion.Subject.SubjectID == "" || assertion.ResourceKey.Segments[1] != assertion.Subject.SubjectID {
			return uuid.Nil, "", "", fmt.Errorf("platform execution: assignment grade baseline has invalid resource binding")
		}
		if current != "" {
			return uuid.Nil, "", "", fmt.Errorf("platform execution: assignment grade baseline is ambiguous")
		}
		var parseErr error
		workerID, parseErr = uuid.Parse(assertion.Subject.SubjectID)
		if parseErr != nil {
			return uuid.Nil, "", "", fmt.Errorf("platform execution: assignment grade subject: %w", parseErr)
		}
		current = strings.TrimSpace(assertion.CanonicalText)
		key = assertion.ResourceKey
	}
	for _, assertion := range rev.ProposedState {
		if assertion.FieldPath == "assignment.grade" && assertion.Subject.SubjectID == workerID.String() {
			if !assertion.ResourceKey.Equal(key) {
				return uuid.Nil, "", "", fmt.Errorf("platform execution: assignment grade proposal resource binding differs")
			}
			if proposed != "" {
				return uuid.Nil, "", "", fmt.Errorf("platform execution: assignment grade proposal is ambiguous")
			}
			proposed = strings.TrimSpace(assertion.CanonicalText)
		}
	}
	if workerID == uuid.Nil || current == "" || proposed == "" {
		return uuid.Nil, "", "", fmt.Errorf("platform execution: assignment grade phase facts are incomplete")
	}
	return workerID, current, proposed, nil
}

func phaseEffectiveAt(rev intent.ProposalRevision) (time.Time, error) {
	if date, ok := rev.EffectiveTime.StartDate(); ok {
		return time.Date(int(date.Year()), date.Month(), int(date.Day()), 0, 0, 0, 0, time.UTC), nil
	}
	if instant, ok := rev.EffectiveTime.StartInstant(); ok {
		return instant.Time().UTC(), nil
	}
	return time.Time{}, fmt.Errorf("platform execution: proposal has no effective start")
}

func readAssignmentProof(ctx context.Context, ex runtime.Executor, tenantID uuid.UUID, rev intent.ProposalRevision, workerID, assignmentID uuid.UUID, rowID, digest string) ([]promotioncommit.AssignmentWriteEvidence, error) {
	proposalID, err := uuid.Parse(rev.ProposalRevisionID)
	if err != nil {
		return nil, err
	}
	intentID, err := uuid.Parse(rev.IntentID)
	if err != nil {
		return nil, err
	}
	proofs, err := promotioncommit.ReadAssignmentWriteEvidence(ctx, ex, promotioncommit.AssignmentWriteEvidenceRequest{
		TenantID: tenantID, IntentID: intentID, ProposalRevisionID: proposalID, WorkerID: workerID, AssignmentID: assignmentID,
		ProposalRevisionNumber: int64(rev.Revision), ProposalDigest: rev.MaterialDigest.Digest, AssignmentRowID: rowID, AssignmentDigest: digest,
	})
	if err != nil {
		return nil, err
	}
	return proofs, nil
}
