// Optional business starter templates for SMB-SETUP-003.
//
// A StarterProfileManifest pins the modules, workflow versions, terminology,
// roles, forms and settings one starter template proposes. Compile proves
// the pins against a finite catalog; missing references keep the profile
// Planned, and a compiler proof never marks a profile production Ready.
//
// Application is idempotent and override-preserving: the same key yields one
// revision, upgrades create draft diffs, and tenant overrides win by default
// (Keep current) unless explicitly switched to Replace. Publish enforces
// tenant mandatory restrictions through canonical setting identities —
// localized aliases never change what is enforced — and activates only under
// a distinct valid approval through the promotion control plane reused from
// SMB-SETUP-001. Execution always resolves through the existing versioned
// workflow runtime; profiles carry pins, never an executor.
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
	"time"

	wfversion "github.com/monstercameron/human-capital-management-suite/internal/workflow/version"
)

// Stable starter-profile refusal causes.
var (
	ErrStarterInvalid             = errors.New("configbundle: invalid starter profile request")
	ErrStarterNotFound            = errors.New("configbundle: starter adoption is not visible")
	ErrStarterRevisionConflict    = errors.New("configbundle: starter adoption revision conflict")
	ErrStarterPolicy              = errors.New("configbundle: starter profile violates a tenant mandatory restriction")
	ErrStarterRefMissing          = errors.New("configbundle: starter profile reference is not available")
	ErrStarterIdempotencyConflict = errors.New("configbundle: starter idempotency key was reused for another adoption")
	ErrStarterResponseLost        = errors.New("configbundle: starter adoption recorded but response lost")
	ErrStarterSnapshotInvalid     = errors.New("configbundle: starter snapshot is invalid")
)

// StarterProfileStatus is the compiler verdict. Available means every pin
// resolved against the finite catalog; Planned names what is missing.
// Neither claims production readiness.
type StarterProfileStatus string

const (
	StarterProfileAvailable StarterProfileStatus = "available"
	StarterProfilePlanned   StarterProfileStatus = "planned"
)

// StarterAdoptionStatus is the adoption lifecycle.
type StarterAdoptionStatus string

const (
	StarterAdoptionDraft     StarterAdoptionStatus = "draft"
	StarterAdoptionActive    StarterAdoptionStatus = "active"
	StarterAdoptionBlocked   StarterAdoptionStatus = "blocked"
	StarterAdoptionDiscarded StarterAdoptionStatus = "discarded"
)

// Starter diff decisions. Keep current is the default for existing values;
// Replace requires explicit selection with the previous value visible.
const (
	StarterDiffKeepCurrent = "keep-current"
	StarterDiffReplace     = "replace"
)

// Served starter-view vocabulary.
const (
	StarterActionApply   = "Apply to draft"
	StarterActionPublish = "Publish"
	StarterActionDiscard = "Discard draft"
	StarterActionUpgrade = "Upgrade"
	StarterActionReview  = "Review again"
)

// StarterWorkflowRef pins one existing workflow version. There is no
// "latest": the digest names the exact governed artifact.
type StarterWorkflowRef struct {
	WorkflowID         string `json:"workflow_id"`
	CompiledPlanDigest string `json:"compiled_plan_digest"`
}

// StarterRole recommends one tenant role. Recommendations never grant
// anything; the tenant selects or keeps a safe baseline.
type StarterRole struct {
	Role             string `json:"role"`
	ApprovalRequired bool   `json:"approval_required"`
}

// StarterProfileManifest is one versioned starter template.
type StarterProfileManifest struct {
	ProfileID      string               `json:"profile_id"`
	Version        string               `json:"version"`
	Modules        []string             `json:"modules"`
	Workflows      []StarterWorkflowRef `json:"workflows"`
	Terminology    []string             `json:"terminology"`
	Roles          []StarterRole        `json:"roles"`
	Forms          []string             `json:"forms"`
	RequiredConfig []string             `json:"required_config"`
	Settings       map[string]string    `json:"settings"`
}

// StarterCatalog is the finite registry set Compile proves manifests
// against. It is test scope, never a production readiness claim.
type StarterCatalog struct {
	Modules     *ModuleRegistry
	Workflows   wfversion.Store
	Terminology map[string]bool
}

// CompiledStarterProfile is one compiler verdict with its exact digest.
type CompiledStarterProfile struct {
	Manifest        StarterProfileManifest `json:"manifest"`
	Digest          string                 `json:"digest"`
	Status          StarterProfileStatus   `json:"status"`
	MissingRefs     []string               `json:"missing_refs,omitempty"`
	Reasons         []string               `json:"reasons,omitempty"`
	ProductionReady bool                   `json:"production_ready"`
}

