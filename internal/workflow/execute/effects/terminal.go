package effects

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	datalogger "github.com/monstercameron/human-capital-management-suite/internal/data/ledger"
	"github.com/monstercameron/human-capital-management-suite/internal/data/outbox"
	"github.com/monstercameron/human-capital-management-suite/internal/data/projection"
	"github.com/monstercameron/human-capital-management-suite/internal/data/promotionguard"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	ledgerport "github.com/monstercameron/human-capital-management-suite/internal/ledger"
	"github.com/monstercameron/human-capital-management-suite/internal/transaction/idempotency"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
)

// PromotionOutcomeSchema names the payload schema [LedgerTerminalWriter]
// records its governed business fact under.
//
// WF-RUN-030 bumped this from /v1 to /v2 when the payload gained the facts a
// downstream consumer needs to act on the promotion from this event alone:
// worker, target placement and effective date (read from the proposal
// revision's own material content, never from the END node's output, which
// this package does not interpret), the END node's own output digest, and
// the approval decision and task submission ids the instance's
// work_item_decision rows recorded (WORK-010). /v1 named only identifiers
// and digests.
const PromotionOutcomeSchema = "hcmnext.workflow.PromotionOutcome/v2"

// promotionOutcomePayload is the canonical (fixed field order) payload one
// terminal write appends to the ledger.
type promotionOutcomePayload struct {
	SchemaRef          string `json:"schema_ref"`
	WorkflowID         string `json:"workflow_id"`
	PlanDigest         string `json:"plan_digest"`
	InstanceID         string `json:"instance_id"`
	ProposalRevisionID string `json:"proposal_revision_id"`
	ProposalDigest     string `json:"proposal_digest"`
	TerminalCode       string `json:"terminal_code"`
	CorrelationID      string `json:"correlation_id"`

	// EndNodeID and EndOutputDigest are the completed END node and its typed
	// output digest, threaded through from [execute.TerminalWriteRequest]
	// rather than discarded (WF-RUN-030's RED finding).
	EndNodeID       string `json:"end_node_id"`
	EndOutputDigest string `json:"end_output_digest"`

	// WorkerRef, TargetPlacement and EffectiveDate are read from the
	// proposal revision's own material content -- the subject the proposal
	// names, its proposed-state assertions and its effective interval -- not
	// from the END node's output, which this package never interprets.
	WorkerRef       string `json:"worker_ref"`
	TargetPlacement string `json:"target_placement,omitempty"`
	EffectiveDate   string `json:"effective_date,omitempty"`

	// ApprovalDecisionIDs and TaskSubmissionIDs are the work_item_decision
	// row ids (WORK-010) recorded for this instance's completed APPROVAL and
	// TASK work items respectively, sorted for a stable payload.
	ApprovalDecisionIDs []string `json:"approval_decision_ids,omitempty"`
	TaskSubmissionIDs   []string `json:"task_submission_ids,omitempty"`
}

// deriveWorkerRef reads the promotion's subject from the proposal revision's
// own material content. A revision naming no subject cannot name a worker,
// and a terminal write that would silently name none instead is refused.
func deriveWorkerRef(rev intent.ProposalRevision) (string, error) {
	if len(rev.Subjects) == 0 || rev.Subjects[0].SubjectID == "" {
		return "", fmt.Errorf("effects: proposal revision %s names no subject to record as the promotion's worker", rev.ProposalRevisionID)
	}
	return rev.Subjects[0].SubjectID, nil
}

// deriveTargetPlacement renders the proposal's proposed-state assertions --
// the fields the promotion actually changes -- as a stable, sorted summary.
// Empty when the revision declares none.
func deriveTargetPlacement(rev intent.ProposalRevision) string {
	if len(rev.ProposedState) == 0 {
		return ""
	}
	parts := make([]string, 0, len(rev.ProposedState))
	for _, a := range rev.ProposedState {
		parts = append(parts, a.ResourceKey.String()+"#"+a.FieldPath+"="+a.CanonicalText)
	}
	sort.Strings(parts)
	return strings.Join(parts, ";")
}

