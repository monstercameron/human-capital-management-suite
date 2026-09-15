package application

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/runtimestate"
	"github.com/monstercameron/human-capital-management-suite/internal/data/signals"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	executionscheduler "github.com/monstercameron/human-capital-management-suite/internal/platform/execution/scheduler"
)

func TestTodo_WF_RUN_005_SignalResumerDispositions(t *testing.T) {
	tenant := pgstore.TenantID("tenant-a")
	instanceID, signalID, subscriptionID := uuid.New(), uuid.New(), uuid.New()
	work := executionscheduler.SignalWork{
		Row:     runtimestate.ReadyWork{TenantID: tenant, InstanceID: instanceID, NodeID: "await", Attempt: 2},
		Receipt: signals.MatchedReceipt{SignalID: signalID, SubscriptionID: subscriptionID},
	}
	var calls []string
	result := app.ExecutionResult{}
	var resumeErr error
	resumer := signalResumer(tenant.String(), "tenant-a", func(ctx context.Context, instance, node string, attempt int, signal, subscription string) (app.ExecutionResult, error) {
		calls = append(calls, fmt.Sprintf("%s|%s|%d|%s|%s", instance, node, attempt, signal, subscription))
		return result, resumeErr
	})

	result = app.ExecutionResult{InstanceID: instanceID.String()}
	if got, err := resumer.ResumeSignal(context.Background(), work); err != nil || got != executionscheduler.DispositionCompleted {
		t.Fatalf("resumed = %s, %v; want COMPLETED", got, err)
	}
	want := fmt.Sprintf("%s|await|2|%s|%s", instanceID, signalID, subscriptionID)
	if len(calls) != 1 || calls[0] != want {
		t.Fatalf("resume calls = %v, want exactly [%s] (the receipt identity, not the payload)", calls, want)
	}

	result, resumeErr = app.ExecutionResult{}, fmt.Errorf("%w: node consumed", app.ErrParkedResumeStale)
	if got, err := resumer.ResumeSignal(context.Background(), work); err != nil || got != executionscheduler.DispositionCompleted {
		t.Fatalf("stale resume = %s, %v; want COMPLETED so a consumed wait is never retried", got, err)
	}
	resumeErr = errors.New("transient")
	if got, err := resumer.ResumeSignal(context.Background(), work); err == nil || got != executionscheduler.DispositionRetry {
		t.Fatalf("transient failure = %s, %v; want RETRY with the error", got, err)
	}

	foreign := work
	foreign.Row.TenantID = pgstore.TenantID("tenant-b")
	before := len(calls)
	if got, err := resumer.ResumeSignal(context.Background(), foreign); err != nil || got != executionscheduler.DispositionAbandoned || len(calls) != before {
		t.Fatalf("foreign tenant = %s, %v, calls %d; want ABANDONED without resuming", got, err, len(calls)-before)
	}
}
