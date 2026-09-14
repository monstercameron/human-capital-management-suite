package workflow_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/application"
	"github.com/monstercameron/human-capital-management-suite/internal/data/aggregates"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/positionfacts"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/fixtures"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/position"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const promoUXAt = "2026-09-03T12:00:00Z"

type promoUXPersona struct {
	Name    string
	Subject string
	Roles   []string
	Token   string
}

type promoUXServer struct {
	db          *pgtest.DB
	pool        *pgxadapter.Pool
	app         *application.App
	verifier    *trust.HMACVerifier
	client      journeyv1.JourneyServiceClient
	people      map[string]promoUXPersona
	positionRef string
}

// newPromoUXServer starts the same composed gRPC/HTTP cell as the serve
// command, over a private migrated PostgreSQL schema. The app owns its bound
// listeners and the scheduler, so the test exercises the real transport and
// wait/commit workload rather than calling an in-process engine.
func newPromoUXServer(t *testing.T) *promoUXServer {
	t.Helper()
	at, err := time.Parse(time.RFC3339, promoUXAt)
	if err != nil {
		t.Fatalf("parse promo UX clock: %v", err)
	}
	db := pgtest.New(t)
	pool, err := pgxadapter.NewPool(context.Background(), db.URL, map[string]string{"search_path": db.Schema})
	if err != nil {
		t.Fatalf("open promo UX pool: %v", err)
	}
	t.Cleanup(pool.Close)

	const signingKey = "hcm-next-promo-ux-regression-signing-key-32+"
	cfg := application.ServeConfig{
		GRPCListen: "127.0.0.1:0", HTTPListen: "127.0.0.1:0", DatabaseURL: db.URL,
		DevHMACKey: signingKey, Issuer: application.DefaultIssuer, Audience: application.DefaultAudience,
		Tenant: string(fixtures.Tenant), CellID: "cell-promo-ux-regression", MaxDeadline: 30 * time.Second,
		// The workflow contract drives the served journey RPCs. The separate
		// test/workspace promo_ux_* suite owns the optional rendered bundle,
		// whose generated asset manifest may be unavailable in a source checkout.
		Migrate: false, Workspace: false, OTelExporter: application.OTelExporterNone,
		ExecutionAuthority: true, ExecutionAuthorityDigest: "sha256:promo-ux-execution-authority",
		ExecutionAuthorityRole: "promotion_operator", ExecutionApprover: "principal:promo-ux-finance",
		ExecutionManagerApprover: "principal:promo-ux-manager",
		WorkflowPlan:             application.WorkflowPlanExecute, Scheduler: true,
		TimerTzdbVersion: application.DefaultTimerTzdbVersion, TimerCalendarVersion: application.DefaultTimerCalendarVersion,
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("validate promo UX config: %v", err)
	}
	staticVerifier := func(application.ServeConfig) (trust.Verifier, error) {
		return trust.NewHMACVerifier(trust.HMACVerifierConfig{
			Key: []byte(signingKey), Issuer: cfg.Issuer, Audience: cfg.Audience, Now: func() time.Time { return at },
		})
	}
	composed, err := application.ComposeServe(context.Background(), application.ServeInput{
		Config: cfg, Pool: pool, Identity: "promo-ux-regression", Options: application.Options{
			Now: func() time.Time { return at }, NewVerifier: staticVerifier,
		},
	})
	if err != nil {
		t.Fatalf("compose promo UX serve cell: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := composed.Stop(ctx); err != nil {
			t.Errorf("stop promo UX serve cell: %v", err)
		}
	})
	runCtx, stopRun := context.WithCancel(context.Background())
	// The scheduler is one of the real serve workloads. Cancel its lifecycle
	// context before App.Stop waits for workloads during test cleanup.
	t.Cleanup(stopRun)
	if err := composed.Start(runCtx); err != nil {
		t.Fatalf("start promo UX serve cell: %v", err)
	}
	positionRef := promoUXSeedTargetPosition(t, db)

	verifier, err := trust.NewHMACVerifier(trust.HMACVerifierConfig{
		Key: []byte(signingKey), Issuer: cfg.Issuer, Audience: cfg.Audience, Now: func() time.Time { return at },
	})
	if err != nil {
		t.Fatalf("build promo UX verifier: %v", err)
	}
	financePrincipal, err := promotionexec.FinanceApproverFor(cfg.ExecutionApprover)
	if err != nil {
		t.Fatalf("derive routed finance approver: %v", err)
	}
	people := map[string]promoUXPersona{
		"proposer": {Name: "proposer", Subject: "principal:promo-ux-proposer", Roles: []string{"intent_author", "comp_admin", "promotion_operator"}},
		"finance":  {Name: "finance", Subject: financePrincipal, Roles: []string{"payroll_manager"}},
		"manager":  {Name: "manager", Subject: "principal:promo-ux-manager", Roles: []string{"hiring_manager", "manager", "promotion_operator"}},
		"employee": {Name: "employee", Subject: "principal:promo-ux-employee", Roles: []string{"worker_self"}},
	}
	for name, person := range people {
		token, err := verifier.Issue(trust.Claims{
			Issuer: cfg.Issuer, Audience: cfg.Audience, Subject: person.Subject, SubjectKind: "human",
			Tenant: string(fixtures.Tenant), OrganizationScopeID: "org-north-america", Roles: person.Roles,
			Purposes: []string{"compensation_review"}, AuthenticationMethod: "bearer_token", Assurance: "substantial",
			SessionRef: "session:promo-ux-" + name, IssuedAtUnix: at.Add(-time.Minute).Unix(), ExpiresAtUnix: at.Add(time.Hour).Unix(),
		})
		if err != nil {
			t.Fatalf("issue %s credential: %v", name, err)
		}
		person.Token = token
		people[name] = person
	}
	conn, err := grpc.NewClient(composed.GRPCAddr(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial promo UX gRPC server: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	return &promoUXServer{db: db, pool: pool, app: composed, verifier: verifier,
		client: journeyv1.NewJourneyServiceClient(conn), people: people, positionRef: positionRef}
}

// promoUXSeedTargetPosition gives the served position reader a real, vacant
// target and returns the exact revision token a governed picker would issue.
// A guessed display code is intentionally not a valid proposal reference.
func promoUXSeedTargetPosition(t *testing.T, db *pgtest.DB) string {
	t.Helper()
	ctx := context.Background()
	tenantID := pgstore.TenantID(string(fixtures.Tenant))
	tx, err := db.Conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin target position seed: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		t.Fatalf("scope target position seed: %v", err)
	}
	store := aggregates.OrganizationStore{}
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	recorded := from.Add(time.Hour)
	legalEntityID, orgUnitID, jobID, positionID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	legalEntity, err := aggregates.NewLegalEntity(tenantID, legalEntityID, from, nil, recorded, "HarborCare US Inc.", "ACTIVE")
	if err != nil {
		t.Fatalf("new target legal entity: %v", err)
	}
	if _, err := store.PutLegalEntity(ctx, tx, legalEntity); err != nil {
		t.Fatalf("seed target legal entity: %v", err)
	}
	orgUnit, err := aggregates.NewOrganizationUnit(tenantID, orgUnitID, from, nil, recorded, "DEPARTMENT", "people-ops", "People Operations", &legalEntityID, nil, "ACTIVE")
	if err != nil {
		t.Fatalf("new target org unit: %v", err)
	}
	if _, err := store.PutOrganizationUnit(ctx, tx, orgUnit); err != nil {
		t.Fatalf("seed target org unit: %v", err)
	}
	job, err := aggregates.NewJob(tenantID, jobID, from, nil, recorded, "OPS-HRBP3", "Senior HR Business Partner", "PEOPLE", "P3", "EXEMPT")
	if err != nil {
		t.Fatalf("new target job: %v", err)
	}
	if _, err := store.PutJob(ctx, tx, job); err != nil {
		t.Fatalf("seed target job: %v", err)
	}
	jobPosition, err := aggregates.NewJobPosition(tenantID, positionID, jobID, orgUnitID, nil, from, nil, recorded, "POS-HRBP-301", "US East", "1.0000", "OPEN")
	if err != nil {
		t.Fatalf("new target position: %v", err)
	}
	if _, err := store.PutJobPosition(ctx, tx, jobPosition); err != nil {
		t.Fatalf("seed target position: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit target position seed: %v", err)
	}
	positionEntity := values.EntityRef{Tenant: fixtures.Tenant, Kind: position.KindPosition, Id: positionID.String()}
	effectiveOn, err := values.ParseLocalDate("2026-06-01")
	if err != nil {
		t.Fatalf("target position effective date: %v", err)
	}
	knownAt, err := values.NewKnownAt(values.NewInstant(time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC)))
	if err != nil {
		t.Fatalf("target position known-at: %v", err)
	}
	reader := positionfacts.Reader{DB: db.Conn, TenantUUID: func(values.TenantId) uuid.UUID { return tenantID }}
	revision, exists, err := reader.PositionRevisionAt(ctx, position.PositionQuery{
		Tenant: fixtures.Tenant, Position: positionEntity, AsOf: position.AsOf{EffectiveOn: effectiveOn, KnownAt: knownAt},
	})
	if err != nil || !exists {
		t.Fatalf("read seeded target position: exists=%t err=%v", exists, err)
	}
	ref, err := position.EncodeRevisionRef(positionEntity, revision.Revision)
	if err != nil {
		t.Fatalf("encode target position reference: %v", err)
	}
	return ref.String()
}

