package admin_test

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/timestamppb"

	adminv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/admin/v1"
	commonv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/common/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/operator"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/operations/authorizedhealth"
	"github.com/monstercameron/human-capital-management-suite/internal/operations/productcorrelation"
	"github.com/monstercameron/human-capital-management-suite/internal/operations/repairworkbench"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/telemetry"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/admin"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/grpcserver"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// rev087Now pins every REV-087-01 observation instant, so wire digests are
// reproducible and comparable against direct package calls.
var rev087Now = time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)

// rev087FakeSubmitter is a hermetic repairworkbench.Submitter: it records
// every forwarded request and answers with a canned receipt or a canned
// refusal, so the tests prove what the RPC forwarded without running a
// real operator gateway.
type rev087FakeSubmitter struct {
	calls   []operator.Request
	receipt operator.Receipt
	err     error
}

func (f *rev087FakeSubmitter) Submit(_ context.Context, req operator.Request) (operator.Receipt, error) {
	f.calls = append(f.calls, req)
	if f.err != nil {
		return operator.Receipt{}, f.err
	}
	return f.receipt, nil
}

func rev087CannedReceipt() operator.Receipt {
	return operator.Receipt{
		IntentInstanceID: "intent:operator:rev087-outbox-9",
		IntentType:       "hcmnext.operator.connector_redrive",
		Kind:             operator.KindConnectorRedrive,
		Tenant:           values.TenantId(fixtureTenant),
		Operator:         fixtureOperatorSubject,
		Scope:            operator.Scope{Resource: "outbox/cell-a", IDs: []string{"outbox/cell-a/msg-9"}},
		IdempotencyKey:   "idem-rev087-1",
		RequestDigest:    "sha256:rev087-request",
		Reason:           "redrive the stuck outbox message",
		TicketRef:        "OPS-1",
		AuthorityKind:    operator.AuthorityJIT,
		AuthorityRef:     "grant-rev087-1",
		Outcome:          operator.OutcomeApplied,
		EffectRef:        "effect-rev087-1",
	}
}

func startOperationsServer(t *testing.T, deps admin.Dependencies) (*grpc.ClientConn, func()) {
	t.Helper()
	cfg := transport.Config{Verifier: fakeVerifier{}}
	srv := grpc.NewServer(grpc.ChainUnaryInterceptor(grpcserver.UnaryInterceptor(cfg)))
	admin.Register(srv, deps)
	admin.RegisterOperations(srv, deps)

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() { _ = srv.Serve(lis) }()

	conn, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		srv.Stop()
		t.Fatalf("dial: %v", err)
	}
	return conn, func() {
		_ = conn.Close()
		srv.Stop()
		_ = lis.Close()
	}
}

func rev087OperatorDeps() (admin.Dependencies, *rev087FakeSubmitter) {
	fake := &rev087FakeSubmitter{receipt: rev087CannedReceipt()}
	return admin.Dependencies{
		Now:             func() time.Time { return rev087Now },
		RepairSubmitter: fake,
	}, fake
}

func rev087Client(t *testing.T, deps admin.Dependencies) (adminv1.OperationsServiceClient, func()) {
	t.Helper()
	conn, cleanup := startOperationsServer(t, deps)
	return adminv1.NewOperationsServiceClient(conn), cleanup
}

func rev087Scope(tenant string) *commonv1.ScopeContext {
	return &commonv1.ScopeContext{TenantId: tenant}
}

// rev087Principal mints the same operator principal fakeVerifier admits,
// for the integration test's direct package calls.
func rev087Principal(t *testing.T) *trust.Principal {
	t.Helper()
	now := time.Now()
	principal, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant:               fixtureTenant,
		Subject:              fixtureOperatorSubject,
		SubjectKind:          trust.SubjectKindHuman,
		Roles:                []string{admin.OperatorRole},
		AuthenticationMethod: trust.AuthenticationMethodBearerToken,
		Assurance:            trust.AssuranceHigh,
		SessionRef:           "session-rev087",
		Purposes:             []string{"operator_diagnostics"},
		IssuedAt:             now.Add(-time.Minute),
		ExpiresAt:            now.Add(time.Hour),
		CredentialDigest:     "digest:" + fixtureOperatorToken,
	})
	if err != nil {
		t.Fatalf("principal: %v", err)
	}
	return principal
}

