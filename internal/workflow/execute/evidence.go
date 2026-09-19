package execute

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// ErrEvidenceNotTransactional reports an ExecutionEvidence port that cannot
// record on the advance transaction. Callers that need the entry committed
// beside the outcome refuse the advance rather than recording the entry on
// a second transaction, where it could commit without the outcome or be
// read before the outcome it describes exists.
var ErrEvidenceNotTransactional = errors.New("workflow execute: evidence port cannot record on the advance transaction")

// OBS-024's execution-evidence vocabulary: an enumerated, closed set of
// kinds an ExecutionEvidence entry may name. It is duplicated, rather than
// imported, by internal/intent/app (as app.EvidenceKindGateRefused and
// app.EvidenceKindGateAdmitted) for the same reason
// internal/intent/app.ExecutionResult mirrors this package's own Result
// instead of importing it: internal/intent/app must not import
// internal/workflow/execute (see execution.go's ProposalExecutor doc). A
// later phase that centralizes this vocabulary for the inspector
// (REFACTOR) replaces both copies with one shared package neither of these
// two currently depends on.
const (
	// EvidenceKindGateRefused reports an authority-gate refusal —
	// recorded by internal/intent/app.IntentService.ExecuteIntent, never by
	// this package.
	EvidenceKindGateRefused = "GATE_REFUSED"
	// EvidenceKindGateAdmitted reports an authority-gate admission —
	// recorded by internal/intent/app.IntentService.ExecuteIntent, never by
	// this package.
	EvidenceKindGateAdmitted = "GATE_ADMITTED"
	// EvidenceKindApprovalCompleted reports a completed APPROVAL work item
	// entering [Driver.Resume].
	EvidenceKindApprovalCompleted = "APPROVAL_COMPLETED"
	// EvidenceKindTaskSubmitted reports a completed TASK work item
	// entering [Driver.Resume].
	EvidenceKindTaskSubmitted = "TASK_SUBMITTED"
	// EvidenceKindTerminalWritten reports one governed terminal write
	// ([continuationSink.Complete], through [TerminalWriter.Write]).
	EvidenceKindTerminalWritten = "TERMINAL_WRITTEN"
)

// ExecutionEvidence is OBS-024's port: one execution-evidence entry is
// recorded through it per authority-gate decision, approval completion,
// task submission and terminal write.
//
//   - tenantID is the storage tenant the run committed under - the same
//     identity every advance transaction set as its RLS tenant - so a
//     durable sink scopes the row to the tenant that actually ran it rather
//     than guessing one (WF-RUN-035).
//   - kind is one of the EvidenceKind* constants above.
//   - instanceID and nodeID name what ran; nodeID is empty for a
//     GATE_REFUSED/GATE_ADMITTED entry, recorded before any workflow
//     instance exists.
//   - refID names the work item or decision the entry is about (a
//     workitem.WorkItem.WorkItemID for APPROVAL_COMPLETED/TASK_SUBMITTED,
//     empty for the others in this phase).
//   - digest is the content digest the entry binds — never a payload, an
//     approver identity or authority-bearing baggage.
//   - occurredAt is when it happened.
//
// The served implementation (internal/data/evidencestore over the
// capability_invocation_evidence table) records every entry durably,
// tenant-scoped and append-only. Entries are recorded through
// [ExecutionEvidenceTx] on the advance transaction itself, so an entry
// commits beside the outcome it describes and rolls back with it; an entry
// that cannot commit with its outcome is refused, never recorded on a
// transaction of its own.
type ExecutionEvidence interface {
	RecordExecutionEvidence(ctx context.Context, tenantID uuid.UUID, kind, instanceID, nodeID, refID, digest string, occurredAt time.Time) (evidenceID string, err error)
}

// ExecutionEvidenceTx is the optional atomic half of [ExecutionEvidence]: a
// port that records the entry on the caller's transaction instead of opening
// its own, so the entry commits beside the outcome it describes and rolls
// back with it. The transaction is the driver's, already scoped to the run's
// storage tenant; the port must not re-scope it or commit it.
//
// A port that cannot join the transaction does not implement this interface,
// and every in-transaction recording below refuses the advance with
// [ErrEvidenceNotTransactional] rather than splitting the entry onto a
// second transaction.
type ExecutionEvidenceTx interface {
	RecordExecutionEvidenceTx(ctx context.Context, tx dbport.Tx, tenantID uuid.UUID, kind, instanceID, nodeID, refID, digest string, occurredAt time.Time) (evidenceID string, err error)
}

// AdvanceEvidence decides, once an advancement is decided but before its
// transaction commits, whether an OBS-024 execution-evidence entry is
// recorded beside it in that same transaction. It reports the entry's kind
// and refID; the driver supplies tenant, instance, node and digest from the
// advancement itself, so the entry can never describe a different advance
// than the one it commits with. A false ok records nothing.
type AdvanceEvidence func(advanced runtime.AdvanceReceipt) (kind, refID string, ok bool)

// recordTxEvidence records one entry on tx through port, which must be an
// [ExecutionEvidenceTx]. It is the only way the advance paths below record:
// an entry that cannot commit with its outcome is refused, never recorded
// beside it on a transaction of its own.
func recordTxEvidence(ctx context.Context, tx dbport.Tx, port ExecutionEvidence, tenantID uuid.UUID, kind, instanceID, nodeID, refID, digest string, occurredAt time.Time) (string, error) {
	txport, ok := port.(ExecutionEvidenceTx)
	if !ok {
		return "", fmt.Errorf("%w: %T", ErrEvidenceNotTransactional, port)
	}
	return txport.RecordExecutionEvidenceTx(ctx, tx, tenantID, kind, instanceID, nodeID, refID, digest, occurredAt)
}

// NoopExecutionEvidence is the default [ExecutionEvidence] a [Driver] uses
// when none is configured. It records nothing and mints no evidence id, so
// every composition that predates OBS-024 keeps running unchanged.
type NoopExecutionEvidence struct{}

var _ ExecutionEvidence = NoopExecutionEvidence{}

// RecordExecutionEvidence implements ExecutionEvidence.
func (NoopExecutionEvidence) RecordExecutionEvidence(context.Context, uuid.UUID, string, string, string, string, string, time.Time) (string, error) {
	return "", nil
}

var _ ExecutionEvidenceTx = NoopExecutionEvidence{}

// RecordExecutionEvidenceTx implements ExecutionEvidenceTx as the same
// nothing: there is no entry to join to the transaction.
func (NoopExecutionEvidence) RecordExecutionEvidenceTx(context.Context, dbport.Tx, uuid.UUID, string, string, string, string, string, time.Time) (string, error) {
	return "", nil
}
