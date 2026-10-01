package agenteval

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentbudget"
	runtimeeval "github.com/monstercameron/human-capital-management-suite/internal/agenteval"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// ErrSyntheticProvisionUnavailable means no current, isolated synthetic task
// provision can be established from trusted authority.
var ErrSyntheticProvisionUnavailable = errors.New("agenteval: trusted synthetic task provision is unavailable")

const syntheticEvaluationPurpose = "agent-evaluation"

// SyntheticTaskProvision is resolved by the trusted provisioning authority.
// It describes the real evaluator-only composition and the server-resolved
// user identity/authority used to start tasks. Its marker and isolation IDs
// must come from durable provision records, never from a TaskCase or model.
type SyntheticTaskProvision struct {
	Marker    SyntheticTenantMarker
	Isolation SyntheticTaskIsolation
	Platform  *agentsystem.Platform
	Authority agentsystem.SyntheticTenantAuthority
	Usage     runtimeeval.SettledUsageReader
	Now       func() time.Time
	Start     agentsystem.StartRequest
}

// SyntheticTenantMarker is the explicit durable marker binding a provision
// to one tenant and the evaluation purpose.
type SyntheticTenantMarker struct {
	TenantID    string
	MarkerID    string
	Purpose     string
	ProvisionID string
	Status      string
	ExpiresAt   time.Time
}

// SyntheticTaskIsolation identifies each tenant-bound runtime component. The
// fixture owner profile also certifies that tool execution cannot reach live
// systems. All identities are verified by the provision authority.
type SyntheticTaskIsolation struct {
	TenantID         string
	GrantStoreID     string
	TaskStoreID      string
	BudgetLedgerID   string
	AuditStoreID     string
	ToolOwnerID      string
	ToolOwnerProfile string
}

// SyntheticTaskProvisionAuthority must load an active explicit synthetic
// tenant marker and the isolated runtime composition bound to it. A missing
// implementation is a hard denial; tenant-name prefixes are not authority.
type SyntheticTaskProvisionAuthority interface {
	ResolveSyntheticTaskProvision(context.Context, values.TenantId) (SyntheticTaskProvision, error)
}

// TrustedSyntheticTaskSource builds only canonical, fixture-safe task plans
// from authority-resolved synthetic tenant provisions.
type TrustedSyntheticTaskSource struct {
	authority SyntheticTaskProvisionAuthority
}

// NewTrustedSyntheticTaskSource requires the trusted provision authority.
func NewTrustedSyntheticTaskSource(authority SyntheticTaskProvisionAuthority) (*TrustedSyntheticTaskSource, error) {
	if authority == nil {
		return nil, ErrRuntimeExecutorConfig
	}
	return &TrustedSyntheticTaskSource{authority: authority}, nil
}

// Prepare resolves and validates the explicit tenant marker before returning
// a task request. The plan is selected from the canonical suite case, never
// supplied by the model or trusted from caller-provided counters.
func (s *TrustedSyntheticTaskSource) Prepare(ctx context.Context, taskCase TaskCase) (SyntheticTaskRequest, error) {
	if s == nil || s.authority == nil || ctx == nil || !canonicalTaskCase(taskCase) {
		return SyntheticTaskRequest{}, ErrRuntimeExecutorConfig
	}
	tenant := values.TenantId(taskCase.TenantID)
	if err := tenant.Validate(); err != nil {
		return SyntheticTaskRequest{}, ErrRuntimeExecutorConfig
	}
	provision, err := s.authority.ResolveSyntheticTaskProvision(ctx, tenant)
	if err != nil {
		return SyntheticTaskRequest{}, fmt.Errorf("%w: %v", ErrSyntheticProvisionUnavailable, err)
	}
	if provision.Now == nil {
		return SyntheticTaskRequest{}, fmt.Errorf("%w: trusted evaluation clock is unavailable", ErrSyntheticProvisionUnavailable)
	}
	now := provision.Now()
	if err := validateSyntheticProvision(tenant, provision, now); err != nil {
		return SyntheticTaskRequest{}, err
	}
	start := provision.Start
	start.TaskID = taskCase.ID
	start.Goal = taskCase.Goal
	start.Steps = canonicalFixturePlan(taskCase)
	if int64(len(start.Steps)) > start.Limit.Steps {
		return SyntheticTaskRequest{}, fmt.Errorf("%w: isolated task budget is smaller than the canonical fixture plan", ErrSyntheticProvisionUnavailable)
	}
	start.Constraints = append([]string(nil), start.Constraints...)
	start.UserAuthority = cloneAuthority(start.UserAuthority)
	return SyntheticTaskRequest{Platform: provision.Platform, Authority: provision.Authority, Start: start, Usage: provision.Usage, Now: provision.Now}, nil
}

