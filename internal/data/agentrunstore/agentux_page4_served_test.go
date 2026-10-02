package agentrunstore

import (
	"context"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	agentv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/agent/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	transportagents "github.com/monstercameron/human-capital-management-suite/internal/transport/agents"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type agentUXPage4Settings struct{}

func (agentUXPage4Settings) AgentsEnabled(context.Context, values.TenantId) (bool, error) {
	return true, nil
}
func (agentUXPage4Settings) SetAgentsEnabled(context.Context, values.TenantId, bool, string) error {
	return nil
}

type agentUXPage4TaskReader struct{ store *TenantStore }

func (r agentUXPage4TaskReader) GetAgentTask(ctx context.Context, principal *trust.Principal, id string) (agentrun.AgentTask, error) {
	task, err := r.store.Get(ctx, id)
	if err != nil {
		return agentrun.AgentTask{}, err
	}
	if task.TenantID != principal.Tenant().String() || task.UserID != principal.Subject() {
		return agentrun.AgentTask{}, agentrun.ErrNotFound
	}
	return task, nil
}

func (r agentUXPage4TaskReader) ListAgentTasks(ctx context.Context, principal *trust.Principal) ([]agentrun.AgentTask, error) {
	tasks, err := r.store.List(ctx)
	if err != nil {
		return nil, err
	}
	owned := tasks[:0:0]
	for _, task := range tasks {
		if task.TenantID == principal.Tenant().String() && task.UserID == principal.Subject() {
			owned = append(owned, task)
		}
	}
	return owned, nil
}

func TestTodo_AGENTUX_008_Integration_ServedStoredStatesOpenByListedID(t *testing.T) {
	e := newEnv(t)
	store := e.tenantStore(t, "run-one")
	ctx := context.Background()
	for _, fixture := range []struct {
		id      string
		state   agentrun.TaskState
		answer  string
		failure string
	}{
		{"agt_active", agentrun.StateRunning, "", ""},
		{"agt_completed", agentrun.StateCompleted, "A useful answer.", ""},
		{"agt_failed", agentrun.StateFailed, "", "STEP_FAILED"},
		{"agt_legacy", agentrun.StateCompleted, "Legacy answer.", ""},
	} {
		_, task := newRuntimeTask(t, store, "run-one", fixture.id)
		task.State, task.Ledger.AnswerText, task.FailureCode = fixture.state, fixture.answer, fixture.failure
		task.Version++
		if err := store.Save(ctx, task, task.Version-1); err != nil {
			t.Fatalf("save %s: %v", fixture.id, err)
		}
	}
	// Simulate a row written before timestamps, plan steps, and answering-agent
	// identity were projected. The store must still decode it and the served
	// task RPC must return the same id that List exposed.
	e.db.Exec(t, `UPDATE agent_task SET created_at=timestamptz '0001-01-01T00:00:00Z', updated_at=timestamptz '0001-01-01T00:00:00Z',
		plan='{"revision":1,"steps":[],"confirmed":false,"digest":""}'::jsonb WHERE tenant_id=$1 AND task_id='agt_legacy'`, e.one)

	principal, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant: "run-one", Subject: "user-1", SubjectKind: trust.SubjectKindHuman, OrganizationScopeID: "org:run-one",
		Roles: []string{"worker_self"}, Purposes: []string{"agent-task"}, AuthenticationMethod: trust.AuthenticationMethodBearerToken,
		Assurance: trust.AssuranceLow, SessionRef: "session-agentux", IssuedAt: t0.Add(-time.Minute), ExpiresAt: t0.Add(time.Hour), CredentialDigest: "sha256:agentux-page4",
	})
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer(grpc.UnaryInterceptor(func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		return handler(trust.WithPrincipal(ctx, principal), req)
	}))
	transportagents.Register(server, transportagents.Dependencies{Settings: agentUXPage4Settings{}, Tasks: agentUXPage4TaskReader{store: store}})
	listener := bufconn.Listen(1 << 20)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { server.Stop(); _ = listener.Close() })
	conn, err := grpc.NewClient("passthrough:///bufnet", grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	client := agentv1.NewAgentServiceClient(conn)
	listed, err := client.ListAgentTasks(ctx, &agentv1.ListAgentTasksRequest{})
	if err != nil || len(listed.GetTasks()) != 4 {
		t.Fatalf("list = %+v, %v", listed, err)
	}
	groups := map[string]int{"active": 0, "completed": 0, "failed": 0}
	for _, listedTask := range listed.GetTasks() {
		switch listedTask.GetState() {
		case string(agentrun.StateCompleted):
			groups["completed"]++
		case string(agentrun.StateFailed), string(agentrun.StateCancelled), string(agentrun.StateExpired):
			groups["failed"]++
		default:
			groups["active"]++
		}
		opened, getErr := client.GetAgentTask(ctx, &agentv1.GetAgentTaskRequest{TaskId: listedTask.GetTaskId()})
		if getErr != nil || opened.GetTask().GetTaskId() != listedTask.GetTaskId() || opened.GetTask().GetState() != listedTask.GetState() {
			t.Fatalf("open %q = %+v, %v", listedTask.GetTaskId(), opened, getErr)
		}
	}
	if groups["active"] != 1 || groups["completed"] != 2 || groups["failed"] != 1 {
		t.Fatalf("state groups = %+v", groups)
	}
	legacy, err := client.GetAgentTask(ctx, &agentv1.GetAgentTaskRequest{TaskId: "agt_legacy"})
	if err != nil || legacy.GetTask().GetCreatedAt() != nil || len(legacy.GetTask().GetSteps()) != 0 || legacy.GetTask().GetAnsweringAgentDisplayName() != "General agent" {
		t.Fatalf("legacy projection = %+v, %v", legacy, err)
	}
}
