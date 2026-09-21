package execution

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
)

// capabilityEvidenceAdapter adapts a [capability.EvidenceSink] into
// [execute.ExecutionEvidence] (OBS-024 GREEN: "through the existing
// capability evidence sink mechanism").
//
// A sink that already records execution evidence for a storage tenant - every
// [app.EvidenceStore], the durable internal/data/evidencestore and the
// in-memory test double alike - is handed the entry directly, so the row is
// scoped to the tenant the run committed under (WF-RUN-035). A plain
// capability sink has no storage-tenant field; the adapter packs the entry
// with [app.ExecutionEvidenceOf] (Decision carries the kind, SubjectRef
// "<instanceID>|<nodeID>", ReasonCode "<refID>|<digest>") and records it
// there, where it belongs to no tenant.
type capabilityEvidenceAdapter struct {
	sink capability.EvidenceSink
	now  func() time.Time
}

var _ execute.ExecutionEvidence = capabilityEvidenceAdapter{}

// RecordExecutionEvidence implements execute.ExecutionEvidence.
func (a capabilityEvidenceAdapter) RecordExecutionEvidence(
	ctx context.Context, tenantID uuid.UUID, kind, instanceID, nodeID, refID, digest string, occurredAt time.Time,
) (string, error) {
	if occurredAt.IsZero() {
		occurredAt = a.now()
	}
	if tenanted, ok := a.sink.(execute.ExecutionEvidence); ok {
		return tenanted.RecordExecutionEvidence(ctx, tenantID, kind, instanceID, nodeID, refID, digest, occurredAt)
	}
	return a.sink.RecordInvocation(ctx, app.ExecutionEvidenceOf(kind, instanceID, nodeID, refID, digest, occurredAt))
}

var _ execute.ExecutionEvidenceTx = capabilityEvidenceAdapter{}

// RecordExecutionEvidenceTx forwards the in-transaction recording to the
// wrapped sink when it joins transactions itself (the durable store, the
// in-memory double). A plain capability sink has no transaction to join, so
// the advance is refused rather than split across two transactions.
func (a capabilityEvidenceAdapter) RecordExecutionEvidenceTx(
	ctx context.Context, tx dbport.Tx, tenantID uuid.UUID, kind, instanceID, nodeID, refID, digest string, occurredAt time.Time,
) (string, error) {
	if occurredAt.IsZero() {
		occurredAt = a.now()
	}
	if tenanted, ok := a.sink.(execute.ExecutionEvidenceTx); ok {
		return tenanted.RecordExecutionEvidenceTx(ctx, tx, tenantID, kind, instanceID, nodeID, refID, digest, occurredAt)
	}
	return "", fmt.Errorf("%w: capability sink %T", execute.ErrEvidenceNotTransactional, a.sink)
}

// NewCapabilityEvidenceAdapter builds an [execute.ExecutionEvidence] over
// sink and clock, for a caller that composes its own [execute.Driver]
// outside [NewPromotionExecution] (a test wiring its own database/plan, a
// second workflow composition) but still wants OBS-024 evidence recorded
// through the same capability evidence sink mechanism this package's own
// composition uses. A nil clock defaults to time.Now in UTC.
func NewCapabilityEvidenceAdapter(sink capability.EvidenceSink, clock func() time.Time) execute.ExecutionEvidence {
	if clock == nil {
		clock = func() time.Time { return time.Now().UTC() }
	}
	return capabilityEvidenceAdapter{sink: sink, now: clock}
}
