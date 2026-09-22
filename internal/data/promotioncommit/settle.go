package promotioncommit

// Terminal settlement: the promotion domain's owned terminal writes.
//
// The workflow plane coordinates a promotion run but never performs its
// governed business writes itself
// (planning/specs/platform-plane-model.md: "Workflow never directly
// mutates domain or projection tables"). [Settler] is the promotion
// settlement capability the workflow terminal effect invokes: it registers
// the outcome payload schema, appends the promotion outcome ledger event
// with its projection checkpoint and outbox message, and releases the
// admission guard and budget hold the run leaves behind -- all inside the
// caller's transaction. The workflow layer records only the typed
// [SettleResult].

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
	"github.com/monstercameron/human-capital-management-suite/internal/data/promotionbudget"
	"github.com/monstercameron/human-capital-management-suite/internal/data/promotionguard"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	ledgerport "github.com/monstercameron/human-capital-management-suite/internal/ledger"
)

// PromotionOutcomeSchema names the payload schema a terminal settlement
// records its governed business fact under.
//
// WF-RUN-030 bumped this from /v1 to /v2 when the payload gained the facts
// a downstream consumer needs to act on the promotion from this event
// alone: worker, target placement and effective date (read from the
// proposal revision's own material content, never from the END node's
// output, which this package does not interpret), the END node's own
// output digest, and the approval decision and task submission ids the
// instance's work_item_decision rows recorded (WORK-010). /v1 named only
// identifiers and digests.
const PromotionOutcomeSchema = "hcmnext.workflow.PromotionOutcome/v2"

// PromotionOutcome is the canonical (fixed field order) payload one
// terminal settlement appends to the ledger.
type PromotionOutcome struct {
	SchemaRef          string `json:"schema_ref"`
	WorkflowID         string `json:"workflow_id"`
	PlanDigest         string `json:"plan_digest"`
	InstanceID         string `json:"instance_id"`
	ProposalRevisionID string `json:"proposal_revision_id"`
	ProposalDigest     string `json:"proposal_digest"`
	TerminalCode       string `json:"terminal_code"`
	CorrelationID      string `json:"correlation_id"`

	// EndNodeID and EndOutputDigest are the completed END node and its
	// typed output digest, threaded through from the terminal request
	// rather than discarded (WF-RUN-030's RED finding).
	EndNodeID       string `json:"end_node_id"`
	EndOutputDigest string `json:"end_output_digest"`

	// WorkerRef, TargetPlacement and EffectiveDate are read from the
	// proposal revision's own material content -- the subject the proposal
	// names, its proposed-state assertions and its effective interval --
	// not from the END node's output, which this package never interprets.
	WorkerRef       string `json:"worker_ref"`
	TargetPlacement string `json:"target_placement,omitempty"`
	EffectiveDate   string `json:"effective_date,omitempty"`

	// ApprovalDecisionIDs and TaskSubmissionIDs are the work_item_decision
	// row ids (WORK-010) the workflow layer resolved for the completed
	// APPROVAL and TASK work items of the settled instance, sorted for a
	// stable payload. The workflow layer reads its own work items; this
	// capability only records the ids it is given.
	ApprovalDecisionIDs []string `json:"approval_decision_ids,omitempty"`
	TaskSubmissionIDs   []string `json:"task_submission_ids,omitempty"`
}

// SettleRequest is the complete terminal settlement input. Proposal is the
// immutable proposal revision the run executed; ApprovalDecisionIDs and
// TaskSubmissionIDs are the workflow layer's own completed work-item
// decision ids for the settled instance.
type SettleRequest struct {
	TenantID            uuid.UUID
	WorkflowID          string
	PlanDigest          string
	InstanceID          uuid.UUID
	Proposal            intent.ProposalRevision
	TerminalCode        string
	CorrelationID       string
	IdempotencyKey      string
	RecordedAt          time.Time
	EndNodeID           string
	EndOutputDigest     string
	ApprovalDecisionIDs []string
	TaskSubmissionIDs   []string
}

// SettleResult is the typed capability result the workflow layer records.
// It names the domain result and the ledger event the settlement appended,
// and carries the stream coordinates a test or operator joins on.
type SettleResult struct {
	ResultRef string
	EventRef  string
	StreamKey string
	Sequence  int64
}

// Settler is the promotion settlement capability. Appender, ProjectionName
// and SourceRef are the ledger coordinates the settlement records under;
// every write happens exactly once inside the transaction the caller
// supplies, which the workflow driver already wraps in
// internal/transaction/idempotency.Guard. Settler performs no idempotency
// reservation of its own: SettleRequest.IdempotencyKey makes a literal
// retry of the exact settlement a safe no-op at the ledger's own layer,
// belt-and-braces with the guard above it.
type Settler struct {
	Appender       ledgerport.Appender
	ProjectionName string
	SourceRef      string
}

