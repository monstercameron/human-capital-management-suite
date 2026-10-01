package application

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/agentegress"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
)

// LookupRestoredEffect asks the actual capability owner about one retained
// UNKNOWN intent. It performs no capability replay or inference.
func (w *CommonAgentWorker) LookupRestoredEffect(ctx context.Context, tenant, runID string, candidate runstate.Effect) (runstate.EffectStatus, string, string, error) {
	if w == nil || ctx == nil || candidate.Status != runstate.EffectUnknown {
		return runstate.EffectUnknown, "", "", ErrCommonAgentWorker
	}
	record, err := w.cfg.Runtime.GetAdmission(ctx, tenant, runID)
	if err != nil {
		return runstate.EffectUnknown, "", "", err
	}
	run, err := w.cfg.Runtime.GetRun(ctx, tenant, runID)
	if err != nil {
		return runstate.EffectUnknown, "", "", err
	}
	if commonAgentCheckExecutionIdentity(record, run) != nil {
		return runstate.EffectUnknown, "", "", ErrCommonAgentWorker
	}
	found := false
	for _, stored := range run.Effects {
		if stored.ID == candidate.ID && reflect.DeepEqual(stored, candidate) && stored.Status == runstate.EffectUnknown {
			found = true
			break
		}
	}
	if !found {
		return runstate.EffectUnknown, "", "", ErrCommonAgentWorker
	}
	source, ok := w.cfg.Sources[record.Request.Source.Kind].(CommonAgentToolSource)
	if !ok {
		return runstate.EffectUnknown, "", "", ErrCommonAgentWorker
	}
	status, output, err := source.ReconcileCommonAgentEffect(ctx, record, run, candidate)
	if err != nil {
		return runstate.EffectUnknown, "", "", err
	}
	if status == runstate.EffectNotApplied && output == (CommonAgentOutput{}) {
		return status, "", "", nil
	}
	if status != runstate.EffectApplied || !commonAgentOutputValid(output) {
		return runstate.EffectUnknown, "", "", ErrCommonAgentWorker
	}
	return status, output.Ref, output.Digest, nil
}

type commonAgentModelEvidenceKey struct{}
type commonAgentModelEvidence struct {
	Record  agentrun.Record
	Run     runstate.Run
	Request AgentModelExecutorRequest
}

func withCommonAgentModelEvidence(ctx context.Context, record agentrun.Record, run runstate.Run, request AgentModelExecutorRequest) context.Context {
	return context.WithValue(ctx, commonAgentModelEvidenceKey{}, commonAgentModelEvidence{Record: record, Run: run, Request: request})
}

func commonAgentWorkerDigest(value any) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return personaRunBytesDigest(raw), nil
}

func commonAgentBindModelStep(request AgentModelExecutorRequest, run runstate.Run, ordinal uint32) AgentModelExecutorRequest {
	step := fmt.Sprintf("common-agent-model:%s:%d:%d", run.ID, run.Fence, ordinal)
	request.StepID, request.Route.TraceID, request.Model.TraceID = step, step, step
	return request
}

func (w *CommonAgentWorker) reconcile(ctx context.Context, record agentrun.Record, run runstate.Run, state *runstate.Service, source CommonAgentExecutionSource) (runstate.Run, error) {
	tools, ok := source.(CommonAgentToolSource)
	if !ok {
		return run, ErrCommonAgentWorker
	}
	if err := w.cfg.Runtime.Recheck(ctx, run.TenantID, run.ID); err != nil {
		return run, err
	}
	for _, effect := range run.Effects {
		if effect.Status != runstate.EffectUnknown {
			continue
		}
		status, output, err := tools.ReconcileCommonAgentEffect(ctx, record, run, effect)
		if err != nil {
			return run, err
		}
		if status != runstate.EffectNotApplied && (status != runstate.EffectApplied || !commonAgentOutputValid(output)) {
			return run, ErrCommonAgentWorker
		}
		run, err = state.ReconcileEffect(ctx, run.ID, effect.ID, run.Version, status, output.Ref, output.Digest, w.cfg.Now().UTC())
		if err != nil {
			return run, err
		}
	}
	return run, nil
}

