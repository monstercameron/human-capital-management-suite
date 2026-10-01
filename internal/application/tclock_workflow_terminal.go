package application

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	datalogger "github.com/monstercameron/human-capital-management-suite/internal/data/ledger"
	ledgerport "github.com/monstercameron/human-capital-management-suite/internal/ledger"
	"github.com/monstercameron/human-capital-management-suite/internal/transaction/idempotency"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/clockpunch"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/clockrepair"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// ClockWorkflowOutcomeSchema is the versioned schema for time workflow
// terminal facts. It is deliberately independent of promotion outcome
// schemas: clock terminals are consumed as time facts by time systems.
const ClockWorkflowOutcomeSchema = "hcmnext.time.workflow.Outcome/v1"

// NewClockWorkflowTerminalWriter builds the production writer with the
// canonical ledger digest registry and PostgreSQL adapters.
func NewClockWorkflowTerminalWriter(sourceRef string, trustedClock ...func() time.Time) (*ClockWorkflowTerminalWriter, error) {
	registry, err := ledgerport.NewLedgerEventDigestRegistry()
	if err != nil {
		return nil, fmt.Errorf("clock terminal: build digest registry: %w", err)
	}
	appender := ledgerport.NewAppender(registry)
	if len(trustedClock) > 0 && trustedClock[0] != nil {
		appender = ledgerport.NewAppenderWithClock(registry, trustedClock[0])
	}
	return &ClockWorkflowTerminalWriter{Appender: appender, Ledger: PostgresClockWorkflowLedger{}, Proof: PostgresClockWorkflowTerminalProof{}, SourceRef: sourceRef}, nil
}

// ClockWorkflowLedger prepares the existing tenant-scoped ledger stream and
// payload registry inside the caller's workflow transaction. Implementations
// must not open, commit, or roll back a transaction.
type ClockWorkflowLedger interface {
	EnsureSchema(context.Context, dbport.Tx, uuid.UUID, string) error
	EnsureStream(context.Context, dbport.Tx, uuid.UUID, string, string, string) error
	CurrentHead(context.Context, dbport.Tx, uuid.UUID, string) (int64, error)
}

// PostgresClockWorkflowLedger prepares the existing generic ledger tables.
type PostgresClockWorkflowLedger struct{}

func (PostgresClockWorkflowLedger) EnsureSchema(ctx context.Context, tx dbport.Tx, tenant uuid.UUID, schemaRef string) error {
	_, err := tx.Exec(ctx, `INSERT INTO payload_schema (tenant_id, schema_ref, schema_id, schema_version, message_full_name, wire_format, canonicalization_profile) VALUES ($1, $2, $2, 1, 'google.protobuf.Struct', 'PROTOBUF', 'LEDGER_EVENT') ON CONFLICT DO NOTHING`, tenant, schemaRef)
	return err
}

func (PostgresClockWorkflowLedger) EnsureStream(ctx context.Context, tx dbport.Tx, tenant uuid.UUID, stream, kind, subject string) error {
	return datalogger.EnsureStream(ctx, tx, tenant, stream, kind, subject)
}

func (PostgresClockWorkflowLedger) CurrentHead(ctx context.Context, tx dbport.Tx, tenant uuid.UUID, stream string) (int64, error) {
	var head int64
	err := tx.QueryRow(ctx, `SELECT head_sequence FROM stream_head WHERE tenant_id=$1 AND stream_key=$2 FOR UPDATE`, tenant, stream).Scan(&head)
	if errors.Is(err, dbport.ErrNoRows) {
		return 0, fmt.Errorf("clock terminal: outcome stream %q is not registered", stream)
	}
	return head, err
}

// ClockWorkflowTerminalProof verifies the authoritative node executions that
// justify a terminal and returns their exact effect references.
type ClockWorkflowTerminalProof interface {
	VerifyTerminal(context.Context, dbport.Tx, execute.TerminalWriteRequest) ([]string, error)
}

// PostgresClockWorkflowTerminalProof proves successful closure from runtime
// rows in the same transaction as the outcome event.
type PostgresClockWorkflowTerminalProof struct{}

