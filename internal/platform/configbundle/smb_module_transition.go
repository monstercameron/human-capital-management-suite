// Module suspend/re-enable transitions for SMB-SETUP-011.
//
// Disabling an optional business module must preserve unfinished jobs,
// holds, financial obligations and safe corrections; re-enabling must not
// replay old work. This file adds the bounded transition layer over the
// SMB-CORE-001 ModuleRegistry: ModuleTransitionPlan pins the activation
// revision, dependency snapshot and outstanding obligation refs; approval
// binds the exact impact snapshot; apply executes through the registry
// fence (revision compare-and-swap). Domain-owned obligation providers
// inventory reservations, balances and in-flight effects; entered
// physical/payment effects can never be promised cancellation.
//
// The planner is hermetic like the rest of this package: no storage,
// clocks, or network. Restarts restore through Export/Import snapshots;
// durable persistence stays with the existing configregistry stores.
package configbundle

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Stable transition refusal causes. Callers discriminate with errors.Is;
// the *Error Detail carries diagnostics only.
var (
	// ErrTransitionNotFound is the generic, non-revealing refusal for
	// unknown, forged and cross-scope plans and obligation refs.
	ErrTransitionNotFound = errors.New("configbundle: module transition plan is not found")
	// ErrTransitionStale reports an approval or apply whose bound impact
	// snapshot no longer matches the live obligation inventory.
	ErrTransitionStale = errors.New("configbundle: module transition impact is stale")
	// ErrTransitionConflict reports state-machine and readiness
	// violations: double apply, premature approval, unready re-enable.
	ErrTransitionConflict = errors.New("configbundle: module transition state conflict")
	// ErrTransitionBlocked reports execution of a plan whose preview
	// refused the transition (enabled consumers, unsafe dispositions).
	ErrTransitionBlocked = errors.New("configbundle: module transition is blocked")
	// ErrTransitionInvalid reports malformed transition requests and
	// obligation actions outside the provider's allowed dispositions.
	ErrTransitionInvalid = errors.New("configbundle: invalid module transition request")
)

// TransitionKind names the direction of a module transition plan.
type TransitionKind string

const (
	TransitionSuspend  TransitionKind = "suspend"
	TransitionReenable TransitionKind = "reenable"
)

// TransitionState is the lifecycle state of one transition plan.
type TransitionState string

const (
	TransitionPlanned  TransitionState = "planned"
	TransitionWaiting  TransitionState = "waiting"
	TransitionApproved TransitionState = "approved"
	TransitionApplied  TransitionState = "applied"
	TransitionBlocked  TransitionState = "blocked"
)

// ObligationDisposition is one provider-allowed handling of an
// outstanding obligation. Retain keeps reservations and balances;
// drain/cancel apply to queued (never-entered) effects only; quarantine
// holds ambiguous work for explicit review; repair records an
// owning-domain correction such as a reprint.
type ObligationDisposition string

const (
	ObligationRetain     ObligationDisposition = "retain"
	ObligationDrain      ObligationDisposition = "drain"
	ObligationCancel     ObligationDisposition = "cancel"
	ObligationQuarantine ObligationDisposition = "quarantine"
	ObligationRepair     ObligationDisposition = "repair"
)

// EffectClass distinguishes never-entered effects from effects the
// owning domain already entered. Entered physical/payment effects stay
// pending/uncertain until observed; they can never be cancelled.
type EffectClass string

const (
	EffectClassNone    EffectClass = ""
	EffectClassQueued  EffectClass = "queued"
	EffectClassEntered EffectClass = "entered"
)

// ObligationRef is one outstanding obligation owned by a domain: a
// reservation, balance, hold, or in-flight effect touching the module.
// Action and Receipt stay empty until the owning domain reports its
// result receipt; recording a receipt is progress, not impact, so it
// never invalidates a bound approval.
type ObligationRef struct {
	Domain          string                  `json:"domain"`
	Kind            string                  `json:"kind"`
	Ref             string                  `json:"ref"`
	Status          string                  `json:"status"`
	ResponsibleRole string                  `json:"responsible_role"`
	Class           EffectClass             `json:"class,omitempty"`
	Allowed         []ObligationDisposition `json:"allowed,omitempty"`
	Action          string                  `json:"action,omitempty"`
	Receipt         string                  `json:"receipt,omitempty"`
}