func rev087HealthRequest(tenant string) *adminv1.GetOperationsHealthRequest {
	at := timestamppb.New(rev087Now)
	return &adminv1.GetOperationsHealthRequest{
		Scope:      rev087Scope(tenant),
		ObservedAt: at,
		Projections: []*adminv1.ProjectionObservationInput{
			{
				Name: "projection/ledger-stream", SchemaVersion: "v1", DefinitionVersion: "ledger/3",
				SourceSequence: 42, AppliedSequence: 42,
				ObservedAt: at, MaxAgeSeconds: 300, Status: "CURRENT",
			},
		},
		Workflows: []*adminv1.WorkflowObservationInput{
			{
				Name: "workflow/onboarding", SchemaVersion: "v1", DefinitionVersion: "wf/7",
				ActiveRuns: 2, FailedRuns: 0, BlockedRuns: 0, PendingWorkItems: 1, OverdueWorkItems: 0,
				ObservedAt: at, MaxAgeSeconds: 300, Status: "HEALTHY",
			},
		},
		Outbox: &adminv1.OutboxObservationInput{
			SchemaVersion: "v1", Pending: 0, InFlight: 1, Failed: 0, Abandoned: 0,
			ObservedAt: at, MaxAgeSeconds: 300, MaxPendingAgeSeconds: 600, Status: "HEALTHY",
		},
		Reconciliation: &adminv1.ReconciliationObservationInput{
			SchemaVersion: "v1", Pending: 1, Observing: 0, Mismatch: 0, RepairRequired: 0,
			ObservedAt: at, MaxAgeSeconds: 300, Status: "HEALTHY",
		},
	}
}

// rev087Findings returns two in-slice findings plus one foreign-tenant and
// one foreign-slice finding, all validly shaped: Open must show exactly
// the two in-slice rows and omit the others without a count.
func rev087Findings() []*adminv1.RepairFindingInput {
	at := timestamppb.New(rev087Now)
	return []*adminv1.RepairFindingInput{
		{
			Id: "finding-proj", Slice: "ledger", Tenant: fixtureTenant,
			Kind: string(repairworkbench.KindProjectionStale), Severity: string(repairworkbench.SeverityMajor),
			Resource: "projection/ledger-stream", TargetRefs: []string{"projection/ledger-stream#42"},
			EvidenceRef: "evt-ledger-42", ObservedAt: at,
		},
		{
			Id: "finding-outbox", Slice: "ledger", Tenant: fixtureTenant,
			Kind: string(repairworkbench.KindOutboxStuck), Severity: string(repairworkbench.SeverityCritical),
			Resource: "outbox/cell-a", TargetRefs: []string{"outbox/cell-a/msg-9"},
			EvidenceRef: "evt-outbox-9", ObservedAt: at,
		},
		{
			Id: "finding-foreign", Slice: "ledger", Tenant: "other-tenant",
			Kind: string(repairworkbench.KindOutboxStuck), Severity: string(repairworkbench.SeverityCritical),
			Resource: "outbox/cell-b", TargetRefs: []string{"outbox/cell-b/msg-1"},
			EvidenceRef: "evt-foreign-1", ObservedAt: at,
		},
		{
			Id: "finding-payroll", Slice: "payroll", Tenant: fixtureTenant,
			Kind: string(repairworkbench.KindWorkflowStuck), Severity: string(repairworkbench.SeverityMinor),
			Resource: "workflow/payroll-run", TargetRefs: []string{"workflow/payroll-run/node-3"},
			EvidenceRef: "evt-payroll-3", ObservedAt: at,
		},
	}
}

func rev087Events() []*adminv1.TelemetryEventInput {
	at := func(offset time.Duration) *timestamppb.Timestamp {
		return timestamppb.New(rev087Now.Add(offset))
	}
	return []*adminv1.TelemetryEventInput{
		{
			Surface: "UI", Name: "promotion.started", Tenant: fixtureTenant,
			CorrelationId: "corr-1", TraceId: "trace-1", At: at(0),
			Attributes: map[string]string{"operation": "promote_worker", "outcome": "started"},
		},
		{
			Surface: "INTENT", Name: "intent.accepted", Tenant: fixtureTenant,
			CorrelationId: "corr-1", TraceId: "trace-1", At: at(time.Second),
			Attributes: map[string]string{"operation": "promote_worker"},
		},
		{
			Surface: "WORKFLOW", Name: "node.completed", Tenant: fixtureTenant,
			CorrelationId: "corr-1", TraceId: "trace-1", At: at(2 * time.Second),
			Attributes: map[string]string{"workflow_node": "approve"},
		},
		{
			Surface: "LEDGER", Name: "effect.recorded", Tenant: fixtureTenant,
			CorrelationId: "corr-1", TraceId: "trace-1", At: at(3 * time.Second),
			Attributes: map[string]string{"operation": "promote_worker"},
		},
		{
			Surface: "UI", Name: "note.added", Tenant: fixtureTenant,
			CorrelationId: "corr-2", TraceId: "trace-2", At: at(4 * time.Second),
			Attributes: map[string]string{"mystery": "unregistered-key", "outcome": "nurse@example.com"},
		},
		{
			Surface: "UI", Name: "promotion.started", Tenant: "other-tenant",
			CorrelationId: "corr-3", TraceId: "trace-3", At: at(5 * time.Second),
			Attributes: map[string]string{"operation": "promote_worker"},
		},
		{
			Surface: "WORKFLOW", Name: "node.completed", Tenant: fixtureTenant,
			CorrelationId: "", TraceId: "trace-4", At: at(6 * time.Second),
			Attributes: map[string]string{"workflow_node": "approve"},
		},
	}
}

