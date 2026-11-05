// WF-DATA-009: publication gating for custom-type versions.
//
// Publishing a new type version classifies the migration and, when it is
// breaking, refuses publication until every dependent definition and
// running instance pinned to the old version is re-pinned, re-published
// or explicitly left behind. Dependent definitions record their pins in
// the registry; running instances arrive through a read-only provider, so
// the gate can list them but never move them: instances keep the version
// they started on, and the only honest disposition for one is pinned-old.
package custom

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"
)

// ErrInvalidPublication reports an unusable gate input: a mistyped
// dependent, a mis-pinned version, or unattested evidence.
var ErrInvalidPublication = errors.New("custom: invalid publication gate input")

// DependentKind names one class of pinned consumer of a type version.
type DependentKind string

const (
	// DependentWorkflow is a workflow definition or compiled plan.
	DependentWorkflow DependentKind = "workflow"
	// DependentForm is a form definition bound to a task.
	DependentForm DependentKind = "form"
	// DependentRule is a rule bound to tenant data.
	DependentRule DependentKind = "rule"
	// DependentInstance is one running instance. Instances are immutable
	// history: they keep their pinned version and only ever leave it
	// behind by explicit acknowledgement.
	DependentInstance DependentKind = "instance"
)

// Valid reports whether k is a known dependent kind.
func (k DependentKind) Valid() bool {
	switch k {
	case DependentWorkflow, DependentForm, DependentRule, DependentInstance:
		return true
	}
	return false
}

// TypeID identifies one versioned custom type by namespace and kind. Its
// string form matches the schema descriptor the compiler emits for the
// type (WF-DATA-003).
type TypeID struct {
	Namespace string `json:"namespace"`
	Kind      string `json:"kind"`
}

// String returns the descriptor-shaped identity namespace.Kind.
func (t TypeID) String() string { return t.Namespace + "." + t.Kind }

// Dependent is one consumer pinned to a type version: the workflow, form,
// rule or instance identity, the version it pins and the digest of the
// pinned artifact.
type Dependent struct {
	Kind          DependentKind `json:"kind"`
	ID            string        `json:"id"`
	PinnedVersion uint64        `json:"version"`
	Digest        string        `json:"digest"`
}

// DependentKey identifies one dependent independently of what it pins.
type DependentKey struct {
	Kind DependentKind `json:"kind"`
	ID   string        `json:"id"`
}

// Key returns the identity of the dependent.
func (d Dependent) Key() DependentKey { return DependentKey{Kind: d.Kind, ID: d.ID} }

// Disposition names how a dependent leaves the old version behind.
type Disposition string

const (
	// DispositionRepinned means the dependent now references the new
	// version in place.
	DispositionRepinned Disposition = "re-pinned"
	// DispositionRepublished means the dependent was re-published against
	// the new version.
	DispositionRepublished Disposition = "re-published"
	// DispositionPinnedOld means the dependent explicitly stays on the old
	// version. It is the only disposition a running instance may take.
	DispositionPinnedOld Disposition = "pinned-old"
)

// Valid reports whether d is a taken disposition.
func (d Disposition) Valid() bool {
	switch d {
	case DispositionRepinned, DispositionRepublished, DispositionPinnedOld:
		return true
	}
	return false
}

// PinRecord is one attested disposition: who decided it, on what evidence
// and when. Like approvals, a disposition without evidence decides nothing.
type PinRecord struct {
	Dependent      Dependent   `json:"dependent"`
	Disposition    Disposition `json:"disposition"`
	DecidedBy      string      `json:"decided_by"`
	EvidenceDigest string      `json:"evidence_digest"`
	DecidedAt      time.Time   `json:"decided_at"`
}

// PinRegistry tracks which dependents pin which type version. It is the
// in-memory shape; durable projections bind the same entries when they
// land. Definitions record their own pins; running instances never appear
// here -- they arrive through InstancePins at gate time.
type PinRegistry struct {
	mu   sync.RWMutex
	pins map[TypeID]map[uint64][]Dependent
}

// NewPinRegistry returns an empty registry.
func NewPinRegistry() *PinRegistry {
	return &PinRegistry{pins: map[TypeID]map[uint64][]Dependent{}}
}

