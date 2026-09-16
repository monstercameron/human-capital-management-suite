// Package promotionterminal composes the bounded domain mutation with the
// workflow terminal ledger write. Both receive the same caller-owned
// transaction, so COMPLETE can never be durable without all local Promotion
// successor facts (or vice versa).
//
// This file is the production terminal resolver (PROMOUX-016): it
// materializes the approved proposal's validated, plan-bound commit command
// from approval-frozen material, replacing the test-only closures that stood
// in for it. Resolution is read-only derivation, never a second proposal
// path: the revision row (append-only, digest-bound), its produced instant,
// the recorded decisions, and aggregate history as observed at approval.
package promotionterminal

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/aggregates"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/intentcontrol"
	domaincommit "github.com/monstercameron/human-capital-management-suite/internal/domains/promotion/commit"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transaction"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
)

// Approval field vocabulary, pinned to the simulations that emit it:
// internal/domains/promotion/simassign/simulate.go names the manager and
// occupancy writes, internal/domains/promotion/simcomp/simulate.go names the
// pay write. The resolver matches these exact paths and refuses anything
// else, so a renamed simulation field fails the terminal loudly instead of
// resolving to a silently different command.
const (
	resolvePayFieldPath          = "rewards.compensation.annualized_base_pay"
	resolveManagerIDFieldPath    = "org.manager_relationship.manager_id"
	resolveRelationshipFieldPath = "org.manager_relationship.relationship_id"
	resolveReservationFieldPath  = "position.occupancy.reservation"
	// resolvePlacementJobCodePath and resolvePlacementGradePath are the
	// placement writes the promotion simulation's projection records
	// (internal/intent/app proposalFor prefixes people's assignment fields).
	resolvePlacementJobCodePath = "assignment.assignment.job_code"
	resolvePlacementGradePath   = "assignment.assignment.grade"
)

// Approved effect vocabulary, pinned to the same simulations: the payroll
// sync reports the approved compensation revision, the IAM sync the approved
// assignment revision. The terminal renders exactly these two outbox legs,
// and only when approval authorized the local change each one reports.
const (
	resolveCompensationRevisionKind = "rewards.compensation.revision"
	resolveAssignmentRevisionKind   = "people.assignment.revision"
)

// InstanceDecisions carries the approval and task submission identities a
// terminal instance recorded while it ran.
type InstanceDecisions struct {
	ApprovalIDs []string
	TaskIDs     []string
}

var _ CommandResolver = Resolver{}

// DecisionReader loads a terminal instance's recorded decisions. Production
// reads the work_item_decision rows the served run recorded; tests substitute
// recorded fixtures through this seam.
type DecisionReader interface {
	DecisionsForInstance(ctx context.Context, ex dbport.Querier, tenant, instance uuid.UUID) (InstanceDecisions, error)
}

// WorkItemDecisions is the production DecisionReader: approvals and task
// submissions by workflow instance, kinds exactly as the run records them.
type WorkItemDecisions struct{}

// DecisionsForInstance implements DecisionReader.
func (WorkItemDecisions) DecisionsForInstance(ctx context.Context, ex dbport.Querier, tenant, instance uuid.UUID) (ret0 InstanceDecisions, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.promotion_terminal.decisions_for_instance", tenant, instance)
	defer func() { observe.DoneWith(obsOp, retErr, ret0) }()
	rows, err := ex.Query(ctx, `SELECT decision_id::text, kind FROM work_item_decision WHERE tenant_id=$1 AND workflow_instance_id=$2 ORDER BY kind`, tenant, instance)
	if err != nil {
		return InstanceDecisions{}, fmt.Errorf("promotion terminal: read instance decisions: %w", err)
	}
	defer rows.Close()
	var out InstanceDecisions
	for rows.Next() {
		var id, kind string
		if err := rows.Scan(&id, &kind); err != nil {
			return InstanceDecisions{}, fmt.Errorf("promotion terminal: scan instance decision: %w", err)
		}
		switch kind {
		case "APPROVAL":
			out.ApprovalIDs = append(out.ApprovalIDs, id)
		case "TASK":
			out.TaskIDs = append(out.TaskIDs, id)
		}
	}
	if err := rows.Err(); err != nil {
		return InstanceDecisions{}, fmt.Errorf("promotion terminal: iterate instance decisions: %w", err)
	}
	rows.Close()
	if len(out.TaskIDs) == 0 {
		// WF-RUN-034: the served promotion plan routes no human task before
		// its commit on the normal path (its only TASK node is reapproval).
		// The submission the commit attests is then the governed execution
		// submission itself: the verified principal's ExecuteIntent that
		// runtime.Start pinned durably for this instance (migration 00302).
		// No pinned submission means no submission, and the command's own
		// validation refuses it.
		var evidence string
		err := ex.QueryRow(ctx, `SELECT evidence_ref FROM workflow_execution_delegation WHERE tenant_id=$1 AND instance_id=$2`, tenant, instance).Scan(&evidence)
		switch {
		case err == nil && strings.TrimSpace(evidence) != "":
			out.TaskIDs = append(out.TaskIDs, ExecutionSubmissionID(instance))
		case err != nil && !errors.Is(err, dbport.ErrNoRows):
			return InstanceDecisions{}, fmt.Errorf("promotion terminal: read the execution submission: %w", err)
		}
	}
	return out, nil
}

