// Business module registration for SMB-CORE-001.
//
// Optional business modules (party, job, schedule, quote/order, invoice,
// stock, asset, purchasing) register through the existing configuration
// bundle, capability and page boundaries. This file adds only the bounded
// manifest/activation layer: deterministic compile-time admission (dependency
// DAG, exact capability versions, single semantic owner, permission-mapped
// pages) and revision compare-and-swap activation with schema-readiness
// gates. Suspension blocks new writes while preserving authorized history
// and observed-result recovery; retirement retains referenced records.
//
// The registry is hermetic like the rest of this package: no storage,
// clocks, or network. Restarts restore through Export/Import snapshots;
// durable persistence stays with the existing configregistry stores.
package configbundle

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Stable module refusal causes. Callers discriminate with errors.Is; the
// *Error Detail carries diagnostics only.
var (
	ErrModuleUnknown            = errors.New("configbundle: unknown business module")
	ErrModuleCycle              = errors.New("configbundle: business module dependency cycle")
	ErrModuleUnknownCapability  = errors.New("configbundle: business module declares an unknown capability")
	ErrModuleDuplicateOwner     = errors.New("configbundle: business module semantic owner is already claimed")
	ErrModulePagePermission     = errors.New("configbundle: business module page lacks a permission mapping")
	ErrModuleReservedOwner      = errors.New("configbundle: business module claims reserved runtime state")
	ErrModuleUnknownDependency  = errors.New("configbundle: business module depends on an undeclared module")
	ErrModuleDependencyNotReady = errors.New("configbundle: business module dependency is not ready")
	ErrModuleRevisionConflict   = errors.New("configbundle: business module activation revision conflict")
	ErrModuleWritesBlocked      = errors.New("configbundle: business module writes are blocked")
	// ErrModuleRetired reports activation of a retired module. Retirement is
	// terminal: the retired view offers no actions and Activate cannot
	// resurrect it, so the view and the RPC agree on availability.
	ErrModuleRetired = errors.New("configbundle: business module is retired")
	// ErrModuleNotActive is the generic, non-revealing refusal for unknown,
	// never-activated, and cross-scope modules. It deliberately does not
	// disclose which case applied.
	ErrModuleNotActive = errors.New("configbundle: business module is not active")
)

// ModulePage binds one module route to its permission. A page without a
// permission mapping fails admission: navigation visibility never grants
// authority.
type ModulePage struct {
	Route      string `json:"route"`
	Permission string `json:"permission"`
}

// BusinessModuleManifest is the proposed registration for one optional
// business module. Transport phase, field classification and HTTP aliases
// are intentionally absent: those stay owned by the transport manifest and
// endpoint owners, so additive commercial operations cannot inherit them.
type BusinessModuleManifest struct {
	ModuleID       string       `json:"module_id"`
	Version        string       `json:"version"`
	Owner          string       `json:"owner"`
	Dependencies   []string     `json:"dependencies,omitempty"`
	Capabilities   []string     `json:"capabilities,omitempty"`
	Pages          []ModulePage `json:"pages,omitempty"`
	PolicyRefs     []string     `json:"policy_refs,omitempty"`
	MinimumRuntime string       `json:"minimum_runtime,omitempty"`
}

// ModuleState is the lifecycle state pinned by one activation revision.
type ModuleState string

const (
	ModuleStateEnabled   ModuleState = "enabled"
	ModuleStateSuspended ModuleState = "suspended"
	ModuleStateRetired   ModuleState = "retired"
)

// ModuleActivationRevision pins one activation of a module for a scope. It
// is the compare-and-swap unit: writers present the revision they prepared
// against and the server revalidates it at execution.
type ModuleActivationRevision struct {
	ModuleID     string      `json:"module_id"`
	Revision     uint64      `json:"revision"`
	Predecessor  uint64      `json:"predecessor"`
	BundleDigest string      `json:"bundle_digest"`
	Scope        Scope       `json:"scope"`
	State        ModuleState `json:"state"`
	Reason       string      `json:"reason,omitempty"`
}

// ModuleActivationRequest activates (or resumes) a module. SchemaReady maps
// module IDs to schema/migration readiness; absent entries are not ready.
// ExpectedPredecessor is the caller's observed active revision, 0 when the
// module was never activated in the scope.
type ModuleActivationRequest struct {
	ModuleID            string
	ExpectedPredecessor uint64
	BundleDigest        string
	Scope               Scope
	SchemaReady         map[string]bool
}

