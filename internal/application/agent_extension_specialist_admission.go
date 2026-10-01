package application

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentbudget"
	"github.com/monstercameron/human-capital-management-suite/internal/agentdelegation"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmanifest"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var (
	ErrSpecialistInvalid = errors.New("application: invalid specialist invocation")
	ErrSpecialistDenied  = errors.New("application: specialist invocation denied")
)

// SpecialistInvocation is the typed Agents page request. Target is the same
// common Request admitted by CommonAgentRuntime; it carries references and
// digests only. The page never supplies authority or a model/provider choice.
type SpecialistInvocation struct {
	ParentAdmissionID string
	ParentTaskID      string
	ParentCredential  string
	TaskID            string
	Target            agentrun.Request
	Goal              string
	Constraints       []string
	Steps             []agentrun.PlanStep
	Limit             agentbudget.Limits
	Deadline          time.Time
}

// SpecialistDelegator is the narrow application seam over the durable core.
// agentsystem.Runner is the production implementation.
type SpecialistDelegator interface {
	Delegate(context.Context, agentsystem.DelegateRequest) (agentrun.AgentTask, error)
}

// SpecialistAdmissionConfig contains only server-composed authorities. In
// production UserAuthority is the current policy resolver used by the parent
// run; it must never be populated from page input.
type SpecialistAdmissionConfig struct {
	Runtime       *CommonAgentRuntime
	Authority     *CommonAgentAuthority
	UserAuthority agentdelegation.AuthorityResolver
	Delegator     SpecialistDelegator
	Now           func() time.Time
}

// SpecialistAdmission is the Agents page's bounded specialist invocation
// surface. It validates an accepted parent, verifies the current immutable
// target installation, resolves current user authority, then delegates to the
// core Runner. It does not discover personas or recruit another agent.
type SpecialistAdmission struct{ cfg SpecialistAdmissionConfig }

// specialistChildAuthority adapts the target installation check to the common
// admission interface. It reloads the accepted parent through CauseID so the
// child cannot rely on a stale in-process request.
type specialistChildAuthority struct {
	runtime *CommonAgentRuntime
	base    *CommonAgentAuthority
}

func (a specialistChildAuthority) VerifyAdmission(ctx context.Context, request agentrun.Request) (agentrun.AuthoritySnapshot, error) {
	if a.runtime == nil || a.base == nil || strings.TrimSpace(request.CauseID) == "" {
		return agentrun.AuthoritySnapshot{}, commonAgentRefusal("SPECIALIST_PARENT_MISSING")
	}
	parent, err := a.runtime.GetAdmission(ctx, request.Source.TenantID, request.CauseID)
	if err != nil || parent.Decision != agentrun.DecisionAccepted {
		return agentrun.AuthoritySnapshot{}, commonAgentRefusal("SPECIALIST_PARENT_MISSING")
	}
	if err := a.runtime.Recheck(ctx, request.Source.TenantID, request.CauseID); err != nil {
		return agentrun.AuthoritySnapshot{}, commonAgentRefusal("SPECIALIST_PARENT_NOT_CURRENT")
	}
	return a.base.VerifySpecialist(ctx, parent.Request, request)
}

// NewAgentSpecialistChildAuthority returns the restart-safe authority used by
// a child common runtime. It reloads the parent admission through CauseID on
// every check instead of retaining a parent request in process memory.
func NewAgentSpecialistChildAuthority(runtime *CommonAgentRuntime, base *CommonAgentAuthority) agentrun.Authority {
	if runtime == nil || base == nil {
		return nil
	}
	return specialistChildAuthority{runtime: runtime, base: base}
}

func NewAgentSpecialistAdmission(cfg SpecialistAdmissionConfig) (*SpecialistAdmission, error) {
	if cfg.Runtime == nil || cfg.Authority == nil || cfg.UserAuthority == nil || cfg.Delegator == nil || cfg.Now == nil {
		return nil, agentrun.ErrAuthorityMissing
	}
	return &SpecialistAdmission{cfg: cfg}, nil
}

