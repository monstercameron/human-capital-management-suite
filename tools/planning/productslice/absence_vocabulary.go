package productslice

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// AbsenceVocabularySchema and AbsenceVocabularySchemaVersion identify the
// checked-in shape of the absence and disclosure-state vocabulary.
const (
	AbsenceVocabularySchema        = "hcmnext.planning.productslices.absence-vocabulary"
	AbsenceVocabularySchemaVersion = 1
)

// Admitted absence presentations. A disclosure state must present in exactly
// one of these ways: the value is omitted from the response, replaced by a
// fixed placeholder, or deferred to a later authoritative read.
const (
	AbsencePresentOmit        = "omit"
	AbsencePresentPlaceholder = "placeholder"
	AbsencePresentDeferred    = "deferred"
)

// AbsenceDefinition is one stable absence contract shared by the
// presentation, business and persistence lenses. DisclosureRule states how
// the state crosses a trust boundary (dropped, masked, or re-read); the
// owning layer is the authority for the distinction between "no value" and
// "a value the caller may not see".
type AbsenceDefinition struct {
	Kind           string   `yaml:"kind" json:"kind"`
	OwningLayer    string   `yaml:"owning_layer" json:"owning_layer"`
	CanonicalForm  string   `yaml:"canonical_form" json:"canonical_form"`
	DisclosureRule string   `yaml:"disclosure_rule" json:"disclosure_rule"`
	Presentation   string   `yaml:"presentation" json:"presentation"`
	ObservedBy     []string `yaml:"observed_by" json:"observed_by"`
	CarriedBy      []string `yaml:"carried_by" json:"carried_by"`
}

// Comparable reports whether two absence definitions may be substituted for
// each other. Only the same kind compares: an unknown value is never a
// withheld value, even when both present as a placeholder.
func (d AbsenceDefinition) Comparable(other AbsenceDefinition) bool {
	return d.Kind != "" && d.Kind == other.Kind
}

// AbsenceVocabulary is the versioned registry of cross-layer absence kinds.
// Digest is over the schema and sorted rows with Digest omitted.
type AbsenceVocabulary struct {
	Schema        string              `yaml:"schema" json:"schema"`
	SchemaVersion int                 `yaml:"schema_version" json:"schema_version"`
	States        []AbsenceDefinition `yaml:"states" json:"states"`
	Digest        string              `yaml:"digest" json:"digest"`
}

// AbsenceRefusal is a typed refusal from vocabulary validation. It names
// both the absence kind and the layer at which the contract is invalid so
// policy and audit output need not parse a free-form diagnostic.
type AbsenceRefusal struct {
	Kind   string
	Layer  string
	Reason string
	Detail string
	Cause  error
}

var (
	ErrAbsenceOwnerless    = errors.New("productslice: absence kind has no owner")
	ErrAbsenceDoublyOwned  = errors.New("productslice: absence kind has multiple owners")
	ErrAbsenceInconsistent = errors.New("productslice: absence kind is inconsistent")
	ErrAbsenceUnknown      = errors.New("productslice: absence kind is not registered")
)

func (e *AbsenceRefusal) Error() string {
	return fmt.Sprintf("productslice: absence kind %q at layer %q: %s", e.Kind, e.Layer, e.Detail)
}

func (e *AbsenceRefusal) Unwrap() error { return e.Cause }

func absenceRefusal(kind, layer, detail string, cause error) error {
	return &AbsenceRefusal{Kind: kind, Layer: layer, Reason: cause.Error(), Detail: detail, Cause: cause}
}

func validAbsencePresentation(presentation string) bool {
	switch presentation {
	case AbsencePresentOmit, AbsencePresentPlaceholder, AbsencePresentDeferred:
		return true
	default:
		return false
	}
}

