package agentsystem

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentdelegation"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

func controlsAuthority(user string, tenant values.TenantId, active bool, capabilities, purposes []string) agentdelegation.UserAuthority {
	return agentdelegation.UserAuthority{UserID: user, Active: active, Authority: trust.AuthorityScope{
		Tenant: tenant, Capabilities: capabilities, Purposes: purposes,
		NotBefore: fixedNow.Add(-time.Hour), ExpiresAt: fixedNow.Add(time.Hour),
	}}
}

func TestTaskPolicy_ProjectsOnlyCurrentLegalActions(t *testing.T) {
	f := newFixture(t, nil)
	f.runner.p.cfg.Authority = agentdelegation.ResolverFunc(func(user string, tenant values.TenantId, _ string, _ time.Time) (agentdelegation.UserAuthority, error) {
		return controlsAuthority(user, tenant, true, []string{controlRead}, []string{controlPurpose}), nil
	})
	cases := []struct {
		state agentrun.TaskState
		want  TaskActionPolicy
	}{
		{agentrun.StateAwaitingPlanConfirmation, TaskActionPolicy{ConfirmPlan: true, Cancel: true}},
		{agentrun.StateDrafting, TaskActionPolicy{ConfirmPlan: true, Cancel: true}},
		{agentrun.StateRunning, TaskActionPolicy{Pause: true, Cancel: true}},
		{agentrun.StateWaiting, TaskActionPolicy{Pause: true, Cancel: true}},
		{agentrun.StateAwaitingApproval, TaskActionPolicy{Pause: true, Cancel: true}},
		{agentrun.StatePaused, TaskActionPolicy{Resume: true, Cancel: true}},
		{agentrun.StateCompleted, TaskActionPolicy{}},
		{agentrun.StateFailed, TaskActionPolicy{}},
		{agentrun.StateCancelled, TaskActionPolicy{}},
		{agentrun.StateExpired, TaskActionPolicy{}},
		{"FUTURE_STATE", TaskActionPolicy{}},
	}
	for _, tc := range cases {
		t.Run(string(tc.state), func(t *testing.T) {
			task := agentrun.AgentTask{ID: "task", TenantID: tenantKey, UserID: "user-42", State: tc.state}
			if got := f.runner.TaskPolicy(context.Background(), task, "user-42"); got != tc.want {
				t.Fatalf("TaskPolicy(%s) = %+v, want %+v", tc.state, got, tc.want)
			}
		})
	}
}

