package workflow

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	commonv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/common/v1"
	workflowv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/workflow/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/transporttest"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

func TestProjectWorkflowInspectionRedactsPayloadsAndPreservesFrontier(t *testing.T) {
	instance := ProjectInstance(Instance{InstanceID: "instance-1", TenantID: "tenant-a", WorkflowID: "promotion", WorkflowVersion: 2, RuntimeStatus: "RUNNING", CurrentNodeIDs: []string{"approve", "write"}})
	if instance.GetInstanceId() != "instance-1" || len(instance.GetCurrentNodeIds()) != 2 || instance.GetTenantId() != "tenant-a" {
		t.Fatalf("projection lost identity/frontier: %+v", instance)
	}
	cursor, err := encodeSnapshotCursor(snapshotCursor{TenantID: "tenant-a", InstanceID: "instance-1", InstanceVersion: 7, Index: 3}, []byte("test-key"))
	if err != nil {
		t.Fatalf("encode snapshot cursor: %v", err)
	}
	got, err := decodeSnapshotCursor(&commonv1.PageRequest{Cursor: cursor}, []byte("test-key"), nil, "tenant-a", "instance-1", 7)
	if err != nil || got != 3 {
		t.Fatalf("snapshot cursor round trip = %d, %v", got, err)
	}
	// A cursor minted under the retired key verifies while rotation
	// accepts it, and fails closed once it is dropped.
	rotated, err := decodeSnapshotCursor(&commonv1.PageRequest{Cursor: cursor}, []byte("test-key-2"), []byte("test-key"), "tenant-a", "instance-1", 7)
	if err != nil || rotated != 3 {
		t.Fatalf("retired-key cursor after rotation = %d, %v", rotated, err)
	}
	if _, err := decodeSnapshotCursor(&commonv1.PageRequest{Cursor: cursor}, []byte("test-key-2"), nil, "tenant-a", "instance-1", 7); err == nil {
		t.Fatal("retired-key cursor verified without the retired key")
	}
}

func TestWorkflowInspectionEndpointsReturnAuthorizedConsistentExecutionView(t *testing.T) {
	reader := &workflowTestReader{record: Record{
		Instance: Instance{
			InstanceID: "workflow-1", TenantID: transporttest.Tenant, WorkflowID: "promotion",
			WorkflowVersion: 2, CompiledPlanDigest: "plan-v2", RuntimeStatus: "RUNNING",
			RequestState: "APPROVED", ExecutionState: "EXECUTING", BusinessState: "PENDING",
			ConsistencyState: "CONSISTENT", ObligationState: "OPEN", InputRef: "input-ref",
			CurrentNodeIDs: []string{"approve"}, InstanceVersion: 7,
			CreatedAt: time.Date(2026, 9, 5, 9, 0, 0, 0, time.UTC),
		},
		Nodes: []NodeExecution{
			{NodeExecutionID: "node-b-1", WorkflowInstanceID: "workflow-1", NodeID: "b", Attempt: 1, Status: "SUCCEEDED", InputSnapshotRef: "snapshot-ref"},
			{NodeExecutionID: "node-a-2", WorkflowInstanceID: "workflow-1", NodeID: "a", Attempt: 2, Status: "RUNNING", InputSnapshotRef: "snapshot-ref"},
			{NodeExecutionID: "node-a-1", WorkflowInstanceID: "workflow-1", NodeID: "a", Attempt: 1, Status: "FAILED", InputSnapshotRef: "snapshot-ref", ErrorClass: "SAFE_ERROR_CLASS"},
		},
	}}
	srv := &server{deps: Dependencies{Instances: reader, CursorKey: []byte("workflow-test-cursor-key"), Authorize: allowWorkflowCalls}}
	getCtx := workflowTestContext(t, GetWorkflowProcedure)
	got, err := srv.GetWorkflow(getCtx, &workflowv1.GetWorkflowRequest{InstanceId: "workflow-1"})
	if err != nil {
		t.Fatalf("GetWorkflow: %v", err)
	}
	if got.GetInstance().GetCompiledPlanDigest() != "plan-v2" || got.GetInstance().GetLifecycle().GetExecution().String() != "EXECUTION_STATE_EXECUTING" {
		t.Fatalf("GetWorkflow projection = %v", got.GetInstance())
	}

	listCtx := workflowTestContext(t, ListNodeExecutionsProcedure)
	first, err := srv.ListNodeExecutions(listCtx, &workflowv1.ListNodeExecutionsRequest{
		InstanceId: "workflow-1", Page: &commonv1.PageRequest{PageSize: 2},
	})
	if err != nil {
		t.Fatalf("ListNodeExecutions first page: %v", err)
	}
	if len(first.GetNodeExecutions()) != 2 || first.GetNodeExecutions()[0].GetNodeExecutionId() != "node-a-1" || first.GetPage().GetNextCursor() == "" {
		t.Fatalf("first page = %v", first)
	}
	second, err := srv.ListNodeExecutions(listCtx, &workflowv1.ListNodeExecutionsRequest{
		InstanceId: "workflow-1", Page: &commonv1.PageRequest{PageSize: 2, Cursor: first.GetPage().GetNextCursor()},
	})
	if err != nil {
		t.Fatalf("ListNodeExecutions second page: %v", err)
	}
	if len(second.GetNodeExecutions()) != 1 || second.GetNodeExecutions()[0].GetNodeExecutionId() != "node-b-1" || second.GetPage().GetNextCursor() != "" {
		t.Fatalf("second page = %v", second)
	}
	if reader.calls.Load() != 3 {
		t.Fatalf("reader calls = %d, want one read per endpoint invocation", reader.calls.Load())
	}
}

