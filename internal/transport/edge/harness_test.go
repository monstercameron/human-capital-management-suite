package edge_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
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

	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
	registryv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/registry/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/clients"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/edge"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/grpcserver"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/transporttest"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// fixedRequestID is the correlation identifier both transports are told to
// use, so that two equivalent requests differ in nothing observable.
const fixedRequestID = "req-fixture-0001"

// baseTime pins the fixture clock. Nothing under test reads the wall clock for
// business time, so every run sees the same instants.
var baseTime = time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)

// fakeClock is a movable clock for credential-expiry scenarios.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

// Now returns the current fixture instant.
func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// Advance moves the fixture clock forward.
func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// harness stands both transports up in process over the same handler ports,
// the same verifier and the same admission configuration.
//
// The single shared transport.Config is the load-bearing part. The parity
// assertions in this package are only meaningful because there is exactly one
// configuration, one verifier, one validator and one pair of handlers behind
// both surfaces; anything that differs between the transports is therefore a
// difference the transport code introduced.
type harness struct {
	t testing.TB

	clock    *fakeClock
	verifier *trust.HMACVerifier
	intent   *transporttest.IntentHandler
	registry *transporttest.RegistryHandler

	grpcIntent   intentsv1.IntentServiceClient
	grpcRegistry registryv1.RegistryServiceClient

	edgeURL      string
	httpClient   *http.Client
	edgeIntent   *generatedIntentClient
	edgeRegistry *generatedRegistryClient
	edgeJSON     *generatedIntentClient

	token string

	mu      sync.Mutex
	records []transport.LogRecord
}

// generatedIntentClient is a test-only calling-convention shim over the
// canonical generated connect client (REV-003-01). The wire logic lives in
// internal/transport/clients; this type only preserves the connect.Request
// calling convention the edge tests already use, projecting request headers
// onto clients.CallOption and wrapping the decoded response back into a
// connect.Response. Every method delegates to exactly one generated method.
type generatedIntentClient struct {
	inner clients.IntentClient
}

// generatedRegistryClient is the RegistryService counterpart of
// generatedIntentClient: same delegation, same test-only scope.
type generatedRegistryClient struct {
	inner clients.RegistryClient
}

func callThrough[Req, Res any](
	ctx context.Context,
	req *connect.Request[Req],
	do func(context.Context, *Req, ...clients.CallOption) (*Res, error),
) (*connect.Response[Res], error) {
	var opts []clients.CallOption
	for key, values := range req.Header() {
		for _, value := range values {
			opts = append(opts, clients.WithHeader(key, value))
		}
	}
	res, err := do(ctx, req.Msg, opts...)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(res), nil
}

func (c *generatedIntentClient) CreateIntent(ctx context.Context, req *connect.Request[intentsv1.CreateIntentRequest]) (*connect.Response[intentsv1.CreateIntentResponse], error) {
	return callThrough(ctx, req, c.inner.CreateIntent)
}

func (c *generatedIntentClient) GetIntent(ctx context.Context, req *connect.Request[intentsv1.GetIntentRequest]) (*connect.Response[intentsv1.GetIntentResponse], error) {
	return callThrough(ctx, req, c.inner.GetIntent)
}

func (c *generatedIntentClient) ListIntents(ctx context.Context, req *connect.Request[intentsv1.ListIntentsRequest]) (*connect.Response[intentsv1.ListIntentsResponse], error) {
	return callThrough(ctx, req, c.inner.ListIntents)
}

func (c *generatedIntentClient) SimulateIntent(ctx context.Context, req *connect.Request[intentsv1.SimulateIntentRequest]) (*connect.Response[intentsv1.SimulateIntentResponse], error) {
	return callThrough(ctx, req, c.inner.SimulateIntent)
}

func (c *generatedIntentClient) ExecuteIntent(ctx context.Context, req *connect.Request[intentsv1.ExecuteIntentRequest]) (*connect.Response[intentsv1.ExecuteIntentResponse], error) {
	return callThrough(ctx, req, c.inner.ExecuteIntent)
}

func (c *generatedIntentClient) SubmitIntent(ctx context.Context, req *connect.Request[intentsv1.SubmitIntentRequest]) (*connect.Response[intentsv1.SubmitIntentResponse], error) {
	return callThrough(ctx, req, c.inner.SubmitIntent)
}

func (c *generatedIntentClient) CancelIntent(ctx context.Context, req *connect.Request[intentsv1.CancelIntentRequest]) (*connect.Response[intentsv1.CancelIntentResponse], error) {
	return callThrough(ctx, req, c.inner.CancelIntent)
}