// ExecutionSubmissionID names an instance's pinned execution submission as a
// task submission identity.
func ExecutionSubmissionID(instance uuid.UUID) string {
	return "execution-submission:" + instance.String()
}

// Resolver materializes the approved proposal's validated, plan-bound commit
// command for a terminal request (PROMOUX-016).
//
// Resolution is read-only derivation from approval-frozen material, never a
// second proposal path: the revision row (append-only, digest-bound), its
// produced instant, the recorded decisions, and aggregate history as observed
// at approval. Every baseline digest the command carries comes from a
// KnownAsOf read at the revision's ProducedAt, so a baseline that moved after
// approval fails the commit's own comparison instead of passing vacuously.
// Identity (which worker, which employment, which position) resolves from
// current rows; integrity comes from the frozen digests plus the writer's own
// re-verification, which refuses on any disagreement.
//
// Three boundaries are named rather than glossed. Position-less promotions
// (PROMOUX-004 made the target position optional) cannot form a command --
// the commit model requires a target position and occupancy -- so the
// resolver refuses them fail-closed. A same-manager promotion's proposal
// pins the unchanged manager as an identical current/proposed state
// assertion pair (the kernel refuses an unchanged write), and the resolver
// accepts exactly that pair (WF-RUN-034); a revision that pins no manager at
// all is still refused instead of inventing the manager from live state and
// attesting a cycle check approval never saw. And the coordinator epoch
// is the composed boundary's own (see Boundary), so a coordinator failover
// requires cell recomposition, the same carried-evidence model the execution
// authority digest uses.
type Resolver struct {
	Revisions    intentcontrol.RevisionStore
	Decisions    DecisionReader
	People       aggregates.PeopleStore
	Organization aggregates.OrganizationStore
	Compensation aggregates.CompensationStore
	// Boundary is the cell's consistency boundary: LOCAL_POSTGRES admission,
	// serializable isolation, the single-database protocol, coordinator epoch
	// included. The resolver resolves the admitted plan against exactly these
	// terms and passes the boundary's own epoch as the observed one.
	Boundary transaction.ConsistencyBoundary
	// AuthorityDigest is the composed execution-authority evidence the commit
	// records; ActorPrincipalID is the authority executing it.
	AuthorityDigest  string
	ActorPrincipalID string
}

