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

// TemporalVocabularySchema and TemporalVocabularySchemaVersion identify the
// checked-in shape of the cross-layer temporal vocabulary.
const (
	TemporalVocabularySchema        = "hcmnext.planning.productslices.temporal-vocabulary"
	TemporalVocabularySchemaVersion = 1
)

// Temporal time classes. Intervals are half-open; runtime timestamps never
// substitute for business-effective or legally applicable time, so instant
// and sequence kinds are never interchangeable either.
const (
	TimeClassInstant          = "instant"
	TimeClassHalfOpenInterval = "half_open_interval"
	TimeClassSequence         = "monotonic_sequence"
)

// TemporalDefinition is one stable temporal contract shared by the
// presentation, business and persistence lenses. The owning layer is the
// authority for the clock or sequence; other listed layers may only observe
// or carry the value across a boundary.
type TemporalDefinition struct {
	Kind          string   `yaml:"kind" json:"kind"`
	OwningLayer   string   `yaml:"owning_layer" json:"owning_layer"`
	CanonicalForm string   `yaml:"canonical_form" json:"canonical_form"`
	TimeClass     string   `yaml:"time_class" json:"time_class"`
	DigestRule    string   `yaml:"digest_rule" json:"digest_rule"`
	ObservedBy    []string `yaml:"observed_by" json:"observed_by"`
	CarriedBy     []string `yaml:"carried_by" json:"carried_by"`
}

// Comparable reports whether two temporal definitions may be substituted for
// each other. Only the same kind compares: an observed or recorded instant
// is never an effective instant, and a sequence is never a clock reading.
func (d TemporalDefinition) Comparable(other TemporalDefinition) bool {
	return d.Kind != "" && d.Kind == other.Kind
}

// TemporalVocabulary is the versioned registry of cross-layer temporal
// kinds. Digest is over the schema and sorted rows with Digest omitted.
type TemporalVocabulary struct {
	Schema        string               `yaml:"schema" json:"schema"`
	SchemaVersion int                  `yaml:"schema_version" json:"schema_version"`
	Moments       []TemporalDefinition `yaml:"moments" json:"moments"`
	Digest        string               `yaml:"digest" json:"digest"`
}

// TemporalRefusal is a typed refusal from vocabulary validation. It names
// both the temporal kind and the layer at which the contract is invalid so
// policy and audit output need not parse a free-form diagnostic.
type TemporalRefusal struct {
	Kind   string
	Layer  string
	Reason string
	Detail string
	Cause  error
}

var (
	ErrTemporalOwnerless    = errors.New("productslice: temporal kind has no owner")
	ErrTemporalDoublyOwned  = errors.New("productslice: temporal kind has multiple owners")
	ErrTemporalInconsistent = errors.New("productslice: temporal kind is inconsistent")
	ErrTemporalUnknown      = errors.New("productslice: temporal kind is not registered")
)

func (e *TemporalRefusal) Error() string {
	return fmt.Sprintf("productslice: temporal kind %q at layer %q: %s", e.Kind, e.Layer, e.Detail)
}

func (e *TemporalRefusal) Unwrap() error { return e.Cause }

func temporalRefusal(kind, layer, detail string, cause error) error {
	return &TemporalRefusal{Kind: kind, Layer: layer, Reason: cause.Error(), Detail: detail, Cause: cause}
}

func validTimeClass(class string) bool {
	switch class {
	case TimeClassInstant, TimeClassHalfOpenInterval, TimeClassSequence:
		return true
	default:
		return false
	}
}

