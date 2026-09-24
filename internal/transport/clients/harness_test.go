package clients_test

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"

	commonv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/common/v1"
	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
	registryv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/registry/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/clients"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/edge"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/grpcserver"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/manifest"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/transporttest"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// fixedRequestID is the correlation identifier both backends are told to
// use, so that two equivalent requests differ in nothing observable. It
// matches the constant name used in internal/transport/edge's own harness;
// the value itself has no significance beyond being fixed.
const fixedRequestID = "req-clients-fixture-0001"

// baseTime pins the fixture clock.
var baseTime = time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)

// fakeClock is a movable clock for credential-expiry scenarios.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// harness stands up both backends of the generated clients package — native
// gRPC and connect-go — over the same transporttest fake handlers, the same
// verifier and the same admission configuration used by
// internal/transport/edge's own TOOL-008 harness.
//
// The connect-go server under test is edge.NewHandler: it is the P1A edge
// TOOL-008 selected, and reusing it here (rather than building a second
// connect server) is what makes "the connect backend talks to the real
// selected edge" a fact rather than an assumption. Nothing in this package
// implements a server; internal/transport/clients is a client package.
type harness struct {
	t testing.TB

	clock    *fakeClock
	verifier *trust.HMACVerifier
	intent   *transporttest.IntentHandler
	registry *transporttest.RegistryHandler

	grpcIntent   clients.IntentClient
	grpcRegistry clients.RegistryClient

	connectIntent   clients.IntentClient
	connectRegistry clients.RegistryClient

	edgeURL    string
	httpClient *http.Client
	token      string

	mu      sync.Mutex
	records []transport.LogRecord
}

func newHarness(t testing.TB) *harness {
	t.Helper()

	h := &harness{
		t:        t,
		clock:    &fakeClock{now: baseTime},
		intent:   &transporttest.IntentHandler{},
		registry: &transporttest.RegistryHandler{},
	}

	verifier, err := transporttest.NewVerifier(h.clock.Now)
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}
	h.verifier = verifier

	token, err := transporttest.BearerToken(verifier, transporttest.DefaultClaims(baseTime))
	if err != nil {
		t.Fatalf("BearerToken: %v", err)
	}
	h.token = token

	cfg := transporttest.Config(verifier, h.clock.Now, fixedRequestID, transport.LoggerFunc(h.appendRecord))

	grpcServer, err := grpcserver.NewServer(grpcserver.Options{
		Config:   cfg,
		Intent:   h.intent,
		Registry: h.registry,
	})
	if err != nil {
		t.Fatalf("grpcserver.NewServer: %v", err)
	}
	listener := bufconn.Listen(1 << 20)
	go func() {
		_ = grpcServer.Serve(listener)
	}()
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
	h.grpcIntent = clients.NewIntentClientGRPC(conn)
	h.grpcRegistry = clients.NewRegistryClientGRPC(conn)

	edgeHandler, err := edge.NewHandler(edge.Options{
		Config:   cfg,
		Intent:   h.intent,
		Registry: h.registry,
	})
	if err != nil {
		t.Fatalf("edge.NewHandler: %v", err)
	}
	httpServer := httptest.NewServer(edgeHandler)
	t.Cleanup(httpServer.Close)

	h.edgeURL = httpServer.URL
	h.httpClient = httpServer.Client()
	h.connectIntent = clients.NewIntentClientConnect(h.httpClient, h.edgeURL)
	h.connectRegistry = clients.NewRegistryClientConnect(h.httpClient, h.edgeURL)

	return h
}

// appendRecord collects one server-side log record.
func (h *harness) appendRecord(record transport.LogRecord) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, record)
}

// logRecords returns a copy of the collected log records.
func (h *harness) logRecords() []transport.LogRecord {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]transport.LogRecord, len(h.records))
	copy(out, h.records)
	return out
}

// resetRecords drops the collected log records.
func (h *harness) resetRecords() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = nil
}

