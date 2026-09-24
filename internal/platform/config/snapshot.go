package config

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

var (
	// ErrInvalidParameterDefinition reports a malformed tenant parameter definition.
	ErrInvalidParameterDefinition = errors.New("config: invalid parameter definition")
	// ErrParameterConsumerDenied reports a consumer that is not allowed to read a parameter.
	ErrParameterConsumerDenied = errors.New("config: parameter consumer is not allowed")
	// ErrParameterTypeMismatch reports a requested type incompatible with the declared parameter type.
	ErrParameterTypeMismatch = errors.New("config: parameter type mismatch")
)

// ConsumerKind identifies one class of declared parameter consumer.
type ConsumerKind string

const (
	ConsumerWorkflow   ConsumerKind = "WORKFLOW"
	ConsumerPackage    ConsumerKind = "PACKAGE"
	ConsumerCapability ConsumerKind = "CAPABILITY"
)

func (k ConsumerKind) valid() bool {
	switch k {
	case ConsumerWorkflow, ConsumerPackage, ConsumerCapability:
		return true
	default:
		return false
	}
}

// Consumer identifies a workflow, package, or capability permitted to read a parameter.
type Consumer struct {
	Kind ConsumerKind `json:"kind"`
	ID   string       `json:"id"`
}

// ParameterDefault is the authored, typed default for a parameter. Its Type
// must match the enclosing definition; Value remains an opaque canonical
// representation for the owning type system to interpret.
type ParameterDefault struct {
	Type  workflow.ValueType `json:"type"`
	Value string             `json:"value"`
}

// ParameterDefinition describes a namespaced tenant setting and the exact
// workflow type and consumers permitted to use it. Type uses the workflow
// kernel's type algebra so config does not define a competing type system.
// SecretReference marks an opaque, branded reference; secret material and
// fingerprints do not belong in a parameter definition.
type ParameterDefinition struct {
	Key              string             `json:"key"`
	Type             workflow.ValueType `json:"type"`
	Classification   string             `json:"classification"`
	Owner            string             `json:"owner"`
	Default          *ParameterDefault  `json:"default,omitempty"`
	Required         bool               `json:"required"`
	HighImpact       bool               `json:"high_impact"`
	AllowedConsumers []Consumer         `json:"allowed_consumers"`
	SecretReference  bool               `json:"secret_reference,omitempty"`
}

// Validate checks the complete authored definition. Untyped MAP and FLOAT
// are not workflow types, so ValueType.Validate rejects them at this boundary
// while legacy generic config entries retain their existing kinds.
func (d ParameterDefinition) Validate() error {
	if !validParameterKey(d.Key) {
		return fmt.Errorf("%w: key must be lowercase and namespaced", ErrInvalidParameterDefinition)
	}
	if strings.TrimSpace(d.Classification) == "" || strings.TrimSpace(d.Classification) != d.Classification {
		return fmt.Errorf("%w: classification is required", ErrInvalidParameterDefinition)
	}
	if strings.TrimSpace(d.Owner) == "" || strings.TrimSpace(d.Owner) != d.Owner {
		return fmt.Errorf("%w: owner is required", ErrInvalidParameterDefinition)
	}
	if err := d.Type.Validate(); err != nil {
		return fmt.Errorf("%w: type: %v", ErrInvalidParameterDefinition, err)
	}
	if d.Type.Kind == workflow.KindString && d.Type.Brand == "SecretReference" && !d.SecretReference {
		return fmt.Errorf("%w: SecretReference brand requires secret reference rules", ErrInvalidParameterDefinition)
	}
	if d.SecretReference && (d.Type.Kind != workflow.KindString || d.Type.Brand != "SecretReference" || d.Default != nil) {
		return fmt.Errorf("%w: secret references require branded string type and no default", ErrInvalidParameterDefinition)
	}
	if d.Default != nil {
		if err := d.Default.Type.Validate(); err != nil {
			return fmt.Errorf("%w: default type: %v", ErrInvalidParameterDefinition, err)
		}
		if err := sameWorkflowType(d.Type, d.Default.Type); err != nil {
			return fmt.Errorf("%w: default type: %v", ErrInvalidParameterDefinition, err)
		}
	}
	if len(d.AllowedConsumers) == 0 {
		return fmt.Errorf("%w: at least one allowed consumer is required", ErrInvalidParameterDefinition)
	}
	seen := make(map[Consumer]struct{}, len(d.AllowedConsumers))
	for _, consumer := range d.AllowedConsumers {
		if !consumer.Kind.valid() || strings.TrimSpace(consumer.ID) == "" || strings.TrimSpace(consumer.ID) != consumer.ID {
			return fmt.Errorf("%w: consumer kind and id are required", ErrInvalidParameterDefinition)
		}
		if _, duplicate := seen[consumer]; duplicate {
			return fmt.Errorf("%w: duplicate consumer %s:%s", ErrInvalidParameterDefinition, consumer.Kind, consumer.ID)
		}
		seen[consumer] = struct{}{}
	}
	return nil
}