// deriveEffectiveDate reads the proposal's own effective interval start.
// Empty when the interval carries no start instant.
func deriveEffectiveDate(rev intent.ProposalRevision) string {
	start, ok := rev.EffectiveTime.StartInstant()
	if !ok {
		return ""
	}
	return start.String()
}

// deriveEffectiveAt keeps the ledger's bitemporal coordinate aligned with
// the promotion the user reviewed. RecordedAt answers when the fact entered
// the ledger; EffectiveAt answers when the promoted assignment begins. They
// are only the same when a proposal genuinely has no effective interval.
func deriveEffectiveAt(rev intent.ProposalRevision, recordedAt time.Time) time.Time {
	start, ok := rev.EffectiveTime.StartInstant()
	if !ok {
		return recordedAt.UTC()
	}
	return start.Time()
}

// decisionRefsForInstance reads every work_item_decision row WORK-010
// recorded for the instance's completed work items and splits their ids by
// kind. It is this package's own read of internal/humanwork/workitem's
// store, inside the same transaction as the terminal write, never a second
// copy of that evidence.
func decisionRefsForInstance(ctx context.Context, tx dbport.Tx, tenantID, instanceID uuid.UUID) (approvalIDs, taskIDs []string, err error) {
	store := workitem.Store{}
	items, err := store.ListForInstance(ctx, tx, tenantID, instanceID)
	if err != nil {
		return nil, nil, fmt.Errorf("effects: list work items for instance %s: %w", instanceID, err)
	}
	for _, item := range items {
		if item.Status != workitem.StatusCompleted {
			continue
		}
		rec, loadErr := workitem.LoadDecision(ctx, tx, tenantID, item.WorkItemID)
		if loadErr != nil {
			if workitem.CodeOf(loadErr) == workitem.CodeWorkItemNotFound {
				// No WORK-010 decision row exists for this item. That is a
				// gap this write reports rather than papers over.
				return nil, nil, fmt.Errorf("effects: completed work item %s has no recorded decision", item.WorkItemID)
			}
			return nil, nil, fmt.Errorf("effects: load decision for work item %s: %w", item.WorkItemID, loadErr)
		}
		switch rec.Kind {
		case workitem.DecisionKindApproval:
			approvalIDs = append(approvalIDs, rec.DecisionID.String())
		case workitem.DecisionKindTask:
			taskIDs = append(taskIDs, rec.DecisionID.String())
		}
	}
	sort.Strings(approvalIDs)
	sort.Strings(taskIDs)
	return approvalIDs, taskIDs, nil
}

// StreamKeyFor names the ledger stream one workflow instance's governed
// outcome is recorded on: one stream per instance, so two unrelated
// instances never contend on a shared compare-and-swap head.
func StreamKeyFor(workflowID, instanceID string) string {
	return "workflow:" + workflowID + ":" + instanceID
}

// correlationNamespace derives a stable uuid from a free-text correlation
// id, for the one call site (the ledger's CorrelationID column) that wants a
// uuid rather than the string the rest of this call threads through
// unchanged.
var correlationNamespace = uuid.MustParse("6b6e2f2a-2f2e-4e8a-9a3c-6a2b7a9d5c11")

// CorrelationUUID derives the ledger's uuid correlation identifier from the
// same free-text correlation id [runtime.StartRequest.CorrelationID] and
// workflow_instance.correlation_id carry as a semantic_key.
// ledger_event.correlation_id is a uuid column (migrations/00005_ledger.sql)
// while workflow_instance.correlation_id is text
// (migrations/00016_workflow_runtime.sql); this is the one deterministic
// mapping this package's own terminal write derives its ledger event's
// correlation column from, so an operator (or a test) can join the two
// tables on a workflow instance's own correlation id without a second
// lookup:
//
//	SELECT le.*
//	FROM   ledger_event le
//	JOIN   workflow_instance wi
//	  ON   wi.tenant_id = le.tenant_id
//	 AND   le.correlation_id = effects.CorrelationUUID(wi.correlation_id) -- computed client-side; see LookupByCorrelation
//	WHERE  wi.instance_id = $1
//
// An empty correlation id derives [uuid.Nil], matching how this package
// itself has always recorded "no correlation" on the ledger row: it is not
// a fallback a caller should treat as a valid join key.
func CorrelationUUID(correlation string) uuid.UUID {
	if correlation == "" {
		return uuid.Nil
	}
	return uuid.NewSHA1(correlationNamespace, []byte(correlation))
}