// Resolve implements CommandResolver.
func (r Resolver) Resolve(ctx context.Context, tx dbport.Tx, req execute.TerminalWriteRequest) (ret0 domaincommit.Command, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.promotion_terminal.resolve", req)
	defer func() { observe.DoneWith(obsOp, retErr, ret0) }()
	if r.Decisions == nil {
		return domaincommit.Command{}, fmt.Errorf("promotion terminal: no decision reader is composed")
	}
	tenant, instance, proposalID, intentID, revisionNo, err := resolveRequestIdentity(req)
	if err != nil {
		return domaincommit.Command{}, err
	}
	row, err := r.Revisions.Load(ctx, tx, tenant, intentID, revisionNo)
	if err != nil {
		return domaincommit.Command{}, fmt.Errorf("promotion terminal: load approved revision: %w", err)
	}
	if row.MaterialDigest != req.Proposal.Revision.MaterialDigest.Digest {
		return domaincommit.Command{}, fmt.Errorf("%w: stored material digest does not match the terminal request", ErrPlanBinding)
	}
	dto, err := intentcontrol.DecodeFullProposal(row.Payload, digestVerifier{expected: row.MaterialDigest})
	if err != nil {
		return domaincommit.Command{}, fmt.Errorf("promotion terminal: decode approved revision: %w", err)
	}
	if dto.Revision != revisionNo || dto.ProposalRevisionID != proposalID.String() {
		return domaincommit.Command{}, fmt.Errorf("%w: decoded revision does not match the terminal request", ErrPlanBinding)
	}
	// The revision's own tenant is the kernel tenant key the cell minted it
	// under, which equals the physical tenant uuid only in a composition that
	// names them the same way (WF-RUN-034: the served cell's key is
	// "harborcare-demo", not its uuid). Tenant confinement itself comes from
	// the row lookup, which is scoped to the physical tenant, and from the
	// material digest the payload is verified against; this check adds the
	// two comparisons that are meaningful for the spelling at hand.
	if id, parseErr := uuid.Parse(string(dto.Tenant)); parseErr == nil && id != tenant {
		return domaincommit.Command{}, fmt.Errorf("%w: decoded revision names tenant %s, the terminal request names %s",
			ErrPlanBinding, dto.Tenant, tenant)
	} else if parseErr != nil && req.Proposal.Revision.Tenant != "" && dto.Tenant != req.Proposal.Revision.Tenant {
		return domaincommit.Command{}, fmt.Errorf("%w: decoded revision names tenant %q, the running proposal names %q",
			ErrPlanBinding, dto.Tenant, req.Proposal.Revision.Tenant)
	}
	return r.materialize(ctx, tx, tenant, instance, proposalID, req, dto, row.ProducedAt)
}

// digestVerifier checks the decoded revision's embedded digest against the
// stored one: request, row and payload must all name the same material.
type digestVerifier struct {
	expected string
}

func (v digestVerifier) VerifyProposalDigest(rev intent.ProposalRevision) error {
	if rev.MaterialDigest.Digest != v.expected {
		return fmt.Errorf("proposal digest %q does not match stored %q", rev.MaterialDigest.Digest, v.expected)
	}
	return nil
}

var _ intentcontrol.FullProposalVerifier = digestVerifier{}

func resolveRequestIdentity(req execute.TerminalWriteRequest) (tenant, instance, proposalID, intentID uuid.UUID, revisionNo uint64, err error) {
	fail := func(format string, args ...any) (uuid.UUID, uuid.UUID, uuid.UUID, uuid.UUID, uint64, error) {
		return uuid.Nil, uuid.Nil, uuid.Nil, uuid.Nil, 0, fmt.Errorf("%w: "+format, append([]any{ErrPlanBinding}, args...)...)
	}
	if req.TenantID == uuid.Nil {
		return fail("terminal request has no tenant")
	}
	if req.InstanceID == uuid.Nil {
		return fail("terminal request has no workflow instance")
	}
	proposalID, err = uuid.Parse(req.Proposal.Revision.ProposalRevisionID)
	if err != nil {
		return fail("terminal request names no proposal revision: %v", err)
	}
	intentID, err = uuid.Parse(req.Proposal.Revision.IntentID)
	if err != nil {
		return fail("terminal request names no intent: %v", err)
	}
	if strings.TrimSpace(req.Proposal.Revision.MaterialDigest.Digest) == "" {
		return fail("terminal request names no material digest")
	}
	return req.TenantID, req.InstanceID, proposalID, intentID, req.Proposal.Revision.Revision, nil
}