// awaitRecord polls for a server-side log record matching pred, up to a
// deadline. Server-side completion is not synchronized with the client's
// return, so polling rather than sleeping is what makes cancellation
// assertions reliable instead of flaky.
func (h *harness) awaitRecord(pred func(transport.LogRecord) bool) (transport.LogRecord, bool) {
	h.t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		for _, record := range h.logRecords() {
			if pred(record) {
				return record, true
			}
		}
		if time.Now().After(deadline) {
			return transport.LogRecord{}, false
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// authOpts returns the CallOptions that authenticate as the fixture
// principal and carry the fixed correlation identifier.
func (h *harness) authOpts(extra ...clients.CallOption) []clients.CallOption {
	opts := []clients.CallOption{
		clients.WithAuthorization(h.token),
		clients.WithRequestID(fixedRequestID),
	}
	return append(opts, extra...)
}

// noCredentialOpts carries only the fixed correlation identifier, with no
// credential.
func (h *harness) noCredentialOpts(extra ...clients.CallOption) []clients.CallOption {
	opts := []clients.CallOption{
		clients.WithRequestID(fixedRequestID),
	}
	return append(opts, extra...)
}

// assertOwnedParity fails unless two owned errors mean exactly the same
// thing. Equal transport status codes alone are explicitly not sufficient
// parity, per the endpoint contract's required test layers.
func assertOwnedParity(t *testing.T, label string, viaGRPC, viaConnect *envelope.Error) {
	t.Helper()
	if viaGRPC.Code() != viaConnect.Code() {
		t.Errorf("%s: owned code differs: grpc=%v connect=%v", label, viaGRPC.Code(), viaConnect.Code())
	}
	if viaGRPC.Retryable() != viaConnect.Retryable() {
		t.Errorf("%s: retryability differs: grpc=%v connect=%v", label, viaGRPC.Retryable(), viaConnect.Retryable())
	}
	if viaGRPC.Message() != viaConnect.Message() {
		t.Errorf("%s: message differs: grpc=%q connect=%q", label, viaGRPC.Message(), viaConnect.Message())
	}
	if viaGRPC.CorrelationID() != viaConnect.CorrelationID() {
		t.Errorf("%s: correlation differs: grpc=%q connect=%q", label, viaGRPC.CorrelationID(), viaConnect.CorrelationID())
	}
	if viaGRPC.EvidenceRef() != viaConnect.EvidenceRef() {
		t.Errorf("%s: evidence differs: grpc=%+v connect=%+v", label, viaGRPC.EvidenceRef(), viaConnect.EvidenceRef())
	}
	grpcViolations, connectViolations := viaGRPC.Violations(), viaConnect.Violations()
	if len(grpcViolations) != len(connectViolations) {
		t.Fatalf("%s: violation count differs: grpc=%d connect=%d (%v vs %v)",
			label, len(grpcViolations), len(connectViolations), grpcViolations, connectViolations)
	}
	for i := range grpcViolations {
		if grpcViolations[i] != connectViolations[i] {
			t.Errorf("%s: violation %d differs: grpc=%+v connect=%+v", label, i, grpcViolations[i], connectViolations[i])
		}
	}
	if viaGRPC.HTTPStatus() != viaConnect.HTTPStatus() {
		t.Errorf("%s: projected HTTP status differs: grpc=%d connect=%d", label, viaGRPC.HTTPStatus(), viaConnect.HTTPStatus())
	}
}

// ownedError extracts the *envelope.Error DecodeGRPCError/DecodeConnectError
// already produced. Both backends return one for the same reason: the
// generated client's whole point is that a caller does not need a
// transport-specific extraction step.
func ownedError(t *testing.T, err error) *envelope.Error {
	t.Helper()
	owned, ok := envelope.As(err)
	if !ok {
		t.Fatalf("error %v (%T) was not decoded into *envelope.Error", err, err)
	}
	return owned
}

// clone returns a deep copy of msg, so the same vector can be sent to both
// backends without either one observing the other's server-side rewrites.
func clone[T proto.Message](msg T) T {
	return proto.Clone(msg).(T)
}

// submitRequest returns a SubmitIntent request that satisfies the fixture's
// expected revision.
func submitRequest() *intentsv1.SubmitIntentRequest {
	return &intentsv1.SubmitIntentRequest{
		IdempotencyKey:          "idem-submit-1",
		IntentId:                transporttest.KnownIntentID,
		ProposalRevisionId:      "revision-1",
		ExpectedInstanceVersion: transporttest.CurrentInstanceVersion,
	}
}

// executeRequest returns a valid ExecuteIntent request. The transporttest
// fixture answers it deterministically (mirroring SubmitIntent's own
// expected-instance-version check), so this exercises the round trip
// itself, not the real EXECUTE authority gate (internal/intent/app owns
// that).
func executeRequest() *intentsv1.ExecuteIntentRequest {
	return &intentsv1.ExecuteIntentRequest{
		IdempotencyKey:          "idem-execute-1",
		IntentId:                transporttest.KnownIntentID,
		ExpectedInstanceVersion: transporttest.CurrentInstanceVersion,
		Approval: &intentsv1.ProposalApproval{
			ProposalRevisionId: "revision-1",
			Approved:           true,
			ApprovalRef:        "approval-1",
		},
	}
}

// cancelRequest returns a CancelIntent request naming reason.
func cancelRequest(reason string) *intentsv1.CancelIntentRequest {
	return &intentsv1.CancelIntentRequest{
		IdempotencyKey:          "idem-cancel-1",
		IntentId:                transporttest.KnownIntentID,
		ExpectedInstanceVersion: transporttest.CurrentInstanceVersion,
		ReasonRef:               reason,
	}
}

// supersedeRequest returns a valid SupersedeIntent request.
func supersedeRequest() *intentsv1.SupersedeIntentRequest {
	return &intentsv1.SupersedeIntentRequest{
		IdempotencyKey:          "idem-supersede-1",
		SupersededIntentId:      transporttest.KnownIntentID,
		ExpectedInstanceVersion: transporttest.CurrentInstanceVersion,
		Definition:              &intentsv1.DefinitionReference{IntentTypeId: transporttest.KnownDefinitionID, Version: 3},
		ReasonRef:               "supersede/reason",
	}
}

// definitionRequest returns a valid GetIntentDefinition request.
func definitionRequest() *registryv1.GetIntentDefinitionRequest {
	return &registryv1.GetIntentDefinitionRequest{
		Definition: &intentsv1.DefinitionReference{IntentTypeId: transporttest.KnownDefinitionID, Version: 3},
	}
}

// msgOrNil unwraps a possible-nil proto.Message, for round-trip tables that
// compare success results generically.
func msgOrNil(msg proto.Message, err error) (proto.Message, error) {
	if err != nil {
		return nil, err
	}
	return msg, nil
}

// repoRoot locates the repository root from this test file's own path,
// which is stable regardless of the working directory `go test` is invoked
// from.
func repoRoot(t testing.TB) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("harness_test.go: runtime.Caller failed")
	}
	// this file is internal/transport/clients/harness_test.go
	return filepath.Join(filepath.Dir(file), "..", "..", "..")
}