func (PostgresClockWorkflowTerminalProof) VerifyTerminal(ctx context.Context, tx dbport.Tx, req execute.TerminalWriteRequest) ([]string, error) {
	var planHash string
	var subjects []string
	if err := tx.QueryRow(ctx, `SELECT compiled_plan_hash, business_subject_refs FROM workflow_instance WHERE tenant_id=$1 AND instance_id=$2`, req.TenantID, req.InstanceID).Scan(&planHash, &subjects); err != nil {
		return nil, fmt.Errorf("clock terminal: read pinned plan: %w", err)
	}
	if planHash != req.PlanDigest {
		return nil, fmt.Errorf("clock terminal: plan digest %q does not match pinned %q", req.PlanDigest, planHash)
	}
	refs := []string{}
	for _, node := range terminalProofNodes(req.WorkflowID, req.TerminalCode) {
		var status, trace string
		var effects []string
		var executionID, capabilityExecutionID string
		var attempt int
		if err := tx.QueryRow(ctx, `SELECT node_execution_id, attempt, status, capability_execution_id, effect_refs, trace_id FROM workflow_node_execution WHERE tenant_id=$1 AND instance_id=$2 AND node_id=$3 AND status='SUCCEEDED' ORDER BY attempt DESC LIMIT 1`, req.TenantID, req.InstanceID, node).Scan(&executionID, &attempt, &status, &capabilityExecutionID, &effects, &trace); err != nil {
			return nil, fmt.Errorf("clock terminal: prove node %s: %w", node, err)
		}
		wantExecutionID := runtime.NodeExecutionID(req.TenantID, req.InstanceID, node, attempt).String()
		if executionID != wantExecutionID || capabilityExecutionID != wantExecutionID || status != "SUCCEEDED" || strings.TrimSpace(trace) == "" || len(effects) == 0 {
			return nil, fmt.Errorf("clock terminal: node %s lacks successful effect evidence", node)
		}
		if err := validateClockEffectRefs(effects, subjects); err != nil {
			return nil, fmt.Errorf("clock terminal: node %s effect proof: %w", node, err)
		}
		refs = append(refs, effects...)
	}
	return refs, nil
}

func validateClockEffectRefs(refs, subjects []string) error {
	validSession := map[string]bool{}
	for _, subject := range subjects {
		if strings.HasPrefix(subject, "time_session:") {
			validSession[subject] = true
		}
	}
	seenSession, seenObservation := false, false
	for _, ref := range refs {
		switch {
		case strings.HasPrefix(ref, "time_session:"):
			if !validSession[ref] {
				return fmt.Errorf("foreign session reference %q", ref)
			}
			seenSession = true
		case strings.HasPrefix(ref, "time_observation:") && len(ref) > len("time_observation:"):
			seenObservation = true
		default:
			return fmt.Errorf("unsupported or empty effect reference %q", ref)
		}
	}
	if !seenSession || !seenObservation {
		return errors.New("effect references must include session and observation")
	}
	return nil
}

func terminalProofNodes(workflowID, code string) []string {
	if workflowID == clockpunch.WorkflowID && code == "TIME_SESSION_CLOSED" {
		return []string{clockpunch.NodeCommitPunch, clockpunch.NodeCommitClockOut}
	}
	if workflowID == clockrepair.WorkflowID && code == "TIME_PUNCH_CORRECTED" {
		return []string{clockrepair.NodeCorrection}
	}
	return nil
}

// ClockWorkflowTerminalWriter writes the canonical terminal fact for the two
// published time workflow families. Idempotency is owned by execute.Driver's
// external Guard; this writer only appends the fact while that guard's
// transaction is open.
type ClockWorkflowTerminalWriter struct {
	Appender  ledgerport.Appender
	Ledger    ClockWorkflowLedger
	Proof     ClockWorkflowTerminalProof
	SourceRef string
}

var _ execute.TerminalWriter = (*ClockWorkflowTerminalWriter)(nil)