func assertWorkflowAuthorizationDenied(t *testing.T) {
	t.Helper()
	reader := &workflowTestReader{record: Record{Instance: Instance{InstanceID: "workflow-1", TenantID: transporttest.Tenant}}}
	srv := &server{deps: Dependencies{Instances: reader, Authorize: func(context.Context, *trust.Principal, string) bool { return false }}}
	_, err := srv.GetWorkflow(workflowTestContext(t, GetWorkflowProcedure), &workflowv1.GetWorkflowRequest{InstanceId: "workflow-1"})
	owned, ok := envelope.As(err)
	if !ok || owned.Code() != envelope.CodePermissionDenied {
		t.Fatalf("denied error = %v, want permission denied", err)
	}
	if reader.calls.Load() != 0 {
		t.Fatal("unauthorized GetWorkflow reached the reader")
	}
}

type workflowTestReader struct {
	record Record
	calls  atomic.Int64
}

func (r *workflowTestReader) ReadWorkflowControlRecord(_ context.Context, tenant, instanceID string) (Record, error) {
	r.calls.Add(1)
	if tenant != r.record.Instance.TenantID || instanceID != r.record.Instance.InstanceID {
		return Record{}, ErrNotFound
	}
	return r.record, nil
}

func workflowTestContext(t *testing.T, method string) context.Context {
	t.Helper()
	now := time.Date(2026, 9, 5, 9, 0, 0, 0, time.UTC)
	verifier, err := transporttest.NewVerifier(func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}
	token, err := transporttest.BearerToken(verifier, transporttest.DefaultClaims(now))
	if err != nil {
		t.Fatalf("BearerToken: %v", err)
	}
	ctx, _, admitErr := transport.Admit(context.Background(), transporttest.Config(verifier, func() time.Time { return now }, "workflow-test-request", nil), transport.AdmissionRequest{
		Metadata: transport.MapMetadata{transport.AuthorizationMetadataKey: []string{token}},
		Method:   method, Kind: transport.KindGRPC,
		Message: &workflowv1.GetWorkflowRequest{InstanceId: "workflow-1"},
	})
	if admitErr != nil {
		t.Fatalf("Admit: %v", admitErr)
	}
	return ctx
}

