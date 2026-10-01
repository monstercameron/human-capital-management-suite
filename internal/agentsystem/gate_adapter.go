package agentsystem

import (
	"context"
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/agentdelegation"
	"github.com/monstercameron/human-capital-management-suite/internal/agentgate"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
)

// CallAuthorizer is the policy boundary used for each individual agent skill
// call. agentgate.Gate implements it directly.
type CallAuthorizer interface {
	Authorize(context.Context, agentgate.CallRequest) (agentgate.CallDecision, error)
}

// CallScopeRequest contains verified runtime facts needed to resolve current
// user context and the exact record/field scope for one call. Implementations
// must consult the current directory, organization and owner data on every
// call; cached task-start authority is not sufficient.
type CallScopeRequest struct {
	Task     agentrun.AgentTask
	Step     agentrun.PlanStep
	Skill    agentskills.SkillRecord
	Claims   agentdelegation.Claims
	Prepared Prepared
}

// ResolvedCallScope contains the current user view and exact records/fields
// that the owner says this invocation will read or write. Empty or uncertain
// scopes are refused instead of treated as wildcards.
type ResolvedCallScope struct {
	User     agentgate.UserContext
	Subjects []agentgate.Subject
	Fields   []authz.FieldID
}

// CallScopeResolver resolves authoritative, current context for a single
// skill call. It must derive subjects and fields from the owning capability
// declaration and the prepared call, never from model-provided prose.
type CallScopeResolver interface {
	ResolveCallScope(context.Context, CallScopeRequest) (ResolvedCallScope, error)
}

// GateAdapter binds the current call to verified delegation claims before
// asking the per-call policy gate. It owns no policy or cached authority.
type GateAdapter struct {
	authorizer CallAuthorizer
	resolver   CallScopeResolver
}

// NewGateAdapter creates the production adapter. Both ports are required so
// missing production policy wiring cannot silently allow an agent call.
func NewGateAdapter(authorizer CallAuthorizer, resolver CallScopeResolver) (*GateAdapter, error) {
	if authorizer == nil || resolver == nil {
		return nil, fmt.Errorf("%w: agent call authorizer and current scope resolver are required", ErrNotConfigured)
	}
	return &GateAdapter{authorizer: authorizer, resolver: resolver}, nil
}

// Authorize resolves fresh owner-authoritative scope and constructs the gate
// request from verified claims plus the current task and step. The caller must
// invoke it immediately before model generation or owner execution.
func (a *GateAdapter) Authorize(ctx context.Context, task agentrun.AgentTask, step agentrun.PlanStep, skill agentskills.SkillRecord, claims agentdelegation.Claims, prepared Prepared) (agentgate.CallDecision, error) {
	if a == nil || a.authorizer == nil || a.resolver == nil {
		return agentgate.CallDecision{}, fmt.Errorf("%w: agent call gate adapter is not configured", ErrNotConfigured)
	}
	if err := validateGateBinding(task, step, skill, claims, prepared.Purpose); err != nil {
		return agentgate.CallDecision{}, err
	}
	scope, err := a.resolver.ResolveCallScope(ctx, CallScopeRequest{
		Task: task, Step: step, Skill: skill, Claims: claims, Prepared: prepared,
	})
	if err != nil {
		return agentgate.CallDecision{}, fmt.Errorf("%w: current call scope could not be resolved: %v", agentgate.ErrDenied, err)
	}
	if err := validateResolvedCallScope(scope, claims); err != nil {
		return agentgate.CallDecision{}, err
	}
	request := agentgate.CallRequest{
		User: scope.User,
		Actor: agentgate.AgentActor{
			AgentVersion: claims.Actor.AgentVersion, InstallationID: claims.Actor.InstallationID,
			RunID: claims.Actor.RunID, StepID: claims.Actor.StepID,
		},
		Skill:    agentskills.SkillPin{ID: skill.Definition.ID, Version: skill.Definition.Version, Digest: skill.Digest},
		Purpose:  prepared.Purpose,
		Subjects: scope.Subjects,
		Fields:   scope.Fields,
	}
	return a.authorizer.Authorize(ctx, request)
}

func validateGateBinding(task agentrun.AgentTask, step agentrun.PlanStep, skill agentskills.SkillRecord, claims agentdelegation.Claims, purpose string) error {
	if strings.TrimSpace(task.ID) == "" || strings.TrimSpace(task.UserID) == "" || strings.TrimSpace(task.TenantID) == "" ||
		strings.TrimSpace(step.ID) == "" || strings.TrimSpace(skill.Definition.ID) == "" || skill.Definition.Version == 0 || strings.TrimSpace(skill.Digest) == "" ||
		strings.TrimSpace(purpose) == "" {
		return &agentgate.DeniedError{Code: agentgate.DenyInvalid, Detail: "verified task, step, skill and purpose are required"}
	}
	if step.SkillID != skill.Definition.ID || step.SkillVersion != skill.Definition.Version {
		return &agentgate.DeniedError{Code: agentgate.DenySkillNotFound, Skill: skill.Definition.Key(), Detail: "step does not match the resolved skill"}
	}
	if claims.Subject != task.UserID || claims.Tenant != task.TenantID || claims.GrantID != GrantID(task.ID) || claims.Purpose != purpose || claims.Skill != skill.Definition.ID {
		return &agentgate.DeniedError{Code: agentgate.DenyAgent, Skill: skill.Definition.Key(), Detail: "verified delegation does not match this call"}
	}
	actor := claims.Actor
	if actor.AgentVersion == "" || actor.InstallationID == "" || actor.RunID != task.ID || actor.StepID != step.ID {
		return &agentgate.DeniedError{Code: agentgate.DenyAgent, Skill: skill.Definition.Key(), Detail: "verified actor chain does not match this step"}
	}
	return nil
}

func validateResolvedCallScope(scope ResolvedCallScope, claims agentdelegation.Claims) error {
	if scope.User.Principal == nil || scope.User.Principal.Subject() != claims.Subject || scope.User.Principal.Tenant().String() != claims.Tenant ||
		strings.TrimSpace(scope.User.Population) == "" || len(scope.User.Roles) == 0 || len(scope.User.OrganizationScopes) == 0 ||
		len(scope.Subjects) == 0 || len(scope.Fields) == 0 {
		return &agentgate.DeniedError{Code: agentgate.DenyAgent, Detail: "current user, role, organization and exact record/field scope are required"}
	}
	for _, subject := range scope.Subjects {
		if subject.Ref.Validate() != nil || subject.Ref.Tenant.String() != claims.Tenant || subject.Organization.IsZero() || subject.Organization.Tenant.String() != claims.Tenant {
			return &agentgate.DeniedError{Code: agentgate.DenySubject, Detail: "current call contains an invalid or cross-tenant subject"}
		}
	}
	for _, field := range scope.Fields {
		if strings.TrimSpace(string(field)) == "" || string(field) == agentgate.AnyScope {
			return &agentgate.DeniedError{Code: agentgate.DenyField, Field: field, Detail: "current call contains an invalid field scope"}
		}
	}
	return nil
}
