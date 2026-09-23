package cell

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	evidencev1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/evidence/v1"
	workflowv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/workflow/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	transporthumanwork "github.com/monstercameron/human-capital-management-suite/internal/transport/humanwork"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/manifest"
	transportoperations "github.com/monstercameron/human-capital-management-suite/internal/transport/operations"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/streaming"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/transporttest"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

func TestComposedHTTPHandlerUsesOperationStore(t *testing.T) {
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	verifier, err := transporttest.NewVerifier(func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}
	tenant := uuid.MustParse("11111111-1111-4111-8111-111111111111").String()
	claims := transporttest.DefaultClaims(now)
	claims.Tenant = tenant
	token, err := transporttest.BearerToken(verifier, claims)
	if err != nil {
		t.Fatalf("BearerToken: %v", err)
	}
	store := transportoperations.NewMemoryStore(func() time.Time { return now })
	if err := store.Put(transportoperations.Record{
		OperationID: "operation-cell-composed", TenantID: tenant, Owner: transporttest.Subject,
		RequestType: "hcmnext.intent.v1.ExecuteIntent", State: streaming.OperationRunning,
	}); err != nil {
		t.Fatalf("Put operation: %v", err)
	}
	h, err := NewEdgeHandlerWithDependencies(&app.Cell{
		Config:    transporttest.Config(verifier, func() time.Time { return now }, "cell-composition", nil),
		Discovery: &manifest.DiscoveryDocument{},
	}, nil, nil, store, []byte("cell-composition-cursor-key"), nil, transporthumanwork.WritePorts{}, nil)
	if err != nil {
		t.Fatalf("NewEdgeHandlerWithDependencies: %v", err)
	}
	server := httptest.NewServer(h)
	t.Cleanup(server.Close)
	client := connect.NewClient[evidencev1.GetOperationRequest, evidencev1.GetOperationResponse](
		server.Client(), server.URL+transportoperations.GetOperationProcedure, connect.WithProtoJSON(),
	)
	req := connect.NewRequest(&evidencev1.GetOperationRequest{OperationId: "operation-cell-composed"})
	req.Header().Set(transport.AuthorizationMetadataKey, token)
	req.Header().Set(transport.RequestIDMetadataKey, "cell-operation-fixture")
	res, err := client.CallUnary(context.Background(), req)
	if err != nil {
		t.Fatalf("GetOperation through composed HTTP edge: %v", err)
	}
	if res.Msg.GetOperation().GetOperationId() != "operation-cell-composed" || res.Msg.GetOperation().GetState() != evidencev1.OperationState_OPERATION_STATE_RUNNING {
		t.Fatalf("composed operation response = %+v, want durable running operation", res.Msg)
	}
}

func TestComposedHTTPHandlerUsesWorkflowReader(t *testing.T) {
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	verifier, err := transporttest.NewVerifier(func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}
	tenantID := uuid.MustParse("22222222-2222-4222-8222-222222222222")
	instanceID := uuid.MustParse("33333333-3333-4333-8333-333333333333")
	claims := transporttest.DefaultClaims(now)
	claims.Tenant = tenantID.String()
	token, err := transporttest.BearerToken(verifier, claims)
	if err != nil {
		t.Fatalf("BearerToken: %v", err)
	}
	reader := fakeWorkflowReader{record: app.WorkflowInstanceRecord{Instance: runtime.Instance{
		TenantID: tenantID, InstanceID: instanceID, WorkflowID: "promotion", WorkflowVersion: 3,
		RuntimeStatus: runtime.InstanceRunning, InstanceVersion: 9,
	}}}
	// RBAC-RT-004: the composed edge authorizes through the durable hook,
	// so the fixture grants its subject the durable operator role; without
	// it the composed call is refused before it reaches the reader.
	h, err := NewEdgeHandlerWithDependencies(&app.Cell{
		Config:    transporttest.Config(verifier, func() time.Time { return now }, "cell-workflow-composition", nil),
		Discovery: &manifest.DiscoveryDocument{},
		RoleAccess: hookStore{snapshot: assignmentSnapshot(transporttest.Subject,
			"hcmnext.trust.role.operator")},
	}, reader, nil, nil, []byte("cell-workflow-cursor-key"), nil, transporthumanwork.WritePorts{}, nil)
	if err != nil {
		t.Fatalf("NewEdgeHandlerWithDependencies: %v", err)
	}
	server := httptest.NewServer(h)
	t.Cleanup(server.Close)
	client := connect.NewClient[workflowv1.GetWorkflowRequest, workflowv1.GetWorkflowResponse](
		server.Client(), server.URL+"/hcmnext.workflow.v1.WorkflowService/GetWorkflow", connect.WithProtoJSON(),
	)
	req := connect.NewRequest(&workflowv1.GetWorkflowRequest{InstanceId: instanceID.String()})
	req.Header().Set(transport.AuthorizationMetadataKey, token)
	req.Header().Set(transport.RequestIDMetadataKey, "cell-workflow-fixture")
	res, err := client.CallUnary(context.Background(), req)
	if err != nil {
		t.Fatalf("GetWorkflow through composed HTTP edge: %v", err)
	}
	if res.Msg.GetInstance().GetInstanceId() != instanceID.String() || res.Msg.GetInstance().GetTenantId() != tenantID.String() {
		t.Fatalf("composed workflow response = %+v, want reader truth", res.Msg)
	}
}