func rev087FindingDigest(t *testing.T, client adminv1.OperationsServiceClient, ctx context.Context, findings []*adminv1.RepairFindingInput, id string) string {
	t.Helper()
	view, err := client.OpenRepairWorkbench(ctx, &adminv1.OpenRepairWorkbenchRequest{
		Scope: rev087Scope(fixtureTenant), Slice: "ledger", Findings: findings,
	})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	for _, item := range view.GetItems() {
		if item.GetFindingId() == id {
			return item.GetEvidenceDigest()
		}
	}
	t.Fatalf("finding %q missing from workbench: %+v", id, view.GetItems())
	return ""
}

func rev087SubmitRequest(findings []*adminv1.RepairFindingInput, findingID, digest string) *adminv1.SubmitRepairActionRequest {
	return &adminv1.SubmitRepairActionRequest{
		Scope: rev087Scope(fixtureTenant), Slice: "ledger", Findings: findings,
		FindingId: findingID, EvidenceDigest: digest,
		Kind:           string(operator.KindConnectorRedrive),
		IdempotencyKey: "idem-rev087-1",
		Reason:         "redrive the stuck outbox message",
		TicketRef:      "OPS-1",
	}
}

// TestTodo_REV_087_01 is the REV-087-01 primary: an authorized operator
// drives the whole diagnostics surface through the OperationsService RPCs
// alone — health projection, repair workbench, one governed repair submit
// and telemetry correlation — and every response carries its digest and
// evidence. The test holds only the gRPC client: nothing reaches into the
// operations packages except through the RPC.
func TestTodo_REV_087_01(t *testing.T) {
	deps, fake := rev087OperatorDeps()
	client, cleanup := rev087Client(t, deps)
	defer cleanup()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	opCtx := withToken(ctx, fixtureOperatorToken)

	health, err := client.GetOperationsHealth(opCtx, rev087HealthRequest(fixtureTenant))
	if err != nil {
		t.Fatalf("health: %v", err)
	}
	if health.GetState() != string(authorizedhealth.StateHealthy) {
		t.Fatalf("health state = %q, want HEALTHY", health.GetState())
	}
	if len(health.GetProjections()) != 1 || health.GetProjections()[0].GetLag() != 0 ||
		health.GetProjections()[0].GetFreshness() != string(authorizedhealth.FreshnessCurrent) {
		t.Fatalf("projection health = %+v", health.GetProjections())
	}
	if len(health.GetWorkflows()) != 1 || health.GetWorkflows()[0].GetPendingWorkItems() != 1 {
		t.Fatalf("workflow health = %+v", health.GetWorkflows())
	}
	if health.GetOutbox().GetInFlight() != 1 || health.GetReconciliation().GetPending() != 1 {
		t.Fatalf("outbox/reconciliation health = %+v / %+v", health.GetOutbox(), health.GetReconciliation())
	}
	if health.GetDigest() == "" {
		t.Fatal("health response carries no digest")
	}
	if got := health.GetEvidenceRef(); got.GetEvidenceId() == "" || got.GetEvidenceKind() != "admin.operations.get_operations_health" {
		t.Fatalf("health evidence = %+v", got)
	}

	findings := rev087Findings()
	view, err := client.OpenRepairWorkbench(opCtx, &adminv1.OpenRepairWorkbenchRequest{
		Scope: rev087Scope(fixtureTenant), Slice: "ledger", Findings: findings,
	})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	items := view.GetItems()
	if len(items) != 2 || view.GetTruncated() {
		t.Fatalf("workbench shows %d items (truncated=%v), want exactly the 2 in-slice findings", len(items), view.GetTruncated())
	}
	if items[0].GetFindingId() != "finding-outbox" || items[1].GetFindingId() != "finding-proj" {
		t.Fatalf("workbench order = %q, %q; want the CRITICAL finding first", items[0].GetFindingId(), items[1].GetFindingId())
	}
	if len(items[0].GetActions()) != 1 || items[0].GetActions()[0] != string(operator.KindConnectorRedrive) {
		t.Fatalf("outbox-stuck actions = %v, want exactly [CONNECTOR_REDRIVE]", items[0].GetActions())
	}
	if len(items[1].GetActions()) != 1 || items[1].GetActions()[0] != string(operator.KindProjectionRebuild) {
		t.Fatalf("projection-stale actions = %v, want exactly [PROJECTION_REBUILD]", items[1].GetActions())
	}
	if items[0].GetEvidenceDigest() == "" || items[0].GetTargetCount() != 1 {
		t.Fatalf("workbench item = %+v", items[0])
	}
	if view.GetDigest() == "" {
		t.Fatal("workbench response carries no digest")
	}
	if got := view.GetEvidenceRef(); got.GetEvidenceId() == "" || got.GetEvidenceKind() != "admin.operations.open_repair_workbench" {
		t.Fatalf("workbench evidence = %+v", got)
	}

	submitted, err := client.SubmitRepairAction(opCtx, rev087SubmitRequest(findings, "finding-outbox", items[0].GetEvidenceDigest()))
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	want := rev087CannedReceipt()
	if submitted.GetIntentInstanceId() != want.IntentInstanceID || submitted.GetIntentType() != want.IntentType ||
		submitted.GetKind() != string(want.Kind) || submitted.GetTenant() != string(want.Tenant) ||
		submitted.GetOperator() != want.Operator || submitted.GetScopeResource() != want.Scope.Resource ||
		len(submitted.GetScopeIds()) != 1 || submitted.GetScopeIds()[0] != want.Scope.IDs[0] ||
		submitted.GetIdempotencyKey() != want.IdempotencyKey || submitted.GetRequestDigest() != want.RequestDigest ||
		submitted.GetReason() != want.Reason || submitted.GetTicketRef() != want.TicketRef ||
		submitted.GetAuthorityKind() != want.AuthorityKind || submitted.GetAuthorityRef() != want.AuthorityRef ||
		submitted.GetOutcome() != string(want.Outcome) || submitted.GetEffectRef() != want.EffectRef {
		t.Fatalf("submit response does not echo the gateway receipt: %+v", submitted)
	}
	if got := submitted.GetEvidenceRef(); got.GetEvidenceId() == "" || got.GetEvidenceKind() != "admin.operations.submit_repair_action" {
		t.Fatalf("submit evidence = %+v", got)
	}
	if len(fake.calls) != 1 {
		t.Fatalf("gateway received %d submissions, want exactly 1", len(fake.calls))
	}
	forwarded := fake.calls[0]
	if forwarded.Kind != operator.KindConnectorRedrive || forwarded.Tenant != values.TenantId(fixtureTenant) ||
		forwarded.Operator != fixtureOperatorSubject || forwarded.Scope.Resource != "outbox/cell-a" ||
		len(forwarded.Scope.IDs) != 1 || forwarded.Scope.IDs[0] != "outbox/cell-a/msg-9" ||
		forwarded.IdempotencyKey != "idem-rev087-1" || forwarded.Reason != "redrive the stuck outbox message" ||
		forwarded.TicketRef != "OPS-1" || forwarded.ExpectedVersion != "evt-outbox-9" ||
		forwarded.PayloadDigest != items[0].GetEvidenceDigest() {
		t.Fatalf("gateway received a rewritten request: %+v", forwarded)
	}

	report, err := client.CorrelateProductTelemetry(opCtx, &adminv1.CorrelateProductTelemetryRequest{
		Scope: rev087Scope(fixtureTenant), Events: rev087Events(),
	})
	if err != nil {
		t.Fatalf("correlate: %v", err)
	}
	timelines := report.GetTimelines()
	if len(timelines) != 2 || timelines[0].GetCorrelationId() != "corr-1" || timelines[1].GetCorrelationId() != "corr-2" {
		t.Fatalf("timelines = %+v, want exactly corr-1 then corr-2", timelines)
	}
	first := timelines[0]
	if !first.GetComplete() || len(first.GetSteps()) != 4 {
		t.Fatalf("corr-1 = %+v, want a complete 4-step timeline", first)
	}
	for i, wantSurface := range []string{"UI", "INTENT", "WORKFLOW", "LEDGER"} {
		if first.GetSteps()[i].GetSurface() != wantSurface {
			t.Fatalf("corr-1 step %d surface = %q, want %q", i, first.GetSteps()[i].GetSurface(), wantSurface)
		}
	}
	if first.GetSteps()[0].GetAttributes()["operation"] != "promote_worker" {
		t.Fatalf("corr-1 first step attributes = %v", first.GetSteps()[0].GetAttributes())
	}
	dropped := report.GetDropped()
	if dropped[productcorrelation.DropUnknownKey] != 1 || dropped[productcorrelation.DropPayloadShape] != 1 ||
		dropped[productcorrelation.DropForeignTenant] != 1 || dropped[productcorrelation.DropUncorrelated] != 1 {
		t.Fatalf("dropped = %v, want one of each drop reason", dropped)
	}
	raw, err := protojson.Marshal(report)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(raw), "nurse@example.com") || strings.Contains(string(raw), "unregistered-key") {
		t.Fatalf("dropped values leaked onto the wire: %s", raw)
	}
	if report.GetDigest() == "" {
		t.Fatal("correlation response carries no digest")
	}
	if got := report.GetEvidenceRef(); got.GetEvidenceId() == "" || got.GetEvidenceKind() != "admin.operations.correlate_product_telemetry" {
		t.Fatalf("correlation evidence = %+v", got)
	}
}