// SettingDiff is one exact settings change with its decision.
type SettingDiff struct {
	Key      string `json:"key"`
	Previous string `json:"previous"`
	Proposed string `json:"proposed"`
	Decision string `json:"decision"`
}

// StarterAdoptionRequest applies one compiled profile to a tenant.
type StarterAdoptionRequest struct {
	Actor          string
	AdoptionID     string
	Scope          Scope
	ProfileID      string
	CompiledDigest string
	Baseline       BaselinePin
	Overrides      map[string]string
	Replace        []string
	IdempotencyKey string
	At             time.Time
}

// StarterUpgradeRequest moves an adoption to a newer compiled digest. The
// result is always a draft diff, never a silent replacement.
type StarterUpgradeRequest struct {
	Actor            string
	AdoptionID       string
	Scope            Scope
	NewDigest        string
	Replace          []string
	IdempotencyKey   string
	ExpectedRevision uint64
	At               time.Time
}

// StarterAdoptionRevision is one adoption revision: exact digest, preserved
// overrides, effective settings and the diff that produced them.
type StarterAdoptionRevision struct {
	AdoptionID     string                `json:"adoption_id"`
	Scope          Scope                 `json:"scope"`
	ProfileID      string                `json:"profile_id"`
	Digest         string                `json:"digest"`
	Revision       uint64                `json:"revision"`
	Predecessor    uint64                `json:"predecessor"`
	Status         StarterAdoptionStatus `json:"status"`
	Admins         []string              `json:"admins"`
	Baseline       BaselinePin           `json:"baseline"`
	BaselineActive bool                  `json:"baseline_active"`
	Overrides      map[string]string     `json:"overrides"`
	Effective      map[string]string     `json:"effective"`
	EffectiveDiff  []SettingDiff         `json:"effective_diff"`
	BlockedReason  string                `json:"blocked_reason,omitempty"`
}

// EffectiveSettings returns a copy of the adopted effective settings.
func (r StarterAdoptionRevision) EffectiveSettings() map[string]string {
	out := make(map[string]string, len(r.Effective))
	for k, v := range r.Effective {
		out[k] = v
	}
	return out
}

// StarterProfileCard is the How-you-work profile card.
type StarterProfileCard struct {
	ProfileID       string
	DailyWorkflow   string
	RequiredModules []string
	SetupSteps      []string
	Readiness       StarterProfileStatus
	ReadinessNote   string
	Reasons         []string
	Actions         []string
}

// StarterAdoptionView is the served adoption state.
type StarterAdoptionView struct {
	AdoptionID    string
	Status        StarterAdoptionStatus
	Revision      uint64
	Diff          []SettingDiff
	Actions       []string
	BlockedReason string
	ApprovalNote  string
}

type starterEffect struct {
	AdoptionID string `json:"adoption_id"`
	ProfileID  string `json:"profile_id"`
	Digest     string `json:"digest"`
	Revision   uint64 `json:"revision"`
}

// StarterProfileRegistry compiles starter manifests and hosts tenant
// adoptions. It reuses the setup planner's promotion control plane for
// baseline approval checks and the versioned workflow runtime for pins.
type StarterProfileRegistry struct {
	mu        sync.Mutex
	planner   *SetupPlanner
	catalog   StarterCatalog
	byDigest  map[string]CompiledStarterProfile
	latest    map[string]CompiledStarterProfile
	adoptions map[string]*StarterAdoptionRevision
	history   map[string][]StarterAdoptionRevision
	effects   map[string]starterEffect
	mandatory map[string]map[string]string
	aliases   map[string]string
	// AfterRecord is a fault-injection seam: when non-nil it runs after an
	// adoption record is stored but before Apply responds.
	AfterRecord func() error
}

// NewStarterProfileRegistry builds the starter port over a setup planner and
// a finite catalog.
func NewStarterProfileRegistry(planner *SetupPlanner, catalog StarterCatalog) *StarterProfileRegistry {
	return &StarterProfileRegistry{
		planner: planner, catalog: catalog,
		byDigest: make(map[string]CompiledStarterProfile), latest: make(map[string]CompiledStarterProfile),
		adoptions: make(map[string]*StarterAdoptionRevision), history: make(map[string][]StarterAdoptionRevision),
		effects: make(map[string]starterEffect), mandatory: make(map[string]map[string]string),
		aliases: make(map[string]string),
	}
}

func starterErr(code, ref, detail string, cause error) *Error {
	return &Error{Code: code, Ref: ref, Detail: detail, Cause: cause}
}