// ObligationProvider inventories the outstanding obligations one domain
// owns against a module. Providers are hermetic (no I/O, clocks, or
// network) and deterministic: repeated calls with unchanged domain
// state return identical inventories.
type ObligationProvider interface {
	Domain() string
	Inventory(scope Scope, moduleID string) ([]ObligationRef, error)
}

// TransitionBlocker names one refusal: an enabled consumer module or an
// obligation whose disposition would break a domain invariant. GoToModule
// is the admin-surface deep-link target for the blocking module.
type TransitionBlocker struct {
	ModuleID   string `json:"module_id"`
	Reason     string `json:"reason"`
	GoToModule string `json:"go_to_module"`
}

// ModuleTransitionPlan pins one suspend or re-enable transition. It
// records the activation revision the preview prepared against, the
// dependency/capability snapshot, the inventoried obligations, the
// history/correction access policy, and the impact digest the approval
// binds. RetainedRevisions counts registry history entries preserved
// for authorized reads; Compatibility carries re-enable readiness and
// advisory findings.
type ModuleTransitionPlan struct {
	PlanID            string              `json:"plan_id"`
	Scope             Scope               `json:"scope"`
	ModuleID          string              `json:"module_id"`
	Kind              TransitionKind      `json:"kind"`
	State             TransitionState     `json:"state"`
	Reason            string              `json:"reason"`
	RequestedBy       string              `json:"requested_by"`
	ApprovedBy        string              `json:"approved_by,omitempty"`
	PinnedRevision    uint64              `json:"pinned_revision"`
	Dependencies      []string            `json:"dependencies,omitempty"`
	Capabilities      []string            `json:"capabilities,omitempty"`
	Obligations       []ObligationRef     `json:"obligations,omitempty"`
	HistoryPolicy     string              `json:"history_policy"`
	ImpactDigest      string              `json:"impact_digest"`
	ApprovalDigest    string              `json:"approval_digest,omitempty"`
	Blockers          []TransitionBlocker `json:"blockers,omitempty"`
	AppliedRevision   uint64              `json:"applied_revision,omitempty"`
	RetainedRevisions int                 `json:"retained_revisions"`
	Compatibility     []string            `json:"compatibility,omitempty"`
	Readiness         map[string]bool     `json:"readiness,omitempty"`
}

// TransitionPreviewRequest previews a suspend or re-enable transition.
// SchemaReady maps module IDs to schema/migration readiness; absent
// entries are not ready. It gates re-enable previews into waiting.
type TransitionPreviewRequest struct {
	Scope       Scope
	ModuleID    string
	Kind        TransitionKind
	Reason      string
	RequestedBy string
	SchemaReady map[string]bool
}

// TransitionApproveRequest binds an approval to the exact impact
// snapshot the reviewer saw. SchemaReady re-proves re-enable readiness
// at approval time; suspend approvals ignore it.
type TransitionApproveRequest struct {
	Scope        Scope
	PlanID       string
	ImpactDigest string
	ApprovedBy   string
	SchemaReady  map[string]bool
}

// TransitionApplyRequest executes an approved plan. BundleDigest is the
// re-enable activation digest (sha256:hex); suspend applies ignore it.
type TransitionApplyRequest struct {
	Scope        Scope
	PlanID       string
	BundleDigest string
}

// TransitionOrderRequest proposes a safe multi-module transition order:
// suspend runs dependents before their dependencies, re-enable runs
// dependencies before their dependents.
type TransitionOrderRequest struct {
	Scope     Scope
	ModuleIDs []string
	Kind      TransitionKind
}

// TransitionPlanner previews, approves and applies suspend and re-enable
// transitions over a ModuleRegistry. Its state is the plan table and the
// registered obligation providers; the registry stays the only owner of
// activation revisions.
type TransitionPlanner struct {
	mu        sync.Mutex
	registry  *ModuleRegistry
	manifests map[string]BusinessModuleManifest
	providers map[string]ObligationProvider
	plans     map[string]ModuleTransitionPlan
	counter   uint64
}

// transitionPlannerSnapshot is the restart-recovery carrier for plans.
type transitionPlannerSnapshot struct {
	Plans   map[string]ModuleTransitionPlan `json:"plans"`
	Counter uint64                          `json:"counter"`
}