// Invoke admits and creates one bounded specialist task. The target's
// authority snapshot is read from current authority_binding and its immutable
// agent manifest before the core re-exchanges the parent's step credential.
func (s *SpecialistAdmission) Invoke(ctx context.Context, req SpecialistInvocation) (agentrun.AgentTask, error) {
	if s == nil || ctx == nil {
		return agentrun.AgentTask{}, ErrSpecialistInvalid
	}
	if err := validateSpecialistShape(req); err != nil {
		return agentrun.AgentTask{}, err
	}
	if req.Target.Principal.DelegatedCredentialRef != req.TaskID {
		return agentrun.AgentTask{}, fmt.Errorf("%w: child credential reference must bind the child task", ErrSpecialistDenied)
	}
	if !req.Target.Deadline.Equal(req.Deadline) {
		return agentrun.AgentTask{}, fmt.Errorf("%w: target deadline is not pinned", ErrSpecialistDenied)
	}
	parent, err := s.cfg.Runtime.GetAdmission(ctx, req.Target.Source.TenantID, req.ParentAdmissionID)
	if err != nil {
		return agentrun.AgentTask{}, err
	}
	if parent.Decision != agentrun.DecisionAccepted || parent.Request.Principal.Mode != agentrun.ModeOnBehalfOf || parent.Request.Principal.InvokerID == "" {
		return agentrun.AgentTask{}, fmt.Errorf("%w: parent admission is not an accepted on-behalf-of run", ErrSpecialistDenied)
	}
	if req.ParentTaskID == "" || req.ParentCredential == "" || parent.Request.Source.TenantID != req.Target.Source.TenantID || parent.Request.Principal.InvokerID != req.Target.Principal.InvokerID || parent.Request.Principal.DelegatedCredentialRef != req.ParentTaskID || req.Target.Source.Key == parent.Request.Source.Key {
		return agentrun.AgentTask{}, fmt.Errorf("%w: parent and child identity mismatch", ErrSpecialistDenied)
	}
	if err := s.cfg.Runtime.Recheck(ctx, parent.Request.Source.TenantID, req.ParentAdmissionID); err != nil {
		return agentrun.AgentTask{}, fmt.Errorf("%w: parent authority is no longer current: %v", ErrSpecialistDenied, err)
	}
	if err := validateSpecialistNarrowing(parent.Request, req.Target, req.Deadline); err != nil {
		return agentrun.AgentTask{}, err
	}
	snapshot, err := s.cfg.Authority.VerifySpecialist(ctx, parent.Request, req.Target)
	if err != nil {
		return agentrun.AgentTask{}, err
	}
	if snapshot.Principal.Mode != agentrun.ModeOnBehalfOf || snapshot.Principal.InvokerID != parent.Request.Principal.InvokerID {
		return agentrun.AgentTask{}, fmt.Errorf("%w: target authority is not user delegated", ErrSpecialistDenied)
	}
	if err := s.cfg.Authority.ValidateSpecialistPlan(ctx, req.Target, req.Steps); err != nil {
		return agentrun.AgentTask{}, err
	}
	current, err := s.cfg.UserAuthority.Resolve(parent.Request.Principal.InvokerID, values.TenantId(parent.Request.Source.TenantID), req.Target.Purpose, s.cfg.Now().UTC())
	if err != nil {
		return agentrun.AgentTask{}, err
	}
	if !current.Active || current.UserID != parent.Request.Principal.InvokerID || current.Authority.Tenant.String() != parent.Request.Source.TenantID {
		return agentrun.AgentTask{}, fmt.Errorf("%w: current user authority unavailable", ErrSpecialistDenied)
	}
	if req.Target.CauseID != req.ParentAdmissionID {
		return agentrun.AgentTask{}, fmt.Errorf("%w: child cause does not bind parent admission", ErrSpecialistDenied)
	}
	childRuntime, err := NewCommonAgentRuntime(CommonAgentRuntimeConfig{Stores: s.cfg.Runtime.cfg.Stores, Authority: specialistChildAuthority{runtime: s.cfg.Runtime, base: s.cfg.Authority}, Now: s.cfg.Now})
	if err != nil {
		return agentrun.AgentTask{}, err
	}
	accepted, _, err := childRuntime.Admit(ctx, req.Target)
	if err != nil {
		return agentrun.AgentTask{}, err
	}
	if accepted.Decision != agentrun.DecisionAccepted || accepted.Request.Principal.DelegatedCredentialRef != req.TaskID {
		return agentrun.AgentTask{}, fmt.Errorf("%w: child common admission was not accepted", ErrSpecialistDenied)
	}
	return s.cfg.Delegator.Delegate(ctx, agentsystem.DelegateRequest{
		AdmissionID:  accepted.ID,
		ParentTaskID: req.ParentTaskID, ParentCredential: req.ParentCredential,
		TaskID: req.TaskID, AgentID: req.Target.Agent.AgentID, AgentVersion: req.Target.Agent.AgentID + "@" + req.Target.Agent.Version,
		InstallationID: req.Target.InstallationID, Goal: req.Goal, Constraints: append([]string(nil), req.Constraints...), Steps: append([]agentrun.PlanStep(nil), req.Steps...),
		Authority: current.Authority, Limit: req.Limit, Deadline: req.Deadline,
	})
}

// AdmitSpecialist is an alias suitable for application composition roots.
func (s *SpecialistAdmission) AdmitSpecialist(ctx context.Context, req SpecialistInvocation) (agentrun.AgentTask, error) {
	return s.Invoke(ctx, req)
}

