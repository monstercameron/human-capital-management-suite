// Package tunnel_test is the conformance suite for the cell's
// gRPC-over-WebSocket tunnel (internal/transport/cell.TunnelPath).
//
// It exists because what the tunnel claims is not true of any one package:
// it is true of a composed cell serving one HTTP mux on which the bridge, the
// Connect projection and the discovery document all live, reached by a real
// websocket upgrade carrying a real credential. The suite migrates an
// ephemeral PostgreSQL from zero, composes the same cell cmd/hcmnext serves,
// mounts the same handler cmd/hcmnext mounts, and then drives it as two
// different callers: a browser-shaped one (an Origin header) and a
// native one (none).
//
// The claim under test has two halves, and they are independent on purpose.
// The upgrade is admitted - an unauthenticated or cross-origin upgrade never
// becomes a socket. Each RPC is admitted again - the bridge forwards no
// Authorization into per-RPC metadata, so a call that does not carry its own
// credential is UNAUTHENTICATED even on a socket that upgraded cleanly.
package tunnel_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoGRPCBridge/pkg/grpctunnel"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/fixtures"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	transportcell "github.com/monstercameron/human-capital-management-suite/internal/transport/cell"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

// Fixture identity, mirroring test/workspace: the corpus tenant, because the
// cell this suite composes is the cell that serves it.
const (
	testTenant   = string(fixtures.Tenant)
	testIssuer   = "https://issuer.test.hcm-next.invalid"
	testAudience = "hcm-next-api"
	testOrgScope = "org-north-america"
	testCellID   = "cell-tunnel-test"
)

// testSigningKey never leaves this package and authenticates nothing outside
// a test process.
var testSigningKey = []byte("hcm-next-tunnel-suite-signing-key-32++++")

// baseTime pins the credential window and the cell clock.
var baseTime = time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)

// cell is one composed, running cell serving the edge handler cmd/hcmnext
// serves, tunnel included.
type cell struct {
	t *testing.T

	url    string // http://127.0.0.1:port
	host   string // 127.0.0.1:port
	client *http.Client
	token  string // "Bearer <credential>"
}

// newCell migrates a private schema, composes the cell, and serves either
// the tunnel-bearing edge handler or the plain one.
//
// The composition calls are app.NewCell, transportcell.NewGRPCServer and
// transportcell.NewEdgeHandlerWithTunnel, which is exactly what
// cmd/hcmnext's buildServe runs. A suite that mounted the bridge handler
// directly would prove the bridge works and nothing about whether this cell
// serves it.
func newCell(t *testing.T, withTunnel bool) *cell {
	t.Helper()
	return newCellWith(t, withTunnel, nil)
}

// newCellWith is [newCell] with the Promotion journey engine the composed
// cell publishes made explicit.
//
// A cell composed here has no execution authority and no execution database,
// so app.NewCell leaves Cell.Journey nil and
// hcmnext.journey.v1.JourneyService answers UNAVAILABLE. Setting the field
// before transportcell.NewGRPCServer reads it is what lets the journey
// stream be driven end to end without also standing up the P1B execution
// stack; every line of the composition that matters to this suite - the
// shared server constructor, its interceptor chain, the tunnel mount -
// is still the one cmd/hcmnext runs.
func newCellWith(t *testing.T, withTunnel bool, journeyEngine workspace.JourneyEngine) *cell {
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
		RoleAccess:  bootstrapTestRoleAccess(t, pool, testTenant),
		Verifier:    verifier,
		Audience:    testAudience,
		MaxDeadline: 30 * time.Second,
		Now:         func() time.Time { return baseTime },
	})
	if err != nil {
		t.Fatalf("app.NewCell: %v", err)
	}
	if journeyEngine != nil {
		composed.Journey = journeyEngine
	}

	grpcServer, err := transportcell.NewGRPCServer(composed)
	if err != nil {
		t.Fatalf("NewGRPCServer: %v", err)
	}
	t.Cleanup(grpcServer.Stop)

	var handler http.Handler
	if withTunnel {
		handler, err = transportcell.NewEdgeHandlerWithTunnel(composed, grpcServer)
	} else {
		handler, err = transportcell.NewEdgeHandler(composed)
	}
	if err != nil {
		t.Fatalf("edge handler: %v", err)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	token, err := verifier.Issue(trust.Claims{
		Issuer:               testIssuer,
		Audience:             testAudience,
		Subject:              "user-0191f3c4",
		SubjectKind:          "human",
		Tenant:               testTenant,
		OrganizationScopeID:  testOrgScope,
		Roles:                []string{"intent_author", string(authz.RoleCompAdmin)},
		AuthorityRefs:        []string{"authority:position:vp-people"},
		Purposes:             []string{authz.PurposeCompensationReview},
		AuthenticationMethod: "bearer_token",
		Assurance:            "substantial",
		SessionRef:           "session-tunnel",
		IssuedAtUnix:         baseTime.Add(-time.Minute).Unix(),
		ExpiresAtUnix:        baseTime.Add(time.Hour).Unix(),
	})
	if err != nil {
		t.Fatalf("issue credential: %v", err)
	}

	client := server.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return &cell{
		t:      t,
		url:    server.URL,
		host:   strings.TrimPrefix(server.URL, "http://"),
		client: client,
		token:  "Bearer " + token,
	}
}