// Resolved module view vocabulary served to module administration surfaces.
const (
	ModuleStatusUnknown            = "unknown"
	ModuleStatusAvailable          = "available"
	ModuleStatusDependencyNotReady = "dependency-not-ready"

	ModuleHistoryPreserved = "authorized-history-preserved"
	ModuleHistoryNone      = "no-history"

	ModuleActionEnable  = "enable"
	ModuleActionSuspend = "suspend"
	ModuleActionRetire  = "retire"
)

// ModuleView is the resolved per-scope module state: effective status,
// missing dependency reasons, historical-read policy and the actions the
// server will currently accept. It is advisory; AuthorizeWrite revalidates
// the activation revision at execution.
type ModuleView struct {
	ModuleID                 string   `json:"module_id"`
	EffectiveStatus          string   `json:"effective_status"`
	ActiveRevision           uint64   `json:"active_revision"`
	Dependencies             []string `json:"dependencies,omitempty"`
	MissingDependencyReasons []string `json:"missing_dependency_reasons,omitempty"`
	HistoricalReadPolicy     string   `json:"historical_read_policy"`
	AvailableActions         []string `json:"available_actions,omitempty"`
	Reason                   string   `json:"reason,omitempty"`
}

func moduleErr(code, ref, detail string, cause error) *Error {
	return &Error{Code: code, Ref: ref, Detail: detail, Cause: cause}
}

func moduleScopeKey(scope Scope) string {
	return scope.TenantID + "\x00" + scope.CellID
}

func sortedModuleIDs(manifests []BusinessModuleManifest) []string {
	ids := make([]string, 0, len(manifests))
	for _, manifest := range manifests {
		ids = append(ids, manifest.ModuleID)
	}
	sort.Strings(ids)
	return ids
}

// CompileBusinessModules admits one module set. It rejects duplicate module
// IDs, empty owners, duplicate semantic owners, reserved runtime owners,
// undeclared dependencies, dependency cycles (with deterministic paths),
// unknown capability versions and pages without permission mappings. Map
// iteration is sorted everywhere so refusals are deterministic.
func CompileBusinessModules(manifests []BusinessModuleManifest, knownCapabilities map[string]bool) (*ModuleRegistry, error) {
	byID := make(map[string]BusinessModuleManifest, len(manifests))
	for _, manifest := range manifests {
		id := strings.TrimSpace(manifest.ModuleID)
		if id == "" {
			return nil, moduleErr("MODULE_INVALID_MANIFEST", "", "module id is required", ErrInvalidRequest)
		}
		if _, dup := byID[id]; dup {
			return nil, moduleErr("MODULE_DUPLICATE", id, "module id is declared twice", ErrInvalidRequest)
		}
		manifest.ModuleID = id
		byID[id] = manifest
	}
	ids := sortedModuleIDs(manifests)

	owners := make(map[string]string, len(ids))
	for _, id := range ids {
		manifest := byID[id]
		owner := strings.TrimSpace(manifest.Owner)
		if owner == "" {
			return nil, moduleErr("MODULE_INVALID_MANIFEST", id, "exactly one semantic owner is required", ErrInvalidRequest)
		}
		if owner == "workflow" {
			return nil, moduleErr("MODULE_RESERVED_OWNER", id, "modules cannot claim workflow runtime state", ErrModuleReservedOwner)
		}
		if first, dup := owners[owner]; dup {
			pair := []string{first, id}
			sort.Strings(pair)
			return nil, moduleErr("MODULE_DUPLICATE_OWNER", id, fmt.Sprintf("semantic owner %q already claimed by %s", owner, pair[0]), ErrModuleDuplicateOwner)
		}
		owners[owner] = id
	}

	for _, id := range ids {
		manifest := byID[id]
		deps := append([]string(nil), manifest.Dependencies...)
		sort.Strings(deps)
		for _, dep := range deps {
			if _, ok := byID[dep]; !ok {
				return nil, moduleErr("MODULE_UNKNOWN_DEPENDENCY", id, fmt.Sprintf("dependency %q is not declared", dep), ErrModuleUnknownDependency)
			}
			if dep == id {
				return nil, moduleErr("MODULE_CYCLE", id, fmt.Sprintf("dependency cycle: %s -> %s", id, id), ErrModuleCycle)
			}
		}
	}
	if path := findModuleCycle(byID, ids); path != "" {
		return nil, moduleErr("MODULE_CYCLE", "", "dependency cycle: "+path, ErrModuleCycle)
	}

	for _, id := range ids {
		manifest := byID[id]
		for _, capability := range manifest.Capabilities {
			if !knownCapabilities[capability] {
				return nil, moduleErr("MODULE_UNKNOWN_CAPABILITY", id, fmt.Sprintf("capability %q is not registered", capability), ErrModuleUnknownCapability)
			}
		}
		for _, page := range manifest.Pages {
			if strings.TrimSpace(page.Permission) == "" {
				return nil, moduleErr("MODULE_PAGE_PERMISSION", id, fmt.Sprintf("page %q has no permission mapping", page.Route), ErrModulePagePermission)
			}
		}
	}

	return &ModuleRegistry{
		manifests: byID,
		order:     ids,
		active:    make(map[string]map[string]*ModuleActivationRevision),
		history:   make(map[string]map[string][]ModuleActivationRevision),
		completed: make(map[string]bool),
	}, nil
}

