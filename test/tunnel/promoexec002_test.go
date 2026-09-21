package tunnel_test

// PROMO-EXEC-002: prove the promotion execute chain over the served tunnel
// surface. The PROMO-EXEC-001 served promotion fixture (a standard-profile
// ComposeServe cell with the execution engine on) is served through the real
// gRPC-over-WebSocket tunnel, and one tunnel session proposes a promotion,
// executes it, records both separated decisions, ticks the effective-date
// timer and reads the terminal outcome and ledger fact. Unauthorized callers
// are refused over the same surface.
//
// The composition here is the one cmd/hcmnext runs (application.ComposeServe
// over the standard profile, plus the shipped workflow versions activated the
// way hcmnext workflow-version bootstrap-dev does); the edge is the tunnel
// handler internal/transport/cell publishes. Only the scheduler tick runs
// in-process, exactly as the PROMO-EXEC-001 fixture ticks it.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoGRPCBridge/pkg/grpctunnel"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/application"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/workflowversionstore"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/fixtures"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/schedule"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/bootstrap"
	platformexecution "github.com/monstercameron/human-capital-management-suite/internal/platform/execution"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/execution/scheduler"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	transportcell "github.com/monstercameron/human-capital-management-suite/internal/transport/cell"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute/effects"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/lease"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/timer"
)

// promoexec002SigningKey never leaves this test and authenticates nothing
// outside a test process.
const promoexec002SigningKey = "promo-exec-002-tunnel-chain-signing-key"

// promoexec002Cell is one PROMO-EXEC-001-shaped served cell behind the real
// tunnel: the composed application, its tunnel edge, and the credentials the
// separated callers drive it with.
type promoexec002Cell struct {
	t        *testing.T
	pool     *pgxadapter.Pool
	composed *application.App
	server   *httptest.Server
	host     string
	now      time.Time
	cfg      application.ServeConfig
	verifier *trust.HMACVerifier
}

