// Package custom implements domain-scoped custom object definitions with
// typed fields, relationships, effective dating, and classification-aware
// authorization policies. It is the declared domains root for tenant-custom
// types that require per-field AuthZ, classification, residency and retention.
package custom

import (
	"errors"
	"fmt"
	"sort"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

const (
	customObjectDefinitionSchema       = "hcmnext.domains.custom.ObjectDefinition"
	customRelationshipDefinitionSchema = "hcmnext.domains.custom.RelationshipDefinition"
	customRecordRevisionSchema         = "hcmnext.domains.custom.RecordRevision"
)

var (
	ErrInvalidObjectDefinition = errors.New("custom: invalid object definition")
	ErrDanglingField           = errors.New("custom: dangling or unclassified field")
	ErrInvalidRelationship     = errors.New("custom: invalid relationship definition")
	ErrInvalidRecordRevision   = errors.New("custom: invalid record revision")
	ErrUnclassifiedField       = errors.New("custom: field missing classification")
	ErrMissingAuthZDomain      = errors.New("custom: field missing required authz domain")
	ErrInvalidCardinality      = errors.New("custom: invalid relationship cardinality")
)

// Cardinality bounds the relationship edge multiplicity at any effective instant.
type Cardinality string

const (
	CardinalityOneToOne   Cardinality = "ONE_TO_ONE"
	CardinalityOneToMany  Cardinality = "ONE_TO_MANY"
	CardinalityManyToMany Cardinality = "MANY_TO_MANY"
)

// Valid reports whether c is a declared cardinality.
func (c Cardinality) Valid() bool {
	return c == CardinalityOneToOne || c == CardinalityOneToMany || c == CardinalityManyToMany
}

// FieldClassification declares the sensitive-data domain and retention class
// for one field.
type FieldClassification struct {
	// AuthZDomain is the policy domain a principal must hold a grant for to
	// access this field (e.g. "worker.compensation", "worker.bank").
	// It must be a declared DataDomain from internal/trust/authz.
	AuthZDomain string
	// Classification names the sensitive-data label on this field
	// (e.g. "CONFIDENTIAL", "RESTRICTED", "PUBLIC").
	Classification string
	// ResidencyRef names the residency compliance rule binding this field
	// (e.g. "EU_GDPR", "CCPA", "LOCAL_JURISDICTION").
	ResidencyRef string
	// RetentionClass names the records-management disposition for this field
	// (e.g. "PERMANENT", "OPERATIONAL", "TEMPORARY").
	RetentionClass string
}

// Validate reports whether the classification is complete and valid.
func (fc FieldClassification) Validate() error {
	if fc.AuthZDomain == "" {
		return fmt.Errorf("%w: authz_domain required", ErrMissingAuthZDomain)
	}
	if fc.Classification == "" {
		return fmt.Errorf("%w: classification required", ErrUnclassifiedField)
	}
	if fc.ResidencyRef == "" {
		return fmt.Errorf("%w: residency_ref required", ErrMissingAuthZDomain)
	}
	if fc.RetentionClass == "" {
		return fmt.Errorf("%w: retention_class required", ErrMissingAuthZDomain)
	}
	return nil
}

// Canonical returns the deterministic byte encoding of the field classification.
func (fc FieldClassification) Canonical() []byte {
	w := canonicalbytes.New("hcmnext.domains.custom.FieldClassification", 1)
	w.String("authz_domain", fc.AuthZDomain).
		String("classification", fc.Classification).
		String("residency_ref", fc.ResidencyRef).
		String("retention_class", fc.RetentionClass)
	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// CustomObjectDefinition declares a tenant-scoped custom type with typed,
// classified fields. Every field must carry complete classification and
// retention directives.
type CustomObjectDefinition struct {
	// Kind names the custom object type (e.g. "VendorProfile", "ProjectAsset").
	Kind string
	// Namespace organizes definitions by business domain
	// (e.g. "procurement", "project_management").
	Namespace string
	// Version is the immutable schema generation (incremented on breaking changes).
	Version uint64
	// Fields maps field names to their types and classification rules.
	// Every field in the map must have valid classification.
	Fields map[string]FieldDefinition
	// RecordRules declares record-level access by owner, scope and
	// relationship. Empty means the platform policy alone decides.
	RecordRules []RecordRule
}

// FieldDefinition declares one field's type and classification.
type FieldDefinition struct {
	Type           string
	Classification FieldClassification
	// Kind extends a text field with governed behavior (WF-DATA-031):
	// sequence numbers, reference-data pick lists or pinned attachments.
	// Blank is a plain field. Exactly the matching configuration is set.
	Kind       FieldKind
	Sequence   *SequenceConfig
	Reference  *ReferenceConfig
	Attachment *AttachmentConfig
	// Derived computes the field from a bounded expression (WF-DATA-021)
	// instead of storing a caller value. Nil stores caller values. A
	// derived field is always plain: governed kinds carry caller or
	// allocator values, never computed ones.
	Derived *DerivedSpec
}

// Validate reports whether the definition is structurally sound and
// completely classified.
func (d CustomObjectDefinition) Validate() error {
	if d.Kind == "" {
		return fmt.Errorf("%w: kind required", ErrInvalidObjectDefinition)
	}
	if d.Namespace == "" {
		return fmt.Errorf("%w: namespace required", ErrInvalidObjectDefinition)
	}
	if d.Version == 0 {
		return fmt.Errorf("%w: version must be >= 1", ErrInvalidObjectDefinition)
	}
	if len(d.Fields) == 0 {
		return fmt.Errorf("%w: at least one field required", ErrInvalidObjectDefinition)
	}
	for fieldName, fieldDef := range d.Fields {
		if fieldName == "" {
			return fmt.Errorf("%w: field name is empty", ErrInvalidObjectDefinition)
		}
		if fieldDef.Type == "" {
			return fmt.Errorf("%w: field %q has no type", ErrInvalidObjectDefinition, fieldName)
		}
		if err := fieldDef.Classification.Validate(); err != nil {
			return fmt.Errorf("%w: field %q: %v", ErrDanglingField, fieldName, err)
		}
		if err := ValidateFieldKind(fieldName, fieldDef); err != nil {
			return err
		}
		if err := validateDerivedLocal(fieldName, d.Fields, fieldDef); err != nil {
			return err
		}
	}
	if err := validateDerivedGraph(d.Fields); err != nil {
		return err
	}
	return validateRecordRules(d)
}

// Canonical returns the deterministic byte encoding of the definition.
func (d CustomObjectDefinition) Canonical() []byte {
	w := canonicalbytes.New(customObjectDefinitionSchema, 1)
	w.String("kind", d.Kind).
		String("namespace", d.Namespace).
		Int("version", int64(d.Version))

	// Sort field names for determinism.
	fieldNames := make([]string, 0, len(d.Fields))
	for name := range d.Fields {
		fieldNames = append(fieldNames, name)
	}
	sort.Strings(fieldNames)

	for _, name := range fieldNames {
		fieldDef := d.Fields[name]
		w.String(fmt.Sprintf("fields.%s.type", name), fieldDef.Type).
			Value(fmt.Sprintf("fields.%s.classification", name), fieldDef.Classification)
		encodeFieldKind(w, name, fieldDef)
		encodeDerived(w, name, fieldDef)
	}

	for i, rule := range canonicalRecordRules(d.RecordRules) {
		w.String(fmt.Sprintf("record_rules.%d", i), rule)
	}

	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// Digest returns the hex digest of the canonical definition encoding.
func (d CustomObjectDefinition) Digest() string {
	w := canonicalbytes.New(customObjectDefinitionSchema, 1)
	w.String("kind", d.Kind).
		String("namespace", d.Namespace).
		Int("version", int64(d.Version))

	// Sort field names for determinism.
	fieldNames := make([]string, 0, len(d.Fields))
	for name := range d.Fields {
		fieldNames = append(fieldNames, name)
	}
	sort.Strings(fieldNames)

	for _, name := range fieldNames {
		fieldDef := d.Fields[name]
		w.String(fmt.Sprintf("fields.%s.type", name), fieldDef.Type).
			Value(fmt.Sprintf("fields.%s.classification", name), fieldDef.Classification)
		encodeFieldKind(w, name, fieldDef)
		encodeDerived(w, name, fieldDef)
	}

	for i, rule := range canonicalRecordRules(d.RecordRules) {
		w.String(fmt.Sprintf("record_rules.%d", i), rule)
	}

	digest, err := w.Digest()
	if err != nil {
		return ""
	}
	return digest
}

// CustomRelationshipDefinition declares a typed relationship between two
// custom object kinds with effective dating and lifecycle semantics.
type CustomRelationshipDefinition struct {
	// Name identifies this relationship (e.g. "VendorProject", "AssetLocation").
	Name string
	// Namespace organizes relationships by business domain.
	Namespace string
	// Version is the immutable schema generation.
	Version uint64
	// SourceKind and TargetKind name the custom object types at each end.
	SourceKind string
	TargetKind string
	// Cardinality constrains multiplicity at any effective instant.
	Cardinality Cardinality
	// EffectiveDateRule declares how the relationship's temporal interval is governed
	// (e.g. "CALENDAR_DATE_INTERVAL" for LocalDate, "INSTANT_INTERVAL" for UTC instants).
	EffectiveDateRule string
	// AllowCycles reports whether the relationship may form circular references.
	AllowCycles bool
}

// Validate reports whether the definition is structurally sound.
func (d CustomRelationshipDefinition) Validate() error {
	if d.Name == "" {
		return fmt.Errorf("%w: name required", ErrInvalidRelationship)
	}
	if d.Namespace == "" {
		return fmt.Errorf("%w: namespace required", ErrInvalidRelationship)
	}
	if d.Version == 0 {
		return fmt.Errorf("%w: version must be >= 1", ErrInvalidRelationship)
	}
	if d.SourceKind == "" || d.TargetKind == "" {
		return fmt.Errorf("%w: source and target kinds required", ErrInvalidRelationship)
	}
	if !d.Cardinality.Valid() {
		return fmt.Errorf("%w: %q", ErrInvalidCardinality, d.Cardinality)
	}
	if d.EffectiveDateRule == "" {
		return fmt.Errorf("%w: effective_date_rule required", ErrInvalidRelationship)
	}
	return nil
}

// Canonical returns the deterministic byte encoding of the definition.
func (d CustomRelationshipDefinition) Canonical() []byte {
	w := canonicalbytes.New(customRelationshipDefinitionSchema, 1)
	w.String("name", d.Name).
		String("namespace", d.Namespace).
		Int("version", int64(d.Version)).
		String("source_kind", d.SourceKind).
		String("target_kind", d.TargetKind).
		String("cardinality", string(d.Cardinality)).
		String("effective_date_rule", d.EffectiveDateRule).
		Bool("allow_cycles", d.AllowCycles)

	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// Digest returns the hex digest of the canonical definition encoding.
func (d CustomRelationshipDefinition) Digest() string {
	w := canonicalbytes.New(customRelationshipDefinitionSchema, 1)
	w.String("name", d.Name).
		String("namespace", d.Namespace).
		Int("version", int64(d.Version)).
		String("source_kind", d.SourceKind).
		String("target_kind", d.TargetKind).
		String("cardinality", string(d.Cardinality)).
		String("effective_date_rule", d.EffectiveDateRule).
		Bool("allow_cycles", d.AllowCycles)

	digest, err := w.Digest()
	if err != nil {
		return ""
	}
	return digest
}

// TypedValue wraps a field value with its type name for safe, schema-aware
// serialization and policy evaluation.
type TypedValue struct {
	// FieldName is the key in the CustomObjectDefinition.Fields map.
	FieldName string
	// Type echoes the declared type for validation and schema evolution.
	Type string
	// Value holds the scalar or structured value. For policy evaluation, the
	// actual type must match the field's declared type.
	Value any
}

// CustomRecordRevision is the server-held effective-dated record of field
// values for one custom object. It tracks the interval the revision is
// current, bound to a definition version, and carries references to all
// field values (not inline data, so large/mutable fields don't bloat the
// revision record).
type CustomRecordRevision struct {
	// ObjectID identifies the record instance.
	ObjectID string
	// ObjectKind and Namespace identify the definition this revision conforms to.
	ObjectKind string
	Namespace  string
	// DefinitionVersion is the immutable schema version at publication.
	DefinitionVersion uint64
	// Effective is the half-open interval this revision is current: [start, end).
	// start is inclusive, end is exclusive; end may be zero-time to indicate
	// open-ended (still effective).
	Effective values.EffectiveInterval
	// Recorded and Known are optional bitemporal boundaries. Recorded is when
	// the revision was created; Known is as-of which clock it is valid.
	Recorded values.RecordedAt
	Known    values.KnownAt
	// FieldValues maps FieldName to its TypedValue. Every field in the
	// definition must appear (even if nil/empty, per type).
	FieldValues map[string]TypedValue
}

// Validate reports whether the revision is structurally valid.
func (r CustomRecordRevision) Validate(def CustomObjectDefinition) error {
	if r.ObjectID == "" {
		return fmt.Errorf("%w: object_id required", ErrInvalidRecordRevision)
	}
	if r.ObjectKind != def.Kind || r.Namespace != def.Namespace {
		return fmt.Errorf("%w: kind/namespace mismatch", ErrInvalidRecordRevision)
	}
	if r.DefinitionVersion != def.Version {
		return fmt.Errorf("%w: definition version mismatch", ErrInvalidRecordRevision)
	}
	if err := r.Effective.Validate(); err != nil {
		return fmt.Errorf("%w: effective interval: %v", ErrInvalidRecordRevision, err)
	}
	for fieldName := range def.Fields {
		_, ok := r.FieldValues[fieldName]
		if !ok {
			return fmt.Errorf("%w: missing field value for %q", ErrInvalidRecordRevision, fieldName)
		}
	}
	for fieldName := range r.FieldValues {
		_, ok := def.Fields[fieldName]
		if !ok {
			return fmt.Errorf("%w: unexpected field %q", ErrInvalidRecordRevision, fieldName)
		}
	}
	return nil
}

// Canonical returns the deterministic byte encoding of the revision.
func (r CustomRecordRevision) Canonical() []byte {
	w := canonicalbytes.New(customRecordRevisionSchema, 1)
	w.String("object_id", r.ObjectID).
		String("object_kind", r.ObjectKind).
		String("namespace", r.Namespace).
		Int("definition_version", int64(r.DefinitionVersion)).
		Value("effective", r.Effective).
		Value("recorded", r.Recorded).
		Value("known", r.Known)

	// Sort field names for determinism.
	fieldNames := make([]string, 0, len(r.FieldValues))
	for name := range r.FieldValues {
		fieldNames = append(fieldNames, name)
	}
	sort.Strings(fieldNames)

	for _, name := range fieldNames {
		tv := r.FieldValues[name]
		w.String(fmt.Sprintf("field_values.%s.type", name), tv.Type)
		// Value encoding depends on type; for canonical consistency,
		// we encode as a string representation of the value.
		w.String(fmt.Sprintf("field_values.%s.value", name), fmt.Sprintf("%v", tv.Value))
	}

	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// Digest returns the hex digest of the canonical revision encoding.
func (r CustomRecordRevision) Digest() string {
	w := canonicalbytes.New(customRecordRevisionSchema, 1)
	w.String("object_id", r.ObjectID).
		String("object_kind", r.ObjectKind).
		String("namespace", r.Namespace).
		Int("definition_version", int64(r.DefinitionVersion)).
		Value("effective", r.Effective).
		Value("recorded", r.Recorded).
		Value("known", r.Known)

	// Sort field names for determinism.
	fieldNames := make([]string, 0, len(r.FieldValues))
	for name := range r.FieldValues {
		fieldNames = append(fieldNames, name)
	}
	sort.Strings(fieldNames)

	for _, name := range fieldNames {
		tv := r.FieldValues[name]
		w.String(fmt.Sprintf("field_values.%s.type", name), tv.Type)
		w.String(fmt.Sprintf("field_values.%s.value", name), fmt.Sprintf("%v", tv.Value))
	}

	digest, err := w.Digest()
	if err != nil {
		return ""
	}
	return digest
}

// FieldPolicy projects the required authz domain, classification, residency
// and retention constraints for one field in a definition.
type FieldPolicy struct {
	FieldName      string
	AuthZDomain    string
	Classification string
	ResidencyRef   string
	RetentionClass string
}

// PolicyProjection resolves the required policies for a definition: every
// field must have complete classification and residency/retention assignments.
// Publication fails if any field is unclassified.
type PolicyProjection struct {
	Kind              string
	Namespace         string
	DefinitionVersion uint64
	Fields            []FieldPolicy
}

// ResolvePolicy builds a projection from a definition, failing if any field
// is unclassified or missing a required policy constraint.
func ResolvePolicy(def CustomObjectDefinition) (PolicyProjection, error) {
	if err := def.Validate(); err != nil {
		return PolicyProjection{}, err
	}

	// Sort field names for determinism.
	fieldNames := make([]string, 0, len(def.Fields))
	for name := range def.Fields {
		fieldNames = append(fieldNames, name)
	}
	sort.Strings(fieldNames)

	policies := make([]FieldPolicy, 0, len(fieldNames))
	for _, name := range fieldNames {
		fieldDef := def.Fields[name]
		fc := fieldDef.Classification

		// Validate classification is complete.
		if err := fc.Validate(); err != nil {
			return PolicyProjection{}, fmt.Errorf("field %q: %w", name, err)
		}

		policies = append(policies, FieldPolicy{
			FieldName:      name,
			AuthZDomain:    fc.AuthZDomain,
			Classification: fc.Classification,
			ResidencyRef:   fc.ResidencyRef,
			RetentionClass: fc.RetentionClass,
		})
	}

	return PolicyProjection{
		Kind:              def.Kind,
		Namespace:         def.Namespace,
		DefinitionVersion: def.Version,
		Fields:            policies,
	}, nil
}

// CanAuthorize reports whether a principal holding the given authz domains
// can authorize publication of a record conforming to this policy.
// It is a simple per-field check: every field's authz domain must appear
// in the principal's grants.
func (pp PolicyProjection) CanAuthorize(grantedDomains map[string]bool) bool {
	for _, fp := range pp.Fields {
		if !grantedDomains[fp.AuthZDomain] {
			return false
		}
	}
	return true
}

// EffectiveInterval wraps values.EffectiveInterval so we can test interval
// operations without exposing the kernel's internals.
//
// This is a placeholder for testing purposes; in production, callers use
// values.EffectiveInterval directly.
func NewLocalDateInterval(start, end values.LocalDate, calendar values.CalendarRef) (values.EffectiveInterval, error) {
	return values.NewLocalDateInterval(start, end, calendar)
}

// NewOpenLocalDateInterval builds an open-ended date interval.
func NewOpenLocalDateInterval(start values.LocalDate, calendar values.CalendarRef) (values.EffectiveInterval, error) {
	return values.NewOpenLocalDateInterval(start, calendar)
}

// NewInstantInterval builds a closed instant interval.
func NewInstantInterval(start, end values.Instant) (values.EffectiveInterval, error) {
	return values.NewInstantInterval(start, end)
}

// NewOpenInstantInterval builds an open-ended instant interval.
func NewOpenInstantInterval(start values.Instant) (values.EffectiveInterval, error) {
	return values.NewOpenInstantInterval(start)
}
