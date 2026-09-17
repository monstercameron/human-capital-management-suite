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

// LifecycleVocabularySchema and LifecycleVocabularySchemaVersion identify
// the checked-in shape of the multidimensional lifecycle projection
// vocabulary.
const (
	LifecycleVocabularySchema        = "hcmnext.planning.productslices.lifecycle-vocabulary"
	LifecycleVocabularySchemaVersion = 1
)

// Admitted lifecycle dimensions. A projection row must span at least one of
// these axes; inventing an axis outside the set is a contract change, not a
// row addition.
var admittedLifecycleDimensions = map[string]bool{
	"employment": true,
	"workflow":   true,
	"proposal":   true,
	"assignment": true,
	"approval":   true,
	"work_item":  true,
	"payroll":    true,
}

// LifecycleDefinition is one stable lifecycle contract shared by the
// presentation, business and persistence lenses. Dimensions name the
// lifecycle axes the projection joins (employment, workflow, proposal and
// friends); ProjectionRule states how those axes combine into the projected
// product state. The owning layer is the authority for the projection; other
// listed layers may only observe or carry the value across a boundary.
type LifecycleDefinition struct {
	Kind           string   `yaml:"kind" json:"kind"`
	OwningLayer    string   `yaml:"owning_layer" json:"owning_layer"`
	CanonicalForm  string   `yaml:"canonical_form" json:"canonical_form"`
	ProjectionRule string   `yaml:"projection_rule" json:"projection_rule"`
	Dimensions     []string `yaml:"dimensions" json:"dimensions"`
	ObservedBy     []string `yaml:"observed_by" json:"observed_by"`
	CarriedBy      []string `yaml:"carried_by" json:"carried_by"`
}

// Comparable reports whether two lifecycle definitions may be substituted
// for each other. Only the same kind compares: a workflow stage is never an
// employment status, even when both project onto the same product surface.
func (d LifecycleDefinition) Comparable(other LifecycleDefinition) bool {
	return d.Kind != "" && d.Kind == other.Kind
}

// LifecycleVocabulary is the versioned registry of multidimensional
// lifecycle projections. Digest is over the schema and sorted rows with
// Digest omitted.
type LifecycleVocabulary struct {
	Schema        string                `yaml:"schema" json:"schema"`
	SchemaVersion int                   `yaml:"schema_version" json:"schema_version"`
	Projections   []LifecycleDefinition `yaml:"projections" json:"projections"`
	Digest        string                `yaml:"digest" json:"digest"`
}

// LifecycleRefusal is a typed refusal from vocabulary validation. It names
// both the lifecycle kind and the layer at which the contract is invalid so
// policy and audit output need not parse a free-form diagnostic.
type LifecycleRefusal struct {
	Kind   string
	Layer  string
	Reason string
	Detail string
	Cause  error
}

var (
	ErrLifecycleOwnerless    = errors.New("productslice: lifecycle kind has no owner")
	ErrLifecycleDoublyOwned  = errors.New("productslice: lifecycle kind has multiple owners")
	ErrLifecycleInconsistent = errors.New("productslice: lifecycle kind is inconsistent")
	ErrLifecycleUnknown      = errors.New("productslice: lifecycle kind is not registered")
)

func (e *LifecycleRefusal) Error() string {
	return fmt.Sprintf("productslice: lifecycle kind %q at layer %q: %s", e.Kind, e.Layer, e.Detail)
}

func (e *LifecycleRefusal) Unwrap() error { return e.Cause }

func lifecycleRefusal(kind, layer, detail string, cause error) error {
	return &LifecycleRefusal{Kind: kind, Layer: layer, Reason: cause.Error(), Detail: detail, Cause: cause}
}