// materialize builds the validated, plan-bound command from the approved
// revision. businessAt is the promotion's effective start; knownAt is the
// revision's produced instant: together they pin exactly what approval saw.
func (r Resolver) materialize(ctx context.Context, tx dbport.Tx, tenant, instance, proposalID uuid.UUID, req execute.TerminalWriteRequest, dto intent.ProposalRevision, knownAt time.Time) (domaincommit.Command, error) {
	businessAt, err := effectiveStart(dto)
	if err != nil {
		return domaincommit.Command{}, err
	}
	workerID, positionID, err := resolveSubjects(dto)
	if err != nil {
		return domaincommit.Command{}, err
	}
	employment, err := r.People.ActiveEmploymentForWorker(ctx, tx, tenant, workerID, businessAt)
	if err != nil {
		return domaincommit.Command{}, fmt.Errorf("promotion terminal: resolve employment: %w", err)
	}
	assignment, err := r.People.PrimaryAssignmentForEmployment(ctx, tx, tenant, employment.EntityID, businessAt)
	if err != nil {
		return domaincommit.Command{}, fmt.Errorf("promotion terminal: resolve assignment: %w", err)
	}
	pkg, err := r.Compensation.ActivePackageForWorker(ctx, tx, tenant, workerID, businessAt)
	if err != nil {
		return domaincommit.Command{}, fmt.Errorf("promotion terminal: resolve package: %w", err)
	}
	base, err := r.Compensation.BasePayComponentForPackage(ctx, tx, tenant, pkg.EntityID, businessAt)
	if err != nil {
		return domaincommit.Command{}, fmt.Errorf("promotion terminal: resolve base pay: %w", err)
	}
	position, err := r.Organization.CurrentJobPosition(ctx, tx, tenant, positionID, businessAt)
	if err != nil {
		return domaincommit.Command{}, fmt.Errorf("promotion terminal: resolve position: %w", err)
	}
	job, err := r.Organization.CurrentJob(ctx, tx, tenant, position.JobRef, businessAt)
	if err != nil {
		return domaincommit.Command{}, fmt.Errorf("promotion terminal: resolve job: %w", err)
	}
	reservation, err := r.Compensation.ReservationForProposal(ctx, tx, tenant, proposalID, businessAt)
	if err != nil {
		return domaincommit.Command{}, fmt.Errorf("promotion terminal: resolve budget reservation: %w", err)
	}
	managerID, relationshipRef, err := r.resolveManager(ctx, tx, tenant, dto, businessAt)
	if err != nil {
		return domaincommit.Command{}, err
	}
	if reservation.ProposalRef == nil {
		return domaincommit.Command{}, fmt.Errorf("%w: budget reservation names no proposal", ErrPlanBinding)
	}
	ancestors, err := r.walkAncestors(ctx, tx, tenant, managerID, businessAt)
	if err != nil {
		return domaincommit.Command{}, err
	}
	pay, err := resolvePay(dto, base.Currency)
	if err != nil {
		return domaincommit.Command{}, err
	}
	frozen, err := r.frozenDigests(ctx, tx, tenant, workerID, employment.EntityID, assignment.EntityID, job.EntityID, positionID, pkg.EntityID, base.EntityID, reservation.EntityID, businessAt, knownAt)
	if err != nil {
		return domaincommit.Command{}, err
	}
	decisions, err := r.Decisions.DecisionsForInstance(ctx, tx, tenant, instance)
	if err != nil {
		return domaincommit.Command{}, fmt.Errorf("promotion terminal: read instance decisions: %w", err)
	}
	cmd := domaincommit.Command{
		TenantID: tenant.String(), ProposalRevisionID: proposalID.String(),
		ProposalDigest: req.Proposal.Revision.MaterialDigest.Digest,
		WorkerID:       workerID.String(), EmploymentID: employment.EntityID.String(), AssignmentID: assignment.EntityID.String(),
		TargetJobID: job.EntityID.String(), TargetJobCode: job.Code, TargetGrade: job.Grade,
		TargetOrganizationID: position.OrganizationRef.String(), TargetPositionID: positionID.String(),
		PositionOccupancyID: deterministicOccupancyID(proposalID), ManagerWorkerID: managerID.String(),
		ManagerRelationshipRef: relationshipRef, ManagerAncestorWorkerIDs: ancestors,
		CompensationPackageID: pkg.EntityID.String(), BasePayComponentID: base.EntityID.String(),
		BasePay: pay, PayFrequency: base.Frequency,
		BudgetReservationID: reservation.EntityID.String(), BudgetReservationRef: reservation.ProposalRef.String(),
		PositionReservationRef: positionReservationRef(dto, proposalID),
		Location:               position.Location, PayZone: assignment.PayZone, FTE: position.CapacityFTE,
		EffectiveAt: businessAt, RecordedAt: time.Now().UTC(),
		ExpectedWorkerDigest: frozen.worker, ExpectedEmploymentDigest: frozen.employment,
		ExpectedAssignmentDigest: frozen.assignment, ExpectedJobDigest: frozen.job,
		ExpectedPositionDigest: frozen.position, ExpectedPackageDigest: frozen.pkg,
		ExpectedBasePayDigest: frozen.base, ExpectedBudgetDigest: frozen.budget,
		ApprovalDecisionIDs: append([]string{req.Proposal.ApprovalRef}, decisions.ApprovalIDs...),
		TaskSubmissionIDs:   decisions.TaskIDs,
		AuthorityDigest:     r.AuthorityDigest, ActorPrincipalID: r.ActorPrincipalID,
		WorkflowPlanDigest: req.PlanDigest,
		Effects:            renderEffects(proposalID),
	}
	if err := matchApprovedEffects(dto, cmd.Effects); err != nil {
		return domaincommit.Command{}, err
	}
	plan := intent.TransactionPlan{
		PlanID: "promotion.transaction/" + proposalID.String(),
		Tenant: values.TenantId(tenant.String()),
	}
	plan.Participants = append(plan.Participants, []intent.PlanParticipant{
		{ParticipantID: domaincommit.ParticipantAssignment, StreamID: "people.assignment/" + assignment.EntityID.String(), StorageClass: "LOCAL_POSTGRES", Local: true},
		{ParticipantID: domaincommit.ParticipantOccupancy, StreamID: "position.occupancy/" + positionID.String(), StorageClass: "LOCAL_POSTGRES", Local: true},
		{ParticipantID: domaincommit.ParticipantCompensation, StreamID: "rewards.compensation/" + pkg.EntityID.String(), StorageClass: "LOCAL_POSTGRES", Local: true},
		{ParticipantID: domaincommit.ParticipantBudget, StreamID: "rewards.budget/" + reservation.EntityID.String(), StorageClass: "LOCAL_POSTGRES", Local: true},
	}...)
	// Remote legs are named by their outbox effect identity: BindResolution
	// admits only locals and matches each remote leg to exactly one declared
	// effect, so the commit cannot drop an outbox leg or invoke an
	// undeclared one.
	for _, e := range cmd.Effects {
		plan.Participants = append(plan.Participants, intent.PlanParticipant{
			ParticipantID: e.EffectID, StreamID: "external/" + e.EffectID, StorageClass: "REMOTE", Local: false,
		})
	}
	resolution, err := transaction.ResolveConsistencyBoundary(r.Boundary, plan, r.Boundary.CoordinatorEpoch)
	if err != nil {
		return domaincommit.Command{}, fmt.Errorf("promotion terminal: resolve transaction plan: %w", err)
	}
	bound, err := BindResolution(resolution, cmd)
	if err != nil {
		return domaincommit.Command{}, fmt.Errorf("promotion terminal: bind transaction plan: %w", err)
	}
	return bound, nil
}

