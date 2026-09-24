package cell

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/GoGRPCBridge/pkg/grpctunnel"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"

	adminv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/admin/v1"
	commonv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/common/v1"
	humanworkv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/humanwork/v1"
	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	workflowv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/workflow/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/operator/workflowcontrol"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/grpcserver"
	transporthumanwork "github.com/monstercameron/human-capital-management-suite/internal/transport/humanwork"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/transporttest"
	transportworkflow "github.com/monstercameron/human-capital-management-suite/internal/transport/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// The INTAPI-006 acceptance tests prove the served API surface at its
// published boundary: every defect below was a served behavior, so every
// proof below drives a real server — a real gRPC listener, a real tunnel
// bridge — and asserts on the wire answer. The focused unit proofs live
// beside the code they pin: RetryNode's reason in
// internal/transport/workflow/control_test.go, the threshold read in
// internal/transport/humanwork/threshold_test.go, ListJourneys paging in
// internal/transport/journey/listjourneys_test.go, cursor rotation in the
// cursor tests of humanwork, workflow, journey and streaming, and the
// tunnel policy in tunnel_services_test.go.

// intapiRetryControl records the governed requests the PRIMARY retry proof
// sends, answering one canned applied outcome.
type intapiRetryControl struct {
	reqs []workflowcontrol.Request
}

func (f *intapiRetryControl) Handle(_ context.Context, ids workflowcontrol.TenantIDs, req workflowcontrol.Request) (workflowcontrol.Response, error) {
	if _, err := ids(req.Tenant); err != nil {
		return workflowcontrol.Response{}, err
	}
	f.reqs = append(f.reqs, req)
	return workflowcontrol.Response{
		Outcome: workflowcontrol.OutcomeApplied, IntentInstanceID: "intent:operator:intapi-006",
		ReceiptDigest: "sha256:intapi-006", InstanceVersion: 9, InstanceStatus: "PAUSED",
		NodeID: "execute_promotion", Attempt: 2,
	}, nil
}

type intapiInstanceReader struct {
	record transportworkflow.Record
}

func (r *intapiInstanceReader) ReadWorkflowControlRecord(_ context.Context, tenant, instanceID string) (transportworkflow.Record, error) {
	if tenant != r.record.Instance.TenantID || instanceID != r.record.Instance.InstanceID {
		return transportworkflow.Record{}, transportworkflow.ErrNotFound
	}
	return r.record, nil
}

// intapiThresholds answers the one published table for the PRIMARY and
// GOLDEN proofs.
type intapiThresholds struct{}

func (intapiThresholds) GetThresholdTable(_ context.Context, tenant, tableID string) (transporthumanwork.ThresholdTable, error) {
	if tableID != "" && tableID != "hcmnext.rules.promotion_approval_threshold" {
		return transporthumanwork.ThresholdTable{}, transporthumanwork.ErrThresholdNotFound
	}
	return transporthumanwork.ThresholdTable{
		TableID: "hcmnext.rules.promotion_approval_threshold", Version: 1, VersionRef: "2026.1",
		TenantID:   tenant,
		InputNames: []string{"increase_percent", "band_position"},
		Rows: []transporthumanwork.ThresholdRow{
			{Conditions: []transporthumanwork.ThresholdCondition{{InputName: "increase_percent", Comparison: "GREATER_THAN(20.0000)"}}, Outcome: "EXECUTIVE_REQUIRED"},
			{Outcome: "STANDARD"},
		},
		HitPolicy: transporthumanwork.ThresholdHitPolicyFirst,
	}, nil
}

func intapiAdmittedServer(t *testing.T, register func(*grpc.Server)) (string, string) {
	t.Helper()
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	verifier, err := transporttest.NewVerifier(func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}
	cfg := transporttest.Config(verifier, func() time.Time { return now }, "intapi-006", nil)
	claims := transporttest.DefaultClaims(now)
	token, err := transporttest.BearerToken(verifier, claims)
	if err != nil {
		t.Fatalf("BearerToken: %v", err)
	}
	srv := grpc.NewServer(
		grpc.ChainUnaryInterceptor(grpcserver.UnaryInterceptor(cfg)),
		grpc.ChainStreamInterceptor(grpcserver.StreamInterceptor(cfg)),
	)
	register(srv)
	t.Cleanup(srv.Stop)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() { _ = srv.Serve(listener) }()
	t.Cleanup(func() { _ = listener.Close() })
	return listener.Addr().String(), token
}

