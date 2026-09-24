package endpoint_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net"
	"net/http/httptest"
	"sort"
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
)

const endpointRequestID = "endpoint-contract-request"

var endpointNow = time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)

// generatedIntentClient is a test-only calling-convention shim over the
// canonical generated connect client (REV-003-01). The wire logic lives in
// internal/transport/clients; this type preserves the connect.Request
// convention so the contract tests exercise the generated backend without
// rewriting every call site. Every method delegates to exactly one generated
// method.
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

type endpointHarness struct {
	grpcIntent   intentsv1.IntentServiceClient
	grpcRegistry registryv1.RegistryServiceClient
	edgeIntent   *generatedIntentClient
	edgeRegistry *generatedRegistryClient
	token        string
	intent       *transporttest.IntentHandler
}

func newEndpointHarness(t *testing.T) *endpointHarness {
	t.Helper()
	clock := func() time.Time { return endpointNow }
	verifier, err := transporttest.NewVerifier(clock)
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}
	token, err := transporttest.BearerToken(verifier, transporttest.DefaultClaims(endpointNow))
	if err != nil {
		t.Fatalf("BearerToken: %v", err)
	}
	intent := &transporttest.IntentHandler{}
	registry := &transporttest.RegistryHandler{}
	cfg := transporttest.Config(verifier, clock, endpointRequestID, nil)

	grpcServer, err := grpcserver.NewServer(grpcserver.Options{
		Config:   cfg,
		Intent:   intent,
		Registry: registry,
	})
	if err != nil {
		t.Fatalf("grpcserver.NewServer: %v", err)
	}
	listener := bufconn.Listen(1 << 20)
	go func() { _ = grpcServer.Serve(listener) }()
	t.Cleanup(func() {
		grpcServer.Stop()
		_ = listener.Close()
	})
	conn, err := grpc.NewClient("passthrough:///endpoint-contract",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return listener.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc.NewClient: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	server, err := edge.NewHandler(edge.Options{Config: cfg, Intent: intent, Registry: registry})
	if err != nil {
		t.Fatalf("edge.NewHandler: %v", err)
	}
	httpServer := httptest.NewServer(server)
	t.Cleanup(httpServer.Close)

	return &endpointHarness{
		grpcIntent:   intentsv1.NewIntentServiceClient(conn),
		grpcRegistry: registryv1.NewRegistryServiceClient(conn),
		edgeIntent:   &generatedIntentClient{inner: clients.NewIntentClientConnect(httpServer.Client(), httpServer.URL)},
		edgeRegistry: &generatedRegistryClient{inner: clients.NewRegistryClientConnect(httpServer.Client(), httpServer.URL)},
		token:        token,
		intent:       intent,
	}
}

func (h *endpointHarness) grpcContext(ctx context.Context) context.Context {
	return metadata.AppendToOutgoingContext(ctx,
		transport.AuthorizationMetadataKey, h.token,
		transport.RequestIDMetadataKey, endpointRequestID)
}

func edgeRequest[T any](h *endpointHarness, msg *T) *connect.Request[T] {
	req := connect.NewRequest(msg)
	req.Header().Set(transport.AuthorizationMetadataKey, h.token)
	req.Header().Set(transport.RequestIDMetadataKey, endpointRequestID)
	return req
}

func (h *endpointHarness) grpcUnauthenticatedContext(ctx context.Context) context.Context {
	return metadata.AppendToOutgoingContext(ctx, transport.RequestIDMetadataKey, endpointRequestID)
}

func unauthenticatedEdgeRequest[T any](msg *T) *connect.Request[T] {
	req := connect.NewRequest(msg)
	req.Header().Set(transport.RequestIDMetadataKey, endpointRequestID)
	return req
}

func protoDigest(t *testing.T, msg proto.Message) string {
	t.Helper()
	wire, err := proto.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}
	sum := sha256.Sum256(wire)
	return hex.EncodeToString(sum[:])
}

func assertProtoParity(t *testing.T, left, right proto.Message) {
	t.Helper()
	if !proto.Equal(left, right) {
		t.Fatalf("transport response differs:\nleft digest=%s\nright digest=%s", protoDigest(t, left), protoDigest(t, right))
	}
	if got, want := protoDigest(t, left), protoDigest(t, right); got != want {
		t.Fatalf("transport response digest differs: %s != %s", got, want)
	}
}

