// Package integration adapts the generated IntegrationService server
// interface to the transport.IntegrationHandler port. Every method is a
// forward: authentication, trusted-context construction, validation and
// error projection already happened in the interceptor chain, and nothing
// else belongs here.
package integration

import (
	"context"

	integrationv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/integration/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
)

// Service adapts the generated IntegrationService server interface to the
// transport.IntegrationHandler port. The handler owns every rule; this type
// owns none.
type Service struct {
	integrationv1.UnimplementedIntegrationServiceServer

	Handler transport.IntegrationHandler
}

func (s *Service) ListConnectorDefinitions(ctx context.Context, req *integrationv1.ListConnectorDefinitionsRequest) (*integrationv1.ListConnectorDefinitionsResponse, error) {
	return s.Handler.ListConnectorDefinitions(ctx, req)
}

func (s *Service) GetConnectorDefinition(ctx context.Context, req *integrationv1.GetConnectorDefinitionRequest) (*integrationv1.GetConnectorDefinitionResponse, error) {
	return s.Handler.GetConnectorDefinition(ctx, req)
}

func (s *Service) ListConnectorConnections(ctx context.Context, req *integrationv1.ListConnectorConnectionsRequest) (*integrationv1.ListConnectorConnectionsResponse, error) {
	return s.Handler.ListConnectorConnections(ctx, req)
}

func (s *Service) GetConnectorConnection(ctx context.Context, req *integrationv1.GetConnectorConnectionRequest) (*integrationv1.GetConnectorConnectionResponse, error) {
	return s.Handler.GetConnectorConnection(ctx, req)
}

func (s *Service) TestConnectorConnection(ctx context.Context, req *integrationv1.TestConnectorConnectionRequest) (*integrationv1.TestConnectorConnectionResponse, error) {
	return s.Handler.TestConnectorConnection(ctx, req)
}

func (s *Service) ListExternalObservations(ctx context.Context, req *integrationv1.ListExternalObservationsRequest) (*integrationv1.ListExternalObservationsResponse, error) {
	return s.Handler.ListExternalObservations(ctx, req)
}

func (s *Service) GetExternalObservation(ctx context.Context, req *integrationv1.GetExternalObservationRequest) (*integrationv1.GetExternalObservationResponse, error) {
	return s.Handler.GetExternalObservation(ctx, req)
}
