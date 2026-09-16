package execute

import (
	"context"
	"time"

	"github.com/google/uuid"
)

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
// The shipped implementation (internal/platform/execution) adapts this
// port onto [capability.EvidenceSink.RecordInvocation] — the same
// in-memory sink CAP-002's gateway already writes every invocation/refusal
// through — so a P1A cell keeps one evidence mechanism, not two (GREEN:
// "through the existing capability evidence sink mechanism"). That target
// is in-memory only in P1A (internal/intent/app.MemoryEvidenceSink; no
// evidence table exists yet), but this interface is itself
// durable-ready: a later phase's real evidence store implements it (or the
// capability.EvidenceSink it is adapted from) directly, with no change to
// any caller here.
type ExecutionEvidence interface {
	RecordExecutionEvidence(ctx context.Context, tenantID uuid.UUID, kind, instanceID, nodeID, refID, digest string, occurredAt time.Time) (evidenceID string, err error)
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