// TestTodo_REV_087_01_Fault proves malformed, unadmitted, stale and
// unknown repair work fails with owned codes, that a refused submit never
// reaches the gateway, and that the healthy path still works afterwards.
func TestTodo_REV_087_01_Fault(t *testing.T) {
	deps, fake := rev087OperatorDeps()
	client, cleanup := rev087Client(t, deps)
	defer cleanup()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	opCtx := withToken(ctx, fixtureOperatorToken)

	rewound := rev087HealthRequest(fixtureTenant)
	rewound.Projections[0].AppliedSequence = rewound.Projections[0].SourceSequence + 1
	_, err := client.GetOperationsHealth(opCtx, rewound)
	assertOwnedCode(t, err, envelope.CodeInvalidArgument)

	negative := rev087HealthRequest(fixtureTenant)
	negative.Workflows[0].FailedRuns = -1
	_, err = client.GetOperationsHealth(opCtx, negative)
	assertOwnedCode(t, err, envelope.CodeInvalidArgument)

	findings := rev087Findings()
	_, err = client.OpenRepairWorkbench(opCtx, &adminv1.OpenRepairWorkbenchRequest{
		Scope: rev087Scope(fixtureTenant), Slice: "  ", Findings: findings,
	})
	assertOwnedCode(t, err, envelope.CodeInvalidArgument)

	broken := rev087Findings()
	broken[0].Id = ""
	_, err = client.OpenRepairWorkbench(opCtx, &adminv1.OpenRepairWorkbenchRequest{
		Scope: rev087Scope(fixtureTenant), Slice: "ledger", Findings: broken,
	})
	assertOwnedCode(t, err, envelope.CodeInvalidArgument)

	digest := rev087FindingDigest(t, client, opCtx, findings, "finding-outbox")

	wrongKind := rev087SubmitRequest(findings, "finding-outbox", digest)
	wrongKind.Kind = string(operator.KindProjectionRebuild)
	_, err = client.SubmitRepairAction(opCtx, wrongKind)
	assertOwnedCode(t, err, envelope.CodeFailedPrecondition)

	stale := rev087SubmitRequest(findings, "finding-outbox", "sha256:stale-evidence")
	_, err = client.SubmitRepairAction(opCtx, stale)
	assertOwnedCode(t, err, envelope.CodeAborted)

	_, err = client.SubmitRepairAction(opCtx, rev087SubmitRequest(findings, "finding-nope", digest))
	assertOwnedCode(t, err, envelope.CodeNotFound)

	if len(fake.calls) != 0 {
		t.Fatalf("refused submits reached the gateway %d times, want zero effect", len(fake.calls))
	}

	fake.err = &operator.Error{Code: operator.CodeAuthorityRequired, Kind: operator.KindConnectorRedrive, Detail: "no JIT grant"}
	_, err = client.SubmitRepairAction(opCtx, rev087SubmitRequest(findings, "finding-outbox", digest))
	assertOwnedCode(t, err, envelope.CodeFailedPrecondition)
	if len(fake.calls) != 1 {
		t.Fatalf("gateway received %d submissions, want exactly the refused-forwarded one", len(fake.calls))
	}
	fake.err = nil

	events := rev087Events()
	events[0].Surface = "PAGER"
	_, err = client.CorrelateProductTelemetry(opCtx, &adminv1.CorrelateProductTelemetryRequest{
		Scope: rev087Scope(fixtureTenant), Events: events,
	})
	assertOwnedCode(t, err, envelope.CodeInvalidArgument)

	// The healthy path still works after every fault above.
	healthy, err := client.SubmitRepairAction(opCtx, rev087SubmitRequest(findings, "finding-outbox", digest))
	if err != nil {
		t.Fatalf("healthy submit after faults: %v", err)
	}
	if healthy.GetOutcome() != string(operator.OutcomeApplied) {
		t.Fatalf("healthy submit outcome = %q", healthy.GetOutcome())
	}
	if len(fake.calls) != 2 {
		t.Fatalf("gateway received %d submissions, want 2", len(fake.calls))
	}
}

