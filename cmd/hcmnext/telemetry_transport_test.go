package main

import (
	"context"
	"net"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/fixtures"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/buildinfo"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/telemetry"
	hcmotel "github.com/monstercameron/human-capital-management-suite/internal/platform/telemetry/otel"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	transportcell "github.com/monstercameron/human-capital-management-suite/internal/transport/cell"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/edge"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// telemetryTransportHarness composes the same cell "hcmnext serve" composes
// (internal/intent/app.NewCell with CellConfig.Telemetry set, then
// internal/transport/cell.NewGRPCServer/NewEdgeHandler with no interceptor
// options of their own), so this test proves that transport composition
// adapter's own wiring - it chains
// otelmw.UnaryServerInterceptor/otelmw.NewConnectInterceptor itself whenever
// the composed cell's Telemetry is non-nil - rather than otelmw's redaction
// logic in isolation, which is already exhaustively covered in
// internal/transport/otelmw's own frozen suite.
type telemetryTransportHarness struct {
	t testing.TB

	provider     *hcmotel.Provider
	spanExporter *tracetest.InMemoryExporter

	token string

	grpcIntent intentsv1.IntentServiceClient
	edgeIntent *edge.IntentClient
}

// newTelemetryTransportHarness migrates a private schema, composes a real
// cell and publishes it on both transports wired exactly the way buildServe
// wires them, over a Provider backed by an in-memory span exporter.
func newTelemetryTransportHarness(t *testing.T) *telemetryTransportHarness {
	t.Helper()
	baseTime := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)

	db := pgtest.New(t)
	pool, err := pgxadapter.NewPool(context.Background(), db.URL, map[string]string{"search_path": db.Schema})
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(pool.Close)

	store, err := pgstore.New(pool, pgstore.WithCellID("cell-otel-transport-test"))
	if err != nil {
		t.Fatalf("pgstore.New: %v", err)
	}
	tenant := string(fixtures.Tenant)
	if err := store.Bootstrap(context.Background(), tenant); err != nil {
		t.Fatalf("bootstrap tenant: %v", err)
	}

	const (
		testIssuer   = "https://issuer.test.hcm-next.invalid"
		testAudience = "hcm-next-api"
	)
	verifier, err := trust.NewHMACVerifier(trust.HMACVerifierConfig{
		Key:      []byte("hcmnext-otel-transport-test-signing-key-32+"),
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
		Subject:              "user-otel-transport-test",
		SubjectKind:          "human",
		Tenant:               tenant,
		OrganizationScopeID:  "org-north-america",
		Roles:                []string{"intent_author", "comp_admin"},
		AuthorityRefs:        []string{"authority:position:vp-people"},
		Purposes:             []string{"compensation_review"},
		AuthenticationMethod: "bearer_token",
		Assurance:            "substantial",
		SessionRef:           "session-otel-transport-test",
		IssuedAtUnix:         baseTime.Add(-time.Minute).Unix(),
		ExpiresAtUnix:        baseTime.Add(time.Hour).Unix(),
	})
	if err != nil {
		t.Fatalf("issue credential: %v", err)
	}

	spanExporter := tracetest.NewInMemoryExporter()
	provider, err := hcmotel.NewProvider(context.Background(), hcmotel.Config{
		Resource: telemetry.NewResourceFromBuild(
			buildinfo.Info{Revision: "otel-transport-test"},
			"hcmnext", "instance-otel-transport-test", "test", "cell-otel-transport-test", "",
			telemetry.ProcessRoleAPI, telemetry.TenantClassStandard,
		),
		Evaluator:       testTelemetryEvaluator(t),
		ShutdownTimeout: 5 * time.Second,
		Trace:           hcmotel.TraceConfig{Exporter: spanExporter},
		Metric:          hcmotel.MetricConfig{Reader: sdkmetric.NewManualReader()},
	})
	if err != nil {
		t.Fatalf("hcmotel.NewProvider: %v", err)
	}
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })

	// This is exactly buildServe's own wiring: internal/transport/cell chains
	// otelmw.UnaryServerInterceptor/otelmw.NewConnectInterceptor by itself off
	// the composed cell's Telemetry field - neither call below passes an
	// interceptor option, because internal/transport/cell owns that
	// composition (LIB-003: internal/intent/app may not import grpc/connect),
	// not this test.
	composed, err := app.NewCell(app.CellConfig{
		Store:       store,
		Verifier:    verifier,
		Audience:    testAudience,
		MaxDeadline: 30 * time.Second,
		Now:         func() time.Time { return baseTime },
		Telemetry:   provider,
	})
	if err != nil {
		t.Fatalf("app.NewCell: %v", err)
	}

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

	edgeHandler, err := transportcell.NewEdgeHandler(composed)
	if err != nil {
		t.Fatalf("EdgeHandler: %v", err)
	}
	httpServer := httptest.NewServer(edgeHandler)
	t.Cleanup(httpServer.Close)

	return &telemetryTransportHarness{
		t:            t,
		provider:     provider,
		spanExporter: spanExporter,
		token:        "Bearer " + token,
		grpcIntent:   intentsv1.NewIntentServiceClient(conn),
		edgeIntent:   edge.NewIntentClient(httpServer.Client(), httpServer.URL),
	}
}