// upgrade performs one raw websocket upgrade request against the tunnel and
// returns its status. It is a hand-built request rather than a dialer call
// because what is under test is the answer the edge gives before a socket
// exists, and a dialer reports only that it failed.
func (c *cell) upgrade(decorate func(*http.Request)) int {
	c.t.Helper()
	req, err := http.NewRequest(http.MethodGet, c.url+transportcell.TunnelPath, nil)
	if err != nil {
		c.t.Fatalf("build the upgrade request: %v", err)
	}
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Sec-WebSocket-Version", "13")
	req.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
	if decorate != nil {
		decorate(req)
	}
	res, err := c.client.Do(req)
	if err != nil {
		c.t.Fatalf("GET %s: %v", transportcell.TunnelPath, err)
	}
	defer func() { _ = res.Body.Close() }()
	_, _ = io.Copy(io.Discard, res.Body)
	return res.StatusCode
}

// dial opens one tunnel connection with the given upgrade headers.
func (c *cell) dial(ctx context.Context, headers http.Header) *grpc.ClientConn {
	c.t.Helper()
	conn, err := grpctunnel.BuildTunnelConn(ctx, grpctunnel.TunnelConfig{
		Target:           "ws://" + c.host + transportcell.TunnelPath,
		Headers:          headers,
		HandshakeTimeout: 10 * time.Second,
		GRPCOptions: []grpc.DialOption{
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		},
	})
	if err != nil {
		c.t.Fatalf("BuildTunnelConn: %v", err)
	}
	c.t.Cleanup(func() { _ = conn.Close() })
	return conn
}

// authorizedUpgrade is the header set a browser-resident client sends on the
// upgrade: the credential, and nothing else that admission reads.
func (c *cell) authorizedUpgrade() http.Header {
	return http.Header{"Authorization": []string{c.token}}
}

// ---------------------------------------------------------------------------
// The happy path
// ---------------------------------------------------------------------------

// TestTunnelServesTheCanonicalIntentService is the whole point of the route:
// a client that never touches an HTTP/JSON projection calls
// hcmnext.intents.v1.IntentService over gRPC frames on a websocket and gets a
// real answer from the composed cell.
func TestTunnelServesTheCanonicalIntentService(t *testing.T) {
	c := newCell(t, true)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	conn := c.dial(ctx, c.authorizedUpgrade())
	client := intentsv1.NewIntentServiceClient(conn)

	callCtx := metadata.AppendToOutgoingContext(ctx, transport.AuthorizationMetadataKey, c.token)
	res, err := client.ListIntents(callCtx, &intentsv1.ListIntentsRequest{})
	if err != nil {
		t.Fatalf("ListIntents over the tunnel: %v", err)
	}
	if res == nil {
		t.Fatal("ListIntents returned no response")
	}
	if res.GetPage() == nil {
		t.Fatal("ListIntents returned no page")
	}
}

// TestTunnelRPCWithoutMetadataIsUnauthenticated is the second half of the
// admission claim, and the reason the page shell puts the credential in its
// config island at all: upgrading is not authenticating. The bridge forwards
// only x-request-id, x-correlation-id and traceparent from the upgrade
// request into per-RPC metadata - never Authorization - so a call that
// carries no credential of its own is refused even though the socket it
// travels on was admitted.
func TestTunnelRPCWithoutMetadataIsUnauthenticated(t *testing.T) {
	c := newCell(t, true)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	conn := c.dial(ctx, c.authorizedUpgrade())
	client := intentsv1.NewIntentServiceClient(conn)

	_, err := client.ListIntents(ctx, &intentsv1.ListIntentsRequest{})
	if err == nil {
		t.Fatal("a call with no credential must not be answered")
	}
	if got := status.Code(err); got != codes.Unauthenticated {
		t.Fatalf("status = %s, want UNAUTHENTICATED: %v", got, err)
	}
}

// ---------------------------------------------------------------------------
// The upgrade
// ---------------------------------------------------------------------------

func TestTunnelUpgradeRequiresACredential(t *testing.T) {
	c := newCell(t, true)
	if got := c.upgrade(nil); got != http.StatusForbidden {
		t.Fatalf("an unauthenticated upgrade got %d, want 403", got)
	}
}

