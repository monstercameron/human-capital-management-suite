package application

import (
	"context"

	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
)

// AgentTypedModelRequestSource resolves current durable task, grant, skill,
// model route and source labels. Schema and prompt are supplied by SchemaFlux;
// they must not be interpreted as authority or used to choose a provider.
type AgentTypedModelRequestSource interface {
	BuildTypedAgentModelRequest(context.Context, agentmodel.Request, agentmodel.TypedModelInput) (AgentModelGatewayRequest, error)
}

// The trusted source's generated request is available only during this call.
// Source verifiers compare exact fields and digests before reading current
// task authority; external callers cannot manufacture this context key.
type agentTypedModelEvidenceContextKey struct{}

type OpenAIPlatformTypedModelDispatcher struct {
	gateway *AgentModelGateway
	source  AgentTypedModelRequestSource
}

func NewOpenAIPlatformTypedModelDispatcher(gateway *AgentModelGateway, source AgentTypedModelRequestSource) (*OpenAIPlatformTypedModelDispatcher, error) {
	if gateway == nil || isNilPersonaOutputPort(source) {
		return nil, ErrAgentModelGatewayNotConfigured
	}
	return &OpenAIPlatformTypedModelDispatcher{gateway: gateway, source: source}, nil
}
func (d *OpenAIPlatformTypedModelDispatcher) DispatchTypedModel(ctx context.Context, request agentmodel.Request, input agentmodel.TypedModelInput) (agentmodel.ModelResult, error) {
	if d == nil || d.gateway == nil || d.source == nil || ctx == nil || input.WebSearch {
		return agentmodel.ModelResult{}, ErrAgentModelGatewayNotConfigured
	}
	built, err := d.source.BuildTypedAgentModelRequest(ctx, request, input)
	if err != nil {
		return agentmodel.ModelResult{}, err
	}
	if built.TenantID != request.TenantID || built.Dispatch.Outbound.TaskID != request.Actor.TaskID || built.Dispatch.Outbound.Principal != request.Actor.UserID || built.Dispatch.Model.Output.Mode != agentmodel.OutputSchema {
		return agentmodel.ModelResult{}, ErrAgentModelExecutorBinding
	}
	ctx = context.WithValue(ctx, agentTypedModelEvidenceContextKey{}, built)
	result, err := d.gateway.Dispatch(ctx, built)
	if err != nil {
		return agentmodel.ModelResult{}, err
	}
	return result.Dispatch.Model, nil
}