// Write appends one immutable time workflow outcome and returns stable replay
// identities for the driver's idempotency record.
func (w *ClockWorkflowTerminalWriter) Write(ctx context.Context, tx dbport.Tx, req execute.TerminalWriteRequest) (idempotency.ResultIdentity, error) {
	if tx == nil || w == nil || w.Appender == nil || w.Ledger == nil || w.Proof == nil {
		return idempotency.ResultIdentity{}, errors.New("clock terminal: transaction, appender and ledger preparation are required")
	}
	if err := validateClockTerminal(req); err != nil {
		return idempotency.ResultIdentity{}, err
	}
	effectRefs, err := w.Proof.VerifyTerminal(ctx, tx, req)
	if err != nil {
		return idempotency.ResultIdentity{}, err
	}
	stream := "workflow:" + req.WorkflowID + ":" + req.InstanceID.String()
	if err := w.Ledger.EnsureSchema(ctx, tx, req.TenantID, ClockWorkflowOutcomeSchema); err != nil {
		return idempotency.ResultIdentity{}, fmt.Errorf("clock terminal: ensure outcome schema: %w", err)
	}
	if err := w.Ledger.EnsureStream(ctx, tx, req.TenantID, stream, "WORKFLOW_INSTANCE", req.InstanceID.String()); err != nil {
		return idempotency.ResultIdentity{}, fmt.Errorf("clock terminal: ensure outcome stream: %w", err)
	}
	head, err := w.Ledger.CurrentHead(ctx, tx, req.TenantID, stream)
	if err != nil {
		return idempotency.ResultIdentity{}, fmt.Errorf("clock terminal: read outcome stream head: %w", err)
	}
	payload, err := marshalClockOutcome(req, effectRefs)
	if err != nil {
		return idempotency.ResultIdentity{}, err
	}
	sourceRef := strings.TrimSpace(w.SourceRef)
	if sourceRef == "" {
		sourceRef = "hcmnext.time.workflow"
	}
	receipt, err := w.Appender.Append(ctx, tx, ledgerport.AppendRequest{
		Tenant: req.TenantID, StreamKey: stream, ExpectedHead: head,
		AssertionClass: ledgerport.TransactionFact, SourceRef: sourceRef,
		SchemaRef: ClockWorkflowOutcomeSchema, Payload: payload,
		OccurredAt: req.RecordedAt.UTC(), EffectiveAt: req.RecordedAt.UTC(),
		CorrelationID: correlationUUID(req.CorrelationID), IdempotencyKey: req.IdempotencyKey,
	})
	if err != nil {
		return idempotency.ResultIdentity{}, fmt.Errorf("clock terminal: append outcome: %w", err)
	}
	return idempotency.ResultIdentity{
		ResultRef: "clock-terminal:" + receipt.EventID.String(),
		EventRef:  fmt.Sprintf("%s@%d", receipt.StreamKey, receipt.Sequence),
	}, nil
}

type clockWorkflowOutcome struct {
	SchemaRef            string    `json:"schema_ref"`
	TenantID             string    `json:"tenant_id"`
	InstanceID           string    `json:"instance_id"`
	WorkflowID           string    `json:"workflow_id"`
	WorkflowName         string    `json:"workflow_name"`
	PlanDigest           string    `json:"plan_digest"`
	TerminalNodeID       string    `json:"terminal_node_id"`
	TerminalCode         string    `json:"terminal_code"`
	TerminalOutputDigest string    `json:"terminal_output_digest"`
	EffectReceiptRefs    []string  `json:"effect_receipt_refs"`
	ProposalRevisionID   string    `json:"proposal_revision_id,omitempty"`
	ProposalDigest       string    `json:"proposal_digest,omitempty"`
	SourceKind           string    `json:"source_kind"`
	CorrelationID        string    `json:"correlation_id"`
	IdempotencyKey       string    `json:"idempotency_key"`
	RecordedAt           time.Time `json:"recorded_at"`
}

func marshalClockOutcome(req execute.TerminalWriteRequest, effectRefs []string) ([]byte, error) {
	proposalID, proposalDigest := "", ""
	if req.Source.Proposal != nil {
		proposalID = req.Source.Proposal.Revision.ProposalRevisionID
		proposalDigest = req.Source.Proposal.Revision.MaterialDigest.Digest
	}
	out := clockWorkflowOutcome{SchemaRef: ClockWorkflowOutcomeSchema, TenantID: req.TenantID.String(), InstanceID: req.InstanceID.String(), WorkflowID: req.WorkflowID, WorkflowName: clockWorkflowName(req.WorkflowID), PlanDigest: req.PlanDigest, TerminalNodeID: req.EndNodeID, TerminalCode: req.TerminalCode, TerminalOutputDigest: req.EndOutputDigest, EffectReceiptRefs: append([]string(nil), effectRefs...), ProposalRevisionID: proposalID, ProposalDigest: proposalDigest, SourceKind: string(req.Source.Kind), CorrelationID: req.CorrelationID, IdempotencyKey: req.IdempotencyKey, RecordedAt: req.RecordedAt.UTC()}
	fields := map[string]any{
		"schema_ref": out.SchemaRef, "tenant_id": out.TenantID, "instance_id": out.InstanceID,
		"workflow_id": out.WorkflowID, "workflow_name": out.WorkflowName, "plan_digest": out.PlanDigest,
		"terminal_node_id": out.TerminalNodeID, "terminal_code": out.TerminalCode,
		"terminal_output_digest": out.TerminalOutputDigest, "effect_receipt_refs": stringSliceAny(out.EffectReceiptRefs),
		"source_kind": out.SourceKind, "correlation_id": out.CorrelationID, "idempotency_key": out.IdempotencyKey,
		"recorded_at": out.RecordedAt.UTC().Format(time.RFC3339Nano),
	}
	if out.ProposalRevisionID != "" {
		fields["proposal_revision_id"] = out.ProposalRevisionID
	}
	if out.ProposalDigest != "" {
		fields["proposal_digest"] = out.ProposalDigest
	}
	msg, err := structpb.NewStruct(fields)
	if err != nil {
		return nil, fmt.Errorf("clock terminal: build outcome struct: %w", err)
	}
	b, err := proto.MarshalOptions{Deterministic: true}.Marshal(msg)
	if err != nil {
		return nil, fmt.Errorf("clock terminal: marshal outcome: %w", err)
	}
	return b, nil
}

