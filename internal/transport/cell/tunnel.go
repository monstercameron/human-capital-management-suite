package cell

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/monstercameron/GoGRPCBridge/pkg/grpctunnel"
	"google.golang.org/grpc"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	transporthumanwork "github.com/monstercameron/human-capital-management-suite/internal/transport/humanwork"
	transportoperations "github.com/monstercameron/human-capital-management-suite/internal/transport/operations"
)

// TunnelPath is the edge route the gRPC-over-WebSocket bridge is mounted on.
//
// It is one path, not a subtree: a browser client dials exactly this address
// and every canonical gRPC service registered on the shared *grpc.Server is
// reachable through it by its own fully qualified method name. Nothing is
// projected, renamed or re-published here.
// TunnelPath sits under the workspace prefix on purpose: the dev-login
// session cookie the browser presents on the upgrade is scoped to
// RoutePrefix ("/workspace/"), and a WebSocket opened outside that path
// would carry no credential at all.
const TunnelPath = "/workspace/grpc"

// tunnelSessionMaxLifetime bounds how long one tunnel session may outlive the
// authorization its upgrade request was admitted under. A browser whose
// credential expired keeps its socket otherwise; at this bound the bridge
// closes it, the client reconnects, and the reconnect runs the Authorize hook
// again against the credential the page holds now.
const tunnelSessionMaxLifetime = 8 * time.Hour

// tunnelMaxConnectionsPerClient bounds concurrent tunnel sockets per client
// address. A page opens one; a person with several tabs open opens several.
// Eight is generous for that and still a bound.
const tunnelMaxConnectionsPerClient = 8

// NewEdgeHandlerWithTunnel is [NewEdgeHandler] plus the gRPC-over-WebSocket
// bridge (github.com/monstercameron/GoGRPCBridge) mounted at [TunnelPath].
//
// Everything NewEdgeHandler mounts - the Connect/RPC projection, the
// discovery document, the optional workspace - is mounted here identically:
// both constructors call the same internal builder, and the only difference
// between them is whether TunnelPath answers at all. A cell composed without
// a tunnel answers 404 there, which is what NewEdgeHandler still does.
//
// # Why a tunnel
//
// A browser cannot open the raw TCP connection native gRPC needs, so a
// browser-resident GoWebComponents/WASM client would otherwise need an
// HTTP/JSON projection of the business surface to talk to. The Promotion
// journey page uses none: it dials this route and calls the canonical
// services (hcmnext.intents.v1.IntentService, hcmnext.journey.v1.JourneyService)
// over real gRPC frames carried on a WebSocket. There is exactly one
// contract, and the page is a client of it.
//
// # Admission, twice
//
// The bridge is admitted at two independent points, and both are required.
//
//  1. At the upgrade. BridgeConfig.Authorize runs the same
//     [transport.PreAdmit] the workspace's own admit runs, over the upgrade
//     request's headers, with the dev-login cookie standing in for the
//     Authorization header only when this cell was composed with
//     DevBrowserLogin on (workspace.AdmissionMetadata owns that
//     substitution, so the tunnel and the workspace can never disagree about
//     it). A request that does not admit is refused with 403 before any
//     websocket or gRPC resource is allocated.
//
//  2. Per RPC. The bridge is deliberately left on its default handler
//     transport (ShouldUseNativeGRPCTransport stays false) so that the
//     diagnostic headers of the upgrade request - x-request-id,
//     x-correlation-id, traceparent - are forwarded into per-RPC metadata.
//     The bridge forwards those and only those: Authorization and Cookie are
//     never forwarded into metadata, by design, because an upgrade-time
//     credential must not silently authenticate calls made an hour later on
//     the same socket. So the browser client sends
//     "authorization: Bearer <token>" metadata on every RPC itself, and
//     grpcserver's admission interceptor authenticates each call from that.
//     A socket that upgraded successfully but whose calls carry no
//     credential gets UNAUTHENTICATED per call, which is correct.
//
// # Discovery
//
// The route is not advertised in the API-001 discovery document.
// [manifest.DiscoveryDocument] has exactly one place for a non-RPC route -
// the workspace_routes key this package splices in from
// [workspace.Routes] - and that list is derived from, and asserted against,
// the workspace mux's own constants. The tunnel is not a workspace route and
// publishing it there would make that list a list of two different things.
// The tunnel is discovered the way it is reached: by the page shell that
// hands its URL to the client (workspace.PathJourney's config island).
func NewEdgeHandlerWithTunnel(c *app.Cell, grpcServer *grpc.Server, opts ...connect.HandlerOption) (http.Handler, error) {
	return NewEdgeHandlerWithTunnelAndDependencies(c, grpcServer, nil, nil, nil, nil, transporthumanwork.WritePorts{}, opts...)
}