func (c *generatedIntentClient) SupersedeIntent(ctx context.Context, req *connect.Request[intentsv1.SupersedeIntentRequest]) (*connect.Response[intentsv1.SupersedeIntentResponse], error) {
	return callThrough(ctx, req, c.inner.SupersedeIntent)
}

func (c *generatedIntentClient) ExplainIntent(ctx context.Context, req *connect.Request[intentsv1.ExplainIntentRequest]) (*connect.Response[intentsv1.ExplainIntentResponse], error) {
	return callThrough(ctx, req, c.inner.ExplainIntent)
}

func (c *generatedIntentClient) ListIntentTimeline(ctx context.Context, req *connect.Request[intentsv1.ListIntentTimelineRequest]) (*connect.Response[intentsv1.ListIntentTimelineResponse], error) {
	return callThrough(ctx, req, c.inner.ListIntentTimeline)
}

func (c *generatedRegistryClient) ListIntentDefinitions(ctx context.Context, req *connect.Request[registryv1.ListIntentDefinitionsRequest]) (*connect.Response[registryv1.ListIntentDefinitionsResponse], error) {
	return callThrough(ctx, req, c.inner.ListIntentDefinitions)
}

func (c *generatedRegistryClient) GetIntentDefinition(ctx context.Context, req *connect.Request[registryv1.GetIntentDefinitionRequest]) (*connect.Response[registryv1.GetIntentDefinitionResponse], error) {
	return callThrough(ctx, req, c.inner.GetIntentDefinition)
}

func (c *generatedRegistryClient) ListCapabilities(ctx context.Context, req *connect.Request[registryv1.ListCapabilitiesRequest]) (*connect.Response[registryv1.ListCapabilitiesResponse], error) {
	return callThrough(ctx, req, c.inner.ListCapabilities)
}

func (c *generatedRegistryClient) GetCapability(ctx context.Context, req *connect.Request[registryv1.GetCapabilityRequest]) (*connect.Response[registryv1.GetCapabilityResponse], error) {
	return callThrough(ctx, req, c.inner.GetCapability)
}

// newHarness builds the shared fixture.
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
	h.grpcIntent = intentsv1.NewIntentServiceClient(conn)
	h.grpcRegistry = registryv1.NewRegistryServiceClient(conn)

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
	h.edgeIntent = &generatedIntentClient{inner: clients.NewIntentClientConnect(h.httpClient, h.edgeURL)}
	h.edgeRegistry = &generatedRegistryClient{inner: clients.NewRegistryClientConnect(h.httpClient, h.edgeURL)}
	h.edgeJSON = &generatedIntentClient{inner: clients.NewIntentClientConnect(h.httpClient, h.edgeURL, connect.WithProtoJSON())}

	return h
}

// appendRecord collects one server-side log record.
func (h *harness) appendRecord(record transport.LogRecord) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, record)
}

// records returns a copy of the collected log records.
func (h *harness) logRecords() []transport.LogRecord {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]transport.LogRecord, len(h.records))
	copy(out, h.records)
	return out
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

// grpcContext attaches the fixture credential and request identifier to a
// gRPC call.
func (h *harness) grpcContext(ctx context.Context, extra ...string) context.Context {
	pairs := append([]string{
		transport.AuthorizationMetadataKey, h.token,
		transport.RequestIDMetadataKey, fixedRequestID,
	}, extra...)
	return metadata.AppendToOutgoingContext(ctx, pairs...)
}

// grpcContextWithoutCredential attaches only the request identifier.
func (h *harness) grpcContextWithoutCredential(ctx context.Context, extra ...string) context.Context {
	pairs := append([]string{transport.RequestIDMetadataKey, fixedRequestID}, extra...)
	return metadata.AppendToOutgoingContext(ctx, pairs...)
}

// edgeRequest wraps msg with the fixture credential and request identifier.
func edgeRequest[T any](h *harness, msg *T, extra ...string) *connect.Request[T] {
	req := connect.NewRequest(msg)
	req.Header().Set(transport.AuthorizationMetadataKey, h.token)
	req.Header().Set(transport.RequestIDMetadataKey, fixedRequestID)
	for i := 0; i+1 < len(extra); i += 2 {
		req.Header().Set(extra[i], extra[i+1])
	}
	return req
}

// edgeRequestWithoutCredential wraps msg without a credential.
func edgeRequestWithoutCredential[T any](msg *T, extra ...string) *connect.Request[T] {
	req := connect.NewRequest(msg)
	req.Header().Set(transport.RequestIDMetadataKey, fixedRequestID)
	for i := 0; i+1 < len(extra); i += 2 {
		req.Header().Set(extra[i], extra[i+1])
	}
	return req
}

