package clockservice

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/timeprofile"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

var (
	ErrNoTimeWorkflowPlan        = errors.New("clock workflow: no plan matches the resolved time profile")
	ErrAmbiguousTimeWorkflowPlan = errors.New("clock workflow: more than one plan matches the resolved time profile")
)

// TimeWorkflowPlanCandidate is one published plan registration. Match reads
// the resolved profile, never an assignment or worker-type branch in a
// template. TenantKey is empty for the product registration and non-empty for
// a tenant overlay of that same template.
type TimeWorkflowPlanCandidate struct {
	TenantKey string
	Template  timeprofile.Template
	Priority  int
	Match     func(timeprofile.TimeProfile) bool
	Selection runtime.WorkflowSelection
}

// TimeWorkflowPlanRegistry is an immutable value registry for the time
// template registrations visible to a runtime cell. The caller builds a new
// value when a publication changes; open runs retain their selection in their
// durable binding.
type TimeWorkflowPlanRegistry struct {
	Candidates []TimeWorkflowPlanCandidate
}

func NewTimeWorkflowPlanRegistry(candidates []TimeWorkflowPlanCandidate) (TimeWorkflowPlanRegistry, error) {
	r := TimeWorkflowPlanRegistry{Candidates: append([]TimeWorkflowPlanCandidate(nil), candidates...)}
	if err := r.Validate(); err != nil {
		return TimeWorkflowPlanRegistry{}, err
	}
	return r, nil
}

// Validate proves registration shape and rejects duplicate publication keys.
// ResolveProfileFor is the publish-time ambiguity check for a concrete
// profile; it is deliberately separate because predicates are executable
// functions and cannot be safely inferred from their code.
func (r TimeWorkflowPlanRegistry) Validate() error {
	seen := make(map[string]struct{}, len(r.Candidates))
	for i, candidate := range r.Candidates {
		if !candidate.Template.Valid() {
			return fmt.Errorf("%w: candidate %d has invalid template %q", ErrNoTimeWorkflowPlan, i, candidate.Template)
		}
		if candidate.Match == nil {
			return fmt.Errorf("%w: candidate %d has no profile predicate", ErrNoTimeWorkflowPlan, i)
		}
		if strings.TrimSpace(candidate.Selection.WorkflowID) == "" || candidate.Selection.Plan == nil || candidate.Selection.Plan.WorkflowID != candidate.Selection.WorkflowID {
			return fmt.Errorf("%w: candidate %d has incomplete workflow selection", ErrNoTimeWorkflowPlan, i)
		}
		if candidate.Selection.Pin.CompiledPlanDigest == "" && candidate.Selection.Pin.SemanticVersion == "" {
			return fmt.Errorf("%w: candidate %d has no exact version pin", ErrNoTimeWorkflowPlan, i)
		}
		key := fmt.Sprintf("%s\x00%s\x00%d", strings.TrimSpace(candidate.TenantKey), candidate.Template, candidate.Priority)
		if _, ok := seen[key]; ok {
			return fmt.Errorf("%w: duplicate publication key %q", ErrAmbiguousTimeWorkflowPlan, key)
		}
		seen[key] = struct{}{}
	}
	return nil
}

// ResolveProfileFor selects exactly one candidate for the profile. A tenant
// overlay outranks the product registration at the same priority; two equally
// specific matches are a publication/configuration error, never a first-match
// guess.
func (r TimeWorkflowPlanRegistry) ResolveProfileFor(tenant string, profile timeprofile.TimeProfile) (runtime.WorkflowSelection, timeprofile.Template, error) {
	if strings.TrimSpace(tenant) == "" {
		return runtime.WorkflowSelection{}, "", errors.New("clock workflow: tenant key is required for plan resolution")
	}
	if err := r.Validate(); err != nil {
		return runtime.WorkflowSelection{}, "", err
	}
	if err := profile.Validate(); err != nil {
		return runtime.WorkflowSelection{}, "", err
	}
	templateID, err := timeprofile.TemplateFor(profile)
	if err != nil {
		return runtime.WorkflowSelection{}, "", err
	}
	best := -1
	bestPriority := 0
	bestSpecificity := -1
	var selected TimeWorkflowPlanCandidate
	for i, candidate := range r.Candidates {
		if candidate.Template != templateID || (candidate.TenantKey != "" && candidate.TenantKey != tenant) || !candidate.Match(profile) {
			continue
		}
		specificity := 0
		if candidate.TenantKey != "" {
			specificity = 1
		}
		if best < 0 || candidate.Priority > bestPriority || (candidate.Priority == bestPriority && specificity > bestSpecificity) {
			best, bestPriority, bestSpecificity, selected = i, candidate.Priority, specificity, candidate
			continue
		}
		if candidate.Priority == bestPriority && specificity == bestSpecificity {
			return runtime.WorkflowSelection{}, "", fmt.Errorf("%w: template %s", ErrAmbiguousTimeWorkflowPlan, templateID)
		}
	}
	if best < 0 {
		return runtime.WorkflowSelection{}, "", fmt.Errorf("%w: template %s tenant %q", ErrNoTimeWorkflowPlan, templateID, tenant)
	}
	selection := selected.Selection
	selection.RevalidationKeys = append([]string(nil), selection.RevalidationKeys...)
	return selection, templateID, nil
}