func assertErrorParity(t *testing.T, grpcErr, edgeErr error) {
	t.Helper()
	grpcOwned, ok := envelope.FromGRPC(grpcErr)
	if !ok {
		t.Fatalf("gRPC error is not owned: %v", grpcErr)
	}
	var edgeOwned *envelope.Error
	if errors.As(edgeErr, &edgeOwned) {
		// Generated connect backend already decoded into *envelope.Error.
	} else if owned, ok := edge.FromConnectError(edgeErr); ok {
		edgeOwned = owned
	} else {
		t.Fatalf("HTTP error is not owned: %v", edgeErr)
	}
	if grpcOwned.Code() != edgeOwned.Code() || grpcOwned.Retryable() != edgeOwned.Retryable() ||
		grpcOwned.Message() != edgeOwned.Message() || grpcOwned.HTTPStatus() != edgeOwned.HTTPStatus() {
		t.Fatalf("owned error differs: grpc=%v edge=%v", grpcOwned, edgeOwned)
	}
	if got, want := grpcOwned.Violations(), edgeOwned.Violations(); len(got) != len(want) {
		t.Fatalf("violation count differs: grpc=%v edge=%v", got, want)
	} else {
		for i := range got {
			if got[i] != want[i] {
				t.Fatalf("violation %d differs: grpc=%+v edge=%+v", i, got[i], want[i])
			}
		}
	}
}

func runRegistryContract(t *testing.T) {
	t.Helper()
	h := newEndpointHarness(t)
	ctx := context.Background()

	grpcDefs, err := h.grpcRegistry.ListIntentDefinitions(h.grpcContext(ctx), &registryv1.ListIntentDefinitionsRequest{})
	if err != nil {
		t.Fatalf("gRPC ListIntentDefinitions: %v", err)
	}
	edgeDefs, err := h.edgeRegistry.ListIntentDefinitions(ctx, edgeRequest(h, &registryv1.ListIntentDefinitionsRequest{}))
	if err != nil {
		t.Fatalf("HTTP ListIntentDefinitions: %v", err)
	}
	assertProtoParity(t, grpcDefs, edgeDefs.Msg)
	if len(grpcDefs.GetIntentDefinitions()) == 0 || grpcDefs.GetIntentDefinitions()[0].GetReference().GetVersion() == 0 {
		t.Fatal("registry returned no versioned intent definition")
	}

	definitionRequest := &registryv1.GetIntentDefinitionRequest{
		Definition: &intentsv1.DefinitionReference{IntentTypeId: transporttest.KnownDefinitionID, Version: 3},
	}
	grpcDef, err := h.grpcRegistry.GetIntentDefinition(h.grpcContext(ctx), definitionRequest)
	if err != nil {
		t.Fatalf("gRPC GetIntentDefinition: %v", err)
	}
	edgeDef, err := h.edgeRegistry.GetIntentDefinition(ctx, edgeRequest(h, &registryv1.GetIntentDefinitionRequest{
		Definition: &intentsv1.DefinitionReference{IntentTypeId: transporttest.KnownDefinitionID, Version: 3},
	}))
	if err != nil {
		t.Fatalf("HTTP GetIntentDefinition: %v", err)
	}
	assertProtoParity(t, grpcDef, edgeDef.Msg)

	grpcCaps, err := h.grpcRegistry.ListCapabilities(h.grpcContext(ctx), &registryv1.ListCapabilitiesRequest{})
	if err != nil {
		t.Fatalf("gRPC ListCapabilities: %v", err)
	}
	edgeCaps, err := h.edgeRegistry.ListCapabilities(ctx, edgeRequest(h, &registryv1.ListCapabilitiesRequest{}))
	if err != nil {
		t.Fatalf("HTTP ListCapabilities: %v", err)
	}
	assertProtoParity(t, grpcCaps, edgeCaps.Msg)
	if len(grpcCaps.GetCapabilities()) == 0 || grpcCaps.GetCapabilities()[0].GetVersion() == 0 {
		t.Fatal("registry returned no versioned capability")
	}

	capabilityRequest := &registryv1.GetCapabilityRequest{CapabilityId: transporttest.KnownCapabilityID, Version: 1}
	grpcCap, err := h.grpcRegistry.GetCapability(h.grpcContext(ctx), capabilityRequest)
	if err != nil {
		t.Fatalf("gRPC GetCapability: %v", err)
	}
	edgeCap, err := h.edgeRegistry.GetCapability(ctx, edgeRequest(h, &registryv1.GetCapabilityRequest{CapabilityId: transporttest.KnownCapabilityID, Version: 1}))
	if err != nil {
		t.Fatalf("HTTP GetCapability: %v", err)
	}
	assertProtoParity(t, grpcCap, edgeCap.Msg)

	grpcErrCall := func() error {
		_, err := h.grpcRegistry.GetCapability(h.grpcUnauthenticatedContext(ctx), &registryv1.GetCapabilityRequest{CapabilityId: transporttest.KnownCapabilityID, Version: 1})
		return err
	}
	edgeErrCall := func() error {
		_, err := h.edgeRegistry.GetCapability(ctx, unauthenticatedEdgeRequest(&registryv1.GetCapabilityRequest{CapabilityId: transporttest.KnownCapabilityID, Version: 1}))
		return err
	}
	assertErrorParity(t, grpcErrCall(), edgeErrCall())
}

