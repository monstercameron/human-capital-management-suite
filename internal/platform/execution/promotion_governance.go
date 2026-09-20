package execution

// WF-RUN-034: the served promotion's GOVERN-002 history and its GOVERN-003
// revalidation.
//
// One governance record is written per approval decision, from facts read out
// of the durable stores (migration 00309). At the effective date the same
// facts are read again and internal/governance/revalidate recomposes the
// recorded decision from them: an unchanged world reproduces the recorded
// digest and confirms, a moved budget, position or conflict fact demands a
// replan, a moved authorization or control version demands reapproval, and a
// world that no longer allows at all blocks. Nothing is defaulted to pass: an
// input no durable fact answers is UNKNOWN_FAIL_CLOSED, which composes to a
// blocking decision at approval time as much as at revalidation time.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/aggregates"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/intentcontrol"
	"github.com/monstercameron/human-capital-management-suite/internal/governance/decision"
	"github.com/monstercameron/human-capital-management-suite/internal/governance/revalidate"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	intentapproval "github.com/monstercameron/human-capital-management-suite/internal/intent/approval"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/execution/promotionterminal"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// Governance fact tokens. Each names a durable observation, never a default.
const (
	conflictNone              = "NO_CONFLICT"
	conflictInterveningWrite  = "INTERVENING_WRITE"
	conflictCompetingPromo    = "COMPETING_ACTIVE_PROMOTION"
	conflictSubjectUnresolved = "SUBJECT_UNRESOLVED"
)

// governanceExecutor is the read/write surface the governance record and its
// facts need. It is deliberately narrower than a dbport.Tx: the record is
// written inside the approval kernel's own vote transaction, whose hooks hand
// the caller a workitem.Executor rather than the transaction itself
// (WF-STEP-018), and read inside this package's own rolled-back read
// transaction.
type governanceExecutor interface {
	dbport.Execer
	dbport.Querier
}

// ErrNoApprovalGovernance means the instance has no durable GOVERN-002
// record to revalidate against.
var ErrNoApprovalGovernance = errors.New("platform execution: this promotion recorded no approval governance")

// governanceInputs is what one governance evaluation needs beyond the facts:
// the identities the composed decision names.
type governanceInputs struct {
	tenantID   uuid.UUID
	instanceID uuid.UUID
	proposal   runtime.ProposalBinding
	planDigest string
	standing   app.GovernanceStanding
}

// approvalGovernanceRecord is the durable row promotion_approval_governance
// holds: the historical approval plus the identities it belongs to.
type approvalGovernanceRecord struct {
	Historical revalidate.HistoricalApproval `json:"historical"`
	NodeID     string                        `json:"node_id"`
	RecordedAt time.Time                     `json:"recorded_at"`
}

// effectiveStart is the promotion's effective business instant (UTC
// midnight): every governance fact is read at exactly this coordinate, so an
// approval and its revalidation compare the same rows.
func effectiveStart(proposal runtime.ProposalBinding) (time.Time, error) {
	return promotionterminal.EffectiveStart(proposal.Revision.EffectiveTime)
}

// governanceFacts reads the seven GOVERN-003 facts for one promotion.
func (p *promotionStepPorts) governanceFacts(ctx context.Context, tx governanceExecutor, in governanceInputs) (revalidate.Facts, error) {
	at, err := effectiveStart(in.proposal)
	if err != nil {
		return revalidate.Facts{}, err
	}
	standing := in.standing
	facts := revalidate.Facts{
		AuthZ: revalidate.AuthZFact{
			Effect: allowWhen(standing.Authorized), PolicyVersion: orUnknown(standing.PolicyBundleDigest),
			FiredRules: []decision.RuleRef{{ID: "execution.authority.role", Version: orUnknown(standing.RequiredRole)}},
		},
		Session: revalidate.SessionFact{
			Effect:    allowWhen(standing.Authorized && strings.TrimSpace(standing.SessionRef) != ""),
			SessionID: orUnknown(standing.SessionRef), Assurance: orUnknown(standing.Assurance),
		},
		SourceAuthority:     revalidate.SourceAuthorityFact{Decision: sourceAuthorityDecision(in.proposal.Revision, standing)},
		FieldClassification: revalidate.FieldClassificationFact{Version: orUnknown(standing.ClassificationDigest)},
		LegalPolicy: revalidate.LegalPolicyFact{
			LegalPackVersion: orUnknown(standing.LegalContextDigest), PolicyPackVersion: orUnknown(standing.PolicyBundleDigest),
		},
	}
	budget, err := p.budgetFact(ctx, tx, in, at)
	if err != nil {
		return revalidate.Facts{}, err
	}
	position, err := p.positionFact(ctx, tx, in, at)
	if err != nil {
		return revalidate.Facts{}, err
	}
	facts.BudgetPosition = revalidate.BudgetPositionFact{Budget: budget, Position: position}
	conflict, err := p.conflictFact(ctx, tx, in, at)
	if err != nil {
		return revalidate.Facts{}, err
	}
	facts.Conflict = conflict
	return facts, nil
}

