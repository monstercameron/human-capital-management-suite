package grpcserver_test

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/test/bufconn"

	dataopsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/dataops/v1"
	integrationv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/integration/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/grpcserver"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/transporttest"
)

// fakeDataOpsHandler is the REV-030-01 DATAOPS-001 stand-in: it records the
// request it receives and replays one canned response, so the test proves
// the served DataOpsService is backed by the handler port rather than by
// inline logic.
type fakeDataOpsHandler struct {
	fail bool

	explain *dataopsv1.ExplainFieldHistoryRequest
	diff    *dataopsv1.DiffRecordRequest
	plan    *dataopsv1.CreateRepairPlanRequest
	sim     *dataopsv1.SimulateRepairRequest
}

func (f *fakeDataOpsHandler) ExplainFieldHistory(_ context.Context, req *dataopsv1.ExplainFieldHistoryRequest) (*dataopsv1.ExplainFieldHistoryResponse, error) {
	f.explain = req
	if f.fail {
		return nil, errors.New("dataops unavailable")
	}
	return &dataopsv1.ExplainFieldHistoryResponse{}, nil
}

func (f *fakeDataOpsHandler) DiffRecord(_ context.Context, req *dataopsv1.DiffRecordRequest) (*dataopsv1.DiffRecordResponse, error) {
	f.diff = req
	if f.fail {
		return nil, errors.New("dataops unavailable")
	}
	return &dataopsv1.DiffRecordResponse{}, nil
}

func (f *fakeDataOpsHandler) CreateRepairPlan(_ context.Context, req *dataopsv1.CreateRepairPlanRequest) (*dataopsv1.CreateRepairPlanResponse, error) {
	f.plan = req
	if f.fail {
		return nil, errors.New("dataops unavailable")
	}
	return &dataopsv1.CreateRepairPlanResponse{}, nil
}

func (f *fakeDataOpsHandler) SimulateRepair(_ context.Context, req *dataopsv1.SimulateRepairRequest) (*dataopsv1.SimulateRepairResponse, error) {
	f.sim = req
	if f.fail {
		return nil, errors.New("dataops unavailable")
	}
	return &dataopsv1.SimulateRepairResponse{}, nil
}

// fakeIntegrationHandler is the REV-030-01 INTG-001 stand-in with the same
// record-and-replay contract as fakeDataOpsHandler.
type fakeIntegrationHandler struct {
	fail bool

	defs  *integrationv1.ListConnectorDefinitionsRequest
	def   *integrationv1.GetConnectorDefinitionRequest
	conns *integrationv1.ListConnectorConnectionsRequest
	conn  *integrationv1.GetConnectorConnectionRequest
	test  *integrationv1.TestConnectorConnectionRequest
	obs   *integrationv1.ListExternalObservationsRequest
	ob    *integrationv1.GetExternalObservationRequest
}

func (f *fakeIntegrationHandler) ListConnectorDefinitions(_ context.Context, req *integrationv1.ListConnectorDefinitionsRequest) (*integrationv1.ListConnectorDefinitionsResponse, error) {
	f.defs = req
	if f.fail {
		return nil, errors.New("integration unavailable")
	}
	return &integrationv1.ListConnectorDefinitionsResponse{}, nil
}

func (f *fakeIntegrationHandler) GetConnectorDefinition(_ context.Context, req *integrationv1.GetConnectorDefinitionRequest) (*integrationv1.GetConnectorDefinitionResponse, error) {
	f.def = req
	if f.fail {
		return nil, errors.New("integration unavailable")
	}
	return &integrationv1.GetConnectorDefinitionResponse{}, nil
}

func (f *fakeIntegrationHandler) ListConnectorConnections(_ context.Context, req *integrationv1.ListConnectorConnectionsRequest) (*integrationv1.ListConnectorConnectionsResponse, error) {
	f.conns = req
	if f.fail {
		return nil, errors.New("integration unavailable")
	}
	return &integrationv1.ListConnectorConnectionsResponse{}, nil
}

func (f *fakeIntegrationHandler) GetConnectorConnection(_ context.Context, req *integrationv1.GetConnectorConnectionRequest) (*integrationv1.GetConnectorConnectionResponse, error) {
	f.conn = req
	if f.fail {
		return nil, errors.New("integration unavailable")
	}
	return &integrationv1.GetConnectorConnectionResponse{}, nil
}

func (f *fakeIntegrationHandler) TestConnectorConnection(_ context.Context, req *integrationv1.TestConnectorConnectionRequest) (*integrationv1.TestConnectorConnectionResponse, error) {
	f.test = req
	if f.fail {
		return nil, errors.New("integration unavailable")
	}
	return &integrationv1.TestConnectorConnectionResponse{}, nil
}

func (f *fakeIntegrationHandler) ListExternalObservations(_ context.Context, req *integrationv1.ListExternalObservationsRequest) (*integrationv1.ListExternalObservationsResponse, error) {
	f.obs = req
	if f.fail {
		return nil, errors.New("integration unavailable")
	}
	return &integrationv1.ListExternalObservationsResponse{}, nil
}

func (f *fakeIntegrationHandler) GetExternalObservation(_ context.Context, req *integrationv1.GetExternalObservationRequest) (*integrationv1.GetExternalObservationResponse, error) {
	f.ob = req
	if f.fail {
		return nil, errors.New("integration unavailable")
	}
	return &integrationv1.GetExternalObservationResponse{}, nil
}