// NewEdgeHandlerWithTunnelAndDependencies is [NewEdgeHandlerWithTunnel] with
// the durable workflow and operation dependencies used by the application
// composition. The tunnel and HTTP edge therefore publish the same handlers.
func NewEdgeHandlerWithTunnelAndDependencies(
	c *app.Cell, grpcServer *grpc.Server, instances app.WorkflowInstanceReader,
	workQueue app.WorkItemQueueReader,
	operationStore transportoperations.Store, cursorKey []byte, workWrites transporthumanwork.WritePorts, opts ...connect.HandlerOption,
) (http.Handler, error) {
	if grpcServer == nil {
		return nil, fmt.Errorf("transport cell: a gRPC server is required to mount the tunnel")
	}
	return buildEdgeHandlerWithDependencies(c, grpcServer, instances, workQueue, operationStore, cursorKey, workWrites, opts...)
}

// newTunnelHandler builds the bridge handler for one composed cell.
// publicHost is the host[:port] of the cell's declared public origin, or
// empty when the cell is reached directly.
func newTunnelHandler(c *app.Cell, grpcServer *grpc.Server, publicHost string) (http.Handler, error) {
	handler, err := grpctunnel.BuildBridgeHandler(grpcServer, grpctunnel.BridgeConfig{
		CheckOrigin:             tunnelOriginCheck(publicHost),
		Authorize:               tunnelAuthorizer(c.Config, c.DevBrowserLogin()),
		SessionMaxLifetime:      tunnelSessionMaxLifetime,
		MaxConnectionsPerClient: tunnelMaxConnectionsPerClient,
		// PingInterval/IdleTimeout are left zero so the library's own
		// keepalive defaults (30s ping, 120s idle) apply, and
		// ReadLimitBytes is left zero so its default read limit applies.
		// Restating a library default here would only be a second place for
		// it to drift.
	})
	if err != nil {
		return nil, fmt.Errorf("transport cell: build the gRPC tunnel: %w", err)
	}
	return handler, nil
}

// sameOriginOnly is the tunnel's origin policy: a browser may upgrade only
// from the origin it loaded the page from.
//
// It compares the Origin header's host to the request's own Host rather than
// consulting a configured allowlist, because the only browser client of this
// route is the page this same edge served. A request with no Origin header at
// all is allowed: that is a non-browser client (a native Go client, a test,
// a CLI), for which the header is absent by protocol rather than withheld,
// and which is authenticated by [tunnelAuthorizer] like every other caller.
func sameOriginOnly(r *http.Request) bool {
	if r == nil {
		return false
	}
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Host == "" {
		return false
	}
	return strings.EqualFold(parsed.Host, r.Host)
}

// tunnelOriginCheck is the upgrade's origin policy for one composed cell.
// With no declared public origin it is [sameOriginOnly] unchanged. With one,
// the origin the public authority names is admitted too: a TLS-terminating
// proxy may deliver an upgrade whose request Host is the internal listener
// while the browser's page origin is the authority the deployment declared.
// The Origin's host is compared rather than its scheme, matching
// sameOriginOnly's own reading of a Host-rewriting hop.
func tunnelOriginCheck(publicHost string) func(*http.Request) bool {
	if publicHost == "" {
		return sameOriginOnly
	}
	return func(r *http.Request) bool {
		if sameOriginOnly(r) {
			return true
		}
		if r == nil {
			return false
		}
		parsed, err := url.Parse(strings.TrimSpace(r.Header.Get("Origin")))
		return err == nil && parsed.Host != "" && strings.EqualFold(parsed.Host, publicHost)
	}
}

// tunnelAuthorizer runs the cell's own admission over a websocket upgrade
// request. A returned error is projected by the bridge as 403 before the
// upgrade; the returned value is the owned admission error, so an operator
// reading the bridge's log sees the same reason identifier every other
// surface records.
func tunnelAuthorizer(cfg transport.Config, devBrowserLogin bool) func(*http.Request) error {
	return func(r *http.Request) error {
		if r == nil {
			return fmt.Errorf("transport cell: the tunnel upgrade carries no request")
		}
		if _, _, ownedErr := transport.PreAdmit(r.Context(), cfg,
			workspace.AdmissionMetadata(r, devBrowserLogin), TunnelPath); ownedErr != nil {
			return ownedErr
		}
		return nil
	}
}