// loadEndpointManifest reads the checked-in endpoint manifest, the same
// document tools/gen/clients reads to generate this package. Reading it
// again here — rather than hard-coding the REFUSED_P1A method names — is
// what keeps the parity suite's expectations honest against the manifest
// instead of against a second, driftable copy of it.
func loadEndpointManifest(t testing.TB) *manifest.EndpointManifest {
	t.Helper()
	path := filepath.Join(repoRoot(t), "definitions", "api", "endpoint-manifest.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	var doc manifest.EndpointManifest
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}
	return &doc
}

// dispositionByMethod maps each manifest row's bare MethodName (e.g.
// "CreateIntent") to its [manifest.Disposition]. Method names do not repeat
// across IntentService and RegistryService today, so this is unambiguous.
func dispositionByMethod(t testing.TB) map[string]manifest.Disposition {
	t.Helper()
	doc := loadEndpointManifest(t)
	out := make(map[string]manifest.Disposition, len(doc.Endpoints))
	for _, e := range doc.Endpoints {
		out[e.MethodName] = e.Disposition
	}
	return out
}

// methodCase is one of the 18 public RPCs, callable through either
// generated backend with a valid canonical request.
type methodCase struct {
	Name        string
	Disposition manifest.Disposition
	GRPC        func(ctx context.Context, opts ...clients.CallOption) (proto.Message, error)
	Connect     func(ctx context.Context, opts ...clients.CallOption) (proto.Message, error)
}

