package productslice

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
)

const dispositionSchemaVersion = 1

// ProductDisposition is the closed release-scope vocabulary for one product
// slice element.
type ProductDisposition string

// Disposition is the concise spelling used by callers.
type Disposition = ProductDisposition

// The default_disposition vocabulary is the closed six-value set
// planning/specs/default-product-slice-alignment.md defines:
// CORE_REQUIRED, DOMAIN_PACK_DEFAULT, AVAILABLE_NOT_ENABLED,
// CUSTOMER_DEFINED, DEFERRED and PROHIBITED (REV-081-01 retired
// the older CORE, OPTIONAL, EXCLUDED and PARTNER_ONLY codes,
// which never matched the contract).
const (
	DispositionCoreRequired        ProductDisposition = "CORE_REQUIRED"
	DispositionDomainPackDefault   ProductDisposition = "DOMAIN_PACK_DEFAULT"
	DispositionAvailableNotEnabled ProductDisposition = "AVAILABLE_NOT_ENABLED"
	DispositionCustomerDefined     ProductDisposition = "CUSTOMER_DEFINED"
	DispositionDeferred            ProductDisposition = "DEFERRED"
	DispositionProhibited          ProductDisposition = "PROHIBITED"
)

func (d ProductDisposition) Valid() bool {
	switch d {
	case DispositionCoreRequired, DispositionDomainPackDefault, DispositionAvailableNotEnabled, DispositionCustomerDefined, DispositionDeferred, DispositionProhibited:
		return true
	default:
		return false
	}
}

func (d ProductDisposition) String() string { return string(d) }

// DispositionDefinition is one vocabulary entry. Definitions are stable
// contract text, not user-provided explanations.
type DispositionDefinition struct {
	Code       ProductDisposition `json:"code" yaml:"code"`
	Definition string             `json:"definition" yaml:"definition"`
}

func (d DispositionDefinition) Validate() error {
	if !d.Code.Valid() {
		return fmt.Errorf("productslice: unknown disposition %q", d.Code)
	}
	if strings.TrimSpace(d.Definition) == "" {
		return fmt.Errorf("productslice: definition for %s is required", d.Code)
	}
	return nil
}

func (d DispositionDefinition) Canonical() []byte {
	if d.Validate() != nil {
		return nil
	}
	b, err := canonicalbytes.New("hcmnext.planning.productslice.DispositionDefinition", dispositionSchemaVersion).
		String("code", string(d.Code)).String("definition", d.Definition).Bytes()
	if err != nil {
		return nil
	}
	return b
}

var defaultDispositionDefinitions = []DispositionDefinition{
	{Code: DispositionCoreRequired, Definition: "Required for every supported installation."},
	{Code: DispositionDomainPackDefault, Definition: "Installed and enabled with an admitted domain pack."},
	{Code: DispositionAvailableNotEnabled, Definition: "Shipped but requires explicit customer activation."},
	{Code: DispositionCustomerDefined, Definition: "Governed customer configuration using platform parts."},
	{Code: DispositionDeferred, Definition: "Not present in a production release."},
	{Code: DispositionProhibited, Definition: "Not present in a production release."},
}

// DefaultProductDispositionVocabulary returns a detached copy in canonical
// order. Callers cannot mutate the package vocabulary through this result.
func DefaultProductDispositionVocabulary() []DispositionDefinition {
	return append([]DispositionDefinition(nil), defaultDispositionDefinitions...)
}

// DefaultDispositionVocabulary is a compatibility spelling for the default
// product vocabulary.
func DefaultDispositionVocabulary() []DispositionDefinition {
	return DefaultProductDispositionVocabulary()
}

func dispositionVocabularyCanonical() []byte {
	w := canonicalbytes.New("hcmnext.planning.productslice.DefaultDispositionVocabulary", dispositionSchemaVersion).
		Count("disposition", len(defaultDispositionDefinitions))
	for _, definition := range defaultDispositionDefinitions {
		w.Value("disposition", definition)
	}
	b, err := w.Bytes()
	if err != nil {
		return nil
	}
	return b
}