// TimeWorkflowResolver is the runtime resolver for a trigger start. It
// resolves the assignment profile once and selects the matching published
// plan. ResolvedProfile is optional and is used by StartRun to keep the
// profile read and the plan selection in one request-scoped snapshot.
type TimeWorkflowResolver struct {
	Profiles        ProfileResolver
	Plans           TimeWorkflowPlanRegistry
	TenantKey       func(uuid.UUID) string
	ResolvedProfile *timeprofile.TimeProfile
}

func (r TimeWorkflowResolver) ResolveWorkflow(ctx context.Context, req runtime.StartRequest) (runtime.WorkflowSelection, error) {
	source := req.StartSourceValue()
	if source.Trigger == nil || source.Kind != runtime.StartSourceTrigger {
		return runtime.WorkflowSelection{}, errors.New("clock workflow: time plan resolution requires a trigger start")
	}
	tenant := req.TenantID.String()
	if r.TenantKey != nil {
		tenant = strings.TrimSpace(r.TenantKey(req.TenantID))
	}
	if tenant == "" {
		return runtime.WorkflowSelection{}, errors.New("clock workflow: profile tenant key is required")
	}
	profile := r.ResolvedProfile
	if profile == nil {
		if r.Profiles == nil {
			return runtime.WorkflowSelection{}, errors.New("clock workflow: profile resolver is required")
		}
		worker := strings.TrimSpace(source.Trigger.Facts["worker_ref"])
		assignment := strings.TrimSpace(source.Trigger.Facts["assignment_ref"])
		occurredAt, err := time.Parse(time.RFC3339Nano, source.Trigger.Facts["occurred_at"])
		if err != nil || worker == "" || assignment == "" {
			return runtime.WorkflowSelection{}, errors.New("clock workflow: worker, assignment and occurred_at facts are required")
		}
		resolved, err := r.Profiles.Resolve(ctx, tenant, worker, assignment, occurredAt)
		if err != nil {
			return runtime.WorkflowSelection{}, fmt.Errorf("clock workflow: resolve assignment profile: %w", err)
		}
		profile = &resolved
	}
	selection, _, err := r.Plans.ResolveProfileFor(tenant, *profile)
	return selection, err
}

func profileSnapshot(profile timeprofile.TimeProfile) (id string, version uint64, digest, template string, err error) {
	digest, err = profile.Digest()
	if err != nil {
		return "", 0, "", "", err
	}
	tmpl, err := timeprofile.TemplateFor(profile)
	if err != nil {
		return "", 0, "", "", err
	}
	return profile.ID, profile.Version, digest, string(tmpl), nil
}

// profileFromStart resolves a profile before the workflow engine is called.
// This ensures the exact profile used for selection is the one recorded with
// the run binding, while a later signal uses only the binding's pinned plan.
func (a TimeClockRuntimeAdapter) profileFromStart(ctx context.Context, tenant, worker, assignment string, at time.Time) (*timeprofile.TimeProfile, error) {
	if a.Profiles == nil {
		return nil, nil
	}
	profile, err := a.Profiles.Resolve(ctx, tenant, worker, assignment, at)
	if err != nil {
		return nil, fmt.Errorf("clock workflow runtime: resolve profile: %w", err)
	}
	return &profile, nil
}