// testTelemetryEvaluator returns the frozen, unmodified OBS-004 policy
// evaluator, unmodified - the same one production wires - so this test proves
// the composition root's wiring is governed by the real policy, not a looser
// one built for the test.
func testTelemetryEvaluator(t testing.TB) *telemetry.Evaluator {
	t.Helper()
	allow, err := telemetry.DefaultAllowlist()
	if err != nil {
		t.Fatalf("DefaultAllowlist: %v", err)
	}
	return telemetry.NewEvaluator(allow, telemetry.DefaultExportPolicy(telemetry.DefaultPolicyVersion), telemetry.DefaultSamplingPolicy())
}

// grpcContext attaches this harness's credential and a caller-supplied,
// non-reserved header to a gRPC call. The header exists only to prove it
// never reaches an exported span attribute (see the test below): admission
// accepts it because it is not on trust.ReservedMetadataKeys, and this
// package's telemetry wiring must still never leak it.
func (h *telemetryTransportHarness) grpcContext(ctx context.Context, spoofed string) context.Context {
	return metadata.AppendToOutgoingContext(ctx,
		transport.AuthorizationMetadataKey, h.token,
		"x-debug-principal-hint", spoofed)
}

// edgeRequest wraps msg with the same credential and spoofed header for the
// HTTP edge.
func edgeRequest[T any](h *telemetryTransportHarness, msg *T, spoofed string) *connect.Request[T] {
	req := connect.NewRequest(msg)
	req.Header().Set(transport.AuthorizationMetadataKey, h.token)
	req.Header().Set("x-debug-principal-hint", spoofed)
	return req
}

// promoteWorkerRequest builds the canonical P1A promotion request: Omar
// Reyes's OPS-HRBP2/P2 -> OPS-HRBP3/P3 raise, the corpus scenario
// internal/domains/promotion certifies as READY. It mirrors
// test/bootstrap/harness_test.go's fixture of the same name; both compose the
// same cell and both need a request that resolves.
func promoteWorkerRequest(t *testing.T, idempotencyKey string) *intentsv1.CreateIntentRequest {
	t.Helper()
	worker, err := fixtures.WorkerRef("omar-reyes")
	if err != nil {
		t.Fatalf("resolve worker: %v", err)
	}
	fields := map[string]any{
		"worker_ref": "omar-reyes",
		"known_at":   "2026-05-15",
		"target": map[string]any{
			"job_code":    "OPS-HRBP3",
			"grade":       "P3",
			"org_unit":    "people-ops",
			"position_id": "POS-HRBP-301",
			"pay_zone":    "US-EAST",
		},
		"effective_date":  "2026-06-01",
		"evaluation_date": "2026-05-15",
		"business_reason": "promotion_into_senior_hrbp",
		"current": map[string]any{
			"base":              "93000.00",
			"currency":          "USD",
			"pay_basis":         "ANNUAL_SALARY",
			"bonus_target":      "0.0500",
			"effective_date":    "2026-06-01",
			"revision_stream":   "rewards.package.omar",
			"revision_sequence": "11",
		},
		"proposed": map[string]any{
			"base":              "98000.00",
			"currency":          "USD",
			"pay_basis":         "ANNUAL_SALARY",
			"bonus_target":      "0.0500",
			"effective_date":    "2026-06-01",
			"revision_stream":   "rewards.package.omar",
			"revision_sequence": "11",
		},
		"budget": map[string]any{
			"available_amount": "50000.00",
			"currency":         "USD",
			"owner_system":     "adaptive.planning",
			"policy_ref":       "finance.authority/2026.1",
			"scope":            "people-ops:FY26-merit",
			"period":           "FY2026",
			"baseline_version": "fy26-merit-r7",
			"observation_id":   "obs_budget_fy26_merit_r7",
		},
	}
	s, err := structpb.NewStruct(fields)
	if err != nil {
		t.Fatalf("encode request payload: %v", err)
	}
	payload, err := proto.Marshal(s)
	if err != nil {
		t.Fatalf("marshal request payload: %v", err)
	}

	return &intentsv1.CreateIntentRequest{
		IdempotencyKey: idempotencyKey,
		Definition: &intentsv1.DefinitionReference{
			IntentTypeId: promotion.IntentType,
			Version:      1,
		},
		Subjects: []*intentsv1.SubjectReference{
			{SubjectKind: "EMPLOYMENT", SubjectId: worker.Id, AuthorityDomain: "PEOPLE"},
			{SubjectKind: "POSITION", SubjectId: "POS-HRBP-301", AuthorityDomain: "POSITION"},
		},
		Request: &intentsv1.TypedPayload{
			Schema: &intentsv1.SchemaReference{
				SchemaId:         "hcmnext.people.v1.PromoteWorkerRequest",
				Version:          1,
				ProtobufFullName: "hcmnext.people.v1.PromoteWorkerRequest",
			},
			ProtobufWireBytes: payload,
		},
		ExecutionMode: intentsv1.ExecutionMode_EXECUTION_MODE_SIMULATE,
	}
}

