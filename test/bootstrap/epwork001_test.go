package bootstrap_test

// TestTodo_EP_WORK_001_Integration reaches the real work_item store through
// both transports the same composition serves: the gRPC surface direct and
// tunneled, and the Connect edge. It seeds items through workitem.Store so
// the read path proves the projection chain end to end — durable rows, the
// queue reader's tenant scoping, membership and the server's disclosure
// rules — rather than a fake.

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"

	grpctunnel "github.com/monstercameron/GoGRPCBridge/pkg/grpctunnel"
	commonv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/common/v1"
	humanworkv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/humanwork/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	transportcell "github.com/monstercameron/human-capital-management-suite/internal/transport/cell"
	transporthumanwork "github.com/monstercameron/human-capital-management-suite/internal/transport/humanwork"
)

// TestTodo_EP_WORK_001_Integration seeds three real work items — one assigned
// to the caller, one claimed and in progress as an approval, one owned by
// another principal — and reads them through the same composed cell on all
// three transports.
func TestTodo_EP_WORK_001_Integration(t *testing.T) {
	c := newCell(t)
	ctx := context.Background()
	tenantUUID := pgstore.TenantID(testTenant)
	instanceID := uuid.New()

	c.db.Exec(t, `
		INSERT INTO workflow_instance (
			tenant_id, instance_id, cell_id, workflow_id, workflow_version,
			compiled_plan_hash, execution_mode, runtime_status, input_ref,
			current_node_ids, correlation_id, created_at)
		VALUES ($1, $2, $3, 'wf.promotion', 1,
			'0000000000000000000000000000000000000000000000000000000000000000',
			'SIMULATE', 'RUNNING', 'sha256:input', ARRAY['approval_node'], $4, $5)`,
		tenantUUID, instanceID, testCellID, "corr-"+instanceID.String(), baseTime)

	store := workitem.Store{}
	proposal := "sha256:" + strings.Repeat("9", 64)
	write := func(fn func(tx dbport.Tx) error) {
		t.Helper()
		tx, err := c.pool.Begin(ctx)
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		if err := tenancy.WithTenant(ctx, tx, tenantUUID); err != nil {
			t.Fatalf("scope tenant: %v", err)
		}
		if err := fn(tx); err != nil {
			t.Fatalf("seed work item: %v", err)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatalf("commit seed: %v", err)
		}
	}
	resolution := func(principals ...string) humanwork.Resolution {
		candidates := make([]humanwork.Candidate, len(principals))
		for i, p := range principals {
			candidates[i] = humanwork.Candidate{PrincipalID: p, Via: humanwork.SourceDirect, TermRef: "term.test"}
		}
		return humanwork.Resolution{
			RequirementID:    "req.bootstrap/v1",
			Outcome:          humanwork.OutcomeResolved,
			Candidates:       candidates,
			ResolvedAt:       values.NewInstant(baseTime),
			EffectiveAt:      values.NewInstant(baseTime),
			DirectoryVersion: "directory.test/1",
			QuorumRequired:   1,
		}
	}
	meta := workitem.TransitionMeta{ActorPrincipalID: "principal:seed", Reason: "seed", At: baseTime}
	newItem := func(deadline time.Time) workitem.WorkItem {
		item, err := workitem.NewWorkItem(workitem.NewWorkItemInput{
			TenantID: tenantUUID, Kind: workitem.KindTask,
			WorkType: "worktype.promotion.review/v1", CorrelationID: "corr-" + instanceID.String(),
			WorkflowInstanceID: instanceID, NodeID: "approval_node",
			SubjectRefs:         []string{"worker:jane"},
			PolicyRouteRef:      "route.promotion.current_manager/v1",
			Visibility:          workitem.VisibilityAssigneeOnly,
			OrganizationScopeID: testOrgScope,
			DeadlineAt:          deadline, CreatedAt: baseTime,
		})
		if err != nil {
			t.Fatalf("NewWorkItem: %v", err)
		}
		return item
	}

	var assigned, claimedItemID, strangerID uuid.UUID
	// item 1: ASSIGNED to the caller, latest deadline.
	seed := newItem(baseTime.Add(72 * time.Hour))
	write(func(tx dbport.Tx) error {
		stored, err := store.Create(ctx, tx, seed, meta)
		if err != nil {
			return err
		}
		routed, err := store.Route(ctx, tx, tenantUUID, stored.WorkItemID, stored.ItemVersion,
			workitem.Assignment{Resolution: resolution(testSubject)}, meta)
		if err != nil {
			return err
		}
		assigned = routed.WorkItemID
		return nil
	})
	// item 2: claimed and IN_PROGRESS approval for the caller, earliest deadline.
	seed = newItem(baseTime.Add(24 * time.Hour))
	seed.Kind = workitem.KindApproval
	seed.ApprovalRequirementRef = "req.approval.bootstrap/v1"
	seed.ProposalRef = proposal
	write(func(tx dbport.Tx) error {
		stored, err := store.Create(ctx, tx, seed, meta)
		if err != nil {
			return err
		}
		routed, err := store.Route(ctx, tx, tenantUUID, stored.WorkItemID, stored.ItemVersion,
			workitem.Assignment{Resolution: resolution(testSubject)}, meta)
		if err != nil {
			return err
		}
		claimed, err := store.Claim(ctx, tx, workitem.ClaimInput{
			TenantID: tenantUUID, WorkItemID: stored.WorkItemID, ExpectedVersion: routed.ItemVersion,
			ClaimantPrincipalID: testSubject, ClaimExpiresAt: baseTime.Add(4 * time.Hour),
			Now: baseTime, Meta: meta,
		})
		if err != nil {
			return err
		}
		if _, err := store.Start(ctx, tx, tenantUUID, stored.WorkItemID, claimed.ItemVersion, baseTime, meta); err != nil {
			return err
		}
		claimedItemID = stored.WorkItemID
		return nil
	})
	// item 3: owned by another principal — exists, but not this caller's.
	seed = newItem(baseTime.Add(48 * time.Hour))
	write(func(tx dbport.Tx) error {
		stored, err := store.Create(ctx, tx, seed, meta)
		if err != nil {
			return err
		}
		if _, err := store.Route(ctx, tx, tenantUUID, stored.WorkItemID, stored.ItemVersion,
			workitem.Assignment{Resolution: resolution("principal:other")}, meta); err != nil {
			return err
		}
		strangerID = stored.WorkItemID
		return nil
	})

	// The reader the composition hands the transports.
	queueReader := app.NewWorkItemQueueReader(c.pool,
		func(tenant values.TenantId) uuid.UUID { return pgstore.TenantID(string(tenant)) })
	cursorKey := []byte("bootstrap-work-queue-key")

	grpcServer, err := transportcell.NewGRPCServerWithWorkflowInspectorAndOperations(
		c.app, nil, queueReader, nil, cursorKey, transporthumanwork.WritePorts{})
	if err != nil {
		t.Fatalf("NewGRPCServerWithWorkflowInspectorAndOperations: %v", err)
	}
	t.Cleanup(grpcServer.Stop)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() { _ = grpcServer.Serve(listener) }()
	t.Cleanup(func() { _ = listener.Close() })

	edgeHandler, err := transportcell.NewEdgeHandlerWithTunnelAndDependencies(
		c.app, grpcServer, nil, queueReader, nil, cursorKey, transporthumanwork.WritePorts{})
	if err != nil {
		t.Fatalf("NewEdgeHandlerWithTunnelAndDependencies: %v", err)
	}
	httpServer := httptest.NewServer(edgeHandler)
	t.Cleanup(httpServer.Close)
	host := strings.TrimPrefix(httpServer.URL, "http://")

	direct, err := grpc.NewClient(listener.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc.NewClient: %v", err)
	}
	t.Cleanup(func() { _ = direct.Close() })

	tunnelCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	tunnelConn, err := grpctunnel.BuildTunnelConn(tunnelCtx, grpctunnel.TunnelConfig{
		Target:           "ws://" + host + transportcell.TunnelPath,
		Headers:          http.Header{"Authorization": []string{c.token}},
		HandshakeTimeout: 10 * time.Second,
		GRPCOptions:      []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())},
	})
	if err != nil {
		t.Fatalf("BuildTunnelConn: %v", err)
	}
	t.Cleanup(func() { _ = tunnelConn.Close() })

	callCtx := func() context.Context {
		ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		t.Cleanup(cancel)
		return metadata.AppendToOutgoingContext(ctx, transport.AuthorizationMetadataKey, c.token)
	}
	wantIDs := fmt.Sprint([]string{claimedItemID.String(), assigned.String()})

	// Direct gRPC and the tunnel must return the same two-item queue in
	// deadline order.
	for name, client := range map[string]humanworkv1.WorkServiceClient{
		"direct gRPC": humanworkv1.NewWorkServiceClient(direct),
		"tunnel gRPC": humanworkv1.NewWorkServiceClient(tunnelConn),
	} {
		list, err := client.ListWorkItems(callCtx(), &humanworkv1.ListWorkItemsRequest{})
		if err != nil {
			t.Fatalf("%s ListWorkItems: %v", name, err)
		}
		var ids []string
		for _, item := range list.GetWorkItems() {
			ids = append(ids, item.GetWorkItemId())
		}
		if fmt.Sprint(ids) != wantIDs {
			t.Fatalf("%s queue ids = %v, want %v", name, ids, wantIDs)
		}
		detail, err := client.GetWorkItem(callCtx(), &humanworkv1.GetWorkItemRequest{WorkItemId: claimedItemID.String()})
		if err != nil {
			t.Fatalf("%s GetWorkItem: %v", name, err)
		}
		got := detail.GetWorkItem()
		if got.GetProposalRef() != proposal || got.GetClaimedBy() != testSubject ||
			got.GetStatus() != humanworkv1.WorkItemStatus_WORK_ITEM_STATUS_IN_PROGRESS {
			t.Fatalf("%s claimed detail = %v", name, got)
		}
		if _, err := client.GetWorkItem(callCtx(), &humanworkv1.GetWorkItemRequest{WorkItemId: strangerID.String()}); err == nil {
			t.Fatalf("%s GetWorkItem(stranger) succeeded, want NOT_FOUND", name)
		}
	}

	// The Connect edge answers the same projection.
	list := connect.NewClient[humanworkv1.ListWorkItemsRequest, humanworkv1.ListWorkItemsResponse](
		httpServer.Client(), httpServer.URL+transporthumanwork.ListWorkItemsProcedure,
		connect.WithProtoJSON())
	req := connect.NewRequest(&humanworkv1.ListWorkItemsRequest{Page: &commonv1.PageRequest{PageSize: 10}})
	req.Header().Set(transport.AuthorizationMetadataKey, c.token)
	res, err := list.CallUnary(callCtx(), req)
	if err != nil {
		t.Fatalf("edge ListWorkItems: %v", err)
	}
	var edgeIDs []string
	for _, item := range res.Msg.GetWorkItems() {
		edgeIDs = append(edgeIDs, item.GetWorkItemId())
	}
	if fmt.Sprint(edgeIDs) != wantIDs {
		t.Fatalf("edge queue ids = %v, want %v", edgeIDs, wantIDs)
	}
	get := connect.NewClient[humanworkv1.GetWorkItemRequest, humanworkv1.GetWorkItemResponse](
		httpServer.Client(), httpServer.URL+transporthumanwork.GetWorkItemProcedure,
		connect.WithProtoJSON())
	getReq := connect.NewRequest(&humanworkv1.GetWorkItemRequest{WorkItemId: claimedItemID.String()})
	getReq.Header().Set(transport.AuthorizationMetadataKey, c.token)
	getRes, err := get.CallUnary(callCtx(), getReq)
	if err != nil {
		t.Fatalf("edge GetWorkItem: %v", err)
	}
	if getRes.Msg.GetWorkItem().GetProposalRef() != proposal {
		t.Fatalf("edge detail proposal_ref = %q, want %q", getRes.Msg.GetWorkItem().GetProposalRef(), proposal)
	}
}