// methodCases returns one entry for every public RPC of IntentService and
// RegistryService, each backed by a request the transporttest fixture
// handlers accept. It is the shared table TOOL-007's and PROTO-006's
// integration/parity/refusal suites all run.
func (h *harness) methodCases(t testing.TB) []methodCase {
	t.Helper()
	dispositions := dispositionByMethod(t)
	disp := func(name string) manifest.Disposition {
		d, ok := dispositions[name]
		if !ok {
			t.Fatalf("endpoint manifest names no disposition for method %q", name)
		}
		return d
	}

	return []methodCase{
		{
			Name: "CreateIntent", Disposition: disp("CreateIntent"),
			GRPC: func(ctx context.Context, opts ...clients.CallOption) (proto.Message, error) {
				return h.grpcIntent.CreateIntent(ctx, transporttest.CanonicalCreateIntentRequest(), opts...)
			},
			Connect: func(ctx context.Context, opts ...clients.CallOption) (proto.Message, error) {
				return msgOrNil(h.connectIntent.CreateIntent(ctx, transporttest.CanonicalCreateIntentRequest(), opts...))
			},
		},
		{
			Name: "GetIntent", Disposition: disp("GetIntent"),
			GRPC: func(ctx context.Context, opts ...clients.CallOption) (proto.Message, error) {
				return h.grpcIntent.GetIntent(ctx, &intentsv1.GetIntentRequest{IntentId: transporttest.KnownIntentID}, opts...)
			},
			Connect: func(ctx context.Context, opts ...clients.CallOption) (proto.Message, error) {
				return msgOrNil(h.connectIntent.GetIntent(ctx, &intentsv1.GetIntentRequest{IntentId: transporttest.KnownIntentID}, opts...))
			},
		},
		{
			Name: "ListIntents", Disposition: disp("ListIntents"),
			GRPC: func(ctx context.Context, opts ...clients.CallOption) (proto.Message, error) {
				return h.grpcIntent.ListIntents(ctx, &intentsv1.ListIntentsRequest{Page: &commonv1.PageRequest{PageSize: 10}}, opts...)
			},
			Connect: func(ctx context.Context, opts ...clients.CallOption) (proto.Message, error) {
				return msgOrNil(h.connectIntent.ListIntents(ctx, &intentsv1.ListIntentsRequest{Page: &commonv1.PageRequest{PageSize: 10}}, opts...))
			},
		},
		{
			Name: "SimulateIntent", Disposition: disp("SimulateIntent"),
			GRPC: func(ctx context.Context, opts ...clients.CallOption) (proto.Message, error) {
				return h.grpcIntent.SimulateIntent(ctx, &intentsv1.SimulateIntentRequest{IntentId: transporttest.KnownIntentID}, opts...)
			},
			Connect: func(ctx context.Context, opts ...clients.CallOption) (proto.Message, error) {
				return msgOrNil(h.connectIntent.SimulateIntent(ctx, &intentsv1.SimulateIntentRequest{IntentId: transporttest.KnownIntentID}, opts...))
			},
		},
		{
			Name: "SubmitIntent", Disposition: disp("SubmitIntent"),
			GRPC: func(ctx context.Context, opts ...clients.CallOption) (proto.Message, error) {
				return h.grpcIntent.SubmitIntent(ctx, submitRequest(), opts...)
			},
			Connect: func(ctx context.Context, opts ...clients.CallOption) (proto.Message, error) {
				return msgOrNil(h.connectIntent.SubmitIntent(ctx, submitRequest(), opts...))
			},
		},
		{
			Name: "ExecuteIntent", Disposition: disp("ExecuteIntent"),
			GRPC: func(ctx context.Context, opts ...clients.CallOption) (proto.Message, error) {
				return h.grpcIntent.ExecuteIntent(ctx, executeRequest(), opts...)
			},
			Connect: func(ctx context.Context, opts ...clients.CallOption) (proto.Message, error) {
				return msgOrNil(h.connectIntent.ExecuteIntent(ctx, executeRequest(), opts...))
			},
		},
		{
			Name: "CancelIntent", Disposition: disp("CancelIntent"),
			GRPC: func(ctx context.Context, opts ...clients.CallOption) (proto.Message, error) {
				return h.grpcIntent.CancelIntent(ctx, cancelRequest("cancel/reason"), opts...)
			},
			Connect: func(ctx context.Context, opts ...clients.CallOption) (proto.Message, error) {
				return msgOrNil(h.connectIntent.CancelIntent(ctx, cancelRequest("cancel/reason"), opts...))
			},
		},
		{
			Name: "SupersedeIntent", Disposition: disp("SupersedeIntent"),
			GRPC: func(ctx context.Context, opts ...clients.CallOption) (proto.Message, error) {
				return h.grpcIntent.SupersedeIntent(ctx, supersedeRequest(), opts...)
			},
			Connect: func(ctx context.Context, opts ...clients.CallOption) (proto.Message, error) {
				return msgOrNil(h.connectIntent.SupersedeIntent(ctx, supersedeRequest(), opts...))
			},
		},
		{
			Name: "ExplainIntent", Disposition: disp("ExplainIntent"),
			GRPC: func(ctx context.Context, opts ...clients.CallOption) (proto.Message, error) {
				return h.grpcIntent.ExplainIntent(ctx, &intentsv1.ExplainIntentRequest{IntentId: transporttest.KnownIntentID}, opts...)
			},
			Connect: func(ctx context.Context, opts ...clients.CallOption) (proto.Message, error) {
				return msgOrNil(h.connectIntent.ExplainIntent(ctx, &intentsv1.ExplainIntentRequest{IntentId: transporttest.KnownIntentID}, opts...))
			},
		},
		{
			Name: "ListIntentTimeline", Disposition: disp("ListIntentTimeline"),
			GRPC: func(ctx context.Context, opts ...clients.CallOption) (proto.Message, error) {
				return h.grpcIntent.ListIntentTimeline(ctx, &intentsv1.ListIntentTimelineRequest{IntentId: transporttest.KnownIntentID}, opts...)
			},
			Connect: func(ctx context.Context, opts ...clients.CallOption) (proto.Message, error) {
				return msgOrNil(h.connectIntent.ListIntentTimeline(ctx, &intentsv1.ListIntentTimelineRequest{IntentId: transporttest.KnownIntentID}, opts...))
			},
		},
		{
			Name: "RecommendIntentAction", Disposition: disp("RecommendIntentAction"),
			GRPC: func(ctx context.Context, opts ...clients.CallOption) (proto.Message, error) {
				return h.grpcIntent.RecommendIntentAction(ctx, recommendIntentActionRequest(), opts...)
			},
			Connect: func(ctx context.Context, opts ...clients.CallOption) (proto.Message, error) {
				return msgOrNil(h.connectIntent.RecommendIntentAction(ctx, recommendIntentActionRequest(), opts...))
			},
		},
		{
			Name: "GetIntentDeepLink", Disposition: disp("GetIntentDeepLink"),
			GRPC: func(ctx context.Context, opts ...clients.CallOption) (proto.Message, error) {
				return h.grpcIntent.GetIntentDeepLink(ctx, &intentsv1.GetIntentDeepLinkRequest{IntentId: transporttest.KnownIntentID}, opts...)
			},
			Connect: func(ctx context.Context, opts ...clients.CallOption) (proto.Message, error) {
				return msgOrNil(h.connectIntent.GetIntentDeepLink(ctx, &intentsv1.GetIntentDeepLinkRequest{IntentId: transporttest.KnownIntentID}, opts...))
			},
		},
		{
			Name: "InspectIntentFields", Disposition: disp("InspectIntentFields"),
			GRPC: func(ctx context.Context, opts ...clients.CallOption) (proto.Message, error) {
				return h.grpcIntent.InspectIntentFields(ctx, &intentsv1.InspectIntentFieldsRequest{IntentId: transporttest.KnownIntentID}, opts...)
			},
			Connect: func(ctx context.Context, opts ...clients.CallOption) (proto.Message, error) {
				return msgOrNil(h.connectIntent.InspectIntentFields(ctx, &intentsv1.InspectIntentFieldsRequest{IntentId: transporttest.KnownIntentID}, opts...))
			},
		},
		{
			Name: "ExportIntentFields", Disposition: disp("ExportIntentFields"),
			GRPC: func(ctx context.Context, opts ...clients.CallOption) (proto.Message, error) {
				return h.grpcIntent.ExportIntentFields(ctx, &intentsv1.ExportIntentFieldsRequest{IntentId: transporttest.KnownIntentID, Purpose: transporttest.PurposeAnalytics}, opts...)
			},
			Connect: func(ctx context.Context, opts ...clients.CallOption) (proto.Message, error) {
				return msgOrNil(h.connectIntent.ExportIntentFields(ctx, &intentsv1.ExportIntentFieldsRequest{IntentId: transporttest.KnownIntentID, Purpose: transporttest.PurposeAnalytics}, opts...))
			},
		},
		{
			Name: "ListIntentDefinitions", Disposition: disp("ListIntentDefinitions"),
			GRPC: func(ctx context.Context, opts ...clients.CallOption) (proto.Message, error) {
				return h.grpcRegistry.ListIntentDefinitions(ctx, &registryv1.ListIntentDefinitionsRequest{}, opts...)
			},
			Connect: func(ctx context.Context, opts ...clients.CallOption) (proto.Message, error) {
				return msgOrNil(h.connectRegistry.ListIntentDefinitions(ctx, &registryv1.ListIntentDefinitionsRequest{}, opts...))
			},
		},
		{
			Name: "GetIntentDefinition", Disposition: disp("GetIntentDefinition"),
			GRPC: func(ctx context.Context, opts ...clients.CallOption) (proto.Message, error) {
				return h.grpcRegistry.GetIntentDefinition(ctx, definitionRequest(), opts...)
			},
			Connect: func(ctx context.Context, opts ...clients.CallOption) (proto.Message, error) {
				return msgOrNil(h.connectRegistry.GetIntentDefinition(ctx, definitionRequest(), opts...))
			},
		},
		{
			Name: "ListCapabilities", Disposition: disp("ListCapabilities"),
			GRPC: func(ctx context.Context, opts ...clients.CallOption) (proto.Message, error) {
				return h.grpcRegistry.ListCapabilities(ctx, &registryv1.ListCapabilitiesRequest{}, opts...)
			},
			Connect: func(ctx context.Context, opts ...clients.CallOption) (proto.Message, error) {
				return msgOrNil(h.connectRegistry.ListCapabilities(ctx, &registryv1.ListCapabilitiesRequest{}, opts...))
			},
		},
		{
			Name: "GetCapability", Disposition: disp("GetCapability"),
			GRPC: func(ctx context.Context, opts ...clients.CallOption) (proto.Message, error) {
				return h.grpcRegistry.GetCapability(ctx, &registryv1.GetCapabilityRequest{CapabilityId: transporttest.KnownCapabilityID}, opts...)
			},
			Connect: func(ctx context.Context, opts ...clients.CallOption) (proto.Message, error) {
				return msgOrNil(h.connectRegistry.GetCapability(ctx, &registryv1.GetCapabilityRequest{CapabilityId: transporttest.KnownCapabilityID}, opts...))
			},
		},
	}
}