// intapiQueueReader is the two-item Reader the cursor SECURITY proof pages
// through. Items are assigned to the fixture subject, so the admitted
// caller is a member of both and paging is about cursors, not membership.
type intapiQueueReader struct {
	items []workitem.WorkItem
}

func (r *intapiQueueReader) ListQueue(_ context.Context, _, _ string, _ time.Time) ([]workitem.WorkItem, error) {
	return append([]workitem.WorkItem(nil), r.items...), nil
}

func (r *intapiQueueReader) LoadItem(_ context.Context, _ string, id string) (workitem.WorkItem, error) {
	for _, item := range r.items {
		if item.WorkItemID.String() == id {
			return item, nil
		}
	}
	return workitem.WorkItem{}, transporthumanwork.ErrNotFound
}

func intapiQueueItem() workitem.WorkItem {
	return workitem.WorkItem{
		TenantID:            uuid.NewMD5(uuid.NameSpaceDNS, []byte(transporttest.Tenant)),
		WorkItemID:          uuid.New(),
		ItemVersion:         3,
		Kind:                workitem.KindTask,
		WorkType:            "worktype.review/v1",
		Status:              workitem.StatusAssigned,
		OwnerKind:           workitem.OwnerPrincipal,
		OwnerRef:            transporttest.Subject,
		Visibility:          workitem.VisibilityAssigneeOnly,
		OrganizationScopeID: transporttest.OrganizationScopeID,
		DeadlineAt:          time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC),
		CreatedAt:           time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC),
	}
}

func intapiPageRequest(cursor string, size int32) *commonv1.PageRequest {
	return &commonv1.PageRequest{Cursor: cursor, PageSize: size}
}

// flipLastByte flips a hex digit to a different hex digit, so a tampered
// cursor still parses and the refusal proves the MAC check fired rather
// than the encoding check.
func flipLastByte(b byte) byte {
	if b == 'a' {
		return 'b'
	}
	return 'a'
}

// intapiUpgrade performs one raw websocket upgrade request against a bridge
// and returns its status. It is a hand-built request rather than a dialer
// call because what is under test is the answer the edge gives before a
// socket exists, and a dialer reports only that it failed.
func intapiUpgrade(t *testing.T, edgeURL string, decorate func(*http.Request)) int {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, edgeURL+TunnelPath, nil)
	if err != nil {
		t.Fatalf("build the upgrade request: %v", err)
	}
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Sec-WebSocket-Version", "13")
	req.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
	if decorate != nil {
		decorate(req)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", TunnelPath, err)
	}
	defer func() { _ = res.Body.Close() }()
	_, _ = io.Copy(io.Discard, res.Body)
	return res.StatusCode
}

func intapiDial(t *testing.T, addr, token string) (*grpc.ClientConn, context.Context) {
	t.Helper()
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc.NewClient: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	return conn, metadata.AppendToOutgoingContext(ctx, transport.AuthorizationMetadataKey, token)
}

