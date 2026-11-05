package taskpool

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

var (
	ErrInvalidCatalog = errors.New("taskpool: invalid catalog entry")
	ErrKindExists     = errors.New("taskpool: catalog kind already registered")
	ErrEffectRefused  = errors.New("taskpool: task kind carries an effect")
	ErrInvalidPool    = errors.New("taskpool: invalid pool declaration")
	ErrKindNotAllowed = errors.New("taskpool: task kind is not allow-listed")
	ErrPoolFull       = errors.New("taskpool: pool is at its declared maximum")
	ErrPoolIdentity   = errors.New("taskpool: request is for another pool")
	ErrTaskNotFound   = errors.New("taskpool: task is not in the pool")
	ErrTaskClosed     = errors.New("taskpool: task is already completed")
	ErrOutputMismatch = errors.New("taskpool: outputs do not match the kind contract")
)

// Task states and audit actions: the closed pool vocabularies.
const (
	TaskOpen      = "OPEN"
	TaskCompleted = "COMPLETED"

	AuditAdded     = "ADDED"
	AuditCompleted = "COMPLETED"
)

// KindRef pins one allow-listed catalog kind version. Identity is
// (Kind, Version); there is no "latest" admission.
type KindRef struct {
	Kind    string
	Version uint32
}

// CatalogEntry is one allow-listable task kind: its typed output contract
// and its effect class, which must never write. The pool cannot add effects
// because no entry that could add one is registrable.
type CatalogEntry struct {
	Kind         string
	Version      uint32
	OutputSchema workflow.SchemaRef
	Outputs      []workflow.Field
	EffectClass  capability.EffectClass
}

func (e CatalogEntry) Validate() error {
	if strings.TrimSpace(e.Kind) == "" || e.Version == 0 {
		return fmt.Errorf("%w: kind and positive version are required", ErrInvalidCatalog)
	}
	if strings.TrimSpace(e.OutputSchema.SchemaID) == "" || e.OutputSchema.Version == 0 {
		return fmt.Errorf("%w: output schema is required", ErrInvalidCatalog)
	}
	if len(e.Outputs) == 0 {
		return fmt.Errorf("%w: at least one declared output is required", ErrInvalidCatalog)
	}
	seen := map[string]bool{}
	for _, field := range e.Outputs {
		if strings.TrimSpace(field.Path) == "" || field.Type.Kind == "" || seen[field.Path] {
			return fmt.Errorf("%w: outputs must be uniquely pathed and typed", ErrInvalidCatalog)
		}
		seen[field.Path] = true
	}
	if e.EffectClass.IsWrite() {
		return fmt.Errorf("%w: %s", ErrEffectRefused, e.Kind)
	}
	return nil
}

// Catalog publishes task kinds. Once a (Kind, Version) key is registered it
// can never be replaced.
type Catalog struct {
	mu      sync.RWMutex
	entries map[KindRef]CatalogEntry
}

// NewCatalog returns an empty catalog.
func NewCatalog() *Catalog { return &Catalog{entries: map[KindRef]CatalogEntry{}} }

// NewCatalogLegacyForTest builds a catalog from entries without the effect
// gate, so tests can prove pool declarations re-check effect classes on
// catalogs that predate the gate. Production code uses NewCatalog with
// Register, which refuses effect-carrying kinds at publication.
func NewCatalogLegacyForTest(entries ...CatalogEntry) *Catalog {
	catalog := NewCatalog()
	for _, entry := range entries {
		catalog.entries[KindRef{Kind: entry.Kind, Version: entry.Version}] = entry
	}
	return catalog
}

// Register publishes one kind. Duplicates refuse, and effect-carrying kinds
// refuse: the pool cannot add effects.
func (c *Catalog) Register(entry CatalogEntry) error {
	if err := entry.Validate(); err != nil {
		return err
	}
	ref := KindRef{Kind: entry.Kind, Version: entry.Version}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.entries[ref]; ok {
		return fmt.Errorf("%w: %s/%d", ErrKindExists, entry.Kind, entry.Version)
	}
	clone := entry
	clone.Outputs = append([]workflow.Field(nil), entry.Outputs...)
	c.entries[ref] = clone
	return nil
}