// promoexec002Compose builds the served fixture: the standard profile parsed
// with only secrets supplied (the PROMO-EXEC-001 default-composition claim),
// the shipped workflow versions activated, and the tunnel edge mounted over
// the composed cell exactly as cmd/hcmnext mounts it.
func promoexec002Compose(t *testing.T) *promoexec002Cell {
	t.Helper()
	db := pgtest.New(t)
	pool, err := pgxadapter.NewPool(context.Background(), db.URL, map[string]string{"search_path": db.Schema})
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(pool.Close)
	const tenant = string(fixtures.Tenant)
	const at = "2026-09-03T12:00:00Z"
	clockAt, _ := time.Parse(time.RFC3339, at)
	now := clockAt
	values, err := bootstrap.ParseConfig([]string{
		"-grpc-listen=127.0.0.1:0", "-http-listen=127.0.0.1:0", "-database-url=" + db.URL,
		"-dev-hmac-key=" + promoexec002SigningKey, "-tenant=" + tenant,
		"-execution-authority-digest=sha256:promo-exec-002-tunnel",
		"-migrate=false",
	}, func(string) (string, bool) { return "", false }, application.ServeConfigFields())
	if err != nil {
		t.Fatalf("ParseConfig(standard defaults): %v", err)
	}
	cfg, err := application.ServeConfigFromValues(values)
	if err != nil {
		t.Fatalf("ServeConfigFromValues: %v", err)
	}
	if !cfg.ExecutionAuthority || cfg.WorkflowPlan != application.WorkflowPlanExecute || !cfg.Scheduler {
		t.Fatalf("standard defaults = authority %t plan %q scheduler %t, want the engine on",
			cfg.ExecutionAuthority, cfg.WorkflowPlan, cfg.Scheduler)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("served tunnel configuration: %v", err)
	}
	composed, err := application.ComposeServe(context.Background(), application.ServeInput{
		Config: cfg, Pool: pool, Identity: "promo-exec-002-tunnel", Options: application.Options{Now: func() time.Time { return now }},
	})
	if err != nil {
		t.Fatalf("ComposeServe(standard defaults): %v", err)
	}
	if _, err := platformexecution.BootstrapDevVersions(context.Background(), workflowversionstore.Store{DB: pool}, now); err != nil {
		t.Fatalf("activate the shipped workflow versions: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_ = composed.Stop(ctx)
	})
	grpcServer, err := transportcell.NewGRPCServer(composed.Cell())
	if err != nil {
		t.Fatalf("NewGRPCServer over the served cell: %v", err)
	}
	t.Cleanup(grpcServer.Stop)
	handler, err := transportcell.NewEdgeHandlerWithTunnel(composed.Cell(), grpcServer)
	if err != nil {
		t.Fatalf("NewEdgeHandlerWithTunnel over the served cell: %v", err)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	verifier, err := trust.NewHMACVerifier(trust.HMACVerifierConfig{
		Key: []byte(promoexec002SigningKey), Issuer: cfg.Issuer, Audience: cfg.Audience, Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("build verifier: %v", err)
	}
	return &promoexec002Cell{t: t, pool: pool, composed: composed, server: server,
		host: strings.TrimPrefix(server.URL, "http://"), now: now, cfg: cfg, verifier: verifier}
}

// credential mints a bearer token for one separated caller on the cell clock.
func (c *promoexec002Cell) credential(subject string, roles []string) string {
	c.t.Helper()
	token, err := c.verifier.Issue(trust.Claims{
		Issuer: c.cfg.Issuer, Audience: c.cfg.Audience, Subject: subject, SubjectKind: "human", Tenant: c.cfg.Tenant,
		OrganizationScopeID: "org-north-america", Roles: roles,
		Purposes: []string{"compensation_review"}, AuthenticationMethod: "bearer_token", Assurance: "substantial",
		SessionRef: "session:" + subject, IssuedAtUnix: c.now.Add(-time.Minute).Unix(), ExpiresAtUnix: c.now.Add(48 * time.Hour).Unix(),
	})
	if err != nil {
		c.t.Fatalf("issue %s credential: %v", subject, err)
	}
	return token
}

// dial opens one tunnel session as the browser does: a websocket upgrade
// carrying the credential, then gRPC frames over the socket.
func (c *promoexec002Cell) dial(ctx context.Context, token string) *grpc.ClientConn {
	c.t.Helper()
	conn, err := grpctunnel.BuildTunnelConn(ctx, grpctunnel.TunnelConfig{
		Target:           "ws://" + c.host + transportcell.TunnelPath,
		Headers:          http.Header{"Authorization": []string{"Bearer " + token}},
		HandshakeTimeout: 10 * time.Second,
		GRPCOptions:      []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())},
	})
	if err != nil {
		c.t.Fatalf("BuildTunnelConn: %v", err)
	}
	c.t.Cleanup(func() { _ = conn.Close() })
	return conn
}

// callCtx carries the caller's own credential on the RPC, because the bridge
// never forwards the upgrade's Authorization into per-RPC metadata.
func (c *promoexec002Cell) callCtx(ctx context.Context, token string) context.Context {
	return metadata.AppendToOutgoingContext(ctx, transport.AuthorizationMetadataKey, "Bearer "+token)
}

// approvers derives the two principals the standard profile routes the
// corpus worker's approvals to, exactly as the served tests do.
func (c *promoexec002Cell) approvers() (finance, manager string) {
	c.t.Helper()
	finance = c.cfg.ExecutionFinancePartner
	if finance == "" {
		var err error
		finance, err = promotionexec.FinanceApproverFor(c.cfg.ExecutionApprover)
		if err != nil {
			c.t.Fatalf("FinanceApproverFor: %v", err)
		}
	}
	manager = c.cfg.ExecutionManagerApprover
	if manager == "" {
		var err error
		manager, err = promotionexec.ManagerApproverFor(c.cfg.ExecutionApprover)
		if err != nil {
			c.t.Fatalf("ManagerApproverFor: %v", err)
		}
	}
	return finance, manager
}

