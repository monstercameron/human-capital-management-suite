package application

import (
	"context"
	"sync"

	"github.com/monstercameron/human-capital-management-suite/internal/agenteval"
	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentcandidateevalstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
)

// PersonaLiveCaseFixtureSource commits a new human post and tainted peer
// context in the reserved synthetic deployment, then returns the granted,
// durable invocation and its authenticated invoker context. Implementations
// use fixture-only owner stores and never copy production document contents.
type PersonaLiveCaseFixtureSource interface {
	CreateSyntheticPersonaCase(context.Context, agenteval.PersonaEvaluationTarget, agenteval.PersonaCase) (context.Context, agentinvoke.RunRequest, error)
}

type PersonaLiveCaseExecutorConfig struct {
	Target      agenteval.PersonaEvaluationTarget
	Scope       PersonaCandidateScope
	Fixtures    PersonaLiveCaseFixtureSource
	Runtime     PersonaRunStarterConfig
	Model       *PersonaCandidateModelGateway
	Journal     *agentcandidateevalstore.Store
	Outputs     *agentpersonastore.TenantStore
	Delivery    PersonaLiveDeliveryEvidenceReader
	RefusalLink string
}

// PersonaLiveCaseExecutor runs the same admitted worker, tool gateway, output
// validator and audience-aware chat deliverer as served persona invocations.
// The separate candidate model gateway is restricted to the evaluator scope.
type PersonaLiveCaseExecutor struct {
	config    PersonaLiveCaseExecutorConfig
	admission *agentrun.AdmissionService
	executor  *personaAdmittedRunExecutor
	mu        sync.Mutex
	baselines map[string]agenteval.PersonaCaseExecution
}

func NewPersonaLiveCaseExecutor(config PersonaLiveCaseExecutorConfig) (*PersonaLiveCaseExecutor, error) {
	if config.Scope == nil || config.Fixtures == nil || config.Model == nil || config.Journal == nil || config.Outputs == nil || config.Delivery == nil || !required(config.RefusalLink) || config.Target != config.Model.config.Target {
		return nil, agenteval.ErrPersonaEvaluation
	}
	config.Runtime.Model = config.Model
	starter, err := NewPersonaRunStarter(config.Runtime)
	if err != nil {
		return nil, err
	}
	admission, ok := starter.adapter.admission.(*agentrun.AdmissionService)
	executor, runnerOK := starter.adapter.executor.(*personaAdmittedRunExecutor)
	if !ok || !runnerOK {
		return nil, agenteval.ErrPersonaEvaluation
	}
	return &PersonaLiveCaseExecutor{config: config, admission: admission, executor: executor, baselines: make(map[string]agenteval.PersonaCaseExecution)}, nil
}

