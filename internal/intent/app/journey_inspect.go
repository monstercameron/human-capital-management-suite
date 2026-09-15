package app

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/protomap"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute/effects"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/prototype"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// The read half of the journey engine: how one promotion's durable execution
// record is found, loaded and projected onto [workspace.JourneyDetail].

// journeyRecord is everything durable one journey has, as loaded inside one
// tenant-scoped transaction. Every field is nil/empty before the journey has
// been executed.
type journeyRecord struct {
	instance    *runtime.Instance
	nodes       []runtime.NodeExecution
	items       []workitem.WorkItem
	transitions []workspace.JourneyTransition
	ledger      *workspace.JourneyLedgerEvent
}

// beginTenant opens the read transaction every durable journey read runs
// inside. Every workflow, work-item and ledger table this engine touches is
// row-level-security protected, so the tenant is set for the life of the
// transaction (internal/data/tenancy) rather than filtered in a WHERE clause
// this package could forget.
func (e *journeyEngine) beginTenant(ctx context.Context, principal *trust.Principal) (dbport.Tx, error) {
	if e.db == nil || e.svc.tenantUUID == nil {
		return nil, fmt.Errorf("%w: this cell was composed with no execution database", workspace.ErrJourneyUnavailable)
	}
	tx, err := e.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("app: journey: begin: %w", err)
	}
	if err := tenancy.WithTenant(ctx, tx, e.svc.tenantUUID(principal.Tenant())); err != nil {
		_ = tx.Rollback(ctx)
		return nil, fmt.Errorf("app: journey: scope tenant: %w", err)
	}
	return tx, nil
}

// locateInstance finds the workflow instance one journey is running as.
//
// migrations/00016_workflow_runtime.sql gives workflow_instance no intent
// column: an instance names its tenant, its correlation and its business
// subjects, and its own identity is derived from the start idempotency key
// inside internal/workflow/runtime (derivedStartInstanceID, unexported). The
// durable link back to one intent that does exist is the WorkItem the
// instance raised: work_item.proposal_ref is the material proposal digest,
// which P1A derives from the intent's own identity and canonical request
// digest ([derivedIDs]) and is therefore unique per intent. Two intents with
// byte-identical payloads still mint different digests, so this is an
// identity lookup and not a heuristic.
//
// Correlation is deliberately not used: an in-process caller has no
// transport.Invocation, so [IntentService.specFor] falls back to the
// principal's evidence id, which is the same string for every intent that
// principal creates.
func (e *journeyEngine) locateInstance(
	ctx context.Context, tx dbport.Tx, tenantID uuid.UUID, materialDigest string,
) (uuid.UUID, bool, error) {
	if materialDigest == "" {
		return uuid.Nil, false, nil
	}
	var instanceID uuid.UUID
	row := tx.QueryRow(ctx, `
		SELECT workflow_instance_id FROM work_item
		WHERE tenant_id = $1 AND proposal_ref = $2
		ORDER BY created_at LIMIT 1`, tenantID, materialDigest)
	if err := row.Scan(&instanceID); err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return uuid.Nil, false, nil
		}
		return uuid.Nil, false, fmt.Errorf("app: journey: locate the workflow instance: %w", err)
	}
	return instanceID, true, nil
}

// readRecord loads the complete durable record for one journey, or the zero
// record when the journey has not been executed.
func (e *journeyEngine) readRecord(
	ctx context.Context, tx dbport.Tx, principal *trust.Principal, materialDigest string,
) (journeyRecord, error) {
	tenantID := e.svc.tenantUUID(principal.Tenant())
	instanceID, found, err := e.locateInstance(ctx, tx, tenantID, materialDigest)
	if err != nil || !found {
		return journeyRecord{}, err
	}

	// The durable rows are the same four reads the operator surface's
	// GetWorkflowInstance performs ([loadWorkflowInstanceRecord]); the journey
	// adds only its own projection of the transitions and the ledger read.
	loaded, err := loadWorkflowInstanceRecord(ctx, tx, tenantID, instanceID)
	if err != nil {
		return journeyRecord{}, fmt.Errorf("app: journey: %w", err)
	}
	record := journeyRecord{instance: &loaded.Instance, nodes: loaded.Nodes, items: loaded.WorkItems}
	for _, item := range record.items {
		for _, t := range loaded.Transitions[item.WorkItemID.String()] {
			record.transitions = append(record.transitions, workspace.JourneyTransition{
				WorkItemID: t.WorkItemID.String(),
				From:       string(t.FromStatus),
				To:         string(t.ToStatus),
				Actor:      t.ActorPrincipalID,
				Reason:     t.Reason,
				At:         t.At.UTC(),
			})
		}
	}
	sort.SliceStable(record.transitions, func(i, j int) bool {
		return record.transitions[i].At.Before(record.transitions[j].At)
	})

	if record.ledger, err = e.readLedger(ctx, tx, tenantID, loaded.Instance); err != nil {
		return journeyRecord{}, err
	}
	return record, nil
}