// DefaultDispositionVocabularyDigest is the checked-in golden identity of
// the six-entry spec vocabulary. It changes only when the vocabulary or its
// contract text changes.
const DefaultDispositionVocabularyDigest = "sha256:fec4134a620f0818e1c6896be98ba27bc89acc240e891f9166a3457a0c398946"

// DefaultProductDispositionVocabularyDigest is the descriptive alias used by
// registry and evidence consumers.
const DefaultProductDispositionVocabularyDigest = DefaultDispositionVocabularyDigest

func DispositionVocabularyDigest() string {
	return canonicalbytes.Digest(dispositionVocabularyCanonical())
}

// SliceElementDisposition assigns one closed disposition and one concise
// reason code to one slice element. ElementRef is preferred; Kind and Ref are
// a structured compatibility spelling for callers that keep element kind
// separate from its identifier.
type SliceElementDisposition struct {
	ElementRef  string             `json:"element_ref" yaml:"element_ref"`
	ElementID   string             `json:"element_id,omitempty" yaml:"element_id,omitempty"`
	Kind        string             `json:"kind,omitempty" yaml:"kind,omitempty"`
	Ref         string             `json:"ref,omitempty" yaml:"ref,omitempty"`
	Disposition ProductDisposition `json:"disposition" yaml:"disposition"`
	ReasonCode  string             `json:"reason_code" yaml:"reason_code"`
}

type ElementDisposition = SliceElementDisposition
type ProductSliceElementDisposition = SliceElementDisposition

var (
	ErrInvalidDisposition        = errors.New("productslice: invalid disposition")
	ErrMissingElementDisposition = errors.New("productslice: slice element has no disposition")
)

func (e SliceElementDisposition) identity() string {
	if strings.TrimSpace(e.ElementRef) != "" {
		return strings.TrimSpace(e.ElementRef)
	}
	if strings.TrimSpace(e.ElementID) != "" {
		return strings.TrimSpace(e.ElementID)
	}
	if strings.TrimSpace(e.Kind) != "" && strings.TrimSpace(e.Ref) != "" {
		return strings.TrimSpace(e.Kind) + ":" + strings.TrimSpace(e.Ref)
	}
	return strings.TrimSpace(e.Ref)
}

func (e SliceElementDisposition) Validate() error {
	if e.identity() == "" {
		return &DispositionValidationError{Field: "element_ref", Detail: "element reference is required", Cause: ErrInvalidDisposition}
	}
	if strings.TrimSpace(e.ElementRef) != "" && strings.TrimSpace(e.ElementID) != "" && strings.TrimSpace(e.ElementRef) != strings.TrimSpace(e.ElementID) {
		return &DispositionValidationError{Field: "element_ref", ElementRef: e.identity(), Detail: "element_ref and element_id disagree", Cause: ErrInvalidDisposition}
	}
	if (strings.TrimSpace(e.ElementRef) != "" || strings.TrimSpace(e.ElementID) != "") && (e.Kind != "" || e.Ref != "") {
		return &DispositionValidationError{Field: "element_ref", ElementRef: e.ElementRef, Detail: "use element_ref or kind/ref, not both", Cause: ErrInvalidDisposition}
	}
	if !e.Disposition.Valid() {
		return &DispositionValidationError{Field: "disposition", ElementRef: e.identity(), Detail: "disposition is not declared", Cause: ErrInvalidDisposition}
	}
	if strings.TrimSpace(e.ReasonCode) == "" {
		return &DispositionValidationError{Field: "reason_code", ElementRef: e.identity(), Detail: "reason code is required", Cause: ErrInvalidDisposition}
	}
	if strings.TrimSpace(e.ReasonCode) != e.ReasonCode || strings.IndexFunc(e.ReasonCode, func(r rune) bool { return r == '\r' || r == '\n' }) >= 0 {
		return &DispositionValidationError{Field: "reason_code", ElementRef: e.identity(), Detail: "reason code must be one line without surrounding whitespace", Cause: ErrInvalidDisposition}
	}
	return nil
}

