package bootstrap_test

import (
	"context"
	"errors"
	"net"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
	registryv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/registry/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	kernelvalues "github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	ledgerport "github.com/monstercameron/human-capital-management-suite/internal/ledger"
	platformexecution "github.com/monstercameron/human-capital-management-suite/internal/platform/execution"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	transportcell "github.com/monstercameron/human-capital-management-suite/internal/transport/cell"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/edge"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute/effects"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/prototype"
)

// The Promotion journey end to end, over the same composed cell
// [newExecutionCell] publishes plus the two fields the journey engine needs:
// the execution database it reads the durable record through, and the approver
// principal its approval WorkItem is routed to.
//
// The terminal writer here is the real
// internal/workflow/execute/effects.LedgerTerminalWriter rather than this
// suite's recording fake, because the whole point of the journey's last step
// is that the END node's governed business write lands as one durable
// ledger_event row a page can read back.

// journeyApprover is who this suite's approval WorkItem is routed to. It is
// passed to both halves of the composition -- the driver's own work-item
// factory and the cell's journey engine -- because a disagreement between them
// is refused by internal/workflow/steps/approval.Complete rather than silently
// accepted.
const journeyApprover = "principal:promotion-approver"

// journeyHarness is one composed journey cell plus the verifier that issues
// the credentials its callers act under.
type journeyHarness struct {
	cell     *cell
	verifier *trust.HMACVerifier
	engine   workspace.JourneyEngine
}