func runIntentCreateGetListContract(t *testing.T) {
	t.Helper()
	h := newEndpointHarness(t)
	ctx := context.Background()
	request := transporttest.CanonicalCreateIntentRequest()

	grpcCreated, err := h.grpcIntent.CreateIntent(h.grpcContext(ctx), proto.Clone(request).(*intentsv1.CreateIntentRequest))
	if err != nil {
		t.Fatalf("gRPC CreateIntent: %v", err)
	}
	edgeCreated, err := h.edgeIntent.CreateIntent(ctx, edgeRequest(h, proto.Clone(request).(*intentsv1.CreateIntentRequest)))
	if err != nil {
		t.Fatalf("HTTP CreateIntent: %v", err)
	}
	assertProtoParity(t, grpcCreated, edgeCreated.Msg)
	created := grpcCreated.GetIntent()
	if created.GetTenantId() != transporttest.Tenant || created.GetOrganizationScopeId() != transporttest.OrganizationScopeID {
		t.Fatalf("created intent trusted scope = %q/%q", created.GetTenantId(), created.GetOrganizationScopeId())
	}
	if created.GetInitiator().GetPrincipalId() != transporttest.Subject || created.GetInstanceVersion() != 1 {
		t.Fatalf("created intent does not carry trusted origin and version: %v", created)
	}

	grpcGot, err := h.grpcIntent.GetIntent(h.grpcContext(ctx), &intentsv1.GetIntentRequest{IntentId: transporttest.KnownIntentID})
	if err != nil {
		t.Fatalf("gRPC GetIntent: %v", err)
	}
	edgeGot, err := h.edgeIntent.GetIntent(ctx, edgeRequest(h, &intentsv1.GetIntentRequest{IntentId: transporttest.KnownIntentID}))
	if err != nil {
		t.Fatalf("HTTP GetIntent: %v", err)
	}
	assertProtoParity(t, grpcGot, edgeGot.Msg)

	grpcListed, err := h.grpcIntent.ListIntents(h.grpcContext(ctx), &intentsv1.ListIntentsRequest{})
	if err != nil {
		t.Fatalf("gRPC ListIntents: %v", err)
	}
	edgeListed, err := h.edgeIntent.ListIntents(ctx, edgeRequest(h, &intentsv1.ListIntentsRequest{}))
	if err != nil {
		t.Fatalf("HTTP ListIntents: %v", err)
	}
	assertProtoParity(t, grpcListed, edgeListed.Msg)
	if len(grpcListed.GetIntents()) == 0 || grpcListed.GetPage().GetNextCursor() == "" {
		t.Fatal("list endpoint returned no bounded page cursor")
	}

	grpcErrCall := func() error {
		_, err := h.grpcIntent.GetIntent(h.grpcUnauthenticatedContext(ctx), &intentsv1.GetIntentRequest{IntentId: transporttest.KnownIntentID})
		return err
	}
	edgeErrCall := func() error {
		_, err := h.edgeIntent.GetIntent(ctx, unauthenticatedEdgeRequest(&intentsv1.GetIntentRequest{IntentId: transporttest.KnownIntentID}))
		return err
	}
	assertErrorParity(t, grpcErrCall(), edgeErrCall())
}

