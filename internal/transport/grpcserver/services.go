package grpcserver

import (
	"context"

	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
	registryv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/registry/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
)

// intentService adapts the generated IntentService server interface to the
// transport.IntentHandler port. Every method is a forward: authentication,
// trusted-context construction, validation and error projection already
// happened in the interceptor chain, and nothing else belongs here.
type intentService struct {
	intentsv1.UnimplementedIntentServiceServer

	handler transport.IntentHandler
}

func (s *intentService) CreateIntent(ctx context.Context, req *intentsv1.CreateIntentRequest) (*intentsv1.CreateIntentResponse, error) {
	return s.handler.CreateIntent(ctx, req)
}

func (s *intentService) GetIntent(ctx context.Context, req *intentsv1.GetIntentRequest) (*intentsv1.GetIntentResponse, error) {
	return s.handler.GetIntent(ctx, req)
}

func (s *intentService) ListIntents(ctx context.Context, req *intentsv1.ListIntentsRequest) (*intentsv1.ListIntentsResponse, error) {
	return s.handler.ListIntents(ctx, req)
}

func (s *intentService) SimulateIntent(ctx context.Context, req *intentsv1.SimulateIntentRequest) (*intentsv1.SimulateIntentResponse, error) {
	return s.handler.SimulateIntent(ctx, req)
}

func (s *intentService) ExecuteIntent(ctx context.Context, req *intentsv1.ExecuteIntentRequest) (*intentsv1.ExecuteIntentResponse, error) {
	return s.handler.ExecuteIntent(ctx, req)
}

func (s *intentService) SubmitIntent(ctx context.Context, req *intentsv1.SubmitIntentRequest) (*intentsv1.SubmitIntentResponse, error) {
	return s.handler.SubmitIntent(ctx, req)
}

func (s *intentService) CancelIntent(ctx context.Context, req *intentsv1.CancelIntentRequest) (*intentsv1.CancelIntentResponse, error) {
	return s.handler.CancelIntent(ctx, req)
}

func (s *intentService) SupersedeIntent(ctx context.Context, req *intentsv1.SupersedeIntentRequest) (*intentsv1.SupersedeIntentResponse, error) {
	return s.handler.SupersedeIntent(ctx, req)
}

func (s *intentService) ExplainIntent(ctx context.Context, req *intentsv1.ExplainIntentRequest) (*intentsv1.ExplainIntentResponse, error) {
	return s.handler.ExplainIntent(ctx, req)
}

func (s *intentService) ListIntentTimeline(ctx context.Context, req *intentsv1.ListIntentTimelineRequest) (*intentsv1.ListIntentTimelineResponse, error) {
	return s.handler.ListIntentTimeline(ctx, req)
}

func (s *intentService) RecommendIntentAction(ctx context.Context, req *intentsv1.RecommendIntentActionRequest) (*intentsv1.RecommendIntentActionResponse, error) {
	return s.handler.RecommendIntentAction(ctx, req)
}

func (s *intentService) GetIntentDeepLink(ctx context.Context, req *intentsv1.GetIntentDeepLinkRequest) (*intentsv1.GetIntentDeepLinkResponse, error) {
	return s.handler.GetIntentDeepLink(ctx, req)
}

func (s *intentService) InspectIntentFields(ctx context.Context, req *intentsv1.InspectIntentFieldsRequest) (*intentsv1.InspectIntentFieldsResponse, error) {
	return s.handler.InspectIntentFields(ctx, req)
}

func (s *intentService) ExportIntentFields(ctx context.Context, req *intentsv1.ExportIntentFieldsRequest) (*intentsv1.ExportIntentFieldsResponse, error) {
	return s.handler.ExportIntentFields(ctx, req)
}

// registryService adapts the generated RegistryService server interface to the
// transport.RegistryHandler port.
type registryService struct {
	registryv1.UnimplementedRegistryServiceServer

	handler transport.RegistryHandler
}

func (s *registryService) ListIntentDefinitions(ctx context.Context, req *registryv1.ListIntentDefinitionsRequest) (*registryv1.ListIntentDefinitionsResponse, error) {
	return s.handler.ListIntentDefinitions(ctx, req)
}

func (s *registryService) GetIntentDefinition(ctx context.Context, req *registryv1.GetIntentDefinitionRequest) (*registryv1.GetIntentDefinitionResponse, error) {
	return s.handler.GetIntentDefinition(ctx, req)
}

func (s *registryService) ListCapabilities(ctx context.Context, req *registryv1.ListCapabilitiesRequest) (*registryv1.ListCapabilitiesResponse, error) {
	return s.handler.ListCapabilities(ctx, req)
}

func (s *registryService) GetCapability(ctx context.Context, req *registryv1.GetCapabilityRequest) (*registryv1.GetCapabilityResponse, error) {
	return s.handler.GetCapability(ctx, req)
}
