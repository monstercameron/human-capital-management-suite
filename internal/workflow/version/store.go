package version

import (
	"sort"
	"sync"
)

// Store is the persistence port this package publishes and resolves through.
// [Registry] is the in-memory adapter this phase ships; a durable adapter
// (Postgres, per planning/specs/workflow-runtime.md's publication pipeline)
// implements the same three-method contract without this package changing at
// all. No adapter but [Registry] exists here: a database belongs to a
// different lane.
type Store interface {
	// Put persists v, keyed by v.CompiledPlanDigest. Calling Put again with
	// the same digest replaces the current record for that digest — this is
	// how a lifecycle transition (Activate/Quarantine/Retire) is recorded,
	// since [CompiledVersion] itself has no setters. Put never edits an
	// existing value in place; every CompiledVersion that ever reaches Put is
	// a value already fully formed by this package.
	Put(v CompiledVersion) error
	// GetByDigest returns the current record for a compiled-plan digest.
	GetByDigest(digest string) (CompiledVersion, bool, error)
	// GetActiveForWorkflow returns the one version currently ACTIVE for a
	// workflow, if any. There is never more than one: [Activate] refuses a
	// competing activation unless the caller explicitly supersedes it.
	GetActiveForWorkflow(workflowID string) (CompiledVersion, bool, error)
	// List returns every version published for a workflow, oldest first.
	List(workflowID string) ([]CompiledVersion, error)
}

// CatalogStore is the read capability needed by workflow-authoring and
// inspection surfaces that list every published workflow family. Keeping it
// separate from Store avoids widening the runtime's exact-pin dependency:
// execution resolves one workflow and never needs a global catalog.
type CatalogStore interface {
	Store
	// ListAll returns every published version in deterministic workflow and
	// publication order. Returned records are defensive copies.
	ListAll() ([]CompiledVersion, error)
}

// Registry is an in-memory [Store]. It is safe for concurrent use.
type Registry struct {
	mu       sync.RWMutex
	byDigest map[string]CompiledVersion
	order    map[string][]string // workflowID -> compiled-plan digests, publish order
}

// NewRegistry returns an empty in-memory store.
func NewRegistry() *Registry {
	return &Registry{
		byDigest: make(map[string]CompiledVersion),
		order:    make(map[string][]string),
	}
}

// Put implements [Store].
func (r *Registry) Put(v CompiledVersion) error {
	if v.WorkflowID == "" {
		return refuse(CodeInvalidRecord, "", "a record with no workflow id cannot be stored")
	}
	if v.CompiledPlanDigest == "" {
		return refuse(CodeInvalidRecord, v.WorkflowID, "a record with no compiled-plan digest cannot be stored")
	}
	if err := ValidateSemanticVersion(v.SemanticVersion); err != nil {
		return wrap(CodeInvalidSemanticVersion, v.WorkflowID, err,
			"compiled version carries an invalid semantic version")
	}
	stored := v.clone()
	r.mu.Lock()
	defer r.mu.Unlock()
	for digest, existing := range r.byDigest {
		if v.SemanticVersion != "" && digest != v.CompiledPlanDigest && existing.WorkflowID == v.WorkflowID && existing.SemanticVersion == v.SemanticVersion {
			return refuse(CodeSemanticVersionConflict, v.WorkflowID,
				"semantic version %s is already stored with compiled-plan digest %s", v.SemanticVersion, digest)
		}
	}
	if _, exists := r.byDigest[v.CompiledPlanDigest]; !exists {
		r.order[v.WorkflowID] = append(r.order[v.WorkflowID], v.CompiledPlanDigest)
	}
	r.byDigest[v.CompiledPlanDigest] = stored
	return nil
}

// GetByDigest implements [Store].
func (r *Registry) GetByDigest(digest string) (CompiledVersion, bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	v, ok := r.byDigest[digest]
	if !ok {
		return CompiledVersion{}, false, nil
	}
	return v.clone(), true, nil
}

// GetActiveForWorkflow implements [Store].
func (r *Registry) GetActiveForWorkflow(workflowID string) (CompiledVersion, bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, digest := range r.order[workflowID] {
		if v := r.byDigest[digest]; v.Status == StatusActive {
			return v.clone(), true, nil
		}
	}
	return CompiledVersion{}, false, nil
}

// List implements [Store].
func (r *Registry) List(workflowID string) ([]CompiledVersion, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	digests := r.order[workflowID]
	out := make([]CompiledVersion, 0, len(digests))
	for _, digest := range digests {
		out = append(out, r.byDigest[digest].clone())
	}
	return out, nil
}

// ListAll implements [CatalogStore].
func (r *Registry) ListAll() ([]CompiledVersion, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	workflowIDs := make([]string, 0, len(r.order))
	for workflowID := range r.order {
		workflowIDs = append(workflowIDs, workflowID)
	}
	sort.Strings(workflowIDs)
	var out []CompiledVersion
	for _, workflowID := range workflowIDs {
		for _, digest := range r.order[workflowID] {
			out = append(out, r.byDigest[digest].clone())
		}
	}
	return out, nil
}