// tick fires due promotion timers through the production scheduler, the way
// the served fixture settles its effective-date wait.
func (c *promoexec002Cell) tick(ctx context.Context) {
	c.t.Helper()
	tenantID := pgstore.TenantID(c.cfg.Tenant)
	runner, err := scheduler.New(scheduler.Config{
		DB: c.pool, Claims: []lease.AcquireRequest{{TenantID: tenantID, Resource: lease.Resource{Kind: lease.ResourceQueue, ID: scheduler.DefaultQueueKey}, Holder: lease.Identity{WorkloadRef: "workload:promo-exec-002", InstanceRef: "promo-exec-002-tunnel"}}},
		Leases: lease.Manager{}, Timers: timer.Scheduler{Attempts: runtime.Store{}}, Misfire: schedule.MisfireConfig{Policy: schedule.MisfireCatchUpOnce, Grace: time.Hour, MaxCatchUp: 1},
		Dispatcher: scheduler.DispatcherFunc(func(dispatchCtx context.Context, work scheduler.Work) (scheduler.Disposition, error) {
			_, resumeErr := c.composed.Cell().ResumeFiredTimer(app.WithResumeTenant(dispatchCtx, c.cfg.Tenant), work.Row.InstanceID.String(), work.Row.NodeID, work.Row.Attempt)
			if resumeErr != nil {
				return scheduler.DispositionRetry, resumeErr
			}
			return scheduler.DispositionCompleted, nil
		}), Clock: func() time.Time { return c.now },
	})
	if err != nil {
		c.t.Fatalf("scheduler.New: %v", err)
	}
	tick, err := runner.Tick(ctx)
	if err != nil || tick.Fired != 1 {
		c.t.Fatalf("scheduler.Tick = %+v, %v; want one fired timer", tick, err)
	}
}

// TestTodo_PROMO_EXEC_002_TunnelChain drives the whole promotion execute
// chain through the served tunnel: one session proposes, executes, records
// both separated decisions, and after the timer reads the terminal outcome
// and its ledger fact. Every stage transition is read back over the tunnel.
func TestTodo_PROMO_EXEC_002_TunnelChain(t *testing.T) {
	c := promoexec002Compose(t)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	operator := c.credential("principal:promo-exec-002-operator", []string{"intent_author", "comp_admin", "promotion_operator"})
	financeSubject, managerSubject := c.approvers()
	finance := c.credential(financeSubject, []string{"comp_admin"})
	manager := c.credential(managerSubject, []string{"comp_admin"})

	client := journeyv1.NewJourneyServiceClient(c.dial(ctx, operator))

	proposed, err := client.ProposeJourney(c.callCtx(ctx, operator), &journeyv1.ProposeJourneyRequest{
		WorkerRef: "omar-reyes", Target: &journeyv1.Placement{JobCode: "OPS-HRBP3", Grade: "P3"},
		ProposedBase: "98000.00", EffectiveDate: "2026-06-01", BusinessReason: "Promotion into the senior HRBP role",
	})
	if err != nil {
		t.Fatalf("ProposeJourney over the tunnel: %v", err)
	}
	if got := proposed.GetJourney().GetStage(); got != journeyv1.JourneyStage_JOURNEY_STAGE_PROPOSED {
		t.Fatalf("proposed stage over the tunnel = %s, want PROPOSED", got)
	}
	id := proposed.GetJourney().GetIntentId()
	if id == "" {
		t.Fatal("the tunnel proposal names no intent")
	}

	executed, err := client.ExecuteJourney(c.callCtx(ctx, operator), &journeyv1.ExecuteJourneyRequest{IntentId: id})
	if err != nil {
		t.Fatalf("ExecuteJourney over the tunnel: %v", err)
	}
	if got := executed.GetDetail().GetJourney().GetStage(); got != journeyv1.JourneyStage_JOURNEY_STAGE_FINANCE_APPROVAL {
		t.Fatalf("executed stage over the tunnel = %s, want FINANCE_APPROVAL", got)
	}

	c.now = c.now.Add(10 * time.Minute)
	decided, err := client.DecideJourney(c.callCtx(ctx, finance), &journeyv1.DecideJourneyRequest{IntentId: id, Approve: true, Reason: "finance approved"})
	if err != nil {
		t.Fatalf("DecideJourney(finance) over the tunnel: %v", err)
	}
	if got := decided.GetDetail().GetJourney().GetStage(); got != journeyv1.JourneyStage_JOURNEY_STAGE_MANAGER_APPROVAL {
		t.Fatalf("after finance stage over the tunnel = %s, want MANAGER_APPROVAL", got)
	}

	c.now = c.now.Add(10 * time.Minute)
	waiting, err := client.DecideJourney(c.callCtx(ctx, manager), &journeyv1.DecideJourneyRequest{IntentId: id, Approve: true, Reason: "manager approved"})
	if err != nil {
		t.Fatalf("DecideJourney(manager) over the tunnel: %v", err)
	}
	if got := waiting.GetDetail().GetJourney().GetStage(); got != journeyv1.JourneyStage_JOURNEY_STAGE_WAITING_EFFECTIVE_DATE {
		t.Fatalf("after manager stage over the tunnel = %s, want WAITING_EFFECTIVE_DATE", got)
	}
	if waiting.GetDetail().GetInstance() == nil {
		t.Fatal("the waiting journey names no workflow instance over the tunnel")
	}
	instanceID := waiting.GetDetail().GetInstance().GetInstanceId()

	c.now = c.now.Add(10 * time.Minute)
	c.tick(ctx)

	completed, err := client.InspectJourney(c.callCtx(ctx, operator), &journeyv1.InspectJourneyRequest{IntentId: id})
	if err != nil {
		t.Fatalf("InspectJourney(terminal) over the tunnel: %v", err)
	}
	// The corpus worker has no projection, so the served chain closes
	// BLOCKED with its one ledger fact -- the same terminal the native
	// PROMO-EXEC-001 fixture reads, now read over the tunnel.
	if got := completed.GetDetail().GetJourney().GetStage(); got != journeyv1.JourneyStage_JOURNEY_STAGE_BLOCKED {
		t.Fatalf("terminal stage over the tunnel = %s, want BLOCKED", got)
	}
	if completed.GetDetail().GetLedger() == nil {
		t.Fatal("the terminal journey carries no ledger fact over the tunnel")
	}
	tenantID := pgstore.TenantID(c.cfg.Tenant)
	var ledgerCount int
	if err := c.pool.QueryRow(ctx, `SELECT count(*) FROM ledger_event WHERE tenant_id = $1 AND stream_key = $2`,
		tenantID, effects.StreamKeyFor(completed.GetDetail().GetInstance().GetWorkflowId(), instanceID)).Scan(&ledgerCount); err != nil {
		t.Fatalf("count journey ledger facts: %v", err)
	}
	if ledgerCount != 1 {
		t.Fatalf("ledger facts on %s = %d, want exactly one", instanceID, ledgerCount)
	}
}

