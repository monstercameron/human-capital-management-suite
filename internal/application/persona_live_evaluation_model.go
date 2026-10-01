package application

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentbudget"
	"github.com/monstercameron/human-capital-management-suite/internal/agentegress"
	"github.com/monstercameron/human-capital-management-suite/internal/agenteval"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentcandidateevalstore"
)

// PersonaCandidateScope rechecks the evaluation deployment before every
// provider call. It must also prove that every input source belongs to the
// reserved synthetic tenant, including model messages and tool results.
type PersonaCandidateScope interface {
	AuthorizeSyntheticPersonaEvaluation(context.Context, agenteval.PersonaEvaluationTarget) error
	AuthorizePersonaCandidateModelRequest(context.Context, agenteval.PersonaEvaluationTarget, AgentModelExecutorRequest) error
}

// PersonaCandidateModelConfig is only used by the evaluator deployment. Its
// exact model selection may be unevaluated; it conveys no production route
// approval. SchemaFlux, egress policy, credential leases and budget settlement
// are still mandatory.
type PersonaCandidateModelConfig struct {
	Target    agenteval.PersonaEvaluationTarget
	Selection agentmodel.ModelSelection
	Adapter   *agentmodel.SchemaFluxAdapter
	Egress    *agentegress.ProviderDispatcher
	Pricing   *agentmodel.PricingSchedule
	Budget    agentmodel.Budget
	Scope     PersonaCandidateScope
	Journal   *agentcandidateevalstore.Store
}

type PersonaCandidateModelGateway struct{ config PersonaCandidateModelConfig }

func NewPersonaCandidateModelGateway(config PersonaCandidateModelConfig) (*PersonaCandidateModelGateway, error) {
	if config.Adapter == nil || config.Egress == nil || config.Pricing == nil || config.Budget == nil || config.Scope == nil || config.Journal == nil ||
		config.Target.TenantID == "" || config.Target.TenantID == config.Target.SyntheticTenantID || config.Target.SyntheticTenantID == "" ||
		!required(config.Selection.ProfileID) || !required(config.Selection.Identity.ProviderID) || !required(config.Selection.Identity.ModelID) || !required(config.Selection.Identity.Version) ||
		!personaRequestDigest(config.Target.ProfileDigest) || !personaRequestDigest(config.Target.ModelDigest) ||
		config.Target.ModelDigest != "sha256:"+config.Selection.ProfileDigest || config.Selection.Identity != config.Adapter.Identity() {
		return nil, agenteval.ErrPersonaEvaluation
	}
	return &PersonaCandidateModelGateway{config: config}, nil
}