// Validate rejects ownerless, doubly-owned and internally inconsistent rows.
// The owner must observe its own clock or sequence and must not appear in
// the carry-only set. Layer lists are treated as sets.
func (v TemporalVocabulary) Validate() error {
	if v.Schema != TemporalVocabularySchema {
		return temporalRefusal("<vocabulary>", "governance", fmt.Sprintf("schema %q is not %q", v.Schema, TemporalVocabularySchema), ErrTemporalInconsistent)
	}
	if v.SchemaVersion != TemporalVocabularySchemaVersion {
		return temporalRefusal("<vocabulary>", "governance", fmt.Sprintf("schema version %d is not %d", v.SchemaVersion, TemporalVocabularySchemaVersion), ErrTemporalInconsistent)
	}
	seen := make(map[string]TemporalDefinition, len(v.Moments))
	for _, row := range v.Moments {
		kind := strings.TrimSpace(row.Kind)
		if kind == "" {
			return temporalRefusal("<empty>", "<none>", "kind is required", ErrTemporalOwnerless)
		}
		owner := strings.TrimSpace(row.OwningLayer)
		if owner == "" {
			return temporalRefusal(kind, "<none>", "owning_layer is required", ErrTemporalOwnerless)
		}
		if strings.TrimSpace(row.CanonicalForm) == "" || strings.TrimSpace(row.DigestRule) == "" {
			return temporalRefusal(kind, owner, "canonical_form and digest_rule are required", ErrTemporalInconsistent)
		}
		if !validTimeClass(strings.TrimSpace(row.TimeClass)) {
			return temporalRefusal(kind, owner, fmt.Sprintf("time_class %q is not a known temporal class", row.TimeClass), ErrTemporalInconsistent)
		}
		observed, err := temporalLayerSet(row.ObservedBy, kind, owner, "observed_by")
		if err != nil {
			return err
		}
		carried, err := temporalLayerSet(row.CarriedBy, kind, owner, "carried_by")
		if err != nil {
			return err
		}
		if !observed[owner] {
			return temporalRefusal(kind, owner, "owning_layer must observe its own clock or sequence", ErrTemporalInconsistent)
		}
		if carried[owner] {
			return temporalRefusal(kind, owner, "owning_layer cannot also be carry-only", ErrTemporalInconsistent)
		}
		if prior, exists := seen[kind]; exists {
			layers := prior.OwningLayer + "," + owner
			if prior.OwningLayer != owner {
				return temporalRefusal(kind, layers, "the temporal kind has two owning layers", ErrTemporalDoublyOwned)
			}
			return temporalRefusal(kind, owner, "the temporal kind has duplicate or conflicting rows", ErrTemporalInconsistent)
		}
		row.Kind = kind
		row.OwningLayer = owner
		row.TimeClass = strings.TrimSpace(row.TimeClass)
		row.ObservedBy = sortedUnique(row.ObservedBy)
		row.CarriedBy = sortedUnique(row.CarriedBy)
		seen[kind] = row
	}
	return nil
}

func temporalLayerSet(layers []string, kind, owner, field string) (map[string]bool, error) {
	set := make(map[string]bool, len(layers))
	for _, layer := range layers {
		layer = strings.TrimSpace(layer)
		if layer == "" {
			return nil, temporalRefusal(kind, owner, field+" contains an empty layer", ErrTemporalInconsistent)
		}
		if set[layer] {
			return nil, temporalRefusal(kind, layer, field+" contains a duplicate layer", ErrTemporalInconsistent)
		}
		set[layer] = true
	}
	return set, nil
}

func (v TemporalVocabulary) sorted() TemporalVocabulary {
	out := v
	out.Moments = append([]TemporalDefinition(nil), v.Moments...)
	for i := range out.Moments {
		out.Moments[i].Kind = strings.TrimSpace(out.Moments[i].Kind)
		out.Moments[i].OwningLayer = strings.TrimSpace(out.Moments[i].OwningLayer)
		out.Moments[i].CanonicalForm = strings.TrimSpace(out.Moments[i].CanonicalForm)
		out.Moments[i].TimeClass = strings.TrimSpace(out.Moments[i].TimeClass)
		out.Moments[i].DigestRule = strings.TrimSpace(out.Moments[i].DigestRule)
		out.Moments[i].ObservedBy = sortedUnique(out.Moments[i].ObservedBy)
		out.Moments[i].CarriedBy = sortedUnique(out.Moments[i].CarriedBy)
	}
	sort.Slice(out.Moments, func(i, j int) bool { return out.Moments[i].Kind < out.Moments[j].Kind })
	return out
}

