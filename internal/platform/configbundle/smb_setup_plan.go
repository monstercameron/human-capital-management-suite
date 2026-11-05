// Resumable business setup plans for SMB-SETUP-001.
//
// A SetupPlanRevision is a tenant-owned, revisioned setup plan: a pinned
// starter baseline, chosen business modules, a required/optional step DAG,
// per-step verified receipts, and required role/policy checks. The planner
// is hermetic like the rest of this package: no storage, clocks, or
// network. Restarts restore through Export/Import snapshots; durable
// persistence stays with the existing configregistry stores.
//
// Setup never mints tenants, employees, customers, imports, or connections:
// Advance invokes bounded owner capabilities (or records their verified
// receipts), and baseline activation reuses the promotion control plane's
// own approval records, including its self-approval rejection.
package configbundle

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/platform/config/promotion"
)

// Stable setup-plan refusal causes. Callers discriminate with errors.Is;
// the *Error Detail carries diagnostics only.
var (
	ErrSetupInvalid               = errors.New("configbundle: invalid setup plan request")
	ErrSetupNotFound              = errors.New("configbundle: setup plan is not visible")
	ErrSetupRevisionConflict      = errors.New("configbundle: setup plan revision conflict")
	ErrSetupBlocked               = errors.New("configbundle: setup step is blocked on a dependency")
	ErrSetupSkipRefused           = errors.New("configbundle: required setup step cannot be skipped")
	ErrSetupAbandoned             = errors.New("configbundle: setup plan is abandoned")
	ErrSetupBaselineRefused       = errors.New("configbundle: setup baseline approval is not valid")
	ErrSetupIdempotencyConflict   = errors.New("configbundle: setup idempotency key was reused for another step")
	ErrSetupUnknownModule         = errors.New("configbundle: setup plan names an unregistered module")
	ErrSetupResponseLost          = errors.New("configbundle: setup receipt recorded but response lost")
	ErrStepCapabilityUnavailable  = errors.New("configbundle: setup step capability is unavailable")
	ErrStepCapabilityNotSupported = errors.New("configbundle: setup step capability is not supported")
)

// Setup plan lifecycle states. READY is derived from verified required
// outcomes (receipts, role/policy checks, activated baseline), never from a
// visited-step percentage.
type SetupState string

const (
	SetupStateDraft     SetupState = "DRAFT"
	SetupStateInProg    SetupState = "IN_PROGRESS"
	SetupStateBlocked   SetupState = "BLOCKED"
	SetupStateReady     SetupState = "READY"
	SetupStateAbandoned SetupState = "ABANDONED"
)

// SetupStateInProgress is the descriptive spelling of SetupStateInProg.
const SetupStateInProgress = SetupStateInProg

// Setup step progress states.
type SetupStepStatus string

const (
	SetupStepPending  SetupStepStatus = "pending"
	SetupStepBlocked  SetupStepStatus = "blocked"
	SetupStepVerified SetupStepStatus = "verified"
	SetupStepSkipped  SetupStepStatus = "skipped"
)

// Served setup-view vocabulary.
const (
	SetupActionAdvance = "advance"
	SetupActionSkip    = "skip"
	SetupActionAbandon = "abandon"
	SetupActionResume  = "resume"
)

// SetupStep is one node of the plan DAG. ModuleID is empty for core steps
// and names the owning business module otherwise; removing that module
// drops the step's pending work but never its verified receipt.
type SetupStep struct {
	ID         string   `json:"id"`
	Title      string   `json:"title"`
	Required   bool     `json:"required"`
	DependsOn  []string `json:"depends_on,omitempty"`
	ModuleID   string   `json:"module_id,omitempty"`
	Capability string   `json:"capability"`
}

// StepProgress is the live state of one plan step.
type StepProgress struct {
	Status        SetupStepStatus `json:"status"`
	ReceiptRef    string          `json:"receipt_ref,omitempty"`
	ReceiptTitle  string          `json:"receipt_title,omitempty"`
	SkipReason    string          `json:"skip_reason,omitempty"`
	BlockedReason string          `json:"blocked_reason,omitempty"`
}

// BaselinePin cites the exact approved starter bundle a solo owner may
// activate: package, digest, tenant scope, distinct approver, and expiry.
// Every byte is re-verified against the promotion registry at activation.
type BaselinePin struct {
	PackageID string    `json:"package_id"`
	Digest    string    `json:"digest"`
	Scope     Scope     `json:"scope"`
	Approver  string    `json:"approver"`
	ExpiresAt time.Time `json:"expires_at"`
}

// CheckProgress records one required role/policy check outcome and the
// evidence behind a pass.
type CheckProgress struct {
	Passed      bool   `json:"passed"`
	EvidenceRef string `json:"evidence_ref,omitempty"`
}

// SetupPlanRevision pins one revision of a tenant's setup plan. It is the
// compare-and-swap unit: writers present the revision they prepared
// against and the planner revalidates it at execution.
type SetupPlanRevision struct {
	PlanID         string                   `json:"plan_id"`
	Scope          Scope                    `json:"scope"`
	Revision       uint64                   `json:"revision"`
	Predecessor    uint64                   `json:"predecessor"`
	State          SetupState               `json:"state"`
	Admins         []string                 `json:"admins"`
	Baseline       BaselinePin              `json:"baseline"`
	BaselineActive bool                     `json:"baseline_active"`
	Modules        []string                 `json:"modules"`
	Steps          []SetupStep              `json:"steps"`
	StepState      map[string]StepProgress  `json:"step_state"`
	RequiredChecks []string                 `json:"required_checks"`
	Checks         map[string]CheckProgress `json:"checks"`
	AbandonReason  string                   `json:"abandon_reason,omitempty"`
}

// StepReceipt is the owner capability's verified evidence for one step.
type StepReceipt struct {
	ReceiptRef string    `json:"receipt_ref"`
	Title      string    `json:"title"`
	EffectID   string    `json:"effect_id"`
	At         time.Time `json:"at"`
}