func (c *Catalog) lookup(ref KindRef) (CatalogEntry, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	entry, ok := c.entries[ref]
	return entry, ok
}

// PoolDeclaration is the compiled node fragment: the pool identity, the
// allow-listed kinds and the declared maximum.
type PoolDeclaration struct {
	Tenant     string
	InstanceID string
	NodeID     string
	Allow      []KindRef
	MaxTasks   int
}

// AuditEvent is one immutable pool fact about a task.
type AuditEvent struct {
	At     values.Instant
	Actor  string
	Action string
	Detail string
}

// PooledTask is one runtime-added task with its typed outputs and audit.
type PooledTask struct {
	TaskID   string
	Kind     string
	Version  uint32
	State    string
	Outputs  map[string]workflow.TypedValue
	Audit    []AuditEvent
	AddedBy  string
	AddedAt  values.Instant
	Resolved string
}

// Pool admits runtime-added tasks for one node. It is safe for concurrent
// use; Add serializes on the pool so the maximum cannot be overrun.
type Pool struct {
	mu       sync.Mutex
	tenant   string
	instance string
	node     string
	allow    map[KindRef]CatalogEntry
	max      int
	seq      int
	tasks    map[string]*PooledTask
	order    []string
	catalog  *Catalog
}

// NewPool compiles one pool declaration against its catalog. Every
// allow-listed kind must resolve, and every kind's effect class is re-checked
// here: a declaration can never smuggle an effect past publication.
func NewPool(declaration PoolDeclaration, catalog *Catalog) (*Pool, error) {
	if catalog == nil {
		return nil, fmt.Errorf("%w: catalog is required", ErrInvalidPool)
	}
	if strings.TrimSpace(declaration.Tenant) == "" || strings.TrimSpace(declaration.InstanceID) == "" || strings.TrimSpace(declaration.NodeID) == "" {
		return nil, fmt.Errorf("%w: tenant, instance and node are required", ErrInvalidPool)
	}
	if len(declaration.Allow) == 0 || declaration.MaxTasks < 1 {
		return nil, fmt.Errorf("%w: a non-empty allow-list and a positive maximum are required", ErrInvalidPool)
	}
	allow := make(map[KindRef]CatalogEntry, len(declaration.Allow))
	for _, ref := range declaration.Allow {
		entry, ok := catalog.lookup(ref)
		if !ok {
			return nil, fmt.Errorf("%w: %s/%d is not published", ErrKindNotAllowed, ref.Kind, ref.Version)
		}
		if entry.EffectClass.IsWrite() {
			return nil, fmt.Errorf("%w: %s", ErrEffectRefused, ref.Kind)
		}
		allow[ref] = entry
	}
	return &Pool{
		tenant: declaration.Tenant, instance: declaration.InstanceID, node: declaration.NodeID,
		allow: allow, max: declaration.MaxTasks,
		tasks: map[string]*PooledTask{}, catalog: catalog,
	}, nil
}

// AddRequest admits one runtime task to the pool.
type AddRequest struct {
	Tenant     string
	InstanceID string
	Kind       string
	Version    uint32
	AddedBy    string
	At         values.Instant
}

// Add admits one task. The kind must be allow-listed at its exact version,
// the pool must be under its maximum, and the request must name this pool's
// tenant and instance.
func (p *Pool) Add(req AddRequest) (PooledTask, error) {
	if req.Tenant != p.tenant || req.InstanceID != p.instance {
		return PooledTask{}, fmt.Errorf("%w: %s/%s", ErrPoolIdentity, req.Tenant, req.InstanceID)
	}
	if strings.TrimSpace(req.AddedBy) == "" || !req.At.IsSet() {
		return PooledTask{}, fmt.Errorf("%w: actor and instant are required", ErrInvalidPool)
	}
	entry, ok := p.allow[KindRef{Kind: req.Kind, Version: req.Version}]
	if !ok {
		return PooledTask{}, fmt.Errorf("%w: %s/%d", ErrKindNotAllowed, req.Kind, req.Version)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.tasks) >= p.max {
		return PooledTask{}, fmt.Errorf("%w: max %d", ErrPoolFull, p.max)
	}
	p.seq++
	task := &PooledTask{
		TaskID:  fmt.Sprintf("%s#%d", p.node, p.seq),
		Kind:    entry.Kind,
		Version: entry.Version,
		State:   TaskOpen,
		Outputs: map[string]workflow.TypedValue{},
		Audit: []AuditEvent{{
			At: req.At, Actor: req.AddedBy, Action: AuditAdded,
			Detail: fmt.Sprintf("admitted %s/%d", entry.Kind, entry.Version),
		}},
		AddedBy: req.AddedBy,
		AddedAt: req.At,
	}
	p.tasks[task.TaskID] = task
	p.order = append(p.order, task.TaskID)
	return *task, nil
}