func starterAdoptionKey(scope Scope, adoptionID string) string {
	return scope.TenantID + "\x00" + scope.CellID + "\x00" + adoptionID
}

// SetTenantMandatory records the tenant restrictions no profile or alias may
// remove. It is hermetic test scope; production policy comes from the tenant
// policy owner through the same canonical identities.
func (r *StarterProfileRegistry) SetTenantMandatory(tenant string, restrictions map[string]string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	kept := make(map[string]string, len(restrictions))
	for k, v := range restrictions {
		kept[k] = v
	}
	r.mandatory[tenant] = kept
}

// SetAliases records localized presentation labels. Aliases resolve to
// canonical identities before any diff, policy or adoption decision.
func (r *StarterProfileRegistry) SetAliases(aliases map[string]string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for k, v := range aliases {
		r.aliases[k] = v
	}
}

// Workflows exposes the catalog's versioned workflow store so callers prove
// pins resolve through the existing runtime.
func (r *StarterProfileRegistry) Workflows() wfversion.Store { return r.catalog.Workflows }

// Compiled returns the latest compiled profile under an ID, or the zero
// value when the profile never compiled.
func (r *StarterProfileRegistry) Compiled(profileID string) CompiledStarterProfile {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.latest[profileID]
}

// HistoryLength counts recorded adoption revisions.
func (r *StarterProfileRegistry) HistoryLength(scope Scope, adoptionID string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.history[starterAdoptionKey(scope, adoptionID)])
}

func starterDigest(manifest StarterProfileManifest) string {
	raw, _ := json.Marshal(manifest)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// Compile proves manifests against the finite catalog. Unresolvable module,
// workflow or terminology references keep the profile Planned with reasons;
// the compiler never marks a profile production Ready.
func (r *StarterProfileRegistry) Compile(manifests []StarterProfileManifest) ([]CompiledStarterProfile, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]CompiledStarterProfile, 0, len(manifests))
	for _, manifest := range manifests {
		if strings.TrimSpace(manifest.ProfileID) == "" || strings.TrimSpace(manifest.Version) == "" {
			return nil, starterErr("STARTER_INVALID_MANIFEST", "", "profile id and version are required", ErrStarterInvalid)
		}
		compiled := CompiledStarterProfile{Manifest: manifest, Digest: starterDigest(manifest), Status: StarterProfileAvailable}
		if r.catalog.Modules == nil {
			return nil, starterErr("STARTER_NO_MODULES", manifest.ProfileID, "no compiled module set is configured", ErrStarterRefMissing)
		}
		for _, module := range manifest.Modules {
			if view := r.catalog.Modules.ResolveView(Scope{}, module, nil); view.EffectiveStatus == ModuleStatusUnknown {
				compiled.Status = StarterProfilePlanned
				compiled.MissingRefs = append(compiled.MissingRefs, module)
				compiled.Reasons = append(compiled.Reasons, fmt.Sprintf("module %q is not registered", module))
			}
		}
		if r.catalog.Workflows == nil {
			return nil, starterErr("STARTER_NO_WORKFLOWS", manifest.ProfileID, "no workflow version store is configured", ErrStarterRefMissing)
		}
		for _, ref := range manifest.Workflows {
			if _, err := wfversion.Resolve(r.catalog.Workflows, ref.WorkflowID, wfversion.Pin{CompiledPlanDigest: ref.CompiledPlanDigest}); err != nil {
				compiled.Status = StarterProfilePlanned
				compiled.MissingRefs = append(compiled.MissingRefs, ref.WorkflowID)
				compiled.Reasons = append(compiled.Reasons, fmt.Sprintf("workflow %q has no published version %q", ref.WorkflowID, ref.CompiledPlanDigest))
			}
		}
		for _, term := range manifest.Terminology {
			if !r.catalog.Terminology[term] {
				compiled.Status = StarterProfilePlanned
				compiled.MissingRefs = append(compiled.MissingRefs, term)
				compiled.Reasons = append(compiled.Reasons, fmt.Sprintf("terminology token %q is not admitted", term))
			}
		}
		sort.Strings(compiled.MissingRefs)
		r.byDigest[compiled.Digest] = compiled
		r.latest[manifest.ProfileID] = compiled
		out = append(out, compiled)
	}
	return out, nil
}

func (r *StarterProfileRegistry) canonicalLocked(key string) string {
	if canonical, ok := r.aliases[key]; ok {
		return canonical
	}
	return key
}

