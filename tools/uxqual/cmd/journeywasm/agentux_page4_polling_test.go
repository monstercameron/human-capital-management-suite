package main

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

func TestTodo_AGENTUX_011_PollingStopConditions(t *testing.T) {
	tasks := []productui.AgentTask{{ID: "listed"}}
	if selectedAgentTaskCanRefresh("stale", tasks) || !selectedAgentTaskCanRefresh("listed", tasks) {
		t.Fatal("detail refresh did not stay within the owner-scoped list")
	}
	var calls atomic.Int32
	updates := make(chan productui.AgentTask, 2)
	pollAgentTask(context.Background(), time.Millisecond, "listed", func(context.Context, string) (productui.AgentTask, error) {
		call := calls.Add(1)
		state := "running"
		if call == 2 {
			state = "completed"
		}
		// Polling stops on the task's own final state; a step state alone
		// does not end it.
		return productui.AgentTask{ID: "listed", State: productui.AgentTaskState(state), Steps: []productui.AgentTaskStep{{State: state}}}, nil
	}, func(task productui.AgentTask) {
		select {
		case updates <- task:
		default:
			t.Error("polling continued after the task reached a final state")
		}
	})
	if calls.Load() != 2 || len(updates) != 2 {
		t.Fatalf("final step did not stop polling: calls=%d updates=%d", calls.Load(), len(updates))
	}

	ctx, cancel := context.WithCancel(context.Background())
	calls.Store(0)
	done := make(chan struct{})
	go func() {
		pollAgentTask(ctx, time.Millisecond, "listed", func(context.Context, string) (productui.AgentTask, error) {
			calls.Add(1)
			return productui.AgentTask{ID: "listed", Steps: []productui.AgentTaskStep{{State: "working"}}}, nil
		}, func(productui.AgentTask) { cancel() })
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("page-leave cancellation did not stop polling")
	}
	if calls.Load() != 1 {
		t.Fatalf("cancelled polling calls = %d", calls.Load())
	}
}
