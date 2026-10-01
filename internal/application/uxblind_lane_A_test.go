package application

import (
	"context"
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
	"github.com/monstercameron/human-capital-management-suite/internal/data/workeridstore"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	intentapp "github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	platformexecution "github.com/monstercameron/human-capital-management-suite/internal/platform/execution"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/productclient"
)

// uxblind001Managers is the HarborCare reporting line the failing promotion
// ran over: Linh reports to Rafael, Rafael to Darius.
type uxblind001Managers map[string]string

func (m uxblind001Managers) CurrentManagerOf(_ context.Context, _ workitem.Executor, _ uuid.UUID, worker string) (platformexecution.ManagerOf, error) {
	manager, inGraph := m[worker]
	return platformexecution.ManagerOf{InGraph: inGraph, SubjectKey: worker, ManagerPrincipal: manager}, nil
}

// TestTodo_UXBLIND_001 pins the root cause: Rafael (the admin persona) is
// Linh's current manager, so a promotion he proposes could not route its
// manager approval to him (the requester may not approve) and routing refused
// the whole approval start. The manager approval now escalates to the
// requester's own manager, Darius, under its own authority term, and the
// decision-time recheck derives the same holder from the requester.
func TestTodo_UXBLIND_001(t *testing.T) {
	const linh, rafael, darius = "hc-051-linh-tran", "hc-050-rafael-torres", "hc-004-darius-bennett"
	authority := platformexecution.NewPromotionApprovalAuthority(platformexecution.PromotionExecutionConfig{
		Plan: platformexecution.PLAN_EXECUTE, ApproverPrincipalID: demoworkforce.HarborCarePack.ExecutionApproverKey,
		FinancePartnerPrincipalID: demoworkforce.HarborCarePack.FinancePartnerKey,
		Managers:                  uxblind001Managers{linh: rafael, rafael: darius, darius: ""},
	})
	tenant := uuid.New()
	item := workitem.WorkItem{TenantID: tenant, Kind: workitem.KindApproval, NodeID: promotionexec.NodeApproveManager}
	for _, tc := range []struct {
		requester, wantHolder, wantTerm string
	}{
		{rafael, darius, "term:current-manager-of-requester"},
		{darius, rafael, "term:current-manager-of-worker"},
	} {
		current, err := authority.CurrentApprovalAuthority(context.Background(), nil, intentapp.ApprovalAuthorityQuery{
			TenantID: tenant, Item: item, SubjectID: linh, RequesterID: tc.requester, AuthorityPrincipalID: tc.wantHolder,
		})
		if err != nil {
			t.Fatalf("requester %s: CurrentApprovalAuthority: %v", tc.requester, err)
		}
		if current.PrincipalID != tc.wantHolder || current.AuthorityRef != tc.wantTerm || !current.Active {
			t.Fatalf("requester %s: manager approval held by %q under %q (active %v), want %q under %q",
				tc.requester, current.PrincipalID, current.AuthorityRef, current.Active, tc.wantHolder, tc.wantTerm)
		}
	}
}

func TestTodo_UXBLIND_001_Browser(t *testing.T) {
	now := time.Date(2026, 9, 6, 16, 0, 0, 0, time.UTC)
	cfg := twoCompanyConfig()
	verifier := devDirectoryVerifier(t, cfg, now)
	personas := composeDevPersonas(verifier, cfg, func() time.Time { return now })
	for _, persona := range personas {
		if persona.Slot != "finance-partner" {
			continue
		}
		pack, found := demoworkforce.PackFor(persona.Company)
		if !found || persona.WorkerRef != pack.FinancePartnerKey {
			t.Fatalf("%s finance quick pick = %q, want the company's routed worker", persona.Company, persona.WorkerRef)
		}
	}
}

