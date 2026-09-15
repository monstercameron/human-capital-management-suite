package execute_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/cancellation"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

func cancellationDecisions(t *testing.T, f promotionFixture, instanceID uuid.UUID) []cancellation.Record {
	t.Helper()
	ctx := context.Background()
	tx, err := f.conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, f.tenantID); err != nil {
		t.Fatal(err)
	}
	recs, err := cancellation.Decisions(ctx, tx, f.tenantID, instanceID)
	if err != nil {
		t.Fatal(err)
	}
	return recs
}

// TestTodo_WF_RUN_010_DriverCancel proves execute.Driver.Cancel is the
// governed decision: a clean cancel reports CANCELLED with a recorded decision
// and intact history, a second cancel of the terminal instance is refused,
// and a cancel after the promotion's irreversible core effect records
// CANNOT_CANCEL and leaves the instance live.
func TestTodo_WF_RUN_010_DriverCancel(t *testing.T) {
	t.Run("clean", func(t *testing.T) {
		f := newPromotionWaitingFixture(t, "wfrun010-driver-clean", false)
		current := instanceByTenant(t, f)
		result, err := cancellationDriver(t, f).Cancel(context.Background(), cancelRequest(f, current))
		if err != nil {
			t.Fatal(err)
		}
		if result.Decision != workflow.Cancelled || result.DecisionID == uuid.Nil || result.Evidence.Digest == "" {
			t.Fatalf("result = %+v", result)
		}
		recs := cancellationDecisions(t, f, current.InstanceID)
		if len(recs) != 1 || recs[0].DecisionID != result.DecisionID || recs[0].StatusAfter != runtime.InstanceCancelled {
			t.Fatalf("decisions = %+v", recs)
		}
		after := instanceByTenant(t, f)
		if _, err := cancellationDriver(t, f).Cancel(context.Background(), cancelRequest(f, after)); !errors.Is(err, execute.ErrCancellationTerminal) {
			t.Fatalf("second cancel = %v, want terminal refusal", err)
		}
	})

	t.Run("irreversible effect", func(t *testing.T) {
		f := newPromotionFixture(t, "wfrun010-driver-effect")
		markCoreEffectExecuted(t, f)
		current := instanceByTenant(t, f)
		result, err := cancellationDriver(t, f).Cancel(context.Background(), cancelRequest(f, current))
		if !errors.Is(err, execute.ErrCancellationAmbiguous) || result.Decision != workflow.CannotCancel {
			t.Fatalf("result = %+v, err = %v", result, err)
		}
		after := instanceByTenant(t, f)
		if after.InstanceVersion != current.InstanceVersion || after.RuntimeStatus != current.RuntimeStatus {
			t.Fatalf("a refused cancel changed the instance %s/%d -> %s/%d", current.RuntimeStatus, current.InstanceVersion, after.RuntimeStatus, after.InstanceVersion)
		}
		if recs := cancellationDecisions(t, f, current.InstanceID); len(recs) != 1 || recs[0].Decision != workflow.CannotCancel {
			t.Fatalf("refusal evidence = %+v", recs)
		}
	})

	t.Run("invalid and stale", func(t *testing.T) {
		f := newPromotionWaitingFixture(t, "wfrun010-driver-invalid", false)
		current := instanceByTenant(t, f)
		req := cancelRequest(f, current)
		req.RequestedBy = ""
		if _, err := cancellationDriver(t, f).Cancel(context.Background(), req); err == nil {
			t.Fatal("a request without an actor was accepted")
		}
		stale := cancelRequest(f, current)
		stale.ExpectedInstanceVersion++
		if _, err := cancellationDriver(t, f).Cancel(context.Background(), stale); !errors.Is(err, cancellation.ErrStale) {
			t.Fatalf("stale cancel = %v", err)
		}
		if recs := cancellationDecisions(t, f, current.InstanceID); len(recs) != 0 {
			t.Fatalf("refused requests recorded %d decisions", len(recs))
		}
	})
}