// StepInput is the verified context one capability execution runs under.
type StepInput struct {
	PlanID   string
	Scope    Scope
	Revision uint64
	Actor    string
	At       time.Time
}

// StepCapability is the bounded owner-step port Advance invokes. It is
// synchronous and side-effecting; exactly-once across crashes comes from
// the planner's receipt-before-response record plus idempotency keys, not
// from the capability itself.
type StepCapability interface {
	Execute(step SetupStep, in StepInput) (StepReceipt, error)
}

// SetupPlanRequest creates a plan. Scope names the existing tenant; setup
// never creates a tenant, so an empty tenant is refused, not invented.
type SetupPlanRequest struct {
	PlanID         string
	Scope          Scope
	Admin          string
	Baseline       BaselinePin
	Modules        []string
	Steps          []SetupStep
	RequiredChecks []string
	At             time.Time
}

// SetupAdvanceRequest advances one step. IdempotencyKey converges retries:
// the same key and step replays the recorded receipt without a second
// owner effect, while the same key on another step conflicts.
type SetupAdvanceRequest struct {
	Actor            string
	PlanID           string
	ExpectedRevision uint64
	StepID           string
	IdempotencyKey   string
	At               time.Time
	Caps             StepCapability
}

// SetupSkipRequest waives one optional step with a reason.
type SetupSkipRequest struct {
	Actor            string
	PlanID           string
	ExpectedRevision uint64
	StepID           string
	Reason           string
}

// SetupCheckRequest records one required role/policy check outcome.
type SetupCheckRequest struct {
	Actor            string
	PlanID           string
	ExpectedRevision uint64
	CheckID          string
	Passed           bool
	EvidenceRef      string
	At               time.Time
}

// SetupReviseRequest changes the chosen modules. Steps owned by removed
// modules lose pending work; verified receipts and committed steps survive.
type SetupReviseRequest struct {
	Actor            string
	PlanID           string
	ExpectedRevision uint64
	Modules          []string
	At               time.Time
}

// SetupAdminRequest hands resume to another admin.
type SetupAdminRequest struct {
	Actor            string
	PlanID           string
	ExpectedRevision uint64
	Admin            string
	At               time.Time
}

// SetupAbandonRequest stops new work while preserving committed records.
type SetupAbandonRequest struct {
	Actor            string
	PlanID           string
	ExpectedRevision uint64
	Reason           string
	At               time.Time
}

// SetupView is the resolved per-plan state served to the admin setup page.
// It is advisory; every mutation revalidates revision, tenant, and admin
// at execution.
type SetupView struct {
	PlanID            string
	State             SetupState
	Revision          uint64
	RequiredRemaining int
	RequiredTotal     int
	OptionalPending   []string
	BlockedReasons    []string
	BaselineExplain   string
	Summary           string
	AvailableActions  []string
}

// setupEffect records one idempotency key's outcome for replay.
type setupEffect struct {
	PlanID   string      `json:"plan_id"`
	StepID   string      `json:"step_id"`
	Receipt  StepReceipt `json:"receipt"`
	Revision uint64      `json:"revision"`
}

// SetupPlanner is the concurrency-safe in-memory setup port. Modules is the
// compiled business-module set plans choose from; approvals is the
// promotion control plane whose records baseline pins cite. AfterReceipt
// is a fault-injection seam: when non-nil it runs after the owner receipt
// is recorded but before Advance responds, so tests can prove a lost
// response still converges without a second effect.
type SetupPlanner struct {
	mu           sync.Mutex
	modules      *ModuleRegistry
	approvals    *promotion.Registry
	plans        map[string]*SetupPlanRevision
	history      map[string][]SetupPlanRevision
	effects      map[string]setupEffect
	AfterReceipt func() error
}

// NewSetupPlanner builds the setup port over a compiled module set and the
// promotion control plane. Either dependency may be nil only when the
// caller never exercises the path that needs it; every use is guarded.
func NewSetupPlanner(modules *ModuleRegistry, approvals *promotion.Registry) *SetupPlanner {
	return &SetupPlanner{
		modules:   modules,
		approvals: approvals,
		plans:     make(map[string]*SetupPlanRevision),
		history:   make(map[string][]SetupPlanRevision),
		effects:   make(map[string]setupEffect),
	}
}

func setupErr(code, ref, detail string, cause error) *Error {
	return &Error{Code: code, Ref: ref, Detail: detail, Cause: cause}
}

func setupPlanKey(scope Scope, planID string) string {
	return scope.TenantID + "\x00" + scope.CellID + "\x00" + planID
}

func cloneSetupPlan(in SetupPlanRevision) SetupPlanRevision {
	in.Admins = append([]string(nil), in.Admins...)
	in.Modules = append([]string(nil), in.Modules...)
	in.Steps = append([]SetupStep(nil), in.Steps...)
	for i := range in.Steps {
		in.Steps[i].DependsOn = append([]string(nil), in.Steps[i].DependsOn...)
	}
	state := make(map[string]StepProgress, len(in.StepState))
	for id, progress := range in.StepState {
		state[id] = progress
	}
	in.StepState = state
	in.RequiredChecks = append([]string(nil), in.RequiredChecks...)
	checks := make(map[string]CheckProgress, len(in.Checks))
	for id, check := range in.Checks {
		checks[id] = check
	}
	in.Checks = checks
	return in
}

func (p *SetupPlanner) visibleLocked(scope Scope, planID, actor string) (*SetupPlanRevision, bool) {
	plan, ok := p.plans[setupPlanKey(scope, planID)]
	if !ok || plan.Scope.TenantID != scope.TenantID {
		return nil, false
	}
	for _, admin := range plan.Admins {
		if admin == actor {
			return plan, true
		}
	}
	return nil, false
}