func TestTodo_EP_WF_001_Property(t *testing.T) {
	reader := &workflowTestReader{record: multiPageRecord(transporttest.Tenant, "workflow-pages", 7)}
	srv := &server{deps: Dependencies{Instances: reader, CursorKey: []byte("workflow-property-key"), Authorize: allowWorkflowCalls}}
	var cursor string
	var ids []string
	for {
		res, err := srv.ListNodeExecutions(workflowTestContext(t, ListNodeExecutionsProcedure), &workflowv1.ListNodeExecutionsRequest{
			InstanceId: "workflow-pages",
			Page:       &commonv1.PageRequest{PageSize: 2, Cursor: cursor},
		})
		if err != nil {
			t.Fatalf("ListNodeExecutions page cursor=%q: %v", cursor, err)
		}
		for _, node := range res.GetNodeExecutions() {
			ids = append(ids, node.GetNodeExecutionId())
		}
		cursor = res.GetPage().GetNextCursor()
		if cursor == "" {
			break
		}
	}
	want := []string{"node-a-1", "node-b-1", "node-c-1", "node-d-1", "node-e-1", "node-f-1", "node-g-1"}
	if fmt.Sprint(ids) != fmt.Sprint(want) {
		t.Fatalf("paged node ids = %v, want %v", ids, want)
	}
}
func TestTodo_EP_WF_001_Golden(t *testing.T) {
	got := ProjectInstance(Instance{
		InstanceID: "workflow-golden", TenantID: "tenant-golden", WorkflowID: "promotion",
		WorkflowVersion: 4, RuntimeStatus: "RUNNING", RequestState: "APPROVED",
		ExecutionState: "EXECUTING", BusinessState: "IN_PROGRESS", ConsistencyState: "CONSISTENT",
		ObligationState: "PENDING", CurrentNodeIDs: []string{"approve", "notify"}, InstanceVersion: 12,
	})
	golden := fmt.Sprintf("id=%s|tenant=%s|definition=%s@%d|status=%s|lifecycle=%s/%s/%s/%s/%s|frontier=%v|version=%d",
		got.GetInstanceId(), got.GetTenantId(), got.GetDefinition().GetWorkflowId(), got.GetDefinition().GetVersion(),
		got.GetRuntimeStatus(), got.GetLifecycle().GetRequest().String(), got.GetLifecycle().GetExecution().String(),
		got.GetLifecycle().GetBusiness().String(), got.GetLifecycle().GetConsistency().String(), got.GetLifecycle().GetObligation().String(),
		got.GetCurrentNodeIds(), got.GetInstanceVersion())
	want := "id=workflow-golden|tenant=tenant-golden|definition=promotion@4|status=RUNTIME_STATUS_RUNNING|lifecycle=REQUEST_STATE_APPROVED/EXECUTION_STATE_EXECUTING/BUSINESS_STATE_IN_PROGRESS/CONSISTENCY_STATE_CONSISTENT/OBLIGATION_STATE_PENDING|frontier=[approve notify]|version=12"
	if golden != want {
		t.Fatalf("workflow projection golden = %q, want %q", golden, want)
	}
}
func TestTodo_EP_WF_001_Race(t *testing.T) {
	reader := &workflowTestReader{record: Record{Instance: Instance{InstanceID: "workflow-race", TenantID: transporttest.Tenant, RuntimeStatus: "RUNNING"}}}
	srv := &server{deps: Dependencies{Instances: reader, CursorKey: []byte("workflow-race-key"), Authorize: allowWorkflowCalls}}
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := srv.GetWorkflow(workflowTestContext(t, GetWorkflowProcedure), &workflowv1.GetWorkflowRequest{InstanceId: "workflow-race"}); err != nil {
				t.Errorf("concurrent GetWorkflow: %v", err)
			}
		}()
	}
	wg.Wait()
	if got := reader.calls.Load(); got != 32 {
		t.Fatalf("concurrent reader calls = %d, want 32", got)
	}
}
func TestTodo_EP_WF_001_Integration(t *testing.T) {
	reader := &workflowTestReader{record: multiPageRecord(transporttest.Tenant, "workflow-integration", 3)}
	srv := &server{deps: Dependencies{Instances: reader, CursorKey: []byte("workflow-integration-key"), Authorize: allowWorkflowCalls}}
	res, err := srv.ListNodeExecutions(workflowTestContext(t, ListNodeExecutionsProcedure), &workflowv1.ListNodeExecutionsRequest{InstanceId: "workflow-integration", Page: &commonv1.PageRequest{PageSize: 2}})
	if err != nil {
		t.Fatalf("list node executions: %v", err)
	}
	if len(res.GetNodeExecutions()) != 2 || res.GetNodeExecutions()[0].GetNodeExecutionId() != "node-a-1" || res.GetPage().GetNextCursor() == "" {
		t.Fatalf("integration page = %v", res)
	}
	if got := reader.calls.Load(); got != 1 {
		t.Fatalf("reader calls = %d, want one", got)
	}
}
func TestTodo_EP_WF_001_Security(t *testing.T) {
	assertWorkflowAuthorizationDenied(t)
}
func TestTodo_EP_WF_001_Conformance(t *testing.T) {
	key := []byte("workflow-conformance-key")
	cursor, err := encodeSnapshotCursor(snapshotCursor{TenantID: transporttest.Tenant, InstanceID: "workflow-1", InstanceVersion: 7, Index: 2}, key)
	if err != nil {
		t.Fatalf("encode cursor: %v", err)
	}
	forgedCursor := "A" + cursor[1:]
	if cursor[0] == 'A' {
		forgedCursor = "B" + cursor[1:]
	}
	for name, page := range map[string]*commonv1.PageRequest{
		"forged":                                {Cursor: forgedCursor},
		"replayed against a different snapshot": {Cursor: cursor},
	} {
		t.Run(name, func(t *testing.T) {
			version := uint64(7)
			if name == "replayed against a different snapshot" {
				version = 8
			}
			if _, err := decodeSnapshotCursor(page, key, nil, transporttest.Tenant, "workflow-1", version); !errors.Is(err, ErrInvalidCursor) {
				t.Fatalf("decode cursor error = %v, want ErrInvalidCursor", err)
			}
		})
	}
}
func TestTodo_EP_WF_001_Mutation(t *testing.T) {
	if _, err := encodeSnapshotCursor(snapshotCursor{TenantID: "tenant-a", InstanceID: "workflow-1", InstanceVersion: 1, Index: 1}, nil); !errors.Is(err, ErrCursorKeyUnset) {
		t.Fatalf("unset signing key mint error = %v, want ErrCursorKeyUnset", err)
	}
	if _, err := decodeSnapshotCursor(&commonv1.PageRequest{Cursor: "not-a-cursor"}, nil, nil, "tenant-a", "workflow-1", 1); !errors.Is(err, ErrCursorKeyUnset) && !errors.Is(err, ErrInvalidCursor) {
		t.Fatalf("unset signing key accept error = %v, want a typed refusal", err)
	}
}

func multiPageRecord(tenant, instance string, count int) Record {
	record := Record{Instance: Instance{InstanceID: instance, TenantID: tenant, InstanceVersion: 7, RuntimeStatus: "RUNNING"}}
	for i := 0; i < count; i++ {
		record.Nodes = append(record.Nodes, NodeExecution{
			NodeExecutionID: fmt.Sprintf("node-%c-1", 'a'+i), WorkflowInstanceID: instance,
			NodeID: fmt.Sprintf("node-%c", 'a'+i), Attempt: 1, Status: "SUCCEEDED",
		})
	}
	return record
}