// findModuleCycle reports one dependency cycle as a deterministic
// "a -> b -> a" path, or "" when the graph is acyclic.
func findModuleCycle(byID map[string]BusinessModuleManifest, ids []string) string {
	const (
		white = 0
		gray  = 1
		black = 2
	)
	color := make(map[string]int, len(ids))
	var stack []string
	var visit func(id string) string
	visit = func(id string) string {
		color[id] = gray
		stack = append(stack, id)
		deps := append([]string(nil), byID[id].Dependencies...)
		sort.Strings(deps)
		for _, dep := range deps {
			if color[dep] == gray {
				start := 0
				for stack[start] != dep {
					start++
				}
				return strings.Join(append(append([]string(nil), stack[start:]...), dep), " -> ")
			}
			if color[dep] == white {
				if path := visit(dep); path != "" {
					return path
				}
			}
		}
		stack = stack[:len(stack)-1]
		color[id] = black
		return ""
	}
	for _, id := range ids {
		if color[id] == white {
			if path := visit(id); path != "" {
				return path
			}
		}
	}
	return ""
}

// ModuleRegistry is the concurrency-safe in-memory module activation port.
// It persists nothing; Export/Import snapshots carry restart recovery while
// durable history stays with the existing configregistry stores.
type ModuleRegistry struct {
	mu        sync.Mutex
	manifests map[string]BusinessModuleManifest
	order     []string
	active    map[string]map[string]*ModuleActivationRevision
	history   map[string]map[string][]ModuleActivationRevision
	completed map[string]bool
}

// missingReadiness lists the module and its dependencies whose schema is not
// ready, in deterministic order.
func (r *ModuleRegistry) missingReadiness(manifest BusinessModuleManifest, ready map[string]bool) []string {
	check := append([]string{manifest.ModuleID}, manifest.Dependencies...)
	sort.Strings(check)
	var missing []string
	for _, id := range check {
		if !ready[id] {
			missing = append(missing, id+": schema-not-ready")
		}
	}
	return missing
}