// TestTodo_UXBLIND_001_Regression drives the failing story end to end over
// the real gRPC JourneyService: the admin persona (Rafael) proposes and starts
// a promotion for one of his own reports. Approval start used to fail with
// STORAGE_FAILED because the manager approval could only route to Rafael, the
// requester. Now finance routes to Thomas, the manager approval escalates to
// Rafael's manager Darius (notification audience and decision-time authority
// recheck included), Rafael cannot decide it, and Darius's decision moves the
// journey to its effective-date wait.
func TestTodo_UXBLIND_001_Regression(t *testing.T) {
	conn, pool, personas, _ := uxblind003Cell(t)
	client := journeyv1.NewJourneyServiceClient(conn)
	rpc := func(slot string) context.Context {
		for _, persona := range personas {
			if persona.Company == demoworkforce.HarborCarePack.Key && persona.Slot == slot {
				ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
				t.Cleanup(cancel)
				return metadata.AppendToOutgoingContext(ctx, transport.AuthorizationMetadataKey, "Bearer "+persona.Token)
			}
		}
		t.Fatalf("no HarborCare %s persona", slot)
		return nil
	}
	items := func() map[string][2]string {
		rows, err := pool.Query(context.Background(), `SELECT node_id, owner_ref, status FROM work_item`)
		if err != nil {
			t.Fatalf("read work items: %v", err)
		}
		defer rows.Close()
		out := map[string][2]string{}
		for rows.Next() {
			var node, owner, status string
			if err := rows.Scan(&node, &owner, &status); err != nil {
				t.Fatalf("scan work item: %v", err)
			}
			out[node] = [2]string{owner, status}
		}
		return out
	}
	const rafael, darius, thomas = "hc-050-rafael-torres", "hc-004-darius-bennett", "hc-054-thomas-baker"
	proposed, err := client.ProposeJourney(rpc("admin"), &journeyv1.ProposeJourneyRequest{
		WorkerRef: "hc-051-linh-tran", Target: &journeyv1.Placement{JobCode: "PPL-HRBP4", Grade: "P5"},
		ProposedBase: "160000.00", EffectiveDate: time.Now().UTC().AddDate(0, 1, 0).Format(time.DateOnly),
		BusinessReason: "uxblind-001 manager proposes own report",
	})
	if err != nil {
		t.Fatalf("ProposeJourney as Linh's own manager: %v", err)
	}
	id := proposed.GetJourney().GetIntentId()
	executed, err := client.ExecuteJourney(rpc("admin"), &journeyv1.ExecuteJourneyRequest{IntentId: id})
	if err != nil {
		t.Fatalf("ExecuteJourney (start approval) for a proposal by the subject's manager: %v", err)
	}
	if got := executed.GetDetail().GetJourney().GetStage(); got != journeyv1.JourneyStage_JOURNEY_STAGE_FINANCE_APPROVAL {
		t.Fatalf("executed stage = %s, want FINANCE_APPROVAL", got)
	}
	if finance := items()[promotionexec.NodeApproveFinance]; finance != [2]string{thomas, string(workitem.StatusAssigned)} {
		t.Fatalf("finance approval = %v, want ASSIGNED to %s", finance, thomas)
	}
	decided, err := client.DecideJourney(rpc("finance-partner"), &journeyv1.DecideJourneyRequest{IntentId: id, Approve: true, Reason: "finance approves"})
	if err != nil {
		t.Fatalf("DecideJourney as the finance partner: %v", err)
	}
	if got := decided.GetDetail().GetJourney().GetStage(); got != journeyv1.JourneyStage_JOURNEY_STAGE_MANAGER_APPROVAL {
		t.Fatalf("after finance stage = %s, want MANAGER_APPROVAL", got)
	}
	if manager := items()[promotionexec.NodeApproveManager]; manager != [2]string{darius, string(workitem.StatusAssigned)} {
		t.Fatalf("manager approval = %v, want ASSIGNED to the requester's manager %s", manager, darius)
	}
	if _, err := client.DecideJourney(rpc("admin"), &journeyv1.DecideJourneyRequest{IntentId: id, Approve: true, Reason: "approving my own proposal"}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("DecideJourney(manager) as the requester %s = %v, want PermissionDenied", rafael, err)
	}
	waiting, err := client.DecideJourney(rpc("hiring-manager"), &journeyv1.DecideJourneyRequest{IntentId: id, Approve: true, Reason: "skip-level approves"})
	if err != nil {
		t.Fatalf("DecideJourney as the requester's manager: %v", err)
	}
	if got := waiting.GetDetail().GetJourney().GetStage(); got != journeyv1.JourneyStage_JOURNEY_STAGE_WAITING_EFFECTIVE_DATE {
		t.Fatalf("after manager stage = %s, want WAITING_EFFECTIVE_DATE", got)
	}
}