func cloneStarterRevision(in StarterAdoptionRevision) StarterAdoptionRevision {
	in.Admins = append([]string(nil), in.Admins...)
	overrides := make(map[string]string, len(in.Overrides))
	for k, v := range in.Overrides {
		overrides[k] = v
	}
	in.Overrides = overrides
	effective := make(map[string]string, len(in.Effective))
	for k, v := range in.Effective {
		effective[k] = v
	}
	in.Effective = effective
	in.EffectiveDiff = append([]SettingDiff(nil), in.EffectiveDiff...)
	return in
}

func (r *StarterProfileRegistry) visibleLocked(scope Scope, adoptionID, actor string) (*StarterAdoptionRevision, bool) {
	adoption, ok := r.adoptions[starterAdoptionKey(scope, adoptionID)]
	if !ok || adoption.Scope.TenantID != scope.TenantID {
		return nil, false
	}
	for _, admin := range adoption.Admins {
		if admin == actor {
			return adoption, true
		}
	}
	return nil, false
}

// diffSettings computes the effective settings and their exact diff.
// Overrides and prior values win by default (keep-current); only keys named
// in replace switch to the proposed value, always with the previous visible.
func (r *StarterProfileRegistry) diffSettingsLocked(proposed map[string]string, overrides map[string]string, prior map[string]string, replace []string) (map[string]string, []SettingDiff) {
	resolvedProposed := make(map[string]string, len(proposed))
	for k, v := range proposed {
		resolvedProposed[r.canonicalLocked(k)] = v
	}
	resolvedOverrides := make(map[string]string, len(overrides))
	for k, v := range overrides {
		resolvedOverrides[r.canonicalLocked(k)] = v
	}
	keys := make(map[string]bool, len(resolvedProposed)+len(resolvedOverrides)+len(prior))
	for k := range resolvedProposed {
		keys[k] = true
	}
	for k := range resolvedOverrides {
		keys[k] = true
	}
	for k := range prior {
		keys[k] = true
	}
	effective := make(map[string]string, len(keys))
	var diff []SettingDiff
	for key := range keys {
		previous, hadPrior := prior[key]
		if !hadPrior {
			if override, ok := resolvedOverrides[key]; ok {
				previous = override
			}
		}
		prop := resolvedProposed[key]
		decision := StarterDiffReplace
		value := prop
		if containsString(replace, key) || containsString(replace, r.canonicalLocked(key)) {
			value = prop
		} else if override, ok := resolvedOverrides[key]; ok {
			decision = StarterDiffKeepCurrent
			value = override
		} else if hadPrior && prior[key] != prop {
			decision = StarterDiffKeepCurrent
			value = prior[key]
		}
		if previous == "" && decision == StarterDiffKeepCurrent {
			decision = StarterDiffReplace
		}
		effective[key] = value
		diff = append(diff, SettingDiff{Key: key, Previous: previous, Proposed: prop, Decision: decision})
	}
	sort.Slice(diff, func(i, j int) bool { return diff[i].Key < diff[j].Key })
	return effective, diff
}