// deterministicOccupancyID names the occupancy row for a proposal: stable
// across retries and replays of the same terminal write, unique across
// proposals by construction. Retries that committed nothing find no row;
// replays after a commit fail on the moved baselines before writing. Races
// between two writers for one proposal are fenced by the commit's assignment
// advisory lock, so the shared name can never double-write.
func deterministicOccupancyID(proposalID uuid.UUID) string {
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte("hcmnext.promotion.position_occupancy:"+proposalID.String())).String()
}

// effectiveStart returns the promotion's effective instant: the start of the
// approved effective interval, UTC midnight. Resolver and writer read history
// at exactly this instant, so identity and baselines observe the same rows.
func effectiveStart(dto intent.ProposalRevision) (time.Time, error) {
	return EffectiveStart(dto.EffectiveTime)
}

// EffectiveStart is the promotion's effective business instant for either
// kind of approved interval: the start date's UTC midnight for a LOCAL_DATE
// interval, and the start instant's own UTC day for an INSTANT one (the
// served proposal path mints instant intervals). Both resolve to the same
// day-aligned coordinate every effective-dated read and write in the commit
// uses, so an approval and its commit never observe different rows for the
// same promotion.
func EffectiveStart(interval values.EffectiveInterval) (time.Time, error) {
	if start, ok := interval.StartDate(); ok {
		return time.Date(int(start.Year()), start.Month(), int(start.Day()), 0, 0, 0, 0, time.UTC), nil
	}
	if start, ok := interval.StartInstant(); ok {
		at := start.Time().UTC()
		return time.Date(at.Year(), at.Month(), at.Day(), 0, 0, 0, 0, time.UTC), nil
	}
	return time.Time{}, fmt.Errorf("%w: approved revision names no effective start", ErrPlanBinding)
}