func (e SliceElementDisposition) Canonical() []byte {
	if err := e.Validate(); err != nil {
		return nil
	}
	b, err := canonicalbytes.New("hcmnext.planning.productslice.SliceElementDisposition", dispositionSchemaVersion).
		String("element_ref", e.identity()).String("disposition", string(e.Disposition)).
		String("reason_code", e.ReasonCode).Bytes()
	if err != nil {
		return nil
	}
	return b
}

// DispositionSet binds dispositions to one immutable slice version.
type DispositionSet struct {
	SliceID         string                    `json:"slice_id" yaml:"slice_id"`
	SliceVersion    int                       `json:"slice_version" yaml:"slice_version"`
	Elements        []SliceElementDisposition `json:"elements" yaml:"elements"`
	Dispositions    []SliceElementDisposition `json:"dispositions,omitempty" yaml:"dispositions,omitempty"`
	CanonicalDigest string                    `json:"canonical_digest,omitempty" yaml:"canonical_digest,omitempty"`
}

type ProductSliceDispositionSet = DispositionSet

func NewDispositionSet(slice ProductSliceDefinition, elements ...SliceElementDisposition) (DispositionSet, error) {
	set := DispositionSet{SliceID: slice.SliceID, SliceVersion: slice.Version, Elements: append([]SliceElementDisposition(nil), elements...)}
	set.CanonicalDigest = ""
	if err := ValidateSliceDispositions(slice, elements); err != nil {
		return DispositionSet{}, err
	}
	if err := set.Validate(); err != nil {
		return DispositionSet{}, err
	}
	set.CanonicalDigest = set.computedDigest()
	return set, nil
}

func (s DispositionSet) Validate() error {
	if err := s.validateWithoutDigest(); err != nil {
		return err
	}
	if s.CanonicalDigest != "" && s.CanonicalDigest != s.computedDigest() {
		return &DispositionValidationError{Field: "canonical_digest", Detail: "digest mismatch", Cause: ErrInvalidDisposition}
	}
	return nil
}

func (s DispositionSet) validateWithoutDigest() error {
	if strings.TrimSpace(s.SliceID) == "" {
		return &DispositionValidationError{Field: "slice_id", Detail: "slice id is required", Cause: ErrInvalidDisposition}
	}
	if s.SliceVersion <= 0 {
		return &DispositionValidationError{Field: "slice_version", Detail: "slice version must be positive", Cause: ErrInvalidDisposition}
	}
	elements := s.effectiveElements()
	if len(elements) == 0 {
		return &DispositionValidationError{Field: "elements", Detail: "at least one slice element disposition is required", Cause: ErrMissingElementDisposition}
	}
	if err := ValidateDispositions(elements); err != nil {
		return err
	}
	return nil
}

func (s DispositionSet) effectiveElements() []SliceElementDisposition {
	if len(s.Elements) == 0 {
		return append([]SliceElementDisposition(nil), s.Dispositions...)
	}
	if len(s.Dispositions) == 0 {
		return append([]SliceElementDisposition(nil), s.Elements...)
	}
	out := append([]SliceElementDisposition(nil), s.Elements...)
	return append(out, s.Dispositions...)
}

// ValidateDispositions refuses an element that is missing a disposition,
// reason code, or stable identity and rejects duplicate element assignments.
func ValidateDispositions(elements []SliceElementDisposition) error {
	seen := make(map[string]struct{}, len(elements))
	for _, element := range elements {
		if err := element.Validate(); err != nil {
			return err
		}
		if _, exists := seen[element.identity()]; exists {
			return &DispositionValidationError{Field: "element_ref", ElementRef: element.identity(), Detail: "duplicate element disposition", Cause: ErrInvalidDisposition}
		}
		seen[element.identity()] = struct{}{}
	}
	return nil
}

func (s DispositionSet) sorted() DispositionSet {
	s.Elements = s.effectiveElements()
	s.Dispositions = nil
	sort.Slice(s.Elements, func(i, j int) bool { return s.Elements[i].identity() < s.Elements[j].identity() })
	return s
}