// readExecutedRecordForIntent follows the durable identity chain from an
// intent's immutable proposal revision to the workflow instance that consumed
// it. It deliberately returns found=false until an instance exists: a stored
// proposal without execution is still live draft state, so ListJourneys may
// re-simulate it; once execution exists, historical rendering must use the
// pinned proposal and recorded runtime instead of current rules.
func (e *journeyEngine) readExecutedRecordForIntent(
	ctx context.Context, tx dbport.Tx, principal *trust.Principal, intentID string,
) (materialDigest string, record journeyRecord, found bool, err error) {
	tenantID := e.svc.tenantUUID(principal.Tenant())
	row := tx.QueryRow(ctx, `
		SELECT material_digest
		FROM proposal_revision
		WHERE tenant_id = $1 AND intent_id = $2::uuid
		ORDER BY revision DESC
		LIMIT 1`, tenantID, intentID)
	if scanErr := row.Scan(&materialDigest); scanErr != nil {
		if errors.Is(scanErr, dbport.ErrNoRows) {
			return "", journeyRecord{}, false, nil
		}
		return "", journeyRecord{}, false, fmt.Errorf("app: journey: locate stored proposal for intent: %w", scanErr)
	}
	record, err = e.readRecord(ctx, tx, principal, materialDigest)
	if err != nil {
		return "", journeyRecord{}, false, err
	}
	if record.instance == nil {
		return "", journeyRecord{}, false, nil
	}
	return materialDigest, record, true, nil
}

// readLedger reads the one governed business fact the END node recorded on
// this instance's own ledger stream, or nil when it has not completed.
//
// The stream key is the driver's own ([effects.StreamKeyFor]): one stream per
// instance, so a journey never reads another journey's fact.
func (e *journeyEngine) readLedger(
	ctx context.Context, tx dbport.Tx, tenantID uuid.UUID, instance runtime.Instance,
) (*workspace.JourneyLedgerEvent, error) {
	streamKey := effects.StreamKeyFor(instance.WorkflowID, instance.InstanceID.String())
	var event workspace.JourneyLedgerEvent
	row := tx.QueryRow(ctx, `
		SELECT stream_key, sequence, schema_ref, digest, idempotency_key,
		       occurred_at, effective_at, recorded_at
		FROM ledger_event
		WHERE tenant_id = $1 AND stream_key = $2
		ORDER BY sequence LIMIT 1`, tenantID, streamKey)
	if err := row.Scan(&event.StreamKey, &event.Sequence, &event.SchemaRef, &event.Digest,
		&event.IdempotencyKey, &event.OccurredAt, &event.EffectiveAt, &event.RecordedAt); err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("app: journey: read the ledger fact: %w", err)
	}
	event.OccurredAt = event.OccurredAt.UTC()
	event.EffectiveAt = event.EffectiveAt.UTC()
	event.RecordedAt = event.RecordedAt.UTC()
	return &event, nil
}

// ---------------------------------------------------------------------------
// Stage derivation
// ---------------------------------------------------------------------------

// journeyOpenApprovalStatuses are the WorkItem statuses that mean the approval
// is still somebody's to decide.
var journeyOpenApprovalStatuses = map[workitem.Status]bool{
	workitem.StatusCreated:    true,
	workitem.StatusRouted:     true,
	workitem.StatusAssigned:   true,
	workitem.StatusAvailable:  true,
	workitem.StatusClaimed:    true,
	workitem.StatusInProgress: true,
	workitem.StatusReturned:   true,
	workitem.StatusEscalated:  true,
}

const (
	journeyStageFinanceApproval  workspace.JourneyStage = "FINANCE_APPROVAL"
	journeyStageManagerApproval  workspace.JourneyStage = "MANAGER_APPROVAL"
	journeyStageWaitingEffective workspace.JourneyStage = "WAITING_EFFECTIVE_DATE"
	journeyStageRevalidation     workspace.JourneyStage = "REVALIDATION"
	journeyStageReapproval       workspace.JourneyStage = "REAPPROVAL"
	journeyStageExecuted         workspace.JourneyStage = "EXECUTED"
	journeyStageObservingEffects workspace.JourneyStage = "OBSERVING_EFFECTS"
	journeyStageRecorded         workspace.JourneyStage = "RECORDED"
	journeyStageRepairRequired   workspace.JourneyStage = "REPAIR_REQUIRED"
)

