package application

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
)

var errPersonaRunBinding = errors.New("application: persona run binding unavailable")

// PersonaRunBindingConfig supplies the authoritative request builder, current
// admission authority, and tenant-scoped durable stores for persona runs.
// Every field is required; the binding has no local authority or provider
// fallback.
type PersonaRunBindingConfig struct {
	Builder          *PersonaRunRequestBuilder
	Authority        agentrun.Authority
	AdmissionStore   agentrun.Store
	ExecutionStore   runstate.Store
	AdmissionRecheck runstate.AdmissionRechecker
	Now              func() time.Time
}

// PersonaRunBinding connects an admitted persona invocation to durable
// admission and execution state. Starting a binding persists the accepted
// admission and creates its READY execution record; workers may execute model
// steps later through the platform's existing executor gates.
type PersonaRunBinding struct {
	adapter *personaChatAdmissionAdapter
	builder *PersonaRunRequestBuilder
}

// NewPersonaRunBinding composes the production persona invocation path. It
// refuses incomplete composition rather than substituting memory, authority,
// clock, or model implementations.
func NewPersonaRunBinding(cfg PersonaRunBindingConfig) (*PersonaRunBinding, error) {
	if cfg.Builder == nil || cfg.Builder.source == nil || cfg.Authority == nil || cfg.AdmissionStore == nil || cfg.ExecutionStore == nil || cfg.AdmissionRecheck == nil {
		return nil, errPersonaRunBinding
	}
	admission, err := agentrun.NewAdmissionService(agentrun.AdmissionConfig{
		Authority: cfg.Authority,
		Store:     cfg.AdmissionStore,
		Now:       cfg.Now,
	})
	if err != nil {
		return nil, fmt.Errorf("%w: admission service: %v", errPersonaRunBinding, err)
	}
	execution, err := runstate.New(cfg.ExecutionStore, cfg.AdmissionRecheck)
	if err != nil {
		return nil, fmt.Errorf("%w: execution service: %v", errPersonaRunBinding, err)
	}
	adapter, err := newPersonaChatAdmissionAdapter(cfg.Builder, admission, execution)
	if err != nil {
		return nil, fmt.Errorf("%w: invocation adapter: %v", errPersonaRunBinding, err)
	}
	return &PersonaRunBinding{adapter: adapter, builder: cfg.Builder}, nil
}

// ResolveTargetAgentID resolves the exact pinned agent identity before the
// invocation service creates its durable delegation grant.
func (b *PersonaRunBinding) ResolveTargetAgentID(ctx context.Context, invocation agentinvoke.RunRequest) (string, error) {
	if b == nil || b.builder == nil || b.builder.source == nil || ctx == nil || !validPersonaRunInvocation(invocation) {
		return "", errPersonaRunBinding
	}
	facts, err := b.builder.source.ResolvePersonaRun(ctx, invocation)
	if err != nil {
		return "", fmt.Errorf("%w: resolve target identity: %v", errPersonaRunBinding, err)
	}
	if err := validatePersonaRunFacts(invocation, facts); err != nil {
		return "", fmt.Errorf("%w: invalid target identity facts: %v", errPersonaRunBinding, err)
	}
	return facts.Agent.AgentID, nil
}

// Start admits and durably starts one canonical on-behalf-of invocation.
// Invalid, stale, refused, or incomplete requests fail before execution state
// is created.
func (b *PersonaRunBinding) Start(ctx context.Context, invocation agentinvoke.RunRequest) error {
	if b == nil || b.adapter == nil || ctx == nil {
		return errPersonaRunBinding
	}
	if err := b.adapter.Start(ctx, invocation); err != nil {
		return fmt.Errorf("%w: %v", errPersonaRunBinding, err)
	}
	return nil
}

var _ agentinvoke.RunStarter = (*PersonaRunBinding)(nil)
var _ agentinvoke.TargetAgentResolver = (*PersonaRunBinding)(nil)
