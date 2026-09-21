package readiness

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

const schemaVersion = 1

var (
	// ErrInvalidRequirement identifies a malformed or tampered requirement.
	ErrInvalidRequirement = errors.New("readiness: invalid requirement")
	// ErrInvalidOrigin identifies a missing domain/capability/version binding.
	ErrInvalidOrigin = errors.New("readiness: invalid origin")
	// ErrInvalidExtension identifies a malformed domain-owned extension point.
	ErrInvalidExtension = errors.New("readiness: invalid domain extension")
)

// Domain is the closed set used by the conformance proof. Domain-specific
// rules remain outside this package; these names only identify ownership.
type Domain string

const (
	DomainReturnToWork   Domain = "RETURN_TO_WORK"
	DomainOnboarding     Domain = "ONBOARDING"
	DomainPayrollRelease Domain = "PAYROLL_RELEASE"
	DomainRecoveryGoLive Domain = "RECOVERY_GO_LIVE"
)

var domains = map[Domain]struct{}{
	DomainReturnToWork: {}, DomainOnboarding: {},
	DomainPayrollRelease: {}, DomainRecoveryGoLive: {},
}

func (d Domain) Valid() bool    { _, ok := domains[d]; return ok }
func (d Domain) String() string { return string(d) }

// RequirementPolicy is the common blocking/conditional policy. Domain
// evaluators may add meaning through their typed extension, but the engine
// does not accept executable predicates or arbitrary expressions.
type RequirementPolicy string

const (
	PolicyBlocking    RequirementPolicy = "BLOCKING"
	PolicyConditional RequirementPolicy = "CONDITIONAL"
)

func (p RequirementPolicy) Valid() bool    { return p == PolicyBlocking || p == PolicyConditional }
func (p RequirementPolicy) String() string { return string(p) }

// EvidenceKind is a closed common-kernel vocabulary. A domain extension
// carries additional typed meaning when one of these kinds is insufficient.
type EvidenceKind string

const (
	EvidenceAuthorization  EvidenceKind = "AUTHORIZATION"
	EvidenceQualification  EvidenceKind = "QUALIFICATION"
	EvidenceTraining       EvidenceKind = "TRAINING"
	EvidencePolicyDecision EvidenceKind = "POLICY_DECISION"
	EvidenceOperational    EvidenceKind = "OPERATIONAL_HEALTH"
	EvidenceExternal       EvidenceKind = "EXTERNAL_OBSERVATION"
)

var evidenceKinds = map[EvidenceKind]struct{}{
	EvidenceAuthorization: {}, EvidenceQualification: {}, EvidenceTraining: {},
	EvidencePolicyDecision: {}, EvidenceOperational: {}, EvidenceExternal: {},
}

func (k EvidenceKind) Valid() bool    { _, ok := evidenceKinds[k]; return ok }
func (k EvidenceKind) String() string { return string(k) }

// Origin binds a requirement to the domain capability and version that owns
// its meaning. It is intentionally not a free-form rule expression.
type Origin struct {
	Domain     Domain
	Capability string
	Version    string
}

func (o Origin) Validate() error {
	if !o.Domain.Valid() {
		return fmt.Errorf("%w: domain %q", ErrInvalidOrigin, o.Domain)
	}
	if strings.TrimSpace(o.Capability) == "" || strings.TrimSpace(o.Version) == "" {
		return fmt.Errorf("%w: capability and version are required", ErrInvalidOrigin)
	}
	return nil
}

func (o Origin) Canonical() []byte {
	if o.Validate() != nil {
		return nil
	}
	b, err := canonicalbytes.New("hcmnext.engines.readiness.Origin", schemaVersion).
		String("domain", o.Domain.String()).String("capability", o.Capability).
		String("version", o.Version).Bytes()
	if err != nil {
		return nil
	}
	return b
}

// DomainExtension is an opaque, typed reference to domain-owned semantics.
// Ref identifies a governed artifact; it never carries an executable
// predicate or raw evidence payload.
type DomainExtension struct {
	Kind    string
	Version string
	Ref     string
}

func (e DomainExtension) Validate() error {
	if strings.TrimSpace(e.Kind) == "" || strings.TrimSpace(e.Version) == "" || strings.TrimSpace(e.Ref) == "" {
		return fmt.Errorf("%w: kind, version and ref are required", ErrInvalidExtension)
	}
	return nil
}

func (e DomainExtension) Canonical() []byte {
	if e.Validate() != nil {
		return nil
	}
	b, err := canonicalbytes.New("hcmnext.engines.readiness.DomainExtension", schemaVersion).
		String("kind", e.Kind).String("version", e.Version).String("ref", e.Ref).Bytes()
	if err != nil {
		return nil
	}
	return b
}