// openApproval returns the journey's undecided approval WorkItem, if any.
func openApproval(items []workitem.WorkItem) (workitem.WorkItem, bool) {
	for _, item := range items {
		if item.Kind == workitem.KindApproval && item.NodeID == prototype.NodeApproval &&
			journeyOpenApprovalStatuses[item.Status] {
			return item, true
		}
	}
	return workitem.WorkItem{}, false
}

func openJourneyWorkItem(items []workitem.WorkItem) (workitem.WorkItem, bool) {
	for _, item := range items {
		if !journeyOpenApprovalStatuses[item.Status] || (item.Kind != workitem.KindApproval && item.Kind != workitem.KindTask) {
			continue
		}
		switch item.NodeID {
		case prototype.NodeApproval, promotionexec.NodeApproveFinance, promotionexec.NodeApproveManager, promotionexec.NodeReapproval:
			return item, true
		}
	}
	return workitem.WorkItem{}, false
}

// approvalReviewRelationships returns the one case-scoped relationship fact
// that lets a manager review a proposal routed to them. A current assignment
// grants review while the decision is pending; a completed item grants the
// same narrow access only to the principal who actually recorded the
// decision. This preserves a reviewer's access to the outcome they helped
// decide without widening their population visibility or trusting a stale
// assignment after reassignment/cancellation. The effective interval starts
// with the case because the governed worker snapshot is read at that instant;
// RecordedAt and KnownAt remain the WorkItem time, so the later assignment can
// never be presented as authority that was known before it existed.
func approvalReviewRelationships(
	principal *trust.Principal,
	subject values.EntityRef,
	caseEffectiveAt values.Instant,
	items []workitem.WorkItem,
) ([]authz.RelationshipFact, error) {
	if principal == nil {
		return nil, nil
	}
	var item workitem.WorkItem
	found := false
	for _, candidate := range items {
		if candidate.NodeID != promotionexec.NodeApproveManager && candidate.NodeID != promotionexec.NodeReapproval {
			continue
		}
		isCurrentOwner := journeyOpenApprovalStatuses[candidate.Status] && candidate.Assignment.ChosenOwner == principal.Subject()
		isRecordedReviewer := candidate.Status == workitem.StatusCompleted && candidate.CompletedBy == principal.Subject()
		if isCurrentOwner || isRecordedReviewer {
			item = candidate
			found = true
			break
		}
	}
	if !found {
		return nil, nil
	}
	if !caseEffectiveAt.IsSet() {
		return nil, fmt.Errorf("app: journey: approval review case effective time is required")
	}
	interval, err := values.NewOpenInstantInterval(caseEffectiveAt)
	if err != nil {
		return nil, fmt.Errorf("app: journey: build approval review interval: %w", err)
	}
	recordedInstant := values.NewInstant(item.RecordedAt.UTC())
	recordedAt, err := values.NewRecordedAt(recordedInstant)
	if err != nil {
		return nil, fmt.Errorf("app: journey: build approval review record time: %w", err)
	}
	knownAt, err := values.NewKnownAt(recordedInstant)
	if err != nil {
		return nil, fmt.Errorf("app: journey: build approval review knowledge time: %w", err)
	}
	return []authz.RelationshipFact{{
		Kind:       authz.RelationshipAssignedPopulation,
		Subject:    subject,
		Source:     "workflow.work_item.assignment.v1",
		Effective:  interval,
		RecordedAt: recordedAt,
		KnownAt:    knownAt,
	}}, nil
}

// journeyStageForNode is the stable page projection for every node in the
// executable promotion graph. Nodes before the first human gate remain
// PROPOSED; this keeps the page vocabulary small while still exposing every
// execution safe point as a named stage once it becomes the frontier.
func journeyStageForNode(nodeID string) (workspace.JourneyStage, bool) {
	switch nodeID {
	case promotionexec.NodeApproveFinance:
		return journeyStageFinanceApproval, true
	case promotionexec.NodeApproveManager:
		return journeyStageManagerApproval, true
	case promotionexec.NodeWaitEffectiveDate:
		return journeyStageWaitingEffective, true
	case promotionexec.NodeRevalidate, promotionexec.NodeStillValid:
		return journeyStageRevalidation, true
	case promotionexec.NodeReapproval:
		return journeyStageReapproval, true
	case promotionexec.NodeExecutePromotion:
		return journeyStageExecuted, true
	case promotionexec.NodeObservePayroll, promotionexec.NodeObserveAccess, promotionexec.NodeObserveReconciliation:
		return journeyStageObservingEffects, true
	case promotionexec.NodeEndComplete:
		return journeyStageRecorded, true
	case promotionexec.NodeEndRepairPlan:
		return journeyStageRepairRequired, true
	case promotionexec.NodeEndBlocked:
		return workspace.JourneyStageBlocked, true
	case promotionexec.NodeEndRejected:
		return workspace.JourneyStageRejected, true
	case promotionexec.NodeEndInvalidated, promotionexec.NodeEndExpired, promotionexec.NodeEndCancelled:
		return workspace.JourneyStageFailed, true
	case promotionexec.NodeSnapshotWorker, promotionexec.NodeSimulateCompensation, promotionexec.NodeEvaluateBand, promotionexec.NodeRaiseThreshold:
		return workspace.JourneyStageProposed, true
	default:
		return "", false
	}
}

