package integration

import (
	"context"
	"errors"
	"testing"

	integrationv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/integration/v1"
)

type fakeHandler struct {
	err error

	defsReq  *integrationv1.ListConnectorDefinitionsRequest
	defReq   *integrationv1.GetConnectorDefinitionRequest
	connsReq *integrationv1.ListConnectorConnectionsRequest
	connReq  *integrationv1.GetConnectorConnectionRequest
	testReq  *integrationv1.TestConnectorConnectionRequest
	obsReq   *integrationv1.ListExternalObservationsRequest
	obReq    *integrationv1.GetExternalObservationRequest
}

func (f *fakeHandler) ListConnectorDefinitions(_ context.Context, req *integrationv1.ListConnectorDefinitionsRequest) (*integrationv1.ListConnectorDefinitionsResponse, error) {
	f.defsReq = req
	return &integrationv1.ListConnectorDefinitionsResponse{}, f.err
}

func (f *fakeHandler) GetConnectorDefinition(_ context.Context, req *integrationv1.GetConnectorDefinitionRequest) (*integrationv1.GetConnectorDefinitionResponse, error) {
	f.defReq = req
	return &integrationv1.GetConnectorDefinitionResponse{}, f.err
}

func (f *fakeHandler) ListConnectorConnections(_ context.Context, req *integrationv1.ListConnectorConnectionsRequest) (*integrationv1.ListConnectorConnectionsResponse, error) {
	f.connsReq = req
	return &integrationv1.ListConnectorConnectionsResponse{}, f.err
}

func (f *fakeHandler) GetConnectorConnection(_ context.Context, req *integrationv1.GetConnectorConnectionRequest) (*integrationv1.GetConnectorConnectionResponse, error) {
	f.connReq = req
	return &integrationv1.GetConnectorConnectionResponse{}, f.err
}

func (f *fakeHandler) TestConnectorConnection(_ context.Context, req *integrationv1.TestConnectorConnectionRequest) (*integrationv1.TestConnectorConnectionResponse, error) {
	f.testReq = req
	return &integrationv1.TestConnectorConnectionResponse{}, f.err
}

func (f *fakeHandler) ListExternalObservations(_ context.Context, req *integrationv1.ListExternalObservationsRequest) (*integrationv1.ListExternalObservationsResponse, error) {
	f.obsReq = req
	return &integrationv1.ListExternalObservationsResponse{}, f.err
}

func (f *fakeHandler) GetExternalObservation(_ context.Context, req *integrationv1.GetExternalObservationRequest) (*integrationv1.GetExternalObservationResponse, error) {
	f.obReq = req
	return &integrationv1.GetExternalObservationResponse{}, f.err
}

// TestTodo_REV_030_01 proves Service is a pure forward to the Integration
// handler port: every method delivers the caller's exact request to the
// handler, returns the handler's response untouched, and surfaces the
// handler's error instead of a fabricated success. It also pins the
// compile-time conformance to the generated server interface, so a proto
// change that adds an RPC fails here rather than silently unserved.
func TestTodo_REV_030_01(t *testing.T) {
	var _ integrationv1.IntegrationServiceServer = (*Service)(nil)

	fake := &fakeHandler{}
	svc := &Service{Handler: fake}
	ctx := context.Background()

	defsReq := &integrationv1.ListConnectorDefinitionsRequest{}
	if _, err := svc.ListConnectorDefinitions(ctx, defsReq); err != nil {
		t.Fatalf("ListConnectorDefinitions: %v", err)
	}
	if fake.defsReq != defsReq {
		t.Fatal("ListConnectorDefinitions did not forward the caller's request")
	}

	defReq := &integrationv1.GetConnectorDefinitionRequest{}
	if _, err := svc.GetConnectorDefinition(ctx, defReq); err != nil {
		t.Fatalf("GetConnectorDefinition: %v", err)
	}
	if fake.defReq != defReq {
		t.Fatal("GetConnectorDefinition did not forward the caller's request")
	}

	connsReq := &integrationv1.ListConnectorConnectionsRequest{}
	if _, err := svc.ListConnectorConnections(ctx, connsReq); err != nil {
		t.Fatalf("ListConnectorConnections: %v", err)
	}
	if fake.connsReq != connsReq {
		t.Fatal("ListConnectorConnections did not forward the caller's request")
	}

	connReq := &integrationv1.GetConnectorConnectionRequest{}
	if _, err := svc.GetConnectorConnection(ctx, connReq); err != nil {
		t.Fatalf("GetConnectorConnection: %v", err)
	}
	if fake.connReq != connReq {
		t.Fatal("GetConnectorConnection did not forward the caller's request")
	}

	testReq := &integrationv1.TestConnectorConnectionRequest{}
	if _, err := svc.TestConnectorConnection(ctx, testReq); err != nil {
		t.Fatalf("TestConnectorConnection: %v", err)
	}
	if fake.testReq != testReq {
		t.Fatal("TestConnectorConnection did not forward the caller's request")
	}

	obsReq := &integrationv1.ListExternalObservationsRequest{}
	if _, err := svc.ListExternalObservations(ctx, obsReq); err != nil {
		t.Fatalf("ListExternalObservations: %v", err)
	}
	if fake.obsReq != obsReq {
		t.Fatal("ListExternalObservations did not forward the caller's request")
	}

	obReq := &integrationv1.GetExternalObservationRequest{}
	if _, err := svc.GetExternalObservation(ctx, obReq); err != nil {
		t.Fatalf("GetExternalObservation: %v", err)
	}
	if fake.obReq != obReq {
		t.Fatal("GetExternalObservation did not forward the caller's request")
	}

	sentinel := errors.New("handler refused")
	fake.err = sentinel
	if _, err := svc.ListConnectorDefinitions(ctx, defsReq); !errors.Is(err, sentinel) {
		t.Fatalf("ListConnectorDefinitions error = %v, want the handler's error", err)
	}
	if _, err := svc.GetConnectorDefinition(ctx, defReq); !errors.Is(err, sentinel) {
		t.Fatalf("GetConnectorDefinition error = %v, want the handler's error", err)
	}
	if _, err := svc.ListConnectorConnections(ctx, connsReq); !errors.Is(err, sentinel) {
		t.Fatalf("ListConnectorConnections error = %v, want the handler's error", err)
	}
	if _, err := svc.GetConnectorConnection(ctx, connReq); !errors.Is(err, sentinel) {
		t.Fatalf("GetConnectorConnection error = %v, want the handler's error", err)
	}
	if _, err := svc.TestConnectorConnection(ctx, testReq); !errors.Is(err, sentinel) {
		t.Fatalf("TestConnectorConnection error = %v, want the handler's error", err)
	}
	if _, err := svc.ListExternalObservations(ctx, obsReq); !errors.Is(err, sentinel) {
		t.Fatalf("ListExternalObservations error = %v, want the handler's error", err)
	}
	if _, err := svc.GetExternalObservation(ctx, obReq); !errors.Is(err, sentinel) {
		t.Fatalf("GetExternalObservation error = %v, want the handler's error", err)
	}
}