// TestTodo_REV_087_01_Unconfigured proves SubmitRepairAction is honestly
// unavailable until the composition root supplies a gateway submitter,
// while the read-only RPCs keep working on the same server.
func TestTodo_REV_087_01_Unconfigured(t *testing.T) {
	deps, _ := rev087OperatorDeps()
	deps.RepairSubmitter = nil
	client, cleanup := rev087Client(t, deps)
	defer cleanup()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	opCtx := withToken(ctx, fixtureOperatorToken)

	findings := rev087Findings()
	digest := rev087FindingDigest(t, client, opCtx, findings, "finding-outbox")
	_, err := client.SubmitRepairAction(opCtx, rev087SubmitRequest(findings, "finding-outbox", digest))
	assertOwnedCode(t, err, envelope.CodeUnavailable)

	if _, err := client.GetOperationsHealth(opCtx, rev087HealthRequest(fixtureTenant)); err != nil {
		t.Fatalf("health on an unconfigured-submit server: %v", err)
	}
	if _, err := client.CorrelateProductTelemetry(opCtx, &adminv1.CorrelateProductTelemetryRequest{
		Scope: rev087Scope(fixtureTenant), Events: rev087Events(),
	}); err != nil {
		t.Fatalf("correlate on an unconfigured-submit server: %v", err)
	}
}

