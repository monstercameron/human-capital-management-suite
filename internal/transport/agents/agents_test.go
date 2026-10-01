package agents

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/reflect/protoreflect"

	agentv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/agent/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/agentclient"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/grpcserver"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/transporttest"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type fakeSettings struct {
	enabled map[values.TenantId]bool
	actors  []string
	writes  int
	err     error
}

func (s *fakeSettings) AgentsEnabled(_ context.Context, tenant values.TenantId) (bool, error) {
	return s.enabled[tenant], nil
}

func (s *fakeSettings) SetAgentsEnabled(_ context.Context, tenant values.TenantId, enabled bool, actor string) error {
	s.writes++
	if s.err != nil {
		return s.err
	}
	if s.enabled == nil {
		s.enabled = map[values.TenantId]bool{}
	}
	s.enabled[tenant] = enabled
	s.actors = append(s.actors, actor)
	return nil
}

type fakeStarter struct {
	calls     int
	principal *trust.Principal
	prompt    string
	err       error
}

type fakeController struct {
	request   agentclient.TaskControl
	principal *trust.Principal
	result    agentclient.ControlledTask
	err       error
	calls     int
}

func (c *fakeController) ControlTask(_ context.Context, p *trust.Principal, request agentclient.TaskControl) (agentclient.ControlledTask, error) {
	c.calls++
	c.principal, c.request = p, request
	return c.result, c.err
}

func (s *fakeStarter) StartTask(_ context.Context, p *trust.Principal, prompt string) (agentclient.StartedTask, error) {
	s.calls++
	s.principal, s.prompt = p, prompt
	if s.err != nil {
		return agentclient.StartedTask{}, s.err
	}
	return agentclient.StartedTask{ID: "task-1", State: "running"}, nil
}

func principalOf(t *testing.T, tenant string, kind trust.SubjectKind) *trust.Principal {
	t.Helper()
	p, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant: values.TenantId(tenant), Subject: "subject-1", SubjectKind: kind, ClientID: "client-1",
		AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh, SessionRef: "session-1",
		IssuedAt: time.Now().Add(-time.Minute), ExpiresAt: time.Now().Add(time.Minute), CredentialDigest: "digest-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func allow() Admin {
	return func(context.Context, *trust.Principal) (bool, error) { return true, nil }
}

func deny() Admin { return func(context.Context, *trust.Principal) (bool, error) { return false, nil } }

func code(err error) codes.Code { return status.Code(err) }

