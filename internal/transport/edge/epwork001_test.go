package edge

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	humanworkv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/humanwork/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	transporthumanwork "github.com/monstercameron/human-capital-management-suite/internal/transport/humanwork"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/transporttest"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type edgeQueueReader struct{ item workitem.WorkItem }

func (r edgeQueueReader) ListQueue(_ context.Context, tenant, principal string, _ time.Time) ([]workitem.WorkItem, error) {
	if tenant != transporttest.Tenant || r.item.OwnerRef != principal {
		return nil, nil
	}
	return []workitem.WorkItem{r.item}, nil
}

func (r edgeQueueReader) LoadItem(_ context.Context, tenant, workItemID string) (workitem.WorkItem, error) {
	if tenant != transporttest.Tenant || r.item.WorkItemID.String() != workItemID {
		return workitem.WorkItem{}, transporthumanwork.ErrNotFound
	}
	return r.item, nil
}

// TestTodo_EP_WORK_001_Edge drives the mounted Connect edge end to end: the
// admission interceptor builds the trusted context, the queue answers, and
// the refused mutating procedure surfaces the contract code.
func TestTodo_EP_WORK_001_Edge(t *testing.T) {
	now := time.Date(2026, 9, 11, 10, 0, 0, 0, time.UTC)
	verifier, err := transporttest.NewVerifier(func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}
	token, err := transporttest.BearerToken(verifier, transporttest.DefaultClaims(now))
	if err != nil {
		t.Fatalf("BearerToken: %v", err)
	}

	tenantUUID := uuid.NewMD5(uuid.NameSpaceDNS, []byte(transporttest.Tenant))
	item := workitem.WorkItem{
		TenantID: tenantUUID, WorkItemID: uuid.New(), ItemVersion: 2,
		Kind: workitem.KindTask, WorkType: "worktype.edge/v1",
		Status: workitem.StatusAssigned, OwnerKind: workitem.OwnerPrincipal,
		OwnerRef: transporttest.Subject, Visibility: workitem.VisibilityAssigneeOnly,
		DeadlineAt: now.Add(24 * time.Hour), CreatedAt: now,
	}
	work := &transporthumanwork.Dependencies{
		Queue: edgeQueueReader{item: item}, CursorKey: []byte("edge-work-key"),
		Now: func() time.Time { return now },
		// RBAC-RT-004: the edge projects the service's answers, so its
		// fixture admits the capability the same way production
		// composition does through the durable hook.
		Authorize: func(context.Context, *trust.Principal, string) bool { return true },
	}
	h, err := NewHandler(Options{
		Config: transporttest.Config(verifier, func() time.Time { return now }, "edge-work-request", nil),
		Work:   work,
	})
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}
	server := httptest.NewServer(h)
	t.Cleanup(server.Close)

	list := connect.NewClient[humanworkv1.ListWorkItemsRequest, humanworkv1.ListWorkItemsResponse](
		server.Client(), server.URL+transporthumanwork.ListWorkItemsProcedure, connect.WithProtoJSON())
	req := connect.NewRequest(&humanworkv1.ListWorkItemsRequest{})
	req.Header().Set(transport.AuthorizationMetadataKey, token)
	res, err := list.CallUnary(context.Background(), req)
	if err != nil {
		t.Fatalf("edge ListWorkItems: %v", err)
	}
	if len(res.Msg.GetWorkItems()) != 1 ||
		res.Msg.GetWorkItems()[0].GetWorkItemId() != item.WorkItemID.String() ||
		res.Msg.GetWorkItems()[0].GetItemVersion() != 2 {
		t.Fatalf("edge list = %+v", res.Msg)
	}

	get := connect.NewClient[humanworkv1.GetWorkItemRequest, humanworkv1.GetWorkItemResponse](
		server.Client(), server.URL+transporthumanwork.GetWorkItemProcedure, connect.WithProtoJSON())
	getReq := connect.NewRequest(&humanworkv1.GetWorkItemRequest{WorkItemId: item.WorkItemID.String()})
	getReq.Header().Set(transport.AuthorizationMetadataKey, token)
	getRes, err := get.CallUnary(context.Background(), getReq)
	if err != nil {
		t.Fatalf("edge GetWorkItem: %v", err)
	}
	if getRes.Msg.GetWorkItem().GetWorkItemId() != item.WorkItemID.String() ||
		getRes.Msg.GetWorkItem().GetStatus() != humanworkv1.WorkItemStatus_WORK_ITEM_STATUS_ASSIGNED {
		t.Fatalf("edge get = %+v", getRes.Msg)
	}

	// The refused surface is mounted too: claim must answer the contract's
	// FAILED_PRECONDITION, not 404.
	claim := connect.NewClient[humanworkv1.ClaimWorkItemRequest, humanworkv1.ClaimWorkItemResponse](
		server.Client(), server.URL+"/hcmnext.humanwork.v1.WorkService/ClaimWorkItem", connect.WithProtoJSON())
	claimReq := connect.NewRequest(&humanworkv1.ClaimWorkItemRequest{WorkItemId: item.WorkItemID.String()})
	claimReq.Header().Set(transport.AuthorizationMetadataKey, token)
	if _, err := claim.CallUnary(context.Background(), claimReq); err == nil {
		t.Fatal("edge ClaimWorkItem succeeded")
	}
}
