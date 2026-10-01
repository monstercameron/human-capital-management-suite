package clockservice

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/frontier"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// PunchCommitter is the time-plane effect owned by the composition root. It
// must be idempotent for Observation.IdempotencyKey and return only after the
// authoritative observation/session/outbox transaction commits.
type PunchCommitter interface {
	CommitPunch(context.Context, string, PunchWork) (PunchResult, error)
}

// PunchCommitStepRunner executes the authoritative first node for one fixed,
// request-scoped PunchWork. A new runner must be constructed per runtime start;
// no mutable work is shared through global state or context values.
type PunchCommitStepRunner struct {
	Tenant   string
	TenantID uuid.UUID
	Work     PunchWork
	Commit   PunchCommitter
	NodeID   string
}

// RunsInTransaction is false because the time-plane store is a separate
// database boundary from the workflow runtime transaction. The effect is
// committed first and the runtime records its outcome afterward; retries are
// made safe by PunchWork's durable idempotency key.
func (r PunchCommitStepRunner) RunsInTransaction(node workflow.CompiledNode) bool {
	return false
}

// Run commits the idempotent time-plane effect and returns its durable references.
func (r PunchCommitStepRunner) Run(ctx context.Context, req execute.StepRequest) (frontier.NodeOutcome, runtime.GovernanceRefs, error) {
	if r.Commit == nil || r.NodeID == "" || req.Node.ID != r.NodeID || r.Tenant == "" || r.TenantID == uuid.Nil || req.TenantID != r.TenantID || req.InstanceID == uuid.Nil || req.Attempt < 1 || req.Node.EffectRole != workflow.RoleAuthoritativeCore || r.Work.Observation.TenantID != r.Tenant || r.Work.Session.TenantID != r.Tenant || r.Work.Observation.ID == "" || r.Work.Session.ID == "" {
		return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, errors.New("clock workflow: invalid commit step request")
	}
	if _, err := r.Commit.CommitPunch(ctx, r.Tenant, r.Work); err != nil {
		return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, err
	}
	refs := runtime.GovernanceRefs{
		CapabilityExecutionID: runtime.NodeExecutionID(req.TenantID, req.InstanceID, r.NodeID, req.Attempt).String(),
		EffectRefs:            []string{"time_observation:" + r.Work.Observation.ID, "time_session:" + r.Work.Session.ID},
	}
	outputDigest := PunchCommitEvidenceDigest(r.Work.Observation.ID, r.Work.Session.ID)
	return frontier.NodeOutcome{NodeID: r.NodeID, Outcome: workflow.OutcomeSucceeded, OutputDigest: outputDigest}, refs, nil
}

// RunInTx is retained for callers that explicitly choose a shared transaction
// boundary; the default driver path uses Run because the time store is
// separate. It does not schedule continuations or invent a second engine.
func (r PunchCommitStepRunner) RunInTx(ctx context.Context, _ runtime.Executor, req execute.StepRequest) (frontier.NodeOutcome, runtime.GovernanceRefs, error) {
	return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, errors.New("clock workflow: transactional commit requires shared time/workflow database")
}
