package application

// PROMOUX-015 prerequisites, against the real composition: embedded
// PostgreSQL, ComposeServe with the local-dev routing configuration, the demo
// workforce and persona credentials composeDevPersonas issues, and the real
// gRPC JourneyService (page grants included) or the composed journey engine.
//
// The four personas are one separated promotion: hiring-manager (Darius
// Bennett, the Chief People Officer) proposes; finance-partner (Thomas Baker,
// the configured finance partner) decides the finance approval; admin (Rafael
// Torres, Director of People Operations and the execution operator) executes
// and decides the manager approval; individual-contributor (Linh Tran, one of
// Rafael's reports) is the employee persona.
//
// The promotion subject is a worker created through the real CreateWorker path
// as another of Rafael's reports, on the corpus placement OPS-HRBP2/P2. It is
// not a seeded demo worker because no seeded demo role has a pay band: the
// simulation of every demo ladder promotion is BLOCKED with
// promotion.pay_band_not_found, so none can reach an approval. The subject's
// reporting line (subject -> Rafael -> Darius) is the same relationship the
// seeded people-operations workers have.

import (
	"context"
	"errors"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/data/demoworkforce"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/fixtures"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
)

const (
	promoux015SigningKey = "promoux015-separation-of-duties-signing-key"
	promoux015Employee   = "hc-051-linh-tran"
	promoux015Proposer   = "hc-004-darius-bennett"
	promoux015Finance    = "hc-054-thomas-baker"
	promoux015Manager    = "hc-050-rafael-torres"
)

type promoux015Harness struct {
	t          *testing.T
	cfg        ServeConfig
	pool       *pgxadapter.Pool
	composed   *App
	client     journeyv1.JourneyServiceClient
	tokens     map[string]string
	principals map[string]*trust.Principal
	verifier   *trust.HMACVerifier
	effective  string
	// subject is the created worker every promotion here is about.
	subject string
}

// promoux015Compose composes and starts the serve role the local-dev profile
// runs: the demo tenant, dev personas, the executable plan and the finance
// partner default.
func promoux015Compose(t *testing.T) *promoux015Harness {
	t.Helper()
	return promoux015ComposeWith(t, Options{})
}

