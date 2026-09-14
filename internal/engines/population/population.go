// Package population is the pure population engine: given a typed definition
// of "which subjects", it compiles the criteria, resolves membership against a
// bitemporal fact port as of a declared instant known no later than a declared
// instant, applies restriction decisions handed to it as input, freezes an
// immutable snapshot, diffs snapshots deterministically and explains
// inclusion/exclusion without ever inventing a boolean where the true answer
// is UNKNOWN or PARTIAL.
//
// Semantic owner: shared-engines. Phase: P1A (business-intent population
// scope). The engine never decides AuthZ, privacy, organization or purpose
// policy itself: POP-004 accepts those as already-decided restrictions and
// applies them, because deciding them is TRUST/PRIV territory this package
// does not own. It never reads a database, a clock or a config file; every
// external fact arrives through the FactReader port a caller supplies, and
// every restriction arrives through the RestrictionDecision a caller supplies.
//
// All arithmetic and comparison is exact: text, values.Decimal and
// values.LocalDate/Instant. There is no float64 anywhere in this package.
package population

import (
	"errors"
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// schemaID and schemaVersion tag the canonical stream of this engine's types.
const (
	definitionSchema   = "hcmnext.engines.population.Definition"
	revisionSchema     = "hcmnext.engines.population.Revision"
	planSchema         = "hcmnext.engines.population.CompiledPlan"
	memberSchema       = "hcmnext.engines.population.Member"
	snapshotSchema     = "hcmnext.engines.population.Snapshot"
	diffSchema         = "hcmnext.engines.population.SnapshotDiff"
	explainSchema      = "hcmnext.engines.population.Explanation"
	completenessSchema = "hcmnext.engines.population.CompletenessReport"
	schemaVersion      = 1
)

// writerFor opens a canonicalbytes stream at this engine's schema version, so
// every canonical encoder in this package agrees on one version constant.
func writerFor(schema string) *canonicalbytes.Writer {
	return canonicalbytes.New(schema, schemaVersion)
}

// canonicalDigest returns "sha256:<hex>" over raw.
func canonicalDigest(raw []byte) string {
	return canonicalbytes.Digest(raw)
}

// Version reports this engine's own package contract version: the schema
// version every canonical encoder in this package agrees on (see
// writerFor above). It is part of the ARCH-GO-009 engine package contract,
// not a business-facing evaluation input.
func Version() int { return schemaVersion }

// Engine errors. All are matchable with errors.Is.
var (
	// ErrDefinitionOwner is returned when a definition has no accountable owner.
	ErrDefinitionOwner = errors.New("population: definition requires an owner")
	// ErrDefinitionSubject is returned when the subject type is not declared, or
	// is not one of the registered subject kinds. A population never infers its
	// subject type from the criteria: it is always explicit.
	ErrDefinitionSubject = errors.New("population: definition requires an explicit, valid subject kind")
	// ErrDefinitionScope is returned when tenant, organization scope or purpose
	// is missing.
	ErrDefinitionScope = errors.New("population: definition requires tenant, organization scope and purpose")
	// ErrDefinitionTemporalPolicy is returned when the definition does not
	// declare how effective time and known-at time are resolved.
	ErrDefinitionTemporalPolicy = errors.New("population: definition requires an effective/known-at temporal policy")
	// ErrDefinitionUnknownPolicy is returned when no unknown-disclosure policy
	// is declared.
	ErrDefinitionUnknownPolicy = errors.New("population: definition requires an unknown-disclosure policy")
	// ErrDefinitionCountPolicy is returned when no count-disclosure policy is
	// declared.
	ErrDefinitionCountPolicy = errors.New("population: definition requires a count-disclosure policy")
	// ErrDefinitionCriteria is returned when the criteria is empty or a raw
	// free-form query rather than a typed predicate tree.
	ErrDefinitionCriteria = errors.New("population: definition requires typed, non-empty criteria")
	// ErrRevisionIdentity is returned when a revision has no version or does
	// not cite its definition.
	ErrRevisionIdentity = errors.New("population: revision requires a definition and a version")
	// ErrRevisionInputs is returned when a revision cites no typed fact inputs.
	ErrRevisionInputs = errors.New("population: revision requires at least one typed input reference")
)

// SubjectKind is the population's subject type. It is registry-owned
// vocabulary, never inferred from criteria content.
type SubjectKind string

// Registered subject kinds.
const (
	SubjectUnspecified      SubjectKind = ""
	SubjectWorker           SubjectKind = "WORKER"
	SubjectPosition         SubjectKind = "POSITION"
	SubjectOrganizationUnit SubjectKind = "ORGANIZATION_UNIT"
)

var subjectKinds = map[SubjectKind]bool{
	SubjectWorker:           true,
	SubjectPosition:         true,
	SubjectOrganizationUnit: true,
}

// Valid reports whether s is a registered, non-empty subject kind.
func (s SubjectKind) Valid() bool { return subjectKinds[s] }

// String returns the wire token.
func (s SubjectKind) String() string {
	if s == SubjectUnspecified {
		return "SUBJECT_UNSPECIFIED"
	}
	return string(s)
}

// UnknownDisclosure states how a resolution must treat a subject whose
// membership cannot be determined because a required fact is UNKNOWN,
// UNAVAILABLE or REDACTED. There is no default: a definition must pick one.
type UnknownDisclosure uint8

// Unknown-disclosure policies.
const (
	UnknownDisclosureUnspecified UnknownDisclosure = iota
	// UnknownDisclosureExcludeAndReport removes the subject from the counted
	// membership set but keeps it visible as an UNKNOWN outcome; it never
	// silently disappears.
	UnknownDisclosureExcludeAndReport
	// UnknownDisclosureBlock refuses to freeze a snapshot while any subject's
	// membership is undetermined.
	UnknownDisclosureBlock
)

var unknownDisclosureWire = map[UnknownDisclosure]string{
	UnknownDisclosureExcludeAndReport: "EXCLUDE_AND_REPORT",
	UnknownDisclosureBlock:            "BLOCK",
}

// Valid reports whether u is a legal policy.
func (u UnknownDisclosure) Valid() bool { return unknownDisclosureWire[u] != "" }

// String returns the wire token, or UNKNOWN_DISCLOSURE_UNSPECIFIED.
func (u UnknownDisclosure) String() string {
	if s, ok := unknownDisclosureWire[u]; ok {
		return s
	}
	return "UNKNOWN_DISCLOSURE_UNSPECIFIED"
}

// CountDisclosure states whether-and-how a snapshot's total count may be
// disclosed to a caller not authorized to see membership.
type CountDisclosure uint8

// Count-disclosure policies.
const (
	CountDisclosureUnspecified CountDisclosure = iota
	// CountDisclosureSuppressed never reveals a count to an unauthorized caller.
	CountDisclosureSuppressed
	// CountDisclosureExact reveals the exact count.
	CountDisclosureExact
	// CountDisclosureBanded reveals a rounded band (e.g. "50-99") rather than an
	// exact count.
	CountDisclosureBanded
)

var countDisclosureWire = map[CountDisclosure]string{
	CountDisclosureSuppressed: "SUPPRESSED",
	CountDisclosureExact:      "EXACT",
	CountDisclosureBanded:     "BANDED",
}

// Valid reports whether c is a legal policy.
func (c CountDisclosure) Valid() bool { return countDisclosureWire[c] != "" }

// String returns the wire token, or COUNT_DISCLOSURE_UNSPECIFIED.
func (c CountDisclosure) String() string {
	if s, ok := countDisclosureWire[c]; ok {
		return s
	}
	return "COUNT_DISCLOSURE_UNSPECIFIED"
}

// TemporalBasis names which of a fact's bitemporal axes a definition resolves
// against. It exists so a population definition states its effective/known-at
// handling explicitly rather than inheriting whatever the caller's clock
// happened to be at query time.
type TemporalBasis uint8

// Temporal bases.
const (
	TemporalBasisUnspecified TemporalBasis = iota
	// TemporalBasisAsOfCaller resolves at a caller-supplied effective instant
	// and known-at instant, both required on every Resolve call.
	TemporalBasisAsOfCaller
	// TemporalBasisCurrent resolves at "now" as observed by the caller's clock,
	// still passed in explicitly by the caller; the engine never reads a clock.
	TemporalBasisCurrent
)

var temporalBasisWire = map[TemporalBasis]string{
	TemporalBasisAsOfCaller: "AS_OF_CALLER",
	TemporalBasisCurrent:    "CURRENT",
}

// Valid reports whether t is a legal basis.
func (t TemporalBasis) Valid() bool { return temporalBasisWire[t] != "" }

// String returns the wire token, or TEMPORAL_BASIS_UNSPECIFIED.
func (t TemporalBasis) String() string {
	if s, ok := temporalBasisWire[t]; ok {
		return s
	}
	return "TEMPORAL_BASIS_UNSPECIFIED"
}

// Scope is the tenant, organization and purpose a population is defined for.
// A definition with no scope cannot be authorized or audited against the
// question it was built to answer.
type Scope struct {
	Tenant               values.TenantId
	OrganizationScopeRef string
	Purpose              string
}

// Validate reports whether the scope is fully specified.
func (s Scope) Validate() error {
	if err := s.Tenant.Validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrDefinitionScope, err)
	}
	if s.OrganizationScopeRef == "" {
		return fmt.Errorf("%w: no organization scope", ErrDefinitionScope)
	}
	if s.Purpose == "" {
		return fmt.Errorf("%w: no purpose", ErrDefinitionScope)
	}
	return nil
}

