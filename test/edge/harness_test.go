// Package edge_test is the EDGE-001 conformance suite: proof that native
// gRPC, the HTTP/Connect edge and the gRPC-over-WebSocket tunnel run one
// admission pipeline, not three.
//
// It exists because that claim is not true of any one package. It is true of
// a composed cell serving all three surfaces the way cmd/hcmnext serves them:
// a native *grpc.Server on its own TCP listener, the same *grpc.Server's
// interceptor chain reached a second way through the tunnel's WebSocket
// bridge, and the HTTP/Connect edge on the same mux the tunnel is mounted
// beside. internal/transport/transporttest's conformance kit
// (RefusalMatrix/AssertConformant) names the eight requests every edge must
// answer identically; this package is what drives them over three real wire
// transports against one real, ephemeral-PostgreSQL-backed cell.
//
// EDGE-004 (browser Origin/Host/CSRF policy) is a separate, later todo. This
// suite does not test cross-origin behavior beyond what already exists in
// test/tunnel; it tests that the three edges agree on admission.
package edge_test

import (
	"context"
	"maps"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoGRPCBridge/pkg/grpctunnel"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"

	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/fixtures"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	transportcell "github.com/monstercameron/human-capital-management-suite/internal/transport/cell"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/clients"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	transporthumanwork "github.com/monstercameron/human-capital-management-suite/internal/transport/humanwork"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/transporttest"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

// Fixture identity. The tenant is the corpus tenant, matching every other
// suite under test/ that composes a real cell.
const (
	testTenant   = string(fixtures.Tenant)
	testIssuer   = "https://issuer.test.hcm-next.invalid"
	testAudience = "hcm-next-api"
	testSubject  = "user-0191f3c4"
	testOrgScope = "org-north-america"
	testCellID   = "cell-edge001-test"

	// defaultCallDeadline bounds a scenario call that does not itself set a
	// deadline. It has nothing to do with the server's own admission cap
	// (CellConfig.MaxDeadline below); it only keeps a stuck test from hanging.
	defaultCallDeadline = 10 * time.Second
)

// testSigningKey never leaves this package and authenticates nothing outside
// a test process.
var testSigningKey = []byte("hcm-next-edge001-suite-signing-key-32+++")

// baseTime pins the credential window and the cell's business clock.
var baseTime = time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)

// harness is one composed, running cell serving all three published
// admission edges: a native gRPC listener, the HTTP/Connect edge, and the
// gRPC-over-WebSocket tunnel mounted beside it - exactly the topology
// cmd/hcmnext serves (main.go dials grpcListener and httpListener from the
// same transportcell.NewGRPCServerWithWorkflowInspector /
// transportcell.NewEdgeHandlerWithTunnel call pair this harness makes).
type harness struct {
	t testing.TB

	verifier *trust.HMACVerifier
	token    string // "Bearer <credential>", the fixture's ordinary valid credential

	grpcIntent   intentsv1.IntentServiceClient // native gRPC, over a real TCP listener
	edgeIntent   clients.IntentClient          // HTTP/Connect edge
	tunnelIntent intentsv1.IntentServiceClient // gRPC frames over the WebSocket tunnel
}

