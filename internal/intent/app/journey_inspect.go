package app

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
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
	principal, err := journeyPrincipal(ctx)
	if err != nil {
		return workspace.JourneyDetail{}, err
	}
	got, getErr := e.svc.GetIntent(ctx, &intentsv1.GetIntentRequest{IntentId: intentID})
	if getErr != nil {
		return workspace.JourneyDetail{}, journeyError(getErr)
	}
	if got.GetIntent().GetDefinition().GetIntentTypeId() != promotion.IntentType {
		return workspace.JourneyDetail{}, fmt.Errorf("%w: %s is not a promotion journey",
			workspace.ErrJourneyUnknown, intentID)
	}
	summary, sumErr := journeySummaryFromProto(got.GetIntent())
	if sumErr != nil {
		return workspace.JourneyDetail{}, sumErr
	}

	// The re-simulation is the same read-only path ExecuteIntent itself runs
	// before it will execute anything: it is what proves the proposal revision
	// and material digest the page shows name current content rather than a
	// value remembered from an earlier call.
	artifact, simErr := e.resimulate(ctx, intentID)
	if simErr != nil {
		return workspace.JourneyDetail{}, simErr
	}
	summary.ProposalRevisionID = artifact.GetProposalRevisionId()
	summary.MaterialDigest = artifact.GetMaterialProposalDigest().GetDigest()

	detail := workspace.JourneyDetail{Summary: summary, Approver: e.approver}
	for _, f := range artifact.GetFindings() {
		detail.Findings = append(detail.Findings, workspace.JourneyFinding{
			Severity: f.GetSeverity(), Code: f.GetCode(), Message: f.GetMessage(),
		})
	}
	for _, w := range artifact.GetPlannedWrites() {
		detail.PlannedWrites = append(detail.PlannedWrites, w.GetOperation()+" "+w.GetTargetRef())
	}

	tx, txErr := e.beginTenant(ctx, principal)
	if txErr != nil {
		return workspace.JourneyDetail{}, txErr
	}
	defer func() { _ = tx.Rollback(ctx) }()
	record, recErr := e.readRecord(ctx, tx, principal, summary.MaterialDigest)
	if recErr != nil {
		return workspace.JourneyDetail{}, recErr
	}

	detail.Summary.Stage = deriveJourneyStage(summary.ProposalRevisionID, record)
	detail.Nodes = journeyNodes(record.nodes)
	detail.WorkItems = record.items
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
	detail.Timeline = journeyTimeline(detail)

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
	return detail, nil
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
	if detail.Summary.ProposalRevisionID != "" {
		add(detail.Summary.UpdatedAt, JourneyEventSimulated,
			"Proposal simulated", detail.Summary.MaterialDigest, detail.Summary.ProposalRevisionID)
	}
	if detail.Instance != nil {
		at := detail.Instance.CreatedAt
		if detail.Instance.StartedAt != nil {
			at = *detail.Instance.StartedAt
		}
		add(at, JourneyEventInstanceStarted, "Execution admitted and instance started",
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
	for _, t := range detail.Transitions {
		add(t.At, JourneyEventWorkItem, t.To, t.Reason, t.WorkItemID)
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
