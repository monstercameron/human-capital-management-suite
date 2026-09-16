package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/data/evidencestore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/workflowversionstore"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/fixtures"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/version"
)

// servedEvidence reads tenant's durable evidence chronology from a served
// cell, failing the test when the cell was composed with anything but the
// durable store.
func servedEvidence(t *testing.T, cell *app.Cell, tenant values.TenantId) []evidencestore.Record {
	t.Helper()
	store, ok := cell.Evidence.(*evidencestore.Store)
	if !ok {
		t.Fatalf("the served cell records evidence on %T, want the durable store", cell.Evidence)
	}
	records, err := store.List(context.Background(), tenant)
	if err != nil {
		t.Fatalf("read the durable evidence chronology: %v", err)
	}
	return records
}

// TestServeNeverWiresAnInMemoryEvidenceSink is the WF-RUN-035 REFACTOR
// guard: the serve role's default composition records evidence on the
// durable PostgreSQL store, and the in-memory sink exists only as a test
// double a caller supplies explicitly. It fails the moment serve falls back
// to app.MemoryEvidenceSink again.
func TestServeNeverWiresAnInMemoryEvidenceSink(t *testing.T) {
	composed, _, _ := composeStub(t, stubServeConfig())
	component, ok := composed.Graph().Component(ComponentEvidenceSink)
	if !ok {
		t.Fatal("the serve graph records no evidence sink")
	}
	if component.Impl != "*evidencestore.Store" {
		t.Fatalf("serve wires %s as its evidence sink, want *evidencestore.Store", component.Impl)
	}
	if _, memory := composed.Cell().Evidence.(*app.MemoryEvidenceSink); memory {
		t.Fatal("serve wired the in-memory evidence sink into the cell")
	}
	// Without a database the default store refuses rather than degrading
	// to memory.
	if _, err := composed.Cell().Evidence.RecordInvocation(context.Background(), capability.InvocationEvidence{
		CapabilityID: "c", CapabilityVersion: 1, Tenant: "acme", Decision: "X", OccurredAt: time.Now(),
	}); !errors.Is(err, evidencestore.ErrInvalid) {
		t.Fatalf("recording on a pool-less serve composition = %v, want evidencestore.ErrInvalid", err)
	}

	// A test double is only ever an explicit choice.
	double := app.NewMemoryEvidenceSink()
	explicit, _, _ := composeStub(t, stubServeConfig(), WithEvidence(double))
	if explicit.Cell().Evidence != app.EvidenceStore(double) {
		t.Fatal("an explicitly supplied evidence double was not the one composed")
	}
}

