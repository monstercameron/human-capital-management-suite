package bootstrap_test

import (
	"context"
	"encoding/json"
	"net"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/reflect/protoreflect"

	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
	registryv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/registry/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	kernelvalues "github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	platformexecution "github.com/monstercameron/human-capital-management-suite/internal/platform/execution"
	transactioncommit "github.com/monstercameron/human-capital-management-suite/internal/transaction/commit"
	"github.com/monstercameron/human-capital-management-suite/internal/transaction/idempotency"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	transportcell "github.com/monstercameron/human-capital-management-suite/internal/transport/cell"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/edge"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/prototype"
)

// recordingTerminalWriter is the recording fake execute.TerminalWriter this
// suite uses in place of internal/workflow/execute/effects.LedgerTerminalWriter
// (a concurrent lane's own deliverable, and not this suite's to exercise): it
// records every call it receives and writes nothing itself, so
// TestExecuteIntentRunsThePromotionDriverUnderAuthority can assert that
// parking at the first approval WorkItem raises no governed business write at
// all.
type recordingTerminalWriter struct {
	mu    sync.Mutex
	calls []execute.TerminalWriteRequest
}

func (w *recordingTerminalWriter) Write(_ context.Context, _ dbport.Tx, req execute.TerminalWriteRequest) (idempotency.ResultIdentity, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.calls = append(w.calls, req)
	return idempotency.ResultIdentity{EventRef: "fake-terminal:" + req.InstanceID.String()}, nil
}

func (w *recordingTerminalWriter) Calls() []execute.TerminalWriteRequest {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]execute.TerminalWriteRequest(nil), w.calls...)
}

// newExecutionCell composes the same P1A cell [newCell] does — the same
// pgtest schema, pgstore.Store, HMAC verifier and issued credential — plus
// the P1B execution-authority wiring: the real caller-driven promotion
// driver (internal/platform/execution.NewPromotionExecution) over this test's
// own database pool and the real internal/workflow/runtime and
// internal/humanwork/workitem stores, with terminal standing in for the
// governed business write [ExecuteIntent]'s driver raises at END.
//
// It duplicates [newCell]'s composition body (rather than composing through
// it and rewiring) because internal/intent/app.NewCell is called exactly
// once per composed cell, and this suite's whole point is proving what one
// specific composition — one with ExecutionAuthority — does; a cell
// assembled by mutating another test's already-published cell would prove
// nothing about a real composition root doing the same thing once.
func newExecutionCell(t *testing.T, terminal execute.TerminalWriter) *cell {
	return newExecutionCellWithDB(t, terminal, nil)
}