// transitionImpact is the exact set of facts an approval binds. Recorded
// receipts and actions are deliberately absent: progress never invalidates
// an approval.
type transitionImpact struct {
	ModuleID     string                       `json:"module_id"`
	Kind         TransitionKind               `json:"kind"`
	Pinned       uint64                       `json:"pinned_revision"`
	Dependencies []string                     `json:"dependencies"`
	Capabilities []string                     `json:"capabilities"`
	Obligations  []transitionObligationImpact `json:"obligations"`
	Blockers     []TransitionBlocker          `json:"blockers"`
	Retained     int                          `json:"retained_revisions"`
}

type transitionObligationImpact struct {
	Domain          string                  `json:"domain"`
	Kind            string                  `json:"kind"`
	Ref             string                  `json:"ref"`
	Status          string                  `json:"status"`
	ResponsibleRole string                  `json:"responsible_role"`
	Class           EffectClass             `json:"class"`
	Allowed         []ObligationDisposition `json:"allowed"`
}

// TransitionActivityRow is one line of the review page's activity lists.
type TransitionActivityRow struct {
	Domain    string                  `json:"domain,omitempty"`
	Kind      string                  `json:"kind,omitempty"`
	Reference string                  `json:"reference,omitempty"`
	Status    string                  `json:"status"`
	Actions   []ObligationDisposition `json:"actions,omitempty"`
}

// TransitionHistoryAccess states what stays readable after the transition.
type TransitionHistoryAccess struct {
	HistoryReadable   bool `json:"history_readable"`
	ExportAvailable   bool `json:"export_available"`
	RetainedRevisions int  `json:"retained_revisions"`
}

// TransitionReviewView groups one plan for the review page: new activity
// stopped by the fence, work already in progress, the consumers that block
// a suspension and what history stays available.
type TransitionReviewView struct {
	PlanID            string                  `json:"plan_id"`
	ModuleID          string                  `json:"module_id"`
	Kind              TransitionKind          `json:"kind"`
	State             TransitionState         `json:"state"`
	CompletionSummary string                  `json:"completion_summary"`
	NewActivity       []TransitionActivityRow `json:"new_activity"`
	WorkInProgress    []TransitionActivityRow `json:"work_in_progress"`
	BlockedConsumers  []TransitionBlocker     `json:"blocked_consumers"`
	HistoricalAccess  TransitionHistoryAccess `json:"historical_access"`
}

// NewTransitionPlanner binds a planner to a registry and the manifests it
// was compiled from. Obligation providers register afterwards.
func NewTransitionPlanner(registry *ModuleRegistry, manifests []BusinessModuleManifest) *TransitionPlanner {
	byID := make(map[string]BusinessModuleManifest, len(manifests))
	for _, m := range manifests {
		byID[m.ModuleID] = m
	}
	return &TransitionPlanner{
		registry:  registry,
		manifests: byID,
		providers: make(map[string]ObligationProvider),
		plans:     make(map[string]ModuleTransitionPlan),
	}
}