func (h *promoUXServer) ctx(t *testing.T, persona string, timeout time.Duration) (context.Context, context.CancelFunc) {
	t.Helper()
	p, ok := h.people[persona]
	if !ok {
		t.Fatalf("unknown promo UX persona %q", persona)
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	return metadata.AppendToOutgoingContext(ctx, transport.AuthorizationMetadataKey, "Bearer "+p.Token), cancel
}

func (h *promoUXServer) call(t *testing.T, persona string) (context.Context, context.CancelFunc) {
	return h.ctx(t, persona, 30*time.Second)
}

func promoUXStatusCode(err error) string {
	if err == nil {
		return "OK"
	}
	return status.Code(err).String()
}

func promoUXPosition(t *testing.T, workers *journeyv1.ListWorkersResponse) (*journeyv1.Worker, string, string) {
	t.Helper()
	var worker *journeyv1.Worker
	for _, candidate := range workers.GetWorkers() {
		if candidate.GetWorkerRef() == "omar-reyes" {
			worker = candidate
			break
		}
	}
	if worker == nil {
		t.Fatalf("ListWorkers did not discover eligible worker omar-reyes")
	}
	if worker.GetJobCode() == "" || worker.GetPositionId() == "" {
		t.Fatalf("eligible worker has no current placement: %+v", worker)
	}
	var targetJob, targetGrade string
	for _, path := range workers.GetOptions().GetPromotionPaths() {
		if path.GetSourceJobCode() == worker.GetJobCode() && path.GetSourceGrade() == worker.GetGrade() &&
			path.GetTargetJobCode() == "OPS-HRBP3" && path.GetTargetGrade() == "P3" {
			targetJob, targetGrade = path.GetTargetJobCode(), path.GetTargetGrade()
			break
		}
	}
	if targetJob == "" {
		t.Fatalf("no published promotion path from %s/%s to OPS-HRBP3/P3", worker.GetJobCode(), worker.GetGrade())
	}
	// The promotion path publishes the job/grade edge. Position IDs are
	// selected from the promotion form's exact governed target (the worker
	// creation picker intentionally publishes only createable positions).
	return worker, targetJob, targetGrade
}

func promoUXPropose(worker *journeyv1.Worker, targetJob, targetGrade, positionRef, clientID, expectedRevision string) *journeyv1.ProposePromotionRequest {
	return &journeyv1.ProposePromotionRequest{
		SubjectWorkerRef: worker.GetWorkerRef(), DesiredJobCode: targetJob, DesiredGrade: targetGrade,
		DesiredPositionId: positionRef, DesiredBasePay: "98000.00", DesiredPayCurrency: "USD",
		EffectiveDate: "2026-06-01", Reason: "promotion_into_senior_hrbp",
		ExpectedSubjectRevision: expectedRevision, ClientRequestId: clientID,
	}
}

func promoUXInspect(t *testing.T, h *promoUXServer, persona, intentID string) *journeyv1.JourneyDetail {
	t.Helper()
	ctx, cancel := h.call(t, persona)
	defer cancel()
	res, err := h.client.InspectJourney(ctx, &journeyv1.InspectJourneyRequest{IntentId: intentID})
	if err != nil {
		t.Fatalf("InspectJourney(%s): %v", intentID, err)
	}
	if res.GetDetail() == nil || res.GetDetail().GetJourney() == nil {
		t.Fatalf("InspectJourney(%s) returned no detail", intentID)
	}
	return res.GetDetail()
}

func promoUXWaitForStage(t *testing.T, h *promoUXServer, persona, intentID string, want journeyv1.JourneyStage) *journeyv1.JourneyDetail {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		detail := promoUXInspect(t, h, persona, intentID)
		if detail.GetJourney().GetStage() == want {
			return detail
		}
		time.Sleep(100 * time.Millisecond)
	}
	detail := promoUXInspect(t, h, persona, intentID)
	t.Fatalf("journey %s stayed at %s, want %s", intentID, detail.GetJourney().GetStage(), want)
	return nil
}

func promoUXCount(t *testing.T, h *promoUXServer, query string, args ...any) int {
	t.Helper()
	var count int
	if err := h.pool.QueryRow(context.Background(), query, args...).Scan(&count); err != nil {
		t.Fatalf("promo UX count query: %v", err)
	}
	return count
}
