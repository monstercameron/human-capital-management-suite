// Package bootstrap_test is the cross-package bootstrap conformance suite
// (definitions/architecture/repository-layout.yaml, root "test").
//
// It exists because what NEXT-004 and NEXT-005 assert is not true of any one
// package: it is true of a composed cell. The suite migrates an ephemeral
// PostgreSQL from zero, composes the same cell cmd/hcmnext serves
// (internal/intent/app.NewCell) in process, drives it through both published
// transports, and then reads the database directly to check what the cell
// actually wrote.
package bootstrap_test

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"

	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
	registryv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/registry/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/fixtures"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/position"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	transportcell "github.com/monstercameron/human-capital-management-suite/internal/transport/cell"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
)

func TestMain(m *testing.M) {
	pgtest.RunMain(m)
}

// Fixture identity. The tenant is the corpus tenant so that the governed
// worker read this cell performs is a read of the design-partner corpus,
// not of a second population invented for the test.
const (
	testTenant   = string(fixtures.Tenant)
	testIssuer   = "https://issuer.test.hcm-next.invalid"
	testAudience = "hcm-next-api"
	testSubject  = "user-0191f3c4"
	testOrgScope = "org-north-america"
	testCellID   = "cell-bootstrap-test"

	// testPurpose is the purpose the suite reads under. It is
	// compensation_review because the P1A intents that touch pay are only
	// granted under it by the BOOTSTRAP policy (internal/trust/authz), and a
	// suite that ran under a purpose the policy blanket-allows would never
	// exercise a purpose-bound grant at all.
	testPurpose = authz.PurposeCompensationReview
	// deniedPurpose is authorized for the principal but carries no grant over
	// the compensation domain. It is what the refusal test invokes under: the
	// credential is valid, admission passes, and the policy is the thing that
	// says no.
	deniedPurpose = "hcm_operations"
	// testRole is the administrative role template the suite's principal
	// holds. A comp admin reaches a record through the tenant boundary rather
	// than through a manager or HR-partner relationship fact, which is the
	// only scope this cell can resolve: P1A publishes no relationship
	// projection.
	testRole = string(authz.RoleCompAdmin)
)

// testSigningKey is a fixed test key. It never leaves this package and it
// authenticates nothing outside a test process.
var testSigningKey = []byte("hcm-next-bootstrap-suite-signing-key-32+")

// baseTime pins the credential window. Business time comes from the request,
// never from this clock.
var baseTime = time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)

// cell is one composed, running P1A cell plus direct database access, so an
// assertion can compare what a caller was told against what was written.
type cell struct {
	t testing.TB

	db    *pgtest.DB
	pool  *pgxadapter.Pool
	store *pgstore.Store
	app   *app.Cell

	grpcIntent   intentsv1.IntentServiceClient
	grpcRegistry registryv1.RegistryServiceClient

	edgeIntent   *bootstrapIntentClient
	edgeRegistry *bootstrapRegistryClient
	// edgeURL and edgeClient reach the HTTP edge directly, for the routes
	// that are not connect procedures - the discovery document - and for the
	// unauthenticated probes those routes have to refuse.
	edgeURL    string
	edgeClient *http.Client

	token string

	mu      sync.Mutex
	records []transport.LogRecord
}