// Activate enables (or resumes) a module for a scope. The expected
// predecessor must match the active revision or the activation conflicts;
// dependency schemas must all be ready or nothing is recorded. Replaying
// the same bundle digest against the same predecessor is idempotent and
// returns the single recorded revision. Retirement is terminal: activating
// a retired module is refused. The digest must be well-formed sha256:hex;
// cryptographic bundle verification happens at the served activation
// boundary, which also supplies release authority.
func (r *ModuleRegistry) Activate(req ModuleActivationRequest) (ModuleActivationRevision, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	manifest, ok := r.manifests[req.ModuleID]
	if !ok {
		return ModuleActivationRevision{}, moduleErr("MODULE_UNKNOWN", req.ModuleID, "module is not registered", ErrModuleUnknown)
	}
	if _, err := digestBytes(req.BundleDigest); err != nil {
		return ModuleActivationRevision{}, moduleErr("MODULE_INVALID_DIGEST", req.ModuleID, "bundle digest must be sha256:hex", ErrInvalidRequest)
	}
	key := moduleScopeKey(req.Scope)
	current := r.active[key][req.ModuleID]
	if current != nil && current.State == ModuleStateRetired {
		return ModuleActivationRevision{}, moduleErr("MODULE_RETIRED", req.ModuleID, "retirement is terminal; the retired module offers no activation", ErrModuleRetired)
	}
	if current != nil &&
		current.BundleDigest == req.BundleDigest &&
		current.State == ModuleStateEnabled &&
		req.ExpectedPredecessor+1 == current.Revision {
		return *current, nil
	}
	var currentRev uint64
	if current != nil {
		currentRev = current.Revision
	}
	if req.ExpectedPredecessor != currentRev {
		return ModuleActivationRevision{}, moduleErr("MODULE_REVISION_CONFLICT", req.ModuleID,
			fmt.Sprintf("expected predecessor %d, active revision is %d", req.ExpectedPredecessor, currentRev),
			ErrModuleRevisionConflict)
	}
	if missing := r.missingReadiness(manifest, req.SchemaReady); len(missing) > 0 {
		return ModuleActivationRevision{}, moduleErr("MODULE_DEPENDENCY_NOT_READY", req.ModuleID,
			"missing prerequisites: "+strings.Join(missing, ", "), ErrModuleDependencyNotReady)
	}
	next := ModuleActivationRevision{
		ModuleID: req.ModuleID, Revision: currentRev + 1, Predecessor: currentRev,
		BundleDigest: req.BundleDigest, Scope: req.Scope, State: ModuleStateEnabled,
	}
	r.storeLocked(key, next)
	return next, nil
}

func (r *ModuleRegistry) storeLocked(key string, rev ModuleActivationRevision) {
	if r.active[key] == nil {
		r.active[key] = make(map[string]*ModuleActivationRevision)
	}
	if r.history[key] == nil {
		r.history[key] = make(map[string][]ModuleActivationRevision)
	}
	stored := rev
	r.active[key][rev.ModuleID] = &stored
	r.history[key][rev.ModuleID] = append(r.history[key][rev.ModuleID], rev)
}

// Suspend blocks new writes for an enabled module while preserving
// authorized history and observed-result recovery. The expected revision
// must match the active revision and the reason is mandatory: suspension
// always exposes why.
func (r *ModuleRegistry) Suspend(scope Scope, moduleID, reason string, expectedRevision uint64) (ModuleActivationRevision, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.manifests[moduleID]; !ok {
		return ModuleActivationRevision{}, moduleErr("MODULE_UNKNOWN", moduleID, "module is not registered", ErrModuleUnknown)
	}
	if strings.TrimSpace(reason) == "" {
		return ModuleActivationRevision{}, moduleErr("MODULE_INVALID_REQUEST", moduleID, "suspension reason is required", ErrInvalidRequest)
	}
	key := moduleScopeKey(scope)
	current := r.active[key][moduleID]
	if current == nil {
		return ModuleActivationRevision{}, moduleErr("MODULE_NOT_ACTIVE", moduleID, "module is not active", ErrModuleNotActive)
	}
	if expectedRevision != current.Revision {
		return ModuleActivationRevision{}, moduleErr("MODULE_REVISION_CONFLICT", moduleID,
			fmt.Sprintf("expected revision %d, active revision is %d", expectedRevision, current.Revision),
			ErrModuleRevisionConflict)
	}
	if current.State != ModuleStateEnabled {
		return ModuleActivationRevision{}, moduleErr("MODULE_REVISION_CONFLICT", moduleID,
			fmt.Sprintf("module is %s, only enabled modules suspend", current.State), ErrModuleRevisionConflict)
	}
	next := *current
	next.Revision++
	next.Predecessor = current.Revision
	next.State = ModuleStateSuspended
	next.Reason = reason
	r.storeLocked(key, next)
	return next, nil
}