// Apply adopts one compiled profile. The same idempotency key replays the
// recorded revision; the same key on another adoption conflicts. Baseline
// approval is verified at Publish, never weaker and never earlier than the
// activation gate it guards.
func (r *StarterProfileRegistry) Apply(req StarterAdoptionRequest) (StarterAdoptionRevision, error) {
	if strings.TrimSpace(req.Actor) == "" || strings.TrimSpace(req.AdoptionID) == "" || strings.TrimSpace(req.ProfileID) == "" || strings.TrimSpace(req.CompiledDigest) == "" || strings.TrimSpace(req.IdempotencyKey) == "" {
		return StarterAdoptionRevision{}, starterErr("STARTER_INVALID_APPLY", req.AdoptionID, "actor, adoption, profile, digest, and idempotency key are required", ErrStarterInvalid)
	}
	if req.Scope.TenantID == "" {
		return StarterAdoptionRevision{}, starterErr("STARTER_NO_TENANT", req.AdoptionID, "adoption works on an existing tenant and never creates one", ErrStarterInvalid)
	}
	if req.At.IsZero() {
		return StarterAdoptionRevision{}, starterErr("STARTER_NO_TIME", req.AdoptionID, "adoption time is required", ErrStarterInvalid)
	}
	if req.Baseline.Scope.TenantID != req.Scope.TenantID {
		// A pin scoped to another tenant looks exactly like a missing
		// adoption: the caller learns nothing about either tenant.
		return StarterAdoptionRevision{}, starterErr("STARTER_NOT_FOUND", req.AdoptionID, "starter adoption is not visible", ErrStarterNotFound)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if prior, ok := r.effects[req.IdempotencyKey]; ok {
		if prior.AdoptionID != req.AdoptionID || prior.ProfileID != req.ProfileID || prior.Digest != req.CompiledDigest {
			return StarterAdoptionRevision{}, starterErr("STARTER_IDEMPOTENCY_CONFLICT", req.AdoptionID, "idempotency key was reused for another adoption", ErrStarterIdempotencyConflict)
		}
		adoption, visible := r.visibleLocked(req.Scope, req.AdoptionID, req.Actor)
		if !visible {
			return StarterAdoptionRevision{}, starterErr("STARTER_NOT_FOUND", req.AdoptionID, "starter adoption is not visible", ErrStarterNotFound)
		}
		return cloneStarterRevision(*adoption), nil
	}
	compiled, ok := r.byDigest[req.CompiledDigest]
	if !ok || compiled.Manifest.ProfileID != req.ProfileID {
		return StarterAdoptionRevision{}, starterErr("STARTER_UNKNOWN_DIGEST", req.AdoptionID, "compiled profile digest is not known", ErrStarterRefMissing)
	}
	if compiled.Status != StarterProfileAvailable {
		return StarterAdoptionRevision{}, starterErr("STARTER_PLANNED", req.AdoptionID, "profile is planned with unresolved references: "+strings.Join(compiled.MissingRefs, ", "), ErrStarterRefMissing)
	}
	key := starterAdoptionKey(req.Scope, req.AdoptionID)
	if _, dup := r.adoptions[key]; dup {
		return StarterAdoptionRevision{}, starterErr("STARTER_DUPLICATE_ADOPTION", req.AdoptionID, "adoption id already exists", ErrStarterInvalid)
	}
	for _, existing := range r.adoptions {
		if existing.AdoptionID == req.AdoptionID {
			return StarterAdoptionRevision{}, starterErr("STARTER_DUPLICATE_ADOPTION", req.AdoptionID, "adoption id already exists", ErrStarterInvalid)
		}
	}
	overrides := make(map[string]string, len(req.Overrides))
	for k, v := range req.Overrides {
		overrides[r.canonicalLocked(k)] = v
	}
	effective, diff := r.diffSettingsLocked(compiled.Manifest.Settings, overrides, nil, req.Replace)
	adoption := StarterAdoptionRevision{
		AdoptionID: req.AdoptionID, Scope: req.Scope, ProfileID: req.ProfileID, Digest: req.CompiledDigest,
		Revision: 1, Status: StarterAdoptionDraft, Admins: []string{req.Actor}, Baseline: req.Baseline,
		Overrides: overrides, Effective: effective, EffectiveDiff: diff,
	}
	r.adoptions[key] = &adoption
	r.history[key] = []StarterAdoptionRevision{cloneStarterRevision(adoption)}
	r.effects[req.IdempotencyKey] = starterEffect{AdoptionID: req.AdoptionID, ProfileID: req.ProfileID, Digest: req.CompiledDigest, Revision: 1}
	out := cloneStarterRevision(adoption)
	if r.AfterRecord != nil {
		if crash := r.AfterRecord(); crash != nil {
			return StarterAdoptionRevision{}, starterErr("STARTER_RESPONSE_LOST", req.AdoptionID, "starter adoption recorded but the response was lost", fmt.Errorf("%w: %w", ErrStarterResponseLost, crash))
		}
	}
	return out, nil
}

// Upgrade moves an adoption to a newer compiled digest as a draft diff.
// Overrides carry forward and live workflow versions never swap silently.
func (r *StarterProfileRegistry) Upgrade(req StarterUpgradeRequest) (StarterAdoptionRevision, error) {
	if strings.TrimSpace(req.Actor) == "" || strings.TrimSpace(req.AdoptionID) == "" || strings.TrimSpace(req.NewDigest) == "" || strings.TrimSpace(req.IdempotencyKey) == "" {
		return StarterAdoptionRevision{}, starterErr("STARTER_INVALID_UPGRADE", req.AdoptionID, "actor, adoption, digest, and idempotency key are required", ErrStarterInvalid)
	}
	if req.Scope.TenantID == "" || req.At.IsZero() {
		return StarterAdoptionRevision{}, starterErr("STARTER_INVALID_UPGRADE", req.AdoptionID, "tenant scope and time are required", ErrStarterInvalid)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if prior, ok := r.effects[req.IdempotencyKey]; ok {
		adoption, visible := r.visibleLocked(req.Scope, req.AdoptionID, req.Actor)
		if !visible || prior.AdoptionID != req.AdoptionID || prior.Digest != req.NewDigest {
			return StarterAdoptionRevision{}, starterErr("STARTER_IDEMPOTENCY_CONFLICT", req.AdoptionID, "idempotency key was reused for another adoption", ErrStarterIdempotencyConflict)
		}
		return cloneStarterRevision(*adoption), nil
	}
	adoption, visible := r.visibleLocked(req.Scope, req.AdoptionID, req.Actor)
	if !visible {
		return StarterAdoptionRevision{}, starterErr("STARTER_NOT_FOUND", req.AdoptionID, "starter adoption is not visible", ErrStarterNotFound)
	}
	if adoption.Revision != req.ExpectedRevision {
		return StarterAdoptionRevision{}, starterErr("STARTER_REVISION_CONFLICT", req.AdoptionID, "adoption revision moved under this writer", ErrStarterRevisionConflict)
	}
	compiled, ok := r.byDigest[req.NewDigest]
	if !ok || compiled.Manifest.ProfileID != adoption.ProfileID {
		return StarterAdoptionRevision{}, starterErr("STARTER_UNKNOWN_DIGEST", req.AdoptionID, "compiled profile digest is not known", ErrStarterRefMissing)
	}
	if compiled.Status != StarterProfileAvailable {
		return StarterAdoptionRevision{}, starterErr("STARTER_PLANNED", req.AdoptionID, "profile is planned with unresolved references: "+strings.Join(compiled.MissingRefs, ", "), ErrStarterRefMissing)
	}
	effective, diff := r.diffSettingsLocked(compiled.Manifest.Settings, adoption.Overrides, adoption.Effective, req.Replace)
	adoption.Digest = req.NewDigest
	adoption.Predecessor, adoption.Revision = adoption.Revision, adoption.Revision+1
	adoption.Status = StarterAdoptionDraft
	adoption.BaselineActive = false
	adoption.BlockedReason = ""
	adoption.Effective = effective
	adoption.EffectiveDiff = diff
	r.history[starterAdoptionKey(adoption.Scope, adoption.AdoptionID)] = append(r.history[starterAdoptionKey(adoption.Scope, adoption.AdoptionID)], cloneStarterRevision(*adoption))
	r.effects[req.IdempotencyKey] = starterEffect{AdoptionID: adoption.AdoptionID, ProfileID: adoption.ProfileID, Digest: req.NewDigest, Revision: adoption.Revision}
	return cloneStarterRevision(*adoption), nil
}

// Publish activates a draft adoption. Tenant mandatory restrictions are
// enforced first through canonical identities; then the baseline pin is
// verified against the live promotion control plane, reusing its distinct
// approval guard. Either failure blocks with its exact reason.
func (r *StarterProfileRegistry) Publish(scope Scope, adoptionID, actor string, expectedRevision uint64, at time.Time) (StarterAdoptionRevision, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	adoption, visible := r.visibleLocked(scope, adoptionID, actor)
	if !visible {
		return StarterAdoptionRevision{}, starterErr("STARTER_NOT_FOUND", adoptionID, "starter adoption is not visible", ErrStarterNotFound)
	}
	if adoption.Revision != expectedRevision {
		return StarterAdoptionRevision{}, starterErr("STARTER_REVISION_CONFLICT", adoptionID, "adoption revision moved under this writer", ErrStarterRevisionConflict)
	}
	if adoption.Status != StarterAdoptionDraft {
		return StarterAdoptionRevision{}, starterErr("STARTER_NOT_DRAFT", adoptionID, "only a draft adoption can publish", ErrStarterInvalid)
	}
	for key, required := range r.mandatory[scope.TenantID] {
		if got := adoption.Effective[key]; got != required {
			adoption.Status = StarterAdoptionBlocked
			adoption.BlockedReason = fmt.Sprintf("setting %q: tenant requires %q, profile proposes %q", key, required, got)
			adoption.Predecessor, adoption.Revision = adoption.Revision, adoption.Revision+1
			r.history[starterAdoptionKey(adoption.Scope, adoption.AdoptionID)] = append(r.history[starterAdoptionKey(adoption.Scope, adoption.AdoptionID)], cloneStarterRevision(*adoption))
			return StarterAdoptionRevision{}, starterErr("STARTER_POLICY", adoptionID, adoption.BlockedReason, ErrStarterPolicy)
		}
	}
	if r.planner == nil {
		return StarterAdoptionRevision{}, starterErr("STARTER_NO_PLANNER", adoptionID, "no setup planner is configured", ErrStarterInvalid)
	}
	if err := r.planner.verifyBaselineLocked(adoption.Baseline, scope.TenantID, at, "STARTER_BASELINE_REFUSED"); err != nil {
		adoption.Status = StarterAdoptionBlocked
		adoption.BlockedReason = "distinct approval required: the cited baseline is not a valid approval by another authority"
		adoption.Predecessor, adoption.Revision = adoption.Revision, adoption.Revision+1
		r.history[starterAdoptionKey(adoption.Scope, adoption.AdoptionID)] = append(r.history[starterAdoptionKey(adoption.Scope, adoption.AdoptionID)], cloneStarterRevision(*adoption))
		return StarterAdoptionRevision{}, err
	}
	adoption.Status = StarterAdoptionActive
	adoption.BaselineActive = true
	adoption.Predecessor, adoption.Revision = adoption.Revision, adoption.Revision+1
	r.history[starterAdoptionKey(adoption.Scope, adoption.AdoptionID)] = append(r.history[starterAdoptionKey(adoption.Scope, adoption.AdoptionID)], cloneStarterRevision(*adoption))
	return cloneStarterRevision(*adoption), nil
}

// Discard drops a draft adoption. Committed history stays recorded.
func (r *StarterProfileRegistry) Discard(scope Scope, adoptionID, actor string, expectedRevision uint64) (StarterAdoptionRevision, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	adoption, visible := r.visibleLocked(scope, adoptionID, actor)
	if !visible {
		return StarterAdoptionRevision{}, starterErr("STARTER_NOT_FOUND", adoptionID, "starter adoption is not visible", ErrStarterNotFound)
	}
	if adoption.Revision != expectedRevision {
		return StarterAdoptionRevision{}, starterErr("STARTER_REVISION_CONFLICT", adoptionID, "adoption revision moved under this writer", ErrStarterRevisionConflict)
	}
	if adoption.Status != StarterAdoptionDraft {
		return StarterAdoptionRevision{}, starterErr("STARTER_NOT_DRAFT", adoptionID, "only a draft adoption can be discarded", ErrStarterInvalid)
	}
	adoption.Status = StarterAdoptionDiscarded
	adoption.Predecessor, adoption.Revision = adoption.Revision, adoption.Revision+1
	r.history[starterAdoptionKey(adoption.Scope, adoption.AdoptionID)] = append(r.history[starterAdoptionKey(adoption.Scope, adoption.AdoptionID)], cloneStarterRevision(*adoption))
	return cloneStarterRevision(*adoption), nil
}

var starterDailyWork = map[string]string{
	"professional-services": "Open client engagements, capture time and expense, and connect billing.",
	"field-services":        "Dispatch jobs, crews and materials, and track job progress.",
	"recurring-services":    "Run service agreements, recurrences and checklists.",
}

// ProfileCard serves the How-you-work card for one compiled profile.
func (r *StarterProfileRegistry) ProfileCard(profileID string) (StarterProfileCard, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	compiled, ok := r.latest[profileID]
	if !ok {
		return StarterProfileCard{}, false
	}
	daily := starterDailyWork[profileID]
	if daily == "" {
		daily = "Review the proposed modules, workflows and settings before applying."
	}
	card := StarterProfileCard{
		ProfileID:       profileID,
		DailyWorkflow:   daily,
		RequiredModules: append([]string(nil), compiled.Manifest.Modules...),
		Readiness:       compiled.Status,
		Reasons:         append([]string(nil), compiled.Reasons...),
		ReadinessNote:   "Compiled against the finite test catalog; production readiness is evaluated separately against admitted capabilities.",
	}
	for _, module := range compiled.Manifest.Modules {
		card.SetupSteps = append(card.SetupSteps, "Review "+module+" module setup")
	}
	for _, ref := range compiled.Manifest.Workflows {
		card.SetupSteps = append(card.SetupSteps, "Confirm "+ref.WorkflowID+" version "+ref.CompiledPlanDigest)
	}
	card.SetupSteps = append(card.SetupSteps, compiled.Manifest.Forms...)
	card.SetupSteps = append(card.SetupSteps, compiled.Manifest.RequiredConfig...)
	if compiled.Status == StarterProfileAvailable {
		card.Actions = []string{StarterActionApply}
	}
	return card, true
}

// View serves the adoption state. Unknown, cross-tenant and non-admin reads
// all look identical.
func (r *StarterProfileRegistry) View(scope Scope, adoptionID, viewer string) (StarterAdoptionView, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	adoption, visible := r.visibleLocked(scope, adoptionID, viewer)
	if !visible {
		return StarterAdoptionView{}, false
	}
	view := StarterAdoptionView{
		AdoptionID: adoption.AdoptionID, Status: adoption.Status, Revision: adoption.Revision,
		Diff: append([]SettingDiff(nil), adoption.EffectiveDiff...), BlockedReason: adoption.BlockedReason,
	}
	switch adoption.Status {
	case StarterAdoptionDraft:
		view.Actions = []string{StarterActionPublish, StarterActionDiscard}
	case StarterAdoptionActive:
		view.Actions = []string{StarterActionUpgrade}
		view.ApprovalNote = "Activated under a distinct valid approval."
	case StarterAdoptionBlocked:
		view.Actions = []string{StarterActionReview}
		view.ApprovalNote = "Resolve the block, then review again."
	}
	return view, true
}

type starterSnapshot struct {
	Version   int                                  `json:"version"`
	ByDigest  map[string]CompiledStarterProfile    `json:"by_digest"`
	Latest    map[string]CompiledStarterProfile    `json:"latest"`
	Adoptions map[string]StarterAdoptionRevision   `json:"adoptions"`
	History   map[string][]StarterAdoptionRevision `json:"history"`
	Effects   map[string]starterEffect             `json:"effects"`
	Mandatory map[string]map[string]string         `json:"mandatory"`
	Aliases   map[string]string                    `json:"aliases"`
}

// Export captures the registry for crash recovery.
func (r *StarterProfileRegistry) Export() ([]byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	snapshot := starterSnapshot{
		Version:   1,
		ByDigest:  make(map[string]CompiledStarterProfile, len(r.byDigest)),
		Latest:    make(map[string]CompiledStarterProfile, len(r.latest)),
		Adoptions: make(map[string]StarterAdoptionRevision, len(r.adoptions)),
		History:   make(map[string][]StarterAdoptionRevision, len(r.history)),
		Effects:   make(map[string]starterEffect, len(r.effects)),
		Mandatory: make(map[string]map[string]string, len(r.mandatory)),
		Aliases:   make(map[string]string, len(r.aliases)),
	}
	for k, v := range r.byDigest {
		snapshot.ByDigest[k] = v
	}
	for k, v := range r.latest {
		snapshot.Latest[k] = v
	}
	for k, v := range r.adoptions {
		snapshot.Adoptions[k] = cloneStarterRevision(*v)
	}
	for k, v := range r.history {
		kept := make([]StarterAdoptionRevision, 0, len(v))
		for _, rev := range v {
			kept = append(kept, cloneStarterRevision(rev))
		}
		snapshot.History[k] = kept
	}
	for k, v := range r.effects {
		snapshot.Effects[k] = v
	}
	for tenant, restrictions := range r.mandatory {
		kept := make(map[string]string, len(restrictions))
		for k, v := range restrictions {
			kept[k] = v
		}
		snapshot.Mandatory[tenant] = kept
	}
	for k, v := range r.aliases {
		snapshot.Aliases[k] = v
	}
	return json.Marshal(snapshot)
}

// Import restores registry state. Corrupt or versioned-unknown snapshots are
// refused without touching live state.
func (r *StarterProfileRegistry) Import(data []byte) error {
	var decoded starterSnapshot
	if err := json.Unmarshal(data, &decoded); err != nil {
		return fmt.Errorf("%w: %v", ErrStarterSnapshotInvalid, err)
	}
	if decoded.Version != 1 {
		return fmt.Errorf("%w: unsupported version %d", ErrStarterSnapshotInvalid, decoded.Version)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byDigest = make(map[string]CompiledStarterProfile, len(decoded.ByDigest))
	for k, v := range decoded.ByDigest {
		r.byDigest[k] = v
	}
	r.latest = make(map[string]CompiledStarterProfile, len(decoded.Latest))
	for k, v := range decoded.Latest {
		r.latest[k] = v
	}
	r.adoptions = make(map[string]*StarterAdoptionRevision, len(decoded.Adoptions))
	for k, v := range decoded.Adoptions {
		kept := cloneStarterRevision(v)
		r.adoptions[k] = &kept
	}
	r.history = make(map[string][]StarterAdoptionRevision, len(decoded.History))
	for k, v := range decoded.History {
		kept := make([]StarterAdoptionRevision, 0, len(v))
		for _, rev := range v {
			kept = append(kept, cloneStarterRevision(rev))
		}
		r.history[k] = kept
	}
	r.effects = make(map[string]starterEffect, len(decoded.Effects))
	for k, v := range decoded.Effects {
		r.effects[k] = v
	}
	r.mandatory = make(map[string]map[string]string, len(decoded.Mandatory))
	for tenant, restrictions := range decoded.Mandatory {
		kept := make(map[string]string, len(restrictions))
		for k, v := range restrictions {
			kept[k] = v
		}
		r.mandatory[tenant] = kept
	}
	r.aliases = make(map[string]string, len(decoded.Aliases))
	for k, v := range decoded.Aliases {
		r.aliases[k] = v
	}
	return nil
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
