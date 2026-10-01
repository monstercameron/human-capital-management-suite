package application

import (
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
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
			ChatClasses: owner.ChatClasses,
			TenantUUID:  owner.TenantUUID, Now: owner.Now,
		})
		if err != nil {
			return PersonaRuntimeModelComposition{}, err
		}
		budget, err := NewPersonaLedgerModelBudget(in.Runtime.Budget, owner.AgentStore, owner.Authority, owner.TenantUUID, owner.Now)
		if err != nil {
			return PersonaRuntimeModelComposition{}, err
		}
		model, verifier, err := ComposePersonaRuntimeModel(in.Deployment, in.APIKey, in.Config.PersonaOutputSigningSeed, in.Config.PersonaWorkloadSigningSeed,
			PersonaModelDeploymentDependencies{Budget: agentmodel.Budget(budget), Routes: evidence, Sources: evidence, Now: owner.Now, Resources: in.Runtime.Resources})
		if err != nil {
			return PersonaRuntimeModelComposition{}, err
		}
		model.RecoveryVerifier = verifier
		return model, nil
	}, nil
}