// reachedNode reports whether the instance actually ran the named node to
// success.
//
// The status check is load-bearing, not defensive: when a workflow instance
// completes, internal/workflow/runtime records a row for every terminal the
// frontier could have taken -- the one that ran is SUCCEEDED and its siblings
// are SKIPPED. A presence-only check would read a rejected promotion as an
// approved one, because end_approved has a row either way.
func reachedNode(nodes []runtime.NodeExecution, nodeID string) bool {
	for _, n := range nodes {
		if n.NodeID == nodeID && n.Status == runtime.NodeSucceeded {
			return true
		}
	}
	return false
}

// deriveJourneyStage decides where one journey stands, from durable state
// only.
//
// No instance means nothing has been executed: PROPOSED when the simulation
// currently mints a proposal revision to execute, BLOCKED when it does not
// (which is exactly the p1b.no_executable_plan condition ExecuteIntent would
// refuse with). Once an instance exists the terminal the run actually reached
// decides: the approved END is COMPLETED, the rejected END is REJECTED, any
// other terminal is FAILED, and an instance still holding an open approval
// WorkItem is AWAITING_APPROVAL.
func deriveJourneyStage(proposalRevisionID string, record journeyRecord) workspace.JourneyStage {
	if record.instance == nil {
		if proposalRevisionID == "" {
			return workspace.JourneyStageBlocked
		}
		return workspace.JourneyStageProposed
	}
	switch {
	case reachedNode(record.nodes, promotionexec.NodeEndComplete):
		return journeyStageRecorded
	case reachedNode(record.nodes, promotionexec.NodeEndRepairPlan):
		return journeyStageRepairRequired
	case reachedNode(record.nodes, promotionexec.NodeEndBlocked):
		return workspace.JourneyStageBlocked
	case reachedNode(record.nodes, promotionexec.NodeEndRejected), reachedNode(record.nodes, prototype.NodeRejected):
		return workspace.JourneyStageRejected
	case reachedNode(record.nodes, promotionexec.NodeEndInvalidated), reachedNode(record.nodes, promotionexec.NodeEndExpired), reachedNode(record.nodes, promotionexec.NodeEndCancelled),
		reachedNode(record.nodes, prototype.NodeInvalidated), reachedNode(record.nodes, prototype.NodeExpired), reachedNode(record.nodes, prototype.NodeCancelled):
		return workspace.JourneyStageFailed
	case reachedNode(record.nodes, prototype.NodeApproved):
		return workspace.JourneyStageCompleted
	}
	for _, nodeID := range record.instance.CurrentNodeIDs {
		if stage, ok := journeyStageForNode(nodeID); ok {
			return stage
		}
	}
	if item, open := openJourneyWorkItem(record.items); open {
		if stage, ok := journeyStageForNode(item.NodeID); ok {
			return stage
		}
		return workspace.JourneyStageAwaitingApproval
	}
	switch record.instance.RuntimeStatus {
	case runtime.InstanceCreated, runtime.InstanceRunning, runtime.InstanceWaiting:
		return workspace.JourneyStageAwaitingApproval
	default:
		return workspace.JourneyStageFailed
	}
}

// ---------------------------------------------------------------------------
// Inspect
// ---------------------------------------------------------------------------

// Inspect implements [workspace.JourneyEngine].
func (e *journeyEngine) Inspect(ctx context.Context, intentID string) (workspace.JourneyDetail, error) {
	return e.inspectWithRelationships(ctx, intentID, nil)
}