func validateSetupSteps(steps []SetupStep) error {
	byID := make(map[string]SetupStep, len(steps))
	for _, step := range steps {
		id := strings.TrimSpace(step.ID)
		if id == "" || strings.TrimSpace(step.Title) == "" || strings.TrimSpace(step.Capability) == "" {
			return setupErr("SETUP_INVALID_STEP", id, "step id, title, and capability are required", ErrSetupInvalid)
		}
		if _, dup := byID[id]; dup {
			return setupErr("SETUP_DUPLICATE_STEP", id, "step id is declared twice", ErrSetupInvalid)
		}
		byID[id] = step
	}
	for _, step := range steps {
		for _, dep := range step.DependsOn {
			if _, ok := byID[dep]; !ok {
				return setupErr("SETUP_UNKNOWN_DEPENDENCY", step.ID, fmt.Sprintf("dependency %q is not declared", dep), ErrSetupInvalid)
			}
			if dep == step.ID {
				return setupErr("SETUP_STEP_CYCLE", step.ID, "step depends on itself", ErrSetupInvalid)
			}
		}
	}
	return nil
}

func (p *SetupPlanner) checkModulesLocked(scope Scope, modules []string) error {
	if p.modules == nil {
		return setupErr("SETUP_NO_MODULES", "", "no compiled module set is configured", ErrSetupUnknownModule)
	}
	for _, id := range modules {
		if strings.TrimSpace(id) == "" {
			return setupErr("SETUP_INVALID_MODULE", "", "module id is required", ErrSetupInvalid)
		}
		if view := p.modules.ResolveView(scope, id, nil); view.EffectiveStatus == ModuleStatusUnknown {
			return setupErr("SETUP_UNKNOWN_MODULE", id, "module is not registered", ErrSetupUnknownModule)
		}
	}
	return nil
}

// verifyBaselineLocked binds a pin to a live approval record: the package
// must be approved or active, the digest/scope/approver must match exactly,
// and the approver must differ from the package signer. The promotion
// registry's own self-approval rejection is reused, never reimplemented.
func (p *SetupPlanner) verifyBaselineLocked(pin BaselinePin, tenantID string, at time.Time, code string) error {
	if p.approvals == nil {
		return setupErr(code, pin.PackageID, "no promotion control plane is configured", ErrSetupBaselineRefused)
	}
	if strings.TrimSpace(pin.PackageID) == "" || strings.TrimSpace(pin.Digest) == "" || strings.TrimSpace(pin.Approver) == "" {
		return setupErr(code, pin.PackageID, "baseline package, digest, and approver are required", ErrSetupBaselineRefused)
	}
	if pin.Scope.TenantID != tenantID {
		return setupErr(code, pin.PackageID, "baseline scope differs from the setup tenant", ErrSetupBaselineRefused)
	}
	if !pin.ExpiresAt.After(at) {
		return setupErr(code, pin.PackageID, "baseline approval has expired", ErrSetupBaselineRefused)
	}
	record, ok := p.approvals.Get(pin.PackageID)
	if !ok || (record.Status != promotion.StatusApproved && record.Status != promotion.StatusActive) {
		return setupErr(code, pin.PackageID, "baseline package has no approval record", ErrSetupBaselineRefused)
	}
	if record.Approval.Approver != pin.Approver || !strings.EqualFold(record.Approval.Digest, pin.Digest) {
		return setupErr(code, pin.PackageID, "baseline pin differs from its approval record", ErrSetupBaselineRefused)
	}
	if pin.Approver == record.Package.Bundle.SignerKeyID {
		return setupErr(code, pin.PackageID, "baseline approver is the package signer", ErrSetupBaselineRefused)
	}
	return nil
}

// deriveStateLocked recomputes the plan state from verified outcomes. READY
// demands every required step verified, every required check passed, and an
// activated baseline; anything less stays in progress or blocked.
func deriveStateLocked(plan *SetupPlanRevision) {
	if plan.State == SetupStateAbandoned {
		return
	}
	blocked := false
	progressed := plan.BaselineActive
	ready := plan.BaselineActive
	for _, step := range plan.Steps {
		progress := plan.StepState[step.ID]
		switch progress.Status {
		case SetupStepVerified:
			progressed = true
		case SetupStepSkipped:
			progressed = true
			if step.Required {
				ready = false
			}
		case SetupStepBlocked:
			blocked = true
			if step.Required {
				ready = false
			} else {
				progressed = true
			}
		default:
			if step.Required {
				ready = false
			}
		}
	}
	for _, check := range plan.RequiredChecks {
		if !plan.Checks[check].Passed {
			ready = false
		} else {
			progressed = true
		}
	}
	switch {
	case ready && len(plan.Steps) > 0:
		plan.State = SetupStateReady
	case blocked:
		plan.State = SetupStateBlocked
	case progressed:
		plan.State = SetupStateInProg
	default:
		plan.State = SetupStateDraft
	}
}

