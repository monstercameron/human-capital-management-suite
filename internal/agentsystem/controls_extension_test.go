package agentsystem

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentbudget"
	"github.com/monstercameron/human-capital-management-suite/internal/agentdelegation"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestTodo_AGENT2_012_ExtendBudgetRequiresPausedOwnerAndExplicitResume(t *testing.T) {
	f := newFixture(t, nil)
	f.defaultOwner(t)
	f.runner.p.cfg.Authority = agentdelegation.ResolverFunc(func(user string, tenant values.TenantId, _ string, _ time.Time) (agentdelegation.UserAuthority, error) {
		return controlsAuthority(user, tenant, true, []string{controlRead}, []string{controlPurpose}), nil
	})
	task := f.start(t, "extension-runner", planStep("read", agentrun.StepRead, "skill.lookup", agentrun.TierRead))
	pausedRuntime, err := f.runner.Runtime.Pause(context.Background(), task.ID, task.Version, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.runner.ExtendBudget(context.Background(), task.ID, task.UserID, "request-before-budget-pause", 1, agentbudget.Limits{Steps: 1}, fixedNow); !errors.Is(err, agentbudget.ErrExtensionUnavailable) {
		t.Fatalf("runtime-only pause extension = %v", err)
	}
	// Drive the budget to its task ceiling while the runtime remains paused.
	for _, step := range []string{"s1", "s2"} {
		reservation, reserveErr := f.ledger.Reserve(context.Background(), agentbudget.Request{TaskID: task.ID, StepID: step, Fingerprint: step, Estimate: agentbudget.Usage{Steps: 50, Tokens: 1, WallClock: 1, SpendMicros: 1}})
		if reserveErr == nil {
			if settleErr := reservation.Settle(agentbudget.Usage{Steps: 50, Tokens: 1, WallClock: 1, SpendMicros: 1}); settleErr != nil {
				t.Fatal(settleErr)
			}
			continue
		}
		if !errors.Is(reserveErr, agentbudget.ErrPaused) {
			t.Fatal(reserveErr)
		}
	}
	snapshot := f.ledger.Snapshot()
	if len(snapshot.Tasks) != 1 || snapshot.Tasks[0].Paused == "" {
		t.Fatalf("budget did not pause: %+v", snapshot.Tasks)
	}
	result, err := f.runner.ExtendBudget(context.Background(), task.ID, task.UserID, "request-authorized", snapshot.Tasks[0].Revision, agentbudget.Limits{Steps: 1}, fixedNow)
	if err != nil || result.Revision == 0 || result.Limit.Steps != 51 {
		t.Fatalf("authorized extension = %+v, %v", result, err)
	}
	stillPaused, err := f.runner.Runtime.GetTask(context.Background(), task.ID)
	if err != nil || stillPaused.State != agentrun.StatePaused {
		t.Fatalf("extension resumed runtime: %+v, %v", stillPaused, err)
	}
	if _, err := f.runner.ExtendBudget(context.Background(), task.ID, "other-user", "request-other", result.Revision, agentbudget.Limits{Steps: 1}, fixedNow); !errors.Is(err, ErrDenied) {
		t.Fatalf("wrong owner extension = %v", err)
	}
	resumed, err := f.runner.ControlTask(context.Background(), task.ID, task.UserID, ActionResume, pausedRuntime.Version, fixedNow)
	if err != nil || resumed.State != agentrun.StateRunning {
		t.Fatalf("explicit resume = %+v, %v", resumed, err)
	}
}

func TestTodo_AGENT2_012_ExtendBudgetHonorsTenantWakeGate(t *testing.T) {
	f := newFixture(t, nil)
	f.defaultOwner(t)
	f.runner.p.cfg.Authority = agentdelegation.ResolverFunc(func(user string, tenant values.TenantId, _ string, _ time.Time) (agentdelegation.UserAuthority, error) {
		return controlsAuthority(user, tenant, true, []string{controlRead}, []string{controlPurpose}), nil
	})
	f.runner.p.cfg.WakeGate = func(context.Context, string) bool { return false }
	task := f.start(t, "extension-gate", planStep("read", agentrun.StepRead, "skill.lookup", agentrun.TierRead))
	paused, err := f.runner.Runtime.Pause(context.Background(), task.ID, task.Version, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.runner.ExtendBudget(context.Background(), task.ID, task.UserID, "request-gated", 1, agentbudget.Limits{Steps: 1}, fixedNow); !errors.Is(err, ErrDenied) {
		t.Fatalf("disabled tenant extension = %v", err)
	}
	if paused.State != agentrun.StatePaused {
		t.Fatalf("fixture pause = %s", paused.State)
	}
}