// inspectWithRelationships renders one journey with a relationship that was
// already proven earlier in the same application operation. Decide uses this
// after it closes an assigned manager WorkItem: the close correctly removes
// future case access, but the response to that successful decision must still
// describe the state the operation just committed. The relationship is never
// accepted from transport input and never survives this call.
func (e *journeyEngine) inspectWithRelationships(
	ctx context.Context, intentID string, carried []authz.RelationshipFact,
) (workspace.JourneyDetail, error) {
	principal, err := journeyPrincipal(ctx)
	if err != nil {
		return workspace.JourneyDetail{}, err
	}
	inst, _, ownedErr := e.svc.loadInstance(ctx, principal.Tenant().String(), intentID)
	if ownedErr != nil {
		return workspace.JourneyDetail{}, journeyError(ownedErr)
	}
	if inst.Definition.TypeID != promotion.IntentType {
		return workspace.JourneyDetail{}, fmt.Errorf("%w: %s is not a promotion journey",
			workspace.ErrJourneyUnknown, intentID)
	}
	msg, mapErr := protomap.InstanceToProto(inst)
	if mapErr != nil {
		return workspace.JourneyDetail{}, fmt.Errorf("app: journey: map stored intent: %w", mapErr)
	}
	summary, sumErr := journeySummaryFromProto(msg)
	if sumErr != nil {
		return workspace.JourneyDetail{}, sumErr
	}

	tx, txErr := e.beginTenant(ctx, principal)
	if txErr != nil {
		return workspace.JourneyDetail{}, txErr
	}
	defer func() { _ = tx.Rollback(ctx) }()

	materialDigest, record, executed, recErr := e.readExecutedRecordForIntent(ctx, tx, principal, intentID)
	if recErr != nil {
		return workspace.JourneyDetail{}, recErr
	}
	relationships, relErr := approvalReviewRelationships(principal, summary.Worker, inst.CreatedAt, record.items)
	if relErr != nil {
		return workspace.JourneyDetail{}, relErr
	}
	relationships = append(relationships, managerChainFacts(ctx, e.locate, principal, summary.Worker)...)
	if len(carried) != 0 {
		relationships = append(append([]authz.RelationshipFact(nil), carried...), relationships...)
	}
	_, inv, callerErr := caller(ctx)
	if callerErr != nil {
		return workspace.JourneyDetail{}, journeyError(callerErr)
	}

	// Once execution exists, the stored proposal and runtime are historical
	// authority. Re-simulating against today's worker state or rules can fail
	// after a successful promotion and must not make its audit detail vanish.
	// Unexecuted proposals still need a live simulation for their current
	// findings and material digest.
	var findings []workspace.JourneyFinding
	var plannedWrites []string
	if executed {
		if authErr := authorizeHistoricalJourneyRead(principal, purposeOf(principal, inv), summary.Worker, inst.CreatedAt, relationships); authErr != nil {
			if errors.Is(authErr, ErrAuthorizationDenied) {
				return workspace.JourneyDetail{}, journeyError(authorizationRefusal(authErr))
			}
			return workspace.JourneyDetail{}, fmt.Errorf("app: journey: authorize historical inspection: %w", authErr)
		}
		summary.MaterialDigest = materialDigest
	} else {
		artifact, simErr := e.resimulateWithRelationships(ctx, intentID, relationships)
		if simErr != nil {
			return workspace.JourneyDetail{}, simErr
		}
		summary.ProposalRevisionID = artifact.GetProposalRevisionId()
		summary.MaterialDigest = artifact.GetMaterialProposalDigest().GetDigest()
		for _, f := range artifact.GetFindings() {
			findings = append(findings, workspace.JourneyFinding{
				Severity: f.GetSeverity(), Code: f.GetCode(), Message: f.GetMessage(),
			})
		}
		for _, w := range artifact.GetPlannedWrites() {
			plannedWrites = append(plannedWrites, w.GetOperation()+" "+w.GetTargetRef())
		}
	}

	detail := workspace.JourneyDetail{
		Summary:              summary,
		DiagnosticsAvailable: journeyDiagnosticsAllowed(principal),
		Findings:             findings,
		PlannedWrites:        plannedWrites,
	}

	if !executed {
		record, recErr = e.readRecord(ctx, tx, principal, summary.MaterialDigest)
		if recErr != nil {
			return workspace.JourneyDetail{}, recErr
		}
	}

	detail.Summary.Stage = deriveJourneyStage(summary.ProposalRevisionID, record)
	// PROMOUX-012: the same viewer projection the list resolves. The work item
	// summary is used only for the viewer's own membership; the detail page
	// reads the work items themselves.
	detail.Summary.Viewer = journeyViewerProjection(detail.Summary.Stage,
		isJourneyInitiator(inst.Initiator.PrincipalID, principal.Subject()),
		journeyWorkItemSummary(record.items, principal.Subject(), principal.OrganizationScopeID(), e.now(), nil))
	detail.Nodes = journeyNodes(record.nodes)
	detail.WorkItems = record.items
	if item, open := openJourneyWorkItem(record.items); open && item.Assignment.ChosenOwner != "" {
		detail.CanDecide = item.Assignment.ChosenOwner == principal.Subject()
		detail.Approver = journeyApproverLabel(item, detail.CanDecide)
	}
	detail.Transitions = record.transitions
	detail.Ledger = record.ledger
	if record.instance != nil {
		inst := *record.instance
		detail.Summary.InstanceID = inst.InstanceID.String()
		detail.Summary.InstanceVersion = inst.InstanceVersion
		detail.Instance = &workspace.JourneyInstance{
			InstanceID:      inst.InstanceID.String(),
			InstanceVersion: inst.InstanceVersion,
			WorkflowID:      inst.WorkflowID,
			WorkflowVersion: inst.WorkflowVersion,
			PlanDigest:      inst.CompiledPlanHash,
			Status:          string(inst.RuntimeStatus),
			CurrentNodeIDs:  append([]string(nil), inst.CurrentNodeIDs...),
			CorrelationID:   inst.CorrelationID,
			CreatedAt:       inst.CreatedAt.UTC(),
			StartedAt:       utcPtr(inst.StartedAt),
			CompletedAt:     utcPtr(inst.CompletedAt),
		}
	}
	detail.EvidenceIDs = e.evidenceIDsFor(intentID, detail.Summary.InstanceID)
	applyJourneyChronology(&detail, record)

	// PROMOUX-014: a journey parked on the effective-date wait explains
	// itself -- see wait_explain.go for why Findings is the vessel and what
	// each fact sources from.
	if detail.Summary.Stage == workspace.JourneyStageWaitingEffectiveDate && record.instance != nil {
		tenantID := e.svc.tenantUUID(principal.Tenant())
		waitTimer, waitErr := journeyWaitTimer(ctx, tx, tenantID, record.instance.InstanceID)
		if waitErr != nil {
			return workspace.JourneyDetail{}, waitErr
		}
		detail.Findings = append(detail.Findings, journeyWaitFindings(waitTimer, detail.WorkItems)...)
	}
	if !detail.DiagnosticsAvailable {
		redactJourneyDiagnostics(&detail)
	}
	return detail, nil
}