func TestTodo_UXBLIND_122_Security(t *testing.T) {
	human := principalOf(t, "tenant-a", trust.SubjectKindHuman)
	ctx := trust.WithPrincipal(context.Background(), human)

	// A non-admin is refused and nothing is written.
	settings := &fakeSettings{}
	server := NewServer(Dependencies{Settings: settings, Admin: deny(), Starter: &fakeStarter{}})
	if _, err := server.SetAgentsEnabled(ctx, &agentv1.SetAgentsEnabledRequest{Enabled: true}); code(err) != codes.PermissionDenied {
		t.Fatalf("non-admin toggle = %v, want PermissionDenied", err)
	}
	if settings.writes != 0 {
		t.Fatal("a refused toggle reached the store")
	}

	// An unauthenticated caller and every machine subject are refused before
	// the authority check or the store run.
	asked := 0
	counting := Admin(func(context.Context, *trust.Principal) (bool, error) { asked++; return true, nil })
	server = NewServer(Dependencies{Settings: settings, Admin: counting, Starter: &fakeStarter{}})
	if _, err := server.SetAgentsEnabled(context.Background(), &agentv1.SetAgentsEnabledRequest{Enabled: true}); code(err) != codes.Unauthenticated {
		t.Fatalf("unauthenticated toggle = %v, want Unauthenticated", err)
	}
	for _, kind := range []trust.SubjectKind{trust.SubjectKindService, trust.SubjectKindAgent, trust.SubjectKindIntegration} {
		machine := trust.WithPrincipal(context.Background(), principalOf(t, "tenant-a", kind))
		if _, err := server.SetAgentsEnabled(machine, &agentv1.SetAgentsEnabledRequest{Enabled: true}); code(err) != codes.PermissionDenied {
			t.Fatalf("%s toggle = %v, want PermissionDenied", kind, err)
		}
		if _, err := server.StartAgentTask(machine, &agentv1.StartAgentTaskRequest{Prompt: "hello"}); code(err) != codes.PermissionDenied {
			t.Fatalf("%s start = %v, want PermissionDenied", kind, err)
		}
	}
	if _, err := server.StartAgentTask(context.Background(), &agentv1.StartAgentTaskRequest{Prompt: "hello"}); code(err) != codes.Unauthenticated {
		t.Fatalf("unauthenticated start = %v, want Unauthenticated", err)
	}
	if asked != 0 || settings.writes != 0 {
		t.Fatalf("refused callers reached the authority check (%d) or the store (%d)", asked, settings.writes)
	}

	// The tenant and actor come from the principal, never the request: the
	// request messages have no such field, and the write lands on the
	// principal's tenant under the principal's subject.
	if _, err := server.SetAgentsEnabled(ctx, &agentv1.SetAgentsEnabledRequest{Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if !settings.enabled["tenant-a"] || len(settings.enabled) != 1 || len(settings.actors) != 1 || settings.actors[0] != "subject-1" {
		t.Fatalf("write = %v actors=%v", settings.enabled, settings.actors)
	}
	messages := agentv1.File_hcmnext_agent_v1_agent_service_proto.Messages()
	for _, message := range []string{"StartAgentTaskRequest", "SetAgentsEnabledRequest"} {
		for _, name := range []string{"tenant_id", "tenant", "actor", "user_id", "subject", "principal", "scope"} {
			if messages.ByName(protoreflect.Name(message)).Fields().ByName(protoreflect.Name(name)) != nil {
				t.Fatalf("%s carries a %s field", message, name)
			}
		}
	}

	// A failed authority lookup refuses; a failed write does not leak.
	failing := NewServer(Dependencies{Settings: settings, Admin: func(context.Context, *trust.Principal) (bool, error) {
		return false, errors.New("role store password=hunter2")
	}})
	_, err := failing.SetAgentsEnabled(ctx, &agentv1.SetAgentsEnabledRequest{Enabled: false})
	if code(err) != codes.Unavailable || strings.Contains(err.Error(), "hunter2") {
		t.Fatalf("authority failure = %v", err)
	}
	broken := NewServer(Dependencies{Settings: &fakeSettings{err: errors.New("pq: secret-dsn")}, Admin: allow()})
	_, err = broken.SetAgentsEnabled(ctx, &agentv1.SetAgentsEnabledRequest{Enabled: true})
	if code(err) != codes.Unavailable || strings.Contains(err.Error(), "secret-dsn") {
		t.Fatalf("store failure = %v", err)
	}
	if _, err := NewServer(Dependencies{}).SetAgentsEnabled(ctx, &agentv1.SetAgentsEnabledRequest{}); code(err) != codes.Unavailable {
		t.Fatalf("missing ports = %v", err)
	}
	if _, err := server.SetAgentsEnabled(ctx, nil); code(err) != codes.InvalidArgument {
		t.Fatalf("nil request = %v", err)
	}

	// Prompt bounds are enforced before the starter runs.
	starter := &fakeStarter{}
	server = NewServer(Dependencies{Settings: settings, Admin: allow(), Starter: starter})
	for name, prompt := range map[string]string{
		"empty": "", "blank": " \n\t ", "invalid utf8": "ok\xff", "over the cap": strings.Repeat("a", MaxPromptBytes+1),
	} {
		if _, err := server.StartAgentTask(ctx, &agentv1.StartAgentTaskRequest{Prompt: prompt}); code(err) != codes.InvalidArgument {
			t.Fatalf("%s prompt = %v, want InvalidArgument", name, err)
		}
	}
	if starter.calls != 0 {
		t.Fatal("an invalid prompt reached the starter")
	}
	if _, err := server.StartAgentTask(ctx, &agentv1.StartAgentTaskRequest{Prompt: strings.Repeat("a", MaxPromptBytes)}); err != nil {
		t.Fatalf("prompt at the cap = %v", err)
	}
	if starter.principal != human {
		t.Fatal("the starter did not receive the verified principal")
	}

	// Starter errors map to closed codes and leak nothing.
	for _, tc := range []struct {
		err  error
		want codes.Code
	}{
		{agentclient.ErrDisabled, codes.FailedPrecondition},
		{agentclient.ErrInvalidPrompt, codes.InvalidArgument},
		{agentclient.ErrNotAuthorized, codes.PermissionDenied},
		{context.Canceled, codes.Unavailable},
		{context.DeadlineExceeded, codes.DeadlineExceeded},
		{errors.New("sql: tenant-b row 7 secret-token"), codes.Unavailable},
	} {
		s := NewServer(Dependencies{Settings: settings, Starter: &fakeStarter{err: tc.err}})
		_, err := s.StartAgentTask(ctx, &agentv1.StartAgentTaskRequest{Prompt: "hello"})
		if code(err) != tc.want || strings.Contains(err.Error(), "secret-token") || strings.Contains(err.Error(), "tenant-b") {
			t.Fatalf("starter error %v -> %v, want %v with no detail", tc.err, err, tc.want)
		}
	}
	if _, err := NewServer(Dependencies{Settings: settings}).StartAgentTask(ctx, &agentv1.StartAgentTaskRequest{Prompt: "hello"}); code(err) != codes.Unavailable {
		t.Fatalf("no starter = %v", err)
	}
	if _, err := server.StartAgentTask(ctx, nil); code(err) != codes.InvalidArgument {
		t.Fatalf("nil start request = %v", err)
	}
}

func TestStartAgentTask_LongModeFailsClosedWithoutModeStarter(t *testing.T) {
	human := principalOf(t, "tenant-a", trust.SubjectKindHuman)
	ctx := trust.WithPrincipal(context.Background(), human)
	starter := &fakeStarter{}
	server := NewServer(Dependencies{Settings: &fakeSettings{enabled: map[values.TenantId]bool{"tenant-a": true}}, Admin: allow(), Starter: starter})
	_, err := server.StartAgentTask(ctx, &agentv1.StartAgentTaskRequest{Prompt: "long", Mode: agentv1.AgentStartMode_AGENT_START_MODE_LONG_TASK})
	if code(err) != codes.Unavailable || starter.calls != 0 {
		t.Fatalf("long fallback = %v calls=%d, want unavailable and no start", err, starter.calls)
	}
}

func TestControlAgentTask_AuthorizesMapsActionsAndErrors(t *testing.T) {
	human := principalOf(t, "tenant-a", trust.SubjectKindHuman)
	ctx := trust.WithPrincipal(context.Background(), human)
	controller := &fakeController{result: agentclient.ControlledTask{ID: "task-1", State: "paused", Version: 9}}
	server := NewServer(Dependencies{Controller: controller})
	for _, tc := range []struct {
		name   string
		action agentv1.AgentTaskAction
		want   agentclient.TaskAction
	}{
		{"confirm plan", agentv1.AgentTaskAction_AGENT_TASK_ACTION_CONFIRM_PLAN, agentclient.TaskConfirmPlan},
		{"pause", agentv1.AgentTaskAction_AGENT_TASK_ACTION_PAUSE, agentclient.TaskPause},
		{"resume", agentv1.AgentTaskAction_AGENT_TASK_ACTION_RESUME, agentclient.TaskResume},
		{"cancel", agentv1.AgentTaskAction_AGENT_TASK_ACTION_CANCEL, agentclient.TaskCancel},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := server.ControlAgentTask(ctx, &agentv1.ControlAgentTaskRequest{TaskId: "task-1", ExpectedVersion: 8, Action: tc.action})
			if err != nil || got.GetTaskId() != "task-1" || got.GetState() != "paused" || got.GetVersion() != 9 {
				t.Fatalf("control = %+v, %v", got, err)
			}
			if controller.principal != human || controller.request != (agentclient.TaskControl{TaskID: "task-1", ExpectedVersion: 8, Action: tc.want}) {
				t.Fatalf("forwarded principal/request = %p/%+v", controller.principal, controller.request)
			}
		})
	}
	if controller.calls != 4 {
		t.Fatalf("controller calls = %d, want 4", controller.calls)
	}

	for _, tc := range []struct {
		name string
		in   *agentv1.ControlAgentTaskRequest
		want codes.Code
	}{
		{"nil request", nil, codes.InvalidArgument},
		{"missing task", &agentv1.ControlAgentTaskRequest{ExpectedVersion: 1, Action: agentv1.AgentTaskAction_AGENT_TASK_ACTION_PAUSE}, codes.InvalidArgument},
		{"missing version", &agentv1.ControlAgentTaskRequest{TaskId: "task-1", Action: agentv1.AgentTaskAction_AGENT_TASK_ACTION_PAUSE}, codes.InvalidArgument},
		{"unknown action", &agentv1.ControlAgentTaskRequest{TaskId: "task-1", ExpectedVersion: 1}, codes.InvalidArgument},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := controller.calls
			_, err := server.ControlAgentTask(ctx, tc.in)
			if code(err) != tc.want || controller.calls != before {
				t.Fatalf("error=%v calls=%d", err, controller.calls)
			}
		})
	}
	if _, err := NewServer(Dependencies{}).ControlAgentTask(ctx, &agentv1.ControlAgentTaskRequest{TaskId: "task-1", ExpectedVersion: 1, Action: agentv1.AgentTaskAction_AGENT_TASK_ACTION_PAUSE}); code(err) != codes.Unavailable {
		t.Fatalf("missing controller = %v", err)
	}
	if _, err := server.ControlAgentTask(context.Background(), &agentv1.ControlAgentTaskRequest{TaskId: "task-1", ExpectedVersion: 1, Action: agentv1.AgentTaskAction_AGENT_TASK_ACTION_PAUSE}); code(err) != codes.Unauthenticated {
		t.Fatalf("missing human = %v", err)
	}
	if _, err := server.ControlAgentTask(trust.WithPrincipal(context.Background(), principalOf(t, "tenant-a", trust.SubjectKindService)), &agentv1.ControlAgentTaskRequest{TaskId: "task-1", ExpectedVersion: 1, Action: agentv1.AgentTaskAction_AGENT_TASK_ACTION_PAUSE}); code(err) != codes.PermissionDenied {
		t.Fatalf("machine principal = %v", err)
	}

	for _, tc := range []struct {
		err  error
		want codes.Code
	}{
		{agentclient.ErrDisabled, codes.FailedPrecondition}, {agentclient.ErrNotAuthorized, codes.PermissionDenied},
		{agentsystem.ErrDenied, codes.PermissionDenied}, {agentrun.ErrConflict, codes.Aborted},
		{agentrun.ErrTerminal, codes.FailedPrecondition}, {errors.New("private backend detail"), codes.Unavailable},
	} {
		controller.err = tc.err
		_, err := server.ControlAgentTask(ctx, &agentv1.ControlAgentTaskRequest{TaskId: "task-1", ExpectedVersion: 1, Action: agentv1.AgentTaskAction_AGENT_TASK_ACTION_PAUSE})
		if code(err) != tc.want || strings.Contains(err.Error(), "private backend detail") {
			t.Fatalf("%v -> %v, want %v", tc.err, err, tc.want)
		}
	}
}

