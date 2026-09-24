package clients_test

import (
	"context"
	"net"
	"net/http/httptest"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"

	commonv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/common/v1"
	dataopsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/dataops/v1"
	integrationv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/integration/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/clients"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/edge"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/grpcserver"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/transporttest"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type parityDataOps struct{}

func (parityDataOps) ExplainFieldHistory(context.Context, *dataopsv1.ExplainFieldHistoryRequest) (*dataopsv1.ExplainFieldHistoryResponse, error) {
	return &dataopsv1.ExplainFieldHistoryResponse{Explanation: &dataopsv1.HistoryExplanation{Subject: &commonv1.EntityRef{Id: "worker-parity"}}}, nil
}
func (parityDataOps) DiffRecord(context.Context, *dataopsv1.DiffRecordRequest) (*dataopsv1.DiffRecordResponse, error) {
	return &dataopsv1.DiffRecordResponse{Diff: &dataopsv1.RecordDiff{Digest: "sha256:parity"}}, nil
}
func (parityDataOps) CreateRepairPlan(context.Context, *dataopsv1.CreateRepairPlanRequest) (*dataopsv1.CreateRepairPlanResponse, error) {
	return &dataopsv1.CreateRepairPlanResponse{Plan: &dataopsv1.RepairPlan{Id: "plan-parity"}}, nil
}
func (parityDataOps) SimulateRepair(context.Context, *dataopsv1.SimulateRepairRequest) (*dataopsv1.SimulateRepairResponse, error) {
	return &dataopsv1.SimulateRepairResponse{Simulation: &dataopsv1.RepairSimulation{PlanDigest: "sha256:parity"}}, nil
}

type parityIntegration struct{}

func (parityIntegration) ListConnectorDefinitions(context.Context, *integrationv1.ListConnectorDefinitionsRequest) (*integrationv1.ListConnectorDefinitionsResponse, error) {
	return &integrationv1.ListConnectorDefinitionsResponse{Page: &commonv1.PageResponse{}}, nil
}
func (parityIntegration) GetConnectorDefinition(context.Context, *integrationv1.GetConnectorDefinitionRequest) (*integrationv1.GetConnectorDefinitionResponse, error) {
	return &integrationv1.GetConnectorDefinitionResponse{ConnectorDefinition: &integrationv1.ConnectorDefinition{ConnectorId: "connector-parity"}}, nil
}
func (parityIntegration) ListConnectorConnections(context.Context, *integrationv1.ListConnectorConnectionsRequest) (*integrationv1.ListConnectorConnectionsResponse, error) {
	return &integrationv1.ListConnectorConnectionsResponse{Page: &commonv1.PageResponse{}}, nil
}
func (parityIntegration) GetConnectorConnection(context.Context, *integrationv1.GetConnectorConnectionRequest) (*integrationv1.GetConnectorConnectionResponse, error) {
	return &integrationv1.GetConnectorConnectionResponse{Connection: &integrationv1.ConnectorConnection{ConnectionId: "connection-parity"}}, nil
}
func (parityIntegration) TestConnectorConnection(context.Context, *integrationv1.TestConnectorConnectionRequest) (*integrationv1.TestConnectorConnectionResponse, error) {
	return &integrationv1.TestConnectorConnectionResponse{Diagnostic: &integrationv1.ConnectionDiagnostic{ConnectionId: "connection-parity", TestedAt: timestamppb.New(baseTime)}}, nil
}
func (parityIntegration) ListExternalObservations(context.Context, *integrationv1.ListExternalObservationsRequest) (*integrationv1.ListExternalObservationsResponse, error) {
	return &integrationv1.ListExternalObservationsResponse{Page: &commonv1.PageResponse{}}, nil
}
func (parityIntegration) GetExternalObservation(context.Context, *integrationv1.GetExternalObservationRequest) (*integrationv1.GetExternalObservationResponse, error) {
	return &integrationv1.GetExternalObservationResponse{Observation: &integrationv1.ExternalObservation{ObservationId: "observation-parity"}}, nil
}

// TestTodo_PROTO_006_DataOpsIntegration exercises generated gRPC and Connect
// clients against the same admitted handlers for every newly published unary
// endpoint. The fakes return distinctive values so absent or misrouted RPCs
// cannot pass as transport parity.
func TestTodo_PROTO_006_DataOpsIntegration(t *testing.T) {
	h := newHarness(t)
	cfg := transporttest.Config(h.verifier, h.clock.Now, fixedRequestID, transport.LoggerFunc(h.appendRecord))
	do, integ := parityDataOps{}, parityIntegration{}
	server, err := grpcserver.NewServer(grpcserver.Options{Config: cfg, Intent: h.intent, Registry: h.registry, DataOps: do, Integration: integ})
	if err != nil {
		t.Fatal(err)
	}
	listener := bufconn.Listen(1 << 20)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { server.Stop(); _ = listener.Close() })
	conn, err := grpc.NewClient("passthrough:///bufnet-rev030",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return listener.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	edgeHandler, err := edge.NewHandler(edge.Options{Config: cfg, Intent: h.intent, Registry: h.registry, DataOps: do, Integration: integ})
	if err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(edgeHandler)
	t.Cleanup(httpServer.Close)
	grpcDO, grpcI := clients.NewDataOpsClientGRPC(conn), clients.NewIntegrationClientGRPC(conn)
	connDO := clients.NewDataOpsClientConnect(httpServer.Client(), httpServer.URL)
	connI := clients.NewIntegrationClientConnect(httpServer.Client(), httpServer.URL)
	scope := &commonv1.ScopeContext{TenantId: transporttest.Tenant, OrganizationScopeId: transporttest.OrganizationScopeID, Purpose: transporttest.PurposeAnalytics}
	type call struct {
		name          string
		grpc, connect func(context.Context, ...clients.CallOption) (proto.Message, error)
	}
	calls := []call{
		{"DataOps.ExplainFieldHistory", func(ctx context.Context, o ...clients.CallOption) (proto.Message, error) {
			return grpcDO.ExplainFieldHistory(ctx, &dataopsv1.ExplainFieldHistoryRequest{Scope: scope}, o...)
		}, func(ctx context.Context, o ...clients.CallOption) (proto.Message, error) {
			return msgOrNil(connDO.ExplainFieldHistory(ctx, &dataopsv1.ExplainFieldHistoryRequest{Scope: scope}, o...))
		}},
		{"DataOps.DiffRecord", func(ctx context.Context, o ...clients.CallOption) (proto.Message, error) {
			return grpcDO.DiffRecord(ctx, &dataopsv1.DiffRecordRequest{Scope: scope}, o...)
		}, func(ctx context.Context, o ...clients.CallOption) (proto.Message, error) {
			return msgOrNil(connDO.DiffRecord(ctx, &dataopsv1.DiffRecordRequest{Scope: scope}, o...))
		}},
		{"DataOps.CreateRepairPlan", func(ctx context.Context, o ...clients.CallOption) (proto.Message, error) {
			return grpcDO.CreateRepairPlan(ctx, &dataopsv1.CreateRepairPlanRequest{Scope: scope}, o...)
		}, func(ctx context.Context, o ...clients.CallOption) (proto.Message, error) {
			return msgOrNil(connDO.CreateRepairPlan(ctx, &dataopsv1.CreateRepairPlanRequest{Scope: scope}, o...))
		}},
		{"DataOps.SimulateRepair", func(ctx context.Context, o ...clients.CallOption) (proto.Message, error) {
			return grpcDO.SimulateRepair(ctx, &dataopsv1.SimulateRepairRequest{Scope: scope}, o...)
		}, func(ctx context.Context, o ...clients.CallOption) (proto.Message, error) {
			return msgOrNil(connDO.SimulateRepair(ctx, &dataopsv1.SimulateRepairRequest{Scope: scope}, o...))
		}},
		{"Integration.ListConnectorDefinitions", func(ctx context.Context, o ...clients.CallOption) (proto.Message, error) {
			return grpcI.ListConnectorDefinitions(ctx, &integrationv1.ListConnectorDefinitionsRequest{Scope: scope}, o...)
		}, func(ctx context.Context, o ...clients.CallOption) (proto.Message, error) {
			return msgOrNil(connI.ListConnectorDefinitions(ctx, &integrationv1.ListConnectorDefinitionsRequest{Scope: scope}, o...))
		}},
		{"Integration.GetConnectorDefinition", func(ctx context.Context, o ...clients.CallOption) (proto.Message, error) {
			return grpcI.GetConnectorDefinition(ctx, &integrationv1.GetConnectorDefinitionRequest{Scope: scope}, o...)
		}, func(ctx context.Context, o ...clients.CallOption) (proto.Message, error) {
			return msgOrNil(connI.GetConnectorDefinition(ctx, &integrationv1.GetConnectorDefinitionRequest{Scope: scope}, o...))
		}},
		{"Integration.ListConnectorConnections", func(ctx context.Context, o ...clients.CallOption) (proto.Message, error) {
			return grpcI.ListConnectorConnections(ctx, &integrationv1.ListConnectorConnectionsRequest{Scope: scope}, o...)
		}, func(ctx context.Context, o ...clients.CallOption) (proto.Message, error) {
			return msgOrNil(connI.ListConnectorConnections(ctx, &integrationv1.ListConnectorConnectionsRequest{Scope: scope}, o...))
		}},
		{"Integration.GetConnectorConnection", func(ctx context.Context, o ...clients.CallOption) (proto.Message, error) {
			return grpcI.GetConnectorConnection(ctx, &integrationv1.GetConnectorConnectionRequest{Scope: scope}, o...)
		}, func(ctx context.Context, o ...clients.CallOption) (proto.Message, error) {
			return msgOrNil(connI.GetConnectorConnection(ctx, &integrationv1.GetConnectorConnectionRequest{Scope: scope}, o...))
		}},
		{"Integration.TestConnectorConnection", func(ctx context.Context, o ...clients.CallOption) (proto.Message, error) {
			return grpcI.TestConnectorConnection(ctx, &integrationv1.TestConnectorConnectionRequest{Scope: scope}, o...)
		}, func(ctx context.Context, o ...clients.CallOption) (proto.Message, error) {
			return msgOrNil(connI.TestConnectorConnection(ctx, &integrationv1.TestConnectorConnectionRequest{Scope: scope}, o...))
		}},
		{"Integration.ListExternalObservations", func(ctx context.Context, o ...clients.CallOption) (proto.Message, error) {
			return grpcI.ListExternalObservations(ctx, &integrationv1.ListExternalObservationsRequest{Scope: scope}, o...)
		}, func(ctx context.Context, o ...clients.CallOption) (proto.Message, error) {
			return msgOrNil(connI.ListExternalObservations(ctx, &integrationv1.ListExternalObservationsRequest{Scope: scope}, o...))
		}},
		{"Integration.GetExternalObservation", func(ctx context.Context, o ...clients.CallOption) (proto.Message, error) {
			return grpcI.GetExternalObservation(ctx, &integrationv1.GetExternalObservationRequest{Scope: scope}, o...)
		}, func(ctx context.Context, o ...clients.CallOption) (proto.Message, error) {
			return msgOrNil(connI.GetExternalObservation(ctx, &integrationv1.GetExternalObservationRequest{Scope: scope}, o...))
		}},
	}
	for _, tc := range calls {
		t.Run(tc.name, func(t *testing.T) {
			grpcResult, grpcErr := tc.grpc(context.Background(), h.authOpts()...)
			connectResult, connectErr := tc.connect(context.Background(), h.authOpts()...)
			if (grpcErr == nil) != (connectErr == nil) {
				t.Fatalf("gRPC error %v differs from Connect error %v", grpcErr, connectErr)
			}
			if grpcErr != nil {
				assertOwnedParity(t, tc.name, ownedError(t, grpcErr), ownedError(t, connectErr))
				return
			}
			if !proto.Equal(grpcResult, connectResult) {
				t.Fatalf("gRPC %v differs from Connect %v", grpcResult, connectResult)
			}
		})
	}
}
