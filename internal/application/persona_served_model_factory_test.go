package application

import (
	"errors"
	"testing"
)

func TestTodo_AGENT_021_ServedModelRequiresCurrentOwners(t *testing.T) {
	deployment := modelDeploymentFixture(t)
	in := personaServedModelFactoryInput{
		Config: ServeConfig{CellID: deployment.Worker.Cell}, Deployment: deployment,
		APIKey: "test-only-provider-key", Runtime: servedProviderTestRuntime(t),
	}
	factory, err := newPersonaServedModelFactory(in)
	if err != nil || factory == nil {
		t.Fatalf("configured factory unavailable: %v", err)
	}
	if model, err := factory(PersonaRuntimeModelOwnerDependencies{}); !errors.Is(err, ErrAgentModelGatewayNotConfigured) || model.Gateway != nil {
		t.Fatalf("model accepted missing current owners: gateway=%v err=%v", model.Gateway != nil, err)
	}
	in.Runtime.Audit = nil
	if factory, err := newPersonaServedModelFactory(in); !errors.Is(err, ErrAgentModelGatewayNotConfigured) || factory != nil {
		t.Fatalf("model accepted missing durable audit owner: %v", err)
	}
	in.Runtime = servedProviderTestRuntime(t)
	in.Runtime.Budget = nil
	if factory, err := newPersonaServedModelFactory(in); !errors.Is(err, ErrAgentModelGatewayNotConfigured) || factory != nil {
		t.Fatalf("model accepted missing shared budget owner: %v", err)
	}
}
