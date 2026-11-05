// Package docquery implements WF-DATA-016: governed document types plus a
// bounded query builder over the datasource fabric.
//
// Documents that enter the system from foreign sources must be checked
// against a registered schema before they are trusted: every field of an
// unregistered document type, or a value that does not match its declared
// wire type, quarantines the document with a machine-readable reason rather
// than failing the whole batch.
//
// Queries are expressed with the Predicate builders (Eq, Range) and executed
// through Registry.Query. Only equality and range operators are emitted, and
// only over fields the document type declares Indexed. Arbitrary JSON is
// never spliced into predicates: values must be JSON scalars, and operator
// keys that do not come from this package's builders are rejected before the
// query reaches a store.
package docquery

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/datasource"
)

// Classification is the data-classification label attached to a document
// type. The vocabulary is intentionally small: every label maps to exactly
// one retention/exposure posture understood by downstream stores.
type Classification string

// Document-type classifications. Public documents may leave the tenant
// boundary; Internal documents stay inside it; Confidential documents
// require field-level access review before export; Restricted documents
// (credentials, keys, biometric templates) never leave the vault store.
const (
	ClassificationPublic       Classification = "public"
	ClassificationInternal     Classification = "internal"
	ClassificationConfidential Classification = "confidential"
	ClassificationRestricted   Classification = "restricted"
)

// validClassifications is the closed vocabulary for Classification.
var validClassifications = map[Classification]bool{
	ClassificationPublic:       true,
	ClassificationInternal:     true,
	ClassificationConfidential: true,
	ClassificationRestricted:   true,
}

// Field declares one governed field of a document type.
type Field struct {
	// Name is the JSON key of the field in canonical documents.
	Name string
	// WireType is the schema wire type (see internal/customobject
	// supportedTypes) the field's values must validate against.
	WireType string
	// Indexed reports whether the field carries a store index and may
	// therefore appear in Query predicates. Non-indexed fields can be
	// stored and returned but never queried on: unbounded collection
	// scans are a denial-of-service vector on large tenants.
	Indexed bool
	// Required reports whether a document missing this field (or
	// carrying an explicit null for it) quarantines instead of storing.
	Required bool
}

// DocumentType is a registered, governed document schema.
type DocumentType struct {
	// Name is the registry key, e.g. "hris.employee.v1".
	Name string
	// Classification labels every document of this type.
	Classification Classification
	// Fields are the declared fields. Documents carrying undeclared
	// fields quarantine (schema drift from a foreign source must be an
	// explicit registration decision, never silent acceptance).
	Fields []Field

	index map[string]Field
}

// fieldNames returns the sorted declared field names.
func (t *DocumentType) fieldNames() []string {
	names := make([]string, 0, len(t.Fields))
	for _, f := range t.Fields {
		names = append(names, f.Name)
	}
	sort.Strings(names)
	return names
}

// indexed reports whether name is a declared indexed field.
func (t *DocumentType) indexed(name string) bool {
	f, ok := t.index[name]
	return ok && f.Indexed
}

// Registry is a concurrency-safe store of governed document types plus
// the quarantine ledger for rejected documents.
type Registry struct {
	mu         sync.RWMutex
	types      map[string]*DocumentType
	quarantine []QuarantinedDocument
}

// NewRegistry returns an empty document-type registry.
func NewRegistry() *Registry {
	return &Registry{types: map[string]*DocumentType{}}
}

// typeNameRE constrains document-type names to dotted lowercase segments
// so a type name can never smuggle a path separator, whitespace, or a
// query operator into store calls.
var typeNameRE = regexp.MustCompile(`^[a-z][a-z0-9]*(\.[a-z][a-z0-9]*)*$`)