// Validate rejects ownerless, doubly-owned and internally inconsistent rows.
// The owner must observe its own absence signal and must not appear in the
// carry-only set. Omitted states must never carry a placeholder value rule.
func (v AbsenceVocabulary) Validate() error {
	if v.Schema != AbsenceVocabularySchema {
		return absenceRefusal("<vocabulary>", "governance", fmt.Sprintf("schema %q is not %q", v.Schema, AbsenceVocabularySchema), ErrAbsenceInconsistent)
	}
	if v.SchemaVersion != AbsenceVocabularySchemaVersion {
		return absenceRefusal("<vocabulary>", "governance", fmt.Sprintf("schema version %d is not %d", v.SchemaVersion, AbsenceVocabularySchemaVersion), ErrAbsenceInconsistent)
	}
	seen := make(map[string]AbsenceDefinition, len(v.States))
	for _, row := range v.States {
		kind := strings.TrimSpace(row.Kind)
		if kind == "" {
			return absenceRefusal("<empty>", "<none>", "kind is required", ErrAbsenceOwnerless)
		}
		owner := strings.TrimSpace(row.OwningLayer)
		if owner == "" {
			return absenceRefusal(kind, "<none>", "owning_layer is required", ErrAbsenceOwnerless)
		}
		if strings.TrimSpace(row.CanonicalForm) == "" || strings.TrimSpace(row.DisclosureRule) == "" {
			return absenceRefusal(kind, owner, "canonical_form and disclosure_rule are required", ErrAbsenceInconsistent)
		}
		presentation := strings.TrimSpace(row.Presentation)
		if !validAbsencePresentation(presentation) {
			return absenceRefusal(kind, owner, fmt.Sprintf("presentation %q is not omit, placeholder or deferred", row.Presentation), ErrAbsenceInconsistent)
		}
		observed, err := absenceLayerSet(row.ObservedBy, kind, owner, "observed_by")
		if err != nil {
			return err
		}
		carried, err := absenceLayerSet(row.CarriedBy, kind, owner, "carried_by")
		if err != nil {
			return err
		}
		if !observed[owner] {
			return absenceRefusal(kind, owner, "owning_layer must observe its own absence signal", ErrAbsenceInconsistent)
		}
		if carried[owner] {
			return absenceRefusal(kind, owner, "owning_layer cannot also be carry-only", ErrAbsenceInconsistent)
		}
		if prior, exists := seen[kind]; exists {
			layers := prior.OwningLayer + "," + owner
			if prior.OwningLayer != owner {
				return absenceRefusal(kind, layers, "the absence kind has two owning layers", ErrAbsenceDoublyOwned)
			}
			return absenceRefusal(kind, owner, "the absence kind has duplicate or conflicting rows", ErrAbsenceInconsistent)
		}
		row.Kind = kind
		row.OwningLayer = owner
		row.CanonicalForm = strings.TrimSpace(row.CanonicalForm)
		row.DisclosureRule = strings.TrimSpace(row.DisclosureRule)
		row.Presentation = presentation
		row.ObservedBy = sortedUnique(row.ObservedBy)
		row.CarriedBy = sortedUnique(row.CarriedBy)
		seen[kind] = row
	}
	return nil
}

func absenceLayerSet(layers []string, kind, owner, field string) (map[string]bool, error) {
	set := make(map[string]bool, len(layers))
	for _, layer := range layers {
		layer = strings.TrimSpace(layer)
		if layer == "" {
			return nil, absenceRefusal(kind, owner, field+" contains an empty layer", ErrAbsenceInconsistent)
		}
		if set[layer] {
			return nil, absenceRefusal(kind, layer, field+" contains a duplicate layer", ErrAbsenceInconsistent)
		}
		set[layer] = true
	}
	return set, nil
}

func (v AbsenceVocabulary) sorted() AbsenceVocabulary {
	out := v
	out.States = append([]AbsenceDefinition(nil), v.States...)
	for i := range out.States {
		out.States[i].Kind = strings.TrimSpace(out.States[i].Kind)
		out.States[i].OwningLayer = strings.TrimSpace(out.States[i].OwningLayer)
		out.States[i].CanonicalForm = strings.TrimSpace(out.States[i].CanonicalForm)
		out.States[i].DisclosureRule = strings.TrimSpace(out.States[i].DisclosureRule)
		out.States[i].Presentation = strings.TrimSpace(out.States[i].Presentation)
		out.States[i].ObservedBy = sortedUnique(out.States[i].ObservedBy)
		out.States[i].CarriedBy = sortedUnique(out.States[i].CarriedBy)
	}
	sort.Slice(out.States, func(i, j int) bool { return out.States[i].Kind < out.States[j].Kind })
	return out
}