// newCell migrates a private schema, composes the cell and publishes it on
// both transports.
//
// The composition call is internal/intent/app.NewCell, which is the same call
// cmd/hcmnext makes. A suite that assembled the service differently from the
// binary would prove nothing about the binary.
func newCell(t *testing.T) *cell {
	t.Helper()

	db := pgtest.New(t)

	// The migration tree is schema-relative; pinning search_path on every
	// pooled connection is what keeps this cell inside its own schema.
	pool, err := pgxadapter.NewPool(context.Background(), db.URL, map[string]string{"search_path": db.Schema})
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(pool.Close)

	store, err := pgstore.New(pool, pgstore.WithCellID(testCellID),
		pgstore.WithClock(func() time.Time { return baseTime }))
	if err != nil {
		t.Fatalf("pgstore.New: %v", err)
	}
	if err := store.Bootstrap(context.Background(), testTenant); err != nil {
		t.Fatalf("bootstrap tenant: %v", err)
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
	token, err := verifier.Issue(trust.Claims{
		Issuer:               testIssuer,
		Audience:             testAudience,
		Subject:              testSubject,
		SubjectKind:          "human",
		Tenant:               testTenant,
		OrganizationScopeID:  testOrgScope,
		Roles:                []string{"intent_author", testRole},
		AuthorityRefs:        []string{"authority:position:vp-people"},
		Purposes:             []string{testPurpose, deniedPurpose, "workforce_analytics"},
		AuthenticationMethod: "bearer_token",
		Assurance:            "substantial",
		SessionRef:           "session-bootstrap",
		IssuedAtUnix:         baseTime.Add(-time.Minute).Unix(),
		ExpiresAtUnix:        baseTime.Add(time.Hour).Unix(),
	})
	if err != nil {
		t.Fatalf("issue credential: %v", err)
	}

	c := &cell{t: t, db: db, pool: pool, store: store, token: "Bearer " + token}

	composed, err := app.NewCell(app.CellConfig{
		Store:       store,
		RoleAccess:  bootstrapTestRoleAccess(t, pool, testTenant),
		Verifier:    verifier,
		Audience:    testAudience,
		MaxDeadline: 30 * time.Second,
		Logger:      transport.LoggerFunc(c.appendRecord),
		Now:         func() time.Time { return baseTime },
	})
	if err != nil {
		t.Fatalf("app.NewCell: %v", err)
	}
	c.app = composed

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
	c.grpcIntent = intentsv1.NewIntentServiceClient(conn)
	c.grpcRegistry = registryv1.NewRegistryServiceClient(conn)

	edgeHandler, err := transportcell.NewEdgeHandler(composed)
	if err != nil {
		t.Fatalf("EdgeHandler: %v", err)
	}
	httpServer := httptest.NewServer(edgeHandler)
	t.Cleanup(httpServer.Close)
	c.edgeIntent = newBootstrapIntentClient(httpServer.Client(), httpServer.URL)
	c.edgeRegistry = newBootstrapRegistryClient(httpServer.Client(), httpServer.URL)
	c.edgeURL = httpServer.URL
	c.edgeClient = httpServer.Client()

	return c
}

// memoryEvidence returns the in-memory evidence test double an in-process
// cell was composed with (WF-RUN-035: only a served composition records on
// the durable store), failing the test when the cell holds anything else.
func memoryEvidence(t testing.TB, composed *app.Cell) *app.MemoryEvidenceSink {
	t.Helper()
	sink, ok := composed.Evidence.(*app.MemoryEvidenceSink)
	if !ok {
		t.Fatalf("cell evidence is %T, want the in-memory test double", composed.Evidence)
	}
	return sink
}

func (c *cell) appendRecord(record transport.LogRecord) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.records = append(c.records, record)
}

// logRecords returns a copy of the server-side request log.
func (c *cell) logRecords() []transport.LogRecord {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]transport.LogRecord, len(c.records))
	copy(out, c.records)
	return out
}

// grpcContext attaches the credential to a native gRPC call.
func (c *cell) grpcContext(ctx context.Context) context.Context {
	return metadata.AppendToOutgoingContext(ctx, transport.AuthorizationMetadataKey, c.token)
}

// edgeRequest wraps a message with the credential for the HTTP edge.
func edgeRequest[T any](c *cell, msg *T) *connect.Request[T] {
	req := connect.NewRequest(msg)
	req.Header().Set(transport.AuthorizationMetadataKey, c.token)
	return req
}

// queryOne runs a single-value query against the cell's own schema.
func queryOne[T any](t testing.TB, c *cell, sql string, args ...any) T {
	t.Helper()
	var out T
	if err := c.pool.QueryRow(context.Background(), sql, args...).Scan(&out); err != nil {
		t.Fatalf("query %q: %v", sql, err)
	}
	return out
}

// ---------------------------------------------------------------------------
// Request construction
// ---------------------------------------------------------------------------