// Validate rejects ownerless, doubly-owned and internally inconsistent rows.
// The owner must observe its own projection and must not appear in the
// carry-only set. Every row must span at least one admitted dimension.
func (v LifecycleVocabulary) Validate() error {
	if v.Schema != LifecycleVocabularySchema {
		return lifecycleRefusal("<vocabulary>", "governance", fmt.Sprintf("schema %q is not %q", v.Schema, LifecycleVocabularySchema), ErrLifecycleInconsistent)
	}
	if v.SchemaVersion != LifecycleVocabularySchemaVersion {
		return lifecycleRefusal("<vocabulary>", "governance", fmt.Sprintf("schema version %d is not %d", v.SchemaVersion, LifecycleVocabularySchemaVersion), ErrLifecycleInconsistent)
	}
	seen := make(map[string]LifecycleDefinition, len(v.Projections))
	for _, row := range v.Projections {
		kind := strings.TrimSpace(row.Kind)
		if kind == "" {
			return lifecycleRefusal("<empty>", "<none>", "kind is required", ErrLifecycleOwnerless)
		}
		owner := strings.TrimSpace(row.OwningLayer)
		if owner == "" {
			return lifecycleRefusal(kind, "<none>", "owning_layer is required", ErrLifecycleOwnerless)
		}
		if strings.TrimSpace(row.CanonicalForm) == "" || strings.TrimSpace(row.ProjectionRule) == "" {
			return lifecycleRefusal(kind, owner, "canonical_form and projection_rule are required", ErrLifecycleInconsistent)
		}
		if len(row.Dimensions) == 0 {
			return lifecycleRefusal(kind, owner, "at least one lifecycle dimension is required", ErrLifecycleInconsistent)
		}
		dims := make(map[string]bool, len(row.Dimensions))
		for _, dim := range row.Dimensions {
			dim = strings.TrimSpace(dim)
			if dim == "" || dims[dim] {
				return lifecycleRefusal(kind, owner, "dimensions must be non-empty and unique", ErrLifecycleInconsistent)
			}
			if !admittedLifecycleDimensions[dim] {
				return lifecycleRefusal(kind, owner, fmt.Sprintf("dimension %q is not admitted", dim), ErrLifecycleInconsistent)
			}
			dims[dim] = true
		}
		observed, err := lifecycleLayerSet(row.ObservedBy, kind, owner, "observed_by")
		if err != nil {
			return err
		}
		carried, err := lifecycleLayerSet(row.CarriedBy, kind, owner, "carried_by")
		if err != nil {
			return err
		}
		if !observed[owner] {
			return lifecycleRefusal(kind, owner, "owning_layer must observe its own projection", ErrLifecycleInconsistent)
		}
		if carried[owner] {
			return lifecycleRefusal(kind, owner, "owning_layer cannot also be carry-only", ErrLifecycleInconsistent)
		}
		if prior, exists := seen[kind]; exists {
			layers := prior.OwningLayer + "," + owner
			if prior.OwningLayer != owner {
				return lifecycleRefusal(kind, layers, "the lifecycle kind has two owning layers", ErrLifecycleDoublyOwned)
			}
			return lifecycleRefusal(kind, owner, "the lifecycle kind has duplicate or conflicting rows", ErrLifecycleInconsistent)
		}
		row.Kind = kind
		row.OwningLayer = owner
		row.CanonicalForm = strings.TrimSpace(row.CanonicalForm)
		row.ProjectionRule = strings.TrimSpace(row.ProjectionRule)
		row.Dimensions = sortedUnique(row.Dimensions)
		row.ObservedBy = sortedUnique(row.ObservedBy)
		row.CarriedBy = sortedUnique(row.CarriedBy)
		seen[kind] = row
	}
	return nil
}

func lifecycleLayerSet(layers []string, kind, owner, field string) (map[string]bool, error) {
	set := make(map[string]bool, len(layers))
	for _, layer := range layers {
		layer = strings.TrimSpace(layer)
		if layer == "" {
			return nil, lifecycleRefusal(kind, owner, field+" contains an empty layer", ErrLifecycleInconsistent)
		}
		if set[layer] {
			return nil, lifecycleRefusal(kind, layer, field+" contains a duplicate layer", ErrLifecycleInconsistent)
		}
		set[layer] = true
	}
	return set, nil
}

func (v LifecycleVocabulary) sorted() LifecycleVocabulary {
	out := v
	out.Projections = append([]LifecycleDefinition(nil), v.Projections...)
	for i := range out.Projections {
		out.Projections[i].Kind = strings.TrimSpace(out.Projections[i].Kind)
		out.Projections[i].OwningLayer = strings.TrimSpace(out.Projections[i].OwningLayer)
		out.Projections[i].CanonicalForm = strings.TrimSpace(out.Projections[i].CanonicalForm)
		out.Projections[i].ProjectionRule = strings.TrimSpace(out.Projections[i].ProjectionRule)
		out.Projections[i].Dimensions = sortedUnique(out.Projections[i].Dimensions)
		out.Projections[i].ObservedBy = sortedUnique(out.Projections[i].ObservedBy)
		out.Projections[i].CarriedBy = sortedUnique(out.Projections[i].CarriedBy)
	}
	sort.Slice(out.Projections, func(i, j int) bool { return out.Projections[i].Kind < out.Projections[j].Kind })
	return out
}

