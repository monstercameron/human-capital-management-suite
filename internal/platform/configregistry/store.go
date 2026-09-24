package configregistry

import "sync"

// Store is the persistence port this package publishes and resolves
// through. [Registry] is the in-memory adapter this phase ships;
// internal/data/configregistry provides a durable PostgreSQL adapter
// implementing the same contract without this package changing at all — no
// database driver is named here.
type Store interface {
	// PutObject persists o, keyed by o.Ref(). o is already a fully-formed
	// immutable value minted by [Publish]; PutObject never edits an existing
	// record and an adapter must refuse (or no-op) a second Put for a key
	// that already holds a *different* record — [Publish] itself checks
	// this before calling PutObject, so a conforming adapter only needs to
	// guard against a caller that bypasses this package's own functions.
	PutObject(o ConfigurationObject) error
	// GetObject returns the published revision named by ref, if any.
	GetObject(ref ObjectRef) (ConfigurationObject, bool, error)
	// ListRevisions returns every revision published for (scope, kind, id),
	// oldest first.
	ListRevisions(scope Scope, kind Kind, id string) ([]ConfigurationObject, error)

	// PutActivation appends rec to the activation history for its
	// (scope, kind, id). It never edits or removes an earlier record for the
	// same key.
	PutActivation(rec ActivationRecord) error
	// GetLatestActivation returns the most recently appended
	// [ActivationRecord] for (scope, kind, id), if any.
	GetLatestActivation(scope Scope, kind Kind, id string) (ActivationRecord, bool, error)
	// ListActivations returns every activation ever recorded for
	// (scope, kind, id), oldest first — the full supersession history.
	ListActivations(scope Scope, kind Kind, id string) ([]ActivationRecord, error)
}

// objectGroupKey identifies one (scope, kind, id) family of revisions or
// activations.
func objectGroupKey(scope Scope, kind Kind, id string) string {
	return scope.key() + "\x00" + string(kind) + "\x00" + id
}

// Registry is an in-memory [Store]. It is safe for concurrent use.
type Registry struct {
	mu sync.RWMutex

	objects map[ObjectRef]ConfigurationObject
	// revisionOrder tracks publish order per (scope, kind, id) group so
	// ListRevisions can return oldest-first without depending on Go's
	// unordered map iteration.
	revisionOrder map[string][]uint32

	// activations tracks activation history per (scope, kind, id) group,
	// oldest first. The last element is always the currently active
	// revision.
	activations map[string][]ActivationRecord
}

var _ Store = (*Registry)(nil)
var _ AtomicActivationStore = (*Registry)(nil)

// NewRegistry returns an empty in-memory store.
func NewRegistry() *Registry {
	return &Registry{
		objects:       make(map[ObjectRef]ConfigurationObject),
		revisionOrder: make(map[string][]uint32),
		activations:   make(map[string][]ActivationRecord),
	}
}

// PutObject implements [Store].
func (r *Registry) PutObject(o ConfigurationObject) error {
	if o.ID == "" {
		return refuse(CodeMissingID, "", "a record with no id cannot be stored")
	}
	if !o.Scope.valid() {
		return refuse(CodeMissingScope, o.ID, "a record with no tenant scope cannot be stored")
	}
	ref := o.Ref()
	stored := o.clone()

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.objects[ref]; !exists {
		gk := objectGroupKey(o.Scope, o.Kind, o.ID)
		r.revisionOrder[gk] = append(r.revisionOrder[gk], o.Revision)
	}
	r.objects[ref] = stored
	return nil
}

// GetObject implements [Store].
func (r *Registry) GetObject(ref ObjectRef) (ConfigurationObject, bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	o, ok := r.objects[ref]
	if !ok {
		return ConfigurationObject{}, false, nil
	}
	return o.clone(), true, nil
}

// ListRevisions implements [Store].
func (r *Registry) ListRevisions(scope Scope, kind Kind, id string) ([]ConfigurationObject, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	revisions := r.revisionOrder[objectGroupKey(scope, kind, id)]
	out := make([]ConfigurationObject, 0, len(revisions))
	for _, rev := range revisions {
		ref := ObjectRef{Scope: scope, Kind: kind, ID: id, Revision: rev}
		out = append(out, r.objects[ref].clone())
	}
	return out, nil
}

// PutActivation implements [Store].
func (r *Registry) PutActivation(rec ActivationRecord) error {
	if rec.ID == "" {
		return refuse(CodeMissingID, "", "an activation with no id cannot be stored")
	}
	gk := objectGroupKey(rec.Scope, rec.Kind, rec.ID)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.activations[gk] = append(r.activations[gk], rec.clone())
	return nil
}

// CommitObjectActivation stores a validated object and appends its activation
// while holding one registry lock. It is the in-memory equivalent of the
// database adapter's transaction.
func (r *Registry) CommitObjectActivation(o ConfigurationObject, evidence ActivationEvidence) (ActivationRecord, error) {
	if r == nil {
		return ActivationRecord{}, refuse(CodeNoStore, o.ID, "no registry supplied")
	}
	if err := o.Verify(); err != nil {
		return ActivationRecord{}, err
	}
	if evidence.ActivatedBy == "" {
		return ActivationRecord{}, refuse(CodeUnauthorizedActivation, o.ID, "activation has no activating principal")
	}
	if evidence.ActivatedAt.IsZero() {
		return ActivationRecord{}, refuse(CodeMissingActivationTime, o.ID, "activation has no activation time")
	}
	ref := o.Ref()
	gk := objectGroupKey(o.Scope, o.Kind, o.ID)
	rec := ActivationRecord{
		Scope: o.Scope, Kind: o.Kind, ID: o.ID, Revision: o.Revision,
		ActivatedBy: evidence.ActivatedBy, Authority: evidence.Authority,
		Reason: evidence.Reason, ActivatedAt: evidence.ActivatedAt, ObjectDigest: o.Digest(),
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if prior, exists := r.objects[ref]; exists {
		if prior.Digest() != o.Digest() {
			return ActivationRecord{}, refuse(CodeRevisionConflict, o.ID, "revision %d is already published with different content", o.Revision)
		}
	} else {
		r.objects[ref] = o.clone()
		r.revisionOrder[gk] = append(r.revisionOrder[gk], o.Revision)
	}
	r.activations[gk] = append(r.activations[gk], rec.clone())
	return rec, nil
}

// GetLatestActivation implements [Store].
func (r *Registry) GetLatestActivation(scope Scope, kind Kind, id string) (ActivationRecord, bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	history := r.activations[objectGroupKey(scope, kind, id)]
	if len(history) == 0 {
		return ActivationRecord{}, false, nil
	}
	return history[len(history)-1].clone(), true, nil
}

// ListActivations implements [Store].
func (r *Registry) ListActivations(scope Scope, kind Kind, id string) ([]ActivationRecord, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	history := r.activations[objectGroupKey(scope, kind, id)]
	out := make([]ActivationRecord, 0, len(history))
	for _, rec := range history {
		out = append(out, rec.clone())
	}
	return out, nil
}
