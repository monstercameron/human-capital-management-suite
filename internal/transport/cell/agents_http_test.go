package cell

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	agentv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/agent/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/agentclient"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	transportagents "github.com/monstercameron/human-capital-management-suite/internal/transport/agents"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/grpcserver"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/transporttest"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// agent042Settings records the tenant and actor the projection resolved.
type agent042Settings struct {
	enabled map[values.TenantId]bool
	tenant  values.TenantId
	actor   string
	writes  int
}

func (s *agent042Settings) AgentsEnabled(_ context.Context, tenant values.TenantId) (bool, error) {
	return s.enabled[tenant], nil
}

func (s *agent042Settings) SetAgentsEnabled(_ context.Context, tenant values.TenantId, enabled bool, actor string) error {
	s.writes++
	s.tenant, s.actor = tenant, actor
	if s.enabled == nil {
		s.enabled = map[values.TenantId]bool{}
	}
	s.enabled[tenant] = enabled
	return nil
}

type agent042Starter struct {
	principal *trust.Principal
	prompt    string
	calls     int
	err       error
}

func (s *agent042Starter) StartTask(_ context.Context, p *trust.Principal, prompt string) (agentclient.StartedTask, error) {
	s.calls++
	s.principal, s.prompt = p, prompt
	if s.err != nil {
		return agentclient.StartedTask{}, s.err
	}
	return agentclient.StartedTask{ID: "task-1", State: "RUNNING", Version: 1}, nil
}

type agent042Controller struct {
	principal *trust.Principal
	request   agentclient.TaskControl
	calls     int
	err       error
}

func (c *agent042Controller) ControlTask(_ context.Context, p *trust.Principal, request agentclient.TaskControl) (agentclient.ControlledTask, error) {
	c.calls++
	c.principal, c.request = p, request
	if c.err != nil {
		return agentclient.ControlledTask{}, c.err
	}
	return agentclient.ControlledTask{ID: request.TaskID, State: "PAUSED", Version: request.ExpectedVersion + 1}, nil
}

func agent042Allow() transportagents.Admin {
	return func(context.Context, *trust.Principal) (bool, error) { return true, nil }
}

func agent042Deny() transportagents.Admin {
	return func(context.Context, *trust.Principal) (bool, error) { return false, nil }
}

// agent042Fixture serves [AgentHTTPOverlay] ahead of next under the real
// admission configuration, and returns the pieces a caller needs to mint
// tokens for any subject.
func agent042Fixture(t *testing.T, next http.Handler, deps transportagents.Dependencies, cfg transport.Config, verifier *trust.HMACVerifier, now time.Time) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(AgentHTTPOverlay(next, cfg, deps))
	t.Cleanup(server.Close)
	return server
}

func agent042Admission(t *testing.T) (transport.Config, *trust.HMACVerifier, time.Time) {
	t.Helper()
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	verifier, err := transporttest.NewVerifier(func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}
	return transporttest.Config(verifier, func() time.Time { return now }, "agent-042-overlay", nil), verifier, now
}

func agent042Token(t *testing.T, verifier *trust.HMACVerifier, now time.Time, subject, kind string) string {
	t.Helper()
	claims := transporttest.DefaultClaims(now)
	claims.Subject, claims.SubjectKind = subject, kind
	if kind != "human" {
		claims.ClientID = "client-" + subject
	}
	token, err := transporttest.BearerToken(verifier, claims)
	if err != nil {
		t.Fatalf("BearerToken: %v", err)
	}
	return token
}

