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

// The task view's one-click "add budget" extends a paused task by exactly the
// policy's allowance, for its owner only, against the task's current budget
// revision, and leaves the task paused until the person resumes it.
func TestTodo_AGENT2_017_ExtendBudgetByAllowance(t *testing.T) {
	f := newFixture(t, nil)
	f.defaultOwner(t)
	f.runner.p.cfg.Authority = agentdelegation.ResolverFunc(func(user string, tenant values.TenantId, _ string, _ time.Time) (agentdelegation.UserAuthority, error) {
		return controlsAuthority(user, tenant, true, []string{controlRead}, []string{controlPurpose}), nil
	})
	task := f.start(t, "extension-allowance", planStep("read", agentrun.StepRead, "skill.lookup", agentrun.TierRead))
	paused, err := f.runner.Runtime.Pause(context.Background(), task.ID, task.Version, fixedNow)
	if err != nil {
		t.Fatal(err)
	}
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
	policy := f.runner.TaskPolicy(context.Background(), paused, task.UserID)
	if !policy.ExtendBudget {
		t.Fatalf("the policy does not offer the extension on a task paused at its ceiling: %+v", policy)
	}
	before := f.ledger.Snapshot().Tasks[0]
	allowance := f.ledger.ExtensionAllowance(task.ID)
	if _, err := f.runner.ExtendBudgetByAllowance(context.Background(), task.ID, "other-user", "request-other", fixedNow); !errors.Is(err, ErrDenied) {
		t.Fatalf("another user extended the budget: %v", err)
	}
	result, err := f.runner.ExtendBudgetByAllowance(context.Background(), task.ID, task.UserID, "request-allowance", fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	if result.Additional != allowance || result.Limit.Steps != before.Limit.Steps+allowance.Steps || allowance.Steps == 0 || result.PreviousRevision != before.Revision {
		t.Fatalf("extension = %+v, allowance %+v, before %+v", result, allowance, before)
	}
	if again, err := f.runner.ExtendBudgetByAllowance(context.Background(), task.ID, task.UserID, "request-allowance", fixedNow); err == nil && again.Revision != result.Revision {
		t.Fatalf("a repeated click extended the budget twice: %+v", again)
	}
	still, err := f.runner.Runtime.GetTask(context.Background(), task.ID)
	if err != nil || still.State != agentrun.StatePaused {
		t.Fatalf("the extension resumed the task: %+v %v", still, err)
	}
	if _, err := f.runner.ExtendBudgetByAllowance(context.Background(), "unknown-task", task.UserID, "request-unknown", fixedNow); err == nil {
		t.Fatal("an unknown task was extended")
	}
}
