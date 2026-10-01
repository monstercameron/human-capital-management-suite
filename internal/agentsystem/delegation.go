package agentsystem

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentaudit"
	"github.com/monstercameron/human-capital-management-suite/internal/agentbudget"
	"github.com/monstercameron/human-capital-management-suite/internal/agentdelegation"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

var ErrParentStopped = errors.New("agentsystem: specialist parent is stopped")

// DelegateRequest is assembled from a published specialist version and an
// accepted common run request. ParentCredential is the current calling step,
// never a browser token. The specialist remains a task owned by the invoker.
type DelegateRequest struct {
	ParentTaskID, ParentCredential                string
	AdmissionID                                   string
	TaskID, AgentID, AgentVersion, InstallationID string
	Goal                                          string
	Constraints                                   []string
	Steps                                         []agentrun.PlanStep
	Authority                                     trust.AuthorityScope
	Limit                                         agentbudget.Limits
	Deadline                                      time.Time
}

// Delegate creates and confirms a durable child from the already confirmed
// parent skill pins. It opens a child allocation under the root budget, so
// inherited spend cannot be hidden in a new independent task ceiling.
func (r *Runner) Delegate(ctx context.Context, req DelegateRequest) (agentrun.AgentTask, error) {
	if r == nil || r.p == nil || req.AgentID == "" || req.TaskID == "" || req.ParentTaskID == "" {
		return agentrun.AgentTask{}, ErrInvalid
	}
	parent, err := r.Runtime.GetTask(ctx, req.ParentTaskID)
	if err != nil {
		return agentrun.AgentTask{}, err
	}
	if parent.TenantID != r.tenant.String() || !parent.Plan.Confirmed || parent.State != agentrun.StateRunning || parent.CurrentStep >= len(parent.Plan.Steps) {
		return agentrun.AgentTask{}, ErrDenied
	}
	parent, err = r.recheckTaskLineage(ctx, parent)
	if err != nil {
		return parent, err
	}
	claims, err := r.Delegation.Verify(req.ParentCredential, agentdelegation.VerifyRequest{Audience: r.p.cfg.Audience, Sender: r.p.cfg.Workload})
	if err != nil {
		return agentrun.AgentTask{}, err
	}
	if claims.Subject != parent.UserID || claims.Actor.RunID != parent.ID || claims.Actor.StepID != parent.Plan.Steps[parent.CurrentStep].ID || claims.GrantID != GrantID(parent.ID) {
		return agentrun.AgentTask{}, ErrDenied
	}
	grant, err := r.grants.Get(claims.GrantID)
	if err != nil {
		return agentrun.AgentTask{}, err
	}
	if parent.DelegationDepth >= agentaudit.MaxSubAgentDepth {
		return agentrun.AgentTask{}, fmt.Errorf("%w: specialist depth exhausted", ErrDenied)
	}
	if req.Deadline.IsZero() || !req.Deadline.After(r.p.cfg.Clock()) || req.Deadline.After(parent.ExpiresAt) {
		return agentrun.AgentTask{}, ErrDenied
	}
	plan, err := agentrun.NewPlan(req.Steps)
	if err != nil {
		return agentrun.AgentTask{}, err
	}
	if req.AdmissionID != "" && r.p.cfg.ChildAuthority == nil {
		return agentrun.AgentTask{}, fmt.Errorf("%w: child admission authority unavailable", ErrDenied)
	}
	for _, step := range plan.Steps {
		pinned := false
		for _, ancestor := range parent.Plan.Steps {
			if step.SkillID == ancestor.SkillID && step.SkillVersion == ancestor.SkillVersion && step.Tier <= ancestor.Tier {
				pinned = true
				break
			}
		}
		if !pinned {
			return agentrun.AgentTask{}, fmt.Errorf("%w: specialist skill/tier was not confirmed in the parent", ErrDenied)
		}
		for _, input := range step.Inputs {
			if input.SourceID != "" && !slices.Contains(req.Authority.Resources, input.SourceID) {
				return agentrun.AgentTask{}, fmt.Errorf("%w: specialist source is outside its grant", ErrDenied)
			}
		}
	}
	records, scopes, err := r.pinSkills(plan.Steps)
	if err != nil {
		return agentrun.AgentTask{}, err
	}
	request := agentdelegation.GrantRequest{GrantID: GrantID(req.TaskID), CommonAdmissionID: req.AdmissionID, UserID: parent.UserID, Tenant: r.tenant, AgentVersion: req.AgentVersion, TargetAgentID: req.AgentID, InstallationID: req.InstallationID, TaskID: req.TaskID, PlanSkillSetDigest: skillSetDigest(records), Purpose: grant.Purpose, OrganizationScopeID: grant.OrganizationScopeID, Skills: skillIDs(records), SkillScopes: scopes, NotBefore: r.p.cfg.Clock().UTC(), ExpiresAt: req.Deadline, UserAuthority: req.Authority}
	if grant.SkillAuthorities != nil {
		request.SkillAuthorities = trust.CloneSkillAuthorities(req.Authority.SkillAuthorities)
		for skill := range request.SkillAuthorities {
			if !slices.Contains(request.Skills, skill) {
				delete(request.SkillAuthorities, skill)
			}
		}
	}
	root := parent.RootTaskID
	if root == "" {
		root = parent.ID
	}
	if req.AdmissionID != "" || r.p.cfg.ChildAuthority != nil {
		if req.AdmissionID == "" || r.p.cfg.ChildAuthority == nil {
			return agentrun.AgentTask{}, ErrDenied
		}
		preview := agentrun.AgentTask{ID: req.TaskID, TenantID: parent.TenantID, UserID: parent.UserID, ParentTaskID: parent.ID, RootTaskID: root, BudgetTaskID: root, DelegationDepth: parent.DelegationDepth + 1, Plan: plan, ExpiresAt: req.Deadline}
		candidate := agentdelegation.Grant{GrantID: request.GrantID, CommonAdmissionID: req.AdmissionID, ParentGrantID: grant.GrantID, ParentActor: &claims.Actor, UserID: parent.UserID, Tenant: r.tenant, TargetAgentID: req.AgentID, AgentVersion: req.AgentVersion, InstallationID: req.InstallationID, TaskID: req.TaskID, Purpose: grant.Purpose, ExpiresAt: req.Deadline, Skills: request.Skills, SkillScopes: request.SkillScopes}
		if err := r.p.cfg.ChildAuthority.CheckChildTask(ctx, preview, candidate); err != nil {
			return agentrun.AgentTask{}, err
		}
	}
	if _, err := r.Delegation.CreateChildGrant(request, req.ParentCredential, agentdelegation.VerifyRequest{Audience: r.p.cfg.Audience, Sender: r.p.cfg.Workload}); err != nil {
		return agentrun.AgentTask{}, err
	}
	if err := r.p.cfg.Budget.OpenChildTask(agentbudget.TaskSpec{ID: req.TaskID, TenantID: parent.TenantID, UserID: parent.UserID, Limit: req.Limit}, parent.ID); err != nil {
		_ = r.Delegation.RevokeGrant(GrantID(req.TaskID), "child budget admission failed")
		return agentrun.AgentTask{}, err
	}
	created, err := r.Runtime.CreateTask(ctx, agentrun.CreateRequest{ID: req.TaskID, TenantID: parent.TenantID, UserID: parent.UserID, Goal: req.Goal, Constraints: req.Constraints, Plan: plan, Now: r.p.cfg.Clock().UTC(), ExpiresAt: req.Deadline, ParentTaskID: parent.ID, RootTaskID: root, BudgetTaskID: root, DelegationDepth: parent.DelegationDepth + 1})
	if err != nil {
		_ = r.Delegation.RevokeGrant(GrantID(req.TaskID), "child task persistence failed")
		return agentrun.AgentTask{}, err
	}
	return r.Runtime.ConfirmPlan(ctx, created.ID, created.UserID, created.Version, r.p.cfg.Clock().UTC())
}