// promoteWorkerRequest builds the canonical P1A promotion request: Omar Reyes
// from OPS-HRBP2/P2 into the governed POS-HRBP-301 target with the legacy
// 93,000 -> 98,000 USD raise, which the corpus certifies as READY.
func promoteWorkerRequest(t *testing.T, idempotencyKey string) *intentsv1.CreateIntentRequest {
	t.Helper()
	worker, err := fixtures.WorkerRef("omar-reyes")
	if err != nil {
		t.Fatalf("resolve worker: %v", err)
	}
	payload := mustStruct(t, map[string]any{
		"worker_ref": "omar-reyes",
		"known_at":   "2026-05-15",
		"target": map[string]any{
			"job_code": "OPS-HRBP3",
			"grade":    "P3",
			// Position-bound P1A resolves current compensation and capacity
			// through the governed snapshot; callers cannot supply current truth.
			"position_id": governedPromotionPositionRef(t),
			"org_unit":    "people-ops",
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
	})

	return &intentsv1.CreateIntentRequest{
		IdempotencyKey: idempotencyKey,
		Definition: &intentsv1.DefinitionReference{
			IntentTypeId: promotion.IntentType,
			Version:      1,
		},
		Subjects: []*intentsv1.SubjectReference{
			{SubjectKind: "EMPLOYMENT", SubjectId: worker.Id, AuthorityDomain: "PEOPLE"},
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

func governedPromotionPositionRef(t testing.TB) string {
	t.Helper()
	catalog, err := fixtures.NewMemoryPositionCatalog()
	if err != nil {
		t.Fatalf("NewMemoryPositionCatalog: %v", err)
	}
	ref, ok := catalog.PositionRefForCode("POS-HRBP-301")
	if !ok {
		t.Fatal("POS-HRBP-301 is not in the governed fixture catalog")
	}
	effective, err := values.ParseLocalDate("2026-06-01")
	if err != nil {
		t.Fatalf("ParseLocalDate: %v", err)
	}
	known, err := values.NewKnownAt(values.NewInstant(time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)))
	if err != nil {
		t.Fatalf("NewKnownAt: %v", err)
	}
	revision, exists, err := catalog.PositionRevisionAt(context.Background(), position.PositionQuery{
		Tenant: fixtures.Tenant, Position: ref, AsOf: position.AsOf{EffectiveOn: effective, KnownAt: known},
	})
	if err != nil || !exists {
		t.Fatalf("PositionRevisionAt exists=%v err=%v", exists, err)
	}
	encoded, err := position.EncodeRevisionRef(ref, revision.Revision)
	if err != nil {
		t.Fatalf("EncodeRevisionRef: %v", err)
	}
	return encoded.String()
}

// mustStruct encodes a P1A request payload.
//
// The four P1A definitions declare input schemas the generation toolchain does
// not emit descriptors for yet, so a request travels as a
// google.protobuf.Struct under the declared schema identity. That is the
// contract internal/intent/app decodes, and this is the client half of it.
func mustStruct(t *testing.T, fields map[string]any) []byte {
	t.Helper()
	s, err := structpb.NewStruct(fields)
	if err != nil {
		t.Fatalf("encode request payload: %v", err)
	}
	// The idempotency profile binds the typed wire bytes. A protobuf Struct
	// holds maps, so two equivalent fixtures must use deterministic encoding
	// or a harmless map iteration order will look like a changed request.
	wire, err := proto.MarshalOptions{Deterministic: true}.Marshal(s)
	if err != nil {
		t.Fatalf("marshal request payload: %v", err)
	}
	return wire
}

// createAndSimulate drives one intent through create and simulate on the gRPC
// surface and returns both answers.
func (c *cell) createAndSimulate(t *testing.T, idempotencyKey string) (*intentsv1.IntentInstance, *intentsv1.SimulationArtifact) {
	t.Helper()
	ctx := c.grpcContext(context.Background())

	created, err := c.grpcIntent.CreateIntent(ctx, promoteWorkerRequest(t, idempotencyKey))
	if err != nil {
		t.Fatalf("CreateIntent(%s): %v", idempotencyKey, err)
	}
	simulated, err := c.grpcIntent.SimulateIntent(ctx, &intentsv1.SimulateIntentRequest{
		IntentId: created.GetIntent().GetIntentId(),
	})
	if err != nil {
		t.Fatalf("SimulateIntent(%s): %v", idempotencyKey, err)
	}
	return created.GetIntent(), simulated.GetSimulation()
}

// pollUntil waits for cond to hold, polling rather than sleeping a fixed
// interval so a slow machine does not turn a correct result into a failure.
func pollUntil(t testing.TB, within time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(within)
	for {
		if cond() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out after %s waiting for %s", within, what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// httpNoop is a fake outbound HTTP sink. Nothing in P1A may call it; the
// counter is what turns "we believe there is no provider call" into an
// assertion.
type httpNoop struct {
	mu     sync.Mutex
	calls  int
	server *httptest.Server
}

func newHTTPNoop(t *testing.T) *httpNoop {
	t.Helper()
	sink := &httpNoop{}
	sink.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sink.mu.Lock()
		sink.calls++
		sink.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(sink.server.Close)
	return sink
}

func (s *httpNoop) Calls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

// URL is the endpoint a connector would have been configured with.
func (s *httpNoop) URL() string { return s.server.URL }

func describe(v any) string { return fmt.Sprintf("%v", v) }