func TestTunnelUpgradeRefusesAnInvalidCredential(t *testing.T) {
	c := newCell(t, true)
	got := c.upgrade(func(r *http.Request) {
		r.Header.Set("Authorization", "Bearer not-a-credential")
	})
	if got != http.StatusForbidden {
		t.Fatalf("an invalid credential got %d, want 403", got)
	}
}

// TestTunnelUpgradeAdmitsACredential pins that the refusals above are the
// admission hook talking and not the upgrade failing for some other reason:
// the same request with a valid credential switches protocols.
func TestTunnelUpgradeAdmitsACredential(t *testing.T) {
	c := newCell(t, true)
	got := c.upgrade(func(r *http.Request) {
		r.Header.Set("Authorization", c.token)
		r.Header.Set("Origin", "http://"+c.host)
	})
	if got != http.StatusSwitchingProtocols {
		t.Fatalf("an authorized same-origin upgrade got %d, want 101", got)
	}
}

// TestTunnelUpgradeRefusesACrossOriginBrowser is the browser half of the
// policy: a credential alone is not enough if the page presenting it was
// served by somebody else's origin.
func TestTunnelUpgradeRefusesACrossOriginBrowser(t *testing.T) {
	c := newCell(t, true)
	got := c.upgrade(func(r *http.Request) {
		r.Header.Set("Authorization", c.token)
		r.Header.Set("Origin", "https://evil.example")
	})
	if got != http.StatusForbidden {
		t.Fatalf("a cross-origin upgrade got %d, want 403", got)
	}
}

// TestTunnelUpgradeRefusesCallerSelectedAuthority proves the upgrade runs the
// whole trusted-request boundary, not only authentication.
func TestTunnelUpgradeRefusesCallerSelectedAuthority(t *testing.T) {
	c := newCell(t, true)
	got := c.upgrade(func(r *http.Request) {
		r.Header.Set("Authorization", c.token)
		r.Header.Set("X-HCM-Roles", "operator")
	})
	if got != http.StatusForbidden {
		t.Fatalf("a caller-selected trusted-context header got %d, want 403", got)
	}
}

// ---------------------------------------------------------------------------
// The cell that mounts no tunnel
// ---------------------------------------------------------------------------

// TestEdgeHandlerWithoutTunnelHasNoRoute pins that the tunnel is opt-in at
// composition. NewEdgeHandler is unchanged by this work, and the address the
// bridge would answer on is an address this cell does not serve.
func TestEdgeHandlerWithoutTunnelHasNoRoute(t *testing.T) {
	c := newCell(t, false)
	if got := c.upgrade(func(r *http.Request) {
		r.Header.Set("Authorization", c.token)
	}); got != http.StatusNotFound {
		t.Fatalf("an upgrade against a tunnel-less edge got %d, want 404", got)
	}

	req, err := http.NewRequest(http.MethodGet, c.url+transportcell.TunnelPath, nil)
	if err != nil {
		t.Fatalf("build GET %s: %v", transportcell.TunnelPath, err)
	}
	req.Header.Set("Authorization", c.token)
	res, err := c.client.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", transportcell.TunnelPath, err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("GET %s got %d, want 404", transportcell.TunnelPath, res.StatusCode)
	}
}

// TestEdgeHandlerWithTunnelStillServesTheRestOfTheEdge is the byte-identical
// claim in the only form a test can make it: adding the tunnel added a route
// and changed nothing else, so the discovery document still answers exactly
// as it did.
func TestEdgeHandlerWithTunnelStillServesTheRestOfTheEdge(t *testing.T) {
	c := newCell(t, true)
	req, err := http.NewRequest(http.MethodGet, c.url+app.DiscoveryPath, nil)
	if err != nil {
		t.Fatalf("build GET %s: %v", app.DiscoveryPath, err)
	}
	req.Header.Set("Authorization", c.token)
	res, err := c.client.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", app.DiscoveryPath, err)
	}
	defer func() { _ = res.Body.Close() }()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("read the discovery document: %v", err)
	}
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET %s got %d: %s", app.DiscoveryPath, res.StatusCode, body)
	}
	if !strings.Contains(string(body), "manifest_digest") {
		t.Fatalf("the discovery document is not the discovery document: %s", body)
	}
	// The tunnel is deliberately not advertised there; see
	// internal/transport/cell/tunnel.go's doc comment.
	if strings.Contains(string(body), `"`+transportcell.TunnelPath+`"`) {
		t.Fatal("the discovery document advertises the tunnel route")
	}
}

// TestNewEdgeHandlerWithTunnelRequiresAServer is the constructor's own
// refusal, checked here rather than only in the unit test because this suite
// composes the real cell the constructor is called with.
func TestNewEdgeHandlerWithTunnelRequiresAServer(t *testing.T) {
	if _, err := transportcell.NewEdgeHandlerWithTunnel(nil, nil); err == nil {
		t.Fatal("expected a refusal for a nil gRPC server")
	}
}