// budgetFact reads the proposal's own budget reservation and the pool behind
// it. A proposal holding no reservation is refused, never assumed funded.
func (p *promotionStepPorts) budgetFact(ctx context.Context, tx governanceExecutor, in governanceInputs, at time.Time) (revalidate.ResourceFact, error) {
	proposalID, err := uuid.Parse(in.proposal.Revision.ProposalRevisionID)
	if err != nil {
		return revalidate.ResourceFact{Effect: decision.UnknownFailClosed, ObservationID: "budget:unresolved-proposal"}, nil
	}
	comp := aggregates.CompensationStore{}
	reservation, err := comp.ReservationForProposal(ctx, tx, in.tenantID, proposalID, at)
	if errors.Is(err, aggregates.ErrNotFound) {
		return revalidate.ResourceFact{Effect: decision.Deny, ObservationID: "budget:no-reservation:" + proposalID.String()}, nil
	}
	if err != nil {
		return revalidate.ResourceFact{}, fmt.Errorf("platform execution: read the budget reservation: %w", err)
	}
	pool, err := comp.CurrentWorkforceBudget(ctx, tx, in.tenantID, reservation.BudgetRef, at)
	if errors.Is(err, aggregates.ErrNotFound) {
		return revalidate.ResourceFact{Effect: decision.Deny, ObservationID: "budget:no-pool:" + reservation.Digest}, nil
	}
	if err != nil {
		return revalidate.ResourceFact{}, fmt.Errorf("platform execution: read the compensation pool: %w", err)
	}
	held := reservation.Status == "HELD" || reservation.Status == "COMMITTED"
	return revalidate.ResourceFact{
		Effect:        allowWhen(held),
		ObservationID: "budget:" + reservation.Digest + ":pool:" + pool.Digest,
		FiredRules:    []decision.RuleRef{{ID: "budget.reservation.status", Version: reservation.Status}},
	}, nil
}

// positionFact reads the approved target position's own capacity: OPEN, and
// with room for this promotion's placement beside whatever already occupies
// it (this proposal's own occupancy, once committed, is not competition).
func (p *promotionStepPorts) positionFact(ctx context.Context, tx governanceExecutor, in governanceInputs, at time.Time) (revalidate.ResourceFact, error) {
	positionID, ok := positionSubject(in.proposal.Revision)
	if !ok {
		return revalidate.ResourceFact{Effect: decision.Deny, ObservationID: "position:none"}, nil
	}
	org := aggregates.OrganizationStore{}
	position, err := org.CurrentJobPosition(ctx, tx, in.tenantID, positionID, at)
	if errors.Is(err, aggregates.ErrNotFound) {
		return revalidate.ResourceFact{Effect: decision.Deny, ObservationID: "position:absent:" + positionID.String()}, nil
	}
	if err != nil {
		return revalidate.ResourceFact{}, fmt.Errorf("platform execution: read the target position: %w", err)
	}
	var occupied string
	row := tx.QueryRow(ctx, `
		SELECT COALESCE(SUM(allocation_fte), 0)::numeric(9,4)::text FROM position_occupancy
		WHERE tenant_id = $1 AND position_ref = $2 AND superseded_at IS NULL
		  AND entity_id <> $3
		  AND effective_from <= $4 AND (effective_to IS NULL OR effective_to > $4)`,
		in.tenantID, positionID, occupancyOf(in.proposal.Revision), at)
	if err := row.Scan(&occupied); err != nil {
		return revalidate.ResourceFact{}, fmt.Errorf("platform execution: read the target position's occupancy: %w", err)
	}
	room := false
	capacity, capErr := values.NewDecimal(position.CapacityFTE, 4, values.RoundingHalfEven)
	used, usedErr := values.NewDecimal(occupied, 4, values.RoundingHalfEven)
	if capErr == nil && usedErr == nil {
		if remaining, subErr := capacity.Sub(used); subErr == nil {
			one, _ := values.NewDecimal("1.0000", 4, values.RoundingHalfEven)
			room = remaining.Cmp(one) >= 0
		}
	}
	return revalidate.ResourceFact{
		Effect:        allowWhen(position.LifecycleState == "OPEN" && room),
		ObservationID: "position:" + position.Digest + ":occupied:" + occupied,
		FiredRules: []decision.RuleRef{
			{ID: "position.lifecycle", Version: position.LifecycleState},
			{ID: "position.capacity_fte", Version: position.CapacityFTE},
		},
	}, nil
}