// agent042Native serves the same dependencies over native gRPC through the
// real admission interceptor chain.
func agent042Native(t *testing.T, cfg transport.Config, deps transportagents.Dependencies) agentv1.AgentServiceClient {
	t.Helper()
	server := grpc.NewServer(grpc.ChainUnaryInterceptor(grpcserver.UnaryInterceptor(cfg)))
	transportagents.Register(server, deps)
	listener := bufconn.Listen(1 << 20)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { server.Stop(); _ = listener.Close() })
	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return agentv1.NewAgentServiceClient(conn)
} // TestTodo_AGENT_042_Integration proves the served agent projection shares the
// native path's admission, authorization, revision and rate-limit decisions:
// an authenticated HTTP call reaches the same ports with the verified caller,
// an anonymous one is refused before the service, a stale revision fails with
// the identical typed conflict on both protocols, and a rate-limited machine
// credential is exhausted identically on both.
func TestTodo_AGENT_042_Integration(t *testing.T) {
	edge := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTeapot) })
	cfg, verifier, now := agent042Admission(t)
	settings, starter, controller := &agent042Settings{}, &agent042Starter{}, &agent042Controller{}
	deps := transportagents.Dependencies{Settings: settings, Admin: agent042Allow(), Starter: starter, Controller: controller}
	server := agent042Fixture(t, edge, deps, cfg, verifier, now)
	humanToken := agent042Token(t, verifier, now, "admin-1", "human")

	toggle := connect.NewClient[agentv1.SetAgentsEnabledRequest, agentv1.SetAgentsEnabledResponse](
		server.Client(), server.URL+agentv1.AgentService_SetAgentsEnabled_FullMethodName, connect.WithProtoJSON(),
	)
	if _, err := toggle.CallUnary(context.Background(), connect.NewRequest(&agentv1.SetAgentsEnabledRequest{Enabled: true})); connect.CodeOf(err) != connect.CodeUnauthenticated {
		t.Fatalf("anonymous toggle = %v (code %v), want Unauthenticated", err, connect.CodeOf(err))
	}
	if settings.writes != 0 {
		t.Fatal("an unauthenticated request reached the agent settings")
	}

	authorized := connect.NewRequest(&agentv1.SetAgentsEnabledRequest{Enabled: true})
	authorized.Header().Set(transport.AuthorizationMetadataKey, humanToken)
	resp, err := toggle.CallUnary(context.Background(), authorized)
	if err != nil {
		t.Fatalf("authorized toggle: %v", err)
	}
	if !resp.Msg.GetEnabled() || settings.writes != 1 || settings.tenant != values.TenantId(transporttest.Tenant) || settings.actor != "admin-1" {
		t.Fatalf("toggle reached tenant=%q actor=%q writes=%d, want the verified caller once",
			settings.tenant, settings.actor, settings.writes)
	}

	// The same administrator decision gates the overlay: a denied caller is
	// refused without a write.
	deniedDeps := deps
	deniedDeps.Admin = agent042Deny()
	deniedServer := agent042Fixture(t, edge, deniedDeps, cfg, verifier, now)
	deniedToggle := connect.NewClient[agentv1.SetAgentsEnabledRequest, agentv1.SetAgentsEnabledResponse](
		deniedServer.Client(), deniedServer.URL+agentv1.AgentService_SetAgentsEnabled_FullMethodName, connect.WithProtoJSON(),
	)
	deniedCall := connect.NewRequest(&agentv1.SetAgentsEnabledRequest{Enabled: false})
	deniedCall.Header().Set(transport.AuthorizationMetadataKey, humanToken)
	if _, err := deniedToggle.CallUnary(context.Background(), deniedCall); connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("denied toggle = %v, want PermissionDenied", err)
	}
	if settings.writes != 1 {
		t.Fatalf("a refused toggle wrote: writes=%d", settings.writes)
	}

	// A stale revision fails with the identical typed conflict through the
	// real admission chain on both protocols.
	controller.err = agentrun.ErrConflict
	control := connect.NewClient[agentv1.ControlAgentTaskRequest, agentv1.ControlAgentTaskResponse](
		server.Client(), server.URL+agentv1.AgentService_ControlAgentTask_FullMethodName, connect.WithProtoJSON(),
	)
	stale := connect.NewRequest(&agentv1.ControlAgentTaskRequest{
		TaskId: "task-1", ExpectedVersion: 8, Action: agentv1.AgentTaskAction_AGENT_TASK_ACTION_PAUSE,
	})
	stale.Header().Set(transport.AuthorizationMetadataKey, humanToken)
	if _, err := control.CallUnary(context.Background(), stale); connect.CodeOf(err) != connect.CodeAborted {
		t.Fatalf("HTTP stale revision = %v, want Aborted", err)
	}
	native := agent042Native(t, cfg, deps)
	nativeCtx := metadata.AppendToOutgoingContext(context.Background(), transport.AuthorizationMetadataKey, humanToken)
	if _, err := native.ControlAgentTask(nativeCtx, &agentv1.ControlAgentTaskRequest{
		TaskId: "task-1", ExpectedVersion: 8, Action: agentv1.AgentTaskAction_AGENT_TASK_ACTION_PAUSE,
	}); status.Code(err) != codes.Aborted {
		t.Fatalf("native stale revision = %v, want Aborted", err)
	}
	if controller.calls != 2 || controller.request.ExpectedVersion != 8 {
		t.Fatalf("controller calls=%d request=%+v, want one revision-checked call per protocol", controller.calls, controller.request)
	}
	controller.err = nil

	// A machine caller passes admission but is refused by the human-only
	// boundary identically on both protocols.
	machineToken := agent042Token(t, verifier, now, "svc-1", "service")
	start := connect.NewClient[agentv1.StartAgentTaskRequest, agentv1.StartAgentTaskResponse](
		server.Client(), server.URL+agentv1.AgentService_StartAgentTask_FullMethodName, connect.WithProtoJSON(),
	)
	machineCall := connect.NewRequest(&agentv1.StartAgentTaskRequest{Prompt: "hello"})
	machineCall.Header().Set(transport.AuthorizationMetadataKey, machineToken)
	if _, err := start.CallUnary(context.Background(), machineCall); connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("HTTP machine start = %v, want PermissionDenied", err)
	}
	machineCtx := metadata.AppendToOutgoingContext(context.Background(), transport.AuthorizationMetadataKey, machineToken)
	if _, err := native.StartAgentTask(machineCtx, &agentv1.StartAgentTaskRequest{Prompt: "hello"}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("native machine start = %v, want PermissionDenied", err)
	}
	if starter.calls != 0 {
		t.Fatalf("a refused machine caller started %d tasks", starter.calls)
	}

	// A rate-limited machine credential is exhausted identically on both
	// protocols, before either reaches the service.
	quotaCalls := 0
	limited := cfg
	limited.CredentialQuota = func(time.Time, string, string) (transport.CredentialQuotaOutcome, int, error) {
		quotaCalls++
		return transport.CredentialQuotaRateLimited, 30, nil
	}
	limitedServer := agent042Fixture(t, edge, deps, limited, verifier, now)
	limitedStart := connect.NewClient[agentv1.StartAgentTaskRequest, agentv1.StartAgentTaskResponse](
		limitedServer.Client(), limitedServer.URL+agentv1.AgentService_StartAgentTask_FullMethodName, connect.WithProtoJSON(),
	)
	limitedCall := connect.NewRequest(&agentv1.StartAgentTaskRequest{Prompt: "hello"})
	limitedCall.Header().Set(transport.AuthorizationMetadataKey, machineToken)
	if _, err := limitedStart.CallUnary(context.Background(), limitedCall); connect.CodeOf(err) != connect.CodeResourceExhausted {
		t.Fatalf("HTTP rate-limited start = %v, want ResourceExhausted", err)
	}
	limitedNative := agent042Native(t, limited, deps)
	if _, err := limitedNative.StartAgentTask(machineCtx, &agentv1.StartAgentTaskRequest{Prompt: "hello"}); status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("native rate-limited start = %v, want ResourceExhausted", err)
	}
	if quotaCalls != 2 {
		t.Fatalf("quota decisions = %d, want one per protocol", quotaCalls)
	}
	if starter.calls != 0 || settings.writes != 1 {
		t.Fatalf("a rate-limited caller reached a port: starts=%d writes=%d", starter.calls, settings.writes)
	}

	// Routes outside the agent prefix stay with the wrapped edge, and a cell
	// without the agents setting leaves the edge untouched.
	w := httptest.NewRecorder()
	AgentHTTPOverlay(edge, transport.Config{}, deps).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/anything", nil))
	if w.Code != http.StatusTeapot {
		t.Fatalf("non-agent route = %d, want the wrapped edge", w.Code)
	}
	w = httptest.NewRecorder()
	AgentHTTPOverlay(edge, transport.Config{}, transportagents.Dependencies{}).ServeHTTP(w, httptest.NewRequest(http.MethodPost, transportagents.ProcedurePrefix+"StartAgentTask", nil))
	if w.Code != http.StatusTeapot {
		t.Fatalf("nil settings replaced the edge: %d", w.Code)
	}
}