// resolveSubjects extracts the promotion subject (EMPLOYMENT names the worker
// by id) and the target (POSITION names the position by id). A promotion
// without an approved target position cannot form a commit command -- the
// commit model requires a target position and an occupancy row -- so
// position-less promotions are refused fail-closed rather than resolved into
// a weaker mutation.
func resolveSubjects(dto intent.ProposalRevision) (workerID, positionID uuid.UUID, err error) {
	for _, s := range dto.Subjects {
		switch s.Kind {
		case "EMPLOYMENT":
			workerID, err = uuid.Parse(s.SubjectID)
			if err != nil {
				return uuid.Nil, uuid.Nil, fmt.Errorf("%w: subject %q names no worker: %v", ErrPlanBinding, s.SubjectID, err)
			}
		case "POSITION":
			positionID, err = uuid.Parse(s.SubjectID)
			if err != nil {
				return uuid.Nil, uuid.Nil, fmt.Errorf("%w: subject %q names no position: %v", ErrPlanBinding, s.SubjectID, err)
			}
		}
	}
	if workerID == uuid.Nil {
		return uuid.Nil, uuid.Nil, fmt.Errorf("%w: approved revision names no EMPLOYMENT subject", ErrPlanBinding)
	}
	if positionID == uuid.Nil {
		return uuid.Nil, uuid.Nil, fmt.Errorf("%w: approved revision names no POSITION target; position-less promotions cannot commit", ErrPlanBinding)
	}
	return workerID, positionID, nil
}

// resolveManager reads the approved manager writes: the manager's worker
// identity (a UUID a live worker owns at effectivity, else a worker-number
// alias the writer can provenance) and the relationship reference recorded
// verbatim. An unchanged manager leaves no worker identity in the frozen
// material -- the simulation omits unchanged writes -- so the resolver
// refuses rather than inventing the manager from live state.
func (r Resolver) resolveManager(ctx context.Context, tx dbport.Tx, tenant uuid.UUID, dto intent.ProposalRevision, at time.Time) (uuid.UUID, string, error) {
	var managerText, relationship string
	for _, w := range dto.Writes {
		switch w.FieldPath {
		case resolveManagerIDFieldPath:
			managerText = w.ProposedCanonicalText
		case resolveRelationshipFieldPath:
			relationship = w.ProposedCanonicalText
		}
	}
	// WF-RUN-034: a promotion that keeps the worker's manager cannot carry a
	// manager write (the kernel refuses a write whose proposed text equals
	// its current text), so its proposal pins the unchanged manager as a
	// current/proposed state assertion pair. That pair is approval-frozen
	// material, bound by the revision digest exactly as a write is, so the
	// resolver accepts it -- but only when both sides name the same manager:
	// a one-sided or disagreeing pair is not an approved unchanged manager.
	if strings.TrimSpace(managerText) == "" {
		managerText = unchangedAssertion(dto, resolveManagerIDFieldPath)
	}
	if strings.TrimSpace(relationship) == "" {
		relationship = unchangedAssertion(dto, resolveRelationshipFieldPath)
	}
	if strings.TrimSpace(managerText) == "" {
		return uuid.Nil, "", fmt.Errorf("%w: approved revision names no manager", ErrPlanBinding)
	}
	if strings.TrimSpace(relationship) == "" {
		return uuid.Nil, "", fmt.Errorf("%w: approved revision names no manager relationship", ErrPlanBinding)
	}
	if id, err := uuid.Parse(managerText); err == nil {
		if _, err := r.People.CurrentWorker(ctx, tx, tenant, id, at); err != nil {
			return uuid.Nil, "", fmt.Errorf("promotion terminal: resolve manager: %w", err)
		}
		return id, relationship, nil
	}
	manager, err := r.People.WorkerByRef(ctx, tx, tenant, managerText, at)
	if err != nil {
		return uuid.Nil, "", fmt.Errorf("promotion terminal: resolve manager: %w", err)
	}
	return manager.EntityID, relationship, nil
}

// unchangedAssertion returns the text an approved revision pins for field
// when its current and proposed state both assert it identically, and "" for
// an absent, one-sided or changed assertion.
func unchangedAssertion(dto intent.ProposalRevision, field string) string {
	current, proposed := assertionText(dto.CurrentState, field), assertionText(dto.ProposedState, field)
	if current == nil || proposed == nil || *current != *proposed {
		return ""
	}
	return *current
}

// assertionText returns the single assertion text for field, or nil when the
// field is not asserted exactly once.
func assertionText(assertions []intent.StateAssertion, field string) *string {
	var found *string
	for i := range assertions {
		if assertions[i].FieldPath != field {
			continue
		}
		if found != nil {
			return nil
		}
		text := assertions[i].CanonicalText
		found = &text
	}
	return found
}