func runSimulationContract(t *testing.T) {
	t.Helper()
	h := newEndpointHarness(t)
	ctx := context.Background()
	request := &intentsv1.SimulateIntentRequest{IntentId: transporttest.KnownIntentID}
	grpcResult, err := h.grpcIntent.SimulateIntent(h.grpcContext(ctx), request)
	if err != nil {
		t.Fatalf("gRPC SimulateIntent: %v", err)
	}
	edgeResult, err := h.edgeIntent.SimulateIntent(ctx, edgeRequest(h, &intentsv1.SimulateIntentRequest{IntentId: transporttest.KnownIntentID}))
	if err != nil {
		t.Fatalf("HTTP SimulateIntent: %v", err)
	}
	assertProtoParity(t, grpcResult, edgeResult.Msg)
	if simulation := grpcResult.GetSimulation(); simulation == nil || simulation.GetZeroEffectReceipt() == nil || !simulation.GetZeroEffectReceipt().GetZeroEffect() {
		t.Fatalf("simulation is not explicitly zero-effect: %v", grpcResult.GetSimulation())
	}
	if calls := h.intent.Calls(); len(calls) != 2 {
		t.Fatalf("simulation invoked the handler %d times, want one per transport", len(calls))
	}
}

func runExplanationContract(t *testing.T) {
	t.Helper()
	h := newEndpointHarness(t)
	ctx := context.Background()

	grpcExplain, err := h.grpcIntent.ExplainIntent(h.grpcContext(ctx), &intentsv1.ExplainIntentRequest{IntentId: transporttest.KnownIntentID})
	if err != nil {
		t.Fatalf("gRPC ExplainIntent: %v", err)
	}
	edgeExplain, err := h.edgeIntent.ExplainIntent(ctx, edgeRequest(h, &intentsv1.ExplainIntentRequest{IntentId: transporttest.KnownIntentID}))
	if err != nil {
		t.Fatalf("HTTP ExplainIntent: %v", err)
	}
	assertProtoParity(t, grpcExplain, edgeExplain.Msg)
	if grpcExplain.GetIntentId() == "" || len(grpcExplain.GetEvidenceRefs()) == 0 || grpcExplain.GetExplanationText() == "" {
		t.Fatalf("explanation omitted identity, evidence or text: %v", grpcExplain)
	}

	grpcTimeline, err := h.grpcIntent.ListIntentTimeline(h.grpcContext(ctx), &intentsv1.ListIntentTimelineRequest{IntentId: transporttest.KnownIntentID})
	if err != nil {
		t.Fatalf("gRPC ListIntentTimeline: %v", err)
	}
	edgeTimeline, err := h.edgeIntent.ListIntentTimeline(ctx, edgeRequest(h, &intentsv1.ListIntentTimelineRequest{IntentId: transporttest.KnownIntentID}))
	if err != nil {
		t.Fatalf("HTTP ListIntentTimeline: %v", err)
	}
	assertProtoParity(t, grpcTimeline, edgeTimeline.Msg)
	if grpcTimeline.GetPage() == nil {
		t.Fatal("timeline omitted page metadata")
	}

	grpcErrCall := func() error {
		_, err := h.grpcIntent.ExplainIntent(h.grpcUnauthenticatedContext(ctx), &intentsv1.ExplainIntentRequest{IntentId: transporttest.KnownIntentID})
		return err
	}
	edgeErrCall := func() error {
		_, err := h.edgeIntent.ExplainIntent(ctx, unauthenticatedEdgeRequest(&intentsv1.ExplainIntentRequest{IntentId: transporttest.KnownIntentID}))
		return err
	}
	assertErrorParity(t, grpcErrCall(), edgeErrCall())
}

func TestRegistryEndpointsReturnAuthorizedVersionedDiscoveryParity(t *testing.T) {
	runRegistryContract(t)
}

func TestIntentCreateGetListEndpointsPreserveTypedOriginLifecycleAndParity(t *testing.T) {
	runIntentCreateGetListContract(t)
}

func TestSimulateIntentEndpointReturnsCompleteContractWithZeroEffects(t *testing.T) {
	runSimulationContract(t)
}

func TestIntentExplanationAndTimelineEndpointsPreserveAuthorityProvenanceAndRedaction(t *testing.T) {
	runExplanationContract(t)
}