// Definition is a typed, immutable description of "which subjects" a
// population names. It never carries a free-form query string: Criteria is a
// typed predicate tree (see criteria.go) that POP-002 compiles.
type Definition struct {
	ID                string
	Owner             string
	Subject           SubjectKind
	Scope             Scope
	TemporalBasis     TemporalBasis
	UnknownDisclosure UnknownDisclosure
	CountDisclosure   CountDisclosure
	Criteria          Criteria
}

// Validate reports whether the definition is complete and typed. It is the
// single gate a caller must pass before a definition can be compiled,
// revised, resolved or cited as evidence.
func (d Definition) Validate() error {
	if d.ID == "" {
		return fmt.Errorf("%w: definition has no id", ErrDefinitionOwner)
	}
	if d.Owner == "" {
		return ErrDefinitionOwner
	}
	if !d.Subject.Valid() {
		return fmt.Errorf("%w: %q", ErrDefinitionSubject, string(d.Subject))
	}
	if err := d.Scope.Validate(); err != nil {
		return err
	}
	if !d.TemporalBasis.Valid() {
		return ErrDefinitionTemporalPolicy
	}
	if !d.UnknownDisclosure.Valid() {
		return ErrDefinitionUnknownPolicy
	}
	if !d.CountDisclosure.Valid() {
		return ErrDefinitionCountPolicy
	}
	if err := d.Criteria.Validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrDefinitionCriteria, err)
	}
	return nil
}