func stringSliceAny(values []string) []any {
	out := make([]any, len(values))
	for i, value := range values {
		out[i] = value
	}
	return out
}

func validateClockTerminal(req execute.TerminalWriteRequest) error {
	if req.TenantID == uuid.Nil || req.InstanceID == uuid.Nil || strings.TrimSpace(req.WorkflowID) == "" || strings.TrimSpace(req.PlanDigest) == "" || strings.TrimSpace(req.TerminalCode) == "" || strings.TrimSpace(req.EndNodeID) == "" || strings.TrimSpace(req.EndOutputDigest) == "" || strings.TrimSpace(req.IdempotencyKey) == "" || req.RecordedAt.IsZero() {
		return errors.New("clock terminal: incomplete terminal request")
	}
	if req.WorkflowID != clockpunch.WorkflowID && req.WorkflowID != clockrepair.WorkflowID {
		return fmt.Errorf("clock terminal: unsupported workflow family %q", req.WorkflowID)
	}
	if !clockTerminalCodeAllowed(req.WorkflowID, req.TerminalCode) {
		return fmt.Errorf("clock terminal: terminal code %q is not declared by workflow %q", req.TerminalCode, req.WorkflowID)
	}
	if req.Source.Kind == "" {
		return errors.New("clock terminal: start source kind is required")
	}
	if want := terminalNodeFor(req.WorkflowID, req.TerminalCode); want != req.EndNodeID {
		return fmt.Errorf("clock terminal: node %q does not produce terminal %q; want %q", req.EndNodeID, req.TerminalCode, want)
	}
	return nil
}

func terminalNodeFor(workflowID, code string) string {
	if workflowID == clockpunch.WorkflowID {
		switch code {
		case "TIME_SESSION_CLOSED":
			return clockpunch.NodeClosed
		case "TIME_SESSION_MISSING_OUT":
			return clockpunch.NodeMissingOut
		case "TIME_SESSION_REPAIR_REQUIRED":
			return clockpunch.NodeRepair
		case "TIME_SESSION_CANCELLED":
			return clockpunch.NodeCancelled
		}
	}
	if workflowID == clockrepair.WorkflowID {
		switch code {
		case "TIME_PUNCH_CORRECTED":
			return clockrepair.NodeApproved
		case "TIME_PUNCH_CORRECTION_REJECTED":
			return clockrepair.NodeRejected
		case "TIME_PUNCH_REPAIR_REQUIRED":
			return clockrepair.NodeRepair
		case "TIME_PUNCH_CORRECTION_CONFLICT":
			return clockrepair.NodeConflict
		case "TIME_PUNCH_CLOSED_PERIOD_REOPEN_REQUIRED":
			return clockrepair.NodeClosedPeriod
		case "TIME_PUNCH_CORRECTION_CANCELLED":
			return clockrepair.NodeCancelled
		}
	}
	return ""
}

func clockTerminalCodeAllowed(workflowID, code string) bool {
	if workflowID == clockpunch.WorkflowID {
		switch code {
		case "TIME_SESSION_CLOSED", "TIME_SESSION_MISSING_OUT", "TIME_SESSION_REPAIR_REQUIRED", "TIME_SESSION_CANCELLED":
			return true
		}
	}
	if workflowID == clockrepair.WorkflowID {
		switch code {
		case "TIME_PUNCH_CORRECTED", "TIME_PUNCH_CORRECTION_REJECTED", "TIME_PUNCH_REPAIR_REQUIRED", "TIME_PUNCH_CORRECTION_CONFLICT", "TIME_PUNCH_CLOSED_PERIOD_REOPEN_REQUIRED", "TIME_PUNCH_CORRECTION_CANCELLED":
			return true
		}
	}
	return false
}

func clockWorkflowName(id string) string {
	if id == clockrepair.WorkflowID {
		return "Fix missing punch"
	}
	return "Clock in and clock out"
}

func correlationUUID(value string) uuid.UUID {
	if parsed, err := uuid.Parse(value); err == nil && parsed != uuid.Nil {
		return parsed
	}
	h := sha256.Sum256([]byte(value))
	var id uuid.UUID
	copy(id[:], h[:16])
	id[6] = (id[6] & 0x0f) | 0x50
	id[8] = (id[8] & 0x3f) | 0x80
	return id
}