func TestTodo_UXBLIND_001_Integration(t *testing.T) {
	db := pgtest.New(t)
	pool, err := pgxadapter.NewPool(context.Background(), db.URL, map[string]string{"search_path": db.Schema})
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(pool.Close)
	now := time.Date(2026, 9, 6, 16, 0, 0, 0, time.UTC)
	cfg := twoCompanyExecutionConfig(db.URL)
	composed, err := ComposeServe(context.Background(), ServeInput{Config: cfg, Pool: pool, Identity: "uxblind-lane-a-routing", Options: Options{Now: func() time.Time { return now }}})
	if err != nil {
		t.Fatalf("ComposeServe: %v", err)
	}
	t.Cleanup(func() { _ = composed.Stop(context.Background()) })
	verifier, err := trust.NewHMACVerifier(trust.HMACVerifierConfig{Key: []byte(cfg.DevHMACKey), Issuer: cfg.Issuer, Audience: cfg.Audience, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatalf("build verifier: %v", err)
	}
	for _, tc := range []struct {
		company, slot, worker, job, grade, base, approver string
	}{
		{demoworkforce.HarborCarePack.Key, "admin", "hc-051-linh-tran", "PPL-HRBP4", "P5", "160000.00", demoworkforce.HarborCarePack.FinancePartnerKey},
		{demoworkforce.IronridgePack.Key, "admin", "ir-013-ana-flores", "IR-FMN", "C4", "40.00", demoworkforce.IronridgePack.FinancePartnerKey},
	} {
		ctx := devPersonaContext(t, verifier, cfg, tc.company, tc.slot, now)
		proposed, err := composed.Cell().Journey.Propose(ctx, workspace.ProposalInput{
			WorkerRef: tc.worker, TargetJobCode: tc.job, TargetGrade: tc.grade,
			ProposedBase: tc.base, EffectiveDate: "2026-10-01", BusinessReason: "promotion routing regression",
		})
		if err != nil {
			t.Fatalf("%s Journey.Propose: %v", tc.company, err)
		}
		detail, err := composed.Cell().Journey.Execute(ctx, proposed.IntentID)
		if err != nil {
			t.Fatalf("%s Journey.Execute: %v", tc.company, err)
		}
		if detail.Summary.Stage != workspace.JourneyStage("FINANCE_APPROVAL") {
			t.Fatalf("%s stage = %s, want FINANCE_APPROVAL", tc.company, detail.Summary.Stage)
		}
		found := false
		for _, item := range detail.WorkItems {
			if item.NodeID == "approve_finance" && !item.Status.Terminal() {
				found = true
				if item.OwnerRef != tc.approver {
					t.Fatalf("%s finance owner = %q, want %q", tc.company, item.OwnerRef, tc.approver)
				}
			}
		}
		if !found {
			t.Fatalf("%s has no open approve_finance work item: %+v", tc.company, detail.WorkItems)
		}
	}
}

func devPersonaContext(t *testing.T, verifier *trust.HMACVerifier, cfg ServeConfig, company, slot string, now time.Time) context.Context {
	t.Helper()
	for _, persona := range composeDevPersonas(verifier, cfg, func() time.Time { return now }) {
		if persona.Company != company || persona.Slot != slot {
			continue
		}
		principal, err := verifier.Verify(context.Background(), trust.Credential{Scheme: "Bearer", Token: persona.Token, Audience: cfg.Audience})
		if err != nil {
			t.Fatalf("verify %s %s: %v", company, slot, err)
		}
		return trust.WithPrincipal(context.Background(), principal)
	}
	t.Fatalf("missing %s %s persona", company, slot)
	return nil
}

// TestUXBLIND003PersonaCredentialsBindTheirPackWorker is the credential half
// of the binding: every quick pick is issued for its pack worker's key.
func TestUXBLIND003PersonaCredentialsBindTheirPackWorker(t *testing.T) {
	now := time.Date(2026, 9, 6, 16, 0, 0, 0, time.UTC)
	cfg := twoCompanyConfig()
	verifier := devDirectoryVerifier(t, cfg, now)
	byCompanySlot := map[string]workspace.DevPersona{}
	for _, persona := range composeDevPersonas(verifier, cfg, func() time.Time { return now }) {
		byCompanySlot[persona.Company+":"+persona.Slot] = persona
	}
	for _, pack := range []*demoworkforce.Pack{demoworkforce.HarborCarePack, demoworkforce.IronridgePack} {
		planned, err := pack.Plan(pgstore.TenantID(pack.Key))
		if err != nil {
			t.Fatalf("%s plan: %v", pack.Key, err)
		}
		byNumber := map[string]string{}
		for _, employee := range planned {
			byNumber[employee.Row.WorkerNumber] = employee.Row.WorkerKey
		}
		for _, spec := range pack.Personas {
			persona, found := byCompanySlot[pack.Key+":"+spec.ID]
			if !found {
				t.Fatalf("missing %s %s quick pick", pack.Key, spec.ID)
			}
			if persona.WorkerRef != byNumber[spec.WorkerNumber] || persona.WorkerRef == "" {
				t.Fatalf("%s %s binds %q, want worker for %s", pack.Key, spec.ID, persona.WorkerRef, spec.WorkerNumber)
			}
			principal, err := verifier.Verify(context.Background(), trust.Credential{Scheme: "Bearer", Token: persona.Token, Audience: cfg.Audience})
			if err != nil {
				t.Fatalf("verify %s %s: %v", pack.Key, spec.ID, err)
			}
			if principal.Subject() != persona.WorkerRef || string(principal.Tenant()) != pack.Key {
				t.Fatalf("%s %s principal = %q / %q", pack.Key, spec.ID, principal.Subject(), principal.Tenant())
			}
		}
	}
}

// uxblind003Cell composes and starts the two-company served cell with the
// local-dev workforce bootstrap and returns a gRPC connection, the quick
// picks and each pick's expected worker number.
func uxblind003Cell(t *testing.T) (*grpc.ClientConn, *pgxadapter.Pool, []workspace.DevPersona, map[string]string) {
	t.Helper()
	db := pgtest.New(t)
	pool, err := pgxadapter.NewPool(context.Background(), db.URL, map[string]string{"search_path": db.Schema})
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(pool.Close)
	cfg := twoCompanyExecutionConfig(db.URL)
	composed, err := ComposeServe(context.Background(), ServeInput{Config: cfg, Pool: pool, Identity: "uxblind-003-myself"})
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
	conn, err := grpc.NewClient(composed.GRPCAddr(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc client: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	verifier, err := trust.NewHMACVerifier(trust.HMACVerifierConfig{Key: []byte(cfg.DevHMACKey), Issuer: cfg.Issuer, Audience: cfg.Audience})
	if err != nil {
		t.Fatalf("verifier: %v", err)
	}
	personas := composeDevPersonas(verifier, cfg, time.Now)
	numbers := map[string]string{}
	for _, pack := range []*demoworkforce.Pack{demoworkforce.HarborCarePack, demoworkforce.IronridgePack} {
		for _, spec := range pack.Personas {
			numbers[pack.Key+":"+spec.ID] = spec.WorkerNumber
		}
	}
	if len(personas) != len(numbers) {
		t.Fatalf("quick picks = %d, want %d (four per served company)", len(personas), len(numbers))
	}
	return conn, pool, personas, numbers
}

// TestTodo_UXBLIND_003_Integration signs in every quick pick of both served
// demo companies against the real composed cell and asserts the directory
// read Myself binds from returns the persona's own worker record. The
// individual contributor (worker_self) and the finance partner were refused
// the read outright, and no durable worker matched its own principal.
func TestTodo_UXBLIND_003_Integration(t *testing.T) {
	conn, _, personas, numbers := uxblind003Cell(t)
	client := journeyv1.NewJourneyServiceClient(conn)
	for _, persona := range personas {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		rpc := metadata.AppendToOutgoingContext(ctx, transport.AuthorizationMetadataKey, "Bearer "+persona.Token)
		response, err := client.ListWorkers(rpc, &journeyv1.ListWorkersRequest{})
		cancel()
		if err != nil {
			t.Errorf("%s:%s ListWorkers: %v", persona.Company, persona.Slot, err)
			continue
		}
		own := 0
		for _, worker := range response.GetWorkers() {
			if worker.GetSubjectId() == persona.WorkerRef {
				own++
				if want := numbers[persona.Company+":"+persona.Slot]; worker.GetWorkerNumber() != want {
					t.Errorf("%s:%s own row number = %q, want %q", persona.Company, persona.Slot, worker.GetWorkerNumber(), want)
				}
			}
		}
		if own != 1 {
			t.Errorf("%s:%s listing holds %d rows for its own worker %s, want 1", persona.Company, persona.Slot, own, persona.WorkerRef)
		}
		if persona.Slot == "individual-contributor" && len(response.GetWorkers()) != 1 {
			t.Errorf("%s individual contributor listed %d workers, want only its own", persona.Company, len(response.GetWorkers()))
		}
	}
}

// TestTodo_UXBLIND_003_Browser renders Myself for every quick pick through
// the workspace's own product client (tools/uxqual/productclient, the code
// the browser runs) over the real gRPC transport, and asserts the page shows
// the persona's worker number instead of "Employee profile not connected".
func TestTodo_UXBLIND_003_Browser(t *testing.T) {
	conn, _, personas, numbers := uxblind003Cell(t)
	state, err := productclient.ParseState(productui.Path(productui.PageMyself), "")
	if err != nil {
		t.Fatalf("ParseState: %v", err)
	}
	for _, persona := range personas {
		rpcService := journeyclient.NewGRPCService(conn, persona.Token)
		service := productclient.Service{ListJourneys: rpcService.ListJourneys, ListWorkers: rpcService.ListWorkers}
		session := productclient.Session{Tenant: persona.Company, Principal: persona.WorkerRef, Roles: persona.Roles, EnforceRoleVisibility: true}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		view, loadErr := productclient.Load(ctx, service, session, state)
		cancel()
		if loadErr != nil {
			t.Errorf("%s:%s load Myself: %v", persona.Company, persona.Slot, loadErr)
			continue
		}
		markup, err := productui.Render(view)
		if err != nil {
			t.Fatalf("%s:%s render Myself: %v", persona.Company, persona.Slot, err)
		}
		if strings.Contains(markup, "Employee profile not connected") {
			t.Errorf("%s:%s Myself says the employee profile is not connected", persona.Company, persona.Slot)
			continue
		}
		if want := numbers[persona.Company+":"+persona.Slot]; !strings.Contains(markup, want) {
			t.Errorf("%s:%s Myself does not show the worker number %s", persona.Company, persona.Slot, want)
		}
	}
}

func TestTodo_UXBLIND_038(t *testing.T) {
	for _, pack := range []*demoworkforce.Pack{demoworkforce.HarborCarePack, demoworkforce.IronridgePack} {
		employees, err := pack.Plan(pgstore.TenantID(pack.Key))
		if err != nil {
			t.Fatalf("%s plan: %v", pack.Key, err)
		}
		if len(employees) == 0 || employees[0].Row.WorkerNumber == "" {
			t.Fatalf("%s has no seeded worker number", pack.Key)
		}
	}
}

func TestTodo_UXBLIND_038_Integration(t *testing.T) {
	db := pgtest.New(t)
	pool, err := pgxadapter.NewPool(context.Background(), db.URL, map[string]string{"search_path": db.Schema})
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(pool.Close)
	now := time.Date(2026, 9, 6, 16, 0, 0, 0, time.UTC)
	cfg := twoCompanyExecutionConfig(db.URL)
	composed, err := ComposeServe(context.Background(), ServeInput{Config: cfg, Pool: pool, Identity: "uxblind-lane-a-worker-ids", Options: Options{Now: func() time.Time { return now }}})
	if err != nil {
		t.Fatalf("ComposeServe: %v", err)
	}
	t.Cleanup(func() { _ = composed.Stop(context.Background()) })
	store := workeridstore.New(pool, tenantKeyMapper[values.TenantId](pgstore.TenantID))
	for _, pack := range []*demoworkforce.Pack{demoworkforce.HarborCarePack, demoworkforce.IronridgePack} {
		policy, err := store.Load(context.Background(), values.TenantId(pack.Key), pack.OrgScope())
		if err != nil {
			t.Fatalf("load %s policy: %v", pack.Key, err)
		}
		if policy.Prefix == "" || policy.SequenceDigits != 5 || policy.NextSequence <= policy.StartAt {
			t.Fatalf("%s policy = %+v", pack.Key, policy)
		}
	}
}

func twoCompanyExecutionConfig(databaseURL string) ServeConfig {
	return ServeConfig{
		Profile:    ServeProfileLocalDev,
		GRPCListen: "127.0.0.1:0", HTTPListen: "127.0.0.1:0", DatabaseURL: databaseURL,
		DevHMACKey: integrationSigningKey, PageCursorKey: integrationPageCursorKey,
		Issuer: DefaultIssuer, Audience: DefaultAudience, Tenant: demoworkforce.HarborCarePack.Key,
		Tenants: demoworkforce.HarborCarePack.Key + "," + demoworkforce.IronridgePack.Key,
		CellID:  "cell-uxblind-lane-a", MaxDeadline: 30 * time.Second, Migrate: false, Workspace: true,
		DevBrowserLogin: true, DevWorkforceBootstrap: true, OTelExporter: OTelExporterNone,
		ExecutionAuthority: true, ExecutionAuthorityDigest: "sha256:uxblind-lane-a", ExecutionAuthorityRole: "promotion_operator",
		ExecutionApprover:        demoworkforce.HarborCarePack.ExecutionApproverKey,
		ExecutionManagerApprover: demoworkforce.HarborCarePack.ManagerApproverKey,
		ExecutionFinancePartner:  demoworkforce.HarborCarePack.FinancePartnerKey,
		WorkflowPlan:             WorkflowPlanExecute, TimerTzdbVersion: DefaultTimerTzdbVersion, TimerCalendarVersion: DefaultTimerCalendarVersion,
	}
}