// Create starts a plan at revision 1. The baseline pin is verified against
// the promotion control plane immediately: a plan that cites no valid
// approval is never created.
func (p *SetupPlanner) Create(req SetupPlanRequest) (SetupPlanRevision, error) {
	if strings.TrimSpace(req.PlanID) == "" {
		return SetupPlanRevision{}, setupErr("SETUP_INVALID_PLAN", "", "plan id is required", ErrSetupInvalid)
	}
	if req.Scope.TenantID == "" {
		return SetupPlanRevision{}, setupErr("SETUP_NO_TENANT", req.PlanID, "setup works on an existing tenant and never creates one", ErrSetupInvalid)
	}
	if strings.TrimSpace(req.Admin) == "" {
		return SetupPlanRevision{}, setupErr("SETUP_NO_ADMIN", req.PlanID, "creating admin is required", ErrSetupInvalid)
	}
	if req.At.IsZero() {
		return SetupPlanRevision{}, setupErr("SETUP_NO_TIME", req.PlanID, "creation time is required", ErrSetupInvalid)
	}
	if len(req.Steps) == 0 {
		return SetupPlanRevision{}, setupErr("SETUP_NO_STEPS", req.PlanID, "at least one setup step is required", ErrSetupInvalid)
	}
	if err := validateSetupSteps(req.Steps); err != nil {
		return SetupPlanRevision{}, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.checkModulesLocked(req.Scope, req.Modules); err != nil {
		return SetupPlanRevision{}, err
	}
	if err := p.verifyBaselineLocked(req.Baseline, req.Scope.TenantID, req.At, "SETUP_BASELINE_REFUSED"); err != nil {
		return SetupPlanRevision{}, err
	}
	key := setupPlanKey(req.Scope, req.PlanID)
	if _, dup := p.plans[key]; dup {
		return SetupPlanRevision{}, setupErr("SETUP_DUPLICATE_PLAN", req.PlanID, "plan id already exists", ErrSetupInvalid)
	}
	// Plan IDs are globally unique so bare-ID writers resolve
	// unambiguously; a second tenant reusing an ID is refused, not
	// shadowed.
	for _, existing := range p.plans {
		if existing.PlanID == req.PlanID {
			return SetupPlanRevision{}, setupErr("SETUP_DUPLICATE_PLAN", req.PlanID, "plan id already exists", ErrSetupInvalid)
		}
	}
	plan := SetupPlanRevision{
		PlanID: req.PlanID, Scope: req.Scope, Revision: 1, State: SetupStateDraft,
		Admins: []string{req.Admin}, Baseline: req.Baseline,
		Modules:        append([]string(nil), req.Modules...),
		Steps:          append([]SetupStep(nil), req.Steps...),
		StepState:      make(map[string]StepProgress, len(req.Steps)),
		RequiredChecks: append([]string(nil), req.RequiredChecks...),
		Checks:         make(map[string]CheckProgress),
	}
	for i := range plan.Steps {
		plan.Steps[i].DependsOn = append([]string(nil), plan.Steps[i].DependsOn...)
		plan.StepState[plan.Steps[i].ID] = StepProgress{Status: SetupStepPending}
	}
	p.plans[key] = &plan
	p.history[key] = []SetupPlanRevision{cloneSetupPlan(plan)}
	return cloneSetupPlan(plan), nil
}

func setupStepByID(plan *SetupPlanRevision, stepID string) (SetupStep, bool) {
	for _, step := range plan.Steps {
		if step.ID == stepID {
			return step, true
		}
	}
	return SetupStep{}, false
}

// Load returns the caller's tenant plan. Unknown plans, cross-tenant reads,
// and non-admin reads all look identical: this port never discloses what
// the caller may not see.
func (p *SetupPlanner) Load(scope Scope, planID, actor string) (SetupPlanRevision, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	plan, ok := p.visibleLocked(scope, planID, actor)
	if !ok {
		return SetupPlanRevision{}, false
	}
	return cloneSetupPlan(*plan), true
}

// Advance executes one step through its owner capability and records the
// verified receipt before responding. Replaying an idempotency key returns
// the recorded receipt without a second owner effect.
func (p *SetupPlanner) Advance(req SetupAdvanceRequest) (SetupPlanRevision, StepReceipt, error) {
	if strings.TrimSpace(req.Actor) == "" || strings.TrimSpace(req.PlanID) == "" || strings.TrimSpace(req.StepID) == "" || strings.TrimSpace(req.IdempotencyKey) == "" {
		return SetupPlanRevision{}, StepReceipt{}, setupErr("SETUP_INVALID_ADVANCE", req.PlanID, "actor, plan, step, and idempotency key are required", ErrSetupInvalid)
	}
	if req.At.IsZero() {
		return SetupPlanRevision{}, StepReceipt{}, setupErr("SETUP_NO_TIME", req.PlanID, "advance time is required", ErrSetupInvalid)
	}
	if req.Caps == nil {
		return SetupPlanRevision{}, StepReceipt{}, setupErr("SETUP_NO_CAPABILITY", req.PlanID, "an owner capability adapter is required", ErrStepCapabilityNotSupported)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	// Replay converges before any revision check: the recorded receipt is
	// the truth a crashed caller is asking for.
	if prior, ok := p.effects[req.IdempotencyKey]; ok {
		if prior.PlanID != req.PlanID || prior.StepID != req.StepID {
			return SetupPlanRevision{}, StepReceipt{}, setupErr("SETUP_IDEMPOTENCY_CONFLICT", req.PlanID, "idempotency key was reused for another step", ErrSetupIdempotencyConflict)
		}
		plan, visible := p.planForWriteLocked(req.PlanID, req.Actor)
		if !visible {
			return SetupPlanRevision{}, StepReceipt{}, setupErr("SETUP_NOT_FOUND", req.PlanID, "setup plan is not visible", ErrSetupNotFound)
		}
		return cloneSetupPlan(*plan), prior.Receipt, nil
	}
	plan, visible := p.planForWriteLocked(req.PlanID, req.Actor)
	if !visible {
		return SetupPlanRevision{}, StepReceipt{}, setupErr("SETUP_NOT_FOUND", req.PlanID, "setup plan is not visible", ErrSetupNotFound)
	}
	if plan.State == SetupStateAbandoned {
		return SetupPlanRevision{}, StepReceipt{}, setupErr("SETUP_ABANDONED", req.PlanID, "setup plan is abandoned", ErrSetupAbandoned)
	}
	if plan.Revision != req.ExpectedRevision {
		return SetupPlanRevision{}, StepReceipt{}, setupErr("SETUP_REVISION_CONFLICT", req.PlanID, "plan revision moved under this writer", ErrSetupRevisionConflict)
	}
	step, ok := setupStepByID(plan, req.StepID)
	if !ok {
		return SetupPlanRevision{}, StepReceipt{}, setupErr("SETUP_UNKNOWN_STEP", req.PlanID, fmt.Sprintf("step %q is not on this plan", req.StepID), ErrSetupInvalid)
	}
	progress := plan.StepState[step.ID]
	if progress.Status == SetupStepVerified || progress.Status == SetupStepSkipped {
		return SetupPlanRevision{}, StepReceipt{}, setupErr("SETUP_STEP_SETTLED", req.PlanID, fmt.Sprintf("step %q is already %s", step.ID, progress.Status), ErrSetupInvalid)
	}
	for _, dep := range step.DependsOn {
		depProgress := plan.StepState[dep]
		depStep, _ := setupStepByID(plan, dep)
		if depProgress.Status == SetupStepVerified {
			continue
		}
		if depProgress.Status == SetupStepSkipped && !depStep.Required {
			continue
		}
		return SetupPlanRevision{}, StepReceipt{}, setupErr("SETUP_BLOCKED", req.PlanID, fmt.Sprintf("step %q waits on %q", step.ID, dep), ErrSetupBlocked)
	}
	receipt, err := req.Caps.Execute(step, StepInput{PlanID: plan.PlanID, Scope: plan.Scope, Revision: plan.Revision, Actor: req.Actor, At: req.At})
	if errors.Is(err, ErrStepCapabilityUnavailable) {
		plan.StepState[step.ID] = StepProgress{Status: SetupStepBlocked, BlockedReason: fmt.Sprintf("capability %q is unavailable; retry when the owner reports ready", step.Capability)}
		plan.Predecessor, plan.Revision = plan.Revision, plan.Revision+1
		deriveStateLocked(plan)
		p.history[setupPlanKey(plan.Scope, plan.PlanID)] = append(p.history[setupPlanKey(plan.Scope, plan.PlanID)], cloneSetupPlan(*plan))
		return SetupPlanRevision{}, StepReceipt{}, setupErr("SETUP_CAPABILITY_UNAVAILABLE", req.PlanID, fmt.Sprintf("step %q is blocked on its owner capability", step.ID), ErrSetupBlocked)
	}
	if err != nil {
		return SetupPlanRevision{}, StepReceipt{}, setupErr("SETUP_CAPABILITY_FAILED", req.PlanID, fmt.Sprintf("step %q capability failed", step.ID), err)
	}
	if strings.TrimSpace(receipt.ReceiptRef) == "" {
		return SetupPlanRevision{}, StepReceipt{}, setupErr("SETUP_NO_RECEIPT", req.PlanID, "owner capability returned no receipt", ErrSetupInvalid)
	}
	// Receipt-before-response: the verified outcome is durable in this
	// revision before the caller ever sees it.
	plan.StepState[step.ID] = StepProgress{Status: SetupStepVerified, ReceiptRef: receipt.ReceiptRef, ReceiptTitle: receipt.Title}
	plan.Predecessor, plan.Revision = plan.Revision, plan.Revision+1
	deriveStateLocked(plan)
	p.effects[req.IdempotencyKey] = setupEffect{PlanID: plan.PlanID, StepID: step.ID, Receipt: receipt, Revision: plan.Revision}
	p.history[setupPlanKey(plan.Scope, plan.PlanID)] = append(p.history[setupPlanKey(plan.Scope, plan.PlanID)], cloneSetupPlan(*plan))
	out := cloneSetupPlan(*plan)
	if p.AfterReceipt != nil {
		if crash := p.AfterReceipt(); crash != nil {
			return SetupPlanRevision{}, StepReceipt{}, setupErr("SETUP_RESPONSE_LOST", req.PlanID, "owner receipt recorded but the response was lost", fmt.Errorf("%w: %w", ErrSetupResponseLost, crash))
		}
	}
	return out, receipt, nil
}

// planForWriteLocked resolves a plan by bare ID across the planner's scopes
// for callers that already authenticated the tenant at the edge; visibility
// still requires plan-admin membership, and every other check runs on the
// stored scope.
func (p *SetupPlanner) planForWriteLocked(planID, actor string) (*SetupPlanRevision, bool) {
	for _, plan := range p.plans {
		if plan.PlanID != planID {
			continue
		}
		for _, admin := range plan.Admins {
			if admin == actor {
				return plan, true
			}
		}
		return nil, false
	}
	return nil, false
}

// SkipStep waives one optional step with a reason. Required steps and
// reasonless waivers are refused; there is no generic skip bypass.
func (p *SetupPlanner) SkipStep(req SetupSkipRequest) (SetupPlanRevision, error) {
	if strings.TrimSpace(req.Actor) == "" || strings.TrimSpace(req.PlanID) == "" || strings.TrimSpace(req.StepID) == "" {
		return SetupPlanRevision{}, setupErr("SETUP_INVALID_SKIP", req.PlanID, "actor, plan, and step are required", ErrSetupInvalid)
	}
	if strings.TrimSpace(req.Reason) == "" {
		return SetupPlanRevision{}, setupErr("SETUP_SKIP_REASON", req.PlanID, "a skip must carry its reason", ErrSetupInvalid)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	plan, visible := p.planForWriteLocked(req.PlanID, req.Actor)
	if !visible {
		return SetupPlanRevision{}, setupErr("SETUP_NOT_FOUND", req.PlanID, "setup plan is not visible", ErrSetupNotFound)
	}
	if plan.State == SetupStateAbandoned {
		return SetupPlanRevision{}, setupErr("SETUP_ABANDONED", req.PlanID, "setup plan is abandoned", ErrSetupAbandoned)
	}
	if plan.Revision != req.ExpectedRevision {
		return SetupPlanRevision{}, setupErr("SETUP_REVISION_CONFLICT", req.PlanID, "plan revision moved under this writer", ErrSetupRevisionConflict)
	}
	step, ok := setupStepByID(plan, req.StepID)
	if !ok {
		return SetupPlanRevision{}, setupErr("SETUP_UNKNOWN_STEP", req.PlanID, fmt.Sprintf("step %q is not on this plan", req.StepID), ErrSetupInvalid)
	}
	if step.Required {
		return SetupPlanRevision{}, setupErr("SETUP_SKIP_REFUSED", req.PlanID, fmt.Sprintf("step %q is required and cannot be skipped", step.ID), ErrSetupSkipRefused)
	}
	progress := plan.StepState[step.ID]
	if progress.Status == SetupStepVerified || progress.Status == SetupStepSkipped {
		return SetupPlanRevision{}, setupErr("SETUP_STEP_SETTLED", req.PlanID, fmt.Sprintf("step %q is already %s", step.ID, progress.Status), ErrSetupInvalid)
	}
	plan.StepState[step.ID] = StepProgress{Status: SetupStepSkipped, SkipReason: req.Reason}
	plan.Predecessor, plan.Revision = plan.Revision, plan.Revision+1
	deriveStateLocked(plan)
	p.history[setupPlanKey(plan.Scope, plan.PlanID)] = append(p.history[setupPlanKey(plan.Scope, plan.PlanID)], cloneSetupPlan(*plan))
	return cloneSetupPlan(*plan), nil
}

// RecordCheck records one required role/policy check outcome. A pass must
// cite its evidence; READY stays impossible while any required check fails.
func (p *SetupPlanner) RecordCheck(req SetupCheckRequest) (SetupPlanRevision, error) {
	if strings.TrimSpace(req.Actor) == "" || strings.TrimSpace(req.PlanID) == "" || strings.TrimSpace(req.CheckID) == "" {
		return SetupPlanRevision{}, setupErr("SETUP_INVALID_CHECK", req.PlanID, "actor, plan, and check are required", ErrSetupInvalid)
	}
	if req.Passed && strings.TrimSpace(req.EvidenceRef) == "" {
		return SetupPlanRevision{}, setupErr("SETUP_CHECK_EVIDENCE", req.PlanID, "a passing check must cite its evidence", ErrSetupInvalid)
	}
	if req.At.IsZero() {
		return SetupPlanRevision{}, setupErr("SETUP_NO_TIME", req.PlanID, "check time is required", ErrSetupInvalid)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	plan, visible := p.planForWriteLocked(req.PlanID, req.Actor)
	if !visible {
		return SetupPlanRevision{}, setupErr("SETUP_NOT_FOUND", req.PlanID, "setup plan is not visible", ErrSetupNotFound)
	}
	if plan.State == SetupStateAbandoned {
		return SetupPlanRevision{}, setupErr("SETUP_ABANDONED", req.PlanID, "setup plan is abandoned", ErrSetupAbandoned)
	}
	if plan.Revision != req.ExpectedRevision {
		return SetupPlanRevision{}, setupErr("SETUP_REVISION_CONFLICT", req.PlanID, "plan revision moved under this writer", ErrSetupRevisionConflict)
	}
	declared := false
	for _, check := range plan.RequiredChecks {
		if check == req.CheckID {
			declared = true
		}
	}
	if !declared {
		return SetupPlanRevision{}, setupErr("SETUP_UNKNOWN_CHECK", req.PlanID, fmt.Sprintf("check %q is not required by this plan", req.CheckID), ErrSetupInvalid)
	}
	plan.Checks[req.CheckID] = CheckProgress{Passed: req.Passed, EvidenceRef: req.EvidenceRef}
	plan.Predecessor, plan.Revision = plan.Revision, plan.Revision+1
	deriveStateLocked(plan)
	p.history[setupPlanKey(plan.Scope, plan.PlanID)] = append(p.history[setupPlanKey(plan.Scope, plan.PlanID)], cloneSetupPlan(*plan))
	return cloneSetupPlan(*plan), nil
}

// Revise changes the chosen modules under compare-and-swap. Steps owned by
// a removed module lose pending work and their module binding; verified
// receipts stay recorded so completed effects never repeat.
func (p *SetupPlanner) Revise(req SetupReviseRequest) (SetupPlanRevision, error) {
	if strings.TrimSpace(req.Actor) == "" || strings.TrimSpace(req.PlanID) == "" {
		return SetupPlanRevision{}, setupErr("SETUP_INVALID_REVISE", req.PlanID, "actor and plan are required", ErrSetupInvalid)
	}
	if req.At.IsZero() {
		return SetupPlanRevision{}, setupErr("SETUP_NO_TIME", req.PlanID, "revision time is required", ErrSetupInvalid)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	plan, visible := p.planForWriteLocked(req.PlanID, req.Actor)
	if !visible {
		return SetupPlanRevision{}, setupErr("SETUP_NOT_FOUND", req.PlanID, "setup plan is not visible", ErrSetupNotFound)
	}
	if plan.State == SetupStateAbandoned {
		return SetupPlanRevision{}, setupErr("SETUP_ABANDONED", req.PlanID, "setup plan is abandoned", ErrSetupAbandoned)
	}
	if plan.Revision != req.ExpectedRevision {
		return SetupPlanRevision{}, setupErr("SETUP_REVISION_CONFLICT", req.PlanID, "plan revision moved under this writer", ErrSetupRevisionConflict)
	}
	if err := p.checkModulesLocked(plan.Scope, req.Modules); err != nil {
		return SetupPlanRevision{}, err
	}
	kept := make(map[string]bool, len(req.Modules))
	for _, id := range req.Modules {
		kept[id] = true
	}
	steps := make([]SetupStep, 0, len(plan.Steps))
	for _, step := range plan.Steps {
		if step.ModuleID != "" && !kept[step.ModuleID] {
			if plan.StepState[step.ID].Status == SetupStepVerified {
				steps = append(steps, step)
				continue
			}
			delete(plan.StepState, step.ID)
			continue
		}
		steps = append(steps, step)
	}
	plan.Steps = steps
	plan.Modules = append([]string(nil), req.Modules...)
	plan.Predecessor, plan.Revision = plan.Revision, plan.Revision+1
	deriveStateLocked(plan)
	p.history[setupPlanKey(plan.Scope, plan.PlanID)] = append(p.history[setupPlanKey(plan.Scope, plan.PlanID)], cloneSetupPlan(*plan))
	return cloneSetupPlan(*plan), nil
}

// AddAdmin hands resume to another admin. Only a current admin may hand
// off, and the handoff itself is a revisioned write.
func (p *SetupPlanner) AddAdmin(req SetupAdminRequest) (SetupPlanRevision, error) {
	if strings.TrimSpace(req.Actor) == "" || strings.TrimSpace(req.PlanID) == "" || strings.TrimSpace(req.Admin) == "" {
		return SetupPlanRevision{}, setupErr("SETUP_INVALID_ADMIN", req.PlanID, "actor, plan, and new admin are required", ErrSetupInvalid)
	}
	if req.At.IsZero() {
		return SetupPlanRevision{}, setupErr("SETUP_NO_TIME", req.PlanID, "handoff time is required", ErrSetupInvalid)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	plan, visible := p.planForWriteLocked(req.PlanID, req.Actor)
	if !visible {
		return SetupPlanRevision{}, setupErr("SETUP_NOT_FOUND", req.PlanID, "setup plan is not visible", ErrSetupNotFound)
	}
	if plan.State == SetupStateAbandoned {
		return SetupPlanRevision{}, setupErr("SETUP_ABANDONED", req.PlanID, "setup plan is abandoned", ErrSetupAbandoned)
	}
	if plan.Revision != req.ExpectedRevision {
		return SetupPlanRevision{}, setupErr("SETUP_REVISION_CONFLICT", req.PlanID, "plan revision moved under this writer", ErrSetupRevisionConflict)
	}
	for _, admin := range plan.Admins {
		if admin == req.Admin {
			return SetupPlanRevision{}, setupErr("SETUP_DUPLICATE_ADMIN", req.PlanID, "admin is already on this plan", ErrSetupInvalid)
		}
	}
	plan.Admins = append(plan.Admins, req.Admin)
	plan.Predecessor, plan.Revision = plan.Revision, plan.Revision+1
	deriveStateLocked(plan)
	p.history[setupPlanKey(plan.Scope, plan.PlanID)] = append(p.history[setupPlanKey(plan.Scope, plan.PlanID)], cloneSetupPlan(*plan))
	return cloneSetupPlan(*plan), nil
}

// Abandon stops new work while preserving committed records and issued
// invitations for explicit owner correction. Abandon is terminal.
func (p *SetupPlanner) Abandon(req SetupAbandonRequest) (SetupPlanRevision, error) {
	if strings.TrimSpace(req.Actor) == "" || strings.TrimSpace(req.PlanID) == "" || strings.TrimSpace(req.Reason) == "" {
		return SetupPlanRevision{}, setupErr("SETUP_INVALID_ABANDON", req.PlanID, "actor, plan, and reason are required", ErrSetupInvalid)
	}
	if req.At.IsZero() {
		return SetupPlanRevision{}, setupErr("SETUP_NO_TIME", req.PlanID, "abandon time is required", ErrSetupInvalid)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	plan, visible := p.planForWriteLocked(req.PlanID, req.Actor)
	if !visible {
		return SetupPlanRevision{}, setupErr("SETUP_NOT_FOUND", req.PlanID, "setup plan is not visible", ErrSetupNotFound)
	}
	if plan.State == SetupStateAbandoned {
		return SetupPlanRevision{}, setupErr("SETUP_ABANDONED", req.PlanID, "setup plan is abandoned", ErrSetupAbandoned)
	}
	if plan.Revision != req.ExpectedRevision {
		return SetupPlanRevision{}, setupErr("SETUP_REVISION_CONFLICT", req.PlanID, "plan revision moved under this writer", ErrSetupRevisionConflict)
	}
	plan.State = SetupStateAbandoned
	plan.AbandonReason = req.Reason
	plan.Predecessor, plan.Revision = plan.Revision, plan.Revision+1
	p.history[setupPlanKey(plan.Scope, plan.PlanID)] = append(p.history[setupPlanKey(plan.Scope, plan.PlanID)], cloneSetupPlan(*plan))
	return cloneSetupPlan(*plan), nil
}

// ActivateBaseline activates the plan's pinned starter baseline for a
// solo owner. The pin is re-verified against the live approval record:
// any changed byte, lapsed expiry, or self-approved authority refuses,
// and the page explains that distinct approval is required.
func (p *SetupPlanner) ActivateBaseline(scope Scope, planID, actor string, expectedRevision uint64, at time.Time) (SetupPlanRevision, error) {
	if strings.TrimSpace(actor) == "" || strings.TrimSpace(planID) == "" {
		return SetupPlanRevision{}, setupErr("SETUP_INVALID_ACTIVATE", planID, "actor and plan are required", ErrSetupInvalid)
	}
	if at.IsZero() {
		return SetupPlanRevision{}, setupErr("SETUP_NO_TIME", planID, "activation time is required", ErrSetupInvalid)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	plan, visible := p.visibleLocked(scope, planID, actor)
	if !visible {
		return SetupPlanRevision{}, setupErr("SETUP_NOT_FOUND", planID, "setup plan is not visible", ErrSetupNotFound)
	}
	if plan.State == SetupStateAbandoned {
		return SetupPlanRevision{}, setupErr("SETUP_ABANDONED", planID, "setup plan is abandoned", ErrSetupAbandoned)
	}
	if plan.Revision != expectedRevision {
		return SetupPlanRevision{}, setupErr("SETUP_REVISION_CONFLICT", planID, "plan revision moved under this writer", ErrSetupRevisionConflict)
	}
	if err := p.verifyBaselineLocked(plan.Baseline, plan.Scope.TenantID, at, "SETUP_BASELINE_REFUSED"); err != nil {
		return SetupPlanRevision{}, err
	}
	plan.BaselineActive = true
	plan.Predecessor, plan.Revision = plan.Revision, plan.Revision+1
	deriveStateLocked(plan)
	p.history[setupPlanKey(plan.Scope, plan.PlanID)] = append(p.history[setupPlanKey(plan.Scope, plan.PlanID)], cloneSetupPlan(*plan))
	return cloneSetupPlan(*plan), nil
}

// View resolves the admin setup-page projection: exact required-remaining
// counts, optional work labeled separately, and an actionable baseline
// explanation. Non-admin and cross-tenant viewers get nothing.
func (p *SetupPlanner) View(scope Scope, planID, viewer string) (SetupView, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	plan, visible := p.visibleLocked(scope, planID, viewer)
	if !visible {
		return SetupView{}, false
	}
	view := SetupView{PlanID: plan.PlanID, State: plan.State, Revision: plan.Revision}
	for _, step := range plan.Steps {
		progress := plan.StepState[step.ID]
		if step.Required {
			view.RequiredTotal++
			if progress.Status != SetupStepVerified {
				view.RequiredRemaining++
			}
		} else if progress.Status == SetupStepPending || progress.Status == SetupStepBlocked {
			view.OptionalPending = append(view.OptionalPending, step.ID)
		}
		if progress.Status == SetupStepBlocked && progress.BlockedReason != "" {
			view.BlockedReasons = append(view.BlockedReasons, step.ID+": "+progress.BlockedReason)
		}
	}
	sort.Strings(view.OptionalPending)
	sort.Strings(view.BlockedReasons)
	if plan.BaselineActive {
		view.BaselineExplain = "starter baseline " + plan.Baseline.PackageID + " is active"
	} else {
		view.BaselineExplain = "distinct approval required: the starter baseline activates only under its pinned release authority (" + plan.Baseline.Approver + ")"
	}
	view.Summary = fmt.Sprintf("%d required steps remaining", view.RequiredRemaining)
	if len(view.OptionalPending) > 0 {
		view.Summary += fmt.Sprintf("; %d optional pending", len(view.OptionalPending))
	}
	if plan.State == SetupStateAbandoned {
		view.AvailableActions = nil
		return view, true
	}
	view.AvailableActions = []string{SetupActionAdvance}
	if len(view.OptionalPending) > 0 {
		view.AvailableActions = append(view.AvailableActions, SetupActionSkip)
	}
	if plan.State == SetupStateDraft || plan.State == SetupStateInProg || plan.State == SetupStateBlocked {
		view.AvailableActions = append(view.AvailableActions, SetupActionResume, SetupActionAbandon)
	}
	return view, true
}

// setupSnapshot is the restart-recovery carrier for one planner.
type setupSnapshot struct {
	Plans   map[string]SetupPlanRevision   `json:"plans"`
	History map[string][]SetupPlanRevision `json:"history"`
	Effects map[string]setupEffect         `json:"effects"`
}

// Export captures the planner state for restart recovery.
func (p *SetupPlanner) Export() ([]byte, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	snapshot := setupSnapshot{
		Plans:   make(map[string]SetupPlanRevision, len(p.plans)),
		History: make(map[string][]SetupPlanRevision, len(p.history)),
		Effects: make(map[string]setupEffect, len(p.effects)),
	}
	for key, plan := range p.plans {
		snapshot.Plans[key] = cloneSetupPlan(*plan)
	}
	for key, chain := range p.history {
		restored := make([]SetupPlanRevision, 0, len(chain))
		for _, rev := range chain {
			restored = append(restored, cloneSetupPlan(rev))
		}
		snapshot.History[key] = restored
	}
	for key, effect := range p.effects {
		snapshot.Effects[key] = effect
	}
	return json.Marshal(snapshot)
}

// Import restores a snapshot previously captured by Export. Revision
// chains are revalidated so corrupt snapshots fail closed instead of
// resurrecting phantom authority.
func (p *SetupPlanner) Import(data []byte) error {
	var snapshot setupSnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return setupErr("SETUP_INVALID_SNAPSHOT", "", "snapshot is not valid setup state", err)
	}
	for key, plan := range snapshot.Plans {
		if plan.PlanID == "" || plan.Scope.TenantID == "" || plan.Revision == 0 {
			return setupErr("SETUP_INVALID_SNAPSHOT", "", "snapshot plan is incomplete", ErrSetupInvalid)
		}
		if key != setupPlanKey(plan.Scope, plan.PlanID) {
			return setupErr("SETUP_INVALID_SNAPSHOT", plan.PlanID, "snapshot key does not match its plan", ErrSetupInvalid)
		}
		chain := snapshot.History[key]
		if len(chain) == 0 {
			return setupErr("SETUP_INVALID_SNAPSHOT", plan.PlanID, "snapshot plan has no revision chain", ErrSetupInvalid)
		}
		for i, rev := range chain {
			if rev.PlanID != plan.PlanID || rev.Revision != uint64(i+1) || rev.Predecessor != uint64(i) {
				return setupErr("SETUP_INVALID_SNAPSHOT", plan.PlanID, "snapshot revision chain is broken", ErrSetupInvalid)
			}
		}
		if chain[len(chain)-1].Revision != plan.Revision {
			return setupErr("SETUP_INVALID_SNAPSHOT", plan.PlanID, "snapshot head does not match its chain", ErrSetupInvalid)
		}
		for id, progress := range plan.StepState {
			_ = id
			switch progress.Status {
			case SetupStepPending, SetupStepBlocked, SetupStepVerified, SetupStepSkipped:
			default:
				return setupErr("SETUP_INVALID_SNAPSHOT", plan.PlanID, "snapshot step state is not declared", ErrSetupInvalid)
			}
		}
	}
	for key, effect := range snapshot.Effects {
		if key == "" || effect.PlanID == "" || effect.StepID == "" || effect.Receipt.ReceiptRef == "" {
			return setupErr("SETUP_INVALID_SNAPSHOT", "", "snapshot idempotency record is incomplete", ErrSetupInvalid)
		}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.plans == nil {
		p.plans = make(map[string]*SetupPlanRevision)
	}
	if p.history == nil {
		p.history = make(map[string][]SetupPlanRevision)
	}
	if p.effects == nil {
		p.effects = make(map[string]setupEffect)
	}
	for key, plan := range snapshot.Plans {
		stored := cloneSetupPlan(plan)
		p.plans[key] = &stored
	}
	for key, chain := range snapshot.History {
		restored := make([]SetupPlanRevision, 0, len(chain))
		for _, rev := range chain {
			restored = append(restored, cloneSetupPlan(rev))
		}
		p.history[key] = restored
	}
	for key, effect := range snapshot.Effects {
		p.effects[key] = effect
	}
	return nil
}
