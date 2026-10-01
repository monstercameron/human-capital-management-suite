package agentclient

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentbudget"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

func TestTaskState_PreservesControlStates(t *testing.T) {
	cases := []struct {
		name string
		in   agentrun.TaskState
		want productui.AgentTaskState
	}{
		{"waiting", agentrun.StateWaiting, productui.AgentTaskWaiting},
		{"drafting", agentrun.StateDrafting, productui.AgentTaskDrafting},
		{"plan confirmation", agentrun.StateAwaitingPlanConfirmation, productui.AgentTaskAwaitingPlanConfirmation},
		{"cancelled", agentrun.StateCancelled, productui.AgentTaskCancelled},
		{"expired", agentrun.StateExpired, productui.AgentTaskExpired},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := TaskState(tc.in); got != tc.want {
				t.Fatalf("TaskState(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

type controlPolicyReader struct {
	tasks  []agentrun.AgentTask
	policy agentsystem.TaskActionPolicy
	err    error
}

func (r controlPolicyReader) UserTasks(context.Context, string) ([]agentrun.AgentTask, error) {
	return r.tasks, r.err
}

func (controlPolicyReader) BudgetUsage(string) (agentbudget.Limits, agentbudget.Limits, bool) {
	return agentbudget.Limits{}, agentbudget.Limits{}, false
}

func (r controlPolicyReader) TaskPolicy(context.Context, agentrun.AgentTask, string) agentsystem.TaskActionPolicy {
	return r.policy
}

type controlPolicyRunners struct {
	reader TaskReader
	err    error
}

func (r controlPolicyRunners) Runner(context.Context, values.TenantId) (TaskReader, error) {
	return r.reader, r.err
}

type controlSetting struct {
	enabled bool
	err     error
}

func (s controlSetting) AgentsEnabled(context.Context, values.TenantId) (bool, error) {
	return s.enabled, s.err
}

type controlController struct{}

func (controlController) ControlTask(context.Context, *trust.Principal, TaskControl) (ControlledTask, error) {
	return ControlledTask{}, nil
}

func TestClientTaskPolicy_FailsClosedWithoutTenantControls(t *testing.T) {
	want := ActionPolicy{ConfirmPlan: true, Cancel: true}
	reader := controlPolicyReader{
		tasks:  []agentrun.AgentTask{{ID: "task", TenantID: "tenant-a", UserID: "user-a", State: agentrun.StateAwaitingPlanConfirmation}},
		policy: agentsystem.TaskActionPolicy{ConfirmPlan: true, Cancel: true},
	}
	runners := controlPolicyRunners{reader: reader}
	cases := []struct {
		name   string
		client *Client
		want   ActionPolicy
	}{
		{name: "enabled with controller", client: NewWithControls(runners, controlSetting{enabled: true}, controlController{}), want: want},
		{name: "setting absent", client: New(runners)},
		{name: "controller absent", client: NewWithControls(runners, controlSetting{enabled: true}, nil)},
		{name: "tenant disabled", client: NewWithControls(runners, controlSetting{}, controlController{})},
		{name: "setting unavailable", client: NewWithControls(runners, controlSetting{enabled: true, err: errors.New("settings unavailable")}, controlController{})},
		{name: "runner unavailable", client: NewWithControls(controlPolicyRunners{err: errors.New("runner unavailable")}, controlSetting{enabled: true}, controlController{})},
		{name: "authority projection absent", client: NewWithControls(controlPolicyRunners{reader: leakyReader{}}, controlSetting{enabled: true}, controlController{})},
		{name: "owner mismatch", client: NewWithControls(controlPolicyRunners{reader: controlPolicyReader{tasks: []agentrun.AgentTask{{ID: "task", TenantID: "tenant-a", UserID: "user-b"}}, policy: reader.policy}}, controlSetting{enabled: true}, controlController{})},
		{name: "tenant mismatch", client: NewWithControls(controlPolicyRunners{reader: controlPolicyReader{tasks: []agentrun.AgentTask{{ID: "task", TenantID: "tenant-b", UserID: "user-a"}}, policy: reader.policy}}, controlSetting{enabled: true}, controlController{})},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.client.TaskPolicy(context.Background(), "tenant-a", "user-a", "task")
			if got != tc.want {
				t.Fatalf("TaskPolicy = %+v, want %+v", got, tc.want)
			}
		})
	}
}
