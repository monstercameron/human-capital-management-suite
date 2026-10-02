package application

import (
	"context"
	"errors"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

var errAgentTaskProjectionDenied = errors.New("application: agent task projection denied")

// GetAgentTask returns only a durable task owned by the verified human.
func (s *agentStarter) GetAgentTask(ctx context.Context, principal *trust.Principal, id string) (agentrun.AgentTask, error) {
	if s == nil || s.platform == nil || !agentTaskProjectionPrincipal(ctx, principal) || strings.TrimSpace(id) == "" {
		return agentrun.AgentTask{}, errAgentTaskProjectionDenied
	}
	runner, err := s.platform.ForTenant(ctx, principal.Tenant())
	if err != nil {
		return agentrun.AgentTask{}, err
	}
	tasks, err := runner.UserTasks(ctx, principal.Subject())
	if err != nil {
		return agentrun.AgentTask{}, err
	}
	for _, task := range tasks {
		if task.ID == id && task.TenantID == principal.Tenant().String() && task.UserID == principal.Subject() {
			return task, nil
		}
	}
	return agentrun.AgentTask{}, agentrun.ErrNotFound
}

// ListAgentTasks returns only durable tasks owned by the verified human.
func (s *agentStarter) ListAgentTasks(ctx context.Context, principal *trust.Principal) ([]agentrun.AgentTask, error) {
	if s == nil || s.platform == nil || !agentTaskProjectionPrincipal(ctx, principal) {
		return nil, errAgentTaskProjectionDenied
	}
	runner, err := s.platform.ForTenant(ctx, principal.Tenant())
	if err != nil {
		return nil, err
	}
	tasks, err := runner.UserTasks(ctx, principal.Subject())
	if err != nil {
		return nil, err
	}
	out := make([]agentrun.AgentTask, 0, len(tasks))
	for _, task := range tasks {
		if task.TenantID == principal.Tenant().String() && task.UserID == principal.Subject() {
			out = append(out, task)
		}
	}
	return out, nil
}

func agentTaskProjectionPrincipal(ctx context.Context, principal *trust.Principal) bool {
	if ctx == nil || principal == nil || principal.SubjectKind() != trust.SubjectKindHuman {
		return false
	}
	verified, ok := trust.FromContext(ctx)
	return ok && verified != nil && verified.Subject() == principal.Subject() && verified.Tenant() == principal.Tenant()
}