// newExecutionCellWithDB is the same production composition with an optional
// database boundary used by DBEDGE003 to model a commit acknowledgement loss.
func newExecutionCellWithDB(t *testing.T, terminal execute.TerminalWriter, executionDB func(*pgxadapter.Pool) execute.Beginner) *cell {
	t.Helper()
	c := newCell(t)

	db := execute.Beginner(c.pool)
	if executionDB != nil {
		db = executionDB(c.pool)
	}
	var startRetry *transactioncommit.RetryOptions
	if executionDB != nil {
		startRetry = &transactioncommit.RetryOptions{MaxAttempts: 3, Admit: func(context.Context) error { return nil }}
	}
	execution, err := platformexecution.NewPromotionExecution(platformexecution.PromotionExecutionConfig{
		DB:                  db,
		StartRetry:          startRetry,
		Terminal:            terminal,
		Clock:               func() time.Time { return baseTime },
		ApproverPrincipalID: "principal:promotion-approver",
		AuthorityDigest:     "sha256:test-p1b-authority-amendment",
		RequiredRole:        executionAuthorityTestRole,
	})
	if err != nil {
		t.Fatalf("NewPromotionExecution: %v", err)
	}

	verifier, err := trust.NewHMACVerifier(trust.HMACVerifierConfig{
		Key:      testSigningKey,
		Issuer:   testIssuer,
		Audience: testAudience,
		Now:      func() time.Time { return baseTime },
	})
	if err != nil {
		t.Fatalf("NewHMACVerifier: %v", err)
	}
	token, err := verifier.Issue(trust.Claims{
		Issuer:               testIssuer,
		Audience:             testAudience,
		Subject:              testSubject,
		SubjectKind:          "human",
		Tenant:               testTenant,
		OrganizationScopeID:  testOrgScope,
		Roles:                []string{"intent_author", testRole, executionAuthorityTestRole},
		AuthorityRefs:        []string{"authority:position:vp-people"},
		Purposes:             []string{testPurpose, deniedPurpose, "workforce_analytics"},
		AuthenticationMethod: "bearer_token",
		Assurance:            "substantial",
		SessionRef:           "session-bootstrap-execution",
		IssuedAtUnix:         baseTime.Add(-time.Minute).Unix(),
		ExpiresAtUnix:        baseTime.Add(time.Hour).Unix(),
	})
	if err != nil {
		t.Fatalf("issue execution-authority credential: %v", err)
	}

	ec := &cell{t: t, db: c.db, pool: c.pool, store: c.store, token: "Bearer " + token}
	composed, err := app.NewCell(app.CellConfig{
		Store:       c.store,
		Verifier:    verifier,
		Audience:    testAudience,
		MaxDeadline: 30 * time.Second,
		Logger:      transport.LoggerFunc(ec.appendRecord),
		Now:         func() time.Time { return baseTime },

		Executor:           execution.Executor,
		ExecutionAuthority: execution.Authority,
		ExecutionResolver:  execution.Resolver,
		ExecutionVersions:  execution.Versions,
		ExecutionCellID:    testCellID,
		TenantUUID:         func(tenant kernelvalues.TenantId) uuid.UUID { return pgstore.TenantID(string(tenant)) },
		ExecutionDB:        c.pool,
		ExecutionFacts:     approvedExecutionFacts{},
	})
	if err != nil {
		t.Fatalf("app.NewCell (execution): %v", err)
	}
	ec.app = composed

	grpcServer, err := transportcell.NewGRPCServer(composed)
	if err != nil {
		t.Fatalf("GRPCServer: %v", err)
	}
	listener := bufconn.Listen(1 << 20)
	go func() { _ = grpcServer.Serve(listener) }()
	t.Cleanup(func() {
		grpcServer.Stop()
		_ = listener.Close()
	})
	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return listener.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc.NewClient: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	ec.grpcIntent = intentsv1.NewIntentServiceClient(conn)
	ec.grpcRegistry = registryv1.NewRegistryServiceClient(conn)

	edgeHandler, err := transportcell.NewEdgeHandler(composed)
	if err != nil {
		t.Fatalf("EdgeHandler: %v", err)
	}
	httpServer := httptest.NewServer(edgeHandler)
	t.Cleanup(httpServer.Close)
	ec.edgeIntent = edge.NewIntentClient(httpServer.Client(), httpServer.URL)
	ec.edgeRegistry = edge.NewRegistryClient(httpServer.Client(), httpServer.URL)
	ec.edgeURL = httpServer.URL
	ec.edgeClient = httpServer.Client()

	return ec
}

// mustParseUUID parses s or fails the test.
func mustParseUUID(t *testing.T, s string) uuid.UUID {
	t.Helper()
	id, err := uuid.Parse(s)
	if err != nil {
		t.Fatalf("parse uuid %q: %v", s, err)
	}
	return id
}