// CheckRead verifies that consumer is allowlisted and the workflow-requested
// type can accept the parameter's declared type without a coercion.
func (d ParameterDefinition) CheckRead(consumer Consumer, requestedType workflow.ValueType) error {
	if err := d.Validate(); err != nil {
		return err
	}
	allowed := false
	for _, permitted := range d.AllowedConsumers {
		if permitted == consumer {
			allowed = true
			break
		}
	}
	if !allowed {
		return fmt.Errorf("%w: %s:%s cannot read %s", ErrParameterConsumerDenied, consumer.Kind, consumer.ID, d.Key)
	}
	if err := requestedType.Validate(); err != nil || sameWorkflowType(d.Type, requestedType) != nil {
		return fmt.Errorf("%w: %s cannot be read as %s", ErrParameterTypeMismatch, d.Type, requestedType)
	}
	return nil
}

func validParameterKey(key string) bool {
	if strings.TrimSpace(key) != key || key == "" {
		return false
	}
	parts := strings.Split(key, ".")
	if len(parts) < 2 {
		return false
	}
	for _, part := range parts {
		if part == "" || part[0] < 'a' || part[0] > 'z' {
			return false
		}
		for _, r := range part[1:] {
			if !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-') {
				return false
			}
		}
	}
	return true
}

func sameWorkflowType(a, b workflow.ValueType) error {
	if err := a.AssignableTo(b); err != nil {
		return err
	}
	return b.AssignableTo(a)
}

func cloneWorkflowType(typ workflow.ValueType) workflow.ValueType {
	if typ.Element != nil {
		element := cloneWorkflowType(*typ.Element)
		typ.Element = &element
	}
	return typ
}

func cloneParameterDefinition(definition ParameterDefinition) ParameterDefinition {
	definition.Type = cloneWorkflowType(definition.Type)
	if definition.Default != nil {
		value := *definition.Default
		value.Type = cloneWorkflowType(value.Type)
		definition.Default = &value
	}
	definition.AllowedConsumers = append([]Consumer(nil), definition.AllowedConsumers...)
	sort.Slice(definition.AllowedConsumers, func(i, j int) bool {
		if definition.AllowedConsumers[i].Kind != definition.AllowedConsumers[j].Kind {
			return definition.AllowedConsumers[i].Kind < definition.AllowedConsumers[j].Kind
		}
		return definition.AllowedConsumers[i].ID < definition.AllowedConsumers[j].ID
	})
	return definition
}

// ValueKind is the typed kind of a configuration value. A [Snapshot] never
// carries an untyped value: CONFIG-001 requires typed keys so that a type
// change between two snapshots is always detectable, even when the textual
// representation of the two values happens to look similar.
type ValueKind uint8

// Declared value kinds. The zero value, KindUnspecified, is never legal on a
// constructed [Entry].
const (
	KindUnspecified ValueKind = iota
	KindString
	KindInt
	KindBool
	KindFloat
	KindDuration
	KindList
	KindMap
	// KindSecretRef marks an entry whose value is a secret. Such an entry
	// never carries Value; see [NewSecretEntry].
	KindSecretRef
)

var valueKindWire = map[ValueKind]string{
	KindString:    "STRING",
	KindInt:       "INT",
	KindBool:      "BOOL",
	KindFloat:     "FLOAT",
	KindDuration:  "DURATION",
	KindList:      "LIST",
	KindMap:       "MAP",
	KindSecretRef: "SECRET_REF",
}

// String returns the stable wire token.
func (k ValueKind) String() string {
	if w, ok := valueKindWire[k]; ok {
		return w
	}
	return "VALUE_KIND_UNSPECIFIED"
}