// authorizeHistoricalJourneyRead applies the same subject and compensation
// field gate as promotion input resolution without rereading mutable worker
// facts. The durable execution remains the only source of historical values.
func authorizeHistoricalJourneyRead(principal *trust.Principal, purpose string, subject values.EntityRef, evaluatedAt values.Instant, relationships []authz.RelationshipFact) error {
	_, err := authorizeRead(principal, purpose, authorizationRequest{
		Subject: subject, EvaluatedAt: evaluatedAt,
		Gate:          []authz.FieldID{authz.FieldBaseSalary, authz.FieldBonusTarget},
		Read:          peopleFields(promotion.RequiredWorkerFields()),
		Relationships: relationships,
	})
	return err
}

// applyJourneyChronology preserves the intent's simulation instant in the
// timeline before the heading adopts the latest durable business transition.
func applyJourneyChronology(detail *workspace.JourneyDetail, record journeyRecord) {
	if detail == nil {
		return
	}
	detail.Timeline = journeyTimeline(*detail)
	applyDurableJourneyTime(&detail.Summary, record)
}

func journeyDiagnosticsAllowed(principal *trust.Principal) bool {
	return principal != nil && (principal.HasRole("hcm_admin") ||
		principal.HasRole(string(authz.RoleCompAdmin)) ||
		principal.HasRole(string(authz.RoleAuditor)))
}

func journeyApproverLabel(item workitem.WorkItem, viewerIsApprover bool) string {
	label := "Assigned reviewer"
	switch item.NodeID {
	case promotionexec.NodeApproveFinance:
		label = "Finance reviewer"
	case promotionexec.NodeApproveManager:
		label = "Manager reviewer"
	}
	if viewerIsApprover {
		return "You · " + label
	}
	return label
}