func (w *CommonAgentWorker) tool(ctx context.Context, record agentrun.Record, run runstate.Run, state *runstate.Service, source CommonAgentExecutionSource, request AgentModelExecutorRequest, result AgentModelExecutorResult) (runstate.Run, AgentModelExecutorRequest, AgentModelExecutorResult, error) {
	tools, ok := source.(CommonAgentToolSource)
	if !ok || result.Result.Failure != nil || result.Result.Refusal != nil || strings.TrimSpace(result.Result.Text) != "" || len(result.Result.ToolProposals) != 1 {
		failed, err := w.fail(ctx, run, state, "TOOL_POLICY_UNAVAILABLE", ErrCommonAgentWorker)
		return failed, request, result, err
	}
	proposal := result.Result.ToolProposals[0]
	if !personaRunToolProposalProjected(request.Model.Tools, proposal) {
		failed, err := w.fail(ctx, run, state, "TOOL_PROPOSAL_DENIED", ErrCommonAgentWorker)
		return failed, request, result, err
	}
	id := fmt.Sprintf("common-agent-tool:%s:%d:%s", run.ID, run.Fence, proposal.ID)
	key := "common-agent-effect:" + record.ID + ":" + proposal.ID
	run, err := state.BeginEffect(ctx, run.ID, w.cfg.WorkerID, id, key, personaRunBytesDigest(proposal.Arguments), run.Fence, run.Version, w.cfg.Now().UTC())
	if err != nil {
		return run, request, result, err
	}
	bytes, output, err := tools.ExecuteCommonAgentTool(ctx, record, run, proposal, key)
	if err != nil || !commonAgentOutputValid(output) || len(bytes) == 0 {
		// UNKNOWN survives this error. An owner read must resolve it after expiry.
		failed, failure := w.fail(ctx, run, state, "TOOL_OUTCOME_UNKNOWN", ErrCommonAgentWorker)
		return failed, request, result, failure
	}
	run, err = state.ResolveEffect(ctx, run.ID, w.cfg.WorkerID, id, run.Fence, run.Version, runstate.EffectApplied, output.Ref, output.Digest, w.cfg.Now().UTC())
	if err != nil {
		return run, request, result, err
	}
	continuation, err := source.BuildCommonAgentModelWork(ctx, record, run)
	if err != nil {
		return run, request, result, err
	}
	continuation = commonAgentBindModelStep(continuation, run, 2)
	if err = commonAgentAppendToolResult(&continuation, proposal, bytes); err != nil {
		return run, request, result, err
	}
	continuation.Model.Tools = nil
	digest, err := commonAgentWorkerDigest(continuation.Model)
	if err != nil {
		return run, request, result, err
	}
	run, err = state.Checkpoint(ctx, run.ID, w.cfg.WorkerID, run.Fence, run.Version, runstate.PhaseModelCall, 2, continuation.StepID, digest, w.cfg.Now().UTC())
	if err != nil {
		return run, request, result, err
	}
	result, err = w.cfg.Model.Execute(withCommonAgentModelEvidence(ctx, record, run, continuation), continuation)
	return run, continuation, result, err
}

func commonAgentAppendToolResult(request *AgentModelExecutorRequest, proposal agentmodel.ToolProposal, result []byte) error {
	if request == nil || request.ToolResultClass == "" || len(result) == 0 || len(result) > 1<<20 {
		return ErrCommonAgentWorker
	}
	start := len(request.Model.Messages)
	request.Model.Messages = append(request.Model.Messages,
		agentmodel.ModelMessage{Role: agentmodel.RoleAssistant, ToolCallID: proposal.ID, ToolName: proposal.Name, ToolArguments: append([]byte(nil), proposal.Arguments...)},
		agentmodel.ModelMessage{Role: agentmodel.RoleTool, ToolCallID: proposal.ID, Content: string(result)})
	for i := start; i < len(request.Model.Messages); i++ {
		name := fmt.Sprintf("model.message.%d", i)
		value, source, taint := string(result), "common-agent-tool-result", "UNTRUSTED_TOOL_RESULT"
		if i == start {
			value, source, taint = string(proposal.Arguments), "common-agent-tool-proposal", "MODEL_TOOL_PROPOSAL"
		}
		request.Outbound.DeclaredFields = append(request.Outbound.DeclaredFields, name)
		request.FieldSources[name] = source
		request.Outbound.Fields = append(request.Outbound.Fields, agentegress.Field{Name: name, Value: value, Class: request.ToolResultClass, Taint: []string{taint}, Provenance: []string{"common-agent-run:" + request.Task.TaskID, "tool-call:" + proposal.ID}})
	}
	return agentmodel.ValidateModelRequest(request.Model)
}