// ownedFromGRPC extracts the owned error from a gRPC client failure.
func ownedFromGRPC(t *testing.T, err error) *envelope.Error {
	t.Helper()
	owned, ok := envelope.FromGRPC(err)
	if !ok {
		t.Fatalf("gRPC error %v is not a status error", err)
	}
	return owned
}

// ownedFromEdge extracts the owned error from an HTTP edge client failure.
// The generated connect backend already decodes into *envelope.Error, so an
// envelope is returned directly when present; the connect decoding path is
// kept for raw transport failures.
func ownedFromEdge(t *testing.T, err error) *envelope.Error {
	t.Helper()
	var owned *envelope.Error
	if errors.As(err, &owned) {
		return owned
	}
	owned, ok := edge.FromConnectError(err)
	if !ok {
		t.Fatalf("edge error %v is not a connect error", err)
	}
	return owned
}

// assertOwnedParity fails unless two owned errors mean exactly the same thing.
// Equal transport status codes alone are explicitly not sufficient parity, per
// the endpoint contract's required test layers.
func assertOwnedParity(t *testing.T, label string, viaGRPC, viaEdge *envelope.Error) {
	t.Helper()
	if viaGRPC.Code() != viaEdge.Code() {
		t.Errorf("%s: owned code differs: grpc=%v edge=%v", label, viaGRPC.Code(), viaEdge.Code())
	}
	if viaGRPC.Retryable() != viaEdge.Retryable() {
		t.Errorf("%s: retryability differs: grpc=%v edge=%v", label, viaGRPC.Retryable(), viaEdge.Retryable())
	}
	if viaGRPC.Message() != viaEdge.Message() {
		t.Errorf("%s: message differs: grpc=%q edge=%q", label, viaGRPC.Message(), viaEdge.Message())
	}
	if viaGRPC.CorrelationID() != viaEdge.CorrelationID() {
		t.Errorf("%s: correlation differs: grpc=%q edge=%q", label, viaGRPC.CorrelationID(), viaEdge.CorrelationID())
	}
	if viaGRPC.EvidenceRef() != viaEdge.EvidenceRef() {
		t.Errorf("%s: evidence differs: grpc=%+v edge=%+v", label, viaGRPC.EvidenceRef(), viaEdge.EvidenceRef())
	}
	grpcViolations, edgeViolations := viaGRPC.Violations(), viaEdge.Violations()
	if len(grpcViolations) != len(edgeViolations) {
		t.Fatalf("%s: violation count differs: grpc=%d edge=%d (%v vs %v)",
			label, len(grpcViolations), len(edgeViolations), grpcViolations, edgeViolations)
	}
	for i := range grpcViolations {
		if grpcViolations[i] != edgeViolations[i] {
			t.Errorf("%s: violation %d differs: grpc=%+v edge=%+v", label, i, grpcViolations[i], edgeViolations[i])
		}
	}
	if viaGRPC.HTTPStatus() != viaEdge.HTTPStatus() {
		t.Errorf("%s: projected HTTP status differs: grpc=%d edge=%d", label, viaGRPC.HTTPStatus(), viaEdge.HTTPStatus())
	}
}

// postJSON sends a raw JSON body to a procedure, bypassing the typed client so
// that malformed encodings can be presented at all.
func (h *harness) postJSON(tb testing.TB, procedure, body string, headers map[string]string) (int, []byte) {
	tb.Helper()
	req, err := http.NewRequest(http.MethodPost, h.edgeURL+procedure, bytes.NewReader([]byte(body)))
	if err != nil {
		tb.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(transport.AuthorizationMetadataKey, h.token)
	req.Header.Set(transport.RequestIDMetadataKey, fixedRequestID)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := h.httpClient.Do(req)
	if err != nil {
		tb.Fatalf("Do: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		tb.Fatalf("ReadAll: %v", err)
	}
	return resp.StatusCode, payload
}

// wireError is the connect unary error body. Decoding it directly is how the
// tests assert on what a plain HTTP client actually receives, rather than on
// what the typed client reconstructs.
type wireError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Details []struct {
		Type  string          `json:"type"`
		Value string          `json:"value"`
		Debug json.RawMessage `json:"debug,omitempty"`
	} `json:"details"`
}

// decodeWireError parses a connect error body.
func decodeWireError(t *testing.T, payload []byte) wireError {
	t.Helper()
	var out wireError
	if err := json.Unmarshal(payload, &out); err != nil {
		t.Fatalf("decoding connect error body %q: %v", payload, err)
	}
	return out
}

// clone returns a deep copy of msg, so the same vector can be sent to both
// transports without either one observing the other's server-side rewrites.
func clone[T proto.Message](msg T) T {
	return proto.Clone(msg).(T)
}