// TestTodo_INTAPI_006 is the todo's PRIMARY proof: the four served-surface
// defects fixed through their published RPCs. (ListJourneys paging is
// proved in internal/transport/journey/listjourneys_test.go, where the
// engine fakes live, and end to end in TestTodo_INTAPI_006_Integration.)
func TestTodo_INTAPI_006(t *testing.T) {
	t.Run("retry records the caller reason", func(t *testing.T) {
		ctl := &intapiRetryControl{}
		addr, token := intapiAdmittedServer(t, func(srv *grpc.Server) {
			transportworkflow.Register(srv, transportworkflow.Dependencies{
				Instances: &intapiInstanceReader{record: transportworkflow.Record{
					Instance: transportworkflow.Instance{InstanceID: "workflow-1", TenantID: transporttest.Tenant, RuntimeStatus: "PAUSED", InstanceVersion: 9},
					Nodes: []transportworkflow.NodeExecution{{NodeExecutionID: "n-2", WorkflowInstanceID: "workflow-1",
						NodeID: "execute_promotion", Attempt: 2, Status: "READY"}},
				}},
				Control: ctl, TenantIDs: func(values.TenantId) (uuid.UUID, error) { return uuid.New(), nil },
				Authorize: func(context.Context, *trust.Principal, string) bool { return true },
			})
		})
		conn, ctx := intapiDial(t, addr, token)
		client := workflowv1.NewWorkflowServiceClient(conn)
		res, err := client.RetryNode(ctx, &workflowv1.RetryNodeRequest{
			IdempotencyKey: "intapi-006-retry", InstanceId: "workflow-1",
			NodeId: "execute_promotion", ExpectedAttempt: 1, ReasonRef: "INC-006",
		})
		if err != nil {
			t.Fatalf("RetryNode: %v", err)
		}
		if res.GetReceipt().GetOutcome() != workflowv1.WorkflowControlOutcome_WORKFLOW_CONTROL_OUTCOME_APPLIED {
			t.Fatalf("retry outcome = %v, want APPLIED", res.GetReceipt().GetOutcome())
		}
		if len(ctl.reqs) != 1 {
			t.Fatalf("controller saw %d requests, want 1", len(ctl.reqs))
		}
		if ctl.reqs[0].ReasonRef != "INC-006" || ctl.reqs[0].IdempotencyKey != "intapi-006-retry" {
			t.Fatalf("controller request = %+v, want the caller reason, not the idempotency key", ctl.reqs[0])
		}
		if _, err := client.RetryNode(ctx, &workflowv1.RetryNodeRequest{
			IdempotencyKey: "intapi-006-retry-2", InstanceId: "workflow-1",
			NodeId: "execute_promotion", ExpectedAttempt: 1,
		}); status.Code(err) != codes.InvalidArgument {
			t.Fatalf("reasonless RetryNode = %v, want INVALID_ARGUMENT", err)
		}
	})

	t.Run("threshold table is served", func(t *testing.T) {
		addr, token := intapiAdmittedServer(t, func(srv *grpc.Server) {
			transporthumanwork.Register(srv, transporthumanwork.Dependencies{
				Thresholds: intapiThresholds{},
				Authorize:  func(context.Context, *trust.Principal, string) bool { return true },
			})
		})
		conn, ctx := intapiDial(t, addr, token)
		client := humanworkv1.NewWorkServiceClient(conn)
		res, err := client.GetThresholdTable(ctx, &humanworkv1.GetThresholdTableRequest{})
		if err != nil {
			t.Fatalf("GetThresholdTable: %v", err)
		}
		if res.GetTable().GetVersionRef() != "2026.1" || len(res.GetTable().GetRows()) != 2 {
			t.Fatalf("table = %v, want the published decision table", res.GetTable())
		}
		if _, err := client.GetThresholdTable(ctx, &humanworkv1.GetThresholdTableRequest{TableId: "no-such-table"}); status.Code(err) != codes.NotFound {
			t.Fatalf("unknown table = %v, want NOT_FOUND", err)
		}
	})

	t.Run("tunnel serves only workspace session clients and services", func(t *testing.T) {
		c, token := tunnelBridgeCell(t)
		srv, err := NewTunnelGRPCServer(c, nil, nil, nil, nil, transporthumanwork.WritePorts{}, nil)
		if err != nil {
			t.Fatalf("NewTunnelGRPCServer: %v", err)
		}
		t.Cleanup(srv.Stop)
		bridge, err := newTunnelHandler(c, srv, "")
		if err != nil {
			t.Fatalf("newTunnelHandler: %v", err)
		}
		edge := httptest.NewServer(bridge)
		t.Cleanup(edge.Close)
		host := strings.TrimPrefix(edge.URL, "http://")

		dial := func(t *testing.T, headers http.Header) *grpc.ClientConn {
			t.Helper()
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			t.Cleanup(cancel)
			conn, err := grpctunnel.BuildTunnelConn(ctx, grpctunnel.TunnelConfig{
				Target:  "ws://" + host + TunnelPath,
				Headers: headers,
				GRPCOptions: []grpc.DialOption{
					grpc.WithTransportCredentials(insecure.NewCredentials()),
				},
			})
			if err != nil {
				t.Fatalf("BuildTunnelConn: %v", err)
			}
			t.Cleanup(func() { _ = conn.Close() })
			return conn
		}

		// No Origin, valid credential: not a browser holding the page.
		if got := intapiUpgrade(t, edge.URL, func(r *http.Request) {
			r.Header.Set("Authorization", token)
		}); got != http.StatusForbidden {
			t.Fatalf("an upgrade without an Origin header got %d, want 403", got)
		}
		// Same-origin browser: the socket opens.
		if got := intapiUpgrade(t, edge.URL, func(r *http.Request) {
			r.Header.Set("Authorization", token)
			r.Header.Set("Origin", "http://"+host)
		}); got != http.StatusSwitchingProtocols {
			t.Fatalf("an authorized same-origin upgrade got %d, want 101", got)
		}

		browser := dial(t, http.Header{"Authorization": []string{token}, "Origin": []string{"http://" + host}})
		callCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		callCtx = metadata.AppendToOutgoingContext(callCtx, transport.AuthorizationMetadataKey, token)
		if _, err := journeyv1.NewJourneyServiceClient(browser).ListJourneys(callCtx, &journeyv1.ListJourneysRequest{}); status.Code(err) != codes.PermissionDenied {
			t.Fatalf("JourneyService over the tunnel = %v, want the handler's own denial (through the bridge)", err)
		}
		if _, err := adminv1.NewAdminServiceClient(browser).GetWorkflowInstance(callCtx, &adminv1.GetWorkflowInstanceRequest{}); status.Code(err) != codes.Unimplemented {
			t.Fatalf("AdminService over the tunnel = %v, want UNIMPLEMENTED at the bridge", err)
		}
	})
}