// TestTodo_REV_087_01_Security proves every OperationsService method
// refuses before any package runs unless the caller carries the operator
// role for the request tenant, and that the service publishes exactly its
// four declared methods.
func TestTodo_REV_087_01_Security(t *testing.T) {
	deps, fake := rev087OperatorDeps()
	client, cleanup := rev087Client(t, deps)
	defer cleanup()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	opCtx := withToken(ctx, fixtureOperatorToken)
	ordinaryCtx := withToken(ctx, fixtureOrdinaryToken)

	findings := rev087Findings()
	digest := rev087FindingDigest(t, client, opCtx, findings, "finding-outbox")
	calls := []struct {
		name string
		call func(context.Context) error
	}{
		{"GetOperationsHealth", func(c context.Context) error {
			_, err := client.GetOperationsHealth(c, rev087HealthRequest(fixtureTenant))
			return err
		}},
		{"OpenRepairWorkbench", func(c context.Context) error {
			_, err := client.OpenRepairWorkbench(c, &adminv1.OpenRepairWorkbenchRequest{
				Scope: rev087Scope(fixtureTenant), Slice: "ledger", Findings: findings,
			})
			return err
		}},
		{"SubmitRepairAction", func(c context.Context) error {
			_, err := client.SubmitRepairAction(c, rev087SubmitRequest(findings, "finding-outbox", digest))
			return err
		}},
		{"CorrelateProductTelemetry", func(c context.Context) error {
			_, err := client.CorrelateProductTelemetry(c, &adminv1.CorrelateProductTelemetryRequest{
				Scope: rev087Scope(fixtureTenant), Events: rev087Events(),
			})
			return err
		}},
	}

	for _, tc := range calls {
		t.Run("ordinary role refused on "+tc.name, func(t *testing.T) {
			owned := assertOwnedCode(t, tc.call(ordinaryCtx), envelope.CodePermissionDenied)
			if owned.EvidenceRef().ID == "" || owned.CorrelationID() == "" {
				t.Fatal("a permission-denied result must carry evidence and correlation")
			}
		})
	}

	t.Run("no credential is rejected before any capability runs", func(t *testing.T) {
		_, err := client.GetOperationsHealth(ctx, rev087HealthRequest(fixtureTenant))
		assertOwnedCode(t, err, envelope.CodeUnauthenticated)
	})

	t.Run("an unrecognized credential is rejected the same way a missing one is", func(t *testing.T) {
		_, err := client.OpenRepairWorkbench(withToken(ctx, fixtureUnknownToken), &adminv1.OpenRepairWorkbenchRequest{
			Scope: rev087Scope(fixtureTenant), Slice: "ledger", Findings: findings,
		})
		assertOwnedCode(t, err, envelope.CodeUnauthenticated)
	})

	// A scope naming another tenant is refused by the shared admission
	// boundary (transport.ApplyTrustedContext), before any handler runs:
	// the tenant a handler sees always comes from the verified
	// principal, never from the scope message.
	for _, tc := range calls {
		t.Run("foreign scope tenant refused on "+tc.name, func(t *testing.T) {
			foreign := rev087HealthRequest("other-tenant")
			var err error
			switch tc.name {
			case "GetOperationsHealth":
				_, err = client.GetOperationsHealth(opCtx, foreign)
			case "OpenRepairWorkbench":
				_, err = client.OpenRepairWorkbench(opCtx, &adminv1.OpenRepairWorkbenchRequest{
					Scope: rev087Scope("other-tenant"), Slice: "ledger", Findings: findings,
				})
			case "SubmitRepairAction":
				req := rev087SubmitRequest(findings, "finding-outbox", digest)
				req.Scope = rev087Scope("other-tenant")
				_, err = client.SubmitRepairAction(opCtx, req)
			case "CorrelateProductTelemetry":
				_, err = client.CorrelateProductTelemetry(opCtx, &adminv1.CorrelateProductTelemetryRequest{
					Scope: rev087Scope("other-tenant"), Events: rev087Events(),
				})
			}
			owned := assertOwnedCode(t, err, envelope.CodeInvalidArgument)
			found := false
			for _, v := range owned.Violations() {
				if strings.HasPrefix(v.FieldPath, "scope.tenant_id") {
					found = true
				}
			}
			if !found {
				t.Fatalf("violations = %+v, want one naming scope.tenant_id", owned.Violations())
			}
		})
	}

	t.Run("a caller-selected trusted-context header is rejected on the new service too", func(t *testing.T) {
		reserved := metadata.AppendToOutgoingContext(opCtx, "x-hcm-roles", admin.OperatorRole)
		_, err := client.GetOperationsHealth(reserved, rev087HealthRequest(fixtureTenant))
		assertOwnedCode(t, err, envelope.CodeInvalidArgument)
	})

	if len(fake.calls) != 0 {
		t.Fatalf("denied submits reached the gateway %d times, want zero", len(fake.calls))
	}

	t.Run("OperationsService publishes only its four declared methods", func(t *testing.T) {
		desc := adminv1.OperationsService_ServiceDesc
		want := map[string]bool{
			"GetOperationsHealth": true, "OpenRepairWorkbench": true,
			"SubmitRepairAction": true, "CorrelateProductTelemetry": true,
		}
		if len(desc.Methods) != len(want) {
			t.Fatalf("service publishes %d methods, want exactly %d: %v", len(desc.Methods), len(want), methodNames(desc))
		}
		for _, m := range desc.Methods {
			if !want[m.MethodName] {
				t.Fatalf("unexpected operator method %s is published", m.MethodName)
			}
		}
	})
}

