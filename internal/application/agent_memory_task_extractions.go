package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/memory"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func taskExtractionID(tenant, taskID, ref string) string {
	sum := sha256.Sum256([]byte(tenant + "\x00" + taskID + "\x00" + ref))
	return "task-extraction:" + hex.EncodeToString(sum[:])
}

func (s *AgentMemoryOperations) taskActor(ctx context.Context, task agentrun.AgentTask, step agentrun.PlanStep) (memory.Actor, agentsystem.Invocation, error) {
	if s == nil || s.taskAuthority == nil || task.ID == "" || task.TenantID == "" || step.ID == "" {
		return memory.Actor{}, agentsystem.Invocation{}, memory.ErrDenied
	}
	call, err := s.taskAuthority.ResolveTaskMemoryInvocation(ctx, task, step)
	if err != nil {
		return memory.Actor{}, call, errors.Join(memory.ErrDenied, err)
	}
	if call.Credential == "" || call.Task.ID != task.ID || call.Task.TenantID != task.TenantID || call.Step.ID != step.ID || call.Claims.Subject != call.Task.UserID || call.Claims.Tenant != call.Task.TenantID || call.Claims.Purpose == "" || call.Claims.Actor.RunID != call.Task.ID || call.Claims.Actor.StepID != call.Step.ID {
		return memory.Actor{}, call, memory.ErrDenied
	}
	active, err := s.authority.workerActive(ctx, values.TenantId(call.Claims.Tenant), call.Claims.Subject)
	if err != nil {
		return memory.Actor{}, call, err
	}
	if !active {
		return memory.Actor{}, call, memory.ErrDenied
	}
	roles, err := s.authority.roles.Load(ctx, values.TenantId(call.Claims.Tenant), agentOrgScope(values.TenantId(call.Claims.Tenant)))
	if err != nil {
		return memory.Actor{}, call, err
	}
	if !hasActiveRole(roles, call.Claims.Subject) {
		return memory.Actor{}, call, memory.ErrDenied
	}
	return memory.Actor{TenantID: call.Claims.Tenant, PrincipalID: call.Claims.Subject}, call, nil
}

// RetainTaskExtraction uses source-owner registration and records-owner policy.
// It never creates a source, grant, retention policy or hold decision.
func (s *AgentMemoryOperations) RetainTaskExtraction(ctx context.Context, task agentrun.AgentTask, step agentrun.PlanStep, ref string, extraction agentsecurity.QuarantineExtraction) error {
	if strings.TrimSpace(ref) == "" || extraction.Source == "" || extraction.SourceID == "" || extraction.SourceDigest == "" || extraction.SchemaID == "" || extraction.SchemaVersion == "" {
		return memory.ErrInvalid
	}
	actor, call, err := s.taskActor(ctx, task, step)
	if err != nil {
		return err
	}
	purpose := call.Claims.Purpose
	policy, err := s.store.ResolveMemoryPolicy(ctx, actor.TenantID, actor.PrincipalID, purpose)
	if err != nil {
		return err
	}
	pin := memory.SourcePin{TenantID: actor.TenantID, Owner: string(extraction.Source), ID: extraction.SourceID, Purpose: purpose}
	source, err := s.store.CheckMemorySource(ctx, pin, purpose)
	if err != nil {
		return err
	}
	if !source.Current || source.Version == "" || source.Digest != extraction.SourceDigest || source.DataClass == "" {
		return memory.ErrStale
	}
	encoded, err := json.Marshal(extraction)
	if err != nil {
		return err
	}
	item := memory.Item{ID: taskExtractionID(actor.TenantID, call.Task.ID, ref), TenantID: actor.TenantID, OwnerID: actor.PrincipalID, Kind: memory.KindToolResult, SourceOwner: pin.Owner, SourceID: pin.ID, SourceVersion: source.Version, SourceDigest: source.Digest, Audience: source.Audience, Purpose: purpose, DataClass: source.DataClass, RetentionPolicyID: policy.RetentionPolicyID, RetentionVersion: policy.RetentionVersion, CreatedAt: call.Task.CreatedAt, TTL: policy.MaxTTL, Invalidators: source.Invalidators, Payload: encoded}
	return s.manager.Put(ctx, actor, item)
}

// ReadTaskExtraction refreshes task authority and every governed copy pin.
func (s *AgentMemoryOperations) ReadTaskExtraction(ctx context.Context, task agentrun.AgentTask, step agentrun.PlanStep, ref string) (agentsecurity.QuarantineExtraction, error) {
	if strings.TrimSpace(ref) == "" {
		return agentsecurity.QuarantineExtraction{}, memory.ErrInvalid
	}
	actor, call, err := s.taskActor(ctx, task, step)
	if err != nil {
		return agentsecurity.QuarantineExtraction{}, err
	}
	item, err := s.manager.Read(ctx, actor, "raw", taskExtractionID(actor.TenantID, call.Task.ID, ref))
	if err != nil {
		return agentsecurity.QuarantineExtraction{}, err
	}
	if item.OwnerID != actor.PrincipalID || item.Purpose != call.Claims.Purpose || item.Kind != memory.KindToolResult {
		return agentsecurity.QuarantineExtraction{}, memory.ErrDenied
	}
	var extraction agentsecurity.QuarantineExtraction
	if err := json.Unmarshal(item.Payload, &extraction); err != nil {
		return extraction, err
	}
	if extraction.SourceID != item.SourceID || string(extraction.Source) != item.SourceOwner || extraction.SourceDigest != item.SourceDigest {
		return agentsecurity.QuarantineExtraction{}, memory.ErrStale
	}
	return extraction, nil
}