func recommendIntentActionRequest() *intentsv1.RecommendIntentActionRequest {
	return &intentsv1.RecommendIntentActionRequest{
		Scope: &commonv1.ScopeContext{
			TenantId: transporttest.Tenant, OrganizationScopeId: transporttest.OrganizationScopeID, Purpose: transporttest.PurposeAnalytics,
		},
		TenantId: transporttest.Tenant, OrganizationId: transporttest.OrganizationScopeID, Purpose: transporttest.PurposeAnalytics,
		Analysis: &intentsv1.AnalyticalResult{
			ResultId: "result-clients-fixture", RequestId: "request-clients-fixture",
			TenantId: transporttest.Tenant, OrganizationId: transporttest.OrganizationScopeID, Purpose: transporttest.PurposeAnalytics,
			DefinitionRef: "analysis.definition/v1", QueryRef: "query-fixture", QueryVersion: "1", QueryDigest: "sha256:query",
			CohortRef: "cohort-fixture", CohortVersion: "1", CohortDigest: "sha256:cohort",
			ModelRef: "model-fixture", ModelVersion: "1", ModelDigest: "sha256:model", ArtifactRef: "artifact-fixture",
			Digest: "sha256:analysis",
		},
		Action:     &intentsv1.RecommendedActionSpec{DefinitionRef: "action.definition/v1", CapabilityRef: transporttest.KnownCapabilityID, InputDigest: "sha256:action"},
		Population: &intentsv1.RecommendationPopulation{TenantId: transporttest.Tenant, Ref: "population-fixture", Version: "1", Digest: "sha256:population", Authorized: true},
		Governance: &intentsv1.RecommendationGovernance{DecisionRef: "governance-fixture", DecisionDigest: "sha256:governance", State: "ALLOW", ScopeDigest: "sha256:scope", Purpose: transporttest.PurposeAnalytics},
		Simulation: &intentsv1.RecommendationSimulation{SimulationRef: "simulation-fixture", SimulationDigest: "sha256:simulation", ActionInputDigest: "sha256:action", Status: "PASS"},
	}
}