// redactJourneyDiagnostics removes protocol and execution internals before
// they cross the authority boundary. The ordinary projection retains only
// the business facts needed to understand progress and perform an authorized
// current action; CSS or a client-side role check is never the control.
func redactJourneyDiagnostics(detail *workspace.JourneyDetail) {
	if detail == nil {
		return
	}
	detail.Summary.CorrelationID = ""
	detail.Summary.ProposalRevisionID = ""
	detail.Summary.MaterialDigest = ""
	detail.Summary.InstanceID = ""
	detail.Summary.InstanceVersion = 0
	detail.Summary.Approver = ""
	for i := range detail.Findings {
		// UXAUDIT-002: the WAIT_* codes route the wait explanation to its
		// own section for every viewer the messages already reach. Blanking
		// them hid nothing -- the messages still crossed -- and collapsed
		// the explanation into the generic board for the very approvers
		// waiting on it. Every other code still blanks: only the
		// explanation routing keys survive redaction.
		if !isWaitExplanationFindingCode(detail.Findings[i].Code) {
			detail.Findings[i].Code = ""
		}
	}
	detail.PlannedWrites = nil
	detail.Instance = nil
	detail.Nodes = nil
	detail.Transitions = nil
	detail.EvidenceIDs = nil
	if detail.Ledger != nil {
		detail.Ledger = &workspace.JourneyLedgerEvent{
			EffectiveAt: detail.Ledger.EffectiveAt,
			RecordedAt:  detail.Ledger.RecordedAt,
		}
	}
	items := make([]workitem.WorkItem, 0, len(detail.WorkItems))
	for _, item := range detail.WorkItems {
		if item.Status != workitem.StatusCreated && item.Status != workitem.StatusRouted &&
			item.Status != workitem.StatusAssigned && item.Status != workitem.StatusAvailable &&
			item.Status != workitem.StatusClaimed && item.Status != workitem.StatusInProgress {
			continue
		}
		items = append(items, workitem.WorkItem{
			Kind: item.Kind, WorkType: item.WorkType, Status: item.Status,
			DeadlineAt: item.DeadlineAt, CreatedAt: item.CreatedAt,
		})
	}
	detail.WorkItems = items
	timeline := make([]workspace.JourneyEvent, 0, len(detail.Timeline))
	for _, event := range detail.Timeline {
		if event.Kind == JourneyEventNode {
			continue
		}
		event.Ref = ""
		event.Actor = ""
		switch event.Kind {
		case JourneyEventSimulated, JourneyEventInstanceStarted, JourneyEventWorkItem:
			event.Detail = ""
		}
		timeline = append(timeline, event)
	}
	detail.Timeline = timeline
}

// journeyNodes projects the durable node-execution rows onto the port's shape.
func journeyNodes(nodes []runtime.NodeExecution) []workspace.JourneyNode {
	out := make([]workspace.JourneyNode, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, workspace.JourneyNode{
			NodeID:      n.NodeID,
			Attempt:     n.Attempt,
			StepType:    string(n.StepType),
			Status:      string(n.Status),
			TraceID:     n.TraceID,
			StartedAt:   utcPtr(n.StartedAt),
			CompletedAt: utcPtr(n.CompletedAt),
			RecordedAt:  n.RecordedAt.UTC(),
		})
	}
	return out
}

// evidenceIDsFor returns the evidence this cell recorded for the journey, in
// recording order.
//
// It reads the cell-wide sink [NewCell] wires - the one CAP-002's gateway
// writes capability invocation/refusal evidence to, OBS-024's
// GATE_ADMITTED/GATE_REFUSED entries land on, and (when the composition root
// handed the same sink to internal/platform/execution's
// PromotionExecutionConfig.Evidence, as cmd/hcmnext does) the driver's own
// APPROVAL_COMPLETED/TASK_SUBMITTED/TERMINAL_WRITTEN entries land on too.
// Records are matched on the subject the recorder named: the intent id for
// the authority gate, and the instance id for the driver's execution
// evidence, which its adapter packs as "<instanceID>|<nodeID>" (the
// documented convention of internal/platform/execution's
// capabilityEvidenceAdapter). A cell whose driver was composed with a
// private sink simply has none of the latter to show.
func (e *journeyEngine) evidenceIDsFor(intentID, instanceID string) []string {
	sink, ok := e.svc.evidence.(*MemoryEvidenceSink)
	if !ok || sink == nil {
		return nil
	}
	var out []string
	for _, rec := range sink.Records() {
		if journeyEvidenceMatches(rec.SubjectRef, intentID, instanceID) {
			out = append(out, rec.EvidenceID)
		}
	}
	return out
}

// journeyEvidenceMatches reports whether one recorded subject names the
// journey: the intent itself, its instance, or a node of its instance.
func journeyEvidenceMatches(subject, intentID, instanceID string) bool {
	if intentID != "" && subject == intentID {
		return true
	}
	if instanceID == "" {
		return false
	}
	return subject == instanceID || strings.HasPrefix(subject, instanceID+"|")
}

// utcPtr normalizes an optional instant to UTC without aliasing the source.
func utcPtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	at := t.UTC()
	return &at
}

// ---------------------------------------------------------------------------
// Timeline
// ---------------------------------------------------------------------------

