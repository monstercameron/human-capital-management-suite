package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentegress"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/memory"
	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	trustdlp "github.com/monstercameron/human-capital-management-suite/internal/trust/dlp"
)

var (
	errAgentOwnerSkill   = errors.New("application: the agent owner does not execute this skill")
	errAgentOwnerVerify  = errors.New("application: the agent step result failed owner verification")
	errAgentOwnerClaims  = errors.New("application: the delegated claims do not name a tenant, subject and purpose")
	errAgentOwnerScope   = errors.New("application: the delegated claims do not cover the capability")
	errAgentOwnerRetain  = errors.New("application: the quarantined extraction is malformed")
	errAgentOwnerNoModel = ErrAgentModelUnavailable
)

// agentToolOwner is the production agentsystem.ToolOwner. Every call it makes
// is built from the VERIFIED delegated claims of the step's credential:
// tenant, subject and purpose come from Claims, and the capability
// authorization is ALLOW only when the claim's scope names the capability.
// Nothing here reads the task prompt for identity, scope or skill choice; the
// prompt only ever appears as the "goal" field a model step declares.
type agentToolOwner struct {
	gateway *capability.Gateway
	model   agentModel

	mu     sync.Mutex
	memory *AgentMemoryOperations
}

var _ agentsystem.ToolOwner = (*agentToolOwner)(nil)

func newAgentToolOwner(gateway *capability.Gateway, model agentModel) *agentToolOwner {
	return &agentToolOwner{gateway: gateway, model: model}
}

// BindAgentRuntimeMemory connects task retention to the same governed stores
// exposed by owner controls. It must complete before task workers start.
func BindAgentRuntimeMemory(runtime *agentRuntime, service *AgentMemoryOperations) error {
	if runtime == nil || runtime.Platform == nil || runtime.Owner == nil || service == nil {
		return memory.ErrInvalid
	}
	runtime.Owner.mu.Lock()
	defer runtime.Owner.mu.Unlock()
	if runtime.Owner.memory != nil {
		return memory.ErrInvalid
	}
	if err := service.BindTaskAuthority(runtime.Platform); err != nil {
		return err
	}
	runtime.Owner.memory = service
	return nil
}

// Prepare declares what a step wants to send. A model step declares exactly
// one outbound field, the goal, classified PUBLIC; a read step declares none.
func (o *agentToolOwner) Prepare(_ context.Context, req agentsystem.PrepareRequest) (agentsystem.Prepared, error) {
	prepared := agentsystem.Prepared{Purpose: agentPurpose}
	switch req.Step.Type {
	case agentrun.StepAnalyze, agentrun.StepDraft:
		if !o.model.Available() {
			return agentsystem.Prepared{}, errAgentOwnerNoModel
		}
		prepared.Egress = &agentsystem.EgressCall{
			Profile: agentegress.Profile{
				ID: agentModelDestination, Kind: agentegress.TargetModel,
				AllowedRegions: []string{agentModelRegion}, AllowedClasses: []trustdlp.DataClass{trustdlp.ClassPublic},
				Retention: agentegress.RetentionPolicy{Mode: agentegress.RetentionNone},
			},
			Region: agentModelRegion, DeclaredFields: []string{"goal"},
			Fields: []agentegress.Field{{
				Name: "goal", Value: req.Task.Goal, Class: trustdlp.ClassPublic,
				Taint: []string{string(agentsecurity.TaintHuman)}, Provenance: []string{"task:" + req.Task.ID + ":goal"},
			}},
			Task: agentegress.TaskPolicy{
				AllowedRegions: []string{agentModelRegion}, AllowedResultClasses: []trustdlp.DataClass{trustdlp.ClassPublic},
				ResultRetention: time.Hour,
			},
		}
	case agentrun.StepRead, agentrun.StepVerify:
		// No outbound payload: the read goes to the governed capability
		// gateway in process.
	default:
		return agentsystem.Prepared{}, fmt.Errorf("%w: step type %s", errAgentOwnerSkill, req.Step.Type)
	}
	return prepared, nil
}