// TestTodo_INTAPI_006_Security denies without leakage and bounds every
// refusal: a cross-origin browser, a missing origin, an operator method
// over the browser route, a foreign-signed cursor and a reasonless retry
// all fail closed with a typed status and no payload.
func TestTodo_INTAPI_006_Security(t *testing.T) {
	t.Run("cross-origin upgrade is refused despite a valid credential", func(t *testing.T) {
		c, token := tunnelBridgeCell(t)
		srv, err := NewTunnelGRPCServer(c, nil, nil, nil, nil, transporthumanwork.WritePorts{}, nil)
		if err != nil {
			t.Fatalf("NewTunnelGRPCServer: %v", err)
		}
		t.Cleanup(srv.Stop)
		bridge, err := newTunnelHandler(c, srv, "")
		if err != nil {
			t.Fatalf("newTunnelHandler: %v", err)
		}
		edge := httptest.NewServer(bridge)
		t.Cleanup(edge.Close)
		if got := intapiUpgrade(t, edge.URL, func(r *http.Request) {
			r.Header.Set("Authorization", token)
			r.Header.Set("Origin", "https://evil.example")
		}); got != http.StatusForbidden {
			t.Fatalf("a cross-origin upgrade with a valid credential got %d, want 403", got)
		}
	})

	t.Run("page cursors verify under the dedicated key only", func(t *testing.T) {
		queue := &intapiQueueReader{items: []workitem.WorkItem{
			intapiQueueItem(), intapiQueueItem(),
		}}
		addr, token := intapiAdmittedServer(t, func(srv *grpc.Server) {
			transporthumanwork.Register(srv, transporthumanwork.Dependencies{
				Queue:     queue,
				CursorKey: []byte("intapi-006-dedicated-page-key-00000"),
				Authorize: func(context.Context, *trust.Principal, string) bool { return true },
			})
		})
		conn, ctx := intapiDial(t, addr, token)
		client := humanworkv1.NewWorkServiceClient(conn)
		first, err := client.ListWorkItems(ctx, &humanworkv1.ListWorkItemsRequest{
			Page: intapiPageRequest("", 1),
		})
		if err != nil {
			t.Fatalf("ListWorkItems: %v", err)
		}
		cursor := first.GetPage().GetNextCursor()
		if cursor == "" {
			t.Fatal("a two-item queue at size one issued no resume cursor")
		}
		// The issued cursor resumes: the dedicated key verifies its own.
		if _, err := client.ListWorkItems(ctx, &humanworkv1.ListWorkItemsRequest{
			Page: intapiPageRequest(cursor, 1),
		}); err != nil {
			t.Fatalf("resume under the dedicated key: %v", err)
		}
		// A cursor whose signature is flipped is a forgery, not a page:
		// verification is enforced, not decorative.
		forged := cursor[:len(cursor)-1] + string(flipLastByte(cursor[len(cursor)-1]))
		if _, err := client.ListWorkItems(ctx, &humanworkv1.ListWorkItemsRequest{
			Page: intapiPageRequest(forged, 1),
		}); status.Code(err) != codes.InvalidArgument {
			t.Fatalf("forged cursor = %v, want INVALID_ARGUMENT", err)
		}
	})
}