// Settle performs the one governed business settlement a workflow
// instance's COMPLETE continuation raises: it registers the instance's own
// ledger stream and projection checkpoint (both idempotent), registers the
// outcome payload schema, appends the promotion outcome through
// internal/data/outbox.Commit -- one ledger event, one projection advance,
// one outbox message -- and releases the admission guard and budget hold
// the run leaves behind, all inside tx.
func (s Settler) Settle(ctx context.Context, tx dbport.Tx, req SettleRequest) (SettleResult, error) {
	if tx == nil {
		return SettleResult{}, fmt.Errorf("promotion settlement: a transaction is required")
	}
	if req.TenantID == uuid.Nil {
		return SettleResult{}, fmt.Errorf("promotion settlement: a tenant is required")
	}
	if s.Appender == nil {
		return SettleResult{}, fmt.Errorf("promotion settlement: no ledger Appender bound")
	}
	streamKey := StreamKeyFor(req.WorkflowID, req.InstanceID.String())

	if err := datalogger.EnsureStream(ctx, tx, req.TenantID, streamKey, "TRANSACTION", req.InstanceID.String()); err != nil {
		return SettleResult{}, fmt.Errorf("promotion settlement: register the settlement ledger stream %s: %w", streamKey, err)
	}
	if err := projection.EnsureProjection(ctx, tx, req.TenantID, s.ProjectionName, streamKey); err != nil {
		return SettleResult{}, fmt.Errorf("promotion settlement: register the settlement outcome projection checkpoint: %w", err)
	}
	if err := EnsureOutcomeSchema(ctx, tx, req.TenantID, PromotionOutcomeSchema); err != nil {
		return SettleResult{}, fmt.Errorf("promotion settlement: register the settlement outcome payload schema: %w", err)
	}

	payload, err := OutcomePayloadJSON(req)
	if err != nil {
		return SettleResult{}, err
	}

	receipt, err := outbox.Commit(ctx, tx, s.Appender, outbox.CommitRequest{
		Append: datalogger.AppendRequest{
			Tenant: req.TenantID, StreamKey: streamKey, ExpectedHead: 0,
			AssertionClass: datalogger.TransactionFact,
			SourceRef:      s.SourceRef,
			SchemaRef:      PromotionOutcomeSchema,
			Payload:        payload,
			OccurredAt:     req.RecordedAt,
			EffectiveAt:    effectiveAt(req.Proposal, req.RecordedAt),
			CorrelationID:  CorrelationUUID(req.CorrelationID),
			IdempotencyKey: req.IdempotencyKey,
		},
		Projection: outbox.ProjectionSpec{Name: s.ProjectionName},
		Outbox: outbox.OutboxSpec{
			EffectIdentity: "workflow.promotion.apply:" + req.InstanceID.String(),
			OrderingKey:    streamKey,
			SchemaRef:      PromotionOutcomeSchema,
			Payload:        payload,
		},
	})
	if err != nil {
		return SettleResult{}, fmt.Errorf("promotion settlement: commit the settlement ledger write: %w", err)
	}
	// The admission guard protects nonterminal promotion work. Release it
	// in this same transaction as the terminal ledger/outbox fact so a
	// crash cannot leave a completed request occupying the worker's
	// window. A synthetic non-UUID intent cannot have a confirmed guard
	// row.
	if intentID, parseErr := uuid.Parse(req.Proposal.IntentID); parseErr == nil {
		if releaseErr := promotionguard.Release(ctx, tx, req.TenantID, intentID, req.RecordedAt); releaseErr != nil {
			return SettleResult{}, fmt.Errorf("promotion settlement: release the settlement admission guard: %w", releaseErr)
		}
	}
	// WF-RUN-034: the budget the proposal held is released in the same
	// transaction as the terminal fact, so a promotion that ends without
	// committing stops occupying its organization's compensation pool. A
	// promotion that did commit already transitioned its reservation to
	// COMMITTED, and a committed reservation is never released here.
	if proposalID, parseErr := uuid.Parse(req.Proposal.ProposalRevisionID); parseErr == nil {
		if _, releaseErr := promotionbudget.ReleaseProposalBudget(ctx, tx, req.TenantID, proposalID, req.RecordedAt); releaseErr != nil {
			return SettleResult{}, fmt.Errorf("promotion settlement: release the settlement budget hold: %w", releaseErr)
		}
	}

	return SettleResult{
		ResultRef: req.TerminalCode,
		EventRef:  fmt.Sprintf("%s@%d", streamKey, receipt.Ledger.Sequence),
		StreamKey: streamKey,
		Sequence:  receipt.Ledger.Sequence,
	}, nil
}