// attrValue returns one attribute's string value from span.
func attrValue(span tracetest.SpanStub, key string) (string, bool) {
	for _, kv := range span.Attributes {
		if string(kv.Key) == key {
			return kv.Value.AsString(), true
		}
	}
	return "", false
}

// TestTodo_OBS_002_ServeWiresOTelInterceptorsOnBothTransports proves
// internal/transport/cell's own composition - NewGRPCServer chaining
// otelmw.UnaryServerInterceptor, NewEdgeHandler adding
// otelmw.NewConnectInterceptor through edge's HandlerOptions, both
// automatically whenever the composed cell carries a Telemetry provider,
// exactly as buildServe composes it - actually takes effect: a real
// SimulateIntent call over native gRPC and over the HTTP edge each produce an
// exported span, and on both of them a caller-supplied header admission let
// through (because it is not a reserved trusted-context key) never reaches
// an attribute, because this composition's spans carry only what the frozen
// OBS-004 policy allow-lists.
func TestTodo_OBS_002_ServeWiresOTelInterceptorsOnBothTransports(t *testing.T) {
	h := newTelemetryTransportHarness(t)
	const spoofed = "attacker-supplied-principal-should-never-appear"
	const procedure = "/hcmnext.intents.v1.IntentService/SimulateIntent"

	grpcCtx := h.grpcContext(context.Background(), spoofed)
	created, err := h.grpcIntent.CreateIntent(grpcCtx, promoteWorkerRequest(t, "otel-transport-grpc"))
	if err != nil {
		t.Fatalf("grpc CreateIntent: %v", err)
	}
	if _, err := h.grpcIntent.SimulateIntent(grpcCtx, &intentsv1.SimulateIntentRequest{
		IntentId: created.GetIntent().GetIntentId(),
	}); err != nil {
		t.Fatalf("grpc SimulateIntent: %v", err)
	}

	// The edge simulates the intent the gRPC call created: PROMOUX-002 admits
	// one active promotion per employee, so a second create for the same
	// fixture worker is refused before any SimulateIntent span exists.
	if _, err := h.edgeIntent.SimulateIntent(context.Background(), edgeRequest(h, &intentsv1.SimulateIntentRequest{
		IntentId: created.GetIntent().GetIntentId(),
	}, spoofed)); err != nil {
		t.Fatalf("edge SimulateIntent: %v", err)
	}

	if report := h.provider.ForceFlush(context.Background()); report.Err() != nil {
		t.Fatalf("ForceFlush: %v", report.Err())
	}

	var simulateSpans tracetest.SpanStubs
	for _, s := range h.spanExporter.GetSpans() {
		if s.Name == procedure {
			simulateSpans = append(simulateSpans, s)
		}
	}
	if len(simulateSpans) != 2 {
		t.Fatalf("found %d spans named %q, want 2 (one per transport)", len(simulateSpans), procedure)
	}

	for i, span := range simulateSpans {
		if v, ok := attrValue(span, "outcome"); !ok || v != "success" {
			t.Errorf("span %d outcome = %q, %v; want %q", i, v, ok, "success")
		}
		// request_id/evidence_ref/principal_ref are not registered attribute
		// keys (internal/platform/telemetry/otel/trace.go) and must be
		// redacted; correlation_id, when present, must never carry the
		// spoofed header value this test's admitted-but-unreserved header
		// supplied.
		for _, key := range []string{"request_id", "evidence_ref", "principal_ref"} {
			if _, ok := attrValue(span, key); ok {
				t.Errorf("span %d kept unregistered attribute %q; want redacted", i, key)
			}
		}
		for _, kv := range span.Attributes {
			if strings.Contains(kv.Value.AsString(), spoofed) {
				t.Errorf("span %d attribute %s carries the caller-supplied header value %q",
					i, kv.Key, kv.Value.AsString())
			}
		}
	}

	// Both transports produced the identical attribute-key shape after
	// redaction: this is what proves the two interceptor chains buildServe
	// wires are actually the same policy applied twice, not two different
	// configurations that happen to both succeed.
	keys := func(span tracetest.SpanStub) map[string]bool {
		out := make(map[string]bool, len(span.Attributes))
		for _, kv := range span.Attributes {
			out[string(kv.Key)] = true
		}
		return out
	}
	grpcKeys, edgeKeys := keys(simulateSpans[0]), keys(simulateSpans[1])
	if len(grpcKeys) != len(edgeKeys) {
		t.Fatalf("attribute-key counts differ between transports: grpc=%d edge=%d", len(grpcKeys), len(edgeKeys))
	}
	for k := range grpcKeys {
		if !edgeKeys[k] {
			t.Errorf("attribute %q present on one transport's span and not the other", k)
		}
	}
}