// Retire ends a module's active life without dropping referenced records:
// the full revision history stays available for authorized reads. The
// reason is mandatory and retirement is terminal (see Activate).
func (r *ModuleRegistry) Retire(scope Scope, moduleID, reason string, expectedRevision uint64) (ModuleActivationRevision, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.manifests[moduleID]; !ok {
		return ModuleActivationRevision{}, moduleErr("MODULE_UNKNOWN", moduleID, "module is not registered", ErrModuleUnknown)
	}
	if strings.TrimSpace(reason) == "" {
		return ModuleActivationRevision{}, moduleErr("MODULE_INVALID_REQUEST", moduleID, "retirement reason is required", ErrInvalidRequest)
	}
	key := moduleScopeKey(scope)
	current := r.active[key][moduleID]
	if current == nil {
		return ModuleActivationRevision{}, moduleErr("MODULE_NOT_ACTIVE", moduleID, "module is not active", ErrModuleNotActive)
	}
	if expectedRevision != current.Revision {
		return ModuleActivationRevision{}, moduleErr("MODULE_REVISION_CONFLICT", moduleID,
			fmt.Sprintf("expected revision %d, active revision is %d", expectedRevision, current.Revision),
			ErrModuleRevisionConflict)
	}
	if current.State == ModuleStateRetired {
		return ModuleActivationRevision{}, moduleErr("MODULE_REVISION_CONFLICT", moduleID,
			"module is already retired", ErrModuleRevisionConflict)
	}
	next := *current
	next.Revision++
	next.Predecessor = current.Revision
	next.State = ModuleStateRetired
	next.Reason = reason
	r.storeLocked(key, next)
	return next, nil
}

// AuthorizeWrite revalidates the activation revision at execution. Unknown,
// never-activated and cross-scope modules share the generic not-active
// refusal so callers learn nothing about other scopes.
func (r *ModuleRegistry) AuthorizeWrite(scope Scope, moduleID string, revision uint64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	current := r.active[moduleScopeKey(scope)][moduleID]
	if current == nil {
		return moduleErr("MODULE_NOT_ACTIVE", moduleID, "module is not active", ErrModuleNotActive)
	}
	if revision != current.Revision {
		return moduleErr("MODULE_REVISION_CONFLICT", moduleID,
			fmt.Sprintf("dispatch revision %d does not match active revision %d", revision, current.Revision),
			ErrModuleRevisionConflict)
	}
	if current.State != ModuleStateEnabled {
		return moduleErr("MODULE_WRITES_BLOCKED", moduleID,
			fmt.Sprintf("module is %s", current.State), ErrModuleWritesBlocked)
	}
	return nil
}

// AuthorizeRead permits authorized history reads whenever the scope holds
// any activation record, including suspended and retired modules.
func (r *ModuleRegistry) AuthorizeRead(scope Scope, moduleID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.history[moduleScopeKey(scope)][moduleID]) == 0 {
		return moduleErr("MODULE_NOT_ACTIVE", moduleID, "module is not active", ErrModuleNotActive)
	}
	return nil
}

// History returns the retained activation revisions for a scope and module,
// oldest first. Retirement never trims it.
func (r *ModuleRegistry) History(scope Scope, moduleID string) []ModuleActivationRevision {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]ModuleActivationRevision(nil), r.history[moduleScopeKey(scope)][moduleID]...)
}

// CompleteObservedEffect records recovery of an already-observed effect
// against the revision that observed it. It stays available while the
// module is suspended or retired, and repeats are idempotent: the second
// return reports whether this call deduplicated.
func (r *ModuleRegistry) CompleteObservedEffect(scope Scope, moduleID string, revision uint64, effectID string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	found := false
	for _, rev := range r.history[moduleScopeKey(scope)][moduleID] {
		if rev.Revision == revision {
			found = true
			break
		}
	}
	if !found {
		return false, moduleErr("MODULE_REVISION_CONFLICT", moduleID,
			fmt.Sprintf("revision %d was never active", revision), ErrModuleRevisionConflict)
	}
	key := moduleScopeKey(scope) + "\x00" + moduleID + "\x00" + fmt.Sprint(revision) + "\x00" + effectID
	if r.completed[key] {
		return true, nil
	}
	r.completed[key] = true
	return false, nil
}