// LedgerTerminalWriter is the [execute.TerminalWriter] that performs the one
// governed business write a workflow instance's COMPLETE continuation
// raises: it registers the instance's own ledger stream and projection
// checkpoint (both idempotent, matching internal/intent/app/pgstore's own
// AppendIntent shape) and appends the promotion outcome through
// internal/data/outbox.Commit -- one ledger event, one projection advance,
// one outbox message, all inside tx.
//
// It performs no idempotency reservation of its own: the caller
// (internal/workflow/execute's own continuation sink) already wraps this
// call in [idempotency.Guard], so this writer's own AppendRequest.
// IdempotencyKey only has to make a literal retry of this exact write (same
// ledger idempotency key, same bytes) a safe no-op at the ledger's own
// layer too, belt-and-braces with the guard above it.
type LedgerTerminalWriter struct {
	Appender       ledgerport.Appender
	ProjectionName string
	SourceRef      string
}

var _ execute.TerminalWriter = (*LedgerTerminalWriter)(nil)

// Write implements [execute.TerminalWriter].
func (w *LedgerTerminalWriter) Write(ctx context.Context, tx dbport.Tx, req execute.TerminalWriteRequest) (ret0 idempotency.ResultIdentity, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.effects.ledger_terminal_write", req)
	defer func() { observe.DoneWith(obsOp, retErr, ret0) }()
	if w.Appender == nil {
		return idempotency.ResultIdentity{}, fmt.Errorf("effects: LedgerTerminalWriter has no ledger Appender bound")
	}
	streamKey := StreamKeyFor(req.WorkflowID, req.InstanceID.String())

	if err := datalogger.EnsureStream(ctx, tx, req.TenantID, streamKey, "TRANSACTION", req.InstanceID.String()); err != nil {
		return idempotency.ResultIdentity{}, fmt.Errorf("effects: register workflow ledger stream %s: %w", streamKey, err)
	}
	if err := projection.EnsureProjection(ctx, tx, req.TenantID, w.ProjectionName, streamKey); err != nil {
		return idempotency.ResultIdentity{}, fmt.Errorf("effects: register workflow outcome projection checkpoint: %w", err)
	}
	if err := ensurePayloadSchema(ctx, tx, req.TenantID, PromotionOutcomeSchema); err != nil {
		return idempotency.ResultIdentity{}, fmt.Errorf("effects: register workflow outcome payload schema: %w", err)
	}

	workerRef, err := deriveWorkerRef(req.Proposal.Revision)
	if err != nil {
		return idempotency.ResultIdentity{}, err
	}
	approvalIDs, taskIDs, err := decisionRefsForInstance(ctx, tx, req.TenantID, req.InstanceID)
	if err != nil {
		return idempotency.ResultIdentity{}, err
	}

	payload, err := json.Marshal(promotionOutcomePayload{
		SchemaRef:           PromotionOutcomeSchema,
		WorkflowID:          req.WorkflowID,
		PlanDigest:          req.PlanDigest,
		InstanceID:          req.InstanceID.String(),
		ProposalRevisionID:  req.Proposal.Revision.ProposalRevisionID,
		ProposalDigest:      req.Proposal.Revision.MaterialDigest.Digest,
		TerminalCode:        req.TerminalCode,
		CorrelationID:       req.CorrelationID,
		EndNodeID:           req.EndNodeID,
		EndOutputDigest:     req.EndOutputDigest,
		WorkerRef:           workerRef,
		TargetPlacement:     deriveTargetPlacement(req.Proposal.Revision),
		EffectiveDate:       deriveEffectiveDate(req.Proposal.Revision),
		ApprovalDecisionIDs: approvalIDs,
		TaskSubmissionIDs:   taskIDs,
	})
	if err != nil {
		return idempotency.ResultIdentity{}, fmt.Errorf("effects: encode workflow outcome payload: %w", err)
	}

	receipt, err := outbox.Commit(ctx, tx, w.Appender, outbox.CommitRequest{
		Append: datalogger.AppendRequest{
			Tenant: req.TenantID, StreamKey: streamKey, ExpectedHead: 0,
			AssertionClass: datalogger.TransactionFact,
			SourceRef:      w.SourceRef,
			SchemaRef:      PromotionOutcomeSchema,
			Payload:        payload,
			OccurredAt:     req.RecordedAt,
			EffectiveAt:    deriveEffectiveAt(req.Proposal.Revision, req.RecordedAt),
			CorrelationID:  CorrelationUUID(req.CorrelationID),
			IdempotencyKey: req.IdempotencyKey,
		},
		Projection: outbox.ProjectionSpec{Name: w.ProjectionName},
		Outbox: outbox.OutboxSpec{
			EffectIdentity: "workflow.promotion.apply:" + req.InstanceID.String(),
			OrderingKey:    streamKey,
			SchemaRef:      PromotionOutcomeSchema,
			Payload:        payload,
		},
	})
	if err != nil {
		return idempotency.ResultIdentity{}, fmt.Errorf("effects: commit workflow outcome ledger write: %w", err)
	}
	// The admission guard protects nonterminal promotion work. Release it in
	// this same transaction as the terminal ledger/outbox fact so a crash
	// cannot leave a completed request occupying the worker's window. A
	// synthetic non-UUID intent cannot have a confirmed guard row.
	if intentID, parseErr := uuid.Parse(req.Proposal.Revision.IntentID); parseErr == nil {
		if releaseErr := promotionguard.Release(ctx, tx, req.TenantID, intentID, req.RecordedAt); releaseErr != nil {
			return idempotency.ResultIdentity{}, fmt.Errorf("effects: release terminal promotion admission guard: %w", releaseErr)
		}
	}

	return idempotency.ResultIdentity{
		ResultRef: req.TerminalCode,
		EventRef:  fmt.Sprintf("%s@%d", streamKey, receipt.Ledger.Sequence),
	}, nil
}