// Valid reports whether k is a declared kind.
func (k ValueKind) Valid() bool {
	_, ok := valueKindWire[k]
	return ok
}

// SemanticClass names the domain a configuration entry participates in. An
// entry outside SemanticGeneric can never be classified Compatible purely
// because its textual value changed: a workflow, schema, mapping, or policy
// pointer governs behavior other components rely on, so a change to one of
// them is always [CompatibilityBreaking]. This is the mechanism that keeps
// a semantic workflow/policy/schema/mapping change from being hidden inside
// an otherwise-quiet diff.
type SemanticClass uint8

// Declared semantic classes.
const (
	// SemanticGeneric is an ordinary preference or tuning value with no
	// special downstream binding.
	SemanticGeneric SemanticClass = iota
	SemanticWorkflow
	SemanticSchema
	SemanticMapping
	SemanticPolicy
)

var semanticClassWire = map[SemanticClass]string{
	SemanticGeneric:  "GENERIC",
	SemanticWorkflow: "WORKFLOW",
	SemanticSchema:   "SCHEMA",
	SemanticMapping:  "MAPPING",
	SemanticPolicy:   "POLICY",
}

// String returns the stable wire token.
func (s SemanticClass) String() string {
	if w, ok := semanticClassWire[s]; ok {
		return w
	}
	return "SEMANTIC_CLASS_UNSPECIFIED"
}

// Refs names the capability, workflow, and tenant identifiers a
// configuration entry's value references. Diff surfaces these as the
// "referenced capability/workflow/tenant impacts" of a change; a change to
// any of them is always [CompatibilityBreaking], regardless of the entry's
// [SemanticClass].
type Refs struct {
	Capabilities []string
	Workflows    []string
	Tenants      []string
}