// Canonical returns the canonical byte encoding of the definition, or nil when
// the definition fails Validate.
func (d Definition) Canonical() []byte {
	if d.Validate() != nil {
		return nil
	}
	raw, err := canonicalbytes.New(definitionSchema, schemaVersion).
		String("id", d.ID).
		String("owner", d.Owner).
		String("subject", d.Subject.String()).
		String("scope.tenant", d.Scope.Tenant.String()).
		String("scope.org", d.Scope.OrganizationScopeRef).
		String("scope.purpose", d.Scope.Purpose).
		String("temporal_basis", d.TemporalBasis.String()).
		String("unknown_disclosure", d.UnknownDisclosure.String()).
		String("count_disclosure", d.CountDisclosure.String()).
		Value("criteria", d.Criteria).
		Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// InputRef is a typed reference to one fact input a revision was compiled
// against, so a revision's provenance is auditable without re-resolving the
// catalog it was compiled with.
type InputRef struct {
	Field     string
	Kind      FieldType
	Sensitive bool
}

// Revision is one immutable, versioned publication of a Definition. Identity
// is (Definition.ID, Version): republishing a definition with different
// criteria is a new revision, never an edit, because a frozen snapshot cites
// the exact revision it was resolved from.
type Revision struct {
	Definition Definition
	Version    string
	Inputs     []InputRef
	CreatedAt  values.Instant
	KnownAt    values.KnownAt
}

// NewRevision builds an immutable revision. Inputs is defensively copied so a
// caller mutating the slice it passed in cannot mutate the revision.
func NewRevision(def Definition, version string, inputs []InputRef, createdAt values.Instant, knownAt values.KnownAt) (Revision, error) {
	if err := def.Validate(); err != nil {
		return Revision{}, err
	}
	if version == "" {
		return Revision{}, fmt.Errorf("%w: version %q", ErrRevisionIdentity, version)
	}
	if len(inputs) == 0 {
		return Revision{}, ErrRevisionInputs
	}
	if err := createdAt.Validate(); err != nil {
		return Revision{}, fmt.Errorf("population: revision created_at: %w", err)
	}
	if err := knownAt.Instant().Validate(); err != nil {
		return Revision{}, fmt.Errorf("population: revision known_at: %w", err)
	}
	cp := make([]InputRef, len(inputs))
	copy(cp, inputs)
	return Revision{Definition: def, Version: version, Inputs: cp, CreatedAt: createdAt, KnownAt: knownAt}, nil
}

// InputRefs returns a defensive copy of the revision's typed input
// references. Mutating the returned slice never mutates the revision.
func (r Revision) InputRefs() []InputRef {
	cp := make([]InputRef, len(r.Inputs))
	copy(cp, r.Inputs)
	return cp
}

// Validate reports whether the revision is complete.
func (r Revision) Validate() error {
	if err := r.Definition.Validate(); err != nil {
		return err
	}
	if r.Version == "" {
		return ErrRevisionIdentity
	}
	if len(r.Inputs) == 0 {
		return ErrRevisionInputs
	}
	if err := r.CreatedAt.Validate(); err != nil {
		return err
	}
	return r.KnownAt.Instant().Validate()
}

// Canonical returns the canonical byte encoding of the revision, or nil when
// the revision fails Validate.
func (r Revision) Canonical() []byte {
	if r.Validate() != nil {
		return nil
	}
	w := canonicalbytes.New(revisionSchema, schemaVersion).
		Field("definition", r.Definition.Canonical()).
		String("version", r.Version).
		Value("created_at", r.CreatedAt).
		Value("known_at", r.KnownAt.Instant()).
		Count("inputs", len(r.Inputs))
	for _, in := range r.Inputs {
		w.String("inputs.field", in.Field)
		w.String("inputs.type", in.Kind.String())
		w.Bool("inputs.sensitive", in.Sensitive)
	}
	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}