func TestTaskPolicy_FailsClosedWithoutExactCurrentAuthority(t *testing.T) {
	f := newFixture(t, nil)
	task := agentrun.AgentTask{ID: "task", TenantID: tenantKey, UserID: "user-42", State: agentrun.StateRunning}
	good := func(user string, tenant values.TenantId, _ string, _ time.Time) (agentdelegation.UserAuthority, error) {
		return controlsAuthority(user, tenant, true, []string{controlRead}, []string{controlPurpose}), nil
	}
	denials := []struct {
		name     string
		resolver agentdelegation.AuthorityResolver
		user     string
		task     agentrun.AgentTask
	}{
		{name: "missing authority"},
		{name: "resolver error", resolver: agentdelegation.ResolverFunc(func(string, values.TenantId, string, time.Time) (agentdelegation.UserAuthority, error) {
			return agentdelegation.UserAuthority{}, errors.New("authority unavailable")
		}), user: "user-42", task: task},
		{name: "inactive", resolver: agentdelegation.ResolverFunc(func(user string, tenant values.TenantId, purpose string, now time.Time) (agentdelegation.UserAuthority, error) {
			decision, _ := good(user, tenant, purpose, now)
			decision.Active = false
			return decision, nil
		}), user: "user-42", task: task},
		{name: "wrong resolved user", resolver: agentdelegation.ResolverFunc(func(user string, tenant values.TenantId, purpose string, now time.Time) (agentdelegation.UserAuthority, error) {
			decision, _ := good(user, tenant, purpose, now)
			decision.UserID = "someone-else"
			return decision, nil
		}), user: "user-42", task: task},
		{name: "wrong tenant", resolver: agentdelegation.ResolverFunc(func(user string, tenant values.TenantId, purpose string, now time.Time) (agentdelegation.UserAuthority, error) {
			decision, _ := good(user, tenant, purpose, now)
			decision.Authority.Tenant = "another-tenant"
			return decision, nil
		}), user: "user-42", task: task},
		{name: "missing read capability", resolver: agentdelegation.ResolverFunc(func(user string, tenant values.TenantId, purpose string, now time.Time) (agentdelegation.UserAuthority, error) {
			return controlsAuthority(user, tenant, true, nil, []string{controlPurpose}), nil
		}), user: "user-42", task: task},
		{name: "missing purpose", resolver: agentdelegation.ResolverFunc(func(user string, tenant values.TenantId, _ string, _ time.Time) (agentdelegation.UserAuthority, error) {
			return controlsAuthority(user, tenant, true, []string{controlRead}, nil), nil
		}), user: "user-42", task: task},
		{name: "task owner mismatch", resolver: agentdelegation.ResolverFunc(good), user: "another-user", task: task},
		{name: "task tenant mismatch", resolver: agentdelegation.ResolverFunc(good), user: "user-42", task: agentrun.AgentTask{ID: "task", TenantID: "another-tenant", UserID: "user-42", State: agentrun.StateRunning}},
		{name: "missing principal", resolver: agentdelegation.ResolverFunc(good), user: " ", task: task},
	}
	for _, tc := range denials {
		t.Run(tc.name, func(t *testing.T) {
			f.runner.p.cfg.Authority = tc.resolver
			if got := f.runner.TaskPolicy(context.Background(), tc.task, tc.user); got != (TaskActionPolicy{}) {
				t.Fatalf("TaskPolicy for denied input = %+v, want no actions", got)
			}
		})
	}
}

func TestControlTask_EnforcesOwnerCASAndRuntimeLegality(t *testing.T) {
	f := newFixture(t, nil)
	ctx := context.Background()
	f.runner.p.cfg.Authority = agentdelegation.ResolverFunc(func(user string, tenant values.TenantId, _ string, _ time.Time) (agentdelegation.UserAuthority, error) {
		return controlsAuthority(user, tenant, true, []string{controlRead}, []string{controlPurpose}), nil
	})
	request := StartRequest{
		TaskID: "control-cas", UserID: "user-42", AgentVersion: "agent-v1", InstallationID: "install-1",
		Purpose: purposeKey, OrganizationScopeID: "org-west", Goal: "controlled task",
		Steps:         []agentrun.PlanStep{planStep("read", agentrun.StepRead, "skill.lookup", agentrun.TierRead)},
		UserAuthority: userAuthority(true).Authority, Lifetime: time.Hour,
	}
	task, err := f.runner.StartTask(ctx, request)
	if err != nil {
		t.Fatalf("StartTask: %v", err)
	}
	if _, err := f.runner.ControlTask(ctx, task.ID, "other-user", ActionConfirmPlan, task.Version, fixedNow); !errors.Is(err, ErrDenied) {
		t.Fatalf("other owner control = %v, want ErrDenied", err)
	}
	confirmed, err := f.runner.ControlTask(ctx, task.ID, "user-42", ActionConfirmPlan, task.Version, fixedNow)
	if err != nil || confirmed.State != agentrun.StateRunning {
		t.Fatalf("confirm = %+v, %v, want RUNNING", confirmed, err)
	}
	if _, err := f.runner.ControlTask(ctx, task.ID, "user-42", ActionConfirmPlan, task.Version, fixedNow); !errors.Is(err, agentrun.ErrConflict) {
		t.Fatalf("replayed confirmation = %v, want ErrConflict", err)
	}
	paused, err := f.runner.ControlTask(ctx, task.ID, "user-42", ActionPause, confirmed.Version, fixedNow)
	if err != nil || paused.State != agentrun.StatePaused {
		t.Fatalf("pause = %+v, %v, want PAUSED", paused, err)
	}
	if _, err := f.runner.ControlTask(ctx, task.ID, "user-42", ActionResume, confirmed.Version, fixedNow); !errors.Is(err, agentrun.ErrConflict) {
		t.Fatalf("stale resume = %v, want ErrConflict", err)
	}
	resumed, err := f.runner.ControlTask(ctx, task.ID, "user-42", ActionResume, paused.Version, fixedNow)
	if err != nil || resumed.State != agentrun.StateRunning {
		t.Fatalf("resume = %+v, %v, want RUNNING", resumed, err)
	}
	cancelled, err := f.runner.ControlTask(ctx, task.ID, "user-42", ActionCancel, resumed.Version, fixedNow)
	if err != nil || cancelled.State != agentrun.StateCancelled {
		t.Fatalf("cancel = %+v, %v, want CANCELLED", cancelled, err)
	}
	if _, err := f.runner.ControlTask(ctx, task.ID, "user-42", ActionCancel, cancelled.Version, fixedNow); !errors.Is(err, agentrun.ErrTerminal) {
		t.Fatalf("terminal cancel = %v, want ErrTerminal", err)
	}
	if _, err := f.runner.ControlTask(ctx, task.ID, "user-42", TaskAction("unknown"), cancelled.Version, fixedNow); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unknown action = %v, want ErrInvalid", err)
	}
}