// OutcomePayloadJSON renders the canonical outcome payload bytes for req.
// It is pure: the same request always renders the same bytes, which is
// what lets the golden test pin the exact ledger payload without a
// database.
func OutcomePayloadJSON(req SettleRequest) ([]byte, error) {
	workerRef, err := deriveWorkerRef(req.Proposal)
	if err != nil {
		return nil, err
	}
	payload, err := json.Marshal(PromotionOutcome{
		SchemaRef:           PromotionOutcomeSchema,
		WorkflowID:          req.WorkflowID,
		PlanDigest:          req.PlanDigest,
		InstanceID:          req.InstanceID.String(),
		ProposalRevisionID:  req.Proposal.ProposalRevisionID,
		ProposalDigest:      req.Proposal.MaterialDigest.Digest,
		TerminalCode:        req.TerminalCode,
		CorrelationID:       req.CorrelationID,
		EndNodeID:           req.EndNodeID,
		EndOutputDigest:     req.EndOutputDigest,
		WorkerRef:           workerRef,
		TargetPlacement:     deriveTargetPlacement(req.Proposal),
		EffectiveDate:       deriveEffectiveDate(req.Proposal),
		ApprovalDecisionIDs: req.ApprovalDecisionIDs,
		TaskSubmissionIDs:   req.TaskSubmissionIDs,
	})
	if err != nil {
		return nil, fmt.Errorf("promotion settlement: encode the settlement outcome payload: %w", err)
	}
	return payload, nil
}

// deriveWorkerRef reads the promotion's subject from the proposal
// revision's own material content. A revision naming no subject cannot
// name a worker, and a settlement that would silently name none instead is
// refused.
func deriveWorkerRef(rev intent.ProposalRevision) (string, error) {
	if len(rev.Subjects) == 0 || rev.Subjects[0].SubjectID == "" {
		return "", fmt.Errorf("promotion settlement: proposal revision %s names no subject to record as the promotion's worker", rev.ProposalRevisionID)
	}
	return rev.Subjects[0].SubjectID, nil
}

// deriveTargetPlacement renders the proposal's proposed-state assertions
// -- the fields the promotion actually changes -- as a stable, sorted
// summary. Empty when the revision declares none.
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

// effectiveAt keeps the ledger's bitemporal coordinate aligned with the
// promotion the user reviewed. RecordedAt answers when the fact entered
// the ledger; EffectiveAt answers when the promoted assignment begins.
// They are only the same when a proposal genuinely has no effective
// interval.
func effectiveAt(rev intent.ProposalRevision, recordedAt time.Time) time.Time {
	start, ok := rev.EffectiveTime.StartInstant()
	if !ok {
		return recordedAt.UTC()
	}
	return start.Time()
}

// StreamKeyFor names the ledger stream one workflow instance's governed
// outcome is recorded on: one stream per instance, so two unrelated
// instances never contend on a shared compare-and-swap head.
func StreamKeyFor(workflowID, instanceID string) string {
	return "workflow:" + workflowID + ":" + instanceID
}

// correlationNamespace derives a stable uuid from a free-text correlation
// id, for the one call site (the ledger's CorrelationID column) that wants
// a uuid rather than the string the rest of the settlement threads through
// unchanged.
var correlationNamespace = uuid.MustParse("6b6e2f2a-2f2e-4e8a-9a3c-6a2b7a9d5c11")

// CorrelationUUID derives the ledger's uuid correlation identifier from
// the same free-text correlation id the workflow instance carries as a
// semantic_key. ledger_event.correlation_id is a uuid column
// (migrations/00005_ledger.sql) while workflow_instance.correlation_id is
// text (migrations/00016_workflow_runtime.sql); this is the one
// deterministic mapping the settlement derives its ledger event's
// correlation column from, so an operator (or a test) can join the two
// tables on a workflow instance's own correlation id without a second
// lookup. An empty correlation id derives [uuid.Nil], matching how the
// settlement records "no correlation" on the ledger row: it is not a
// fallback a caller should treat as a valid join key.
func CorrelationUUID(correlation string) uuid.UUID {
	if correlation == "" {
		return uuid.Nil
	}
	return uuid.NewSHA1(correlationNamespace, []byte(correlation))
}

// EnsureOutcomeSchema idempotently registers the outcome schema a
// settlement records under. A ledger event's schema_ref is a foreign key
// into payload_schema (migrations/00005_ledger.sql), so a first settlement
// for a tenant that has never seen this schema needs the row to exist
// before Append can succeed. The registration is content-addressed by
// (tenant_id, schema_ref) and only ever inserted once per tenant.
//
// The ON CONFLICT clause deliberately names no arbiter. payload_schema has
// two unique constraints -- its (tenant_id, schema_ref) key and
// payload_schema_version_unique (tenant_id, schema_id, schema_version) --
// and this row sets schema_id = schema_ref, so a second registration
// collides on both. A targeted ON CONFLICT (tenant_id, schema_ref)
// arbitrates only the key: when two sessions (two scheduler replicas
// completing their first instances for a tenant at once) both pass the
// conflict pre-check, the loser raises SQLSTATE 23505 on the other
// constraint, aborting its terminal advance after the WAIT it resumed from
// had already committed. Untargeted DO NOTHING arbitrates every unique
// constraint, so the loser observes the winner's row.
func EnsureOutcomeSchema(ctx context.Context, tx dbport.Tx, tenant uuid.UUID, schemaRef string) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO payload_schema (
			tenant_id, schema_ref, schema_id, schema_version,
			message_full_name, wire_format, canonicalization_profile)
		VALUES ($1, $2, $2, 1, $2, 'PROTOBUF', 'LEDGER_EVENT')
		ON CONFLICT DO NOTHING`,
		tenant, schemaRef)
	return err
}