func (s DispositionSet) body() []byte {
	if err := s.validateWithoutDigest(); err != nil {
		return nil
	}
	s = s.sorted()
	w := canonicalbytes.New("hcmnext.planning.productslice.DispositionSet", dispositionSchemaVersion).
		String("slice_id", s.SliceID).Int("slice_version", int64(s.SliceVersion)).Count("elements", len(s.Elements))
	for _, element := range s.Elements {
		w.Value("element", element)
	}
	b, err := w.Bytes()
	if err != nil {
		return nil
	}
	return b
}

func (s DispositionSet) computedDigest() string {
	body := s.body()
	if body == nil {
		return ""
	}
	return canonicalbytes.Digest(body)
}

func (s DispositionSet) Canonical() []byte { return s.body() }

func (s DispositionSet) Digest() (string, error) {
	if err := s.Validate(); err != nil {
		return "", err
	}
	return s.computedDigest(), nil
}

// ValidateDispositions checks that every reference exposed by a slice has an
// assignment. The returned violations use ProductSliceDefinition's existing
// field-aware diagnostic shape.
func (d ProductSliceDefinition) ValidateDispositions(elements []SliceElementDisposition) []Violation {
	violations := make([]Violation, 0)
	if err := ValidateDispositions(elements); err != nil {
		violations = append(violations, Violation{Field: "dispositions", Detail: err.Error()})
		return violations
	}
	assigned := make(map[string]struct{}, len(elements))
	for _, element := range elements {
		assigned[element.identity()] = struct{}{}
	}
	for _, expected := range d.elementRefs() {
		if _, ok := assigned[expected]; !ok {
			violations = append(violations, Violation{Field: "disposition", Ref: expected, Detail: ErrMissingElementDisposition.Error()})
		}
	}
	return violations
}

// ElementRefs returns the canonical kind-prefixed element identities a slice
// exposes to the disposition validator.
func (d ProductSliceDefinition) ElementRefs() []string {
	return append([]string(nil), d.elementRefs()...)
}

// ValidateSliceDispositions is the error-returning spelling for callers that
// need construction to fail when any slice element lacks an assignment.
func ValidateSliceDispositions(slice ProductSliceDefinition, elements []SliceElementDisposition) error {
	violations := slice.ValidateDispositions(elements)
	if len(violations) == 0 {
		return nil
	}
	first := violations[0]
	cause := ErrMissingElementDisposition
	if strings.Contains(first.Detail, ErrInvalidDisposition.Error()) || first.Field == "dispositions" {
		cause = ErrInvalidDisposition
	}
	return &DispositionValidationError{Field: first.Field, ElementRef: first.Ref, Detail: first.Detail, Cause: cause}
}

func (d ProductSliceDefinition) elementRefs() []string {
	var out []string
	appendRefs := func(kind string, refs []string) {
		for _, ref := range refs {
			if strings.TrimSpace(ref) != "" {
				out = append(out, kind+":"+strings.TrimSpace(ref))
			}
		}
	}
	appendRefs("business_intent", d.BusinessIntents)
	appendRefs("feature", d.Features)
	appendRefs("page", d.Pages)
	appendRefs("widget", d.Widgets)
	appendRefs("capability", d.Capabilities)
	appendRefs("package", d.Packages)
	appendRefs("todo", d.Todos)
	return out
}

// DispositionValidationError names the element and field that was refused.
type DispositionValidationError struct {
	Field      string
	ElementRef string
	Detail     string
	Cause      error
}

func (e *DispositionValidationError) Error() string {
	where := e.Field
	if e.ElementRef != "" {
		where += " " + fmt.Sprintf("%q", e.ElementRef)
	}
	return fmt.Sprintf("productslice: %s: %s", where, e.Detail)
}

func (e *DispositionValidationError) Unwrap() error { return e.Cause }

// Explain renders only bounded structural facts; reason text and element
// values stay out of summaries intended for logs or UI surfaces.
func (s DispositionSet) Explain() string {
	digest := s.CanonicalDigest
	if digest == "" {
		digest = s.computedDigest()
	}
	return fmt.Sprintf("product slice disposition set %s@%d with %d elements (%s)", s.SliceID, s.SliceVersion, len(s.effectiveElements()), digest)
}