func TestControlTask_RechecksRevocableAuthorityBeforeTransition(t *testing.T) {
	f := newFixture(t, nil)
	ctx := context.Background()
	task, err := f.runner.StartTask(ctx, StartRequest{
		TaskID: "control-authority", UserID: "user-42", AgentVersion: "agent-v1", InstallationID: "install-1",
		Purpose: purposeKey, OrganizationScopeID: "org-west", Goal: "controlled task",
		Steps:         []agentrun.PlanStep{planStep("read", agentrun.StepRead, "skill.lookup", agentrun.TierRead)},
		UserAuthority: userAuthority(true).Authority, Lifetime: time.Hour,
	})
	if err != nil {
		t.Fatalf("StartTask: %v", err)
	}
	denials := []struct {
		name     string
		decision agentdelegation.UserAuthority
	}{
		{name: "revoked", decision: controlsAuthority("user-42", tenantKey, false, []string{controlRead}, []string{controlPurpose})},
		{name: "capability removed", decision: controlsAuthority("user-42", tenantKey, true, nil, []string{controlPurpose})},
		{name: "purpose removed", decision: controlsAuthority("user-42", tenantKey, true, []string{controlRead}, nil)},
		{name: "wrong tenant", decision: controlsAuthority("user-42", "another-tenant", true, []string{controlRead}, []string{controlPurpose})},
		{name: "wrong user", decision: controlsAuthority("other-user", tenantKey, true, []string{controlRead}, []string{controlPurpose})},
	}
	for _, tc := range denials {
		t.Run(tc.name, func(t *testing.T) {
			f.runner.p.cfg.Authority = agentdelegation.ResolverFunc(func(string, values.TenantId, string, time.Time) (agentdelegation.UserAuthority, error) {
				return tc.decision, nil
			})
			if _, err := f.runner.ControlTask(ctx, task.ID, "user-42", ActionConfirmPlan, task.Version, fixedNow); !errors.Is(err, ErrDenied) {
				t.Fatalf("unauthorized direct control = %v, want ErrDenied", err)
			}
			current, err := f.runner.Runtime.GetTask(ctx, task.ID)
			if err != nil || current.State != task.State || current.Version != task.Version {
				t.Fatalf("denied control changed task = %+v, %v", current, err)
			}
		})
	}
	f.runner.p.cfg.Authority = agentdelegation.ResolverFunc(func(user string, tenant values.TenantId, _ string, _ time.Time) (agentdelegation.UserAuthority, error) {
		return controlsAuthority(user, tenant, true, []string{controlRead}, []string{controlPurpose}), nil
	})
	if confirmed, err := f.runner.ControlTask(ctx, task.ID, "user-42", ActionConfirmPlan, task.Version, fixedNow); err != nil || confirmed.State != agentrun.StateRunning {
		t.Fatalf("control with current authority = %+v, %v, want RUNNING", confirmed, err)
	}
}
