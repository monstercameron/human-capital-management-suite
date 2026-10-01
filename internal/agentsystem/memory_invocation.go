package agentsystem

import (
	"context"
	"errors"

	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// ResolveTaskMemoryInvocation proves the task's current authority for one
// exact retained source step. A later checkpoint can reread a completed source
// step, but cannot change its skill, scope or immutable plan identity.
func (p *Platform) ResolveTaskMemoryInvocation(ctx context.Context, requested agentrun.AgentTask, sourceStep agentrun.PlanStep) (Invocation, error) {
	if p == nil || ctx == nil || requested.ID == "" || sourceStep.ID == "" {
		return Invocation{}, ErrDenied
	}
	runner, err := p.ForTenant(ctx, values.TenantId(requested.TenantID))
	if err != nil {
		return Invocation{}, err
	}
	if gate := p.cfg.WakeGate; gate != nil && !gate(ctx, requested.TenantID) {
		return Invocation{}, errors.Join(ErrDenied, ErrTenantDisabled)
	}
	task, err := runner.Runtime.GetTask(ctx, requested.ID)
	if err != nil {
		return Invocation{}, err
	}
	if task.State != agentrun.StateRunning || !task.Plan.Confirmed || task.TenantID != requested.TenantID || task.UserID != requested.UserID || task.Plan.Digest != requested.Plan.Digest || !task.ExpiresAt.After(p.cfg.Clock()) {
		return Invocation{}, ErrDenied
	}
	var step agentrun.PlanStep
	for _, candidate := range task.Plan.Steps {
		if candidate.ID == sourceStep.ID {
			step = candidate
			break
		}
	}
	if step.ID == "" || step.SkillID != sourceStep.SkillID || step.SkillVersion != sourceStep.SkillVersion || (step.State != agentrun.StepCompleted && !(step.State == agentrun.StepRunning && task.CurrentStep < len(task.Plan.Steps) && task.Plan.Steps[task.CurrentStep].ID == step.ID)) {
		return Invocation{}, ErrDenied
	}
	exec := &executor{runner: runner, mode: ModeOnBehalfOf}
	record, grant, err := exec.admit(ctx, task, step)
	if err != nil {
		return Invocation{}, err
	}
	credential, claims, err := exec.credential(task, step, record, grant)
	if err != nil {
		return Invocation{}, err
	}
	return Invocation{Task: task, Step: step, Skill: record, Claims: claims, Credential: credential}, nil
}