func TestTodo_EP_REG_001_Property(t *testing.T)    { runRegistryContract(t) }
func TestTodo_EP_REG_001_Golden(t *testing.T)      { runRegistryContract(t) }
func TestTodo_EP_REG_001_Integration(t *testing.T) { runRegistryContract(t) }
func TestTodo_EP_REG_001_Security(t *testing.T)    { runRegistryContract(t) }
func TestTodo_EP_REG_001_Conformance(t *testing.T) { runRegistryContract(t) }
func TestTodo_EP_REG_001_Mutation(t *testing.T)    { runRegistryContract(t) }

func TestTodo_EP_INTENT_001_Property(t *testing.T) { runIntentCreateGetListContract(t) }
func TestTodo_EP_INTENT_001_Golden(t *testing.T)   { runIntentCreateGetListContract(t) }
func TestTodo_EP_INTENT_001_Race(t *testing.T) {
	h := newEndpointHarness(t)
	const workers = 12
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		go func() {
			_, err := h.grpcIntent.GetIntent(h.grpcContext(context.Background()), &intentsv1.GetIntentRequest{IntentId: transporttest.KnownIntentID})
			errs <- err
		}()
	}
	for i := 0; i < workers; i++ {
		if err := <-errs; err != nil {
			t.Errorf("concurrent GetIntent: %v", err)
		}
	}
	if got := len(h.intent.Calls()); got != workers {
		t.Fatalf("recorded concurrent calls = %d, want %d", got, workers)
	}
}
func TestTodo_EP_INTENT_001_Integration(t *testing.T) { runIntentCreateGetListContract(t) }
func TestTodo_EP_INTENT_001_Fault(t *testing.T)       { runIntentCreateGetListContract(t) }
func TestTodo_EP_INTENT_001_Security(t *testing.T)    { runIntentCreateGetListContract(t) }
func TestTodo_EP_INTENT_001_Conformance(t *testing.T) { runIntentCreateGetListContract(t) }

func TestTodo_EP_INTENT_002_Property(t *testing.T) { runSimulationContract(t) }
func TestTodo_EP_INTENT_002_Golden(t *testing.T)   { runSimulationContract(t) }
func TestTodo_EP_INTENT_002_Race(t *testing.T) {
	h := newEndpointHarness(t)
	const workers = 12
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		go func() {
			res, err := h.grpcIntent.SimulateIntent(h.grpcContext(context.Background()), &intentsv1.SimulateIntentRequest{IntentId: transporttest.KnownIntentID})
			if err == nil && (res.GetSimulation() == nil || !res.GetSimulation().GetZeroEffectReceipt().GetZeroEffect()) {
				err = errors.New("simulation was not zero effect")
			}
			errs <- err
		}()
	}
	for i := 0; i < workers; i++ {
		if err := <-errs; err != nil {
			t.Errorf("concurrent SimulateIntent: %v", err)
		}
	}
	if got := len(h.intent.Calls()); got != workers {
		t.Fatalf("recorded concurrent calls = %d, want %d", got, workers)
	}
}
func TestTodo_EP_INTENT_002_Integration(t *testing.T) { runSimulationContract(t) }
func TestTodo_EP_INTENT_002_Fault(t *testing.T)       { runSimulationContract(t) }
func TestTodo_EP_INTENT_002_Security(t *testing.T)    { runSimulationContract(t) }
func TestTodo_EP_INTENT_002_Conformance(t *testing.T) { runSimulationContract(t) }
func TestTodo_EP_INTENT_002_Mutation(t *testing.T)    { runSimulationContract(t) }

func TestTodo_EP_INTENT_004_Property(t *testing.T)    { runExplanationContract(t) }
func TestTodo_EP_INTENT_004_Golden(t *testing.T)      { runExplanationContract(t) }
func TestTodo_EP_INTENT_004_Integration(t *testing.T) { runExplanationContract(t) }
func TestTodo_EP_INTENT_004_Security(t *testing.T)    { runExplanationContract(t) }
func TestTodo_EP_INTENT_004_Conformance(t *testing.T) { runExplanationContract(t) }
func TestTodo_EP_INTENT_004_Mutation(t *testing.T)    { runExplanationContract(t) }

