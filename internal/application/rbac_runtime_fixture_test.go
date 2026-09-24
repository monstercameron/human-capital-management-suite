package application

// Runtime RBAC fixture: one composed serve cell (embedded PostgreSQL, the
// local-dev demo tenant, the executable promotion plan) plus a small, explicit
// reporting structure of new "rbac-" workers inserted through the real
// workforce store and projected into the aggregates, the durable role
// assignments those workers hold, and one signed credential per user the
// suite in rbac_runtime_integration_test.go exercises.
//
// Relationships (unit A = marketing, unit B = sales; both seeded HarborCare
// units):
//
//	<marketing sponsor> <- rbac-gus (director, A)
//	rbac-gus  <- rbac-dana (manager, A) <- rbac-eli (IC, A), rbac-fay (IC, A)
//	rbac-gus  <- rbac-otto (IC, A; same unit as dana's team, NOT dana's report)
//	rbac-gus  <- rbac-hana (manager, B) <- rbac-ivy (IC, B), rbac-rex (IC, B)
//
// rbac-rex exists only to carry the revoked user: a credential that still
// signs comp_admin while the durable assignment says worker_self.

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	adminv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/admin/v1"
	humanworkv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/humanwork/v1"
	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	workflowv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/workflow/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/data/demoworkforce"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/data/workforce"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	kernelvalues "github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

const (
	rbacSigningKey = "rbac-runtime-integration-signing-key-000"
	rbacForgeryKey = "rbac-runtime-integration-forged-key-00000"
	rbacOrgScope   = "org:" + demoworkforce.CompanyKey + ":people-ops"
	rbacUnitA      = "marketing"
	rbacUnitB      = "sales"
	// rbacFinancePartner is LocalDevFinancePartner's worker: the configured
	// finance approver every promotion's FINANCE_APPROVAL routes to.
	rbacFinancePartner = "hc-054-thomas-baker"
)

// rbacWorker is one fixture worker: its reporting line, placement and the
// distinct pay the field checks compare against.
type rbacWorker struct {
	key, name, manager, unit, job, title, grade, base, bonus, zone, location string
	durableRoles                                                             []string
}

// rbacWorkers is inserted in order, so every manager is projected before
// its reports (ProjectWorker resolves the manager reference at insert time).
// gus's manager is filled in from the seeded marketing sponsor.
func rbacWorkers(sponsor string) []rbacWorker {
	return []rbacWorker{
		{key: "rbac-gus", name: "Gus Rbac", manager: sponsor, unit: rbacUnitA, job: "MKT-DIR", title: "Marketing Director", grade: "M4", base: "176000.00", bonus: "0.1900", zone: "US-EAST", location: "Boston, MA", durableRoles: []string{"manager"}},
		{key: "rbac-dana", name: "Dana Rbac", manager: "rbac-gus", unit: rbacUnitA, job: "MKT-DIR", title: "Marketing Director", grade: "M4", base: "152000.00", bonus: "0.1700", zone: "US-EAST", location: "Boston, MA", durableRoles: []string{"manager"}},
		{key: "rbac-hana", name: "Hana Rbac", manager: "rbac-gus", unit: rbacUnitB, job: "SAL-DIR", title: "Sales Director", grade: "M4", base: "171500.00", bonus: "0.3400", zone: "US-EAST", location: "New York, NY", durableRoles: []string{"manager"}},
		{key: "rbac-eli", name: "Eli Rbac", manager: "rbac-dana", unit: rbacUnitA, job: "MKT-CNT3", title: "Content Strategy Lead", grade: "P3", base: "105000.00", bonus: "0.0900", zone: "US-EAST", location: "Boston, MA", durableRoles: []string{"worker_self"}},
		{key: "rbac-fay", name: "Fay Rbac", manager: "rbac-dana", unit: rbacUnitA, job: "MKT-CNT3", title: "Content Strategy Lead", grade: "P3", base: "110000.00", bonus: "0.1000", zone: "US-EAST", location: "Boston, MA", durableRoles: []string{"worker_self"}},
		{key: "rbac-otto", name: "Otto Rbac", manager: "rbac-gus", unit: rbacUnitA, job: "MKT-DG3", title: "Demand Generation Manager", grade: "P3", base: "118500.00", bonus: "0.1150", zone: "US-EAST", location: "Boston, MA", durableRoles: []string{"worker_self"}},
		{key: "rbac-ivy", name: "Ivy Rbac", manager: "rbac-hana", unit: rbacUnitB, job: "SAL-AE3", title: "Senior Account Executive", grade: "P4", base: "133000.00", bonus: "0.3900", zone: "US-EAST", location: "New York, NY", durableRoles: []string{"worker_self"}},
		{key: "rbac-rex", name: "Rex Rbac", manager: "rbac-hana", unit: rbacUnitB, job: "SAL-SOL3", title: "Solutions Consultant", grade: "P3", base: "121000.00", bonus: "0.1700", zone: "US-EAST", location: "New York, NY", durableRoles: []string{"worker_self"}},
	}
}