func (r *Runner) validateDelegatedTask(ctx context.Context, task agentrun.AgentTask, grant agentdelegation.Grant) error {
	if task.ParentTaskID == "" {
		if grant.ParentGrantID != "" || grant.ParentActor != nil {
			return ErrDenied
		}
		return nil
	}
	if task.DelegationDepth == 0 || task.DelegationDepth > agentaudit.MaxSubAgentDepth || task.RootTaskID == "" || task.BudgetTaskID != task.RootTaskID || grant.ParentGrantID != GrantID(task.ParentTaskID) || grant.ParentActor == nil || grant.ParentActor.RunID != task.ParentTaskID {
		return ErrDenied
	}
	if err := r.checkDelegatedCurrent(ctx, task, grant); err != nil {
		return err
	}
	_, err := r.recheckTaskLineage(ctx, task)
	return err
}

func (r *Runner) checkDelegatedCurrent(ctx context.Context, task agentrun.AgentTask, grant agentdelegation.Grant) error {
	if grant.CommonAdmissionID != "" || r.p.cfg.ChildAuthority != nil {
		if grant.CommonAdmissionID == "" || r.p.cfg.ChildAuthority == nil {
			return fmt.Errorf("%w: child admission authority unavailable", ErrDenied)
		}
		if err := r.p.cfg.ChildAuthority.CheckChildTask(ctx, task, grant); err != nil {
			return err
		}
	}
	return nil
}