// Execute dispatches one candidate step without consulting the production
// approved-profile router. No request can change the evaluator's target or
// model, and ordinary tenant requests fail before budget or provider effects.
func (g *PersonaCandidateModelGateway) Execute(ctx context.Context, request AgentModelExecutorRequest) (AgentModelExecutorResult, error) {
	if g == nil || ctx == nil {
		return AgentModelExecutorResult{}, agenteval.ErrPersonaEvaluation
	}
	c := g.config
	if err := c.Scope.AuthorizeSyntheticPersonaEvaluation(ctx, c.Target); err != nil {
		return AgentModelExecutorResult{}, err
	}
	if request.Task.TenantID != c.Target.SyntheticTenantID || request.Outbound.Principal != c.Target.InvokerID ||
		request.Model.ModelProfile != c.Selection.ProfileID || request.Route.Pin.Primary != c.Selection ||
		len(request.Route.Pin.Fallbacks) != 0 || validateExecutorRequest(request) != nil {
		return AgentModelExecutorResult{}, agenteval.ErrPersonaEvaluation
	}
	ctx = context.WithValue(ctx, openAIModelDispatchContextKey{}, openAIModelDispatchBinding{tenant: request.Task.TenantID, runID: request.Task.TaskID, stepID: request.StepID})
	if err := c.Scope.AuthorizePersonaCandidateModelRequest(ctx, c.Target, request); err != nil {
		return AgentModelExecutorResult{}, err
	}
	if err := validateGatewayLease(request.Lease, c.Target.SyntheticTenantID, request.Outbound); err != nil {
		return AgentModelExecutorResult{}, err
	}
	if err := c.Pricing.Validate(ctx, c.Selection, request.Model.Limits); err != nil {
		return AgentModelExecutorResult{}, err
	}
	limits := request.Model.Limits
	if limits.MaxInputTokens < 0 || limits.MaxOutputTokens < 0 || limits.MaxInputTokens > math.MaxInt64-limits.MaxOutputTokens {
		return AgentModelExecutorResult{}, agentmodel.ErrBudgetFailed
	}
	reservation, err := c.Budget.Reserve(ctx, agentbudget.Request{TaskID: request.Task.TaskID, StepID: request.StepID,
		Fingerprint: personaLiveEvidenceDigest("candidate-model-request", request.Model, request.FieldSources), Estimate: agentbudget.Usage{Steps: 1, Tokens: limits.MaxInputTokens + limits.MaxOutputTokens, SpendMicros: limits.MaxCostMicros, WallClock: request.Route.Task.MaxLatency}})
	if err != nil || reservation == nil {
		return AgentModelExecutorResult{}, fmt.Errorf("%w: candidate reservation", agentmodel.ErrBudgetFailed)
	}
	started := time.Now()
	result, dispatchErr := c.Egress.Dispatch(ctx, agentegress.ProviderDispatchRequest{Model: request.Model, Outbound: request.Outbound,
		FieldSources: request.FieldSources, Lease: request.Lease}, c.Adapter)
	// Refusals and normalized provider failures may contain billed tokens.
	// Only pre-dispatch denials release the reservation without settled usage.
	if result.LeaseID != "" || dispatchErr == nil && result.Model.Failure == nil && result.Model.Refusal == nil {
		if result.Model.Usage.TotalTokens > 0 || dispatchErr == nil && result.Model.Failure == nil && result.Model.Refusal == nil {
			if _, err := c.Pricing.Reconcile(c.Selection, result.Model.Usage); err != nil {
				_ = reservation.Fail()
				return AgentModelExecutorResult{}, err
			}
		} else if result.Model.Usage.CostMicros != 0 {
			_ = reservation.Fail()
			return AgentModelExecutorResult{}, agentmodel.ErrPricingMismatch
		}
		if err := reservation.Settle(agentbudget.Usage{Steps: 1, Tokens: result.Model.Usage.TotalTokens, SpendMicros: result.Model.Usage.CostMicros, WallClock: time.Since(started)}); err != nil {
			return AgentModelExecutorResult{}, err
		}
	} else if err := reservation.Fail(); err != nil {
		return AgentModelExecutorResult{}, err
	}
	if result.LeaseID != "" {
		resultDigest, digestErr := personaRunAnyResultDigest(result.Model)
		if digestErr != nil || result.Model.Provider != c.Selection.Identity {
			return AgentModelExecutorResult{}, agenteval.ErrPersonaEvaluation
		}
		if err := c.Journal.AppendModelCall(ctx, agentcandidateevalstore.ModelCall{Target: c.Target, TaskID: request.Task.TaskID, StepID: request.StepID, RequestDigest: personaLiveEvidenceDigest("candidate-provider-request", request.Model, request.FieldSources), ResultDigest: resultDigest, LeaseID: result.LeaseID, Provider: result.Model.Provider, Usage: result.Model.Usage, ActionPolicy: request.Model.ActionPolicy, RequestedActions: result.Model.RequestedActions, CompletedAt: time.Now().UTC()}); err != nil {
			return AgentModelExecutorResult{}, err
		}
	}
	if dispatchErr != nil {
		return AgentModelExecutorResult{}, dispatchErr
	}
	return AgentModelExecutorResult{Result: result.Model}, nil
}