// TestTodo_INTAPI_006_Golden pins the two contract bytes this todo
// introduces: the closed tunnel service set, and the threshold table wire
// shape for a fixed port value. Either changes only by an explicit edit
// here and to testdata/intapi006.golden.json together.
func TestTodo_INTAPI_006_Golden(t *testing.T) {
	var allowed []string
	for name, ok := range tunnelAllowedServices {
		if ok {
			allowed = append(allowed, name)
		}
	}
	sort.Strings(allowed)

	addr, token := intapiAdmittedServer(t, func(srv *grpc.Server) {
		transporthumanwork.Register(srv, transporthumanwork.Dependencies{
			Thresholds: intapiThresholds{},
			Authorize:  func(context.Context, *trust.Principal, string) bool { return true },
		})
	})
	conn, ctx := intapiDial(t, addr, token)
	res, err := humanworkv1.NewWorkServiceClient(conn).GetThresholdTable(ctx, &humanworkv1.GetThresholdTableRequest{})
	if err != nil {
		t.Fatalf("GetThresholdTable: %v", err)
	}
	tableJSON := protojson.Format(res.GetTable())

	golden := map[string]any{}
	raw, err := os.ReadFile(filepath.Join("testdata", "intapi006.golden.json"))
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	if err := json.Unmarshal(raw, &golden); err != nil {
		t.Fatalf("parse golden: %v", err)
	}
	gotAllowed, _ := golden["tunnel_services"].([]any)
	if len(gotAllowed) != len(allowed) {
		t.Fatalf("tunnel_services = %v, want %v", gotAllowed, allowed)
	}
	for i, name := range allowed {
		if gotAllowed[i] != name {
			t.Fatalf("tunnel_services = %v, want %v", gotAllowed, allowed)
		}
	}
	var goldenTable map[string]any
	if err := json.Unmarshal([]byte(golden["threshold_table"].(string)), &goldenTable); err != nil {
		t.Fatalf("parse golden threshold table: %v", err)
	}
	var gotTable map[string]any
	if err := json.Unmarshal([]byte(tableJSON), &gotTable); err != nil {
		t.Fatalf("parse served threshold table: %v", err)
	}
	goldenBytes, _ := json.Marshal(goldenTable)
	gotBytes, _ := json.Marshal(gotTable)
	if string(goldenBytes) != string(gotBytes) {
		t.Fatalf("threshold table drifted:\n got %s\nwant %s", gotBytes, goldenBytes)
	}
}

// intapiHistoryEngine is the HistoryEngine the INTEGRATION proof composes:
// three canned summaries with genuine offset paging. It mints "offset:<n>"
// resume cursors itself and refuses anything else with the live engine's
// invalid-cursor refusal, so the bridge proves the page, resume and refusal
// path without a database behind it.
type intapiHistoryEngine struct {
	workspace.JourneyEngine

	mu      sync.Mutex
	items   []workspace.JourneySummary
	lastReq workspace.JourneyListRequest
}