func (e *PersonaLiveCaseExecutor) ExecutePersonaCase(ctx context.Context, target agenteval.PersonaEvaluationTarget, testCase agenteval.PersonaCase) (agenteval.PersonaCaseExecution, error) {
	if e == nil || ctx == nil || target != e.config.Target {
		return agenteval.PersonaCaseExecution{}, agenteval.ErrPersonaEvaluation
	}
	c := e.config
	if err := c.Scope.AuthorizeSyntheticPersonaEvaluation(ctx, target); err != nil {
		return agenteval.PersonaCaseExecution{}, err
	}
	caseCtx, invocation, err := c.Fixtures.CreateSyntheticPersonaCase(ctx, target, testCase)
	if err != nil || caseCtx == nil || !validPersonaChatRunRequest(invocation) || invocation.TenantID != target.SyntheticTenantID ||
		invocation.PersonaID != target.PersonaID || invocation.InvokerID != target.InvokerID || !personaRunVersionMatches(target.PersonaVersion, invocation.PersonaVersion) {
		return agenteval.PersonaCaseExecution{}, agenteval.ErrPersonaEvaluation
	}
	if err := c.Scope.AuthorizeSyntheticPersonaEvaluation(caseCtx, target); err != nil {
		return agenteval.PersonaCaseExecution{}, err
	}
	request, err := c.Runtime.Builder.BuildPersonaChatAdmission(caseCtx, invocation)
	if err != nil {
		return agenteval.PersonaCaseExecution{}, err
	}
	bindPersonaChatAdmissionRequest(&request, invocation)
	if validatePersonaChatAdmissionBinding(request, invocation) != nil || request.Persona == nil || request.Persona.Digest != target.ProfileDigest {
		return agenteval.PersonaCaseExecution{}, agenteval.ErrPersonaEvaluation
	}
	admitted, _, err := e.admission.Admit(caseCtx, request)
	if err != nil || validatePersonaChatAdmissionRecord(admitted, request) != nil {
		return agenteval.PersonaCaseExecution{}, agenteval.ErrPersonaEvaluation
	}
	execution := agenteval.PersonaCaseExecution{TaskID: admitted.ID, InvocationID: invocation.InvocationID}
	checkpoint := agentcandidateevalstore.Record{Target: target, CaseDigest: agenteval.PersonaCaseDigest(testCase),
		RequestDigest: personaLiveEvidenceDigest("invoker-request", testCase.Prompt, nil),
		InvocationID:  invocation.InvocationID, TaskID: admitted.ID, AdmissionDigest: "sha256:" + admitted.RequestDigest}
	if admitted.Decision == agentrun.DecisionRefused {
		checkpoint.Outcome, checkpoint.RefusalCode = "REFUSED", admitted.RefusalCode
		checkpoint.RefusalPointer = c.RefusalLink
		checkpoint.CompletedAt = admitted.AdmittedAt
		if err := c.Journal.Append(caseCtx, checkpoint); err != nil {
			return agenteval.PersonaCaseExecution{}, err
		}
		return execution, nil
	}
	run, err := e.executor.Start(caseCtx, admitted)
	if run.State == runstate.StateFailed && run.ID == admitted.ID && run.RequestDigest == admitted.RequestDigest && required(run.TerminalCode) {
		checkpoint.Outcome, checkpoint.RefusalCode, checkpoint.CompletedAt = "FAILED", run.TerminalCode, run.UpdatedAt
		if run.TerminalCode == "OUT_OF_SCOPE" {
			checkpoint.Outcome, checkpoint.RefusalPointer = "REFUSED", c.RefusalLink
		}
		if err := c.Journal.Append(caseCtx, checkpoint); err != nil {
			return agenteval.PersonaCaseExecution{}, err
		}
		return execution, nil
	}
	if err != nil || run.State != runstate.StateCompleted {
		return agenteval.PersonaCaseExecution{}, agenteval.ErrPersonaEvaluation
	}
	output, err := c.Outputs.GetFinalOutputByInvocation(caseCtx, invocation.InvocationID)
	if err != nil || output.RunID != run.ID || output.InvokerID != target.InvokerID || output.PersonaID != target.PersonaID {
		return agenteval.PersonaCaseExecution{}, agenteval.ErrPersonaEvaluation
	}
	tools, err := c.Outputs.ListToolResults(caseCtx, run.ID)
	if err != nil {
		return agenteval.PersonaCaseExecution{}, err
	}
	checkpoint.Outcome, checkpoint.OutputDigest = "COMPLETED", output.PersistenceDigest
	checkpoint.PlanDigest = PersonaLivePlanDigest(target.ProfileDigest, invocation.Skills, tools)
	for _, tool := range tools {
		checkpoint.Skills = append(checkpoint.Skills, tool.SkillID)
	}
	for _, saved := range run.Checkpoints {
		if saved.Phase == runstate.PhaseDelivery {
			checkpoint.DeliveryDigest = saved.Digest
			checkpoint.CompletedAt = saved.At
		}
	}
	checkpoint.DeliveredTo, err = c.Delivery.ReadPersonaEvaluationDelivery(caseCtx, output, checkpoint.DeliveryDigest)
	if err != nil {
		return agenteval.PersonaCaseExecution{}, err
	}
	if testCase.Kind == agenteval.PersonaPeerInjection {
		e.mu.Lock()
		baseline, ok := e.baselines[checkpoint.RequestDigest]
		e.mu.Unlock()
		if ok {
			checkpoint.BaselineInvocationID = baseline.InvocationID
		}
	}
	if err := c.Scope.AuthorizeSyntheticPersonaEvaluation(caseCtx, target); err != nil {
		return agenteval.PersonaCaseExecution{}, err
	}
	if err := c.Journal.Append(caseCtx, checkpoint); err != nil {
		return agenteval.PersonaCaseExecution{}, err
	}
	if testCase.Kind == agenteval.PersonaInScope {
		e.mu.Lock()
		e.baselines[checkpoint.RequestDigest] = execution
		e.mu.Unlock()
	}
	return execution, nil
}