// newHarness migrates a private schema, composes the cell exactly as
// cmd/hcmnext does, and serves it on all three edges.
func newHarness(t *testing.T) *harness {
	t.Helper()

	db := pgtest.New(t)
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

	composed, err := app.NewCell(app.CellConfig{
		Store:       store,
		Verifier:    verifier,
		Audience:    testAudience,
		MaxDeadline: 5 * time.Second,
		Now:         func() time.Time { return baseTime },
	})
	if err != nil {
		t.Fatalf("app.NewCell: %v", err)
	}

	grpcServer, err := transportcell.NewGRPCServer(composed)
	if err != nil {
		t.Fatalf("NewGRPCServer: %v", err)
	}
	t.Cleanup(grpcServer.Stop)

	// The native gRPC edge: a real TCP listener, exactly like cmd/hcmnext's
	// grpcListener. This is what makes "native gRPC" a genuinely different
	// physical entry point from the tunnel below, even though both reach the
	// identical interceptor chain on the identical *grpc.Server.
	grpcListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen (grpc): %v", err)
	}
	go func() { _ = grpcServer.Serve(grpcListener) }()
	t.Cleanup(func() { _ = grpcListener.Close() })

	nativeConn, err := grpc.NewClient(grpcListener.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc.NewClient (native): %v", err)
	}
	t.Cleanup(func() { _ = nativeConn.Close() })

	// The HTTP/Connect edge and the tunnel, mounted on one mux, exactly like
	// cmd/hcmnext's httpListener. The tunnel bridges the workspace-only
	// server, never the native one; both reach the identical interceptor
	// chain, only the registered services differ.
	tunnelServer, err := transportcell.NewTunnelGRPCServer(composed, nil, nil, nil, nil, transporthumanwork.WritePorts{}, nil)
	if err != nil {
		t.Fatalf("NewTunnelGRPCServer: %v", err)
	}
	t.Cleanup(tunnelServer.Stop)
	edgeHandler, err := transportcell.NewEdgeHandlerWithTunnel(composed, tunnelServer)
	if err != nil {
		t.Fatalf("NewEdgeHandlerWithTunnel: %v", err)
	}
	httpServer := httptest.NewServer(edgeHandler)
	t.Cleanup(httpServer.Close)

	token, err := verifier.Issue(trust.Claims{
		Issuer:               testIssuer,
		Audience:             testAudience,
		Subject:              testSubject,
		SubjectKind:          "human",
		Tenant:               testTenant,
		OrganizationScopeID:  testOrgScope,
		Roles:                []string{"intent_author", string(authz.RoleCompAdmin)},
		AuthorityRefs:        []string{"authority:position:vp-people"},
		Purposes:             []string{authz.PurposeCompensationReview, "workforce_analytics"},
		AuthenticationMethod: "bearer_token",
		Assurance:            "substantial",
		SessionRef:           "session-edge001",
		IssuedAtUnix:         baseTime.Add(-time.Minute).Unix(),
		ExpiresAtUnix:        baseTime.Add(time.Hour).Unix(),
	})
	if err != nil {
		t.Fatalf("issue credential: %v", err)
	}
	bearer := "Bearer " + token

	// The tunnel is upgraded exactly once, with a valid credential, mirroring
	// the browser-resident client's own protocol (test/tunnel's suite covers
	// the upgrade admission itself in detail; this suite's concern is the
	// per-RPC admission that runs identically after the socket exists).
	// Per-RPC credentials for each matrix scenario travel as call metadata
	// below, never as anything carried over from this upgrade.
	tunnelHost := strings.TrimPrefix(httpServer.URL, "http://")
	tunnelConn, err := grpctunnel.BuildTunnelConn(context.Background(), grpctunnel.TunnelConfig{
		Target:           "ws://" + tunnelHost + transportcell.TunnelPath,
		Headers:          http.Header{"Authorization": []string{bearer}, "Origin": []string{"http://" + tunnelHost}},
		HandshakeTimeout: 10 * time.Second,
		GRPCOptions: []grpc.DialOption{
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		},
	})
	if err != nil {
		t.Fatalf("BuildTunnelConn: %v", err)
	}
	t.Cleanup(func() { _ = tunnelConn.Close() })

	return &harness{
		t:            t,
		verifier:     verifier,
		token:        bearer,
		grpcIntent:   intentsv1.NewIntentServiceClient(nativeConn),
		edgeIntent:   clients.NewIntentClientConnect(httpServer.Client(), httpServer.URL),
		tunnelIntent: intentsv1.NewIntentServiceClient(tunnelConn),
	}
}

// callers returns the three edges under test, named for test output.
func (h *harness) callers() map[string]transporttest.ConformanceCaller {
	return map[string]transporttest.ConformanceCaller{
		"native_grpc":       grpcLikeCaller(h.grpcIntent, h.credentialHeaders),
		"tunnel":            grpcLikeCaller(h.tunnelIntent, h.credentialHeaders),
		"connect_http_edge": h.connectCaller(),
	}
}

// credentialHeaders resolves the header/metadata set one scenario presents:
// the credential its ConformanceCredential names, its correlation identifier,
// and any extra (typically hostile) metadata it declares.
func (h *harness) credentialHeaders(sc transporttest.ConformanceScenario) map[string]string {
	headers := map[string]string{
		transport.RequestIDMetadataKey: "req-edge001-" + sanitizeRequestID(sc.Name),
	}
	switch sc.Credential {
	case transporttest.ConformanceCredentialMissing:
		// No Authorization at all.
	case transporttest.ConformanceCredentialWrongAudience:
		tok, err := h.verifier.Issue(transporttest.WrongAudienceClaims(baseTime))
		if err != nil {
			h.t.Fatalf("issue wrong-audience credential: %v", err)
		}
		headers[transport.AuthorizationMetadataKey] = "Bearer " + tok
	case transporttest.ConformanceCredentialExpired:
		tok, err := h.verifier.Issue(transporttest.ExpiredClaims(baseTime))
		if err != nil {
			h.t.Fatalf("issue expired credential: %v", err)
		}
		headers[transport.AuthorizationMetadataKey] = "Bearer " + tok
	default:
		headers[transport.AuthorizationMetadataKey] = h.token
	}
	maps.Copy(headers, sc.ExtraMetadata)
	return headers
}