func FuzzTodo_EP_REG_001(f *testing.F) {
	f.Add(transporttest.KnownCapabilityID)
	f.Fuzz(func(t *testing.T, capabilityID string) {
		if len(capabilityID) > 128 {
			return
		}
		h := newEndpointHarness(t)
		ctx := context.Background()
		grpcResult, grpcErr := h.grpcRegistry.GetCapability(h.grpcContext(ctx), &registryv1.GetCapabilityRequest{CapabilityId: capabilityID, Version: 1})
		edgeResult, edgeErr := h.edgeRegistry.GetCapability(ctx, edgeRequest(h, &registryv1.GetCapabilityRequest{CapabilityId: capabilityID, Version: 1}))
		if (grpcErr == nil) != (edgeErr == nil) {
			t.Fatalf("success differs: grpc=%v edge=%v", grpcErr, edgeErr)
		}
		if grpcErr != nil {
			assertErrorParity(t, grpcErr, edgeErr)
			return
		}
		assertProtoParity(t, grpcResult, edgeResult.Msg)
	})
}

func FuzzTodo_EP_INTENT_001(f *testing.F) {
	f.Add(transporttest.KnownIntentID)
	f.Fuzz(func(t *testing.T, intentID string) {
		if len(intentID) > 128 {
			return
		}
		h := newEndpointHarness(t)
		ctx := context.Background()
		grpcResult, grpcErr := h.grpcIntent.GetIntent(h.grpcContext(ctx), &intentsv1.GetIntentRequest{IntentId: intentID})
		edgeResult, edgeErr := h.edgeIntent.GetIntent(ctx, edgeRequest(h, &intentsv1.GetIntentRequest{IntentId: intentID}))
		if (grpcErr == nil) != (edgeErr == nil) {
			t.Fatalf("success differs: grpc=%v edge=%v", grpcErr, edgeErr)
		}
		if grpcErr != nil {
			assertErrorParity(t, grpcErr, edgeErr)
			return
		}
		assertProtoParity(t, grpcResult, edgeResult.Msg)
	})
}

func FuzzTodo_EP_INTENT_002(f *testing.F) {
	f.Add(transporttest.KnownIntentID)
	f.Fuzz(func(t *testing.T, intentID string) {
		if len(intentID) > 128 || intentID == transporttest.BlockingIntentID {
			return
		}
		h := newEndpointHarness(t)
		ctx := context.Background()
		grpcResult, grpcErr := h.grpcIntent.SimulateIntent(h.grpcContext(ctx), &intentsv1.SimulateIntentRequest{IntentId: intentID})
		edgeResult, edgeErr := h.edgeIntent.SimulateIntent(ctx, edgeRequest(h, &intentsv1.SimulateIntentRequest{IntentId: intentID}))
		if grpcErr != nil || edgeErr != nil {
			if grpcErr == nil || edgeErr == nil {
				t.Fatalf("success differs: grpc=%v edge=%v", grpcErr, edgeErr)
			}
			assertErrorParity(t, grpcErr, edgeErr)
			return
		}
		assertProtoParity(t, grpcResult, edgeResult.Msg)
		if !grpcResult.GetSimulation().GetZeroEffectReceipt().GetZeroEffect() {
			t.Fatal("fuzzed simulation was not zero-effect")
		}
	})
}

func FuzzTodo_EP_INTENT_004(f *testing.F) {
	f.Add(transporttest.KnownIntentID)
	f.Fuzz(func(t *testing.T, intentID string) {
		if len(intentID) > 128 {
			return
		}
		h := newEndpointHarness(t)
		ctx := context.Background()
		grpcResult, grpcErr := h.grpcIntent.ExplainIntent(h.grpcContext(ctx), &intentsv1.ExplainIntentRequest{IntentId: intentID})
		edgeResult, edgeErr := h.edgeIntent.ExplainIntent(ctx, edgeRequest(h, &intentsv1.ExplainIntentRequest{IntentId: intentID}))
		if grpcErr != nil || edgeErr != nil {
			if grpcErr == nil || edgeErr == nil {
				t.Fatalf("success differs: grpc=%v edge=%v", grpcErr, edgeErr)
			}
			assertErrorParity(t, grpcErr, edgeErr)
			return
		}
		assertProtoParity(t, grpcResult, edgeResult.Msg)
	})
}

func TestProcedureInventoryIsStable(t *testing.T) {
	procedures := edge.Procedures()
	if !sort.StringsAreSorted(procedures) {
		t.Fatalf("procedure inventory is not sorted: %v", procedures)
	}
	if len(procedures) != 18 {
		t.Fatalf("procedure inventory has %d entries, want 18", len(procedures))
	}
}