// Canonical returns deterministic JSON for the vocabulary, excluding Digest.
func (v LifecycleVocabulary) Canonical() []byte {
	s := v.sorted()
	b, err := json.Marshal(struct {
		Schema        string                `json:"schema"`
		SchemaVersion int                   `json:"schema_version"`
		Projections   []LifecycleDefinition `json:"projections"`
	}{s.Schema, s.SchemaVersion, s.Projections})
	if err != nil {
		return nil
	}
	return b
}

// DigestValue returns the SHA-256 identity of the canonical vocabulary.
func (v LifecycleVocabulary) DigestValue() string {
	sum := sha256.Sum256(v.Canonical())
	return "sha256:" + hex.EncodeToString(sum[:])
}

// VerifyDigest reports whether the checked-in digest matches the rows.
func (v LifecycleVocabulary) VerifyDigest() error {
	want := v.DigestValue()
	if v.Digest != want {
		return fmt.Errorf("productslice: lifecycle vocabulary digest=%q, want %q", v.Digest, want)
	}
	return nil
}

// Resolve returns exactly one row for kind. A missing or duplicate kind is a
// typed refusal naming the requested kind and the registry layer.
func (v LifecycleVocabulary) Resolve(kind string) (LifecycleDefinition, error) {
	var found LifecycleDefinition
	count := 0
	for _, row := range v.Projections {
		if row.Kind == kind {
			found = row
			count++
		}
	}
	if count == 0 {
		return LifecycleDefinition{}, lifecycleRefusal(kind, "governance", "no vocabulary row resolves this kind", ErrLifecycleUnknown)
	}
	if count != 1 {
		return LifecycleDefinition{}, lifecycleRefusal(kind, "governance", fmt.Sprintf("%d vocabulary rows resolve this kind", count), ErrLifecycleDoublyOwned)
	}
	return found, nil
}

// DimensionsCovered reports the admitted dimensions spanned by the
// vocabulary, in sorted order.
func (v LifecycleVocabulary) DimensionsCovered() []string {
	set := make(map[string]bool)
	for _, row := range v.Projections {
		for _, dim := range row.Dimensions {
			set[strings.TrimSpace(dim)] = true
		}
	}
	out := make([]string, 0, len(set))
	for dim := range set {
		out = append(out, dim)
	}
	sort.Strings(out)
	return out
}

// Explain returns bounded, audit-safe structure: kind count and digest only,
// never a lifecycle value, projection input, or dimension member.
func (v LifecycleVocabulary) Explain() string {
	digest := v.Digest
	if digest == "" {
		digest = v.DigestValue()
	}
	return fmt.Sprintf("lifecycle vocabulary schema %d with %d kinds (%s)", v.SchemaVersion, len(v.Projections), digest)
}

// NewLifecycleVocabulary builds a canonical vocabulary from rows.
func NewLifecycleVocabulary(rows ...LifecycleDefinition) LifecycleVocabulary {
	v := LifecycleVocabulary{
		Schema:        LifecycleVocabularySchema,
		SchemaVersion: LifecycleVocabularySchemaVersion,
		Projections:   append([]LifecycleDefinition(nil), rows...),
	}
	v.Digest = v.DigestValue()
	return v
}

// LoadLifecycleVocabularyYAML loads a vocabulary fixture or checked-in
// registry. Validation and digest verification remain explicit operations.
func LoadLifecycleVocabularyYAML(path string) (LifecycleVocabulary, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return LifecycleVocabulary{}, fmt.Errorf("productslice: reading lifecycle vocabulary %s: %w", path, err)
	}
	var v LifecycleVocabulary
	if err := yaml.Unmarshal(data, &v); err != nil {
		return LifecycleVocabulary{}, fmt.Errorf("productslice: parsing lifecycle vocabulary %s: %w", path, err)
	}
	return v, nil
}