// newJourneyHarness composes the P1B execution cell [newExecutionCell] builds
// and additionally wires CellConfig.ExecutionDB and CellConfig.ExecutionApprover,
// which is what makes app.Cell.Journey non-nil.
//
// The variadic mutators run against the assembled CellConfig immediately
// before composition. They exist for the one case a test cannot reach any
// other way -- pinning CellConfig.IDs so two calls mint the same identity, and
// therefore the same derived worker key -- and are deliberately not a general
// escape hatch: every other harness in this suite composes the cell the
// process composes it.
func newJourneyHarness(t *testing.T, mutate ...func(*app.CellConfig)) *journeyHarness {
	t.Helper()
	base := newCell(t)
	seedWorkforce(t, base)

	registry, err := ledgerport.NewLedgerEventDigestRegistry()
	if err != nil {
		t.Fatalf("ledger.NewLedgerEventDigestRegistry: %v", err)
	}
	terminal := &effects.LedgerTerminalWriter{
		Appender:       ledgerport.NewAppender(registry),
		ProjectionName: "workflow_promotion_outcome_journey",
		SourceRef:      "hcmnext:test:journey",
	}

	// One sink for the driver and the cell, exactly as cmd/hcmnext composes
	// them, so the driver's own execution evidence is on the list the
	// journey's Inspect reads.
	evidence := app.NewMemoryEvidenceSink()
	execution, err := platformexecution.NewPromotionExecution(platformexecution.PromotionExecutionConfig{
		DB:                  base.pool,
		Terminal:            terminal,
		Clock:               func() time.Time { return baseTime },
		ApproverPrincipalID: journeyApprover,
		AuthorityDigest:     "sha256:test-p1b-authority-amendment",
		RequiredRole:        executionAuthorityTestRole,
		Evidence:            evidence,
	})
	if err != nil {
		t.Fatalf("NewPromotionExecution: %v", err)
	}
	if execution.Evidence != evidence {
		t.Fatal("NewPromotionExecution must record on the sink it was handed")
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

	jc := &cell{t: t, db: base.db, pool: base.pool, store: base.store}
	cfg := app.CellConfig{
		Store:       base.store,
		Verifier:    verifier,
		Audience:    testAudience,
		MaxDeadline: 30 * time.Second,
		Logger:      transport.LoggerFunc(jc.appendRecord),
		Now:         func() time.Time { return baseTime },
		Evidence:    evidence,

		Executor:           execution.Executor,
		ExecutionAuthority: execution.Authority,
		ExecutionResolver:  execution.Resolver,
		ExecutionVersions:  execution.Versions,
		ExecutionCellID:    testCellID,
		TenantUUID:         func(tenant kernelvalues.TenantId) uuid.UUID { return pgstore.TenantID(string(tenant)) },

		ExecutionDB:       base.pool,
		ExecutionApprover: journeyApprover,
	}
	for _, apply := range mutate {
		apply(&cfg)
	}
	composed, err := app.NewCell(cfg)
	if err != nil {
		t.Fatalf("app.NewCell (journey): %v", err)
	}
	jc.app = composed
	if composed.Journey == nil {
		t.Fatal("a cell composed with an Executor and an ExecutionDB must carry a Journey engine")
	}

	// The cell is published on both transports exactly as every other composed
	// cell in this suite is, so the journey engine is proven to be reachable on
	// a cell that is actually served rather than on a bare struct.
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
	jc.grpcIntent = intentsv1.NewIntentServiceClient(conn)
	jc.grpcRegistry = registryv1.NewRegistryServiceClient(conn)

	edgeHandler, err := transportcell.NewEdgeHandler(composed)
	if err != nil {
		t.Fatalf("EdgeHandler: %v", err)
	}
	httpServer := httptest.NewServer(edgeHandler)
	t.Cleanup(httpServer.Close)
	jc.edgeIntent = edge.NewIntentClient(httpServer.Client(), httpServer.URL)
	jc.edgeRegistry = edge.NewRegistryClient(httpServer.Client(), httpServer.URL)
	jc.edgeURL = httpServer.URL
	jc.edgeClient = httpServer.Client()

	h := &journeyHarness{cell: jc, verifier: verifier, engine: composed.Journey}
	jc.token = "Bearer " + h.issue(t, journeyOperatorRoles()...)
	return h
}

// journeyOperatorRoles is the credential an operator holds: authorship, the
// compensation-read role the corpus needs, and the execution-authority role
// the P1B gate requires.
func journeyOperatorRoles() []string {
	return []string{"intent_author", testRole, executionAuthorityTestRole}
}

// issue mints a credential carrying exactly the named roles.
func (h *journeyHarness) issue(t *testing.T, roles ...string) string {
	t.Helper()
	return h.issueAs(t, testSubject, roles...)
}

// issueAs mints a credential for subject carrying exactly the named roles.
func (h *journeyHarness) issueAs(t *testing.T, subject string, roles ...string) string {
	t.Helper()
	token, err := h.verifier.Issue(trust.Claims{
		Issuer:               testIssuer,
		Audience:             testAudience,
		Subject:              subject,
		SubjectKind:          "human",
		Tenant:               testTenant,
		OrganizationScopeID:  testOrgScope,
		Roles:                roles,
		AuthorityRefs:        []string{"authority:position:vp-people"},
		Purposes:             []string{testPurpose, deniedPurpose, "workforce_analytics"},
		AuthenticationMethod: "bearer_token",
		Assurance:            "substantial",
		SessionRef:           "session-journey",
		IssuedAtUnix:         baseTime.Add(-time.Minute).Unix(),
		ExpiresAtUnix:        baseTime.Add(time.Hour).Unix(),
	})
	if err != nil {
		t.Fatalf("issue journey credential: %v", err)
	}
	return token
}

// ctx returns an in-process context carrying a verified principal with the
// named roles. The journey engine is an in-process port, so admission is the
// principal the transport would have put in the context, verified through the
// same verifier the served cell was composed with.
func (h *journeyHarness) ctx(t *testing.T, roles ...string) context.Context {
	t.Helper()
	principal, err := h.verifier.Verify(context.Background(), trust.Credential{
		Scheme: "Bearer", Token: h.issue(t, roles...), Audience: testAudience,
	})
	if err != nil {
		t.Fatalf("verify journey credential: %v", err)
	}
	return trust.WithPrincipal(context.Background(), principal)
}

// approverCtx is the principal the approval WorkItem is routed to. PROMOUX-015
// refuses a decision by the journey's initiator, so every test that decides
// does so as the routed approver rather than as the operator who proposed and
// executed. It holds no execution role -- membership of the routed item, not
// that role, is a decision's authority -- only the read role that lets it
// re-simulate the proposal it decides.
func (h *journeyHarness) approverCtx(t *testing.T) context.Context {
	t.Helper()
	principal, err := h.verifier.Verify(context.Background(), trust.Credential{
		Scheme: "Bearer", Token: h.issueAs(t, journeyApprover, testRole), Audience: testAudience,
	})
	if err != nil {
		t.Fatalf("verify routed approver credential: %v", err)
	}
	return trust.WithPrincipal(context.Background(), principal)
}

// operatorCtx is the ordinary caller: an operator who may execute.
func (h *journeyHarness) operatorCtx(t *testing.T) context.Context {
	t.Helper()
	return h.ctx(t, journeyOperatorRoles()...)
}

// journeyProposal is the manager's form: the target placement, the proposed
// base, the effective date and the reason. Everything else the engine reads.
func journeyProposal() workspace.ProposalInput {
	return workspace.ProposalInput{
		WorkerRef:     "omar-reyes",
		TargetJobCode: "OPS-HRBP3",
		TargetGrade:   "P3",
		// No TargetPositionID: PROMOUX-004 refuses any position reference no
		// picker issued, and POS-HRBP-301 is not a corpus position at all
		// (see internal/platform/sandbox's promotionInput).
		ProposedBase:   "98000.00",
		EffectiveDate:  "2026-06-01",
		BusinessReason: "promotion_into_senior_hrbp",
	}
}

// ledgerEventsOn counts the governed business facts recorded on one workflow
// instance's own ledger stream.
func ledgerEventsOn(t *testing.T, c *cell, instanceID string) int {
	t.Helper()
	return queryOne[int](t, c,
		`SELECT count(*) FROM ledger_event WHERE tenant_id = $1 AND stream_key = $2`,
		pgstore.TenantID(testTenant), effects.StreamKeyFor(prototype.ApprovalWorkflowID, instanceID))
}

// ---------------------------------------------------------------------------
// Propose
// ---------------------------------------------------------------------------

// TestJourneyProposeListsThePromotionAtProposed proves the first step of the
// vertical slice: a manager's form becomes a real, simulated promote_worker
// intent, and the list surface shows it with both sides of the placement
// change and both pay figures derived from durable state.
func TestJourneyProposeListsThePromotionAtProposed(t *testing.T) {
	h := newJourneyHarness(t)
	ctx := h.operatorCtx(t)

	proposed, err := h.engine.Propose(ctx, journeyProposal())
	if err != nil {
		t.Fatalf("Propose: %v", err)
	}
	if proposed.IntentID == "" {
		t.Fatal("Propose returned no intent id")
	}
	if proposed.Stage != workspace.JourneyStageProposed {
		t.Fatalf("stage = %s, want PROPOSED (the simulation must mint an executable proposal)", proposed.Stage)
	}
	if proposed.ProposalRevisionID == "" || proposed.MaterialDigest == "" {
		t.Fatalf("Propose returned no proposal identity: %+v", proposed)
	}
	if proposed.InstanceID != "" {
		t.Fatalf("a proposed journey names a workflow instance %q; nothing has been executed", proposed.InstanceID)
	}

	listed, err := h.engine.ListJourneys(ctx)
	if err != nil {
		t.Fatalf("ListJourneys: %v", err)
	}
	var found *workspace.JourneySummary
	for i := range listed {
		if listed[i].IntentID == proposed.IntentID {
			found = &listed[i]
		}
	}
	if found == nil {
		t.Fatalf("ListJourneys did not list the promotion just proposed: %+v", listed)
	}
	if found.Stage != workspace.JourneyStageProposed {
		t.Errorf("listed stage = %s, want PROPOSED", found.Stage)
	}
	if found.Current.JobCode != "OPS-HRBP2" || found.Current.Grade != "P2" {
		t.Errorf("current placement = %+v, want the governed read's own OPS-HRBP2/P2", found.Current)
	}
	if found.Target.JobCode != "OPS-HRBP3" || found.Target.Grade != "P3" || found.Target.PositionID != "" {
		t.Errorf("target placement = %+v, want the form's own target", found.Target)
	}
	if found.Current.OrgUnit != "people-ops" || found.Current.PayZone != "US-EAST" {
		t.Errorf("organizational placement = %+v, want the governed read's own", found.Current)
	}
	if found.CurrentBase != "93000.00" || found.ProposedBase != "98000.00" || found.Currency != "USD" {
		t.Errorf("pay = %s -> %s %s, want 93000.00 -> 98000.00 USD",
			found.CurrentBase, found.ProposedBase, found.Currency)
	}
	if found.EffectiveDate != "2026-06-01" || found.BusinessReason != "promotion_into_senior_hrbp" {
		t.Errorf("effective/reason = %q/%q", found.EffectiveDate, found.BusinessReason)
	}
	if found.WorkerName == "" {
		t.Error("the listed journey names no worker")
	}
}

func TestJourneyProposeRefusesAnIncompleteForm(t *testing.T) {
	h := newJourneyHarness(t)
	in := journeyProposal()
	in.BusinessReason = ""
	if _, err := h.engine.Propose(h.operatorCtx(t), in); !errors.Is(err, workspace.ErrJourneyInput) {
		t.Fatalf("Propose(no reason) = %v, want ErrJourneyInput", err)
	}
}

// ---------------------------------------------------------------------------
// Execute
// ---------------------------------------------------------------------------

// TestJourneyExecuteParksAtTheRoutedApproval proves the P1B gate admitted the
// call, the real driver started a durable instance, and it parked on one
// APPROVAL WorkItem routed to the configured approver.
func TestJourneyExecuteParksAtTheRoutedApproval(t *testing.T) {
	h := newJourneyHarness(t)
	ctx := h.operatorCtx(t)

	proposed, err := h.engine.Propose(ctx, journeyProposal())
	if err != nil {
		t.Fatalf("Propose: %v", err)
	}
	detail, err := h.engine.Execute(ctx, proposed.IntentID)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if detail.Summary.Stage != workspace.JourneyStageAwaitingApproval {
		t.Fatalf("stage = %s, want AWAITING_APPROVAL", detail.Summary.Stage)
	}
	if detail.Instance == nil {
		t.Fatal("an executed journey names no workflow instance")
	}
	if detail.Instance.WorkflowID != prototype.ApprovalWorkflowID {
		t.Errorf("workflow = %q, want the prototype promotion approval graph", detail.Instance.WorkflowID)
	}
	if detail.Instance.PlanDigest == "" || detail.Instance.InstanceVersion == 0 {
		t.Errorf("instance = %+v, want a pinned plan digest and a version", detail.Instance)
	}
	if detail.Approver != journeyApprover {
		t.Errorf("approver = %q, want %q", detail.Approver, journeyApprover)
	}
	if len(detail.WorkItems) != 1 {
		t.Fatalf("work items = %d, want exactly one", len(detail.WorkItems))
	}
	item := detail.WorkItems[0]
	if item.Kind != workitem.KindApproval || item.NodeID != prototype.NodeApproval {
		t.Fatalf("parked item = %+v, want the APPROVAL on %s", item, prototype.NodeApproval)
	}
	if item.Status != workitem.StatusAssigned {
		t.Errorf("parked item status = %s, want ASSIGNED", item.Status)
	}
	if item.OwnerRef != journeyApprover {
		t.Errorf("parked item owner = %q, want the routed approver %q", item.OwnerRef, journeyApprover)
	}
	if _, ok := item.Assignment.Resolution.Authorizes(journeyApprover); !ok {
		t.Errorf("the routed assignment does not authorize %q: %+v", journeyApprover, item.Assignment)
	}
	// The assignment carries the real compiled requirement's digests - the
	// ones a resume rebuilds from the item's own deadline and approver - and
	// the deadline is the composition's 48-hour window from the pinned clock.
	if want := baseTime.Add(48 * time.Hour); !item.DeadlineAt.Equal(want) {
		t.Errorf("deadline = %s, want %s", item.DeadlineAt, want)
	}
	requirement, err := prototype.CompileApprovalRequirement(journeyApprover, item.DeadlineAt)
	if err != nil {
		t.Fatalf("CompileApprovalRequirement: %v", err)
	}
	if res := item.Assignment.Resolution; res.RequirementDigest != requirement.Digest() ||
		res.ExpressionDigest != requirement.ExpressionDigest || res.QuorumRequired != requirement.Quorum.MinApprovals {
		t.Errorf("routed assignment = %+v, want the compiled requirement's digests %s/%s",
			res, requirement.Digest(), requirement.ExpressionDigest)
	}
	if detail.Ledger != nil {
		t.Errorf("a journey parked at approval already recorded a ledger fact: %+v", detail.Ledger)
	}
	if got := ledgerEventsOn(t, h.cell, detail.Instance.InstanceID); got != 0 {
		t.Errorf("ledger events while parked = %d, want 0", got)
	}
	if len(detail.Transitions) == 0 {
		t.Error("the parked work item carries no recorded transitions")
	}
	if len(detail.EvidenceIDs) == 0 {
		t.Error("an executed journey records no evidence at all")
	}

	// Executing an already-running journey is a stage refusal, not a silent
	// replay of the driver's idempotent start.
	if _, err := h.engine.Execute(ctx, proposed.IntentID); !errors.Is(err, workspace.ErrJourneyStage) {
		t.Fatalf("second Execute = %v, want ErrJourneyStage", err)
	}
}

// TestJourneyExecuteIsRefusedWithoutTheExecutionRole proves the journey obeys
// the same P1B authority gate the RPC surface does: a caller who may read and
// propose may not execute.
func TestJourneyExecuteIsRefusedWithoutTheExecutionRole(t *testing.T) {
	h := newJourneyHarness(t)
	proposed, err := h.engine.Propose(h.operatorCtx(t), journeyProposal())
	if err != nil {
		t.Fatalf("Propose: %v", err)
	}
	withoutRole := h.ctx(t, "intent_author", testRole)
	if _, err := h.engine.Execute(withoutRole, proposed.IntentID); !errors.Is(err, workspace.ErrDenied) {
		t.Fatalf("Execute without %s = %v, want ErrDenied", executionAuthorityTestRole, err)
	}
	// The refusal changed nothing: the journey is still at PROPOSED.
	detail, err := h.engine.Inspect(h.operatorCtx(t), proposed.IntentID)
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if detail.Summary.Stage != workspace.JourneyStageProposed || detail.Instance != nil {
		t.Fatalf("a refused execution left state behind: %+v", detail.Summary)
	}
}

// ---------------------------------------------------------------------------
// Decide
// ---------------------------------------------------------------------------

// TestJourneyDecideApproveCompletesWithOneGovernedWrite is the whole slice:
// propose, execute, approve, and exactly one ledger fact.
func TestJourneyDecideApproveCompletesWithOneGovernedWrite(t *testing.T) {
	h := newJourneyHarness(t)
	ctx := h.operatorCtx(t)

	proposed, err := h.engine.Propose(ctx, journeyProposal())
	if err != nil {
		t.Fatalf("Propose: %v", err)
	}
	if _, err := h.engine.Execute(ctx, proposed.IntentID); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	detail, err := h.engine.Decide(h.approverCtx(t), proposed.IntentID, workspace.Decision{
		Approve: true, Reason: "reason.promotion_supported/v1",
	})
	if err != nil {
		t.Fatalf("Decide(approve): %v", err)
	}
	if detail.Summary.Stage != workspace.JourneyStageCompleted {
		t.Fatalf("stage = %s, want COMPLETED", detail.Summary.Stage)
	}
	if detail.Instance == nil || detail.Instance.Status != "COMPLETED" {
		t.Fatalf("instance = %+v, want a COMPLETED runtime status", detail.Instance)
	}
	if detail.Ledger == nil {
		t.Fatal("a completed journey reports no ledger fact")
	}
	if detail.Ledger.Sequence != 1 || detail.Ledger.SchemaRef == "" || detail.Ledger.Digest == "" {
		t.Fatalf("ledger fact = %+v, want the first sequence with a schema and a digest", detail.Ledger)
	}
	if got := ledgerEventsOn(t, h.cell, detail.Instance.InstanceID); got != 1 {
		t.Fatalf("ledger events on the instance stream = %d, want exactly 1", got)
	}

	// The completed-work list must now resolve this row from the immutable
	// proposal/runtime/ledger chain. Its identity and closed time are the ones
	// just read from PostgreSQL, not a fresh simulation under current rules.
	listed, err := h.engine.ListJourneys(ctx)
	if err != nil {
		t.Fatalf("ListJourneys(after completion): %v", err)
	}
	var historical *workspace.JourneySummary
	for index := range listed {
		if listed[index].IntentID == proposed.IntentID {
			historical = &listed[index]
			break
		}
	}
	if historical == nil {
		t.Fatalf("completed journey is absent from durable history: %+v", listed)
	}
	if historical.InstanceID != detail.Instance.InstanceID || historical.MaterialDigest == "" {
		t.Fatalf("history lost durable proposal/runtime identity: %+v", historical)
	}
	if !historical.UpdatedAt.Equal(detail.Ledger.RecordedAt) {
		t.Fatalf("history closed at %s, want ledger recorded_at %s", historical.UpdatedAt, detail.Ledger.RecordedAt)
	}

	// The approval work item is COMPLETED, claimed and decided by the routed
	// approver who signed in to decide it - never by the operator who proposed
	// and executed the journey.
	if len(detail.WorkItems) != 1 {
		t.Fatalf("work items = %d, want exactly one", len(detail.WorkItems))
	}
	item := detail.WorkItems[0]
	if item.Status != workitem.StatusCompleted {
		t.Fatalf("approval item status = %s, want COMPLETED", item.Status)
	}
	if journeyApprover == testSubject {
		t.Fatal("the fixture approver and the initiating subject must differ for this assertion to mean anything")
	}
	// workitem.Store.Complete releases the claim as it completes the item, so
	// the completed row carries no claimant; what must never appear there is
	// the initiating operator.
	if item.ClaimedBy != "" && item.ClaimedBy != journeyApprover {
		t.Errorf("claimed by %q, want the routed approver %q or a released claim", item.ClaimedBy, journeyApprover)
	}
	if item.CompletedBy != journeyApprover {
		t.Errorf("completed by %q, want the routed approver %q", item.CompletedBy, journeyApprover)
	}
	if item.CompletedOutputDigest == "" {
		t.Error("the completed approval records no immutable decision digest")
	}
	// The recorded decision (WORK-010) names the routed approver too, and its
	// digest is the item's completed output.
	decisionApprover := queryOne[string](t, h.cell,
		`SELECT decision_body->'approver'->>'principal_id' FROM work_item_decision
		 WHERE tenant_id = $1 AND work_item_id = $2 AND decision_body_digest = $3`,
		pgstore.TenantID(testTenant), item.WorkItemID, item.CompletedOutputDigest)
	if decisionApprover != journeyApprover {
		t.Errorf("the recorded decision names approver %q, want %q", decisionApprover, journeyApprover)
	}
	// The routing transitions (CREATED, ROUTED, ASSIGNED) are the work-item
	// factory's, made when Execute parked the instance; only the journey's
	// own claim, start and decision name the person who pressed the button --
	// under PROMOUX-015 the routed approver who decided, never the operator
	// (testSubject) who proposed and executed.
	decided := false
	for _, tr := range detail.Transitions {
		if strings.HasPrefix(tr.Reason, "journey.") && tr.Actor != journeyApprover {
			t.Errorf("transition %s -> %s (%s) names actor %q, want the deciding approver %q",
				tr.From, tr.To, tr.Reason, tr.Actor, journeyApprover)
		}
		if tr.Actor == testSubject {
			t.Errorf("transition %s -> %s (%s) names the initiator %q; neither routing nor the decision is theirs",
				tr.From, tr.To, tr.Reason, testSubject)
		}
		if tr.To == string(workitem.StatusCompleted) {
			decided = true
		}
	}
	if !decided {
		t.Error("no COMPLETED transition was recorded for the approval")
	}
	// The resolved approval is what advanced the node: its output artifact
	// is the resolution's digest, not the one decision's.
	approvalOutput := queryOne[string](t, h.cell,
		`SELECT coalesce(output_artifact_ref, '') FROM workflow_node_execution
		 WHERE tenant_id = $1 AND instance_id = $2::uuid AND node_id = $3`,
		pgstore.TenantID(testTenant), detail.Instance.InstanceID, prototype.NodeApproval)
	if approvalOutput == "" || approvalOutput == item.CompletedOutputDigest {
		t.Errorf("approval node output = %q, want the steps/approval resolution digest (item output %q)",
			approvalOutput, item.CompletedOutputDigest)
	}

	// The driver's own evidence is on the journey: the cell and the driver
	// share one sink, so Inspect lists APPROVAL_COMPLETED and
	// TERMINAL_WRITTEN beside the gate's GATE_ADMITTED.
	assertJourneyEvidence(t, h, detail, "GATE_ADMITTED", "APPROVAL_COMPLETED", "TERMINAL_WRITTEN")

	// The approved terminal actually ran: SUCCEEDED, not one of the SKIPPED
	// sibling terminals a completed instance also records.
	reached := false
	for _, node := range detail.Nodes {
		if node.NodeID == prototype.NodeApproved && node.Status == "SUCCEEDED" {
			reached = true
		}
	}
	if !reached {
		t.Fatalf("the instance did not run %s: %+v", prototype.NodeApproved, detail.Nodes)
	}

	assertJourneyTimeline(t, detail)
}

// assertJourneyEvidence proves detail.EvidenceIDs carries, in recording order,
// an entry of every named kind, resolved against the cell's own sink - the
// only sink there is once the composition root hands the driver the cell's.
func assertJourneyEvidence(t *testing.T, h *journeyHarness, detail workspace.JourneyDetail, kinds ...string) {
	t.Helper()
	byID := map[string]app.EvidenceRecord{}
	for _, rec := range h.cell.app.Evidence.Records() {
		byID[rec.EvidenceID] = rec
	}
	var seen []string
	for _, id := range detail.EvidenceIDs {
		rec, ok := byID[id]
		if !ok {
			t.Fatalf("journey evidence id %q is not on the cell's sink", id)
		}
		seen = append(seen, rec.Decision)
	}
	next := 0
	for _, kind := range seen {
		if next < len(kinds) && kind == kinds[next] {
			next++
		}
	}
	if next != len(kinds) {
		t.Fatalf("journey evidence kinds = %v, want %v in that order", seen, kinds)
	}
}

// assertJourneyTimeline checks the engine-composed chronology of a completed
// journey: every kind is present, the account opens with the proposal, the
// execution follows the simulation, and no entry precedes the one before it.
//
// It asserts order rather than exact instants because the pinned composition
// clock and the database's own recorded_at defaults are two different clocks;
// what the page needs is a causal account, not two clocks agreeing.
func assertJourneyTimeline(t *testing.T, detail workspace.JourneyDetail) {
	t.Helper()
	if len(detail.Timeline) == 0 {
		t.Fatal("a completed journey has an empty timeline")
	}
	index := map[string]int{}
	for i, event := range detail.Timeline {
		if _, seen := index[event.Kind]; !seen {
			index[event.Kind] = i
		}
	}
	for _, kind := range []string{
		app.JourneyEventIntentCreated, app.JourneyEventSimulated, app.JourneyEventInstanceStarted,
		app.JourneyEventWorkItem, app.JourneyEventNode, app.JourneyEventLedgerRecorded,
	} {
		if _, ok := index[kind]; !ok {
			t.Errorf("the timeline is missing a %s entry: %+v", kind, detail.Timeline)
		}
	}
	if detail.Timeline[0].Kind != app.JourneyEventIntentCreated {
		t.Errorf("the timeline opens with %s, want %s", detail.Timeline[0].Kind, app.JourneyEventIntentCreated)
	}
	if index[app.JourneyEventSimulated] < index[app.JourneyEventIntentCreated] ||
		index[app.JourneyEventInstanceStarted] < index[app.JourneyEventSimulated] {
		t.Errorf("the timeline is not causal: created=%d simulated=%d started=%d",
			index[app.JourneyEventIntentCreated], index[app.JourneyEventSimulated],
			index[app.JourneyEventInstanceStarted])
	}
	for i := 1; i < len(detail.Timeline); i++ {
		if detail.Timeline[i].At.Before(detail.Timeline[i-1].At) {
			t.Fatalf("the timeline is not chronological at %d: %v before %v",
				i, detail.Timeline[i].At, detail.Timeline[i-1].At)
		}
	}
}

// TestJourneyDecideRejectReachesTheRejectedTerminal proves the other edge of
// the compiled graph is real too, on its own journey.
//
// The rejected END is still an END: internal/workflow/execute's continuation
// sink runs the composed TerminalWriter on every COMPLETE intent, whatever
// terminal the instance reached, so the rejection is recorded as its own
// governed fact on its own per-instance stream rather than as no fact at all.
// The two journeys never share a stream ([effects.StreamKeyFor] is per
// instance), so the approved journey's single fact is unaffected.
func TestJourneyDecideRejectReachesTheRejectedTerminal(t *testing.T) {
	h := newJourneyHarness(t)
	ctx := h.operatorCtx(t)

	approvedJourney, err := h.engine.Propose(ctx, journeyProposal())
	if err != nil {
		t.Fatalf("Propose(first): %v", err)
	}
	if _, err := h.engine.Execute(ctx, approvedJourney.IntentID); err != nil {
		t.Fatalf("Execute(first): %v", err)
	}
	approvedDetail, err := h.engine.Decide(h.approverCtx(t), approvedJourney.IntentID, workspace.Decision{
		Approve: true, Reason: "reason.promotion_supported/v1",
	})
	if err != nil {
		t.Fatalf("Decide(approve): %v", err)
	}

	// PROMOUX-002: the approved journey above still holds an active window on
	// journeyProposal()'s effective date (nothing in this test advances it to
	// a terminal, ledger-recorded stage), so a second proposal for the same
	// worker and date would now be refused as a conflict rather than admitted
	// as this test's own second, independently decided journey. A distinct,
	// non-overlapping effective date is exactly GREEN's carve-out and keeps
	// this test's real point -- two decisions reaching two different
	// terminals -- intact.
	secondProposal := journeyProposal()
	secondProposal.EffectiveDate = "2026-08-01"
	rejectedJourney, err := h.engine.Propose(ctx, secondProposal)
	if err != nil {
		t.Fatalf("Propose(second): %v", err)
	}
	if rejectedJourney.IntentID == approvedJourney.IntentID {
		t.Fatal("two proposals must be two intents")
	}
	if _, err := h.engine.Execute(ctx, rejectedJourney.IntentID); err != nil {
		t.Fatalf("Execute(second): %v", err)
	}
	detail, err := h.engine.Decide(h.approverCtx(t), rejectedJourney.IntentID, workspace.Decision{
		Approve: false, Reason: "reason.not_supported/v1",
	})
	if err != nil {
		t.Fatalf("Decide(reject): %v", err)
	}
	if detail.Summary.Stage != workspace.JourneyStageRejected {
		t.Fatalf("stage = %s, want REJECTED", detail.Summary.Stage)
	}
	// A completed instance carries a row for every terminal the frontier could
	// have taken; only the one that actually ran is SUCCEEDED, and the rest are
	// SKIPPED. The status is what says which terminal the promotion reached.
	reachedRejected, reachedApproved := false, false
	for _, node := range detail.Nodes {
		if node.Status != "SUCCEEDED" {
			continue
		}
		switch node.NodeID {
		case prototype.NodeRejected:
			reachedRejected = true
		case prototype.NodeApproved:
			reachedApproved = true
		}
	}
	if !reachedRejected || reachedApproved {
		t.Fatalf("a rejected journey ran %+v, want %s and never %s",
			detail.Nodes, prototype.NodeRejected, prototype.NodeApproved)
	}
	if detail.Instance == nil {
		t.Fatal("a decided journey names no instance")
	}
	if got := ledgerEventsOn(t, h.cell, detail.Instance.InstanceID); got != 1 {
		t.Fatalf("ledger events on the rejected instance's own stream = %d, want exactly 1", got)
	}
	// The approved journey's own stream is untouched by the rejection.
	if got := ledgerEventsOn(t, h.cell, approvedDetail.Instance.InstanceID); got != 1 {
		t.Fatalf("ledger events on the approved instance's stream = %d, want still exactly 1", got)
	}
}

// TestJourneyDecideBeforeExecuteIsAStageRefusal proves the approver cannot
// decide a promotion nobody has executed: there is no routed WorkItem to
// decide, and the engine says so rather than inventing one.
func TestJourneyDecideBeforeExecuteIsAStageRefusal(t *testing.T) {
	h := newJourneyHarness(t)
	ctx := h.operatorCtx(t)
	proposed, err := h.engine.Propose(ctx, journeyProposal())
	if err != nil {
		t.Fatalf("Propose: %v", err)
	}
	if _, err := h.engine.Decide(ctx, proposed.IntentID, workspace.Decision{Approve: true}); !errors.Is(err, workspace.ErrJourneyStage) {
		t.Fatalf("Decide before Execute = %v, want ErrJourneyStage", err)
	}
}

// TestJourneyDecideTwiceIsAStageRefusal proves a closed approval cannot be
// re-decided: the identical decision replays idempotently (APPROVAL-008
// binds one decision per task version and answers a repeat with the stored
// one, so a retried click never fails), while a contradicting decision is
// refused rather than rewriting the closed approval.
func TestJourneyDecideTwiceIsAStageRefusal(t *testing.T) {
	h := newJourneyHarness(t)
	ctx := h.operatorCtx(t)
	proposed, err := h.engine.Propose(ctx, journeyProposal())
	if err != nil {
		t.Fatalf("Propose: %v", err)
	}
	if _, err := h.engine.Execute(ctx, proposed.IntentID); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	approver := h.approverCtx(t)
	first, err := h.engine.Decide(approver, proposed.IntentID, workspace.Decision{Approve: true, Reason: "yes"})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	replay, err := h.engine.Decide(approver, proposed.IntentID, workspace.Decision{Approve: true, Reason: "yes"})
	if err != nil {
		t.Fatalf("identical second Decide = %v, want the idempotent replay of the stored decision", err)
	}
	if replay.Summary.Stage != first.Summary.Stage {
		t.Fatalf("replayed Decide moved the journey from %s to %s", first.Summary.Stage, replay.Summary.Stage)
	}
	if _, err := h.engine.Decide(approver, proposed.IntentID, workspace.Decision{Approve: false, Reason: "changed my mind"}); err == nil {
		t.Fatal("a contradicting decision on a closed approval must be refused")
	}
}

// ---------------------------------------------------------------------------
// Visibility
// ---------------------------------------------------------------------------

// TestJourneyInspectOfAnUnknownIntentIsUnknown proves the journey never
// distinguishes "does not exist" from "not visible to you".
func TestJourneyInspectOfAnUnknownIntentIsUnknown(t *testing.T) {
	h := newJourneyHarness(t)
	if _, err := h.engine.Inspect(h.operatorCtx(t), uuid.NewString()); !errors.Is(err, workspace.ErrJourneyUnknown) {
		t.Fatalf("Inspect(unknown) = %v, want ErrJourneyUnknown", err)
	}
}

// TestJourneyRefusesWithoutAVerifiedPrincipal proves every method admits on
// the same principal the RPC surfaces do.
func TestJourneyRefusesWithoutAVerifiedPrincipal(t *testing.T) {
	h := newJourneyHarness(t)
	ctx := context.Background()
	if _, err := h.engine.ListJourneys(ctx); !errors.Is(err, workspace.ErrDenied) {
		t.Errorf("ListJourneys(anonymous) = %v, want ErrDenied", err)
	}
	if _, err := h.engine.Propose(ctx, journeyProposal()); !errors.Is(err, workspace.ErrDenied) {
		t.Errorf("Propose(anonymous) = %v, want ErrDenied", err)
	}
	if _, err := h.engine.Inspect(ctx, "x"); !errors.Is(err, workspace.ErrDenied) {
		t.Errorf("Inspect(anonymous) = %v, want ErrDenied", err)
	}
	if _, err := h.engine.Execute(ctx, "x"); !errors.Is(err, workspace.ErrDenied) {
		t.Errorf("Execute(anonymous) = %v, want ErrDenied", err)
	}
	if _, err := h.engine.Decide(ctx, "x", workspace.Decision{}); !errors.Is(err, workspace.ErrDenied) {
		t.Errorf("Decide(anonymous) = %v, want ErrDenied", err)
	}
}

// TestJourneyIsNilWithoutAnExecutionDatabase proves the composition rule: the
// journey surface exists only on a cell that can actually run one.
func TestJourneyIsNilWithoutAnExecutionDatabase(t *testing.T) {
	c := newCell(t)
	if c.app.Journey != nil {
		t.Fatal("a cell composed with no ExecutionDB must carry no Journey engine")
	}
}