// TestTodo_PROMO_EXEC_002_TunnelChain_Security proves the tunnel chain
// refuses unauthorized callers over the same surface: a call with no
// credential is unauthenticated, and an authenticated caller without the
// journey role is denied before any write.
func TestTodo_PROMO_EXEC_002_TunnelChain_Security(t *testing.T) {
	c := promoexec002Compose(t)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	if got := c.upgradeStatus(nil); got != http.StatusForbidden {
		t.Fatalf("an unauthenticated tunnel upgrade got %d, want 403", got)
	}

	operator := c.credential("principal:promo-exec-002-operator", []string{"intent_author", "comp_admin", "promotion_operator"})
	client := journeyv1.NewJourneyServiceClient(c.dial(ctx, operator))
	if _, err := client.ProposeJourney(ctx, &journeyv1.ProposeJourneyRequest{
		WorkerRef: "omar-reyes", Target: &journeyv1.Placement{JobCode: "OPS-HRBP3", Grade: "P3"},
		ProposedBase: "98000.00", EffectiveDate: "2026-06-01", BusinessReason: "no credential",
	}); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("ProposeJourney with no credential = %v, want UNAUTHENTICATED", err)
	}

	employee := c.credential("principal:promo-exec-002-employee", []string{"employee"})
	if _, err := client.ProposeJourney(c.callCtx(ctx, employee), &journeyv1.ProposeJourneyRequest{
		WorkerRef: "omar-reyes", Target: &journeyv1.Placement{JobCode: "OPS-HRBP3", Grade: "P3"},
		ProposedBase: "98000.00", EffectiveDate: "2026-06-01", BusinessReason: "self promotion",
	}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("ProposeJourney as the employee = %v, want PERMISSION_DENIED", err)
	}
	var intents int
	if err := c.pool.QueryRow(ctx, `SELECT count(*) FROM intent_instance WHERE tenant_id = $1`, pgstore.TenantID(c.cfg.Tenant)).Scan(&intents); err != nil {
		t.Fatalf("count intents after refused proposals: %v", err)
	}
	if intents != 0 {
		t.Fatalf("refused tunnel proposals wrote %d intents, want none", intents)
	}
}