func intapiHistorySummary(id string) workspace.JourneySummary {
	return workspace.JourneySummary{
		IntentID: id, WorkerName: "Worker " + id,
		Stage: workspace.JourneyStageProposed,
		Viewer: workspace.JourneyViewerProjection{
			Responsibility: workspace.JourneyResponsibilityTracking,
		},
		CreatedAt: time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC),
	}
}

func (f *intapiHistoryEngine) ListJourneysPage(_ context.Context, req workspace.JourneyListRequest) (workspace.JourneyListPage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastReq = req
	offset := 0
	if req.Cursor != "" {
		n, err := strconv.Atoi(strings.TrimPrefix(req.Cursor, "offset:"))
		if !strings.HasPrefix(req.Cursor, "offset:") || err != nil || n < 0 || n > len(f.items) {
			return workspace.JourneyListPage{}, errors.New("invalid history cursor")
		}
		offset = n
	}
	size := req.PageSize
	if size <= 0 {
		size = len(f.items)
	}
	end := offset + size
	if end > len(f.items) {
		end = len(f.items)
	}
	page := workspace.JourneyListPage{
		Journeys:   append([]workspace.JourneySummary(nil), f.items[offset:end]...),
		TotalCount: len(f.items),
	}
	if end < len(f.items) {
		page.NextCursor = "offset:" + strconv.Itoa(end)
	}
	return page, nil
}

func (f *intapiHistoryEngine) recordedReq() workspace.JourneyListRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastReq
}

// intapiRoleAccess is the durable role store the INTEGRATION proof composes:
// one assignment row admitting the fixture subject, the journeys page view
// grant the journey list requires, and the journey_list feature view grant
// beneath it. The work reads pass on the assignment row alone.
type intapiRoleAccess struct {
	snapshot roleaccess.Snapshot
}

func intapiComposedRoleAccess() intapiRoleAccess {
	return intapiRoleAccess{snapshot: roleaccess.Snapshot{
		Assignments: []roleaccess.Assignment{
			{WorkerRef: transporttest.Subject, RoleIDs: []string{transporttest.RoleIntentAuthor}},
		},
		PagePermissions: []roleaccess.PagePermission{
			{RoleID: transporttest.RoleIntentAuthor, PageID: "journeys", View: true},
		},
		FeaturePermissions: []roleaccess.FeaturePermission{
			{RoleID: transporttest.RoleIntentAuthor, PageID: "journeys", FeatureID: "journey_list", View: true},
		},
	}}
}

func (s intapiRoleAccess) Bootstrap(context.Context, values.TenantId, string) error {
	return nil
}

func (s intapiRoleAccess) Load(context.Context, values.TenantId, string) (roleaccess.Snapshot, error) {
	return s.snapshot, nil
}

func (s intapiRoleAccess) SaveRole(_ context.Context, _ values.TenantId, _ string, role roleaccess.Role) (roleaccess.Role, error) {
	return role, nil
}

func (s intapiRoleAccess) SaveAssignment(_ context.Context, _ values.TenantId, _ string, assignment roleaccess.Assignment) (roleaccess.Assignment, error) {
	return assignment, nil
}

func (s intapiRoleAccess) SaveVisibility(_ context.Context, _ values.TenantId, _, _ string, policy roleaccess.VisibilityPolicy) (roleaccess.VisibilityPolicy, error) {
	return policy, nil
}

func (s intapiRoleAccess) SavePagePermission(_ context.Context, _ values.TenantId, _ string, permission roleaccess.PagePermission) (roleaccess.PagePermission, error) {
	return permission, nil
}

func (s intapiRoleAccess) SaveFeaturePermission(_ context.Context, _ values.TenantId, _ string, permission roleaccess.FeaturePermission) (roleaccess.FeaturePermission, error) {
	return permission, nil
}

