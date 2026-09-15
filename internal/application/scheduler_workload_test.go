package application

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/runtimestate"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	executionscheduler "github.com/monstercameron/human-capital-management-suite/internal/platform/execution/scheduler"
)

func TestTodo_PROMO_EXEC_TIMERDISPATCH_TimerDispatchDisposition(t *testing.T) {
	if got := timerDispatchDisposition(app.ExecutionResult{InstanceID: "i"}, nil); got != executionscheduler.DispositionCompleted {
		t.Fatalf("success disposition = %q", got)
	}
	if got := timerDispatchDisposition(app.ExecutionResult{}, errors.New("transient")); got != executionscheduler.DispositionRetry {
		t.Fatalf("error disposition = %q", got)
	}
	if got := timerDispatchDisposition(app.ExecutionResult{}, nil); got != executionscheduler.DispositionAbandoned {
		t.Fatalf("empty result disposition = %q", got)
	}
}

func TestTodo_PROMO_EXEC_TIMERDISPATCH_TimerDispatcherRejectsForeignTenant(t *testing.T) {
	configured := pgstore.TenantID("tenant-a")
	foreign := pgstore.TenantID("tenant-b")
	called := false
	dispatcher := timerDispatcher(configured.String(), "tenant-a", func(context.Context, string, string, int) (app.ExecutionResult, error) {
		called = true
		return app.ExecutionResult{InstanceID: "unexpected"}, nil
	})
	disposition, err := dispatcher.Dispatch(context.Background(), executionscheduler.Work{Row: runtimestate.ReadyWork{TenantID: foreign}})
	if err != nil || disposition != executionscheduler.DispositionAbandoned {
		t.Fatalf("foreign dispatch = %q, %v; want ABANDONED without calling the resumer", disposition, err)
	}
	if called {
		t.Fatal("foreign tenant reached timer resumer")
	}
}

// A redelivered fired-timer row whose WAIT node an earlier delivery already
// consumed settles instead of being retried forever; any other failure is
// still a RETRY carrying its error.
func TestTodo_PROMO_EXEC_TIMERDISPATCH_TimerDispatcherSettlesStaleResume(t *testing.T) {
	tenant := pgstore.TenantID("tenant-a")
	work := executionscheduler.Work{Row: runtimestate.ReadyWork{TenantID: tenant, NodeID: "wait_effective_date", Attempt: 1}}

	calls := 0
	stale := timerDispatcher(tenant.String(), "tenant-a", func(context.Context, string, string, int) (app.ExecutionResult, error) {
		calls++
		return app.ExecutionResult{}, fmt.Errorf("%w: app: timer resume node %q is not on instance frontier",
			app.ErrParkedResumeStale, "wait_effective_date")
	})
	disposition, err := stale.Dispatch(context.Background(), work)
	if err != nil || disposition != executionscheduler.DispositionCompleted || calls != 1 {
		t.Fatalf("stale dispatch = %q, %v after %d calls; want COMPLETED with no error", disposition, err, calls)
	}

	transient := errors.New("connection reset")
	failing := timerDispatcher(tenant.String(), "tenant-a", func(context.Context, string, string, int) (app.ExecutionResult, error) {
		return app.ExecutionResult{}, transient
	})
	disposition, err = failing.Dispatch(context.Background(), work)
	if !errors.Is(err, transient) || disposition != executionscheduler.DispositionRetry {
		t.Fatalf("transient dispatch = %q, %v; want RETRY carrying the error", disposition, err)
	}
}