// sanitizeRequestID turns a scenario name into a printable-ASCII request
// identifier. Scenario names are already printable ASCII; this only guards
// against a future row that is not.
func sanitizeRequestID(name string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r > 0x7e {
			return '_'
		}
		return r
	}, name)
}

// buildGetIntentRequest builds the GetIntent request a scenario describes,
// including a material unknown field when the scenario asks for one.
func buildGetIntentRequest(sc transporttest.ConformanceScenario) *intentsv1.GetIntentRequest {
	req := &intentsv1.GetIntentRequest{IntentId: sc.IntentID}
	if !sc.UnknownField {
		return req
	}
	tagged, _ := transporttest.WithUnknownField(req, transporttest.ConformanceUnknownFieldNumber).(*intentsv1.GetIntentRequest)
	return tagged
}

// grpcLikeCaller drives one gRPC-shaped client (native or tunneled - both are
// [intentsv1.IntentServiceClient], and admission for both runs through the
// identical grpcserver interceptor chain) with one scenario.
func grpcLikeCaller(client intentsv1.IntentServiceClient, headersFor func(transporttest.ConformanceScenario) map[string]string) transporttest.ConformanceCaller {
	return func(sc transporttest.ConformanceScenario) transporttest.ConformanceOutcome {
		ctx, cancel := context.WithTimeout(context.Background(), sc.EffectiveDeadline(defaultCallDeadline))
		defer cancel()
		ctx = attachMetadata(ctx, headersFor(sc))

		var err error
		switch sc.Method {
		case transporttest.ConformanceMethodGetIntent:
			_, err = client.GetIntent(ctx, buildGetIntentRequest(sc))
		default:
			_, err = client.ListIntents(ctx, &intentsv1.ListIntentsRequest{})
		}
		if err == nil {
			return transporttest.ConformanceOutcome{}
		}
		return transporttest.ConformanceOutcome{Err: ownedFromGRPCLike(err)}
	}
}

// connectCaller drives the HTTP/Connect edge with one scenario.
func (h *harness) connectCaller() transporttest.ConformanceCaller {
	return func(sc transporttest.ConformanceScenario) transporttest.ConformanceOutcome {
		ctx, cancel := context.WithTimeout(context.Background(), sc.EffectiveDeadline(defaultCallDeadline))
		defer cancel()
		headers := h.credentialHeaders(sc)

		var err error
		switch sc.Method {
		case transporttest.ConformanceMethodGetIntent:
			_, err = h.edgeIntent.GetIntent(ctx, buildGetIntentRequest(sc), clientHeaders(headers)...)
		default:
			_, err = h.edgeIntent.ListIntents(ctx, &intentsv1.ListIntentsRequest{}, clientHeaders(headers)...)
		}
		if err == nil {
			return transporttest.ConformanceOutcome{}
		}
		return transporttest.ConformanceOutcome{Err: ownedFromConnect(err)}
	}
}

// attachMetadata layers headers onto ctx as outgoing gRPC metadata.
func attachMetadata(ctx context.Context, headers map[string]string) context.Context {
	pairs := make([]string, 0, len(headers)*2)
	for k, v := range headers {
		pairs = append(pairs, k, v)
	}
	return metadata.AppendToOutgoingContext(ctx, pairs...)
}

// applyHeaders sets headers on an HTTP header set.
func applyHeaders(h http.Header, headers map[string]string) {
	for k, v := range headers {
		h.Set(k, v)
	}
}

func clientHeaders(headers map[string]string) []clients.CallOption {
	opts := make([]clients.CallOption, 0, len(headers))
	for key, value := range headers {
		opts = append(opts, clients.WithHeader(key, value))
	}
	return opts
}

// ownedFromGRPCLike extracts the owned error from a native-gRPC or
// tunnel-carried gRPC client failure. A failure that does not decode as an
// owned status becomes a visibly-wrong owned error rather than a panic, so a
// genuine protocol regression shows up as an AssertConformant mismatch
// instead of aborting the whole suite.
func ownedFromGRPCLike(err error) *envelope.Error {
	if owned, ok := envelope.FromGRPC(err); ok {
		return owned
	}
	return envelope.New(envelope.CodeUnspecified, "test.undecodable_grpc_error", err.Error())
}

// ownedFromConnect is [ownedFromGRPCLike] for the HTTP/Connect edge.
func ownedFromConnect(err error) *envelope.Error {
	if owned, ok := envelope.As(err); ok {
		return owned
	}
	return envelope.New(envelope.CodeUnspecified, "test.undecodable_connect_error", err.Error())
}