// inTenantConn runs fn with a tenant-scoped Executor, exactly the way every
// row-level-security-protected workflow/work-item table requires
// (internal/data/tenancy: the app.tenant_id session setting, set for the
// life of one transaction).
func inTenantConn(t *testing.T, c *cell, tenantID uuid.UUID, fn func(workitem.Executor) error) {
	t.Helper()
	ctx := context.Background()
	tx, err := c.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("tenant scope: %v", err)
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("transaction body: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}
}

// executionAuthorityTestRole is the role this suite's principal holds, so an
// execution-authority cell admits the same credential [newCell] already
// issued.
const executionAuthorityTestRole = "promotion_operator"

// TestExecuteIntentIsRefusedWithoutExecutionAuthority proves that a cell
// composed with no ExecutionAuthority — every cell today, including one that
// happens to carry a wired Executor — refuses ExecuteIntent under the exact
// P1A envelope every other governed write already uses, on both transports.
func TestExecuteIntentIsRefusedWithoutExecutionAuthority(t *testing.T) {
	c := newCell(t)
	seedWorkforce(t, c)
	ctx := c.grpcContext(context.Background())

	created, err := c.grpcIntent.CreateIntent(ctx, promoteWorkerRequest(t, "execute-refused-1"))
	if err != nil {
		t.Fatalf("CreateIntent: %v", err)
	}
	intentID := created.GetIntent().GetIntentId()
	req := &intentsv1.ExecuteIntentRequest{
		IdempotencyKey: "execute-refused-1",
		IntentId:       intentID,
		Approval: &intentsv1.ProposalApproval{
			ProposalRevisionId: "revision-does-not-matter",
			Approved:           true,
			ApprovalRef:        "approval:test-1",
		},
	}

	t.Run("grpc", func(t *testing.T) {
		_, err := c.grpcIntent.ExecuteIntent(ctx, req)
		if err == nil {
			t.Fatal("expected ExecuteIntent to be refused with no ExecutionAuthority configured")
		}
		owned, ok := envelope.FromGRPC(err)
		if !ok {
			t.Fatalf("grpc: ExecuteIntent failed with an unowned error: %v", err)
		}
		assertP1AWriteRefusal(t, "grpc", owned)
	})

	t.Run("edge", func(t *testing.T) {
		_, err := c.edgeIntent.ExecuteIntent(context.Background(), edgeRequest(c, req))
		if err == nil {
			t.Fatal("expected ExecuteIntent to be refused with no ExecutionAuthority configured")
		}
		owned, ok := edge.FromConnectError(err)
		if !ok {
			t.Fatalf("edge: ExecuteIntent failed with an unowned error: %v", err)
		}
		assertP1AWriteRefusal(t, "edge", owned)
	})

	// OBS-024: the authority gate's own refusal is recorded as evidence on
	// this cell's capability evidence sink -- the same mechanism CAP-002's
	// gateway already writes invocation/refusal evidence through -- for both
	// transports, before ExecuteIntent looked at the presented approval or
	// touched a single node.
	gateRefusals := 0
	for _, rec := range memoryEvidence(t, c.app).Records() {
		if rec.Decision == app.EvidenceKindGateRefused {
			gateRefusals++
			if rec.SubjectRef != intentID {
				t.Errorf("GATE_REFUSED evidence names intent %q, want %q", rec.SubjectRef, intentID)
			}
			// WF-RUN-035: the gate records the verified caller's tenant.
			if rec.Tenant != testTenant {
				t.Errorf("GATE_REFUSED evidence names tenant %q, want %q", rec.Tenant, testTenant)
			}
		}
	}
	if gateRefusals != 2 {
		t.Fatalf("GATE_REFUSED evidence entries = %d, want exactly 2 (grpc, edge)", gateRefusals)
	}
}

// assertP1AWriteRefusal asserts owned is exactly the shape every other P1A
// governed-write refusal (SubmitIntent, CancelIntent, SupersedeIntent) uses:
// FAILED_PRECONDITION, citing the release.p1a_zero_effect_ceiling rule.
func assertP1AWriteRefusal(t *testing.T, surface string, owned *envelope.Error) {
	t.Helper()
	if owned.Code() != envelope.CodeFailedPrecondition {
		t.Fatalf("%s: refused with %s, want FAILED_PRECONDITION", surface, owned.Code())
	}
	ceiling := false
	for _, violation := range owned.Violations() {
		if violation.RuleRef == "release.p1a_zero_effect_ceiling" {
			ceiling = true
		}
	}
	if !ceiling {
		t.Fatalf("%s: refused without citing the P1A ceiling: %v", surface, owned.Violations())
	}
}

// TestExecuteIntentRunsThePromotionDriverUnderAuthority proves the first
// executable run end to end: a cell explicitly composed with
// ExecutionAuthority and a real caller-driven driver runs ExecuteIntent for
// an approved promote_worker proposal, parks the new instance at its first
// approval WorkItem, returns a receipt naming it, and raises no governed
// business write at all while parked.
func TestExecuteIntentRunsThePromotionDriverUnderAuthority(t *testing.T) {
	terminal := &recordingTerminalWriter{}
	c := newExecutionCell(t, terminal)
	seedWorkforce(t, c)
	ctx := c.grpcContext(context.Background())

	created, err := c.grpcIntent.CreateIntent(ctx, promoteWorkerRequest(t, "execute-authority-1"))
	if err != nil {
		t.Fatalf("CreateIntent: %v", err)
	}
	intentID := created.GetIntent().GetIntentId()

	simulated, err := c.grpcIntent.SimulateIntent(ctx, &intentsv1.SimulateIntentRequest{IntentId: intentID})
	if err != nil {
		t.Fatalf("SimulateIntent: %v", err)
	}
	revisionID := simulated.GetSimulation().GetProposalRevisionId()
	if revisionID == "" {
		t.Fatal("simulation minted no proposal revision; the fixture is not READY")
	}
	materialDigest := simulated.GetSimulation().GetMaterialProposalDigest()

	execReq := &intentsv1.ExecuteIntentRequest{
		IdempotencyKey:          "execute-authority-1",
		IntentId:                intentID,
		ExpectedInstanceVersion: created.GetIntent().GetInstanceVersion(),
		Approval: &intentsv1.ProposalApproval{
			ProposalRevisionId:     revisionID,
			MaterialProposalDigest: materialDigest,
			Approved:               true,
			ApprovalRef:            "approval:execute-authority-1",
		},
	}

	executed, err := c.grpcIntent.ExecuteIntent(ctx, execReq)
	if err != nil {
		t.Fatalf("ExecuteIntent: %v", err)
	}
	receipt := executed.GetExecution()
	if receipt.GetInstanceId() == "" {
		t.Fatal("receipt names no workflow instance")
	}
	if receipt.GetInstanceVersion() == 0 {
		t.Fatal("receipt names no instance version")
	}
	//lint:ignore SA1019 wire compatibility: the bootstrap asserts the still-supported deprecated wire field.
	if len(receipt.GetParkedContinuations()) == 0 {
		t.Fatal("receipt names no parked continuation; the driver should have parked at the first approval")
	}
	found := false
	for _, node := range receipt.GetVisitedNodes() {
		if node == prototype.NodeApproval {
			found = true
		}
	}
	if !found {
		t.Fatalf("receipt visited nodes = %v, want %q among them", receipt.GetVisitedNodes(), prototype.NodeApproval)
	}

	// The instance parked at exactly one approval WorkItem, real and
	// durable in internal/humanwork/workitem's own store.
	tenantID := pgstore.TenantID(testTenant)
	instanceID := mustParseUUID(t, receipt.GetInstanceId())
	var items []workitem.WorkItem
	inTenantConn(t, c, tenantID, func(ex workitem.Executor) error {
		var loadErr error
		items, loadErr = workitem.Store{}.ListForInstance(context.Background(), ex, tenantID, instanceID)
		return loadErr
	})
	if len(items) != 1 {
		t.Fatalf("work items for instance %s = %d, want exactly 1", instanceID, len(items))
	}
	if items[0].WorkType != prototype.ApprovalRequirementID {
		t.Fatalf("parked work item type = %q, want %q", items[0].WorkType, prototype.ApprovalRequirementID)
	}
	if items[0].Status != workitem.StatusAssigned {
		t.Fatalf("parked work item status = %s, want %s", items[0].Status, workitem.StatusAssigned)
	}

	// Parking at APPROVAL never reaches END: the recording fake proves no
	// governed business write happened at all.
	if calls := terminal.Calls(); len(calls) != 0 {
		t.Fatalf("terminal writer calls = %d, want 0 while the instance is parked at approval", len(calls))
	}

	t.Run("edge transport agrees", func(t *testing.T) {
		// Exercise an independent promotion through the edge surface. The
		// first cell still owns Omar's active request, so a second request in
		// that cell would correctly be refused by the active-promotion guard.
		edgeTerminal := &recordingTerminalWriter{}
		edgeCell := newExecutionCell(t, edgeTerminal)
		seedWorkforce(t, edgeCell)
		created, err := edgeCell.edgeIntent.CreateIntent(context.Background(), edgeRequest(edgeCell, promoteWorkerRequest(t, "execute-authority-2")))
		if err != nil {
			t.Fatalf("CreateIntent: %v", err)
		}
		intentID := created.Msg.GetIntent().GetIntentId()
		simulated, err := edgeCell.edgeIntent.SimulateIntent(context.Background(),
			edgeRequest(edgeCell, &intentsv1.SimulateIntentRequest{IntentId: intentID}))
		if err != nil {
			t.Fatalf("SimulateIntent: %v", err)
		}
		execReq := &intentsv1.ExecuteIntentRequest{
			IdempotencyKey: "execute-authority-2",
			IntentId:       intentID,
			Approval: &intentsv1.ProposalApproval{
				ProposalRevisionId:     simulated.Msg.GetSimulation().GetProposalRevisionId(),
				MaterialProposalDigest: simulated.Msg.GetSimulation().GetMaterialProposalDigest(),
				Approved:               true,
				ApprovalRef:            "approval:execute-authority-2",
			},
		}
		executed, err := edgeCell.edgeIntent.ExecuteIntent(context.Background(), edgeRequest(edgeCell, execReq))
		if err != nil {
			t.Fatalf("ExecuteIntent (edge): %v", err)
		}
		if executed.Msg.GetExecution().GetInstanceId() == "" {
			t.Fatal("edge receipt names no workflow instance")
		}

		// OBS-024: the edge call's own authority-gate admission is recorded
		// too, on the same cell-wide evidence sink as the grpc call's.
		admitted := false
		for _, rec := range memoryEvidence(t, edgeCell.app).Records() {
			if rec.Decision == app.EvidenceKindGateAdmitted && rec.SubjectRef == intentID {
				admitted = true
			}
		}
		if !admitted {
			t.Fatal("no GATE_ADMITTED evidence recorded for the edge transport's own intent")
		}
		if calls := edgeTerminal.Calls(); len(calls) != 0 {
			t.Fatalf("edge terminal writer calls = %d, want 0 while approval is parked", len(calls))
		}
	})

	// OBS-024: the grpc call above admitted the gate before running the
	// driver; that decision is recorded as evidence, and its id leads the
	// Go-level ExecutionResult.EvidenceIDs (asserted through the driver's
	// own test/workflow suite, since ExecuteIntentResponse's wire receipt
	// carries no evidence_ids field yet).
	grpcAdmitted := false
	for _, rec := range memoryEvidence(t, c.app).Records() {
		if rec.Decision == app.EvidenceKindGateAdmitted && rec.SubjectRef == intentID {
			grpcAdmitted = true
		}
	}
	if !grpcAdmitted {
		t.Fatal("no GATE_ADMITTED evidence recorded for the grpc transport's own intent")
	}
}

// ---------------------------------------------------------------------------
// WF-RUN-032: the wire ExecutionReceipt names parked continuations and
// parked work items as two separate typed lists, never one under the
// other's name.
// ---------------------------------------------------------------------------

// executeAndPark drives one promote_worker intent through CreateIntent,
// SimulateIntent and ExecuteIntent, exactly as
// TestExecuteIntentRunsThePromotionDriverUnderAuthority does, and returns the
// receipt from an instance that parked at its first (and, for this bounded
// prototype graph, only) governed APPROVAL.
func executeAndPark(t *testing.T, c *cell, idempotencyKey string) *intentsv1.ExecutionReceipt {
	t.Helper()
	ctx := c.grpcContext(context.Background())

	created, err := c.grpcIntent.CreateIntent(ctx, promoteWorkerRequest(t, idempotencyKey))
	if err != nil {
		t.Fatalf("CreateIntent: %v", err)
	}
	intentID := created.GetIntent().GetIntentId()

	simulated, err := c.grpcIntent.SimulateIntent(ctx, &intentsv1.SimulateIntentRequest{IntentId: intentID})
	if err != nil {
		t.Fatalf("SimulateIntent: %v", err)
	}
	revisionID := simulated.GetSimulation().GetProposalRevisionId()
	if revisionID == "" {
		t.Fatal("simulation minted no proposal revision; the fixture is not READY")
	}

	executed, err := c.grpcIntent.ExecuteIntent(ctx, &intentsv1.ExecuteIntentRequest{
		IdempotencyKey:          idempotencyKey,
		IntentId:                intentID,
		ExpectedInstanceVersion: created.GetIntent().GetInstanceVersion(),
		Approval: &intentsv1.ProposalApproval{
			ProposalRevisionId:     revisionID,
			MaterialProposalDigest: simulated.GetSimulation().GetMaterialProposalDigest(),
			Approved:               true,
			ApprovalRef:            "approval:" + idempotencyKey,
		},
	})
	if err != nil {
		t.Fatalf("ExecuteIntent: %v", err)
	}
	return executed.GetExecution()
}

// TestTodo_WF_RUN_032 is the PRIMARY case: the receipt's parked_continuation_refs
// name the durable continuation (kind, target node) the instance is waiting
// on, and work_items names the durable WorkItem it raised, as two separate
// lists -- never a work-item id under the continuation name, and never a
// continuation's own scheduling kind (WORK_ITEM_REQUIRED) attributed to the
// work item.
func TestTodo_WF_RUN_032(t *testing.T) {
	terminal := &recordingTerminalWriter{}
	c := newExecutionCell(t, terminal)
	seedWorkforce(t, c)

	receipt := executeAndPark(t, c, "wf-run-032-primary")

	continuations := receipt.GetParkedContinuationRefs()
	if len(continuations) != 1 {
		t.Fatalf("parked_continuation_refs = %v, want exactly one", continuations)
	}
	cont := continuations[0]
	if cont.GetContinuationId() == "" {
		t.Fatal("parked continuation names no continuation id")
	}
	if cont.GetKind() != "WORK_ITEM_REQUIRED" {
		t.Fatalf("parked continuation kind = %q, want %q (a scheduling-intent kind, never a work-item kind)", cont.GetKind(), "WORK_ITEM_REQUIRED")
	}
	if cont.GetTargetNodeId() != prototype.NodeApproval {
		t.Fatalf("parked continuation target node = %q, want %q", cont.GetTargetNodeId(), prototype.NodeApproval)
	}

	items := receipt.GetWorkItems()
	if len(items) != 1 {
		t.Fatalf("work_items = %v, want exactly one", items)
	}
	item := items[0]
	if item.GetWorkItemId() == "" {
		t.Fatal("parked work item names no work item id")
	}
	if item.GetKind() != "APPROVAL" {
		t.Fatalf("parked work item kind = %q, want %q (a WorkItem kind, never a scheduling-intent kind)", item.GetKind(), "APPROVAL")
	}
	if item.GetNodeId() != prototype.NodeApproval {
		t.Fatalf("parked work item node = %q, want %q", item.GetNodeId(), prototype.NodeApproval)
	}

	// The work item id the receipt names is the exact durable row: real,
	// findable in internal/humanwork/workitem's own store, never invented.
	tenantID := pgstore.TenantID(testTenant)
	instanceID := mustParseUUID(t, receipt.GetInstanceId())
	workItemID := mustParseUUID(t, item.GetWorkItemId())
	var stored workitem.WorkItem
	inTenantConn(t, c, tenantID, func(ex workitem.Executor) error {
		var loadErr error
		stored, loadErr = workitem.Store{}.Load(context.Background(), ex, tenantID, workItemID)
		return loadErr
	})
	if stored.WorkflowInstanceID != instanceID || stored.NodeID != prototype.NodeApproval {
		t.Fatalf("stored work item = %+v, want instance %s node %s", stored, instanceID, prototype.NodeApproval)
	}

	// The deprecated string field is still populated (wire compatibility)
	// but is never the golden shape a new caller should read.
	//lint:ignore SA1019 wire compatibility: same compat pin as documented above.
	if len(receipt.GetParkedContinuations()) != 1 {
		//lint:ignore SA1019 wire compatibility: same compat pin as documented above.
		t.Fatalf("deprecated parked_continuations = %v, want exactly one entry for compatibility", receipt.GetParkedContinuations())
	}
}

// TestTodo_WF_RUN_032_Golden pins the wire (JSON, camelCase field names)
// shape of the typed lists: a field silently renamed or moved to the wrong
// message changes this golden.
func TestTodo_WF_RUN_032_Golden(t *testing.T) {
	terminal := &recordingTerminalWriter{}
	c := newExecutionCell(t, terminal)
	seedWorkforce(t, c)

	receipt := executeAndPark(t, c, "wf-run-032-golden")

	raw, err := protojson.Marshal(receipt)
	if err != nil {
		t.Fatalf("protojson.Marshal(ExecutionReceipt): %v", err)
	}
	var generic map[string]any
	if err := json.Unmarshal(raw, &generic); err != nil {
		t.Fatalf("unmarshal receipt JSON: %v", err)
	}

	rawContinuations, ok := generic["parkedContinuationRefs"].([]any)
	if !ok || len(rawContinuations) != 1 {
		t.Fatalf("receipt JSON parkedContinuationRefs = %v, want a one-element array", generic["parkedContinuationRefs"])
	}
	cont, ok := rawContinuations[0].(map[string]any)
	if !ok {
		t.Fatalf("parkedContinuationRefs[0] = %v, want an object", rawContinuations[0])
	}
	for _, field := range []string{"continuationId", "kind", "targetNodeId"} {
		if _, ok := cont[field]; !ok {
			t.Errorf("parkedContinuationRefs[0] is missing field %q", field)
		}
	}
	if _, ok := cont["workItemId"]; ok {
		t.Fatal("parkedContinuationRefs[0] names a workItemId; a continuation must never carry a work-item field")
	}

	rawItems, ok := generic["workItems"].([]any)
	if !ok || len(rawItems) != 1 {
		t.Fatalf("receipt JSON workItems = %v, want a one-element array", generic["workItems"])
	}
	item, ok := rawItems[0].(map[string]any)
	if !ok {
		t.Fatalf("workItems[0] = %v, want an object", rawItems[0])
	}
	for _, field := range []string{"workItemId", "kind", "nodeId"} {
		if _, ok := item[field]; !ok {
			t.Errorf("workItems[0] is missing field %q", field)
		}
	}
	if _, ok := item["continuationId"]; ok {
		t.Fatal("workItems[0] names a continuationId; a work item must never carry a continuation field")
	}
}

// TestTodo_WF_RUN_032_Conformance checks the wire contract itself, not one
// call's output: the compiled ExecutionReceipt message descriptor declares
// parked_continuation_refs (field 6) as a repeated ParkedContinuation and
// work_items (field 7) as a repeated ParkedWorkItem, each with its own,
// non-overlapping field set. This is transport-independent -- the same
// compiled descriptor backs both gRPC and any other decoder over the same
// wire bytes -- so it is what "over both transports" actually pins.
func TestTodo_WF_RUN_032_Conformance(t *testing.T) {
	desc := (&intentsv1.ExecutionReceipt{}).ProtoReflect().Descriptor()

	contField := desc.Fields().ByName("parked_continuation_refs")
	if contField == nil {
		t.Fatal("ExecutionReceipt descriptor has no parked_continuation_refs field")
	}
	if contField.Number() != 6 || contField.Cardinality() != protoreflect.Repeated || contField.Kind() != protoreflect.MessageKind {
		t.Fatalf("parked_continuation_refs = number %d cardinality %s kind %s, want 6/repeated/message",
			contField.Number(), contField.Cardinality(), contField.Kind())
	}
	if got := string(contField.Message().FullName()); got != "hcmnext.intents.v1.ParkedContinuation" {
		t.Fatalf("parked_continuation_refs message type = %q, want ParkedContinuation", got)
	}
	wantContFields := []string{"continuation_id", "kind", "target_node_id"}
	for _, name := range wantContFields {
		if contField.Message().Fields().ByName(protoreflect.Name(name)) == nil {
			t.Errorf("ParkedContinuation descriptor is missing field %q", name)
		}
	}
	for _, name := range []string{"work_item_id", "node_id"} {
		if contField.Message().Fields().ByName(protoreflect.Name(name)) != nil {
			t.Errorf("ParkedContinuation descriptor carries %q, a WorkItem-only field", name)
		}
	}

	itemField := desc.Fields().ByName("work_items")
	if itemField == nil {
		t.Fatal("ExecutionReceipt descriptor has no work_items field")
	}
	if itemField.Number() != 7 || itemField.Cardinality() != protoreflect.Repeated || itemField.Kind() != protoreflect.MessageKind {
		t.Fatalf("work_items = number %d cardinality %s kind %s, want 7/repeated/message",
			itemField.Number(), itemField.Cardinality(), itemField.Kind())
	}
	if got := string(itemField.Message().FullName()); got != "hcmnext.intents.v1.ParkedWorkItem" {
		t.Fatalf("work_items message type = %q, want ParkedWorkItem", got)
	}
	wantItemFields := []string{"work_item_id", "kind", "node_id"}
	for _, name := range wantItemFields {
		if itemField.Message().Fields().ByName(protoreflect.Name(name)) == nil {
			t.Errorf("ParkedWorkItem descriptor is missing field %q", name)
		}
	}
	for _, name := range []string{"continuation_id", "target_node_id"} {
		if itemField.Message().Fields().ByName(protoreflect.Name(name)) != nil {
			t.Errorf("ParkedWorkItem descriptor carries %q, a continuation-only field", name)
		}
	}

	// parked_continuations (field 3, the deprecated string list) still exists
	// for wire compatibility, but is a scalar list, never the typed shape.
	deprecatedField := desc.Fields().ByName("parked_continuations")
	if deprecatedField == nil || deprecatedField.Number() != 3 || deprecatedField.Kind() != protoreflect.StringKind {
		t.Fatalf("parked_continuations = %v, want field 3, string kind, still present for compatibility", deprecatedField)
	}
}
