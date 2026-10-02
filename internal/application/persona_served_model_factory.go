package application

import (
	"context"
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/resources"
)

type personaServedModelFactoryInput struct {
	Config     ServeConfig
	Deployment PersonaModelDeployment
	APIKey     string
	Runtime    *agentRuntime
}

// newPersonaServedModelFactory shares the platform's durable budget and audit
// owners with Chat. Source evidence is composed only after current admission
// authorities exist, and worker credentials are bound to this exact cell.
func newPersonaServedModelFactory(in personaServedModelFactoryInput) (func(PersonaRuntimeModelOwnerDependencies) (PersonaRuntimeModelComposition, error), error) {
	if strings.TrimSpace(in.APIKey) == "" || in.Runtime == nil || in.Runtime.Budget == nil || isNilPersonaOutputPort(in.Runtime.Audit) {
		return nil, ErrAgentModelGatewayNotConfigured
	}
	if in.Config.CellID == "" || in.Deployment.Worker.Cell != in.Config.CellID {
		return nil, fmt.Errorf("%w: persona worker cell does not match the serving cell", ErrPersonaModelConfiguration)
	}
	if _, _, err := in.Deployment.validate(); err != nil {
		return nil, err
	}
	return func(owner PersonaRuntimeModelOwnerDependencies) (PersonaRuntimeModelComposition, error) {
		evidence, err := NewPersonaOpenAIModelEvidence(PersonaOpenAIModelOwnerConfig{
			DB: owner.AgentStore, Personas: owner.Personas, Manifests: owner.Manifests,
			Routes: owner.Routes, Threads: owner.Threads, Authority: owner.Authority,
			Audit: in.Runtime.Audit, ToolJournal: owner.ToolJournal, ToolSources: owner.ToolSources,
			ChatClasses: owner.ChatClasses, Documents: owner.Documents,
			TenantUUID: owner.TenantUUID, Now: owner.Now,
		})
		if err != nil {
			return PersonaRuntimeModelComposition{}, err
		}
		budget, err := NewPersonaLedgerModelBudget(in.Runtime.Budget, owner.AgentStore, owner.Authority, owner.TenantUUID, owner.Now)
		if err != nil {
			return PersonaRuntimeModelComposition{}, err
		}
		model, verifier, err := ComposePersonaRuntimeModel(in.Deployment, in.APIKey, in.Config.PersonaOutputSigningSeed, in.Config.PersonaWorkloadSigningSeed,
			PersonaModelDeploymentDependencies{Budget: agentmodel.Budget(budget), Routes: evidence, Sources: evidence, Now: owner.Now, Resources: personaRunModelResources{runtime: in.Runtime.Resources}})
		if err != nil {
			return PersonaRuntimeModelComposition{}, err
		}
		model.RecoveryVerifier = verifier
		return model, nil
	}, nil
}

// personaRunModelResources reserves worker and provider capacity for a model
// call made by an admitted persona run. A task step holds a worker lease in
// its context; a run started by a chat mention does not, so the lease is
// acquired here from the request's identity, which the fenced model-work
// source resolved from the durable admission and run.
type personaRunModelResources struct{ runtime *AgentResourceRuntime }

func (p personaRunModelResources) AcquireAgentModelResources(ctx context.Context, request AgentModelResourceRequest) (func(), error) {
	if p.runtime == nil || ctx == nil || request.ProviderID == "" {
		return nil, ErrAgentResourceIdentity
	}
	if parent, ok := ctx.Value(agentResourceContextKey{}).(*AgentResourceLease); ok && parent != nil {
		return p.runtime.AcquireAgentModelResources(ctx, request)
	}
	lease, err := p.runtime.Acquire(ctx, AgentResourceIdentity{TenantID: request.TenantID, UserID: request.UserID, TaskID: request.TaskID, Lane: resources.LaneInteractive}, request.ProviderID)
	if err != nil {
		return nil, err
	}
	return lease.Release, nil
}