// TestTodo_WF_RUN_035 proves the GREEN end to end over PostgreSQL: a served
// composition records capability decisions, gate decisions and execution
// evidence as tenant-scoped durable rows; a recomposed server (a restart)
// reads the same journey chronology back, replays a re-recorded decision onto
// the existing row instead of adding one, hides it from another tenant, and
// refuses to activate a quarantined workflow version without an authorized
// approval, which stays quarantined across a further restart.
func TestTodo_WF_RUN_035(t *testing.T) {
	ctx := context.Background()
	db := pgtest.New(t)
	pool, err := pgxadapter.NewPool(ctx, db.URL, map[string]string{"search_path": db.Schema})
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(pool.Close)
	tenant := fixtures.Tenant
	cfg := ServeConfig{
		GRPCListen: "127.0.0.1:0", HTTPListen: "127.0.0.1:0", DatabaseURL: db.URL,
		DevHMACKey: integrationSigningKey, Issuer: DefaultIssuer, Audience: DefaultAudience,
		Tenant: string(tenant), CellID: "cell-wfrun035", MaxDeadline: 30 * time.Second,
		Workspace: true, OTelExporter: OTelExporterNone,
		ExecutionAuthority: true, ExecutionAuthorityDigest: "sha256:wfrun035-authority",
		ExecutionAuthorityRole: "promotion_operator", ExecutionApprover: "principal:promotion-approver",
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("configuration: %v", err)
	}
	compose := func(identity string) *App {
		t.Helper()
		composed, err := ComposeServe(ctx, ServeInput{Config: cfg, Pool: pool, Identity: identity})
		if err != nil {
			t.Fatalf("ComposeServe(%s): %v", identity, err)
		}
		return composed
	}
	stop := func(composed *App) {
		t.Helper()
		stopCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		if err := composed.Stop(stopCtx); err != nil {
			t.Errorf("Stop: %v", err)
		}
	}
	rows := func() int {
		t.Helper()
		var n int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM capability_invocation_evidence`).Scan(&n); err != nil {
			t.Fatalf("count evidence rows: %v", err)
		}
		return n
	}

	first := compose("wfrun035-first")
	cell := first.Cell()
	if len(servedEvidence(t, cell, tenant)) != 0 {
		t.Fatal("a fresh schema already holds evidence")
	}
	// A governed capability read through the served gateway.
	read, err := cell.Gateway.Invoke(ctx, governedWorkerRead(t, cell))
	if err != nil || read.EvidenceID == "" {
		t.Fatalf("governed read = %+v, %v", read, err)
	}
	// The gate decision ExecuteIntent records and the driver's execution
	// evidence, on the same served sink.
	intentID := "intent-wfrun035"
	instanceID := "11111111-2222-3333-4444-555555555555"
	at := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
	gateID, err := cell.Evidence.RecordInvocation(ctx, capability.InvocationEvidence{
		CapabilityID: "workflow.execution_authority_gate", CapabilityVersion: 1, SubjectRef: intentID,
		Tenant: string(tenant), Decision: app.EvidenceKindGateAdmitted, OccurredAt: at,
	})
	if err != nil {
		t.Fatalf("record gate evidence: %v", err)
	}
	tenantID := pgstore.TenantID(string(tenant))
	terminalID, err := cell.Evidence.RecordExecutionEvidence(ctx, tenantID, "TERMINAL_WRITTEN", instanceID, "end", "event:1", "sha256:terminal", at.Add(time.Minute))
	if err != nil {
		t.Fatalf("record execution evidence: %v", err)
	}
	// A capability decision that names no tenant is refused, not guessed.
	untenanted := governedWorkerRead(t, cell)
	untenanted.Authorization.Tenant = ""
	if _, err := cell.Gateway.Invoke(ctx, untenanted); !errors.Is(err, evidencestore.ErrTenantRequired) {
		t.Fatalf("an invocation naming no tenant = %v, want evidencestore.ErrTenantRequired", err)
	}
	recorded := rows()
	if recorded != 3 {
		t.Fatalf("durable evidence rows = %d, want 3", recorded)
	}
	stop(first)

	restarted := compose("wfrun035-restarted")
	defer stop(restarted)
	rcell := restarted.Cell()
	journey, err := rcell.Evidence.JourneyEvidenceIDs(ctx, tenant, tenantID, intentID, instanceID)
	if err != nil {
		t.Fatalf("journey evidence after restart: %v", err)
	}
	if len(journey) != 2 || journey[0] != gateID || journey[1] != terminalID {
		t.Fatalf("journey evidence after restart = %v, want [%s %s]", journey, gateID, terminalID)
	}
	chronology := servedEvidence(t, rcell, tenant)
	if len(chronology) != 3 || chronology[0].EvidenceID != read.EvidenceID || chronology[0].Decision != "INVOKED" {
		t.Fatalf("restarted chronology = %+v, want the governed read first", chronology)
	}
	replayed, err := rcell.Evidence.RecordInvocation(ctx, capability.InvocationEvidence{
		CapabilityID: "workflow.execution_authority_gate", CapabilityVersion: 1, SubjectRef: intentID,
		Tenant: string(tenant), Decision: app.EvidenceKindGateAdmitted, OccurredAt: at,
	})
	if err != nil || replayed != gateID || rows() != recorded {
		t.Fatalf("replayed gate evidence = %q, %v with %d rows; want %s and %d rows", replayed, err, rows(), gateID, recorded)
	}
	if other, err := rcell.Evidence.JourneyEvidenceIDs(ctx, "tenant-wfrun035-other", pgstore.TenantID("tenant-wfrun035-other"), intentID, instanceID); err != nil || len(other) != 0 {
		t.Fatalf("another tenant's journey read = %v, %v; want nothing", other, err)
	}

	// Activation authority survives restart: a quarantined shipped version
	// is not reactivated without an authorized approval, by this server or
	// the next one.
	versions := workflowversionstore.Store{DB: pool}
	var digest string
	if err := pool.QueryRow(ctx, `SELECT compiled_plan_digest FROM workflow_compiled_version WHERE status = 'ACTIVE' ORDER BY workflow_id LIMIT 1`).Scan(&digest); err != nil {
		t.Fatalf("find an active shipped version: %v", err)
	}
	active, found, err := versions.GetByDigest(digest)
	if err != nil || !found {
		t.Fatalf("GetByDigest = %v, %v", found, err)
	}
	if _, err := version.Quarantine(versions, digest, "incident", "principal:incident-commander", "authority:incident",
		version.ActivationEvidence{ApprovedAt: active.PublishedAt}); err != nil {
		t.Fatalf("Quarantine: %v", err)
	}
	if _, err := version.Activate(versions, digest, version.ActivationEvidence{
		ApprovedBy: "principal:someone", ReviewedPlanDigest: digest, TestsPassed: true, ApprovedAt: active.PublishedAt,
	}); version.CodeOf(err) != version.CodeUnauthorizedActivation {
		t.Fatalf("unapproved activation = %v, want %s", err, version.CodeUnauthorizedActivation)
	}
	stop(restarted)
	again := compose("wfrun035-again")
	defer stop(again)
	if after, _, err := (workflowversionstore.Store{DB: pool}).GetByDigest(digest); err != nil || after.Status != version.StatusQuarantined {
		t.Fatalf("after another restart the version is %s (%v), want QUARANTINED", after.Status, err)
	}
}

// versionTestPool opens a pool on a fresh test schema, closed with the test.
func versionTestPool(t *testing.T) *pgxadapter.Pool {
	t.Helper()
	db := pgtest.New(t)
	pool, err := pgxadapter.NewPool(context.Background(), db.URL, map[string]string{"search_path": db.Schema})
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// TestTodo_WF_RUN_035_Recovery proves the served execution authority keeps
// its workflow version registry durably: the composed version store is the
// PostgreSQL registry, never the in-memory one; the shipped versions are
// ACTIVE on a recorded release approval that is not the publisher's; a
// recomposition (a restart) records no second approval or activation; and a
// version an operator quarantined stays quarantined across the restart.
func TestTodo_WF_RUN_035_Recovery(t *testing.T) {
	ctx := context.Background()
	pool := versionTestPool(t)
	cfg := executionServeConfig()
	compose := func() app.CellConfig {
		t.Helper()
		evidence := composeEvidenceStore(pool)
		cellConfig := app.CellConfig{Evidence: evidence}
		if err := ComposeExecutionAuthority(&cellConfig, pool, evidence, cfg); err != nil {
			t.Fatalf("ComposeExecutionAuthority: %v", err)
		}
		return cellConfig
	}
	counts := func() (approvals, transitions int) {
		t.Helper()
		if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM workflow_version_approval), (SELECT count(*) FROM workflow_version_transition)`).Scan(&approvals, &transitions); err != nil {
			t.Fatalf("count registry rows: %v", err)
		}
		return approvals, transitions
	}

	first := compose()
	store, ok := first.ExecutionVersions.(workflowversionstore.Store)
	if !ok {
		t.Fatalf("serve composed %T as the workflow version store, want the durable registry", first.ExecutionVersions)
	}
	var active []version.CompiledVersion
	rows, err := pool.Query(ctx, `SELECT compiled_plan_digest FROM workflow_compiled_version WHERE status = 'ACTIVE' ORDER BY workflow_id`)
	if err != nil {
		t.Fatalf("list active versions: %v", err)
	}
	var digests []string
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			t.Fatalf("scan: %v", err)
		}
		digests = append(digests, d)
	}
	rows.Close()
	for _, d := range digests {
		v, found, err := store.GetByDigest(d)
		if err != nil || !found {
			t.Fatalf("GetByDigest(%s) = %v, %v", d, found, err)
		}
		active = append(active, v)
	}
	if len(active) != 2 {
		t.Fatalf("ACTIVE shipped versions = %d, want the approval and execute promotion workflows", len(active))
	}
	for _, v := range active {
		if len(v.Approvals) != 1 || v.Approvals[0].ApprovedBy == v.PublishedBy {
			t.Fatalf("%s activation history = %+v, want one approval by someone other than %s", v.WorkflowID, v.Approvals, v.PublishedBy)
		}
	}
	approvals, transitions := counts()

	compose()
	if a, tr := counts(); a != approvals || tr != transitions {
		t.Fatalf("a restart recorded %d approvals and %d transitions, want %d and %d", a, tr, approvals, transitions)
	}

	quarantined := active[0]
	if _, err := version.Quarantine(store, quarantined.CompiledPlanDigest, "incident", "principal:incident-commander", "authority:incident",
		version.ActivationEvidence{ApprovedAt: quarantined.PublishedAt}); err != nil {
		t.Fatalf("Quarantine: %v", err)
	}
	restarted := compose()
	after, found, err := restarted.ExecutionVersions.GetByDigest(quarantined.CompiledPlanDigest)
	if err != nil || !found || after.Status != version.StatusQuarantined {
		t.Fatalf("after restart the quarantined version is %s (found %v, %v), want QUARANTINED", after.Status, found, err)
	}
}