// ResolveView returns the advisory module state served to module
// administration surfaces. Unknown modules resolve to the unknown status;
// never-activated modules report dependency readiness without executable
// actions until prerequisites hold.
func (r *ModuleRegistry) ResolveView(scope Scope, moduleID string, schemaReady map[string]bool) ModuleView {
	r.mu.Lock()
	defer r.mu.Unlock()
	manifest, ok := r.manifests[moduleID]
	if !ok {
		return ModuleView{ModuleID: moduleID, EffectiveStatus: ModuleStatusUnknown, HistoricalReadPolicy: ModuleHistoryNone}
	}
	view := ModuleView{
		ModuleID:     moduleID,
		Dependencies: append([]string(nil), manifest.Dependencies...),
	}
	sort.Strings(view.Dependencies)
	current := r.active[moduleScopeKey(scope)][moduleID]
	historyLen := len(r.history[moduleScopeKey(scope)][moduleID])
	if historyLen > 0 {
		view.HistoricalReadPolicy = ModuleHistoryPreserved
	} else {
		view.HistoricalReadPolicy = ModuleHistoryNone
	}
	if current == nil {
		if missing := r.missingReadiness(manifest, schemaReady); len(missing) > 0 {
			view.EffectiveStatus = ModuleStatusDependencyNotReady
			view.MissingDependencyReasons = missing
			return view
		}
		view.EffectiveStatus = ModuleStatusAvailable
		view.AvailableActions = []string{ModuleActionEnable}
		return view
	}
	view.EffectiveStatus = string(current.State)
	view.ActiveRevision = current.Revision
	view.Reason = current.Reason
	switch current.State {
	case ModuleStateEnabled:
		view.AvailableActions = []string{ModuleActionSuspend, ModuleActionRetire}
	case ModuleStateSuspended:
		view.AvailableActions = []string{ModuleActionEnable, ModuleActionRetire}
	default:
		view.AvailableActions = nil
	}
	return view
}

// moduleSnapshot is the restart-recovery carrier for one registry.
type moduleSnapshot struct {
	Manifests []BusinessModuleManifest                         `json:"manifests"`
	Active    map[string]map[string]*ModuleActivationRevision  `json:"active"`
	History   map[string]map[string][]ModuleActivationRevision `json:"history"`
	Completed []string                                         `json:"completed"`
}