// conflictFact classifies the promotion's write footprint against everything
// else recorded: a competing active promotion for the same worker, and any
// intervening write to the baselines the approved proposal was computed from
// (the worker's assignment and base pay, as they stood when the revision was
// produced versus now).
func (p *promotionStepPorts) conflictFact(ctx context.Context, tx governanceExecutor, in governanceInputs, at time.Time) (revalidate.ConflictFact, error) {
	revision := in.proposal.Revision
	workerID, ok := revisionWorkerSubject(revision)
	if !ok {
		return revalidate.ConflictFact{
			Effect: decision.UnknownFailClosed, Classification: conflictSubjectUnresolved, EvidenceRef: "conflict:no-subject",
		}, nil
	}
	intentID, err := uuid.Parse(revision.IntentID)
	if err != nil {
		return revalidate.ConflictFact{
			Effect: decision.UnknownFailClosed, Classification: conflictSubjectUnresolved, EvidenceRef: "conflict:no-intent",
		}, nil
	}
	var competing int
	if err := tx.QueryRow(ctx, `
		SELECT count(*) FROM promotion_active_intent_guard
		WHERE tenant_id = $1 AND worker_ref = $2 AND status = 'ACTIVE'
		  AND (intent_id IS NULL OR intent_id <> $3)`, in.tenantID, workerID.String(), intentID).Scan(&competing); err != nil {
		return revalidate.ConflictFact{}, fmt.Errorf("platform execution: read competing promotions: %w", err)
	}

	// The approved revision's produced instant is the knowledge horizon every
	// approval-time baseline is read at. A revision the store does not hold,
	// or holds under another digest, is not a fact this evaluation can reason
	// from: it is an unresolved conflict, which fails closed.
	produced, err := p.producedAt(ctx, tx, in.tenantID, intentID, revision)
	if err != nil {
		return revalidate.ConflictFact{
			Effect: decision.UnknownFailClosed, Classification: conflictSubjectUnresolved,
			EvidenceRef: "conflict:unresolved:approved-revision",
			FiredRules:  []decision.RuleRef{{ID: "promotion.approved_revision", Version: revision.MaterialDigest.Digest}},
		}, nil
	}
	people, comp := aggregates.PeopleStore{}, aggregates.CompensationStore{}
	employment, err := people.ActiveEmploymentForWorker(ctx, tx, in.tenantID, workerID, at)
	if err != nil {
		return unresolvedConflict(err, "employment")
	}
	assignment, err := people.PrimaryAssignmentForEmployment(ctx, tx, in.tenantID, employment.EntityID, at)
	if err != nil {
		return unresolvedConflict(err, "assignment")
	}
	pkg, err := comp.ActivePackageForWorker(ctx, tx, in.tenantID, workerID, at)
	if err != nil {
		return unresolvedConflict(err, "compensation package")
	}
	base, err := comp.BasePayComponentForPackage(ctx, tx, in.tenantID, pkg.EntityID, at)
	if err != nil {
		return unresolvedConflict(err, "base pay")
	}
	frozenAssignment, err := people.KnownAsOfAssignment(ctx, tx, in.tenantID, assignment.EntityID, at, produced)
	if err != nil {
		return unresolvedConflict(err, "approval-time assignment")
	}
	frozenBase, err := comp.KnownAsOfCompensationComponent(ctx, tx, in.tenantID, base.EntityID, at, produced)
	if err != nil {
		return unresolvedConflict(err, "approval-time base pay")
	}
	classification := conflictNone
	switch {
	case competing > 0:
		classification = conflictCompetingPromo
	case assignment.Digest != frozenAssignment.Digest || base.Digest != frozenBase.Digest:
		classification = conflictInterveningWrite
	}
	return revalidate.ConflictFact{
		Effect:         allowWhen(classification == conflictNone),
		Classification: classification,
		EvidenceRef:    "conflict:" + assignment.Digest + ":" + base.Digest,
		FiredRules:     []decision.RuleRef{{ID: "promotion.active_intent_guard", Version: fmt.Sprint(competing)}},
	}, nil
}