func validateSyntheticProvision(tenant values.TenantId, provision SyntheticTaskProvision, now time.Time) error {
	marker := provision.Marker
	iso := provision.Isolation
	if marker.TenantID != tenant.String() || strings.TrimSpace(marker.MarkerID) == "" || marker.Purpose != syntheticEvaluationPurpose || strings.TrimSpace(marker.ProvisionID) == "" || marker.Status != "ACTIVE" || !marker.ExpiresAt.After(now) {
		return fmt.Errorf("%w: explicit active evaluation tenant marker is missing or mismatched", ErrSyntheticProvisionUnavailable)
	}
	if iso.TenantID != tenant.String() || strings.TrimSpace(iso.ToolOwnerProfile) != "fixture-only/v1" ||
		!distinctNonempty(iso.GrantStoreID, iso.TaskStoreID, iso.BudgetLedgerID, iso.AuditStoreID, iso.ToolOwnerID) {
		return fmt.Errorf("%w: isolated stores, budget, audit or fixture tool owner are incomplete", ErrSyntheticProvisionUnavailable)
	}
	start := provision.Start
	if provision.Platform == nil || provision.Authority == nil || provision.Usage == nil || provision.Now == nil ||
		strings.TrimSpace(start.UserID) == "" || strings.TrimSpace(start.AgentVersion) == "" || strings.TrimSpace(start.InstallationID) == "" ||
		strings.TrimSpace(start.Purpose) == "" || strings.TrimSpace(start.OrganizationScopeID) == "" || start.UserAuthority.Tenant != tenant ||
		start.UserAuthority.NotBefore.After(now) || !start.UserAuthority.ExpiresAt.After(now) || !validSyntheticBudget(start.Limit) ||
		start.Lifetime <= 0 || start.Lifetime > agentrun.MaxTaskLifetime {
		return fmt.Errorf("%w: evaluator runtime authorities or budget are incomplete", ErrSyntheticProvisionUnavailable)
	}
	return nil
}

func distinctNonempty(values ...string) bool {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			return false
		}
		if _, exists := seen[value]; exists {
			return false
		}
		seen[value] = struct{}{}
	}
	return true
}

func canonicalTaskCase(task TaskCase) bool {
	for _, expected := range DefaultSuite().Tasks {
		if task.ID == expected.ID && task.Kind == expected.Kind && task.TenantID == expected.TenantID && task.Goal == expected.Goal &&
			task.FixtureConnection == expected.FixtureConnection && task.ApprovalEffect == expected.ApprovalEffect {
			return true
		}
	}
	return false
}

func canonicalFixturePlan(task TaskCase) []agentrun.PlanStep {
	read := func(id, skill string) agentrun.PlanStep {
		return agentrun.PlanStep{ID: id, Type: agentrun.StepRead, SkillID: skill, SkillVersion: 1, ExpectedOutput: "fixture result", Tier: agentrun.TierRead}
	}
	verify := func(id string) agentrun.PlanStep {
		return agentrun.PlanStep{ID: id, Type: agentrun.StepVerify, SkillID: "skill.lookup", SkillVersion: 1, ExpectedOutput: "verified fixture result", Tier: agentrun.TierRead, VerificationRef: "fixture:" + task.ID}
	}
	switch task.Kind {
	case TaskPromotionPreparation:
		return []agentrun.PlanStep{
			read("read-promotion-fixture", "skill.lookup"),
			{ID: "submit-promotion-fixture", Type: agentrun.StepSubmit, SkillID: "skill.update", SkillVersion: 1, ExpectedOutput: "governed promotion draft", Tier: agentrun.TierSubmitGoverned},
			verify("verify-promotion-fixture"),
		}
	case TaskOnboardingChecklist:
		return []agentrun.PlanStep{read("read-onboarding-fixture", "skill.lookup"), verify("verify-onboarding-fixture")}
	case TaskPolicyQuestion:
		return []agentrun.PlanStep{read("read-policy-fixture", "skill.lookup"), {ID: "draft-policy-answer", Type: agentrun.StepAnalyze, SkillID: "skill.summarize", SkillVersion: 1, ExpectedOutput: "cited fixture answer", Tier: agentrun.TierPrivateDraft}, verify("verify-policy-citations")}
	case TaskCrossSystemLookup:
		step := read("read-cross-system-fixture", "workers.read")
		step.ConnectionID = task.FixtureConnection
		return []agentrun.PlanStep{step, verify("verify-cross-system-fixture")}
	default:
		return nil
	}
}

func cloneAuthority(authority trust.AuthorityScope) trust.AuthorityScope {
	authority.Capabilities = append([]string(nil), authority.Capabilities...)
	authority.Resources = append([]string(nil), authority.Resources...)
	authority.Fields = append([]string(nil), authority.Fields...)
	authority.Purposes = append([]string(nil), authority.Purposes...)
	authority.SkillAuthorities = trust.CloneSkillAuthorities(authority.SkillAuthorities)
	return authority
}

func validSyntheticBudget(limit agentbudget.Limits) bool {
	return limit.Steps > 0 && limit.Tokens > 0 && limit.WallClock > 0 && limit.SpendMicros > 0
}