// TestTodo_INTAPI_006_Integration is the todo's INTEGRATION proof: one
// composed workspace tunnel server — the constructor the process mounts —
// serving a browser-tunnel client a paged journey list and the threshold
// table. The PRIMARY proof isolates each defect on its own server; this one
// proves the fixes compose: the wire filters travel to the engine, the
// resume cursor pages, a forged cursor fails closed, and the threshold read
// answers on the same bridge.
func TestTodo_INTAPI_006_Integration(t *testing.T) {
	c, token := tunnelBridgeCell(t)
	engine := &intapiHistoryEngine{items: []workspace.JourneySummary{
		intapiHistorySummary("intent-1"), intapiHistorySummary("intent-2"), intapiHistorySummary("intent-3"),
	}}
	c.Journey = engine
	c.RoleAccess = intapiComposedRoleAccess()
	srv, err := NewTunnelGRPCServer(c, nil, nil, nil, nil, transporthumanwork.WritePorts{}, intapiThresholds{})
	if err != nil {
		t.Fatalf("NewTunnelGRPCServer: %v", err)
	}
	t.Cleanup(srv.Stop)
	bridge, err := newTunnelHandler(c, srv, "")
	if err != nil {
		t.Fatalf("newTunnelHandler: %v", err)
	}
	edge := httptest.NewServer(bridge)
	t.Cleanup(edge.Close)
	host := strings.TrimPrefix(edge.URL, "http://")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	browser, err := grpctunnel.BuildTunnelConn(ctx, grpctunnel.TunnelConfig{
		Target:  "ws://" + host + TunnelPath,
		Headers: http.Header{"Authorization": []string{token}, "Origin": []string{"http://" + host}},
		GRPCOptions: []grpc.DialOption{
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		},
	})
	if err != nil {
		t.Fatalf("BuildTunnelConn: %v", err)
	}
	t.Cleanup(func() { _ = browser.Close() })
	callCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	callCtx = metadata.AppendToOutgoingContext(callCtx, transport.AuthorizationMetadataKey, token)

	journeys := journeyv1.NewJourneyServiceClient(browser)
	first, err := journeys.ListJourneys(callCtx, &journeyv1.ListJourneysRequest{
		Page: &commonv1.PageRequest{PageSize: 2}, WorkerRef: "worker-1", Query: "vega",
	})
	if err != nil {
		t.Fatalf("ListJourneys page 1 through the tunnel: %v", err)
	}
	if len(first.GetJourneys()) != 2 || first.GetTotalCount() != 3 {
		t.Fatalf("page 1 = %d journeys total %d, want 2 of 3", len(first.GetJourneys()), first.GetTotalCount())
	}
	if first.GetJourneys()[0].GetIntentId() != "intent-1" || first.GetJourneys()[1].GetIntentId() != "intent-2" {
		t.Fatalf("page 1 = %v, want intent-1 and intent-2", first.GetJourneys())
	}
	cursor := first.GetPage().GetNextCursor()
	if cursor == "" {
		t.Fatal("a two-of-three first page issued no resume cursor")
	}
	if got := engine.recordedReq(); got.PageSize != 2 || got.WorkerRef != "worker-1" || got.Query != "vega" {
		t.Fatalf("engine saw %+v, want the wire page size and filters mapped onto the port", got)
	}

	second, err := journeys.ListJourneys(callCtx, &journeyv1.ListJourneysRequest{
		Page: &commonv1.PageRequest{PageSize: 2, Cursor: cursor},
	})
	if err != nil {
		t.Fatalf("ListJourneys resume through the tunnel: %v", err)
	}
	if len(second.GetJourneys()) != 1 || second.GetJourneys()[0].GetIntentId() != "intent-3" || second.GetTotalCount() != 3 {
		t.Fatalf("resume = %v total %d, want intent-3 of 3", second.GetJourneys(), second.GetTotalCount())
	}

	if _, err := journeys.ListJourneys(callCtx, &journeyv1.ListJourneysRequest{
		Page: &commonv1.PageRequest{Cursor: "offset:999"},
	}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("forged page cursor through the tunnel = %v, want INVALID_ARGUMENT", err)
	}

	table, err := humanworkv1.NewWorkServiceClient(browser).GetThresholdTable(callCtx, &humanworkv1.GetThresholdTableRequest{})
	if err != nil {
		t.Fatalf("GetThresholdTable through the tunnel: %v", err)
	}
	if table.GetTable().GetVersionRef() != "2026.1" || len(table.GetTable().GetRows()) != 2 {
		t.Fatalf("threshold table through the tunnel = %v, want the published decision table", table.GetTable())
	}
}