// rbacHarness is the composed cell, its clients and the fixture's state.
type rbacHarness struct {
	t        *testing.T
	cfg      ServeConfig
	pool     *pgxadapter.Pool
	composed *App
	verifier *trust.HMACVerifier

	journey   journeyv1.JourneyServiceClient
	intents   intentsv1.IntentServiceClient
	admin     adminv1.AdminServiceClient
	work      humanworkv1.WorkServiceClient
	workflows workflowv1.WorkflowServiceClient
	http      *http.Client

	// tokens are the suite's users by name; executor is the local-dev admin
	// persona (the execution operator) used only to set the fixture up.
	tokens   map[string]string
	executor string
	workers  map[string]rbacWorker
	// seededA and seededB are seeded HarborCare workers in units A and B.
	seededA, seededB []string

	// The fixture journey: gus's promotion of fay, executed to its finance
	// approval so a workflow instance and work items exist.
	intentID, instanceID, financeItemID string
	proposeErr                          error
}

// rbacCompose composes, starts and populates the one cell the whole suite
// shares.
func rbacCompose(t *testing.T) *rbacHarness {
	t.Helper()
	db := pgtest.New(t)
	pool, err := pgxadapter.NewPool(context.Background(), db.URL, map[string]string{"search_path": db.Schema})
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(pool.Close)
	cfg := ServeConfig{
		GRPCListen: "127.0.0.1:0", HTTPListen: "127.0.0.1:0", DatabaseURL: db.URL,
		DevHMACKey: rbacSigningKey, PageCursorKey: integrationPageCursorKey, Issuer: DefaultIssuer, Audience: DefaultAudience,
		Tenant: demoworkforce.CompanyKey, CellID: "cell-rbac-runtime", MaxDeadline: 60 * time.Second,
		Workspace: true, DevBrowserLogin: true, OTelExporter: OTelExporterNone,
		ExecutionAuthority: true, ExecutionAuthorityDigest: "sha256:rbac-runtime",
		ExecutionAuthorityRole: "promotion_operator", ExecutionApprover: "principal:promotion-approver",
		ExecutionFinancePartner: LocalDevFinancePartner,
		WorkflowPlan:            WorkflowPlanExecute, TimerTzdbVersion: DefaultTimerTzdbVersion,
		TimerCalendarVersion: DefaultTimerCalendarVersion,
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("configuration: %v", err)
	}
	composed, err := ComposeServe(context.Background(), ServeInput{Config: cfg, Pool: pool, Identity: "rbac-runtime",
		Options: Options{ProviderReceipts: newFakeProviderReceipts()}})
	if err != nil {
		t.Fatalf("ComposeServe: %v", err)
	}
	activateShippedWorkflowVersions(t, pool, time.Now().UTC())
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
	conn, err := grpc.NewClient(composed.GRPCAddr(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc client: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	h := &rbacHarness{
		t: t, cfg: cfg, pool: pool, composed: composed, verifier: verifier,
		journey: journeyv1.NewJourneyServiceClient(conn), intents: intentsv1.NewIntentServiceClient(conn),
		admin: adminv1.NewAdminServiceClient(conn), work: humanworkv1.NewWorkServiceClient(conn),
		workflows: workflowv1.NewWorkflowServiceClient(conn),
		// A refused page may answer with a redirect; the suite must see that
		// answer, not the page the redirect lands on.
		http:   &http.Client{Timeout: 60 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
		tokens: map[string]string{}, workers: map[string]rbacWorker{},
	}
	for _, persona := range composeDevPersonas(verifier, cfg, time.Now) {
		if persona.ID == "admin" {
			h.executor = persona.Token
		}
	}
	if h.executor == "" {
		t.Fatal("the local-dev admin persona (the execution operator) was not composed")
	}
	h.insertWorkers()
	h.seedAssignments()
	h.issueUsers()
	h.proposeFixtureJourney()
	return h
}

// insertWorkers records the rbac- workers through workforce.Store.Create and
// projects each into the aggregates, in one tenant-scoped transaction.
func (h *rbacHarness) insertWorkers() {
	h.t.Helper()
	tenant := pgstore.TenantID(h.cfg.Tenant)
	plan, err := demoworkforce.Plan(tenant)
	if err != nil {
		h.t.Fatalf("demoworkforce.Plan: %v", err)
	}
	sponsor := ""
	for _, employee := range plan {
		switch employee.Row.OrgUnit {
		case rbacUnitA:
			h.seededA = append(h.seededA, employee.Row.WorkerKey)
			if sponsor == "" {
				// The unit's first worker reports to the unit sponsor.
				sponsor = employee.ManagerKey
			}
		case rbacUnitB:
			h.seededB = append(h.seededB, employee.Row.WorkerKey)
		}
	}
	if sponsor == "" || len(h.seededA) < 3 || len(h.seededB) < 1 {
		h.t.Fatalf("seeded units: sponsor %q, %d in %s, %d in %s", sponsor, len(h.seededA), rbacUnitA, len(h.seededB), rbacUnitB)
	}
	ctx := context.Background()
	tx, err := h.pool.Begin(ctx)
	if err != nil {
		h.t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, tenant); err != nil {
		h.t.Fatalf("tenant: %v", err)
	}
	recorded := time.Date(2026, time.September, 1, 15, 0, 0, 0, time.UTC)
	for index, w := range rbacWorkers(sponsor) {
		h.workers[w.key] = w
		row := workforce.WorkerRow{
			TenantID: tenant, WorkerID: uuid.NewSHA1(uuid.NameSpaceURL, []byte("hcmnext:rbac-runtime:"+w.key)), WorkerKey: w.key,
			LegalName: w.name, PreferredName: w.name[:len(w.name)-5], WorkerNumber: fmt.Sprintf("RBAC-%03d", index+1),
			WorkerType: "employee", LifecycleStatus: "active",
			EmploymentID: w.key + "-emp", AssignmentID: w.key + "-asg",
			JobCode: w.job, JobTitle: w.title, Grade: w.grade, OrgUnit: w.unit, PositionID: "RBAC-POS-" + w.key[len("rbac-"):],
			Location: w.location, PayZone: w.zone, FTE: "1.0000", ManagerRelationshipRef: w.manager,
			HireDate: "2022-03-01", EffectiveFrom: "2026-01-01", BasePay: w.base, Currency: "USD", PayBasis: "ANNUAL_SALARY", BonusTarget: w.bonus,
			RevisionStream: "people.worker." + w.key, RevisionSequence: 1, KnownAt: recorded, RecordedAt: recorded,
			CreatedBy: "rbac-runtime-fixture", Source: workforce.SourceCreated,
		}
		if _, err := (workforce.Store{}).Create(ctx, tx, row); err != nil {
			h.t.Fatalf("create %s: %v", w.key, err)
		}
		if _, err := demoworkforce.ProjectWorker(ctx, tx, row, demoworkforce.HarborCare.LegalEntity); err != nil {
			h.t.Fatalf("project %s: %v", w.key, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		h.t.Fatalf("commit: %v", err)
	}
}

// seedAssignments records every rbac- worker's durable role assignment, and
// the durable roles of the two seeded HarborCare workers the suite signs
// credentials for.
//
// The second half is not optional. The demo tenant seeds a durable role set
// for every HarborCare worker, and roleaccess.AssignedRoles prefers a stored
// assignment over the roles a credential was admitted with, so the payroll
// and HR-partner credentials bound to seeded workers only carry the authority
// this fixture intends if that authority is also what the directory says.
func (h *rbacHarness) seedAssignments() {
	h.t.Helper()
	ctx := context.Background()
	tenant := kernelvalues.TenantId(h.cfg.Tenant)
	store := h.composed.Cell().RoleAccess
	for _, w := range h.workers {
		if _, err := store.SaveAssignment(ctx, tenant, "system:rbac-runtime-fixture",
			roleaccess.Assignment{WorkerRef: w.key, RoleIDs: w.durableRoles, Reason: "Seed durable role assignments for RBAC runtime authorization tests"}); err != nil {
			h.t.Fatalf("SaveAssignment %s %v: %v", w.key, w.durableRoles, err)
		}
	}
	snapshot, err := store.Load(ctx, tenant, rbacOrgScope)
	if err != nil {
		h.t.Fatalf("load role access: %v", err)
	}
	versions := map[string]int64{}
	for _, assignment := range snapshot.Assignments {
		versions[assignment.WorkerRef] = assignment.Version
	}
	for _, bound := range []struct {
		worker string
		roles  []string
	}{{h.seededA[1], []string{"hr_partner"}}, {h.seededA[2], []string{"payroll_manager"}}} {
		if _, err := store.SaveAssignment(ctx, tenant, "system:rbac-runtime-fixture",
			roleaccess.Assignment{Version: versions[bound.worker], WorkerRef: bound.worker, RoleIDs: bound.roles, Reason: "Set fixture assignment to the authorization case role set"}); err != nil {
			h.t.Fatalf("SaveAssignment %s %v: %v", bound.worker, bound.roles, err)
		}
	}
}

// rbacUser is one credential the suite signs.
type rbacUser struct {
	name, subject string
	roles         []string
	purpose       string
}

// issueUsers signs every user the suite calls as, plus the three
// authentication-hardening credentials.
func (h *rbacHarness) issueUsers() {
	h.t.Helper()
	now := time.Now()
	users := []rbacUser{
		{"admin", "principal:rbac-admin", []string{"hcm_admin"}, "compensation_review"},
		{"compAdmin", "principal:rbac-comp-admin", []string{"comp_admin"}, "compensation_review"},
		{"gus", "rbac-gus", []string{"manager"}, "compensation_review"},
		{"dana", "rbac-dana", []string{"manager"}, "compensation_review"},
		{"hana", "rbac-hana", []string{"manager"}, "compensation_review"},
		{"eli", "rbac-eli", []string{"worker_self"}, "self_service_view"},
		// financePartner is the configured finance approver, so it is the
		// routed assignee of the fixture journey's FINANCE_APPROVAL.
		{"financePartner", rbacFinancePartner, []string{"finance_partner"}, "compensation_review"},
		{"auditor", "principal:rbac-auditor", []string{"auditor"}, "audit_review"},
		// payroll and hrPartner are bound to seeded unit-A workers so the
		// own-unit directory policy gives them a real population.
		{"payroll", h.seededA[2], []string{"payroll_manager"}, "payroll_processing"},
		{"hrPartner", h.seededA[1], []string{"hr_partner"}, "compensation_review"},
		{"operator", "principal:rbac-operator", []string{"hcmnext.trust.role.operator"}, "compensation_review"},
		{"noRoles", "principal:rbac-no-roles", nil, "self_service_view"},
		// revoked: the credential still signs comp_admin, the durable
		// assignment for rbac-rex says worker_self.
		{"revoked", "rbac-rex", []string{"comp_admin"}, "compensation_review"},
	}
	for _, u := range users {
		h.tokens[u.name] = h.issue(h.verifier, u, h.cfg.Tenant, now.Add(-time.Minute), now.Add(8*time.Hour))
	}
	h.tokens["expired"] = h.issue(h.verifier, rbacUser{"expired", "rbac-dana", []string{"manager"}, "compensation_review"}, h.cfg.Tenant, now.Add(-3*time.Hour), now.Add(-time.Hour))
	forger, err := trust.NewHMACVerifier(trust.HMACVerifierConfig{Key: []byte(rbacForgeryKey), Issuer: h.cfg.Issuer, Audience: h.cfg.Audience})
	if err != nil {
		h.t.Fatalf("forger: %v", err)
	}
	h.tokens["forged"] = h.issue(forger, rbacUser{"forged", "principal:rbac-admin", []string{"hcm_admin"}, "compensation_review"}, h.cfg.Tenant, now.Add(-time.Minute), now.Add(time.Hour))
	h.tokens["otherTenant"] = h.issue(h.verifier, rbacUser{"otherTenant", "principal:rbac-admin", []string{"hcm_admin"}, "compensation_review"}, "other-tenant", now.Add(-time.Minute), now.Add(time.Hour))
}

func (h *rbacHarness) issue(issuer *trust.HMACVerifier, u rbacUser, tenant string, issued, expires time.Time) string {
	h.t.Helper()
	var purposes []string
	if u.purpose != "" {
		purposes = []string{u.purpose}
	}
	token, err := issuer.Issue(trust.Claims{
		Issuer: h.cfg.Issuer, Audience: h.cfg.Audience, Subject: u.subject, SubjectKind: "human", Tenant: tenant,
		OrganizationScopeID: rbacOrgScope, Roles: u.roles, Purposes: purposes,
		AuthenticationMethod: "bearer_token", Assurance: "substantial", SessionRef: "session-rbac-" + u.name,
		IssuedAtUnix: issued.Unix(), ExpiresAtUnix: expires.Unix(),
	})
	if err != nil {
		h.t.Fatalf("issue %s: %v", u.name, err)
	}
	return token
}

// rpc authenticates an outgoing gRPC call as a named user.
func (h *rbacHarness) rpc(user string) context.Context {
	token, ok := h.tokens[user]
	if !ok {
		h.t.Fatalf("no user %q", user)
	}
	return h.bearer(token)
}

func (h *rbacHarness) bearer(token string) context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	h.t.Cleanup(cancel)
	return metadata.AppendToOutgoingContext(ctx, transport.AuthorizationMetadataKey, "Bearer "+token)
}

// page fetches one served workspace page as a user and returns its status.
func (h *rbacHarness) page(user, page string) (int, error) {
	req, err := http.NewRequest(http.MethodGet, "http://"+h.composed.HTTPAddr()+"/workspace/app/"+page, nil)
	if err != nil {
		return 0, err
	}
	req.AddCookie(&http.Cookie{Name: "hcmnext_session", Value: h.tokens[user]})
	res, err := h.http.Do(req)
	if err != nil {
		return 0, err
	}
	defer res.Body.Close()
	return res.StatusCode, nil
}

// proposeFixtureJourney creates the journey the intent, inspection, work
// and workflow cases probe: gus (fay's skip-level manager) proposes fay's
// promotion along the MKT-CNT3/P3 -> MKT-DIR/M4 ladder edge and the execution
// operator executes it, so a workflow instance and the FINANCE_APPROVAL work
// item exist. dana is fay's direct manager: she is on the subject's line, and
// the MANAGER_APPROVAL will route to her.
//
// gus, not dana, proposes because dana cannot be both: the executable plan
// routes the manager approval to the subject's current manager, and a
// requester who resolves as their own approver is refused at execution
// (approverclass: "an approver resolved to the proposal's requester"). dana's
// own authority to propose is still probed, for eli (case G-00), and that
// proposal is never executed.
func (h *rbacHarness) proposeFixtureJourney() {
	h.t.Helper()
	effective := time.Now().UTC().AddDate(0, 1, 0).Format(time.DateOnly)
	_, h.proposeErr = h.journey.ProposeJourney(h.rpc("dana"), &journeyv1.ProposeJourneyRequest{
		WorkerRef: "rbac-eli", Target: &journeyv1.Placement{JobCode: "MKT-DIR", Grade: "M4"},
		ProposedBase: "126500.00", EffectiveDate: effective, BusinessReason: "rbac runtime fixture: dana proposes",
	})
	proposed, err := h.journey.ProposeJourney(h.rpc("gus"), &journeyv1.ProposeJourneyRequest{
		WorkerRef: "rbac-fay", Target: &journeyv1.Placement{JobCode: "MKT-DIR", Grade: "M4"},
		ProposedBase: "132000.00", EffectiveDate: effective, BusinessReason: "rbac runtime fixture",
	})
	if err != nil {
		h.t.Fatalf("ProposeJourney(rbac-fay) as gus: %v", err)
	}
	h.intentID = proposed.GetJourney().GetIntentId()
	if _, err := h.journey.ExecuteJourney(h.bearer(h.executor), &journeyv1.ExecuteJourneyRequest{IntentId: h.intentID}); err != nil {
		h.t.Fatalf("ExecuteJourney(%s): %v %v; engine diagnostic: %v", h.intentID, err, status.Convert(err).Details(), h.executeDiagnostic())
	}
	ctx := context.Background()
	if err := h.pool.QueryRow(ctx, `SELECT instance_id::text FROM workflow_instance ORDER BY created_at DESC LIMIT 1`).Scan(&h.instanceID); err != nil {
		h.t.Fatalf("read the fixture workflow instance: %v", err)
	}
	if err := h.pool.QueryRow(ctx, `SELECT work_item_id::text FROM work_item WHERE owner_ref = $1 AND status IN ('ASSIGNED','AVAILABLE') ORDER BY created_at DESC LIMIT 1`,
		rbacFinancePartner).Scan(&h.financeItemID); err != nil {
		h.t.Fatalf("read the fixture finance approval: %v", err)
	}
}

// executeDiagnostic re-runs the refused execution against the composed engine
// and returns the owned error's nested diagnostic, which the wire withholds:
// it is what makes a fixture failure here diagnosable.
func (h *rbacHarness) executeDiagnostic() error {
	principal, err := h.verifier.Verify(context.Background(), trust.Credential{Scheme: "Bearer", Token: h.executor, Audience: h.cfg.Audience})
	if err != nil {
		return err
	}
	_, raw := h.composed.Cell().Journey.Execute(trust.WithPrincipal(context.Background(), principal), h.intentID)
	if owned, ok := envelope.As(raw); ok {
		if diag, ok := owned.Diagnostic(rbacDebugGrant{}); ok {
			return diag
		}
	}
	return raw
}

// rbacDebugGrant lets the fixture read a nested diagnostic in-process.
type rbacDebugGrant struct{}

func (rbacDebugGrant) AllowsInternalDiagnostics() bool { return true }
