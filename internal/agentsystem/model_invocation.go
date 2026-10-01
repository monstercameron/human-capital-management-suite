package agentsystem

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"strconv"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/agentaudit"
	"github.com/monstercameron/human-capital-management-suite/internal/agentdelegation"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
)

type modelInvocationKey struct{}
type modelInvocationEvidence struct {
	platform *Platform
	mode     Mode
	digest   string
	request  agentmodel.Request
}

// ModelInvocation contains freshly checked owner authority for a model call.
// The private context receipt is minted only by an executable task checkpoint.
type ModelInvocation struct {
	Task  agentrun.AgentTask
	Step  agentrun.PlanStep
	Grant agentdelegation.Grant
	Skill agentskills.SkillRecord
	Actor agentaudit.ActorChain
}

func modelRequestDigest(req agentmodel.Request) string {
	encoded, _ := json.Marshal(req)
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

func bindModelInvocation(ctx context.Context, p *Platform, mode Mode, req agentmodel.Request) context.Context {
	return context.WithValue(ctx, modelInvocationKey{}, modelInvocationEvidence{platform: p, mode: mode, digest: modelRequestDigest(req), request: req})
}

// CurrentModelInvocation rechecks the private checkpoint receipt in context.
func (r *Runner) CurrentModelInvocation(ctx context.Context) (ModelInvocation, error) {
	if ctx == nil {
		return ModelInvocation{}, ErrDenied
	}
	evidence, ok := ctx.Value(modelInvocationKey{}).(modelInvocationEvidence)
	if !ok {
		return ModelInvocation{}, ErrDenied
	}
	return r.ResolveModelInvocation(ctx, evidence.request)
}

// ResolveModelInvocation refuses caller-invented actors, prompts and skill pins,
// then rechecks current grant authority before provider routing or egress.
func (r *Runner) ResolveModelInvocation(ctx context.Context, req agentmodel.Request) (ModelInvocation, error) {
	if r == nil || ctx == nil || req.TenantID != r.tenant.String() {
		return ModelInvocation{}, ErrDenied
	}
	evidence, ok := ctx.Value(modelInvocationKey{}).(modelInvocationEvidence)
	if !ok || evidence.platform != r.p || evidence.digest != modelRequestDigest(req) {
		return ModelInvocation{}, ErrDenied
	}
	invocation, err := r.resolveTaskInvocation(ctx, req.Actor.TaskID, req.Actor.StepID, req.Actor.UserID, req.Skill, evidence.mode, true)
	if err != nil {
		return ModelInvocation{}, err
	}
	if !reflect.DeepEqual(invocation.Actor, req.Actor) || !reflect.DeepEqual(req.DataClasses, invocation.Skill.Definition.DataClassesRead) || req.Purpose != invocation.Grant.Purpose || strings.TrimSpace(req.Prompt) == "" {
		return ModelInvocation{}, ErrDenied
	}
	return invocation, nil
}

// ResolveTaskInvocation is the source-owner boundary for an on-behalf-of API
// invocation made by an executing task. The application independently checks
// the request's authenticated invoker before using this current grant snapshot.
func (r *Runner) ResolveTaskInvocation(ctx context.Context, taskID, stepID, userID string, pin agentskills.SkillPin) (ModelInvocation, error) {
	return r.resolveTaskInvocation(ctx, taskID, stepID, userID, pin, ModeOnBehalfOf, false)
}

func (r *Runner) resolveTaskInvocation(ctx context.Context, taskID, stepID, userID string, pin agentskills.SkillPin, mode Mode, quarantine bool) (ModelInvocation, error) {
	if r == nil || ctx == nil {
		return ModelInvocation{}, ErrDenied
	}
	if gate := r.p.cfg.WakeGate; gate != nil && !gate(ctx, r.tenant.String()) {
		return ModelInvocation{}, errors.Join(ErrDenied, ErrTenantDisabled)
	}
	task, err := r.Runtime.GetTask(ctx, taskID)
	if err != nil {
		return ModelInvocation{}, err
	}
	if task.UserID != userID || task.TenantID != r.tenant.String() || !task.Plan.Confirmed || task.State != agentrun.StateRunning || task.CurrentStep >= len(task.Plan.Steps) || !task.ExpiresAt.After(r.p.cfg.Clock()) {
		return ModelInvocation{}, ErrDenied
	}
	step := task.Plan.Steps[task.CurrentStep]
	if step.State != agentrun.StepRunning || (stepID != step.ID && !(quarantine && stepID == step.ID+"/quarantine")) {
		return ModelInvocation{}, ErrDenied
	}
	exec := &executor{runner: r, mode: mode}
	skill, grant, err := exec.admit(ctx, task, step)
	if err != nil {
		return ModelInvocation{}, err
	}
	_, claims, err := exec.credential(task, step, skill, grant)
	if err != nil {
		return ModelInvocation{}, err
	}
	actor := agentaudit.ActorChain{UserID: claims.Subject, AgentVersion: grant.AgentVersion, InstallationID: grant.InstallationID, TaskID: task.ID, PlanRevision: strconv.FormatUint(task.Plan.Revision, 10), StepID: stepID, DelegationGrantID: grant.GrantID, SubAgentDepth: task.DelegationDepth}
	if grant.Tenant.String() != r.tenant.String() || pin != (agentskills.SkillPin{ID: skill.Definition.ID, Version: skill.Definition.Version, Digest: skill.Digest}) {
		return ModelInvocation{}, ErrDenied
	}
	return ModelInvocation{Task: task, Step: step, Grant: grant, Skill: skill, Actor: actor}, nil
}