// upgradeStatus performs one raw upgrade request against the served edge.
func (c *promoexec002Cell) upgradeStatus(decorate func(*http.Request)) int {
	c.t.Helper()
	req, err := http.NewRequest(http.MethodGet, c.server.URL+transportcell.TunnelPath, nil)
	if err != nil {
		c.t.Fatalf("build the upgrade request: %v", err)
	}
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Sec-WebSocket-Version", "13")
	req.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
	if decorate != nil {
		decorate(req)
	}
	res, err := c.server.Client().Do(req)
	if err != nil {
		c.t.Fatalf("GET %s: %v", transportcell.TunnelPath, err)
	}
	defer func() { _ = res.Body.Close() }()
	return res.StatusCode
}

// TestTodo_PROMO_EXEC_002_TunnelChain_Integration proves the tunnel chain
// writes through to the real store: a proposal executed over the tunnel owns
// a proposal revision and a routed finance work item, and the tunnel inspect
// reads the same stage the rows imply.
func TestTodo_PROMO_EXEC_002_TunnelChain_Integration(t *testing.T) {
	c := promoexec002Compose(t)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	operator := c.credential("principal:promo-exec-002-operator", []string{"intent_author", "comp_admin", "promotion_operator"})
	client := journeyv1.NewJourneyServiceClient(c.dial(ctx, operator))
	proposed, err := client.ProposeJourney(c.callCtx(ctx, operator), &journeyv1.ProposeJourneyRequest{
		WorkerRef: "omar-reyes", Target: &journeyv1.Placement{JobCode: "OPS-HRBP3", Grade: "P3"},
		ProposedBase: "98000.00", EffectiveDate: "2026-06-01", BusinessReason: "Promotion into the senior HRBP role",
	})
	if err != nil {
		t.Fatalf("ProposeJourney over the tunnel: %v", err)
	}
	id := proposed.GetJourney().GetIntentId()
	executed, err := client.ExecuteJourney(c.callCtx(ctx, operator), &journeyv1.ExecuteJourneyRequest{IntentId: id})
	if err != nil {
		t.Fatalf("ExecuteJourney over the tunnel: %v", err)
	}
	if got := executed.GetDetail().GetJourney().GetStage(); got != journeyv1.JourneyStage_JOURNEY_STAGE_FINANCE_APPROVAL {
		t.Fatalf("executed stage over the tunnel = %s, want FINANCE_APPROVAL", got)
	}
	tenantID := pgstore.TenantID(c.cfg.Tenant)
	var revisions int
	if err := c.pool.QueryRow(ctx, `SELECT count(*) FROM proposal_revision WHERE tenant_id = $1 AND intent_id = $2`, tenantID, id).Scan(&revisions); err != nil {
		t.Fatalf("count proposal revisions: %v", err)
	}
	if revisions == 0 {
		t.Fatal("the tunnel proposal wrote no proposal revision")
	}
	financeSubject, _ := c.approvers()
	var owner, itemStatus string
	if err := c.pool.QueryRow(ctx, `SELECT owner_ref, status FROM work_item WHERE tenant_id = $1 AND node_id = $2 ORDER BY created_at DESC LIMIT 1`,
		tenantID, promotionexec.NodeApproveFinance).Scan(&owner, &itemStatus); err != nil {
		t.Fatalf("read the finance work item: %v", err)
	}
	if owner != financeSubject || itemStatus != "ASSIGNED" {
		t.Fatalf("finance work item = %s/%s, want ASSIGNED to %s", owner, itemStatus, financeSubject)
	}
	inspected, err := client.InspectJourney(c.callCtx(ctx, operator), &journeyv1.InspectJourneyRequest{IntentId: id})
	if err != nil {
		t.Fatalf("InspectJourney over the tunnel: %v", err)
	}
	if got := inspected.GetDetail().GetJourney().GetStage(); got != journeyv1.JourneyStage_JOURNEY_STAGE_FINANCE_APPROVAL {
		t.Fatalf("inspected stage over the tunnel = %s, want the FINANCE_APPROVAL the rows imply", got)
	}
}