// walkAncestors records the manager chain above managerID for the commit's
// cycle attestation: each hop's current primary assignment names its own
// manager, up to 16 hops. The walk stops at the first manager with no
// further assignment (the top of the recorded world) and refuses on a cycle
// or a chain longer than the attestation bound instead of truncating it.
// These are contextual audit reads at effectivity, not frozen baselines: the
// cycle invariant itself is re-checked by the command's own validation.
func (r Resolver) walkAncestors(ctx context.Context, tx dbport.Tx, tenant, managerID uuid.UUID, at time.Time) ([]string, error) {
	var ancestors []string
	seen := map[uuid.UUID]bool{}
	current := managerID
	for range 16 {
		if seen[current] {
			return nil, fmt.Errorf("promotion terminal: manager chain cycles at %s", current)
		}
		seen[current] = true
		ancestors = append(ancestors, current.String())
		employment, err := r.People.ActiveEmploymentForWorker(ctx, tx, tenant, current, at)
		if err != nil {
			return ancestors, nil
		}
		assignment, err := r.People.PrimaryAssignmentForEmployment(ctx, tx, tenant, employment.EntityID, at)
		if err != nil {
			return ancestors, nil
		}
		next := strings.TrimSpace(assignment.ManagerRelationshipRef)
		if next == "" {
			return ancestors, nil
		}
		id, err := uuid.Parse(next)
		if err != nil {
			manager, err := r.People.WorkerByRef(ctx, tx, tenant, next, at)
			if err != nil {
				return nil, fmt.Errorf("promotion terminal: resolve ancestor manager %q: %w", next, err)
			}
			id = manager.EntityID
		}
		current = id
	}
	return nil, fmt.Errorf("promotion terminal: manager chain exceeds 16 hops")
}

// resolvePay parses the approved annualized base pay exactly: the canonical
// text against the current component's currency, scale 2, exact rounding, so
// a fraction of a cent refuses instead of rounding into a different raise.
func resolvePay(dto intent.ProposalRevision, currency string) (values.Money, error) {
	for _, w := range dto.Writes {
		if w.FieldPath != resolvePayFieldPath {
			continue
		}
		pay, err := values.NewMoney(w.ProposedCanonicalText, currency, 2, values.RoundingExactRequired)
		if err != nil {
			return values.Money{}, fmt.Errorf("promotion terminal: resolve approved pay: %w", err)
		}
		return pay, nil
	}
	return values.Money{}, fmt.Errorf("%w: approved revision names no base pay", ErrPlanBinding)
}

// frozen holds the approval-frozen baseline digests a command carries.
type frozen struct {
	worker, employment, assignment, job, position, pkg, base, budget string
}

// frozenDigests reads every baseline the commit compares, KnownAsOf the
// revision's produced instant: digests of exactly what approval saw. A
// baseline that moved after approval fails the commit's own comparison
// instead of passing vacuously.
func (r Resolver) frozenDigests(ctx context.Context, tx dbport.Tx, tenant, workerID, employmentID, assignmentID, jobID, positionID, packageID, baseID, reservationID uuid.UUID, at, knownAt time.Time) (frozen, error) {
	var out frozen
	worker, err := r.People.KnownAsOfWorker(ctx, tx, tenant, workerID, at, knownAt)
	if err != nil {
		return frozen{}, fmt.Errorf("promotion terminal: pin worker baseline: %w", err)
	}
	out.worker = worker.Digest
	employment, err := r.People.KnownAsOfEmployment(ctx, tx, tenant, employmentID, at, knownAt)
	if err != nil {
		return frozen{}, fmt.Errorf("promotion terminal: pin employment baseline: %w", err)
	}
	out.employment = employment.Digest
	assignment, err := r.People.KnownAsOfAssignment(ctx, tx, tenant, assignmentID, at, knownAt)
	if err != nil {
		return frozen{}, fmt.Errorf("promotion terminal: pin assignment baseline: %w", err)
	}
	out.assignment = assignment.Digest
	job, err := r.Organization.KnownAsOfJob(ctx, tx, tenant, jobID, at, knownAt)
	if err != nil {
		return frozen{}, fmt.Errorf("promotion terminal: pin job baseline: %w", err)
	}
	out.job = job.Digest
	position, err := r.Organization.KnownAsOfJobPosition(ctx, tx, tenant, positionID, at, knownAt)
	if err != nil {
		return frozen{}, fmt.Errorf("promotion terminal: pin position baseline: %w", err)
	}
	out.position = position.Digest
	pkg, err := r.Compensation.KnownAsOfCompensationPackage(ctx, tx, tenant, packageID, at, knownAt)
	if err != nil {
		return frozen{}, fmt.Errorf("promotion terminal: pin package baseline: %w", err)
	}
	out.pkg = pkg.Digest
	base, err := r.Compensation.KnownAsOfCompensationComponent(ctx, tx, tenant, baseID, at, knownAt)
	if err != nil {
		return frozen{}, fmt.Errorf("promotion terminal: pin base pay baseline: %w", err)
	}
	out.base = base.Digest
	budget, err := r.Compensation.KnownAsOfBudgetReservation(ctx, tx, tenant, reservationID, at, knownAt)
	if err != nil {
		return frozen{}, fmt.Errorf("promotion terminal: pin budget baseline: %w", err)
	}
	out.budget = budget.Digest
	return out, nil
}