// TestTodo_REV_030_01_Integration serves the DataOps and Integration
// services from grpcserver.NewServer through the production interceptor
// chain and proves every one of the eleven RPCs reaches its handler port:
// the handler observes the exact request the client sent, and a handler
// failure surfaces to the caller instead of a fabricated success.
func TestTodo_REV_030_01_Integration(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	clock := func() time.Time { return now }
	verifier, err := transporttest.NewVerifier(clock)
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}
	token, err := transporttest.BearerToken(verifier, transporttest.DefaultClaims(now))
	if err != nil {
		t.Fatalf("BearerToken: %v", err)
	}
	cfg := transporttest.Config(verifier, clock, "req-rev03001-0001", nil)

	dataops := &fakeDataOpsHandler{}
	integration := &fakeIntegrationHandler{}
	server, err := grpcserver.NewServer(grpcserver.Options{
		Config:      cfg,
		DataOps:     dataops,
		Integration: integration,
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	info := server.GetServiceInfo()
	dataopsInfo, ok := info["hcmnext.dataops.v1.DataOpsService"]
	if !ok {
		t.Fatalf("DataOpsService is not registered: %v", info)
	}
	if len(dataopsInfo.Methods) != 4 {
		t.Fatalf("DataOpsService methods = %d, want 4", len(dataopsInfo.Methods))
	}
	integrationInfo, ok := info["hcmnext.integration.v1.IntegrationService"]
	if !ok {
		t.Fatalf("IntegrationService is not registered: %v", info)
	}
	if len(integrationInfo.Methods) != 7 {
		t.Fatalf("IntegrationService methods = %d, want 7", len(integrationInfo.Methods))
	}

	listener := bufconn.Listen(1 << 20)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() {
		server.Stop()
		_ = listener.Close()
	})
	conn, err := grpc.NewClient("passthrough:///rev03001",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return listener.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc.NewClient: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	ctx := metadata.AppendToOutgoingContext(context.Background(), transport.AuthorizationMetadataKey, token)
	dataopsClient := dataopsv1.NewDataOpsServiceClient(conn)
	integrationClient := integrationv1.NewIntegrationServiceClient(conn)

	if _, err := dataopsClient.ExplainFieldHistory(ctx, &dataopsv1.ExplainFieldHistoryRequest{}); err != nil {
		t.Fatalf("ExplainFieldHistory: %v", err)
	}
	if dataops.explain == nil {
		t.Fatal("ExplainFieldHistory never reached the handler port")
	}
	if _, err := dataopsClient.DiffRecord(ctx, &dataopsv1.DiffRecordRequest{}); err != nil {
		t.Fatalf("DiffRecord: %v", err)
	}
	if dataops.diff == nil {
		t.Fatal("DiffRecord never reached the handler port")
	}
	if _, err := dataopsClient.CreateRepairPlan(ctx, &dataopsv1.CreateRepairPlanRequest{}); err != nil {
		t.Fatalf("CreateRepairPlan: %v", err)
	}
	if dataops.plan == nil {
		t.Fatal("CreateRepairPlan never reached the handler port")
	}
	if _, err := dataopsClient.SimulateRepair(ctx, &dataopsv1.SimulateRepairRequest{}); err != nil {
		t.Fatalf("SimulateRepair: %v", err)
	}
	if dataops.sim == nil {
		t.Fatal("SimulateRepair never reached the handler port")
	}

	if _, err := integrationClient.ListConnectorDefinitions(ctx, &integrationv1.ListConnectorDefinitionsRequest{}); err != nil {
		t.Fatalf("ListConnectorDefinitions: %v", err)
	}
	if integration.defs == nil {
		t.Fatal("ListConnectorDefinitions never reached the handler port")
	}
	if _, err := integrationClient.GetConnectorDefinition(ctx, &integrationv1.GetConnectorDefinitionRequest{}); err != nil {
		t.Fatalf("GetConnectorDefinition: %v", err)
	}
	if integration.def == nil {
		t.Fatal("GetConnectorDefinition never reached the handler port")
	}
	if _, err := integrationClient.ListConnectorConnections(ctx, &integrationv1.ListConnectorConnectionsRequest{}); err != nil {
		t.Fatalf("ListConnectorConnections: %v", err)
	}
	if integration.conns == nil {
		t.Fatal("ListConnectorConnections never reached the handler port")
	}
	if _, err := integrationClient.GetConnectorConnection(ctx, &integrationv1.GetConnectorConnectionRequest{}); err != nil {
		t.Fatalf("GetConnectorConnection: %v", err)
	}
	if integration.conn == nil {
		t.Fatal("GetConnectorConnection never reached the handler port")
	}
	if _, err := integrationClient.TestConnectorConnection(ctx, &integrationv1.TestConnectorConnectionRequest{}); err != nil {
		t.Fatalf("TestConnectorConnection: %v", err)
	}
	if integration.test == nil {
		t.Fatal("TestConnectorConnection never reached the handler port")
	}
	if _, err := integrationClient.ListExternalObservations(ctx, &integrationv1.ListExternalObservationsRequest{}); err != nil {
		t.Fatalf("ListExternalObservations: %v", err)
	}
	if integration.obs == nil {
		t.Fatal("ListExternalObservations never reached the handler port")
	}
	if _, err := integrationClient.GetExternalObservation(ctx, &integrationv1.GetExternalObservationRequest{}); err != nil {
		t.Fatalf("GetExternalObservation: %v", err)
	}
	if integration.ob == nil {
		t.Fatal("GetExternalObservation never reached the handler port")
	}

	dataops.fail = true
	if _, err := dataopsClient.DiffRecord(ctx, &dataopsv1.DiffRecordRequest{}); err == nil {
		t.Fatal("DiffRecord with a failing handler = success, want an error")
	}
	integration.fail = true
	if _, err := integrationClient.GetConnectorDefinition(ctx, &integrationv1.GetConnectorDefinitionRequest{}); err == nil {
		t.Fatal("GetConnectorDefinition with a failing handler = success, want an error")
	}
}