// rev087DirectHealthRequest rebuilds the primary health fixture as the
// package struct, independently of the RPC's own conversion.
func rev087DirectHealthRequest() authorizedhealth.Request {
	return authorizedhealth.Request{
		Tenant: values.TenantId(fixtureTenant),
		Now:    rev087Now,
		Projections: []authorizedhealth.ProjectionObservation{
			{
				Name: "projection/ledger-stream", SchemaVersion: "v1", DefinitionVersion: "ledger/3",
				SourceSequence: 42, AppliedSequence: 42,
				ObservedAt: rev087Now, MaxAge: 300 * time.Second, Status: "CURRENT",
			},
		},
		Workflows: []authorizedhealth.WorkflowObservation{
			{
				Name: "workflow/onboarding", SchemaVersion: "v1", DefinitionVersion: "wf/7",
				ActiveRuns: 2, FailedRuns: 0, BlockedRuns: 0, PendingWorkItems: 1, OverdueWorkItems: 0,
				ObservedAt: rev087Now, MaxAge: 300 * time.Second, Status: "HEALTHY",
			},
		},
		Outbox: &authorizedhealth.OutboxObservation{
			SchemaVersion: "v1", Pending: 0, InFlight: 1, Failed: 0, Abandoned: 0,
			ObservedAt: rev087Now, MaxAge: 300 * time.Second, MaxPendingAge: 600 * time.Second, Status: "HEALTHY",
		},
		Reconciliation: &authorizedhealth.ReconciliationObservation{
			SchemaVersion: "v1", Pending: 1, Observing: 0, Mismatch: 0, RepairRequired: 0,
			ObservedAt: rev087Now, MaxAge: 300 * time.Second, Status: "HEALTHY",
		},
	}
}

// rev087DirectFindings rebuilds the primary findings fixture as package
// structs, independently of the RPC's own conversion.
func rev087DirectFindings() []repairworkbench.Finding {
	at := rev087Now
	return []repairworkbench.Finding{
		{
			ID: "finding-proj", Slice: "ledger", Tenant: values.TenantId(fixtureTenant),
			Kind: repairworkbench.KindProjectionStale, Severity: repairworkbench.SeverityMajor,
			Resource: "projection/ledger-stream", TargetRefs: []string{"projection/ledger-stream#42"},
			EvidenceRef: "evt-ledger-42", ObservedAt: at,
		},
		{
			ID: "finding-outbox", Slice: "ledger", Tenant: values.TenantId(fixtureTenant),
			Kind: repairworkbench.KindOutboxStuck, Severity: repairworkbench.SeverityCritical,
			Resource: "outbox/cell-a", TargetRefs: []string{"outbox/cell-a/msg-9"},
			EvidenceRef: "evt-outbox-9", ObservedAt: at,
		},
		{
			ID: "finding-foreign", Slice: "ledger", Tenant: values.TenantId("other-tenant"),
			Kind: repairworkbench.KindOutboxStuck, Severity: repairworkbench.SeverityCritical,
			Resource: "outbox/cell-b", TargetRefs: []string{"outbox/cell-b/msg-1"},
			EvidenceRef: "evt-foreign-1", ObservedAt: at,
		},
		{
			ID: "finding-payroll", Slice: "payroll", Tenant: values.TenantId(fixtureTenant),
			Kind: repairworkbench.KindWorkflowStuck, Severity: repairworkbench.SeverityMinor,
			Resource: "workflow/payroll-run", TargetRefs: []string{"workflow/payroll-run/node-3"},
			EvidenceRef: "evt-payroll-3", ObservedAt: at,
		},
	}
}

// rev087DirectEvents rebuilds the primary telemetry fixture as package
// structs, independently of the RPC's own conversion.
func rev087DirectEvents() []productcorrelation.Event {
	at := func(offset time.Duration) time.Time { return rev087Now.Add(offset) }
	tenant := values.TenantId(fixtureTenant)
	return []productcorrelation.Event{
		{Surface: productcorrelation.SurfaceUI, Name: "promotion.started", Tenant: tenant,
			CorrelationID: "corr-1", TraceID: "trace-1", At: at(0),
			Attributes: map[string]string{"operation": "promote_worker", "outcome": "started"}},
		{Surface: productcorrelation.SurfaceIntent, Name: "intent.accepted", Tenant: tenant,
			CorrelationID: "corr-1", TraceID: "trace-1", At: at(time.Second),
			Attributes: map[string]string{"operation": "promote_worker"}},
		{Surface: productcorrelation.SurfaceWorkflow, Name: "node.completed", Tenant: tenant,
			CorrelationID: "corr-1", TraceID: "trace-1", At: at(2 * time.Second),
			Attributes: map[string]string{"workflow_node": "approve"}},
		{Surface: productcorrelation.SurfaceLedger, Name: "effect.recorded", Tenant: tenant,
			CorrelationID: "corr-1", TraceID: "trace-1", At: at(3 * time.Second),
			Attributes: map[string]string{"operation": "promote_worker"}},
		{Surface: productcorrelation.SurfaceUI, Name: "note.added", Tenant: tenant,
			CorrelationID: "corr-2", TraceID: "trace-2", At: at(4 * time.Second),
			Attributes: map[string]string{"mystery": "unregistered-key", "outcome": "nurse@example.com"}},
		{Surface: productcorrelation.SurfaceUI, Name: "promotion.started", Tenant: values.TenantId("other-tenant"),
			CorrelationID: "corr-3", TraceID: "trace-3", At: at(5 * time.Second),
			Attributes: map[string]string{"operation": "promote_worker"}},
		{Surface: productcorrelation.SurfaceWorkflow, Name: "node.completed", Tenant: tenant,
			CorrelationID: "", TraceID: "trace-4", At: at(6 * time.Second),
			Attributes: map[string]string{"workflow_node": "approve"}},
	}
}