// RecordPin records one definition pin. Instances are refused: only the
// live provider may speak for running work.
func (r *PinRegistry) RecordPin(id TypeID, dep Dependent) error {
	if id.Namespace == "" || id.Kind == "" {
		return fmt.Errorf("%w: type identity is required", ErrInvalidPublication)
	}
	if !dep.Kind.Valid() || dep.Kind == DependentInstance {
		return fmt.Errorf("%w: kind %q is not a recordable definition", ErrInvalidPublication, dep.Kind)
	}
	if dep.ID == "" || dep.PinnedVersion == 0 || dep.Digest == "" {
		return fmt.Errorf("%w: dependent identity, version and digest are required", ErrInvalidPublication)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	versions := r.pins[id]
	if versions == nil {
		versions = map[uint64][]Dependent{}
		r.pins[id] = versions
	}
	for _, existing := range versions[dep.PinnedVersion] {
		if existing.Key() == dep.Key() {
			return fmt.Errorf("%w: %s %s already pins %s", ErrInvalidPublication, dep.Kind, dep.ID, id)
		}
	}
	versions[dep.PinnedVersion] = append(versions[dep.PinnedVersion], dep)
	return nil
}

// DependentsOn returns every definition pin on one type version, sorted by
// kind and identity so gate decisions are deterministic.
func (r *PinRegistry) DependentsOn(id TypeID, version uint64) []Dependent {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := append([]Dependent(nil), r.pins[id][version]...)
	sortDependents(out)
	return out
}

func sortDependents(deps []Dependent) {
	sort.Slice(deps, func(i, j int) bool {
		if deps[i].Kind != deps[j].Kind {
			return deps[i].Kind < deps[j].Kind
		}
		return deps[i].ID < deps[j].ID
	})
}

// InstancePins lists the running instances pinned to a type version. The
// served cell binds this to a store query; it is read-only by
// construction, so gating can never move a live instance.
type InstancePins func(ctx context.Context, id TypeID, version uint64) ([]Dependent, error)

// GateDecision is the publication verdict: the classified migration, every
// dependent found on the old version, the dependents still missing an
// attested disposition, and whether publication may proceed.
type GateDecision struct {
	Type        TypeID      `json:"type"`
	FromVersion uint64      `json:"from_version"`
	ToVersion   uint64      `json:"to_version"`
	Breaking    bool        `json:"breaking"`
	Dependents  []Dependent `json:"dependents"`
	Missing     []Dependent `json:"missing"`
	Allowed     bool        `json:"allowed"`
}

// CheckPublication classifies the migration and gates a breaking
// publication on per-dependent dispositions. The registry supplies the
// definition pins and the provider the running instances on the migration
// source; both are required, so a forgotten source fails closed instead of
// silently vacuous. records attests one disposition per dependent. A
// compatible migration is always allowed and needs no dispositions: it
// adds fields only, so no pinned plan can lose a path. A breaking one is
// refused with every undispositioned dependent listed until each is
// re-pinned, re-published or explicitly left on the old version.
func CheckPublication(ctx context.Context, migration TypeMigration, registry *PinRegistry, lives InstancePins, records map[DependentKey]PinRecord) (GateDecision, error) {
	if err := migration.Validate(); err != nil {
		return GateDecision{}, err
	}
	if registry == nil || lives == nil {
		return GateDecision{}, fmt.Errorf("%w: pin registry and instance provider are required", ErrInvalidPublication)
	}
	id := TypeID{Namespace: migration.Namespace, Kind: migration.Kind}
	live, err := lives(ctx, id, migration.FromVersion)
	if err != nil {
		return GateDecision{}, err
	}
	dependents := append(registry.DependentsOn(id, migration.FromVersion), live...)
	for _, dep := range dependents {
		if !dep.Kind.Valid() || dep.ID == "" || dep.Digest == "" {
			return GateDecision{}, fmt.Errorf("%w: dependent %v is mistyped", ErrInvalidPublication, dep.Key())
		}
		if dep.PinnedVersion != migration.FromVersion {
			return GateDecision{}, fmt.Errorf("%w: %s %s pins version %d, migration leaves %d",
				ErrInvalidPublication, dep.Kind, dep.ID, dep.PinnedVersion, migration.FromVersion)
		}
	}
	seen := map[DependentKey]bool{}
	for _, dep := range dependents {
		if seen[dep.Key()] {
			return GateDecision{}, fmt.Errorf("%w: %s %s is listed twice", ErrInvalidPublication, dep.Kind, dep.ID)
		}
		seen[dep.Key()] = true
	}
	sortDependents(dependents)
	decision := GateDecision{
		Type: id, FromVersion: migration.FromVersion, ToVersion: migration.ToVersion,
		Breaking: migration.Breaking(), Dependents: dependents,
	}
	if !decision.Breaking {
		decision.Allowed = true
		return decision, nil
	}
	for _, dep := range dependents {
		record, ok := records[dep.Key()]
		if !ok || !record.Disposition.Valid() || record.DecidedBy == "" ||
			record.EvidenceDigest == "" || record.DecidedAt.IsZero() {
			decision.Missing = append(decision.Missing, dep)
			continue
		}
		if dep.Kind == DependentInstance && record.Disposition != DispositionPinnedOld {
			return GateDecision{}, fmt.Errorf("%w: instance %s cannot %s a version it runs",
				ErrInvalidPublication, dep.ID, record.Disposition)
		}
	}
	if len(decision.Missing) > 0 {
		return decision, fmt.Errorf("%w: %d dependents undispositioned", ErrMigrationBlocked, len(decision.Missing))
	}
	decision.Allowed = true
	return decision, nil
}