func validateSpecialistShape(req SpecialistInvocation) error {
	if strings.TrimSpace(req.ParentAdmissionID) == "" || strings.TrimSpace(req.ParentTaskID) == "" || strings.TrimSpace(req.ParentCredential) == "" || strings.TrimSpace(req.TaskID) == "" || strings.TrimSpace(req.Target.Source.TenantID) == "" || strings.TrimSpace(req.Target.Agent.AgentID) == "" || strings.TrimSpace(req.Target.Agent.Version) == "" || strings.TrimSpace(req.Target.Agent.Digest) == "" || strings.TrimSpace(req.Target.InstallationID) == "" || strings.TrimSpace(req.Target.Purpose) == "" || strings.TrimSpace(req.Target.Principal.AgentPrincipalID) == "" || strings.TrimSpace(req.Target.Principal.InvokerID) == "" || req.Target.Principal.Mode != agentrun.ModeOnBehalfOf || req.Target.Principal.SponsorID != "" || req.Target.Principal.DelegatedCredentialRef == "" || req.Deadline.IsZero() {
		return ErrSpecialistInvalid
	}
	if req.Target.Persona != nil {
		return fmt.Errorf("%w: persona discovery and recruitment are outside specialist delegation", ErrSpecialistDenied)
	}
	if _, err := agentrun.NewPlan(req.Steps); err != nil {
		return fmt.Errorf("%w: %v", ErrSpecialistInvalid, err)
	}
	return nil
}

func validateSpecialistNarrowing(parent, child agentrun.Request, deadline time.Time) error {
	if child.Source.TenantID != parent.Source.TenantID || child.LegalEntity != parent.LegalEntity || child.Purpose != parent.Purpose || child.Audience != parent.Audience || child.Context != parent.Context || child.Principal.InvokerID != parent.Principal.InvokerID || child.Principal.DelegatedCredentialRef == parent.Principal.DelegatedCredentialRef || !deadline.After(time.Time{}) || deadline.After(parent.Deadline) {
		return fmt.Errorf("%w: child authority expands or escapes parent context", ErrSpecialistDenied)
	}
	if child.Budget.MaxCostMicros > parent.Budget.MaxCostMicros || child.Budget.MaxInputTokens > parent.Budget.MaxInputTokens || child.Budget.MaxOutputTokens > parent.Budget.MaxOutputTokens {
		return fmt.Errorf("%w: child budget exceeds parent", ErrSpecialistDenied)
	}
	return nil
}

// VerifySpecialist verifies a target installation with the same immutable
// manifest and trust tables used by common admission. The parent is supplied
// only to bind the user, tenant and context; all target authority comes from
// current rows and the manifest store.
func (a *CommonAgentAuthority) VerifySpecialist(ctx context.Context, parent, target agentrun.Request) (agentrun.AuthoritySnapshot, error) {
	if a == nil || ctx == nil || parent.Source.TenantID != target.Source.TenantID || target.Principal.Mode != agentrun.ModeOnBehalfOf || target.Principal.InvokerID != parent.Principal.InvokerID {
		return agentrun.AuthoritySnapshot{}, commonAgentRefusal("SPECIALIST_DENIED")
	}
	if target.Persona != nil {
		return agentrun.AuthoritySnapshot{}, commonAgentRefusal("SPECIALIST_DISCOVERY_DENIED")
	}
	return a.VerifyBindings(ctx, target)
}

// ValidateSpecialistPlan binds caller supplied steps to the target manifest's
// immutable tool/source ceilings before anything is persisted or delegated.
func (a *CommonAgentAuthority) ValidateSpecialistPlan(ctx context.Context, target agentrun.Request, steps []agentrun.PlanStep) error {
	if a == nil || ctx == nil {
		return ErrSpecialistDenied
	}
	tenant := values.TenantId(target.Source.TenantID)
	tenantID := a.cfg.TenantUUID(tenant)
	version, err := strconv.ParseUint(target.Agent.Version, 10, 64)
	if err != nil || tenantID == uuid.Nil {
		return fmt.Errorf("%w: target manifest unavailable", ErrSpecialistDenied)
	}
	manifest, err := a.cfg.Agents.ManifestVersion(ctx, tenantID, target.Agent.AgentID, version)
	if err != nil {
		return fmt.Errorf("%w: target manifest unavailable: %v", ErrSpecialistDenied, err)
	}
	tools := make(map[string]agentmanifest.Reference, len(manifest.ToolCeiling))
	for _, ref := range manifest.ToolCeiling {
		tools[ref.ID] = ref
	}
	sources := make(map[string]agentmanifest.Reference, len(manifest.SourceCeiling))
	for _, ref := range manifest.SourceCeiling {
		sources[ref.ID] = ref
	}
	for _, step := range steps {
		ref, ok := tools[step.SkillID]
		if !ok || uint64(step.SkillVersion) != ref.Version {
			return fmt.Errorf("%w: step %q is outside target tool ceiling", ErrSpecialistDenied, step.SkillID)
		}
		for _, input := range step.Inputs {
			if input.SourceID == "" {
				continue
			}
			if _, ok := sources[input.SourceID]; !ok || strings.TrimSpace(input.Ref) == "" {
				return fmt.Errorf("%w: step source %q is outside target source ceiling", ErrSpecialistDenied, input.SourceID)
			}
		}
	}
	return nil
}