// CompleteRequest finishes one pooled task with its typed outputs.
type CompleteRequest struct {
	Tenant      string
	InstanceID  string
	TaskID      string
	CompletedBy string
	At          values.Instant
	Outputs     map[string]workflow.TypedValue
}

// Complete records one task's typed outputs. Every declared output must be
// present with its declared type, and nothing undeclared: a mistyped or
// partial answer never completes the task.
func (p *Pool) Complete(req CompleteRequest) error {
	if req.Tenant != p.tenant || req.InstanceID != p.instance {
		return fmt.Errorf("%w: %s/%s", ErrPoolIdentity, req.Tenant, req.InstanceID)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	task, ok := p.tasks[req.TaskID]
	if !ok {
		return fmt.Errorf("%w: %s", ErrTaskNotFound, req.TaskID)
	}
	if task.State != TaskOpen {
		return fmt.Errorf("%w: %s", ErrTaskClosed, req.TaskID)
	}
	entry, ok := p.allow[KindRef{Kind: task.Kind, Version: task.Version}]
	if !ok {
		return fmt.Errorf("%w: %s/%d", ErrKindNotAllowed, task.Kind, task.Version)
	}
	validated := make(map[string]workflow.TypedValue, len(entry.Outputs))
	for _, field := range entry.Outputs {
		value, ok := req.Outputs[field.Path]
		if !ok {
			return fmt.Errorf("%w: output %q is missing", ErrOutputMismatch, field.Path)
		}
		if value.Type != field.Type {
			return fmt.Errorf("%w: output %q has type %v, want %v", ErrOutputMismatch, field.Path, value.Type, field.Type)
		}
		validated[field.Path] = value
	}
	for path := range req.Outputs {
		if _, ok := validated[path]; !ok {
			return fmt.Errorf("%w: output %q is not declared", ErrOutputMismatch, path)
		}
	}
	task.Outputs = validated
	task.State = TaskCompleted
	task.Audit = append(task.Audit, AuditEvent{At: req.At, Actor: req.CompletedBy, Action: AuditCompleted, Detail: fmt.Sprintf("completed %s", task.TaskID)})
	return nil
}

// Tasks returns the pool's tasks in admission order.
func (p *Pool) Tasks() []PooledTask {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]PooledTask, 0, len(p.order))
	for _, id := range p.order {
		task := *p.tasks[id]
		outputs := make(map[string]workflow.TypedValue, len(task.Outputs))
		for path, value := range task.Outputs {
			outputs[path] = value
		}
		task.Outputs = outputs
		task.Audit = append([]AuditEvent(nil), task.Audit...)
		out = append(out, task)
	}
	return out
}

// Digest seals the pool: identity, admission order, kinds, states, typed
// outputs and audit.
func (p *Pool) Digest() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	parts := []string{"taskpool", p.tenant, p.instance, p.node, fmt.Sprint(p.max)}
	for _, id := range p.order {
		task := p.tasks[id]
		paths := make([]string, 0, len(task.Outputs))
		for path := range task.Outputs {
			paths = append(paths, path)
		}
		sort.Strings(paths)
		parts = append(parts, strings.Join([]string{task.TaskID, task.Kind, fmt.Sprint(task.Version), task.State}, "\x01"))
		for _, path := range paths {
			value := task.Outputs[path]
			parts = append(parts, strings.Join([]string{path, value.Type.String(), value.Text}, "\x01"))
		}
		for _, event := range task.Audit {
			parts = append(parts, strings.Join([]string{event.Action, event.Actor, event.Detail}, "\x01"))
		}
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return "sha256:" + hex.EncodeToString(sum[:])
}