// RegisterObligationProvider adds one domain's inventory. A provider needs
// a non-empty domain that no other provider already claims.
func (p *TransitionPlanner) RegisterObligationProvider(provider ObligationProvider) error {
	if provider == nil {
		return fmt.Errorf("%w: obligation provider is nil", ErrTransitionInvalid)
	}
	domain := strings.TrimSpace(provider.Domain())
	if domain == "" {
		return fmt.Errorf("%w: obligation provider domain is empty", ErrTransitionInvalid)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, ok := p.providers[domain]; ok {
		return fmt.Errorf("%w: obligation provider %q is already registered", ErrTransitionInvalid, domain)
	}
	p.providers[domain] = provider
	return nil
}

// Preview pins a plan to the active revision, inventories obligations and
// records blockers. Provider failures fail the preview closed.
func (p *TransitionPlanner) Preview(req TransitionPreviewRequest) (ModuleTransitionPlan, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !validTransitionKind(req.Kind) {
		return ModuleTransitionPlan{}, fmt.Errorf("%w: transition kind %q", ErrInvalidRequest, req.Kind)
	}
	if strings.TrimSpace(req.Reason) == "" {
		return ModuleTransitionPlan{}, fmt.Errorf("%w: transition reason is required", ErrInvalidRequest)
	}
	candidate, err := p.assess(req.Scope, req.ModuleID, req.Kind, req.SchemaReady)
	if err != nil {
		return ModuleTransitionPlan{}, err
	}
	p.counter++
	candidate.PlanID = fmt.Sprintf("tr-%06d", p.counter)
	candidate.Reason = req.Reason
	candidate.RequestedBy = req.RequestedBy
	p.plans[planKey(req.Scope, candidate.PlanID)] = candidate
	return cloneTransitionPlan(candidate), nil
}

// Approve binds an approval to the impact the reviewer saw. A changed
// impact is stale; blocked or not-ready plans cannot be approved.
func (p *TransitionPlanner) Approve(req TransitionApproveRequest) (ModuleTransitionPlan, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	plan, ok := p.plans[planKey(req.Scope, req.PlanID)]
	if !ok {
		return ModuleTransitionPlan{}, ErrTransitionNotFound
	}
	if plan.State == TransitionBlocked {
		return ModuleTransitionPlan{}, fmt.Errorf("%w: plan %s is blocked", ErrTransitionBlocked, plan.PlanID)
	}
	if plan.State != TransitionPlanned && plan.State != TransitionWaiting {
		return ModuleTransitionPlan{}, fmt.Errorf("%w: plan %s is %s, not awaiting approval", ErrTransitionConflict, plan.PlanID, plan.State)
	}
	if strings.TrimSpace(req.ApprovedBy) == "" {
		return ModuleTransitionPlan{}, fmt.Errorf("%w: approver is required", ErrInvalidRequest)
	}
	fresh, err := p.assess(req.Scope, plan.ModuleID, plan.Kind, req.SchemaReady)
	if err != nil {
		return ModuleTransitionPlan{}, err
	}
	if fresh.ImpactDigest != plan.ImpactDigest || req.ImpactDigest != plan.ImpactDigest {
		return ModuleTransitionPlan{}, fmt.Errorf("%w: plan %s impact changed since review", ErrTransitionStale, plan.PlanID)
	}
	if fresh.State == TransitionBlocked {
		return ModuleTransitionPlan{}, fmt.Errorf("%w: plan %s is blocked", ErrTransitionBlocked, plan.PlanID)
	}
	if fresh.State == TransitionWaiting {
		return ModuleTransitionPlan{}, fmt.Errorf("%w: module %s is not ready to re-enable: %s", ErrTransitionConflict, plan.ModuleID, strings.Join(fresh.Compatibility, ", "))
	}
	approval, err := transitionDigest(struct {
		Impact     string `json:"impact"`
		ApprovedBy string `json:"approved_by"`
		PlanID     string `json:"plan_id"`
	}{plan.ImpactDigest, req.ApprovedBy, plan.PlanID})
	if err != nil {
		return ModuleTransitionPlan{}, err
	}
	plan.State = TransitionApproved
	plan.ApprovedBy = req.ApprovedBy
	plan.ApprovalDigest = approval
	plan.Compatibility = nil
	if plan.Kind == TransitionReenable {
		plan.Readiness = cloneReadiness(req.SchemaReady)
	}
	p.plans[planKey(req.Scope, plan.PlanID)] = plan
	return cloneTransitionPlan(plan), nil
}

// Apply executes an approved plan through the registry fence. A suspend
// pins the approved revision; a re-enable activates a new revision and
// never replays prior work.
func (p *TransitionPlanner) Apply(req TransitionApplyRequest) (ModuleTransitionPlan, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	plan, ok := p.plans[planKey(req.Scope, req.PlanID)]
	if !ok {
		return ModuleTransitionPlan{}, ErrTransitionNotFound
	}
	switch plan.State {
	case TransitionApproved:
	case TransitionBlocked:
		return ModuleTransitionPlan{}, fmt.Errorf("%w: plan %s is blocked", ErrTransitionBlocked, plan.PlanID)
	default:
		return ModuleTransitionPlan{}, fmt.Errorf("%w: plan %s is %s, not approved", ErrTransitionConflict, plan.PlanID, plan.State)
	}
	fresh, err := p.assess(req.Scope, plan.ModuleID, plan.Kind, plan.Readiness)
	if err != nil {
		return ModuleTransitionPlan{}, err
	}
	if fresh.ImpactDigest != plan.ImpactDigest {
		return ModuleTransitionPlan{}, fmt.Errorf("%w: plan %s impact changed since approval", ErrTransitionStale, plan.PlanID)
	}
	switch plan.Kind {
	case TransitionSuspend:
		rev, err := p.registry.Suspend(req.Scope, plan.ModuleID, plan.Reason, plan.PinnedRevision)
		if err != nil {
			return ModuleTransitionPlan{}, err
		}
		plan.AppliedRevision = rev.Revision
	case TransitionReenable:
		if _, err := digestBytes(req.BundleDigest); err != nil {
			return ModuleTransitionPlan{}, fmt.Errorf("%w: bundle digest must be sha256:hex", ErrTransitionInvalid)
		}
		rev, err := p.registry.Activate(ModuleActivationRequest{
			ModuleID:            plan.ModuleID,
			ExpectedPredecessor: plan.PinnedRevision,
			BundleDigest:        req.BundleDigest,
			Scope:               req.Scope,
			SchemaReady:         plan.Readiness,
		})
		if err != nil {
			return ModuleTransitionPlan{}, err
		}
		plan.AppliedRevision = rev.Revision
	}
	plan.State = TransitionApplied
	p.plans[planKey(req.Scope, plan.PlanID)] = plan
	return cloneTransitionPlan(plan), nil
}

// RecordObligationResult stores an owning domain's disposition receipt on
// one obligation. The action must be one the provider allowed.
func (p *TransitionPlanner) RecordObligationResult(scope Scope, planID, domain, kind, ref, action, receipt string) (ModuleTransitionPlan, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	plan, ok := p.plans[planKey(scope, planID)]
	if !ok {
		return ModuleTransitionPlan{}, ErrTransitionNotFound
	}
	idx := -1
	for i, ob := range plan.Obligations {
		if ob.Domain == domain && ob.Kind == kind && ob.Ref == ref {
			idx = i
			break
		}
	}
	if idx < 0 {
		return ModuleTransitionPlan{}, ErrTransitionNotFound
	}
	disposition := ObligationDisposition(action)
	if !hasDisposition(plan.Obligations[idx].Allowed, disposition) {
		return ModuleTransitionPlan{}, fmt.Errorf("%w: action %q is not allowed for %s", ErrTransitionInvalid, action, ref)
	}
	if strings.TrimSpace(receipt) == "" {
		return ModuleTransitionPlan{}, fmt.Errorf("%w: receipt is required", ErrTransitionInvalid)
	}
	plan.Obligations[idx].Action = action
	plan.Obligations[idx].Receipt = receipt
	p.plans[planKey(scope, planID)] = plan
	return cloneTransitionPlan(plan), nil
}

// PreviewOrder proposes a safe order for several modules. Suspension runs
// dependents first; re-enable runs dependencies first.
func (p *TransitionPlanner) PreviewOrder(req TransitionOrderRequest) ([]string, error) {
	if !validTransitionKind(req.Kind) {
		return nil, fmt.Errorf("%w: transition kind %q", ErrInvalidRequest, req.Kind)
	}
	if len(req.ModuleIDs) == 0 {
		return nil, fmt.Errorf("%w: at least one module is required", ErrInvalidRequest)
	}
	inSet := make(map[string]bool, len(req.ModuleIDs))
	for _, id := range req.ModuleIDs {
		if _, ok := p.manifests[id]; !ok {
			return nil, ErrModuleUnknown
		}
		inSet[id] = true
	}
	indegree := make(map[string]int, len(inSet))
	dependents := make(map[string][]string, len(inSet))
	for id := range inSet {
		indegree[id] = 0
	}
	for id := range inSet {
		for _, dep := range p.manifests[id].Dependencies {
			if inSet[dep] {
				indegree[id]++
				dependents[dep] = append(dependents[dep], id)
			}
		}
	}
	var ready []string
	for id, n := range indegree {
		if n == 0 {
			ready = append(ready, id)
		}
	}
	var order []string
	for len(ready) > 0 {
		sort.Strings(ready)
		next := ready[0]
		ready = ready[1:]
		order = append(order, next)
		for _, dependent := range dependents[next] {
			indegree[dependent]--
			if indegree[dependent] == 0 {
				ready = append(ready, dependent)
			}
		}
	}
	if len(order) != len(inSet) {
		return nil, fmt.Errorf("%w: dependency cycle among requested modules", ErrTransitionConflict)
	}
	if req.Kind == TransitionSuspend {
		for i, j := 0, len(order)-1; i < j; i, j = i+1, j-1 {
			order[i], order[j] = order[j], order[i]
		}
	}
	return order, nil
}

// Load returns a copy of one plan visible in the scope.
func (p *TransitionPlanner) Load(scope Scope, planID string) (ModuleTransitionPlan, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	plan, ok := p.plans[planKey(scope, planID)]
	if !ok {
		return ModuleTransitionPlan{}, false
	}
	return cloneTransitionPlan(plan), true
}

// Export captures every plan and the id counter for restart recovery.
func (p *TransitionPlanner) Export() ([]byte, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	snapshot := transitionPlannerSnapshot{Plans: make(map[string]ModuleTransitionPlan, len(p.plans)), Counter: p.counter}
	for key, plan := range p.plans {
		snapshot.Plans[key] = cloneTransitionPlan(plan)
	}
	return json.Marshal(snapshot)
}

// Import restores plans from an Export snapshot. Every plan must be filed
// under its own scope and plan id, carry a known state, and reproduce its
// recorded impact digest; any mismatch refuses the whole snapshot.
func (p *TransitionPlanner) Import(data []byte) error {
	var snapshot transitionPlannerSnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return fmt.Errorf("%w: decode planner snapshot: %v", ErrTransitionInvalid, err)
	}
	plans := make(map[string]ModuleTransitionPlan, len(snapshot.Plans))
	for key, stored := range snapshot.Plans {
		if key != planKey(stored.Scope, stored.PlanID) {
			return fmt.Errorf("%w: plan %s is filed under the wrong scope", ErrTransitionInvalid, stored.PlanID)
		}
		if _, ok := p.manifests[stored.ModuleID]; !ok {
			return fmt.Errorf("%w: plan %s names an unknown module", ErrTransitionInvalid, stored.PlanID)
		}
		if !validTransitionKind(stored.Kind) || !validTransitionState(stored.State) {
			return fmt.Errorf("%w: plan %s has an invalid kind or state", ErrTransitionInvalid, stored.PlanID)
		}
		recomputed, err := transitionDigest(transitionImpactOf(stored))
		if err != nil {
			return err
		}
		if stored.ImpactDigest != recomputed {
			return fmt.Errorf("%w: plan %s impact digest does not reproduce", ErrTransitionInvalid, stored.PlanID)
		}
		plans[key] = cloneTransitionPlan(stored)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.plans = plans
	p.counter = snapshot.Counter
	return nil
}

// assess builds the candidate plan for one module and kind from the live
// registry and obligation providers. It is shared by preview, approval and
// apply, so a changed impact always shows up as a digest mismatch.
func (p *TransitionPlanner) assess(scope Scope, moduleID string, kind TransitionKind, schemaReady map[string]bool) (ModuleTransitionPlan, error) {
	manifest, ok := p.manifests[moduleID]
	if !ok {
		return ModuleTransitionPlan{}, moduleErr("MODULE_UNKNOWN", moduleID, "module is not registered", ErrModuleUnknown)
	}
	view := p.registry.ResolveView(scope, moduleID, nil)
	if view.ActiveRevision == 0 {
		return ModuleTransitionPlan{}, moduleErr("MODULE_NOT_ACTIVE", moduleID, "module is not active", ErrModuleNotActive)
	}
	switch kind {
	case TransitionSuspend:
		if view.EffectiveStatus != string(ModuleStateEnabled) {
			return ModuleTransitionPlan{}, moduleErr("MODULE_REVISION_CONFLICT", moduleID,
				fmt.Sprintf("module is %s, only enabled modules suspend", view.EffectiveStatus), ErrTransitionConflict)
		}
	case TransitionReenable:
		if view.EffectiveStatus != string(ModuleStateSuspended) {
			return ModuleTransitionPlan{}, moduleErr("MODULE_REVISION_CONFLICT", moduleID,
				fmt.Sprintf("module is %s, only suspended modules re-enable", view.EffectiveStatus), ErrTransitionConflict)
		}
	default:
		return ModuleTransitionPlan{}, fmt.Errorf("%w: transition kind %q", ErrInvalidRequest, kind)
	}
	obligations, err := p.inventory(scope, moduleID)
	if err != nil {
		return ModuleTransitionPlan{}, err
	}
	blockers := p.blockersFor(scope, manifest, kind, obligations)
	var compatibility []string
	if kind == TransitionReenable {
		compatibility = p.registry.missingReadiness(manifest, schemaReady)
	}
	state := TransitionPlanned
	switch {
	case len(blockers) > 0:
		state = TransitionBlocked
	case len(compatibility) > 0:
		state = TransitionWaiting
	}
	history := len(p.registry.History(scope, moduleID))
	candidate := ModuleTransitionPlan{
		Scope:             scope,
		ModuleID:          moduleID,
		Kind:              kind,
		State:             state,
		PinnedRevision:    view.ActiveRevision,
		Dependencies:      sortedCopy(manifest.Dependencies),
		Capabilities:      sortedCopy(manifest.Capabilities),
		Obligations:       obligations,
		Blockers:          blockers,
		HistoryPolicy:     ModuleHistoryNone,
		RetainedRevisions: history,
		Compatibility:     compatibility,
	}
	if history > 0 {
		candidate.HistoryPolicy = ModuleHistoryPreserved
	}
	digest, err := transitionDigest(transitionImpactOf(candidate))
	if err != nil {
		return ModuleTransitionPlan{}, err
	}
	candidate.ImpactDigest = digest
	return candidate, nil
}

// inventory asks every registered provider, in domain order, for its
// outstanding obligations. Any provider failure aborts the whole inventory.
func (p *TransitionPlanner) inventory(scope Scope, moduleID string) ([]ObligationRef, error) {
	domains := make([]string, 0, len(p.providers))
	for domain := range p.providers {
		domains = append(domains, domain)
	}
	sort.Strings(domains)
	var out []ObligationRef
	for _, domain := range domains {
		rows, err := p.providers[domain].Inventory(scope, moduleID)
		if err != nil {
			return nil, fmt.Errorf("configbundle: obligation provider %s failed closed: %w", domain, err)
		}
		for _, row := range rows {
			row.Allowed = append([]ObligationDisposition(nil), row.Allowed...)
			out = append(out, row)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Domain != out[j].Domain {
			return out[i].Domain < out[j].Domain
		}
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].Ref < out[j].Ref
	})
	return out, nil
}

