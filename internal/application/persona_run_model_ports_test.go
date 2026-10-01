package application

import (
	"errors"
	"testing"
)

func TestPersonaRunModelPorts_RequiresProductionGateway(t *testing.T) {
	ports, err := NewPersonaRunModelPorts(nil, PersonaRunModelWorkSourceConfig{})
	if !errors.Is(err, errPersonaRunModelPorts) {
		t.Fatalf("missing gateway error=%v, want persona model ports unavailable", err)
	}
	if ports.Model != nil || ports.Work != nil {
		t.Fatalf("failed composition returned ports: %+v", ports)
	}
}

func TestPersonaRunModelPorts_RequiresTrustedWorkAuthorities(t *testing.T) {
	ports, err := NewPersonaRunModelPorts(&AgentModelGateway{}, PersonaRunModelWorkSourceConfig{})
	if !errors.Is(err, errPersonaRunModelPorts) || !errors.Is(err, errPersonaRunModelWork) {
		t.Fatalf("incomplete work authorities error=%v, want unavailable ports and work authority", err)
	}
	if ports.Model != nil || ports.Work != nil {
		t.Fatalf("failed composition returned partial ports: %+v", ports)
	}
}

func TestPersonaRunModelPorts_BindRejectsIncompletePortsWithoutMutation(t *testing.T) {
	original := PersonaRunStarterConfig{}
	if err := (PersonaRunModelPorts{}).Bind(&original); !errors.Is(err, errPersonaRunModelPorts) {
		t.Fatalf("empty ports bind error=%v, want unavailable", err)
	}
	if original.Model != nil || original.Work != nil {
		t.Fatal("failed bind mutated starter config")
	}
	if err := (PersonaRunModelPorts{}).Bind(nil); !errors.Is(err, errPersonaRunModelPorts) {
		t.Fatalf("nil config bind error=%v, want unavailable", err)
	}
}

func TestPersonaRunModelPorts_BindInstallsBothPorts(t *testing.T) {
	model := &AgentModelExecutorAdapter{}
	work := &DatabasePersonaRunModelWorkSource{}
	config := PersonaRunStarterConfig{}
	if err := (PersonaRunModelPorts{Model: model, Work: work}).Bind(&config); err != nil {
		t.Fatalf("bind complete ports: %v", err)
	}
	if config.Model != model || config.Work != work {
		t.Fatalf("bound model=%T work=%T, want supplied production port instances", config.Model, config.Work)
	}
}
