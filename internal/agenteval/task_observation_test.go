package agenteval

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentbudget"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

type taskReaderStub struct {
	task agentrun.AgentTask
	err  error
}

func (r taskReaderStub) Get(context.Context, string) (agentrun.AgentTask, error) {
	return r.task, r.err
}

type usageReaderStub struct {
	result agentbudget.SettledTaskUsage
	err    error
	calls  int
}

func (r *usageReaderStub) SettledTaskUsage(context.Context, values.TenantId, string) (agentbudget.SettledTaskUsage, error) {
	r.calls++
	return r.result, r.err
}

func TestTodo_AGENT2_025_UsesDurableTerminalTimeAndSettledTaskUsage(t *testing.T) {
	finished := time.Date(2026, 9, 30, 12, 30, 0, 0, time.FixedZone("EDT", -4*60*60))
	tasks := taskReaderStub{task: agentrun.AgentTask{ID: "task-7", TenantID: "tenant-a", State: agentrun.StateCompleted, UpdatedAt: finished}}
	usage := &usageReaderStub{result: agentbudget.SettledTaskUsage{TenantID: "tenant-a", TaskID: "task-7", Usage: agentbudget.Usage{
		Steps: 4, Tokens: 890, WallClock: 3 * time.Minute, SpendMicros: 271,
	}}}

	got, err := ObserveTask(context.Background(), values.TenantId("tenant-a"), "task-7", tasks, usage)
	if err != nil {
		t.Fatal(err)
	}
	if got.TenantID != "tenant-a" || got.TaskID != "task-7" || got.State != agentrun.StateCompleted || !got.TerminalAt.Equal(finished.UTC()) {
		t.Fatalf("observation identity/state/time = %#v", got)
	}
	if got.SettledUsage != usage.result.Usage {
		t.Fatalf("settled usage = %#v, want %#v", got.SettledUsage, usage.result.Usage)
	}
}

func TestTodo_AGENT2_025_FailsClosedWithoutTerminalOrSettledEvidence(t *testing.T) {
	terminalTask := agentrun.AgentTask{ID: "task-7", TenantID: "tenant-a", State: agentrun.StateCompleted, UpdatedAt: time.Now()}
	tests := []struct {
		name string
		task agentrun.AgentTask
		err  error
	}{
		{name: "active task", task: agentrun.AgentTask{ID: "task-7", TenantID: "tenant-a", State: agentrun.StateRunning, UpdatedAt: terminalTask.UpdatedAt}},
		{name: "terminal time absent", task: agentrun.AgentTask{ID: "task-7", TenantID: "tenant-a", State: agentrun.StateCompleted}},
		{name: "settled usage absent", task: terminalTask, err: errors.New("durable usage missing")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			usage := &usageReaderStub{err: tc.err, result: agentbudget.SettledTaskUsage{TenantID: "tenant-a", TaskID: "task-7"}}
			_, err := ObserveTask(context.Background(), values.TenantId("tenant-a"), "task-7", taskReaderStub{task: tc.task}, usage)
			if err == nil {
				t.Fatal("expected observation to fail without terminal and settled evidence")
			}
			if tc.name == "active task" || tc.name == "terminal time absent" {
				if !errors.Is(err, ErrTaskNotTerminal) || usage.calls != 0 {
					t.Fatalf("error=%v usage reads=%d", err, usage.calls)
				}
			} else if !errors.Is(err, tc.err) {
				t.Fatalf("error=%v, want wrapped %v", err, tc.err)
			}
		})
	}
}

func TestTodo_AGENT2_025_RejectsCrossTenantTaskOrUsage(t *testing.T) {
	finished := time.Date(2026, 9, 30, 12, 30, 0, 0, time.UTC)
	validTask := agentrun.AgentTask{ID: "task-7", TenantID: "tenant-a", State: agentrun.StateCompleted, UpdatedAt: finished}
	validUsage := agentbudget.SettledTaskUsage{TenantID: "tenant-a", TaskID: "task-7"}
	cases := []struct {
		name  string
		task  agentrun.AgentTask
		usage agentbudget.SettledTaskUsage
	}{
		{name: "foreign task", task: func() agentrun.AgentTask { x := validTask; x.TenantID = "tenant-b"; return x }(), usage: validUsage},
		{name: "foreign usage", task: validTask, usage: func() agentbudget.SettledTaskUsage { x := validUsage; x.TenantID = "tenant-b"; return x }()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ObserveTask(context.Background(), values.TenantId("tenant-a"), "task-7", taskReaderStub{task: tc.task}, &usageReaderStub{result: tc.usage})
			if !errors.Is(err, ErrIdentityMismatch) {
				t.Fatalf("error=%v, want identity mismatch", err)
			}
		})
	}
}