// Canonical returns deterministic JSON for the vocabulary, excluding Digest.
func (v AbsenceVocabulary) Canonical() []byte {
	s := v.sorted()
	b, err := json.Marshal(struct {
		Schema        string              `json:"schema"`
		SchemaVersion int                 `json:"schema_version"`
		States        []AbsenceDefinition `json:"states"`
	}{s.Schema, s.SchemaVersion, s.States})
	if err != nil {
		return nil
	}
	return b
}

// DigestValue returns the SHA-256 identity of the canonical vocabulary.
func (v AbsenceVocabulary) DigestValue() string {
	sum := sha256.Sum256(v.Canonical())
	return "sha256:" + hex.EncodeToString(sum[:])
}

// VerifyDigest reports whether the checked-in digest matches the rows.
func (v AbsenceVocabulary) VerifyDigest() error {
	want := v.DigestValue()
	if v.Digest != want {
		return fmt.Errorf("productslice: absence vocabulary digest=%q, want %q", v.Digest, want)
	}
	return nil
}

// Resolve returns exactly one row for kind. A missing or duplicate kind is a
// typed refusal naming the requested kind and the registry layer.
func (v AbsenceVocabulary) Resolve(kind string) (AbsenceDefinition, error) {
	var found AbsenceDefinition
	count := 0
	for _, row := range v.States {
		if row.Kind == kind {
			found = row
			count++
		}
	}
	if count == 0 {
		return AbsenceDefinition{}, absenceRefusal(kind, "governance", "no vocabulary row resolves this kind", ErrAbsenceUnknown)
	}
	if count != 1 {
		return AbsenceDefinition{}, absenceRefusal(kind, "governance", fmt.Sprintf("%d vocabulary rows resolve this kind", count), ErrAbsenceDoublyOwned)
	}
	return found, nil
}

// OmittedKinds reports the kinds that must never appear in a product
// response, in sorted order.
func (v AbsenceVocabulary) OmittedKinds() []string {
	var out []string
	for _, row := range v.States {
		if strings.TrimSpace(row.Presentation) == AbsencePresentOmit {
			out = append(out, strings.TrimSpace(row.Kind))
		}
	}
	sort.Strings(out)
	return out
}

// Explain returns bounded, audit-safe structure: kind count and digest only,
// never an absence detail, disclosure decision, or placeholder content.
func (v AbsenceVocabulary) Explain() string {
	digest := v.Digest
	if digest == "" {
		digest = v.DigestValue()
	}
	return fmt.Sprintf("absence vocabulary schema %d with %d kinds (%s)", v.SchemaVersion, len(v.States), digest)
}

// NewAbsenceVocabulary builds a canonical vocabulary from rows.
func NewAbsenceVocabulary(rows ...AbsenceDefinition) AbsenceVocabulary {
	v := AbsenceVocabulary{
		Schema:        AbsenceVocabularySchema,
		SchemaVersion: AbsenceVocabularySchemaVersion,
		States:        append([]AbsenceDefinition(nil), rows...),
	}
	v.Digest = v.DigestValue()
	return v
}

// LoadAbsenceVocabularyYAML loads a vocabulary fixture or checked-in
// registry. Validation and digest verification remain explicit operations.
func LoadAbsenceVocabularyYAML(path string) (AbsenceVocabulary, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return AbsenceVocabulary{}, fmt.Errorf("productslice: reading absence vocabulary %s: %w", path, err)
	}
	var v AbsenceVocabulary
	if err := yaml.Unmarshal(data, &v); err != nil {
		return AbsenceVocabulary{}, fmt.Errorf("productslice: parsing absence vocabulary %s: %w", path, err)
	}
	return v, nil
}