// producedAt is the instant the approved revision was produced: the knowledge
// horizon every approval-time baseline is read at.
func (p *promotionStepPorts) producedAt(ctx context.Context, tx governanceExecutor, tenantID, intentID uuid.UUID, revision intent.ProposalRevision) (time.Time, error) {
	row, err := (intentcontrol.RevisionStore{}).Load(ctx, tx, tenantID, intentID, revision.Revision)
	if err != nil {
		return time.Time{}, fmt.Errorf("platform execution: load the approved revision: %w", err)
	}
	if row.MaterialDigest != revision.MaterialDigest.Digest {
		return time.Time{}, fmt.Errorf("platform execution: the stored revision digest does not match the running proposal")
	}
	return row.ProducedAt.UTC(), nil
}

func unresolvedConflict(err error, what string) (revalidate.ConflictFact, error) {
	if errors.Is(err, aggregates.ErrNotFound) {
		return revalidate.ConflictFact{
			Effect: decision.UnknownFailClosed, Classification: conflictSubjectUnresolved,
			EvidenceRef: "conflict:unresolved:" + what,
		}, nil
	}
	return revalidate.ConflictFact{}, fmt.Errorf("platform execution: read the %s baseline: %w", what, err)
}

// governanceBase is every input GOVERN-002 composes that revalidation does not
// re-read: the proposal's identity, the material context, the approval
// requirements the run recorded and the control snapshot the decision was
// taken under.
func governanceBase(in governanceInputs, decisions promotionterminalDecisions) decision.Inputs {
	revision := in.proposal.Revision
	fields := make([]string, 0, len(revision.Writes))
	for _, w := range revision.Writes {
		fields = append(fields, w.FieldPath)
	}
	if len(fields) == 0 {
		fields = []string{"promotion.no_declared_write"}
	}
	standing := in.standing
	subject := ""
	if worker, ok := revisionWorkerSubject(revision); ok {
		subject = worker.String()
	}
	approvals := make([]decision.ApprovalRequirement, 0, len(decisions.ApprovalIDs))
	for _, id := range decisions.ApprovalIDs {
		approvals = append(approvals, decision.ApprovalRequirement{
			ID: "approval:" + id, Version: "1", Satisfaction: decision.ApprovalSatisfied,
		})
	}
	if len(approvals) == 0 {
		approvals = append(approvals, decision.ApprovalRequirement{
			ID: "approval:none", Version: "1", Satisfaction: decision.ApprovalUnknown,
		})
	}
	return decision.Inputs{
		ProposalRevisionDigest: revision.MaterialDigest.Digest,
		Context: decision.Context{
			Principal: orUnknown(standing.Subject), Delegation: "delegation:workflow_execution:" + in.instanceID.String(),
			Capability: promotionexec.CapabilityExecutePromotion, Resource: orUnknown(subject), Fields: fields,
			CurrentOrganization: orUnknown(revision.OrganizationScopeID), TargetOrganization: orUnknown(revision.OrganizationScopeID),
			Purpose: orUnknown(standing.Purpose), Risk: orUnknown(standing.RiskClass),
			Authority: sourceAuthorityDecision(revision, standing), Legal: orUnknown(standing.LegalContextDigest),
		},
		ControlSnapshot: decision.ControlSnapshot{
			Digest: orUnknown(standing.ControlDigest), Capability: orUnknown(standing.CapabilityDigest),
			PolicyBundle: orUnknown(standing.PolicyBundleDigest), LegalContext: orUnknown(standing.LegalContextDigest),
			Classification: orUnknown(standing.ClassificationDigest),
		},
		ApprovalRequirements: approvals,
	}
}

// promotionterminalDecisions is the recorded decision set one instance
// carries, kept as its own type so this file does not depend on the terminal
// resolver's package for a two-field value.
type promotionterminalDecisions struct {
	ApprovalIDs []string
	TaskIDs     []string
}

