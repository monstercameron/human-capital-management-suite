package application

import (
	"context"
	"errors"
	"fmt"
)

var errPersonaRunModelPorts = errors.New("application: persona run model ports unavailable")

// PersonaRunModelPorts contains the two model ports required by a persona run.
// Work resolves the current tenant route policy and effective budgets for each
// admitted run; Model dispatches only the resulting pinned request through the
// supplied production gateway.
type PersonaRunModelPorts struct {
	Model *AgentModelExecutorAdapter
	Work  *DatabasePersonaRunModelWorkSource
}

// NewPersonaRunModelPorts binds persona work resolution to the existing model
// gateway. The work source requires tenant-scoped persona and manifest readers,
// current route and budget policy, invoker-readable thread access, and scoped
// credential leases. No model, budget, route, or provider fallback is added.
func NewPersonaRunModelPorts(gateway *AgentModelGateway, work PersonaRunModelWorkSourceConfig) (PersonaRunModelPorts, error) {
	if gateway == nil {
		return PersonaRunModelPorts{}, fmt.Errorf("%w: production model gateway is required", errPersonaRunModelPorts)
	}
	model, err := NewAgentModelExecutorAdapter(gateway)
	if err != nil {
		return PersonaRunModelPorts{}, fmt.Errorf("%w: model executor: %w", errPersonaRunModelPorts, err)
	}
	workSource, err := NewDatabasePersonaRunModelWorkSource(work)
	if err != nil {
		return PersonaRunModelPorts{}, fmt.Errorf("%w: trusted work source: %w", errPersonaRunModelPorts, err)
	}
	return PersonaRunModelPorts{Model: model, Work: workSource}, nil
}

// Bind installs these production ports in a starter config. It refuses nil,
// incomplete or typed-nil values instead of leaving development defaults.
func (p PersonaRunModelPorts) Bind(cfg *PersonaRunStarterConfig) error {
	if cfg == nil || p.Model == nil || p.Work == nil || isNilPersonaOutputPort(p.Model) || isNilPersonaOutputPort(p.Work) {
		return errPersonaRunModelPorts
	}
	cfg.Model = p.Model
	cfg.Work = p.Work
	return nil
}

var _ PersonaRunModelWorkSource = (*DatabasePersonaRunModelWorkSource)(nil)
var _ interface {
	Execute(ctx context.Context, request AgentModelExecutorRequest) (AgentModelExecutorResult, error)
} = (*AgentModelExecutorAdapter)(nil)
