package application

import (
	"context"
	"io/fs"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	commonv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/common/v1"
	timev1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/time/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/data/demoworkforce"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/timestore"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/bootstrap"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	transporttimeclock "github.com/monstercameron/human-capital-management-suite/internal/transport/timeclock"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

const uxblind123SigningKey = "uxblind123-time-clock-served-signing-key"

// uxblind123TimeMigrator is the time schema migration the command supplies,
// applied to the isolated schema exactly as the serve root asks for it.
func uxblind123TimeMigrator(ctx context.Context, databaseURL, coreURL, schema string, _ bootstrap.Logger) error {
	store, err := timestore.New(ctx, timestore.Config{DSN: databaseURL, CoreDSN: coreURL, Schema: schema, MaxConns: 4, MinConns: 1})
	if err != nil {
		return err
	}
	store.Close()
	config, err := pgx.ParseConfig(databaseURL)
	if err != nil {
		return err
	}
	config.RuntimeParams["search_path"] = schema
	db := stdlib.OpenDB(*config)
	defer func() { _ = db.Close() }()
	tree, err := fs.Sub(timestore.Migrations, "migrations")
	if err != nil {
		return err
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, db, tree, goose.WithDisableGlobalRegistry(true))
	if err != nil {
		return err
	}
	_, err = provider.Up(ctx)
	return err
}

type uxblind123Harness struct {
	pool     *pgxadapter.Pool
	client   timev1.WorkerClockServiceClient
	verifier *trust.HMACVerifier
	cfg      ServeConfig
	tokens   map[string]string
}

// uxblind123Compose composes the serve role the local-dev profile runs, for
// Ironridge, with or without the demo workforce (and so the clock).
func uxblind123Compose(t *testing.T, workforce bool) *uxblind123Harness {
	t.Helper()
	db := pgtest.New(t)
	pool, err := pgxadapter.NewPool(context.Background(), db.URL, map[string]string{"search_path": db.Schema, "application_name": "uxblind123-" + db.Schema})
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(pool.Close)
	cfg := ServeConfig{
		Profile: ServeProfileLocalDev, GRPCListen: "127.0.0.1:0", HTTPListen: "127.0.0.1:0", DatabaseURL: db.URL,
		DevHMACKey: uxblind123SigningKey, PageCursorKey: integrationPageCursorKey, Issuer: DefaultIssuer, Audience: DefaultAudience,
		Tenant: demoworkforce.IronridgeKey, CellID: "cell-uxblind123-clock", MaxDeadline: 60 * time.Second,
		Workspace: true, DevBrowserLogin: true, DevWorkforceBootstrap: workforce, OTelExporter: OTelExporterNone,
		ExecutionAuthority: true, ExecutionAuthorityDigest: "sha256:uxblind123-clock",
		ExecutionAuthorityRole: "promotion_operator", ExecutionApprover: "principal:promotion-approver",
		ExecutionFinancePartner: LocalDevFinancePartner,
		WorkflowPlan:            WorkflowPlanExecute, TimerTzdbVersion: DefaultTimerTzdbVersion,
		TimerCalendarVersion: DefaultTimerCalendarVersion,
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("configuration: %v", err)
	}
	composed, err := ComposeServe(context.Background(), ServeInput{Config: cfg, Pool: pool, Identity: "uxblind123-clock", Logger: uxblind123Logger{t}, Options: Options{
		MigrateTime: uxblind123TimeMigrator, ProviderReceipts: newFakeProviderReceipts(),
		// The clock is what is under test; the development HMAC verifier keeps
		// the credentials independent of the machine-client registry.
		NewVerifier: func(cfg ServeConfig) (trust.Verifier, error) {
			return trust.NewHMACVerifier(trust.HMACVerifierConfig{Key: []byte(cfg.DevHMACKey), Issuer: cfg.Issuer, Audience: cfg.Audience})
		},
	}})
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
	conn, err := grpc.NewClient(composed.GRPCAddr(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc client: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	h := &uxblind123Harness{pool: pool, client: timev1.NewWorkerClockServiceClient(conn), verifier: verifier, cfg: cfg, tokens: map[string]string{}}
	for _, persona := range composeDevEmployeePersonas(verifier, cfg, time.Now) {
		h.tokens[persona.ID] = persona.Token
	}
	return h
}

func (h *uxblind123Harness) as(t *testing.T, workerKey string) context.Context {
	t.Helper()
	token, ok := h.tokens[workspace.DevEmployeePersonaID(workerKey)]
	if !ok {
		t.Fatalf("no development credential for %s", workerKey)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	return metadata.AppendToOutgoingContext(ctx, transport.AuthorizationMetadataKey, "Bearer "+token)
}

func uxblind123Reason(t *testing.T, err error) string {
	t.Helper()
	st, ok := status.FromError(err)
	if !ok || st.Code() != codes.FailedPrecondition {
		t.Fatalf("error is not an ineligibility decision: %v", err)
	}
	for _, raw := range st.Details() {
		if detail, isDetail := raw.(*commonv1.ErrorDetail); isDetail && strings.HasPrefix(detail.GetReasonRef(), transporttimeclock.SelfClockReasonRefPrefix) {
			return strings.ToUpper(strings.TrimPrefix(detail.GetReasonRef(), transporttimeclock.SelfClockReasonRefPrefix))
		}
	}
	t.Fatalf("the refusal carries no reason reference: %v", err)
	return ""
}

func (h *uxblind123Harness) observations(t *testing.T, worker string) int {
	t.Helper()
	var count int
	err := h.pool.QueryRow(context.Background(), `SELECT count(*) FROM hcmnext_time.time_observation WHERE tenant_id = $1 AND worker_ref = $2`, demoworkforce.IronridgeKey, worker).Scan(&count)
	if err != nil {
		t.Fatalf("count observations: %v", err)
	}
	return count
}

// TestTodo_UXBLIND_123_Integration serves the time clock end to end on the
// composed cell: embedded PostgreSQL, the local-dev serve root, the seeded
// Ironridge workforce and time profiles, the real published clock workflow on
// the durable engine, and the workspace gRPC service the browser reads.
//
// An hourly field worker sees their state and clocks in and out; the owner is
// told the specific reason and nothing is recorded for them; a cell composed
// without the local demo workforce does not run the clock at all.
func TestTodo_UXBLIND_123_Integration(t *testing.T) {
	pack, ok := demoworkforce.PackFor(demoworkforce.IronridgeKey)
	if !ok {
		t.Fatal("the Ironridge demo pack is not registered")
	}
	employees, err := pack.Plan(pgstore.TenantID(demoworkforce.IronridgeKey))
	if err != nil {
		t.Fatal(err)
	}
	var hourly, owner string
	for _, employee := range employees {
		if owner == "" && employee.Row.PayBasis != demoworkforce.PayBasisHourly {
			owner = employee.Row.WorkerKey
		}
		if hourly == "" && employee.Row.PayBasis == demoworkforce.PayBasisHourly {
			hourly = employee.Row.WorkerKey
		}
	}
	if hourly == "" || owner == "" {
		t.Fatalf("the seed has no hourly worker (%q) or no salaried worker (%q)", hourly, owner)
	}

	h := uxblind123Compose(t, true)

	// The hourly worker starts clocked out, with a revision to act against.
	before, err := h.client.GetSelfClock(h.as(t, hourly), &timev1.GetSelfClockRequest{})
	if err != nil {
		t.Fatalf("GetSelfClock as %s: %v", hourly, err)
	}
	if before.GetStatusCode() != "CLOCKED_OUT" || before.GetStatusLabel() != "Clocked out" || before.GetRevision() == 0 || before.GetWorkerLabel() == "" || before.GetScheduleLabel() == "" {
		t.Fatalf("initial projection = %+v", before)
	}

	// Clock in: a genuine workflow receipt, and the projection moved.
	in, err := h.client.ExecuteSelfClockAction(h.as(t, hourly), &timev1.ExecuteSelfClockActionRequest{Action: timev1.ExecuteSelfClockActionRequest_ACTION_IN, ExpectedRevision: before.GetRevision(), IdempotencyKey: "uxblind123-in"})
	if err != nil {
		t.Fatalf("clock in: %v", err)
	}
	if _, err := uuid.Parse(in.GetReceiptId()); err != nil || in.GetWorkflowId() != "hcmnext.workflows.time.clock_in_out" || in.GetWorkflowNodeId() != "commit_punch" || in.GetWorkflowTraceId() == "" || in.GetPublishedPlanRef() == "" {
		t.Fatalf("clock-in receipt = %+v", in)
	}
	if in.GetStatus().GetStatusCode() != "CLOCKED_IN" || in.GetStatus().GetRevision() <= before.GetRevision() {
		t.Fatalf("clock-in status = %+v", in.GetStatus())
	}
	during, err := h.client.GetSelfClock(h.as(t, hourly), &timev1.GetSelfClockRequest{})
	if err != nil || during.GetStatusCode() != "CLOCKED_IN" || during.GetRevision() != in.GetStatus().GetRevision() {
		t.Fatalf("projection while clocked in = %+v err=%v", during, err)
	}
	// A stale revision cannot clock in a second time, and a retry of the same
	// request replays the first receipt instead of recording a second punch.
	if _, err := h.client.ExecuteSelfClockAction(h.as(t, hourly), &timev1.ExecuteSelfClockActionRequest{Action: timev1.ExecuteSelfClockActionRequest_ACTION_IN, ExpectedRevision: before.GetRevision(), IdempotencyKey: "uxblind123-in-again"}); err == nil {
		t.Fatal("a clocked-in worker clocked in again on a stale revision")
	}
	replay, err := h.client.ExecuteSelfClockAction(h.as(t, hourly), &timev1.ExecuteSelfClockActionRequest{Action: timev1.ExecuteSelfClockActionRequest_ACTION_IN, ExpectedRevision: before.GetRevision(), IdempotencyKey: "uxblind123-in"})
	if err != nil || replay.GetReceiptId() != in.GetReceiptId() {
		t.Fatalf("replay = %+v err=%v, want receipt %s", replay, err, in.GetReceiptId())
	}
	if got := h.observations(t, hourly); got != 1 {
		t.Fatalf("observations after a retried clock-in = %d, want 1", got)
	}

	// Clock out: the second workflow node, and the worker is out again.
	out, err := h.client.ExecuteSelfClockAction(h.as(t, hourly), &timev1.ExecuteSelfClockActionRequest{Action: timev1.ExecuteSelfClockActionRequest_ACTION_OUT, ExpectedRevision: during.GetRevision(), IdempotencyKey: "uxblind123-out"})
	if err != nil {
		t.Fatalf("clock out: %v", err)
	}
	if out.GetWorkflowNodeId() != "commit_clock_out" || out.GetWorkflowInstanceRef() != in.GetWorkflowInstanceRef() || out.GetStatus().GetStatusCode() != "CLOCKED_OUT" {
		t.Fatalf("clock-out receipt = %+v (clock-in instance %s)", out, in.GetWorkflowInstanceRef())
	}
	after, err := h.client.GetSelfClock(h.as(t, hourly), &timev1.GetSelfClockRequest{})
	if err != nil || after.GetStatusCode() != "CLOCKED_OUT" || after.GetRevision() <= during.GetRevision() {
		t.Fatalf("projection after clock out = %+v err=%v", after, err)
	}
	if got := h.observations(t, hourly); got != 2 {
		t.Fatalf("observations after clock in and out = %d, want 2", got)
	}

	// The salaried owner is told the specific reason, on read and on action,
	// and nothing is recorded for them.
	if _, err := h.client.GetSelfClock(h.as(t, owner), &timev1.GetSelfClockRequest{}); uxblind123Reason(t, err) != "EXEMPT" {
		t.Fatalf("owner read reason = %v", err)
	}
	_, err = h.client.ExecuteSelfClockAction(h.as(t, owner), &timev1.ExecuteSelfClockActionRequest{Action: timev1.ExecuteSelfClockActionRequest_ACTION_IN, ExpectedRevision: 1, IdempotencyKey: "uxblind123-owner"})
	if uxblind123Reason(t, err) != "EXEMPT" {
		t.Fatalf("owner action reason = %v", err)
	}
	if got := h.observations(t, owner); got != 0 {
		t.Fatalf("an exempt worker has %d recorded punches", got)
	}
	// The refusal never names anyone.
	if _, err := h.client.GetSelfClock(h.as(t, owner), &timev1.GetSelfClockRequest{}); strings.Contains(err.Error(), owner) {
		t.Fatalf("the refusal named the worker: %v", err)
	}

	// An unauthenticated caller is refused before any worker is resolved.
	if _, err := h.client.GetSelfClock(context.Background(), &timev1.GetSelfClockRequest{}); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("anonymous read = %v", err)
	}
}

// TestTodo_UXBLIND_123_IntegrationClockNotRunning proves a cell that does not
// compose the clock does not register the service, which is exactly what the
// browser reads as "not turned on for this workspace".
func TestTodo_UXBLIND_123_IntegrationClockNotRunning(t *testing.T) {
	h := uxblind123Compose(t, false)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, err := h.client.GetSelfClock(ctx, &timev1.GetSelfClockRequest{})
	if status.Code(err) != codes.Unimplemented {
		t.Fatalf("a cell with no clock answered %v, want Unimplemented", err)
	}
}

type uxblind123Logger struct{ t *testing.T }

func (l uxblind123Logger) Info(msg string, args ...any)  { l.t.Logf("INFO %s %v", msg, args) }
func (l uxblind123Logger) Error(msg string, args ...any) { l.t.Logf("ERROR %s %v", msg, args) }