func TestRegisterPublishesOnlyWithSettings(t *testing.T) {
	server := grpc.NewServer()
	Register(server, Dependencies{})
	if len(server.GetServiceInfo()) != 0 {
		t.Fatal("a cell with no agents setting exposed the service")
	}
	Register(nil, Dependencies{Settings: &fakeSettings{}})
	Register(server, Dependencies{Settings: &fakeSettings{}})
	if _, ok := server.GetServiceInfo()["hcmnext.agent.v1.AgentService"]; !ok {
		t.Fatal("service was not registered")
	}
}

type staticRoles struct {
	roleaccess.Store
}

func (staticRoles) Load(context.Context, values.TenantId, string) (roleaccess.Snapshot, error) {
	return roleaccess.Snapshot{PagePermissions: roleaccess.DefaultPagePermissions()}, nil
}

// TestTodo_UXBLIND_122_Integration drives the service through the real
// admission interceptor chain over an in-process connection, with the
// workspace's own durable-role decision as the administrator check: the
// browser's exact path, minus the WebSocket tunnel.
func TestTodo_UXBLIND_122_Integration(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	verifier, err := transporttest.NewVerifier(func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	cfg := transporttest.Config(verifier, func() time.Time { return now }, "agents-integration", nil)
	settings := &fakeSettings{}
	starter := &fakeStarter{}
	roles := staticRoles{}
	server := grpc.NewServer(grpc.ChainUnaryInterceptor(grpcserver.UnaryInterceptor(cfg)))
	Register(server, Dependencies{
		Settings: settings, Starter: starter,
		Admin: func(ctx context.Context, p *trust.Principal) (bool, error) {
			return workspace.CanChangeAgentsSetting(ctx, roles, p)
		},
	})
	listener := bufconn.Listen(1 << 20)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { server.Stop(); _ = listener.Close() })
	conn, err := grpc.NewClient("passthrough:///bufnet", grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	client := agentv1.NewAgentServiceClient(conn)

	as := func(subject, kind string, roleNames ...string) context.Context {
		claims := transporttest.DefaultClaims(now)
		claims.Subject, claims.SubjectKind, claims.Roles = subject, kind, roleNames
		if kind != "human" {
			claims.ClientID = "client-" + subject
		}
		token, tokenErr := transporttest.BearerToken(verifier, claims)
		if tokenErr != nil {
			t.Fatal(tokenErr)
		}
		return metadata.AppendToOutgoingContext(context.Background(), transport.AuthorizationMetadataKey, token)
	}
	call := func(ctx context.Context) (*agentv1.SetAgentsEnabledResponse, error) {
		return client.SetAgentsEnabled(ctx, &agentv1.SetAgentsEnabledRequest{Enabled: true})
	}

	if _, err := call(context.Background()); code(err) != codes.Unauthenticated {
		t.Fatalf("no bearer = %v, want Unauthenticated", err)
	}
	if _, err := call(as("worker-1", "human", "worker_self")); code(err) != codes.PermissionDenied {
		t.Fatalf("worker toggle = %v, want PermissionDenied", err)
	}
	if _, err := call(as("svc-1", "service", productui.RoleHCMAdmin)); code(err) != codes.PermissionDenied {
		t.Fatalf("service toggle = %v, want PermissionDenied", err)
	}
	if settings.writes != 0 {
		t.Fatalf("refused toggles wrote %d times", settings.writes)
	}
	response, err := call(as("admin-1", "human", productui.RoleHCMAdmin))
	if err != nil || !response.GetEnabled() {
		t.Fatalf("admin toggle = %v, %v", response, err)
	}
	if len(settings.actors) != 1 || settings.actors[0] != "admin-1" || !settings.enabled[values.TenantId(transporttest.Tenant)] {
		t.Fatalf("stored = %v actors=%v", settings.enabled, settings.actors)
	}

	started, err := client.StartAgentTask(as("worker-1", "human", "worker_self"), &agentv1.StartAgentTaskRequest{Prompt: "  summarise my week  "})
	if err != nil || started.GetTaskId() != "task-1" || started.GetState() != "running" {
		t.Fatalf("start = %v, %v", started, err)
	}
	if starter.principal == nil || starter.principal.Subject() != "worker-1" || string(starter.principal.Tenant()) != transporttest.Tenant {
		t.Fatalf("starter principal = %+v", starter.principal)
	}
	starter.err = agentclient.ErrDisabled
	if _, err := client.StartAgentTask(as("worker-1", "human", "worker_self"), &agentv1.StartAgentTaskRequest{Prompt: "again"}); code(err) != codes.FailedPrecondition {
		t.Fatalf("disabled start = %v, want FailedPrecondition", err)
	}
}
