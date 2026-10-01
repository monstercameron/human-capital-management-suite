package application

import (
	"context"
	"sync"

	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
)

// AgentTypedModelDispatchBinding permits the composition root to finish the
// durable owners before connecting the model. An unbound runtime refuses calls.
type AgentTypedModelDispatchBinding struct {
	mu         sync.RWMutex
	dispatcher agentmodel.TypedModelDispatcher
}

func (b *AgentTypedModelDispatchBinding) Available() bool {
	if b == nil {
		return false
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.dispatcher != nil
}

func (b *AgentTypedModelDispatchBinding) Bind(dispatcher agentmodel.TypedModelDispatcher) error {
	if b == nil || isNilPersonaOutputPort(dispatcher) {
		return ErrAgentModelGatewayNotConfigured
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.dispatcher != nil {
		return ErrAgentModelGatewayNotConfigured
	}
	b.dispatcher = dispatcher
	return nil
}

func (b *AgentTypedModelDispatchBinding) DispatchTypedModel(ctx context.Context, req agentmodel.Request, input agentmodel.TypedModelInput) (agentmodel.ModelResult, error) {
	if b == nil || ctx == nil {
		return agentmodel.ModelResult{}, ErrAgentModelUnavailable
	}
	b.mu.RLock()
	dispatcher := b.dispatcher
	b.mu.RUnlock()
	if dispatcher == nil {
		return agentmodel.ModelResult{}, ErrAgentModelUnavailable
	}
	return dispatcher.DispatchTypedModel(ctx, req, input)
}

func BindAgentRuntimeTypedModel(runtime *agentRuntime, dispatcher agentmodel.TypedModelDispatcher) error {
	if runtime == nil || runtime.TypedModels == nil {
		return ErrAgentModelGatewayNotConfigured
	}
	return runtime.TypedModels.Bind(dispatcher)
}