// RecordApprovalGovernance composes and stores the historical GOVERN-002
// decision for one approval, inside tx. A record already written for this
// work item is left alone: an approval is decided once, and a replay must not
// re-date its governance.
func (p *promotionStepPorts) RecordApprovalGovernance(ctx context.Context, tx governanceExecutor, in governanceInputs, nodeID string, workItemID uuid.UUID, decisions promotionterminalDecisions, recordedAt time.Time) (ret0 bool, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.promotion_governance.record_approval", in.instanceID, nodeID)
	defer func() { observe.DoneWith(obsOp, retErr, ret0) }()
	facts, err := p.governanceFacts(ctx, tx, in)
	if err != nil {
		return false, err
	}
	historical, err := revalidate.NewHistoricalApproval(governanceBase(in, decisions), facts, in.planDigest)
	if err != nil {
		return false, fmt.Errorf("platform execution: compose the approval governance record: %w", err)
	}
	body, err := json.Marshal(approvalGovernanceRecord{Historical: historical, NodeID: nodeID, RecordedAt: recordedAt.UTC()})
	if err != nil {
		return false, fmt.Errorf("platform execution: encode the approval governance record: %w", err)
	}
	proposalID, err := uuid.Parse(in.proposal.Revision.ProposalRevisionID)
	if err != nil {
		return false, fmt.Errorf("platform execution: the approved revision names no proposal: %w", err)
	}
	affected, err := tx.Exec(ctx, `
		INSERT INTO promotion_approval_governance
			(tenant_id, instance_id, work_item_id, node_id, proposal_revision_id, material_digest, plan_digest,
			 decision_state, decision_digest, record, recorded_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		ON CONFLICT (tenant_id, instance_id, work_item_id) DO NOTHING`,
		in.tenantID, in.instanceID, workItemID, nodeID, proposalID, in.proposal.Revision.MaterialDigest.Digest,
		in.planDigest, string(historical.Decision.State), historical.Decision.Digest, body, recordedAt.UTC())
	if err != nil {
		return false, fmt.Errorf("platform execution: record the approval governance: %w", err)
	}
	return affected > 0, nil
}

// recordApprovalGovernance records the GOVERN-002 decision behind one
// approval vote, inside the vote's own transaction (WF-STEP-018's approval
// kernel hands this hook the same executor the WorkItem completion and the
// caller's decision evidence are written through). The record and the
// decision it is about therefore commit together or not at all.
//
// It is a no-op for the prototype plan, for a vote on a node that is not one
// of the promotion's approvals, and for a vote that is not an approval: a
// rejection, expiry, invalidation or cancellation authorizes nothing and has
// nothing to revalidate later.
func (a executeDriverAdapter) recordApprovalGovernance(ctx context.Context, req app.ApprovalVoteRequest, ex workitem.Executor) error {
	if a.plan != PLAN_EXECUTE || a.steps == nil {
		return nil
	}
	if req.Decision.Outcome != intentapproval.OutcomeApproved {
		return nil
	}
	nodeID := req.Continuation.NodeID
	if nodeID != promotionexec.NodeApproveFinance && nodeID != promotionexec.NodeApproveManager {
		return nil
	}
	services := a.steps.bound()
	if services == nil {
		return fmt.Errorf("platform execution: promotion step services are not bound")
	}
	tenantID, instanceID := req.Start.TenantID, req.InstanceID
	recordedAt := req.RecordedAt.UTC()
	if recordedAt.IsZero() {
		recordedAt = a.steps.instant().Time()
	}
	delegation, found, err := runtime.LoadExecutionDelegation(ctx, ex, tenantID, instanceID)
	if err != nil {
		return fmt.Errorf("platform execution: record the approval's governance decision: %w", err)
	}
	if !found {
		return fmt.Errorf("platform execution: record the approval's governance decision: %w", errNoDelegation)
	}
	recorded, err := (promotionterminal.WorkItemDecisions{}).DecisionsForInstance(ctx, ex, tenantID, instanceID)
	if err != nil {
		return fmt.Errorf("platform execution: record the approval's governance decision: %w", err)
	}
	call := app.PromotionStepCall{
		Delegation: delegation, IntentID: req.Start.Proposal.Revision.IntentID, NodeID: nodeID,
		IdempotencyKey: fmt.Sprintf("workflow:%s:%s:%s:governance", tenantID, instanceID, nodeID),
		Deadline:       recordedAt.Add(stepInvocationBudget),
	}
	standing, err := services.GovernanceStanding(ctx, call)
	if err != nil {
		return fmt.Errorf("platform execution: record the approval's governance decision: %w", err)
	}
	if _, err := a.steps.RecordApprovalGovernance(ctx, ex, governanceInputs{
		tenantID: tenantID, instanceID: instanceID, proposal: req.Start.Proposal,
		planDigest: a.steps.pinnedPlanDigest(req.Start), standing: standing,
	}, nodeID, req.WorkItemID, promotionterminalDecisions{
		ApprovalIDs: recorded.ApprovalIDs, TaskIDs: recorded.TaskIDs,
	}, recordedAt); err != nil {
		return fmt.Errorf("platform execution: record the approval's governance decision: %w", err)
	}
	return nil
}

