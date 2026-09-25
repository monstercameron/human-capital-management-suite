package cell

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"
	"time"

	"github.com/monstercameron/GoGRPCBridge/pkg/grpctunnel"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	adminv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/admin/v1"
	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	transporthumanwork "github.com/monstercameron/human-capital-management-suite/internal/transport/humanwork"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/transporttest"
)

// TestTunnelOriginRequiresAWorkspacePageOrigin is INTAPI-006's RED for the
// tunnel admission defect: the bridge is the workspace page's own route, so
// an upgrade that carries no Origin header is not a browser holding the
// page — it is a native client that must use the direct gRPC surface — and
// is refused before any socket exists.
func TestTunnelOriginRequiresAWorkspacePageOrigin(t *testing.T) {
	upgrade := func(origin string) *http.Request {
		r := httptest.NewRequest(http.MethodGet, "http://cell.test"+TunnelPath, nil)
		r.Host = "cell.test"
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		return r
	}
	if sameOriginOnly(upgrade("")) {
		t.Fatal("an upgrade without an Origin header passed the origin check")
	}
	if !sameOriginOnly(upgrade("http://cell.test")) {
		t.Fatal("a same-origin upgrade failed the origin check")
	}
	if tunnelOriginCheck("hcm.example.com")(upgrade("")) {
		t.Fatal("an upgrade without an Origin header passed the public-origin check")
	}
	if !tunnelOriginCheck("hcm.example.com")(upgrade("https://hcm.example.com")) {
		t.Fatal("the declared public origin failed the origin check")
	}
}

// tunnelBridgeCell composes the smallest cell the tunnel admits: the shared
// admission configuration over the fixture verifier, no dev login.
func tunnelBridgeCell(t *testing.T) (*app.Cell, string) {
	t.Helper()
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	verifier, err := transporttest.NewVerifier(func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}
	claims := transporttest.DefaultClaims(now)
	token, err := transporttest.BearerToken(verifier, claims)
	if err != nil {
		t.Fatalf("BearerToken: %v", err)
	}
	return &app.Cell{Config: transporttest.Config(verifier, func() time.Time { return now }, "tunnel-services", nil)}, token
}

// TestTunnelServesOnlyWorkspaceServices is INTAPI-006's RED for the tunnel
// exposure defect: the bridge proxied every service on the shared server,
// including the operator-only AdminService. The tunnel now bridges a
// workspace-only server: only the services the page uses are registered,
// so anything else is UNIMPLEMENTED at the bridge before authentication,
// authorization or any handler runs.
func TestTunnelServesOnlyWorkspaceServices(t *testing.T) {
	for path, want := range map[string]bool{
		"":                                   false,
		"nope":                               false,
		"/hcmnext.journey.v1.JourneyService": false,
		"/hcmnext.intents.v1.IntentService/ListIntents":                  true,
		"/hcmnext.journey.v1.JourneyService/ListJourneys":                true,
		"/hcmnext.journey.v1.JourneyService/WatchJourney":                true,
		"/hcmnext.workflow.v1.WorkflowService/GetWorkflow":               true,
		"/hcmnext.humanwork.v1.WorkService/ListWorkItems":                true,
		"/hcmnext.humanwork.v1.WorkService/GetThresholdTable":            true,
		"/hcmnext.admin.v1.AdminService/GetWorkflowInstance":             false,
		"/hcmnext.admin.v1.AdminService/ListIntents":                     false,
		"/hcmnext.admin.v1.OnboardingService/GetOnboarding":              false,
		"/hcmnext.registry.v1.RegistryService/GetCapability":             false,
		"/hcmnext.evidence.v1.OperationsService/GetOperation":            false,
		"/grpc.health.v1.Health/Check":                                   false,
		"/hcmnext.chat.v1.ConversationService/ListPosts":                 true,
		"/hcmnext.chat.v1.ChatExtensionsService/GetCounts":               true,
		"/hcmnext.document.v1.DocumentService/ListDocuments":             true,
		"/hcmnext.document.v1.DocumentService/GetDocument":               true,
		"/hcmnext.position.v1.PositionService/ListPositionObjectOptions": true,
		"/hcmnext.project.v1.ProjectService/ListProjects":                true,
		"/hcmnext.evidence.v1.EvidenceService/GetEvidence":               false,
	} {
		if got := tunnelAllowsService(path); got != want {
			t.Errorf("tunnelAllowsService(%q) = %v, want %v", path, got, want)
		}
	}

	c, token := tunnelBridgeCell(t)
	// The composition root builds the tunnel through the chat-aware
	// constructor; with chat disabled it still registers the generated
	// stubs, so the policy and the wiring agree in both modes.
	srv, err := NewTunnelGRPCServerWithChat(c, nil, nil, nil, nil, transporthumanwork.WritePorts{}, nil, nil, nil)
	if err != nil {
		t.Fatalf("NewTunnelGRPCServerWithChat: %v", err)
	}
	t.Cleanup(srv.Stop)

	// The constructor and the policy agree exactly: every registered
	// service is allowed, and every allowed service is registered. A new
	// registration without a policy edit — or a policy line without its
	// registration — fails here.
	var registered []string
	for name := range srv.GetServiceInfo() {
		registered = append(registered, name)
	}
	sort.Strings(registered)
	var allowed []string
	for name, ok := range tunnelAllowedServices {
		if ok {
			allowed = append(allowed, name)
		}
	}
	sort.Strings(allowed)
	if len(registered) != len(allowed) {
		t.Fatalf("tunnel services = %v, policy allows %v", registered, allowed)
	}
	for i := range registered {
		if registered[i] != allowed[i] {
			t.Fatalf("tunnel services = %v, policy allows %v", registered, allowed)
		}
	}

	// The predicate above is the policy and the constructor is the wiring;
	// this is the proof an RPC takes that path: JourneyService passes the
	// bridge (with no role store composed the page gate denies, which
	// proves the call reached the handler instead of stopping at the
	// bridge), while AdminService — absent from the bridged server although
	// it serves the direct surface — is UNIMPLEMENTED.
	bridge, err := newTunnelHandler(c, srv, "")
	if err != nil {
		t.Fatalf("newTunnelHandler: %v", err)
	}
	edge := httptest.NewServer(bridge)
	t.Cleanup(edge.Close)
	host := edge.URL[len("http://"):]

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	conn, err := grpctunnel.BuildTunnelConn(ctx, grpctunnel.TunnelConfig{
		Target:  "ws://" + host + TunnelPath,
		Headers: http.Header{"Authorization": []string{token}, "Origin": []string{"http://" + host}},
		GRPCOptions: []grpc.DialOption{
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		},
	})
	if err != nil {
		t.Fatalf("BuildTunnelConn: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	callCtx := metadata.AppendToOutgoingContext(ctx, transport.AuthorizationMetadataKey, token)

	if _, err := journeyv1.NewJourneyServiceClient(conn).ListJourneys(callCtx, &journeyv1.ListJourneysRequest{}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("JourneyService over the tunnel = %v, want PERMISSION_DENIED (through the bridge to the handler)", err)
	}
	if _, err := adminv1.NewAdminServiceClient(conn).GetWorkflowInstance(callCtx, &adminv1.GetWorkflowInstanceRequest{}); status.Code(err) != codes.Unimplemented {
		t.Fatalf("AdminService over the tunnel = %v, want UNIMPLEMENTED at the bridge", err)
	}
}