// promoux015ComposeWith is [promoux015Compose] with the composition's options
// (WF-STEP-003's served tests supply a movable cell clock).
func promoux015ComposeWith(t *testing.T, options Options) *promoux015Harness {
	t.Helper()
	db := pgtest.New(t)
	pool, err := pgxadapter.NewPool(context.Background(), db.URL, map[string]string{"search_path": db.Schema})
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(pool.Close)
	cfg := ServeConfig{
		GRPCListen: "127.0.0.1:0", HTTPListen: "127.0.0.1:0", DatabaseURL: db.URL,
		DevHMACKey: promoux015SigningKey, Issuer: DefaultIssuer, Audience: DefaultAudience,
		Tenant: demoworkforce.CompanyKey, CellID: "cell-promoux015-separation", MaxDeadline: 60 * time.Second,
		Workspace: true, DevBrowserLogin: true, OTelExporter: OTelExporterNone,
		ExecutionAuthority: true, ExecutionAuthorityDigest: "sha256:promoux015-separation",
		ExecutionAuthorityRole: "promotion_operator", ExecutionApprover: "principal:promotion-approver",
		ExecutionFinancePartner: LocalDevFinancePartner,
		WorkflowPlan:            WorkflowPlanExecute, TimerTzdbVersion: DefaultTimerTzdbVersion,
		TimerCalendarVersion: DefaultTimerCalendarVersion,
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("configuration: %v", err)
	}
	composed, err := ComposeServe(context.Background(), ServeInput{Config: cfg, Pool: pool, Identity: "promoux015-separation", Options: options})
	if err != nil {
		t.Fatalf("ComposeServe: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		stopCtx, stop := context.WithTimeout(context.Background(), 20*time.Second)
		defer stop()
		_ = composed.Stop(stopCtx)
	})
	if err := composed.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	verifier, err := trust.NewHMACVerifier(trust.HMACVerifierConfig{Key: []byte(cfg.DevHMACKey), Issuer: cfg.Issuer, Audience: cfg.Audience})
	if err != nil {
		t.Fatalf("verifier: %v", err)
	}
	h := &promoux015Harness{
		t: t, cfg: cfg, pool: pool, composed: composed, verifier: verifier,
		tokens: map[string]string{}, principals: map[string]*trust.Principal{},
		effective: time.Now().UTC().AddDate(0, 1, 0).Format(time.DateOnly),
	}
	for _, persona := range composeDevPersonas(verifier, cfg, time.Now) {
		principal, verifyErr := verifier.Verify(context.Background(), trust.Credential{Scheme: "Bearer", Token: persona.Token, Audience: cfg.Audience})
		if verifyErr != nil {
			t.Fatalf("verify %s: %v", persona.ID, verifyErr)
		}
		h.tokens[persona.ID] = persona.Token
		h.principals[persona.ID] = principal
	}
	conn, err := grpc.NewClient(composed.GRPCAddr(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc client: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	h.client = journeyv1.NewJourneyServiceClient(conn)
	created, err := h.client.CreateWorker(h.rpc("admin"), &journeyv1.CreateWorkerRequest{
		LegalName: "Noa Quinn", PreferredName: "Noa", JobCode: "OPS-HRBP2", Grade: "P2", OrgUnit: "people-ops",
		PositionId: "POS-HRBP-204", Location: "Boston, MA", PayZone: "US-EAST", BasePay: "90000.00", Currency: "USD",
		BonusTarget: "0.0500", HireDate: "2021-04-05", ManagerRef: promoux015Manager,
	})
	if err != nil {
		t.Fatalf("CreateWorker reporting to %s: %v", promoux015Manager, err)
	}
	h.subject = created.GetWorker().GetWorkerRef()
	return h
}

// rpc authenticates an outgoing gRPC call as a persona.
func (h *promoux015Harness) rpc(persona string) context.Context {
	token, ok := h.tokens[persona]
	if !ok {
		h.t.Fatalf("no persona %q", persona)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	h.t.Cleanup(cancel)
	return metadata.AppendToOutgoingContext(ctx, transport.AuthorizationMetadataKey, "Bearer "+token)
}

// engine is the composed journey engine and a context carrying a persona's
// verified principal.
func (h *promoux015Harness) engine(persona string) (workspace.JourneyEngine, context.Context) {
	principal, ok := h.principals[persona]
	if !ok {
		h.t.Fatalf("no persona %q", persona)
	}
	return h.composed.Cell().Journey, trust.WithPrincipal(context.Background(), principal)
}

// proposeAndExecute has the proposer propose the subject's promotion and the
// operator execute it, returning the intent id at FINANCE_APPROVAL.
func (h *promoux015Harness) proposeAndExecute() string {
	h.t.Helper()
	proposed, err := h.client.ProposeJourney(h.rpc("hiring-manager"), &journeyv1.ProposeJourneyRequest{
		WorkerRef: h.subject, Target: &journeyv1.Placement{JobCode: "OPS-HRBP3", Grade: "P3"},
		ProposedBase: "98000.00", EffectiveDate: h.effective, BusinessReason: "promoux015 separation of duties",
	})
	if err != nil {
		h.t.Fatalf("ProposeJourney as the proposer: %v", err)
	}
	id := proposed.GetJourney().GetIntentId()
	if proposed.GetJourney().GetStage() != journeyv1.JourneyStage_JOURNEY_STAGE_PROPOSED {
		h.t.Fatalf("proposed stage = %s, want PROPOSED", proposed.GetJourney().GetStage())
	}
	executed, err := h.client.ExecuteJourney(h.rpc("admin"), &journeyv1.ExecuteJourneyRequest{IntentId: id})
	if err != nil {
		h.t.Fatalf("ExecuteJourney as the operator: %v", err)
	}
	if got := executed.GetDetail().GetJourney().GetStage(); got != journeyv1.JourneyStage_JOURNEY_STAGE_FINANCE_APPROVAL {
		h.t.Fatalf("executed stage = %s, want FINANCE_APPROVAL", got)
	}
	return id
}

type promoux015Item struct {
	id                          string
	node, owner, status         string
	claimedBy, completedBy      string
	version                     int64
	decisionApprover, decidedBy string
	claimActor, decisionActor   string
	transitions                 int
}

// items reads every work item of the promotion's workflow instance, with the
// decider named on the claim, the completion, the work_item_decision body, the
// intent_decision row and the WorkItem transitions.
func (h *promoux015Harness) items(intentID string) map[string]promoux015Item {
	h.t.Helper()
	ctx := context.Background()
	rows, err := h.pool.Query(ctx, `
		SELECT w.work_item_id::text, w.node_id, w.owner_ref, w.status, coalesce(w.claimed_by,''), coalesce(w.completed_by,''), w.item_version,
		       coalesce((SELECT d.decision_body->'approver'->>'principal_id' FROM work_item_decision d WHERE d.tenant_id = w.tenant_id AND d.work_item_id = w.work_item_id), ''),
		       coalesce((SELECT t.actor_principal_id FROM work_item_transition t WHERE t.tenant_id = w.tenant_id AND t.work_item_id = w.work_item_id AND t.reason = 'journey.approval.claimed'), ''),
		       coalesce((SELECT t.actor_principal_id FROM work_item_transition t WHERE t.tenant_id = w.tenant_id AND t.work_item_id = w.work_item_id AND t.reason = 'journey.approval.decided'), ''),
		       (SELECT count(*) FROM work_item_transition t WHERE t.tenant_id = w.tenant_id AND t.work_item_id = w.work_item_id)
		FROM work_item w ORDER BY w.created_at`)
	if err != nil {
		h.t.Fatalf("read work items: %v", err)
	}
	defer rows.Close()
	out := map[string]promoux015Item{}
	for rows.Next() {
		var item promoux015Item
		var transitions int64
		if err := rows.Scan(&item.id, &item.node, &item.owner, &item.status, &item.claimedBy, &item.completedBy, &item.version,
			&item.decisionApprover, &item.claimActor, &item.decisionActor, &transitions); err != nil {
			h.t.Fatalf("scan work item: %v", err)
		}
		item.transitions = int(transitions)
		out[item.node] = item
	}
	if err := rows.Err(); err != nil {
		h.t.Fatalf("work items: %v", err)
	}
	decisions, err := h.pool.Query(ctx, `SELECT requirement_id, decided_by FROM intent_decision WHERE decision_kind = 'HUMAN_APPROVAL' AND intent_id::text = $1`, intentID)
	if err != nil {
		h.t.Fatalf("read intent decisions: %v", err)
	}
	defer decisions.Close()
	for decisions.Next() {
		var requirement, decidedBy string
		if err := decisions.Scan(&requirement, &decidedBy); err != nil {
			h.t.Fatalf("scan intent decision: %v", err)
		}
		node := promotionexec.NodeApproveManager
		if requirement == promotionexec.ApprovalFinance {
			node = promotionexec.NodeApproveFinance
		}
		item := out[node]
		item.decidedBy = decidedBy
		out[node] = item
	}
	return out
}

func promoux015Code(t *testing.T, err error, want codes.Code, what string) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s succeeded, want %s", what, want)
	}
	if got := status.Code(err); got != want {
		t.Fatalf("%s = %s (%v), want %s", what, got, err, want)
	}
}

// TestPromotionDecisionSeparationOfDutiesFourPersonasEachDoExactlyTheirStep
// drives one promotion through the real gRPC JourneyService as four separated
// personas and proves each can do exactly its own step: the proposer
// proposes and can neither execute nor decide; the operator executes and
// cannot decide the finance approval although it holds the execution role;
// the finance partner decides only the finance approval; the current manager
// decides only the manager approval; the employee can neither propose nor
// decide. Every decision row names the caller.
func TestPromotionDecisionSeparationOfDutiesFourPersonasEachDoExactlyTheirStep(t *testing.T) {
	h := promoux015Compose(t)
	subjects := map[string]string{}
	for persona, principal := range h.principals {
		subjects[persona] = principal.Subject()
	}
	want := map[string]string{"hiring-manager": promoux015Proposer, "finance-partner": promoux015Finance, "admin": promoux015Manager, "individual-contributor": promoux015Employee}
	for persona, subject := range want {
		if subjects[persona] != subject {
			t.Fatalf("persona %s subject = %q, want %q (the fixture's four separated people)", persona, subjects[persona], subject)
		}
	}

	_, err := h.client.ProposeJourney(h.rpc("individual-contributor"), &journeyv1.ProposeJourneyRequest{
		WorkerRef: promoux015Employee, Target: &journeyv1.Placement{JobCode: "PPL-DIR", Grade: "M4"},
		ProposedBase: "145200.00", EffectiveDate: h.effective, BusinessReason: "self promotion",
	})
	promoux015Code(t, err, codes.PermissionDenied, "ProposeJourney as the employee")
	_, err = h.client.ProposeJourney(h.rpc("finance-partner"), &journeyv1.ProposeJourneyRequest{
		WorkerRef: h.subject, Target: &journeyv1.Placement{JobCode: "OPS-HRBP3", Grade: "P3"},
		ProposedBase: "98000.00", EffectiveDate: h.effective, BusinessReason: "finance proposing",
	})
	promoux015Code(t, err, codes.PermissionDenied, "ProposeJourney as the finance partner")

	proposed, err := h.client.ProposeJourney(h.rpc("hiring-manager"), &journeyv1.ProposeJourneyRequest{
		WorkerRef: h.subject, Target: &journeyv1.Placement{JobCode: "OPS-HRBP3", Grade: "P3"},
		ProposedBase: "98000.00", EffectiveDate: h.effective, BusinessReason: "promoux015 separation of duties",
	})
	if err != nil {
		t.Fatalf("ProposeJourney as the proposer: %v", err)
	}
	id := proposed.GetJourney().GetIntentId()
	_, err = h.client.ExecuteJourney(h.rpc("hiring-manager"), &journeyv1.ExecuteJourneyRequest{IntentId: id})
	promoux015Code(t, err, codes.PermissionDenied, "ExecuteJourney as the proposer (no execution role)")
	_, err = h.client.ExecuteJourney(h.rpc("finance-partner"), &journeyv1.ExecuteJourneyRequest{IntentId: id})
	promoux015Code(t, err, codes.PermissionDenied, "ExecuteJourney as the finance partner")
	executed, err := h.client.ExecuteJourney(h.rpc("admin"), &journeyv1.ExecuteJourneyRequest{IntentId: id})
	if err != nil {
		t.Fatalf("ExecuteJourney as the operator: %v", err)
	}
	if got := executed.GetDetail().GetJourney().GetStage(); got != journeyv1.JourneyStage_JOURNEY_STAGE_FINANCE_APPROVAL {
		t.Fatalf("executed stage = %s, want FINANCE_APPROVAL", got)
	}

	routed := h.items(id)
	finance := routed[promotionexec.NodeApproveFinance]
	if finance.owner != promoux015Finance || finance.status != string(workitem.StatusAssigned) {
		t.Fatalf("finance approval = %+v, want ASSIGNED to the configured finance partner %s", finance, promoux015Finance)
	}

	for _, refused := range []struct{ persona, why string }{
		{"hiring-manager", "the initiator"},
		{"admin", "a non-member holding the execution role"},
		{"individual-contributor", "the employee"},
	} {
		_, err = h.client.DecideJourney(h.rpc(refused.persona), &journeyv1.DecideJourneyRequest{IntentId: id, Approve: true, Reason: "not mine"})
		promoux015Code(t, err, codes.PermissionDenied, "DecideJourney(finance) as "+refused.why)
	}
	if after := h.items(id)[promotionexec.NodeApproveFinance]; after.version != finance.version || after.transitions != finance.transitions {
		t.Fatalf("refused decisions wrote to the finance approval: version %d -> %d, transitions %d -> %d",
			finance.version, after.version, finance.transitions, after.transitions)
	}

	decided, err := h.client.DecideJourney(h.rpc("finance-partner"), &journeyv1.DecideJourneyRequest{IntentId: id, Approve: true, Reason: "finance approves"})
	if err != nil {
		t.Fatalf("DecideJourney as the finance partner: %v", err)
	}
	if got := decided.GetDetail().GetJourney().GetStage(); got != journeyv1.JourneyStage_JOURNEY_STAGE_MANAGER_APPROVAL {
		t.Fatalf("after finance stage = %s, want MANAGER_APPROVAL", got)
	}
	manager := h.items(id)[promotionexec.NodeApproveManager]
	if manager.owner != promoux015Manager || manager.status != string(workitem.StatusAssigned) {
		t.Fatalf("manager approval = %+v, want ASSIGNED to the employee's current manager %s", manager, promoux015Manager)
	}
	for _, refused := range []struct{ persona, why string }{
		{"finance-partner", "the finance partner, who decided the sibling approval"},
		{"hiring-manager", "the initiator"},
	} {
		_, err = h.client.DecideJourney(h.rpc(refused.persona), &journeyv1.DecideJourneyRequest{IntentId: id, Approve: true, Reason: "not mine"})
		promoux015Code(t, err, codes.PermissionDenied, "DecideJourney(manager) as "+refused.why)
	}

	waiting, err := h.client.DecideJourney(h.rpc("admin"), &journeyv1.DecideJourneyRequest{IntentId: id, Approve: true, Reason: "manager approves"})
	if err != nil {
		t.Fatalf("DecideJourney as the current manager: %v", err)
	}
	if got := waiting.GetDetail().GetJourney().GetStage(); got != journeyv1.JourneyStage_JOURNEY_STAGE_WAITING_EFFECTIVE_DATE {
		t.Fatalf("after manager stage = %s, want WAITING_EFFECTIVE_DATE", got)
	}

	final := h.items(id)
	for node, decider := range map[string]string{promotionexec.NodeApproveFinance: promoux015Finance, promotionexec.NodeApproveManager: promoux015Manager} {
		item := final[node]
		if item.status != string(workitem.StatusCompleted) || item.completedBy != decider ||
			item.decisionApprover != decider || item.decidedBy != decider || item.claimActor != decider || item.decisionActor != decider {
			// claimed_by is cleared when an item completes, so the claim is
			// evidenced by the journey.approval.claimed transition's actor.
			t.Errorf("%s decided = %+v, want the claim transition, completion, work_item_decision, intent_decision and decision transition all to name the caller %s", node, item, decider)
		}
	}
}

// TestPromotionDecisionSeparationOfDutiesRefusesANonMemberHoldingTheExecutionRole
// proves the execution role never makes a caller an approver: an operator
// credential that holds every administrative role but is no member of the open
// approval is refused with the route sentinel before any write.
func TestPromotionDecisionSeparationOfDutiesRefusesANonMemberHoldingTheExecutionRole(t *testing.T) {
	h := promoux015Compose(t)
	id := h.proposeAndExecute()
	before := h.items(id)[promotionexec.NodeApproveFinance]

	now := time.Now()
	token, err := h.verifier.Issue(trust.Claims{
		Issuer: h.cfg.Issuer, Audience: h.cfg.Audience, Subject: "principal:promoux015-operator", SubjectKind: "human", Tenant: h.cfg.Tenant,
		OrganizationScopeID: "org:" + h.cfg.Tenant + ":people-ops", Roles: []string{"hcm_admin", "comp_admin", "promotion_operator"},
		Purposes: []string{"compensation_review"}, AuthenticationMethod: "bearer_token", Assurance: "substantial", SessionRef: "session-promoux015-operator",
		IssuedAtUnix: now.Add(-time.Minute).Unix(), ExpiresAtUnix: now.Add(time.Hour).Unix(),
	})
	if err != nil {
		t.Fatalf("issue operator credential: %v", err)
	}
	operator, err := h.verifier.Verify(context.Background(), trust.Credential{Scheme: "Bearer", Token: token, Audience: h.cfg.Audience})
	if err != nil {
		t.Fatalf("verify operator credential: %v", err)
	}
	_, err = h.composed.Cell().Journey.Decide(trust.WithPrincipal(context.Background(), operator), id, workspace.Decision{Approve: true, Reason: "operator override"})
	if !errors.Is(err, app.ErrProposalDecisionRoute) || !errors.Is(err, workspace.ErrDenied) {
		t.Fatalf("Decide as a non-member operator = %v, want ErrProposalDecisionRoute (denied)", err)
	}
	if after := h.items(id)[promotionexec.NodeApproveFinance]; after.version != before.version || after.transitions != before.transitions || after.claimedBy != "" {
		t.Fatalf("a refused non-member decision wrote to the approval: %+v -> %+v", before, after)
	}
}

// promoux015Delegate reassigns the open approval of node from the routed
// owner to delegate through the real workitem.Store.Reassign, with a
// directory in which the owner is unavailable and has delegated that exact
// requirement to delegate. It is the one production mechanism that makes a
// second principal a member of an already-routed approval, and it is how
// these fixtures place the initiator and a sibling decider on the item.
// requester is the requester the re-resolution is told about: humanwork's own
// resolver excludes that principal, so a fixture that must make the initiator
// a member names a different requester -- the misrouted reassignment the
// engine's initiator check exists to refuse.
func (h *promoux015Harness) promoux015Delegate(node, owner, delegate, requester string) {
	h.t.Helper()
	ctx := context.Background()
	tenantID := pgstore.TenantID(h.cfg.Tenant)
	tx, err := h.pool.Begin(ctx)
	if err != nil {
		h.t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		h.t.Fatalf("tenant: %v", err)
	}
	var workItemID string
	var version int64
	var deadline time.Time
	if err := tx.QueryRow(ctx, `SELECT work_item_id::text, item_version, deadline_at FROM work_item WHERE node_id = $1 AND status IN ('ASSIGNED','AVAILABLE')`, node).Scan(&workItemID, &version, &deadline); err != nil {
		h.t.Fatalf("find open %s: %v", node, err)
	}
	compile := promotionexec.CompileManagerApprovalRequirement
	floor := promotionexec.ManagerApprovalAuthorityFloor
	if node == promotionexec.NodeApproveFinance {
		compile, floor = promotionexec.CompileFinanceApprovalRequirement, promotionexec.FinanceApprovalAuthorityFloor
	}
	requirement, err := compile(owner, deadline)
	if err != nil {
		h.t.Fatalf("compile: %v", err)
	}
	at := values.NewInstant(time.Now().UTC())
	scope := requirement.Candidates.Scope
	directory := humanwork.NewMemoryDirectory("promoux015-delegation/1").
		WithPrincipal(humanwork.PrincipalFacts{PrincipalID: owner, Active: true, Available: false, Roles: []string{floor}, OrganizationScopeID: scope.Ref, IdentityAssuranceRef: "assurance:substantial"}).
		WithPrincipal(humanwork.PrincipalFacts{PrincipalID: delegate, Active: true, Available: true, Roles: []string{floor}, OrganizationScopeID: scope.Ref, IdentityAssuranceRef: "assurance:substantial"}).
		WithDelegation(humanwork.Delegation{
			DelegationID: "delegation:promoux015:" + node, FromPrincipalID: owner, ToPrincipalID: delegate,
			AllowedRequirementIDs: []string{requirement.RequirementID}, AllowedRoles: []string{floor}, Scope: scope,
			NotBefore: values.NewInstant(time.Now().UTC().Add(-time.Hour)), Expiry: values.NewInstant(time.Now().UTC().Add(24 * time.Hour)),
			PolicyRef: "policy.promoux015.delegation/v1",
		})
	id, err := uuid.Parse(workItemID)
	if err != nil {
		h.t.Fatalf("work item id: %v", err)
	}
	reassigned, err := (workitem.Store{}).Reassign(ctx, tx, workitem.ReassignInput{
		TenantID: tenantID, WorkItemID: id, ExpectedVersion: version, Requirement: requirement,
		Resolution: humanwork.ResolutionInput{RequesterPrincipalID: requester, EffectiveAt: at},
		Directory:  directory, Clock: func() values.Instant { return at },
		Meta: workitem.TransitionMeta{ActorPrincipalID: "system:promoux015-fixture", Reason: "leave", At: at.Time()},
	})
	if err != nil {
		h.t.Fatalf("Reassign %s to %s: %v", node, delegate, err)
	}
	if membership := workitem.MembershipOf(reassigned, delegate, time.Now().UTC()); membership == workitem.MembershipNone {
		h.t.Fatalf("fixture did not make %s a member of %s: owner %s/%s candidates %+v", delegate, node, reassigned.OwnerKind, reassigned.OwnerRef, reassigned.Assignment.Resolution.Candidates)
	}
	if err := tx.Commit(ctx); err != nil {
		h.t.Fatalf("commit: %v", err)
	}
}

// TestPromotionDecisionSeparationOfDutiesRefusesTheInitiator delegates the open
// finance approval to the proposer, so the initiator is a genuine member of
// the item, and proves the initiator is still refused with the separation
// sentinel before any write.
func TestPromotionDecisionSeparationOfDutiesRefusesTheInitiator(t *testing.T) {
	h := promoux015Compose(t)
	id := h.proposeAndExecute()
	h.promoux015Delegate(promotionexec.NodeApproveFinance, promoux015Finance, promoux015Proposer, "principal:promoux015-misrouted-requester")
	before := h.items(id)[promotionexec.NodeApproveFinance]

	engine, ctx := h.engine("hiring-manager")
	_, err := engine.Decide(ctx, id, workspace.Decision{Approve: true, Reason: "approving my own proposal"})
	if !errors.Is(err, app.ErrProposalDecisionSeparation) || !errors.Is(err, workspace.ErrDenied) {
		t.Fatalf("Decide as the initiator who is a member = %v, want ErrProposalDecisionSeparation (denied)", err)
	}
	if after := h.items(id)[promotionexec.NodeApproveFinance]; after.version != before.version || after.transitions != before.transitions || after.claimedBy != "" {
		t.Fatalf("a refused initiator decision wrote to the approval: %+v -> %+v", before, after)
	}
}

// TestPromotionDecisionSeparationOfDutiesRefusesOnePrincipalDecidingBothApprovals
// has the finance partner decide the finance approval, then delegates the
// manager approval to the same finance partner, and proves they cannot decide
// the sibling approval of the same proposal.
func TestPromotionDecisionSeparationOfDutiesRefusesOnePrincipalDecidingBothApprovals(t *testing.T) {
	h := promoux015Compose(t)
	id := h.proposeAndExecute()
	if _, err := h.client.DecideJourney(h.rpc("finance-partner"), &journeyv1.DecideJourneyRequest{IntentId: id, Approve: true, Reason: "finance approves"}); err != nil {
		t.Fatalf("DecideJourney as the finance partner: %v", err)
	}
	h.promoux015Delegate(promotionexec.NodeApproveManager, promoux015Manager, promoux015Finance, promoux015Proposer)
	before := h.items(id)[promotionexec.NodeApproveManager]

	engine, ctx := h.engine("finance-partner")
	_, err := engine.Decide(ctx, id, workspace.Decision{Approve: true, Reason: "and the manager approval too"})
	if !errors.Is(err, app.ErrProposalDecisionSeparation) || !errors.Is(err, workspace.ErrDenied) {
		t.Fatalf("Decide(manager) by the finance approver = %v, want ErrProposalDecisionSeparation (denied)", err)
	}
	if after := h.items(id)[promotionexec.NodeApproveManager]; after.version != before.version || after.transitions != before.transitions || after.claimedBy != "" {
		t.Fatalf("a refused sibling decision wrote to the approval: %+v -> %+v", before, after)
	}
}

// TestPromotionPublishedPathsAreAllProposable is PROMOUX-015's drift test on
// the served path: every promotion path ListWorkers publishes is proposed
// through ProposeJourney for a worker holding its source profile, and none is
// refused as unpublished.
func TestPromotionPublishedPathsAreAllProposable(t *testing.T) {
	h := promoux015Compose(t)
	listed, err := h.client.ListWorkers(h.rpc("admin"), &journeyv1.ListWorkersRequest{})
	if err != nil {
		t.Fatalf("ListWorkers: %v", err)
	}
	paths := listed.GetOptions().GetPromotionPaths()
	if len(paths) == 0 {
		t.Fatal("ListWorkers published no promotion paths; the drift test proves nothing")
	}
	scenarios, err := fixtures.LegacyScenarios()
	if err != nil {
		t.Fatalf("LegacyScenarios: %v", err)
	}
	corpusBase := map[string]string{scenarios.Worker: scenarios.Scenarios[0].CurrentAmount, "jane-doe": fixtures.JanePromotionBase}
	used := map[string]bool{}
	demoProposed := 0
	for _, path := range paths {
		var worker *journeyv1.Worker
		base := ""
		for _, candidate := range listed.GetWorkers() {
			if used[candidate.GetWorkerRef()] || candidate.GetJobCode() != path.GetSourceJobCode() || candidate.GetGrade() != path.GetSourceGrade() {
				continue
			}
			base = candidate.GetBasePay()
			if base == "" {
				base = corpusBase[candidate.GetWorkerRef()]
			}
			if base != "" {
				worker = candidate
				break
			}
		}
		if worker == nil {
			t.Errorf("published path %s %s/%s -> %s/%s has no listed worker holding its source profile", path.GetPathRef(),
				path.GetSourceJobCode(), path.GetSourceGrade(), path.GetTargetJobCode(), path.GetTargetGrade())
			continue
		}
		used[worker.GetWorkerRef()] = true
		current, _ := new(big.Rat).SetString(base)
		increase := big.NewRat(10, 100)
		if minimum := path.GetMinimumBaseIncrease(); minimum != "" {
			increase, _ = new(big.Rat).SetString(minimum)
		}
		proposedBase := new(big.Rat).Mul(current, new(big.Rat).Add(big.NewRat(1, 1), increase)).FloatString(2)
		_, err := h.client.ProposeJourney(h.rpc("admin"), &journeyv1.ProposeJourneyRequest{
			WorkerRef: worker.GetWorkerRef(), Target: &journeyv1.Placement{JobCode: path.GetTargetJobCode(), Grade: path.GetTargetGrade()},
			ProposedBase: proposedBase, EffectiveDate: h.effective, BusinessReason: "promoux015 published path drift",
		})
		if err != nil {
			t.Errorf("ProposeJourney for published path %s (%s %s/%s -> %s/%s at %s) = %v", path.GetPathRef(), worker.GetWorkerRef(),
				path.GetSourceJobCode(), path.GetSourceGrade(), path.GetTargetJobCode(), path.GetTargetGrade(), proposedBase, err)
			continue
		}
		if strings.HasPrefix(path.GetPathRef(), "demoworkforce:") {
			demoProposed++
		}
	}
	if demoProposed == 0 {
		t.Fatal("no demo ladder path was proposed; the fixture lacks the population the drift defect was found on")
	}
}