// blockersFor names what stops the transition: enabled consumers of a
// module being suspended, dependencies that are not enabled before a
// re-enable, entered effects that were promised cancellation, and
// obligations with no permitted disposition.
func (p *TransitionPlanner) blockersFor(scope Scope, manifest BusinessModuleManifest, kind TransitionKind, obligations []ObligationRef) []TransitionBlocker {
	var blockers []TransitionBlocker
	if kind == TransitionSuspend {
		for _, id := range sortedManifestIDs(p.manifests) {
			consumer := p.manifests[id]
			if id == manifest.ModuleID || !containsString(consumer.Dependencies, manifest.ModuleID) {
				continue
			}
			if p.registry.ResolveView(scope, id, nil).EffectiveStatus != string(ModuleStateEnabled) {
				continue
			}
			blockers = append(blockers, TransitionBlocker{
				ModuleID:   id,
				Reason:     fmt.Sprintf("enabled module %s consumes %s", id, manifest.ModuleID),
				GoToModule: id,
			})
		}
	} else {
		for _, dep := range sortedCopy(manifest.Dependencies) {
			if p.registry.ResolveView(scope, dep, nil).EffectiveStatus != string(ModuleStateEnabled) {
				blockers = append(blockers, TransitionBlocker{
					ModuleID:   dep,
					Reason:     fmt.Sprintf("dependency %s is not enabled", dep),
					GoToModule: dep,
				})
			}
		}
	}
	for _, ob := range obligations {
		if ob.Class == EffectClassEntered && hasDisposition(ob.Allowed, ObligationCancel) {
			blockers = append(blockers, TransitionBlocker{
				ModuleID: manifest.ModuleID,
				Reason:   fmt.Sprintf("entered effect %s cannot be promised cancellation", ob.Ref),
			})
		}
		if len(ob.Allowed) == 0 {
			blockers = append(blockers, TransitionBlocker{
				ModuleID: manifest.ModuleID,
				Reason:   fmt.Sprintf("obligation %s offers no permitted disposition", ob.Ref),
			})
		}
	}
	return blockers
}