func normalizeRefs(ss []string) []string {
	if len(ss) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(ss))
	out := make([]string, 0, len(ss))
	for _, s := range ss {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

func (r Refs) normalized() Refs {
	return Refs{
		Capabilities: normalizeRefs(r.Capabilities),
		Workflows:    normalizeRefs(r.Workflows),
		Tenants:      normalizeRefs(r.Tenants),
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Entry is one typed configuration value. Construct it with [NewEntry] or
// [NewSecretEntry]; the zero Entry is invalid.
//
// A secret-kind entry never carries Value: only SecretFingerprint, an
// opaque comparison token the owner derives out of band (for example a
// salted hash of the secret material, or the version id of a vault
// reference). [NewSecretEntry] is the only constructor that can produce a
// KindSecretRef entry, and it refuses a caller that also supplies a literal
// value. [NewSnapshot] independently re-checks this invariant on every
// entry it is given, because Entry's fields are plain and exported: nothing
// stops a caller from building one with a struct literal instead of a
// constructor, and the platform cannot rely on every caller using the
// constructor.
type Entry struct {
	Key      string
	Kind     ValueKind
	Value    string
	Explicit bool
	Semantic SemanticClass

	// SecretFingerprint is set only when Kind is KindSecretRef.
	SecretFingerprint string

	Refs Refs
}

// NewEntry builds a non-secret typed entry.
func NewEntry(key string, kind ValueKind, value string, explicit bool, semantic SemanticClass, refs Refs) (Entry, error) {
	if key == "" {
		return Entry{}, newError("NewEntry", ErrEmptyKey, "")
	}
	if !kind.Valid() || kind == KindSecretRef {
		return Entry{}, newError("NewEntry", ErrInvalidKind, "%s", kind)
	}
	return Entry{
		Key:      key,
		Kind:     kind,
		Value:    value,
		Explicit: explicit,
		Semantic: semantic,
		Refs:     refs.normalized(),
	}, nil
}

// NewSecretEntry builds a secret-reference entry. fingerprint must be
// non-empty; it is compared as an opaque token, and a [Change] reports only
// whether it changed, never the fingerprint or the underlying secret.
func NewSecretEntry(key, fingerprint string, explicit bool, semantic SemanticClass, refs Refs) (Entry, error) {
	if key == "" {
		return Entry{}, newError("NewSecretEntry", ErrEmptyKey, "")
	}
	if fingerprint == "" {
		return Entry{}, newError("NewSecretEntry", ErrMissingFingerprint, "key %q", key)
	}
	return Entry{
		Key:               key,
		Kind:              KindSecretRef,
		SecretFingerprint: fingerprint,
		Explicit:          explicit,
		Semantic:          semantic,
		Refs:              refs.normalized(),
	}, nil
}

func (e Entry) validate() error {
	if e.Key == "" {
		return newError("Entry.validate", ErrEmptyKey, "")
	}
	if !e.Kind.Valid() {
		return newError("Entry.validate", ErrInvalidKind, "key %q", e.Key)
	}
	if e.Kind == KindSecretRef {
		if e.SecretFingerprint == "" {
			return newError("Entry.validate", ErrMissingFingerprint, "key %q", e.Key)
		}
		if e.Value != "" {
			return newError("Entry.validate", ErrSecretValueLeak, "key %q", e.Key)
		}
	}
	return nil
}

// Snapshot is an immutable set of typed configuration entries, keyed by
// their Key. Two snapshots built from the same entries in different
// construction order compare and hash identically: [Diff] and a snapshot's
// own [Snapshot.Entries] are always produced sorted by key, never by
// insertion or map iteration order.
type Snapshot struct {
	Name                 string
	Version              string
	entries              map[string]Entry
	parameterDefinitions map[string]ParameterDefinition
}

// NewSnapshot validates and indexes entries by key. It rejects an invalid
// entry, a duplicate key, and — independently of [NewSecretEntry] — any
// secret-kind entry that carries a literal value, so a caller cannot bypass
// that invariant by building an Entry directly.
func NewSnapshot(name, version string, entries []Entry) (Snapshot, error) {
	m := make(map[string]Entry, len(entries))
	for _, e := range entries {
		if err := e.validate(); err != nil {
			return Snapshot{}, err
		}
		if _, dup := m[e.Key]; dup {
			return Snapshot{}, newError("NewSnapshot", ErrDuplicateKey, "%q", e.Key)
		}
		e.Refs = e.Refs.normalized()
		m[e.Key] = e
	}
	return Snapshot{Name: name, Version: version, entries: m}, nil
}

// NewSnapshotWithDefinitions builds a snapshot that also carries immutable
// parameter definitions. The existing NewSnapshot constructor remains the
// entry-only form for current CONFIG-001 callers.
func NewSnapshotWithDefinitions(name, version string, entries []Entry, definitions []ParameterDefinition) (Snapshot, error) {
	snapshot, err := NewSnapshot(name, version, entries)
	if err != nil {
		return Snapshot{}, err
	}
	return snapshot.WithParameterDefinitions(definitions)
}

// WithParameterDefinitions returns a snapshot copy with validated parameter
// definitions indexed by key. It never mutates the receiver or caller slices.
func (s Snapshot) WithParameterDefinitions(definitions []ParameterDefinition) (Snapshot, error) {
	indexed := make(map[string]ParameterDefinition, len(definitions))
	for _, definition := range definitions {
		if err := definition.Validate(); err != nil {
			return Snapshot{}, err
		}
		if _, duplicate := indexed[definition.Key]; duplicate {
			return Snapshot{}, fmt.Errorf("%w: duplicate key %q", ErrInvalidParameterDefinition, definition.Key)
		}
		indexed[definition.Key] = cloneParameterDefinition(definition)
	}
	snapshot := s
	snapshot.parameterDefinitions = indexed
	return snapshot, nil
}

// ParameterDefinitions returns every definition in key order. Returned
// definitions are deep copies, including list element types and consumers.
func (s Snapshot) ParameterDefinitions() []ParameterDefinition {
	keys := make([]string, 0, len(s.parameterDefinitions))
	for key := range s.parameterDefinitions {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]ParameterDefinition, 0, len(keys))
	for _, key := range keys {
		out = append(out, cloneParameterDefinition(s.parameterDefinitions[key]))
	}
	return out
}

// ParameterDefinition returns a deep copy of the definition for key.
func (s Snapshot) ParameterDefinition(key string) (ParameterDefinition, bool) {
	definition, ok := s.parameterDefinitions[key]
	if !ok {
		return ParameterDefinition{}, false
	}
	return cloneParameterDefinition(definition), true
}

// Entries returns every entry, sorted by key. The returned slice is a copy;
// mutating it never changes the Snapshot.
func (s Snapshot) Entries() []Entry {
	out := make([]Entry, 0, len(s.entries))
	for _, e := range s.entries {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

// Len returns the number of entries in the snapshot.
func (s Snapshot) Len() int { return len(s.entries) }