// positionReservationRef carries the approved position reservation reference:
// the simulation's After text when the revision records one (the proposal
// identity the hold was cut for), otherwise the proposal identity itself,
// which names the hold the same way. The command requires a reference, so
// there is no empty branch.
func positionReservationRef(dto intent.ProposalRevision, proposalID uuid.UUID) string {
	for _, w := range dto.Writes {
		if w.FieldPath == resolveReservationFieldPath && strings.TrimSpace(w.ProposedCanonicalText) != "" {
			return w.ProposedCanonicalText
		}
	}
	return proposalID.String()
}

// renderEffects builds the commit's two outbox legs byte-for-byte in the
// shape the promotion writer persists (see
// internal/data/promotioncommit/writer_test.go): deterministic identities per
// proposal, so a replayed terminal write enqueues idempotently instead of
// duplicating downstream consequences.
func renderEffects(proposalID uuid.UUID) []domaincommit.ExternalEffect {
	return []domaincommit.ExternalEffect{
		{EffectID: "payroll:" + proposalID.String(), DestinationRef: "payroll", SchemaRef: PayrollEffectSchemaRef, Payload: []byte(`{"kind":"PAYROLL_SYNC"}`)},
		{EffectID: "iam:" + proposalID.String(), DestinationRef: "iam", SchemaRef: IAMEffectSchemaRef, Payload: []byte(`{"kind":"IAM_SYNC"}`)},
	}
}

// The payload schemas the two rendered outbox legs are published under. A
// composition that commits promotions registers exactly these for its tenant
// (internal/data/promotioncommit.RegisterEffectSchemas); the outbox's foreign
// key then refuses any other schema a command might name.
const (
	PayrollEffectSchemaRef = "hcmnext.promotion.payroll/v1"
	IAMEffectSchemaRef     = "hcmnext.promotion.iam/v1"
)

// EffectSchemaRefs are the payload schemas this resolver renders.
func EffectSchemaRefs() []string { return []string{PayrollEffectSchemaRef, IAMEffectSchemaRef} }

// matchApprovedEffects requires the approved revision to carry the local
// effects whose consequences the two syncs report: the terminal may only emit
// what approval authorized. A revision with no compensation change authorizes
// no payroll sync; one with no placement change authorizes no IAM sync.
//
// WF-RUN-034: a revision whose definition is ZERO_EFFECT in its scheduled
// release may not declare effects at all (the kernel refuses it), so for such
// a revision the approved local writes themselves are the authorization: an
// approved annualized base pay write authorizes the payroll sync, and an
// approved placement write (job code or grade) authorizes the IAM sync. A
// revision that approved neither the effect kind nor the write it reports is
// still refused.
func matchApprovedEffects(dto intent.ProposalRevision, rendered []domaincommit.ExternalEffect) error {
	kinds := map[string]bool{}
	for _, e := range dto.Effects {
		kinds[e.Kind] = true
	}
	for _, w := range dto.Writes {
		switch w.FieldPath {
		case resolvePayFieldPath:
			kinds[resolveCompensationRevisionKind] = true
		case resolvePlacementJobCodePath, resolvePlacementGradePath:
			kinds[resolveAssignmentRevisionKind] = true
		}
	}
	if !kinds[resolveCompensationRevisionKind] {
		return fmt.Errorf("%w: approved revision carries no compensation revision for the payroll sync", ErrPlanBinding)
	}
	if !kinds[resolveAssignmentRevisionKind] {
		return fmt.Errorf("%w: approved revision carries no assignment revision for the IAM sync", ErrPlanBinding)
	}
	if len(rendered) != 2 {
		return fmt.Errorf("%w: terminal renders %d effects, want exactly the payroll and IAM syncs", ErrPlanBinding, len(rendered))
	}
	return nil
}