// TestTodo_REV_087_01_Integration wrings the RPCs through the exact
// packages they wrap: independently converted inputs run through the
// package functions directly, and the RPC responses must carry the same
// digests, so a swapped or skipped package call fails the suite.
func TestTodo_REV_087_01_Integration(t *testing.T) {
	deps, fake := rev087OperatorDeps()
	client, cleanup := rev087Client(t, deps)
	defer cleanup()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	opCtx := withToken(ctx, fixtureOperatorToken)
	principal := rev087Principal(t)

	health, err := client.GetOperationsHealth(opCtx, rev087HealthRequest(fixtureTenant))
	if err != nil {
		t.Fatalf("health: %v", err)
	}
	directHealth, err := authorizedhealth.Project(principal, rev087DirectHealthRequest())
	if err != nil {
		t.Fatalf("direct project: %v", err)
	}
	if health.GetDigest() != directHealth.Digest() || health.GetState() != string(directHealth.State) {
		t.Fatalf("RPC health digest/state = %q/%q, direct = %q/%q",
			health.GetDigest(), health.GetState(), directHealth.Digest(), directHealth.State)
	}

	findings := rev087Findings()
	view, err := client.OpenRepairWorkbench(opCtx, &adminv1.OpenRepairWorkbenchRequest{
		Scope: rev087Scope(fixtureTenant), Slice: "ledger", Findings: findings,
	})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	directView, err := repairworkbench.Open(principal, "ledger", rev087DirectFindings())
	if err != nil {
		t.Fatalf("direct open: %v", err)
	}
	if view.GetDigest() != directView.Digest() || len(view.GetItems()) != len(directView.Items) {
		t.Fatalf("RPC workbench digest/items = %q/%d, direct = %q/%d",
			view.GetDigest(), len(view.GetItems()), directView.Digest(), len(directView.Items))
	}

	var directFinding repairworkbench.Finding
	for _, f := range rev087DirectFindings() {
		if f.ID == "finding-outbox" {
			directFinding = f
		}
	}
	if _, err := client.SubmitRepairAction(opCtx, rev087SubmitRequest(findings, "finding-outbox", directFinding.EvidenceDigest())); err != nil {
		t.Fatalf("submit: %v", err)
	}
	if len(fake.calls) != 1 {
		t.Fatalf("gateway received %d submissions, want 1", len(fake.calls))
	}
	forwarded := fake.calls[0]
	if forwarded.PayloadDigest != directFinding.EvidenceDigest() || forwarded.ExpectedVersion != directFinding.EvidenceRef ||
		forwarded.Scope.Resource != directFinding.Resource || len(forwarded.Scope.IDs) != len(directFinding.TargetRefs) ||
		forwarded.Scope.IDs[0] != directFinding.TargetRefs[0] || forwarded.Tenant != directFinding.Tenant {
		t.Fatalf("forwarded request is not the finding's own evidence: %+v vs %+v", forwarded, directFinding)
	}

	report, err := client.CorrelateProductTelemetry(opCtx, &adminv1.CorrelateProductTelemetryRequest{
		Scope: rev087Scope(fixtureTenant), Events: rev087Events(),
	})
	if err != nil {
		t.Fatalf("correlate: %v", err)
	}
	allow, err := telemetry.DefaultAllowlist()
	if err != nil {
		t.Fatalf("default allowlist: %v", err)
	}
	directReport, err := productcorrelation.Correlate(principal, allow, rev087DirectEvents())
	if err != nil {
		t.Fatalf("direct correlate: %v", err)
	}
	if report.GetDigest() != directReport.Digest() || len(report.GetTimelines()) != len(directReport.Timelines) {
		t.Fatalf("RPC report digest/timelines = %q/%d, direct = %q/%d",
			report.GetDigest(), len(report.GetTimelines()), directReport.Digest(), len(directReport.Timelines))
	}
	for reason, count := range directReport.Dropped {
		if report.GetDropped()[reason] != int64(count) {
			t.Fatalf("RPC dropped[%s] = %d, direct = %d (RPC all: %v)",
				reason, report.GetDropped()[reason], count, report.GetDropped())
		}
	}
}