// withApprovalGovernance chains the caller's own vote evidence with this
// promotion's governance record, so both are appended in the vote
// transaction. A caller with no evidence of its own still records governance.
func (a executeDriverAdapter) withApprovalGovernance(req app.ApprovalVoteRequest) func(context.Context, workitem.Executor, workitem.WorkItem) error {
	caller := req.Record
	return func(ctx context.Context, ex workitem.Executor, item workitem.WorkItem) error {
		if caller != nil {
			if err := caller(ctx, ex, item); err != nil {
				return err
			}
		}
		return a.recordApprovalGovernance(ctx, req, ex)
	}
}

// loadApprovalGovernance reads the instance's most recent approval record.
func loadApprovalGovernance(ctx context.Context, tx governanceExecutor, tenantID, instanceID uuid.UUID) (approvalGovernanceRecord, error) {
	var body []byte
	err := tx.QueryRow(ctx, `
		SELECT record FROM promotion_approval_governance
		WHERE tenant_id = $1 AND instance_id = $2
		ORDER BY recorded_at DESC, work_item_id DESC
		LIMIT 1`, tenantID, instanceID).Scan(&body)
	if errors.Is(err, dbport.ErrNoRows) {
		return approvalGovernanceRecord{}, ErrNoApprovalGovernance
	}
	if err != nil {
		return approvalGovernanceRecord{}, fmt.Errorf("platform execution: read the approval governance record: %w", err)
	}
	var record approvalGovernanceRecord
	if err := json.Unmarshal(body, &record); err != nil {
		return approvalGovernanceRecord{}, fmt.Errorf("platform execution: decode the approval governance record: %w", err)
	}
	return record, nil
}

func allowWhen(ok bool) decision.State {
	if ok {
		return decision.Allow
	}
	return decision.Deny
}

func orUnknown(text string) string {
	if strings.TrimSpace(text) == "" {
		return "UNKNOWN"
	}
	return text
}

// sourceAuthorityDecision is the per-field source-authority decision the
// approved writes were proposed under, bound to the cell's current
// source-authority digest.
func sourceAuthorityDecision(revision intent.ProposalRevision, standing app.GovernanceStanding) string {
	decisions := make([]string, 0, len(revision.Writes))
	seen := map[string]bool{}
	for _, w := range revision.Writes {
		if d := strings.TrimSpace(w.SourceAuthorityDecision); d != "" && !seen[d] {
			seen[d] = true
			decisions = append(decisions, d)
		}
	}
	if len(decisions) == 0 {
		decisions = []string{"UNKNOWN"}
	}
	return orUnknown(standing.SourceAuthorityDigest) + "|" + strings.Join(decisions, ",")
}

// revisionWorkerSubject and positionSubject read the revision's own subjects.
func revisionWorkerSubject(revision intent.ProposalRevision) (uuid.UUID, bool) {
	return subjectID(revision, "EMPLOYMENT")
}

func positionSubject(revision intent.ProposalRevision) (uuid.UUID, bool) {
	return subjectID(revision, "POSITION")
}

func subjectID(revision intent.ProposalRevision, kind string) (uuid.UUID, bool) {
	for _, s := range revision.Subjects {
		if s.Kind != kind {
			continue
		}
		id, err := uuid.Parse(s.SubjectID)
		if err != nil {
			return uuid.Nil, false
		}
		return id, true
	}
	return uuid.Nil, false
}

// occupancyOf names the occupancy row this proposal's own commit writes, so a
// re-read after the commit does not count the promotion against itself.
func occupancyOf(revision intent.ProposalRevision) uuid.UUID {
	proposalID, err := uuid.Parse(revision.ProposalRevisionID)
	if err != nil {
		return uuid.Nil
	}
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte("hcmnext.promotion.position_occupancy:"+proposalID.String()))
}