// ensurePayloadSchema idempotently registers the outcome schema this writer
// records under, matching internal/intent/app/pgstore's own Bootstrap-time
// registration of its envelope schema: a ledger event's schema_ref is a
// foreign key into payload_schema (migrations/00005_ledger.sql), so a first
// write for a tenant that has never seen this schema needs the row to exist
// before Append can succeed. The registration is content-addressed by
// (tenant_id, schema_ref) and only ever inserted once per tenant.
//
// The ON CONFLICT clause deliberately names no arbiter. payload_schema has two
// unique constraints -- its (tenant_id, schema_ref) key and
// payload_schema_version_unique (tenant_id, schema_id, schema_version) -- and
// this row sets schema_id = schema_ref, so a second registration collides on
// both. A targeted ON CONFLICT (tenant_id, schema_ref) arbitrates only the key:
// when two sessions (two scheduler replicas completing their first instances
// for a tenant at once) both pass the conflict pre-check, the loser raises
// SQLSTATE 23505 on the other constraint, aborting its terminal advance after
// the WAIT it resumed from had already committed. Untargeted DO NOTHING
// arbitrates every unique constraint, so the loser observes the winner's row.
func ensurePayloadSchema(ctx context.Context, tx dbport.Tx, tenant uuid.UUID, schemaRef string) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO payload_schema (
			tenant_id, schema_ref, schema_id, schema_version,
			message_full_name, wire_format, canonicalization_profile)
		VALUES ($1, $2, $2, 1, $2, 'PROTOBUF', 'LEDGER_EVENT')
		ON CONFLICT DO NOTHING`,
		tenant, schemaRef)
	return err
}
