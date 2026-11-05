package application

import (
	"context"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
)

// personaRunAnswerCallSite and personaRunContinuationCallSite name the two
// model turns of a persona run in the per-call spend ledger (AGENTCOST-001).
const (
	personaRunAnswerCallSite       = "persona_run_executor:answer"
	personaRunContinuationCallSite = "persona_run_executor:tool_continuation"
)

// personaRunTriggerKind maps the admission source to the spend report's
// trigger vocabulary: what caused the run.
func personaRunTriggerKind(kind agentrun.SourceKind) string {
	switch kind {
	case agentrun.SourcePersonaMention:
		return "mention"
	case agentrun.SourceSupportEmail:
		return "email"
	case agentrun.SourceSchedule:
		return "schedule"
	default:
		return strings.ToLower(string(kind))
	}
}

// personaRunCallContext describes one model turn for the recording adapter:
// the run's identity, what caused it, which turn this is, and which question
// it answers. The kind is left for the adapter to classify from the request's
// task profile.
func personaRunCallContext(admission agentrun.Record, run runstate.Run, callSite string, stage agentmodel.CallStage) agentmodel.CallContext {
	return agentmodel.CallContext{
		TenantID: run.TenantID, AgentID: run.AgentID,
		TriggerKind: personaRunTriggerKind(admission.Request.Source.Kind),
		RunID:       run.ID, QuestionID: admission.Request.CauseID,
		CallSite: callSite, Stage: stage,
	}
}

// sealModelCalls seals the run ledger's not-yet-sealed lines into the run
// record under the worker lease, retrying one serialization abort like every
// other run state change. The run's sealed lines mark the frontier, so every
// model turn seals exactly its own new lines. Sealing nothing is a no-op.
func (e *personaAdmittedRunExecutor) sealModelCalls(ctx context.Context, run runstate.Run, ledger *agentmodel.CallLedger) (runstate.Run, error) {
	if ledger == nil {
		return run, nil
	}
	fresh := ledger.Lines()
	if len(fresh) <= len(run.Calls) {
		return run, nil
	}
	return e.qualityStateChange(ctx, run, func() (runstate.Run, error) {
		return e.state.RecordModelCalls(ctx, run.ID, e.workerID, run.Fence, run.Version, fresh[len(run.Calls):], e.now().UTC())
	})
}