// fieldNameRE constrains field names to the identifier shape stores accept.
var fieldNameRE = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*$`)

// Register adds a document type to the registry. Re-registering a name
// is an error: schemas are immutable once published, and a drifted
// re-registration must be an explicit new version (a new name), never a
// silent overwrite.
func (r *Registry) Register(t DocumentType) error {
	if !typeNameRE.MatchString(t.Name) {
		return fmt.Errorf("%w: document type %q", ErrInvalidTypeName, t.Name)
	}
	if !validClassifications[t.Classification] {
		return fmt.Errorf("%w: %q", ErrInvalidClassification, string(t.Classification))
	}
	if len(t.Fields) == 0 {
		return fmt.Errorf("%w: %q declares no fields", ErrInvalidSchema, t.Name)
	}
	seen := map[string]bool{}
	for _, f := range t.Fields {
		if !fieldNameRE.MatchString(f.Name) {
			return fmt.Errorf("%w: %q", ErrInvalidFieldName, f.Name)
		}
		if seen[f.Name] {
			return fmt.Errorf("%w: duplicate field %q in %q", ErrInvalidSchema, f.Name, t.Name)
		}
		seen[f.Name] = true
		if !supportedWireType(f.WireType) {
			return fmt.Errorf("%w: field %q declares %q", ErrUnknownWireType, f.Name, f.WireType)
		}
	}
	nt := t
	nt.index = make(map[string]Field, len(t.Fields))
	for _, f := range t.Fields {
		nt.index[f.Name] = f
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, dup := r.types[t.Name]; dup {
		return fmt.Errorf("%w: %q", ErrTypeExists, t.Name)
	}
	r.types[t.Name] = &nt
	return nil
}

// Lookup returns the registered document type or ErrUnknownType.
func (r *Registry) Lookup(name string) (*DocumentType, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.types[name]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownType, name)
	}
	return t, nil
}

// Types returns the registered type names in ascending order.
func (r *Registry) Types() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.types))
	for name := range r.types {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Quarantine returns a copy of the quarantine ledger: every document this
// registry refused, with the reason. Ledger entries are evidence for the
// foreign-source operator, never an error return that fails a batch.
func (r *Registry) Quarantine() []QuarantinedDocument {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]QuarantinedDocument, len(r.quarantine))
	copy(out, r.quarantine)
	return out
}

// recordQuarantine appends to the ledger. Callers hold no lock.
func (r *Registry) recordQuarantine(q QuarantinedDocument) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.quarantine = append(r.quarantine, q)
}

// SourceOf routes a registered type to its datasource connector name.
// Restricted types route to the vault store; everything else routes to the
// default document store. The mapping is closed: an unregistered type is an
// error, never a guess.
func (r *Registry) SourceOf(typeName string) (string, error) {
	t, err := r.Lookup(typeName)
	if err != nil {
		return "", err
	}
	if t.Classification == ClassificationRestricted {
		return VaultSource, nil
	}
	return DefaultSource, nil
}

// Connector source names routed by classification.
const (
	// DefaultSource is the datasource connector serving non-restricted
	// document reads.
	DefaultSource = "docstore"
	// VaultSource is the datasource connector serving restricted
	// document reads.
	VaultSource = "docstore-vault"
)

// wireShape classes group customobject wire types by their canonical-JSON
// spelling so ValidateDocument can check values without importing the
// schema compiler.
var (
	stringKind = map[string]bool{
		"string": true, "EntityId": true, "EntityRef": true,
		"ResourceKey": true, "RevisionToken": true,
		"LocalDate": true, "LocalTime": true, "TemporalInterval": true,
	}
	instantKind = map[string]bool{
		"TemporalInstant": true, "ZonedDateTime": true,
		"RecordedAt": true, "KnownAt": true,
	}
	boolKind = map[string]bool{
		"bool": true, "PresenceValue": true,
	}
	intKind = map[string]bool{
		"int32": true, "uint32": true, "int64": true,
	}
	decimalKind = map[string]bool{
		"FixedDecimal": true, "Money": true, "Percentage": true,
		"Quantity": true, "Rate": true,
	}
)

// supportedWireType reports whether wire is a wire type docquery can
// validate values against. The set mirrors internal/customobject's
// supportedTypes; the two must stay in sync (see TestWireTypeParity).
func supportedWireType(wire string) bool {
	switch {
	case stringKind[wire], instantKind[wire], boolKind[wire],
		intKind[wire], decimalKind[wire]:
		return true
	case wire == "[]string", wire == "[]byte":
		return true
	case strings.HasPrefix(wire, "[]"):
		// Slices of primitives validate element-wise against the
		// element wire type.
		return supportedWireType(strings.TrimPrefix(wire, "[]"))
	case strings.HasPrefix(wire, "map[string]"):
		// Maps validate value-wise against the value wire type.
		return supportedWireType(strings.TrimPrefix(wire, "map[string]"))
	default:
		return false
	}
}

// datasourceRef keeps the datasource import referenced for the query
// execution seam.
var _ = datasource.ErrInvalidQuery
