package cell

import (
	"fmt"

	"google.golang.org/grpc"

	dataopsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/dataops/v1"
	integrationv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/integration/v1"
	transportdataops "github.com/monstercameron/human-capital-management-suite/internal/transport/dataops"
	transportintegration "github.com/monstercameron/human-capital-management-suite/internal/transport/integration"
)

// RegisterDataOpsAndIntegration registers the application-owned DataOps and
// Integration handlers on an already-admitted gRPC server. The server must
// have been created by the canonical cell composition so these procedures
// inherit the normal authentication, validation and telemetry interceptors.
func RegisterDataOpsAndIntegration(server *grpc.Server, handlers ServiceHandlers) error {
	if server == nil {
		return fmt.Errorf("transport cell: gRPC server is required")
	}
	if handlers.DataOps != nil {
		dataopsv1.RegisterDataOpsServiceServer(server, &transportdataops.Service{Handler: handlers.DataOps, Stage: handlers.DataOpsStage})
	} else if handlers.DataOpsStage != nil {
		return fmt.Errorf("transport cell: DataOps handler is required when StageCSV is configured")
	}
	if handlers.Integration != nil {
		integrationv1.RegisterIntegrationServiceServer(server, &transportintegration.Service{Handler: handlers.Integration})
	}
	return nil
}