// TransitionReview derives the review page contract from one plan.
func TransitionReview(plan ModuleTransitionPlan) TransitionReviewView {
	view := TransitionReviewView{
		PlanID:            plan.PlanID,
		ModuleID:          plan.ModuleID,
		Kind:              plan.Kind,
		State:             plan.State,
		CompletionSummary: transitionCompletionSummary(plan),
		NewActivity: []TransitionActivityRow{{
			Status: "New activity stopped at the transition fence",
		}},
		BlockedConsumers: []TransitionBlocker{},
		HistoricalAccess: TransitionHistoryAccess{
			HistoryReadable:   plan.RetainedRevisions > 0,
			ExportAvailable:   plan.RetainedRevisions > 0,
			RetainedRevisions: plan.RetainedRevisions,
		},
	}
	for _, ob := range plan.Obligations {
		row := TransitionActivityRow{
			Domain:    ob.Domain,
			Kind:      ob.Kind,
			Reference: ob.Ref,
			Status:    ob.Status,
			Actions:   append([]ObligationDisposition(nil), ob.Allowed...),
		}
		if ob.Class == EffectClassQueued {
			view.NewActivity = append(view.NewActivity, row)
		} else {
			view.WorkInProgress = append(view.WorkInProgress, row)
		}
	}
	for _, blocker := range plan.Blockers {
		if blocker.GoToModule != "" {
			view.BlockedConsumers = append(view.BlockedConsumers, blocker)
		}
	}
	return view
}