// recheckTaskLineage reloads every ancestor before a step or wake. Parent
// cancellation is durable on the child; parent pauses require an explicit
// user resume after the parent becomes runnable again.
func (r *Runner) recheckTaskLineage(ctx context.Context, task agentrun.AgentTask) (agentrun.AgentTask, error) {
	if task.ParentTaskID == "" {
		return task, nil
	}
	child := task
	seen := map[string]bool{task.ID: true}
	for depth := 0; child.ParentTaskID != ""; depth++ {
		if depth >= int(agentaudit.MaxSubAgentDepth) || seen[child.ParentTaskID] {
			return task, ErrDenied
		}
		seen[child.ParentTaskID] = true
		parent, err := r.Runtime.GetTask(ctx, child.ParentTaskID)
		if err != nil {
			return task, fmt.Errorf("%w: missing parent", ErrDenied)
		}
		root := parent.RootTaskID
		if root == "" {
			root = parent.ID
		}
		grant, err := r.grants.Get(GrantID(child.ID))
		if err != nil || grant.ParentGrantID != GrantID(parent.ID) || parent.TenantID != child.TenantID || parent.UserID != child.UserID || child.RootTaskID != root || child.BudgetTaskID != root || child.DelegationDepth != parent.DelegationDepth+1 || child.ExpiresAt.After(parent.ExpiresAt) {
			return task, ErrDenied
		}
		if err := r.checkDelegatedCurrent(ctx, child, grant); err != nil {
			return task, err
		}
		stop := parent.State == agentrun.StateCancelled || parent.State == agentrun.StateFailed || parent.State == agentrun.StateExpired || !r.p.cfg.Clock().Before(parent.ExpiresAt)
		pause := parent.State == agentrun.StatePaused || parent.State == agentrun.StateWaiting || parent.State == agentrun.StateAwaitingApproval || parent.State == agentrun.StateAwaitingPlanConfirmation
		if stop || pause {
			if task.State != agentrun.StateCancelled && task.State != agentrun.StateFailed && task.State != agentrun.StateCompleted && task.State != agentrun.StateExpired {
				if stop {
					task, err = r.Runtime.Cancel(ctx, task.ID, task.Version, r.p.cfg.Clock())
				} else {
					task, err = r.Runtime.Pause(ctx, task.ID, task.Version, r.p.cfg.Clock())
				}
				if err != nil {
					return task, err
				}
			}
			return task, fmt.Errorf("%w: %w", ErrDenied, ErrParentStopped)
		}
		child = parent
	}
	return task, nil
}