// Export captures the registry state for restart recovery. Keys are sorted
// so repeated exports of identical state are byte-identical.
func (r *ModuleRegistry) Export() ([]byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	snapshot := moduleSnapshot{
		Active:  make(map[string]map[string]*ModuleActivationRevision, len(r.active)),
		History: make(map[string]map[string][]ModuleActivationRevision, len(r.history)),
	}
	for _, id := range r.order {
		manifest := r.manifests[id]
		manifest.Dependencies = append([]string(nil), manifest.Dependencies...)
		manifest.Capabilities = append([]string(nil), manifest.Capabilities...)
		manifest.Pages = append([]ModulePage(nil), manifest.Pages...)
		manifest.PolicyRefs = append([]string(nil), manifest.PolicyRefs...)
		snapshot.Manifests = append(snapshot.Manifests, manifest)
	}
	for scopeKey, mods := range r.active {
		ids := make([]string, 0, len(mods))
		for id := range mods {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		restored := make(map[string]*ModuleActivationRevision, len(mods))
		for _, id := range ids {
			stored := *mods[id]
			restored[id] = &stored
		}
		snapshot.Active[scopeKey] = restored
	}
	for scopeKey, mods := range r.history {
		ids := make([]string, 0, len(mods))
		for id := range mods {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		restored := make(map[string][]ModuleActivationRevision, len(mods))
		for _, id := range ids {
			restored[id] = append([]ModuleActivationRevision(nil), mods[id]...)
		}
		snapshot.History[scopeKey] = restored
	}
	for key := range r.completed {
		snapshot.Completed = append(snapshot.Completed, key)
	}
	sort.Strings(snapshot.Completed)
	return json.Marshal(snapshot)
}

// moduleRevisionsEqual compares two activation revisions field by field so a
// snapshot cannot smuggle a divergent active pointer past a revision-number
// check.
func moduleRevisionsEqual(a, b ModuleActivationRevision) bool {
	return a.ModuleID == b.ModuleID && a.Revision == b.Revision && a.Predecessor == b.Predecessor &&
		a.BundleDigest == b.BundleDigest && a.Scope == b.Scope && a.State == b.State && a.Reason == b.Reason
}

// parseModuleScopeKey splits a snapshot map key back into its scope. The
// tenant half must be nonempty; the cell half may be empty.
func parseModuleScopeKey(key string) (Scope, error) {
	parts := strings.SplitN(key, "\x00", 2)
	if len(parts) != 2 || parts[0] == "" {
		return Scope{}, ErrInvalidRequest
	}
	return Scope{TenantID: parts[0], CellID: parts[1]}, nil
}

// validateSnapshotRevision revalidates one imported revision against its
// map position. Nothing about the snapshot is trusted: module binding,
// scope binding, digest shape, lifecycle state and reason rules are all
// rechecked, so scope-swapped or forged revisions fail closed.
func (r *ModuleRegistry) validateSnapshotRevision(scopeKey, mapModule string, rev ModuleActivationRevision) error {
	if _, ok := r.manifests[mapModule]; !ok {
		return moduleErr("MODULE_INVALID_SNAPSHOT", mapModule, "snapshot module is not registered", ErrModuleUnknown)
	}
	if rev.ModuleID != mapModule {
		return moduleErr("MODULE_INVALID_SNAPSHOT", mapModule, "snapshot revision is filed under the wrong module", ErrInvalidRequest)
	}
	scope, err := parseModuleScopeKey(scopeKey)
	if err != nil {
		return moduleErr("MODULE_INVALID_SNAPSHOT", mapModule, "snapshot scope key is malformed", ErrInvalidRequest)
	}
	if rev.Scope != scope {
		return moduleErr("MODULE_INVALID_SNAPSHOT", mapModule, "snapshot revision scope disagrees with its map key", ErrInvalidRequest)
	}
	if _, err := digestBytes(rev.BundleDigest); err != nil {
		return moduleErr("MODULE_INVALID_SNAPSHOT", mapModule, "snapshot bundle digest is malformed", ErrInvalidRequest)
	}
	switch rev.State {
	case ModuleStateEnabled:
		if rev.Reason != "" {
			return moduleErr("MODULE_INVALID_SNAPSHOT", mapModule, "enabled snapshot revision must not carry a reason", ErrInvalidRequest)
		}
	case ModuleStateSuspended, ModuleStateRetired:
		if strings.TrimSpace(rev.Reason) == "" {
			return moduleErr("MODULE_INVALID_SNAPSHOT", mapModule, "suspended and retired snapshot revisions must expose a reason", ErrInvalidRequest)
		}
	default:
		return moduleErr("MODULE_INVALID_SNAPSHOT", mapModule, "snapshot revision state is unknown", ErrInvalidRequest)
	}
	return nil
}

// canonicalModuleManifests marshals one manifest set in module-ID order so
// Import can prove the snapshot set is identical to the compiled set.
func canonicalModuleManifests(manifests map[string]BusinessModuleManifest, order []string) ([]byte, error) {
	ids := append([]string(nil), order...)
	sort.Strings(ids)
	list := make([]BusinessModuleManifest, 0, len(ids))
	for _, id := range ids {
		manifest, ok := manifests[id]
		if !ok {
			return nil, ErrInvalidRequest
		}
		list = append(list, manifest)
	}
	return json.Marshal(list)
}

// Import restores a snapshot previously captured by Export. The receiver
// must already be compiled with the identical module set; every imported
// field revalidates fail-closed (manifest identity, scope and module map
// bindings, digest shapes, lifecycle states, reason rules, revision chains
// in both directions, and completed-effect references), so corrupt or
// hostile snapshots refuse instead of resurrecting phantom authority.
func (r *ModuleRegistry) Import(data []byte) error {
	var snapshot moduleSnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return moduleErr("MODULE_INVALID_SNAPSHOT", "", "snapshot is not valid module state", err)
	}
	snapshotByID := make(map[string]BusinessModuleManifest, len(snapshot.Manifests))
	snapshotOrder := make([]string, 0, len(snapshot.Manifests))
	for _, manifest := range snapshot.Manifests {
		if _, dup := snapshotByID[manifest.ModuleID]; dup || manifest.ModuleID == "" {
			return moduleErr("MODULE_INVALID_SNAPSHOT", manifest.ModuleID, "snapshot module set is malformed", ErrInvalidRequest)
		}
		snapshotByID[manifest.ModuleID] = manifest
		snapshotOrder = append(snapshotOrder, manifest.ModuleID)
	}
	wantManifests, err := canonicalModuleManifests(r.manifests, r.order)
	if err != nil {
		return moduleErr("MODULE_INVALID_SNAPSHOT", "", "compiled module set is malformed", ErrInvalidRequest)
	}
	gotManifests, err := canonicalModuleManifests(snapshotByID, snapshotOrder)
	if err != nil {
		return moduleErr("MODULE_INVALID_SNAPSHOT", "", "snapshot module set is malformed", ErrInvalidRequest)
	}
	if string(wantManifests) != string(gotManifests) {
		return moduleErr("MODULE_INVALID_SNAPSHOT", "", "snapshot module set differs from the compiled set", ErrInvalidRequest)
	}
	for scopeKey, mods := range snapshot.History {
		for id, chain := range mods {
			if len(chain) == 0 {
				return moduleErr("MODULE_INVALID_SNAPSHOT", id, "snapshot history chain is empty", ErrInvalidRequest)
			}
			for i, rev := range chain {
				if rev.Revision != uint64(i+1) || rev.Predecessor != uint64(i) {
					return moduleErr("MODULE_INVALID_SNAPSHOT", id, "snapshot revision chain is broken", ErrInvalidRequest)
				}
				if err := r.validateSnapshotRevision(scopeKey, id, rev); err != nil {
					return err
				}
			}
			active := snapshot.Active[scopeKey][id]
			if active == nil {
				return moduleErr("MODULE_INVALID_SNAPSHOT", id, "snapshot history has no active pointer", ErrInvalidRequest)
			}
			if !moduleRevisionsEqual(*active, chain[len(chain)-1]) {
				return moduleErr("MODULE_INVALID_SNAPSHOT", id, "snapshot active pointer disagrees with history", ErrInvalidRequest)
			}
		}
	}
	for scopeKey, mods := range snapshot.Active {
		for id, active := range mods {
			if active == nil {
				return moduleErr("MODULE_INVALID_SNAPSHOT", id, "snapshot active pointer is empty", ErrInvalidRequest)
			}
			if err := r.validateSnapshotRevision(scopeKey, id, *active); err != nil {
				return err
			}
			chain := snapshot.History[scopeKey][id]
			if len(chain) == 0 {
				return moduleErr("MODULE_INVALID_SNAPSHOT", id, "snapshot active pointer has no history chain", ErrInvalidRequest)
			}
			if !moduleRevisionsEqual(*active, chain[len(chain)-1]) {
				return moduleErr("MODULE_INVALID_SNAPSHOT", id, "snapshot active pointer disagrees with history", ErrInvalidRequest)
			}
		}
	}
	for _, key := range snapshot.Completed {
		parts := strings.SplitN(key, "\x00", 5)
		if len(parts) != 5 || parts[0] == "" || parts[2] == "" || parts[4] == "" {
			return moduleErr("MODULE_INVALID_SNAPSHOT", "", "snapshot completion key is malformed", ErrInvalidRequest)
		}
		if _, ok := r.manifests[parts[2]]; !ok {
			return moduleErr("MODULE_INVALID_SNAPSHOT", parts[2], "snapshot completion references an unknown module", ErrModuleUnknown)
		}
		var rev uint64
		if _, err := fmt.Sscanf(parts[3], "%d", &rev); err != nil || rev == 0 || fmt.Sprint(rev) != parts[3] {
			return moduleErr("MODULE_INVALID_SNAPSHOT", parts[2], "snapshot completion revision is malformed", ErrInvalidRequest)
		}
		found := false
		for _, record := range snapshot.History[parts[0]+"\x00"+parts[1]][parts[2]] {
			if record.Revision == rev {
				found = true
				break
			}
		}
		if !found {
			return moduleErr("MODULE_INVALID_SNAPSHOT", parts[2], "snapshot completion references a revision that was never active", ErrInvalidRequest)
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.active = make(map[string]map[string]*ModuleActivationRevision, len(snapshot.Active))
	for scopeKey, mods := range snapshot.Active {
		restored := make(map[string]*ModuleActivationRevision, len(mods))
		for id, rev := range mods {
			stored := *rev
			restored[id] = &stored
		}
		r.active[scopeKey] = restored
	}
	r.history = make(map[string]map[string][]ModuleActivationRevision, len(snapshot.History))
	for scopeKey, mods := range snapshot.History {
		restored := make(map[string][]ModuleActivationRevision, len(mods))
		for id, chain := range mods {
			restored[id] = append([]ModuleActivationRevision(nil), chain...)
		}
		r.history[scopeKey] = restored
	}
	r.completed = make(map[string]bool, len(snapshot.Completed))
	for _, key := range snapshot.Completed {
		r.completed[key] = true
	}
	return nil
}