func transitionCompletionSummary(plan ModuleTransitionPlan) string {
	switch plan.State {
	case TransitionApplied:
		if plan.Kind == TransitionSuspend {
			return fmt.Sprintf("New activity stopped at revision %d. Outstanding obligations were retained or left for their owning domain to resolve; none were cancelled automatically.", plan.AppliedRevision)
		}
		return fmt.Sprintf("Module resumed as revision %d. Prior work was not replayed.", plan.AppliedRevision)
	case TransitionBlocked:
		return "Blocked: resolve the named consumers or entered effects before suspending."
	case TransitionWaiting:
		return "Waiting for schema readiness before this module can be re-enabled."
	case TransitionApproved:
		return "Approved and waiting to apply."
	default:
		return "Preview ready for review; no change has been applied."
	}
}

func impactOrEmpty(v []string) []string {
	if v == nil {
		return []string{}
	}
	return v
}

// transitionImpactOf projects a plan onto the facts an approval binds.
func transitionImpactOf(plan ModuleTransitionPlan) transitionImpact {
	impact := transitionImpact{
		ModuleID:     plan.ModuleID,
		Kind:         plan.Kind,
		Pinned:       plan.PinnedRevision,
		Dependencies: impactOrEmpty(plan.Dependencies),
		Capabilities: impactOrEmpty(plan.Capabilities),
		Obligations:  make([]transitionObligationImpact, 0, len(plan.Obligations)),
		Blockers:     append([]TransitionBlocker{}, plan.Blockers...),
		Retained:     plan.RetainedRevisions,
	}
	for _, ob := range plan.Obligations {
		impact.Obligations = append(impact.Obligations, transitionObligationImpact{
			Domain:          ob.Domain,
			Kind:            ob.Kind,
			Ref:             ob.Ref,
			Status:          ob.Status,
			ResponsibleRole: ob.ResponsibleRole,
			Class:           ob.Class,
			Allowed:         append([]ObligationDisposition{}, ob.Allowed...),
		})
	}
	return impact
}