// Invoke performs the governed read through the capability gateway.
func (o *agentToolOwner) Invoke(ctx context.Context, call agentsystem.Invocation) (agentsystem.Result, error) {
	if call.Skill.Definition.ID != agentReadSkillID {
		return agentsystem.Result{}, fmt.Errorf("%w: %s", errAgentOwnerSkill, call.Skill.Definition.ID)
	}
	claims := call.Claims
	tenant := values.TenantId(claims.Tenant)
	if tenant.Validate() != nil || strings.TrimSpace(claims.Subject) == "" || strings.TrimSpace(claims.Purpose) == "" {
		return agentsystem.Result{}, errAgentOwnerClaims
	}
	var response any
	for _, op := range call.Skill.ResolvedOperations {
		if !op.HasCapability {
			continue
		}
		definition := op.Capability.Definition
		auth := capability.Authorization{
			Decision: capability.Deny, Reason: "the delegated claims do not cover " + definition.ID,
			SubjectRef: claims.Subject, Tenant: claims.Tenant,
		}
		if slices.Contains(claims.Scope, definition.ID) {
			auth = capability.Authorization{
				Decision: capability.Allow, Scopes: []string{definition.AuthZScopeRef},
				SubjectRef: claims.Subject, Tenant: claims.Tenant,
			}
		}
		result, err := o.gateway.Invoke(ctx, capability.InvokeRequest{
			Capability:    capability.Key{ID: definition.ID, Version: definition.Version},
			Payload:       agentWorkerStateCall{Tenant: tenant, Subject: claims.Subject, Purpose: claims.Purpose},
			Authorization: auth,
		})
		if err != nil {
			var gatewayErr *capability.GatewayError
			if errors.As(err, &gatewayErr) && gatewayErr.Code == capability.CodeUnauthorized {
				return agentsystem.Result{}, fmt.Errorf("%w: %w", errAgentOwnerScope, err)
			}
			return agentsystem.Result{}, err
		}
		response = result.Response
	}
	state, ok := response.(agentWorkerState)
	if !ok {
		return agentsystem.Result{}, fmt.Errorf("%w: capability answered %T", errAgentOwnerSkill, response)
	}
	encoded, err := json.Marshal(state)
	if err != nil {
		return agentsystem.Result{}, err
	}
	sum := sha256.Sum256(encoded)
	digest := hex.EncodeToString(sum[:])
	return agentsystem.Result{Ref: "worker-state:" + digest[:16], Digest: "sha256:" + digest}, nil
}

// Retain delegates every raw and search copy to current governed memory.
func (o *agentToolOwner) Retain(ctx context.Context, task agentrun.AgentTask, step agentrun.PlanStep, ref string, extraction agentsecurity.QuarantineExtraction) error {
	if strings.TrimSpace(ref) == "" || strings.TrimSpace(task.ID) == "" || strings.TrimSpace(task.TenantID) == "" {
		return errAgentOwnerRetain
	}
	o.mu.Lock()
	service := o.memory
	o.mu.Unlock()
	if service == nil {
		return memory.ErrDenied
	}
	return service.RetainTaskExtraction(ctx, task, step, ref, extraction)
}

// Retained rechecks task authority, source, audience, retention and legal hold
// policy through the owner before returning bytes from the raw copy store.
func (o *agentToolOwner) Retained(ctx context.Context, task agentrun.AgentTask, step agentrun.PlanStep, ref string) (agentsecurity.QuarantineExtraction, error) {
	o.mu.Lock()
	service := o.memory
	o.mu.Unlock()
	if service == nil {
		return agentsecurity.QuarantineExtraction{}, memory.ErrDenied
	}
	return service.ReadTaskExtraction(ctx, task, step, ref)
}

func (o *agentToolOwner) ReadTaskSource(ctx context.Context, task agentrun.AgentTask, sourceID string) (agentrun.OwnerRead, error) {
	for _, entry := range task.Ledger.Entries {
		if entry.SourceID != sourceID && !slices.Contains(entry.SourceIDs, sourceID) {
			continue
		}
		for _, step := range task.Plan.Steps {
			if step.ResultRef != entry.Ref {
				continue
			}
			extraction, err := o.Retained(ctx, task, step, entry.Ref)
			if errors.Is(err, memory.ErrStale) || errors.Is(err, memory.ErrExpired) {
				return agentrun.OwnerRead{SourceID: sourceID, Revoked: true}, nil
			}
			if err != nil {
				return agentrun.OwnerRead{}, err
			}
			if extraction.SourceID != sourceID {
				return agentrun.OwnerRead{}, memory.ErrDenied
			}
			return agentrun.OwnerRead{SourceID: sourceID, Ref: entry.Ref, ValueRef: entry.Ref, Taint: []string{string(agentsecurity.TaintExternal)}}, nil
		}
	}
	return agentrun.OwnerRead{}, memory.ErrNotFound
}

// Verify completes a VERIFY step. The self-service plan has no owner read to
// compare against, so the check is structural: the result must carry a
// durable reference and a digest, and must belong to the step being verified.
func (o *agentToolOwner) Verify(_ context.Context, task agentrun.AgentTask, step agentrun.PlanStep, result agentrun.StepResult) error {
	if strings.TrimSpace(result.Ref) == "" || !strings.HasPrefix(result.Digest, "sha256:") {
		return fmt.Errorf("%w: result has no reference or digest", errAgentOwnerVerify)
	}
	if task.CurrentStep >= len(task.Plan.Steps) || task.Plan.Steps[task.CurrentStep].ID != step.ID {
		return fmt.Errorf("%w: step %s is not the task's current step", errAgentOwnerVerify, step.ID)
	}
	return nil
}