// ReadinessRequirement is the minimal immutable common-kernel requirement.
// Its constructor copies the evidence-kind slice, computes a canonical
// digest, and validates every descriptor. Domain eligibility, legality and
// qualification remain external inputs.
type ReadinessRequirement struct {
	RequirementID   string
	Revision        uint64
	Origin          Origin
	Owner           string
	EvidenceKinds   []EvidenceKind
	Effective       values.EffectiveInterval
	Policy          RequirementPolicy
	Extension       DomainExtension
	CanonicalDigest string
}

func (r ReadinessRequirement) Validate() error {
	if strings.TrimSpace(r.RequirementID) == "" || r.Revision == 0 {
		return fmt.Errorf("%w: requirement id and non-zero revision are required", ErrInvalidRequirement)
	}
	if err := r.Origin.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(r.Owner) == "" {
		return fmt.Errorf("%w: owner is required", ErrInvalidRequirement)
	}
	if err := r.Effective.Validate(); err != nil {
		return fmt.Errorf("%w: effective interval: %v", ErrInvalidRequirement, err)
	}
	if !r.Policy.Valid() {
		return fmt.Errorf("%w: policy %q", ErrInvalidRequirement, r.Policy)
	}
	if len(r.EvidenceKinds) == 0 {
		return fmt.Errorf("%w: at least one evidence kind is required", ErrInvalidRequirement)
	}
	seen := make(map[EvidenceKind]struct{}, len(r.EvidenceKinds))
	for _, kind := range r.EvidenceKinds {
		if !kind.Valid() {
			return fmt.Errorf("%w: evidence kind %q", ErrInvalidRequirement, kind)
		}
		if _, ok := seen[kind]; ok {
			return fmt.Errorf("%w: duplicate evidence kind %q", ErrInvalidRequirement, kind)
		}
		seen[kind] = struct{}{}
	}
	if err := r.Extension.Validate(); err != nil {
		return err
	}
	if r.CanonicalDigest != "" && r.CanonicalDigest != r.computedDigest() {
		return fmt.Errorf("%w: canonical digest mismatch", ErrInvalidRequirement)
	}
	return nil
}

func (r ReadinessRequirement) body() []byte {
	kinds := append([]EvidenceKind(nil), r.EvidenceKinds...)
	sort.Slice(kinds, func(i, j int) bool { return kinds[i] < kinds[j] })
	w := canonicalbytes.New("hcmnext.engines.readiness.ReadinessRequirement", schemaVersion).
		String("requirement_id", r.RequirementID).Int("revision", int64(r.Revision)).
		Value("origin", r.Origin).String("owner", r.Owner).Value("effective", r.Effective).
		String("policy", r.Policy.String()).Value("extension", r.Extension).
		Count("evidence_kinds", len(kinds))
	for _, kind := range kinds {
		w.String("evidence_kind", kind.String())
	}
	b, err := w.Bytes()
	if err != nil {
		return nil
	}
	return b
}

func (r ReadinessRequirement) computedDigest() string {
	b := r.body()
	if b == nil {
		return ""
	}
	return canonicalbytes.Digest(b)
}

// NewReadinessRequirement constructs a validated, digested requirement.
func NewReadinessRequirement(r ReadinessRequirement) (ReadinessRequirement, error) {
	r.EvidenceKinds = append([]EvidenceKind(nil), r.EvidenceKinds...)
	r.CanonicalDigest = r.computedDigest()
	if err := r.Validate(); err != nil {
		return ReadinessRequirement{}, err
	}
	return r, nil
}

// Compile validates and canonicalizes a readiness requirement into the exact
// immutable form consumed by Evaluate. It is the engine-contract counterpart
// to Evaluate; NewReadinessRequirement remains the descriptive constructor.
func Compile(r ReadinessRequirement) (ReadinessRequirement, error) {
	return NewReadinessRequirement(r)
}

func (r ReadinessRequirement) Canonical() []byte {
	if r.Validate() != nil {
		return nil
	}
	return r.body()
}

func (r ReadinessRequirement) Digest() (string, error) {
	if err := r.Validate(); err != nil {
		return "", err
	}
	return r.computedDigest(), nil
}

// ReadinessStatus is the complete common-kernel outcome vocabulary reserved
// for the later readiness evaluator. Evidence resolution deliberately uses a
// separate SATISFIED/UNSATISFIED vocabulary.
type ReadinessStatus string

const (
	StatusReady       ReadinessStatus = "READY"
	StatusConditional ReadinessStatus = "CONDITIONAL"
	StatusNotReady    ReadinessStatus = "NOT_READY"
	StatusUnknown     ReadinessStatus = "UNKNOWN"
)

var requirementStatuses = map[ReadinessStatus]struct{}{
	StatusReady: {}, StatusConditional: {}, StatusNotReady: {}, StatusUnknown: {},
}

func (s ReadinessStatus) Valid() bool    { _, ok := requirementStatuses[s]; return ok }
func (s ReadinessStatus) String() string { return string(s) }

// RequirementStatus is retained as a descriptive alias for callers that
// refer to the common readiness result before the evaluator exists.
type RequirementStatus = ReadinessStatus