// Timeline entry kinds. They are the engine's own vocabulary for what
// happened, derived from durable rows; the page decides how to show them.
const (
	JourneyEventIntentCreated   = "INTENT_CREATED"
	JourneyEventSimulated       = "SIMULATED"
	JourneyEventInstanceStarted = "INSTANCE_STARTED"
	JourneyEventNode            = "NODE"
	JourneyEventWorkItem        = "WORK_ITEM"
	JourneyEventLedgerRecorded  = "LEDGER_RECORDED"
)

// journeyTimeline composes the chronological account of one journey from the
// durable facts already loaded: nothing here is remembered between calls and
// nothing is asserted that some row does not carry.
//
// Entries are sorted by their own instant, with ties broken by the order the
// engine appends them (intent, simulation, instance, nodes, work items,
// ledger), so two facts stamped at the same pinned instant still read in
// causal order.
func journeyTimeline(detail workspace.JourneyDetail) []workspace.JourneyEvent {
	events := make([]workspace.JourneyEvent, 0, 8+len(detail.Nodes)+len(detail.Transitions))
	add := func(at time.Time, kind, title, detailText, ref string) {
		events = append(events, workspace.JourneyEvent{
			At: at.UTC(), Kind: kind, Title: title, Detail: detailText, Ref: ref,
		})
	}

	add(detail.Summary.CreatedAt, JourneyEventIntentCreated,
		"Promotion proposed", detail.Summary.BusinessReason, detail.Summary.IntentID)
	// An executed journey reads its pinned material digest from the durable
	// proposal revision rather than re-simulating today's worker. That stored
	// revision is sufficient evidence that a proposal simulation completed,
	// even when its presentation ID is unavailable on the historical path.
	if detail.Summary.ProposalRevisionID != "" || detail.Summary.MaterialDigest != "" {
		ref := detail.Summary.ProposalRevisionID
		if ref == "" {
			ref = detail.Summary.MaterialDigest
		}
		add(detail.Summary.UpdatedAt, JourneyEventSimulated,
			"Proposal simulated", detail.Summary.MaterialDigest, ref)
	}
	if detail.Instance != nil && detail.Instance.StartedAt != nil {
		add(*detail.Instance.StartedAt, JourneyEventInstanceStarted, "Execution admitted and instance started",
			detail.Instance.Status, detail.Instance.InstanceID)
	}
	for _, node := range detail.Nodes {
		at := node.RecordedAt
		if node.CompletedAt != nil {
			at = *node.CompletedAt
		} else if node.StartedAt != nil {
			at = *node.StartedAt
		}
		add(at, JourneyEventNode, node.NodeID, node.Status, node.NodeID)
	}
	workItemNodes := make(map[string]string, len(detail.WorkItems))
	for _, item := range detail.WorkItems {
		workItemNodes[item.WorkItemID.String()] = item.NodeID
	}
	for _, t := range detail.Transitions {
		add(t.At, JourneyEventWorkItem, journeyWorkItemEventTitle(workItemNodes[t.WorkItemID], t.To), t.Reason, t.WorkItemID)
		events[len(events)-1].Actor = t.Actor
	}
	if detail.Ledger != nil {
		add(detail.Ledger.RecordedAt, JourneyEventLedgerRecorded,
			"Promotion outcome recorded", detail.Ledger.SchemaRef, detail.Ledger.StreamKey)
	}

	order := map[string]int{
		JourneyEventIntentCreated: 0, JourneyEventSimulated: 1, JourneyEventInstanceStarted: 2,
		JourneyEventNode: 3, JourneyEventWorkItem: 4, JourneyEventLedgerRecorded: 5,
	}
	sort.SliceStable(events, func(i, j int) bool {
		if events[i].At.Equal(events[j].At) {
			return order[events[i].Kind] < order[events[j].Kind]
		}
		return events[i].At.Before(events[j].At)
	})
	return events
}

func journeyWorkItemEventTitle(nodeID, status string) string {
	label := "Approval"
	switch nodeID {
	case promotionexec.NodeApproveFinance:
		label = "Finance review"
	case promotionexec.NodeApproveManager:
		label = "Manager review"
	case promotionexec.NodeReapproval:
		label = "Reapproval"
	}
	switch strings.ToUpper(strings.TrimSpace(status)) {
	case "CREATED", "ROUTED", "ASSIGNED", "AVAILABLE", "OPEN", "READY":
		return label + " assigned"
	case "CLAIMED", "IN_PROGRESS":
		return label + " started"
	case "COMPLETED":
		return label + " completed"
	case "CANCELLED", "CANCELED":
		return label + " cancelled"
	case "EXPIRED":
		return label + " expired"
	default:
		return strings.TrimSpace(status)
	}
}