// transitionDigest hashes the canonical JSON of v as sha256:hex.
func transitionDigest(v any) (string, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("configbundle: canonicalise transition: %w", err)
	}
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func planKey(scope Scope, planID string) string {
	return moduleScopeKey(scope) + "\x00" + planID
}

func validTransitionKind(kind TransitionKind) bool {
	return kind == TransitionSuspend || kind == TransitionReenable
}

func validTransitionState(state TransitionState) bool {
	switch state {
	case TransitionPlanned, TransitionWaiting, TransitionApproved, TransitionApplied, TransitionBlocked:
		return true
	}
	return false
}

func hasDisposition(allowed []ObligationDisposition, want ObligationDisposition) bool {
	for _, d := range allowed {
		if d == want {
			return true
		}
	}
	return false
}

func sortedCopy(values []string) []string {
	out := append([]string(nil), values...)
	sort.Strings(out)
	return out
}

func sortedManifestIDs(manifests map[string]BusinessModuleManifest) []string {
	ids := make([]string, 0, len(manifests))
	for id := range manifests {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func cloneReadiness(in map[string]bool) map[string]bool {
	if in == nil {
		return nil
	}
	out := make(map[string]bool, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func cloneTransitionPlan(plan ModuleTransitionPlan) ModuleTransitionPlan {
	out := plan
	out.Dependencies = append([]string(nil), plan.Dependencies...)
	out.Capabilities = append([]string(nil), plan.Capabilities...)
	out.Compatibility = append([]string(nil), plan.Compatibility...)
	out.Blockers = append([]TransitionBlocker(nil), plan.Blockers...)
	out.Obligations = make([]ObligationRef, len(plan.Obligations))
	for i, ob := range plan.Obligations {
		ob.Allowed = append([]ObligationDisposition(nil), ob.Allowed...)
		out.Obligations[i] = ob
	}
	out.Readiness = cloneReadiness(plan.Readiness)
	return out
}