// Canonical returns deterministic JSON for the vocabulary, excluding Digest.
func (v TemporalVocabulary) Canonical() []byte {
	s := v.sorted()
	b, err := json.Marshal(struct {
		Schema        string               `json:"schema"`
		SchemaVersion int                  `json:"schema_version"`
		Moments       []TemporalDefinition `json:"moments"`
	}{s.Schema, s.SchemaVersion, s.Moments})
	if err != nil {
		return nil
	}
	return b
}

// DigestValue returns the SHA-256 identity of the canonical vocabulary.
func (v TemporalVocabulary) DigestValue() string {
	sum := sha256.Sum256(v.Canonical())
	return "sha256:" + hex.EncodeToString(sum[:])
}

// VerifyDigest reports whether the checked-in digest matches the rows.
func (v TemporalVocabulary) VerifyDigest() error {
	want := v.DigestValue()
	if v.Digest != want {
		return fmt.Errorf("productslice: temporal vocabulary digest=%q, want %q", v.Digest, want)
	}
	return nil
}

// Resolve returns exactly one row for kind. A missing or duplicate kind is a
// typed refusal naming the requested kind and the registry layer.
func (v TemporalVocabulary) Resolve(kind string) (TemporalDefinition, error) {
	var found TemporalDefinition
	count := 0
	for _, row := range v.Moments {
		if row.Kind == kind {
			found = row
			count++
		}
	}
	if count == 0 {
		return TemporalDefinition{}, temporalRefusal(kind, "governance", "no vocabulary row resolves this kind", ErrTemporalUnknown)
	}
	if count != 1 {
		return TemporalDefinition{}, temporalRefusal(kind, "governance", fmt.Sprintf("%d vocabulary rows resolve this kind", count), ErrTemporalDoublyOwned)
	}
	return found, nil
}

// Explain returns bounded, audit-safe structure: kind count and digest only,
// never a clock reading, sequence position, or interval bound.
func (v TemporalVocabulary) Explain() string {
	digest := v.Digest
	if digest == "" {
		digest = v.DigestValue()
	}
	return fmt.Sprintf("temporal vocabulary schema %d with %d kinds (%s)", v.SchemaVersion, len(v.Moments), digest)
}

// NewTemporalVocabulary builds a canonical vocabulary from rows.
func NewTemporalVocabulary(rows ...TemporalDefinition) TemporalVocabulary {
	v := TemporalVocabulary{
		Schema:        TemporalVocabularySchema,
		SchemaVersion: TemporalVocabularySchemaVersion,
		Moments:       append([]TemporalDefinition(nil), rows...),
	}
	v.Digest = v.DigestValue()
	return v
}

// LoadTemporalVocabularyYAML loads a vocabulary fixture or checked-in
// registry. Validation and digest verification remain explicit operations.
func LoadTemporalVocabularyYAML(path string) (TemporalVocabulary, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return TemporalVocabulary{}, fmt.Errorf("productslice: reading temporal vocabulary %s: %w", path, err)
	}
	var v TemporalVocabulary
	if err := yaml.Unmarshal(data, &v); err != nil {
		return TemporalVocabulary{}, fmt.Errorf("productslice: parsing temporal vocabulary %s: %w", path, err)
	}
	return v, nil
}

// MarshalTemporalVocabularyYAML recomputes the digest and renders the
// canonical YAML representation used by the fixture generator.
func MarshalTemporalVocabularyYAML(v TemporalVocabulary) ([]byte, error) {
	v.Digest = v.DigestValue()
	data, err := yaml.Marshal(v.sorted())
	if err != nil {
		return nil, fmt.Errorf("productslice: marshaling temporal vocabulary: %w", err)
	}
	return data, nil
}
