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

// OwnershipSchema and OwnershipSchemaVersion identify the checked-in shape
// of the product-slice ownership and consumer registry.
const (
	OwnershipSchema        = "hcmnext.planning.productslices.slice-ownership"
	OwnershipSchemaVersion = 1
)

// SliceDeclaration names the owning layer of one product slice and every
// consumer admitted to read it. The owner is the single authority for the
// slice's canonical semantics; consumers are registered page, widget, or
// capability contracts that may observe the slice through the authorized
// product-query path. DefinitionDigest pins the slice definition the
// declaration was made against.
type SliceDeclaration struct {
	SliceID          string   `yaml:"slice_id" json:"slice_id"`
	Owner            string   `yaml:"owner" json:"owner"`
	Consumers        []string `yaml:"consumers" json:"consumers"`
	DefinitionDigest string   `yaml:"definition_digest" json:"definition_digest"`
}

// OwnershipRegistry is the versioned registry of slice ownership and
// consumer declarations. Digest is over the schema and sorted declarations
// with Digest omitted.
type OwnershipRegistry struct {
	Schema        string             `yaml:"schema" json:"schema"`
	SchemaVersion int                `yaml:"schema_version" json:"schema_version"`
	Slices        []SliceDeclaration `yaml:"slices" json:"slices"`
	Digest        string             `yaml:"digest" json:"digest"`
}

// OwnershipRefusal is a typed refusal from registry validation. It names
// the slice at which the contract is invalid so policy and audit output
// need not parse a free-form diagnostic.
type OwnershipRefusal struct {
	Slice  string
	Layer  string
	Reason string
	Detail string
	Cause  error
}

var (
	ErrOwnershipOwnerless    = errors.New("productslice: product slice has no owner")
	ErrOwnershipDoublyOwned  = errors.New("productslice: product slice has multiple owners")
	ErrOwnershipInconsistent = errors.New("productslice: product slice declaration is inconsistent")
	ErrOwnershipUnknown      = errors.New("productslice: product slice is not declared")
	ErrOwnershipConsumerless = errors.New("productslice: product slice has no admitted consumer")
)

func (e *OwnershipRefusal) Error() string {
	return fmt.Sprintf("productslice: product slice %q at layer %q: %s", e.Slice, e.Layer, e.Detail)
}

func (e *OwnershipRefusal) Unwrap() error { return e.Cause }

func ownershipRefusal(slice, layer, detail string, cause error) error {
	return &OwnershipRefusal{Slice: slice, Layer: layer, Reason: cause.Error(), Detail: detail, Cause: cause}
}

func safeContractToken(s string) bool {
	if len(s) == 0 || len(s) > 128 || strings.TrimSpace(s) != s {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c != '.' && c != '-' && c != '_' && !(c >= 'a' && c <= 'z') && !(c >= '0' && c <= '9') {
			return false
		}
	}
	return true
}

// Validate enforces one owner and at least one admitted consumer per slice.
// An ownerless slice has no canonical authority; a consumerless slice has
// no governed reader; an owner that also consumes its own slice blurs the
// authority boundary the registry exists to keep.
func (r OwnershipRegistry) Validate() error {
	if r.Schema != OwnershipSchema {
		return ownershipRefusal("<registry>", "governance", fmt.Sprintf("schema %q is not %q", r.Schema, OwnershipSchema), ErrOwnershipInconsistent)
	}
	if r.SchemaVersion != OwnershipSchemaVersion {
		return ownershipRefusal("<registry>", "governance", fmt.Sprintf("schema version %d is not %d", r.SchemaVersion, OwnershipSchemaVersion), ErrOwnershipInconsistent)
	}
	seen := make(map[string]SliceDeclaration, len(r.Slices))
	for _, row := range r.Slices {
		id := strings.TrimSpace(row.SliceID)
		if !safeContractToken(id) {
			return ownershipRefusal(id, "<none>", "slice_id must be a non-empty safe token", ErrOwnershipInconsistent)
		}
		owner := strings.TrimSpace(row.Owner)
		if owner == "" {
			return ownershipRefusal(id, "<none>", "exactly one owner is required", ErrOwnershipOwnerless)
		}
		if !safeContractToken(owner) {
			return ownershipRefusal(id, owner, "owner must be a safe contract token", ErrOwnershipInconsistent)
		}
		if strings.TrimSpace(row.DefinitionDigest) == "" {
			return ownershipRefusal(id, owner, "definition_digest is required", ErrOwnershipInconsistent)
		}
		if len(row.Consumers) == 0 {
			return ownershipRefusal(id, owner, "at least one admitted consumer is required", ErrOwnershipConsumerless)
		}
		consumers := make(map[string]bool, len(row.Consumers))
		for _, consumer := range row.Consumers {
			consumer = strings.TrimSpace(consumer)
			if !safeContractToken(consumer) {
				return ownershipRefusal(id, owner, "consumers must be non-empty safe tokens", ErrOwnershipInconsistent)
			}
			if consumer == owner {
				return ownershipRefusal(id, owner, "owner cannot also consume its own slice", ErrOwnershipInconsistent)
			}
			if consumers[consumer] {
				return ownershipRefusal(id, owner, fmt.Sprintf("consumer %q is declared twice", consumer), ErrOwnershipInconsistent)
			}
			consumers[consumer] = true
		}
		if prior, exists := seen[id]; exists {
			if prior.Owner != owner {
				return ownershipRefusal(id, prior.Owner+","+owner, "the product slice has two owners", ErrOwnershipDoublyOwned)
			}
			return ownershipRefusal(id, owner, "the product slice has duplicate declarations", ErrOwnershipInconsistent)
		}
		row.SliceID = id
		row.Owner = owner
		row.DefinitionDigest = strings.TrimSpace(row.DefinitionDigest)
		row.Consumers = sortedUnique(row.Consumers)
		seen[id] = row
	}
	return nil
}

// Admits reports whether consumer may read the named slice. Unknown slices
// and undeclared consumers are refused without distinguishing them.
func (r OwnershipRegistry) Admits(sliceID, consumer string) bool {
	for _, row := range r.Slices {
		if strings.TrimSpace(row.SliceID) != sliceID {
			continue
		}
		for _, admitted := range row.Consumers {
			if strings.TrimSpace(admitted) == consumer {
				return true
			}
		}
		return false
	}
	return false
}

func (r OwnershipRegistry) sorted() OwnershipRegistry {
	out := r
	out.Slices = append([]SliceDeclaration(nil), r.Slices...)
	for i := range out.Slices {
		out.Slices[i].SliceID = strings.TrimSpace(out.Slices[i].SliceID)
		out.Slices[i].Owner = strings.TrimSpace(out.Slices[i].Owner)
		out.Slices[i].DefinitionDigest = strings.TrimSpace(out.Slices[i].DefinitionDigest)
		out.Slices[i].Consumers = sortedUnique(out.Slices[i].Consumers)
	}
	sort.Slice(out.Slices, func(i, j int) bool { return out.Slices[i].SliceID < out.Slices[j].SliceID })
	return out
}

// Canonical returns deterministic JSON for the registry, excluding Digest.
func (r OwnershipRegistry) Canonical() []byte {
	s := r.sorted()
	b, err := json.Marshal(struct {
		Schema        string             `json:"schema"`
		SchemaVersion int                `json:"schema_version"`
		Slices        []SliceDeclaration `json:"slices"`
	}{s.Schema, s.SchemaVersion, s.Slices})
	if err != nil {
		return nil
	}
	return b
}

// DigestValue returns the SHA-256 identity of the canonical registry.
func (r OwnershipRegistry) DigestValue() string {
	sum := sha256.Sum256(r.Canonical())
	return "sha256:" + hex.EncodeToString(sum[:])
}

// VerifyDigest reports whether the checked-in digest matches the rows.
func (r OwnershipRegistry) VerifyDigest() error {
	want := r.DigestValue()
	if r.Digest != want {
		return fmt.Errorf("productslice: slice ownership digest=%q, want %q", r.Digest, want)
	}
	return nil
}

// Resolve returns exactly one declaration for a slice. A missing or
// duplicate slice is a typed refusal naming the requested slice.
func (r OwnershipRegistry) Resolve(sliceID string) (SliceDeclaration, error) {
	var found SliceDeclaration
	count := 0
	for _, row := range r.Slices {
		if row.SliceID == sliceID {
			found = row
			count++
		}
	}
	if count == 0 {
		return SliceDeclaration{}, ownershipRefusal(sliceID, "governance", "no ownership declaration covers this slice", ErrOwnershipUnknown)
	}
	if count != 1 {
		return SliceDeclaration{}, ownershipRefusal(sliceID, "governance", fmt.Sprintf("%d ownership declarations cover this slice", count), ErrOwnershipDoublyOwned)
	}
	return found, nil
}

// VerifyDefinition confirms that the registry declaration pins the supplied
// live definition. This binds ownership to the exact slice contract instead
// of merely checking that some non-empty digest was recorded.
func (r OwnershipRegistry) VerifyDefinition(definition ProductSliceDefinition) error {
	declaration, err := r.Resolve(definition.SliceID)
	if err != nil {
		return err
	}
	want := definition.Digest()
	if declaration.DefinitionDigest != want {
		return ownershipRefusal(definition.SliceID, declaration.Owner,
			fmt.Sprintf("definition digest %q does not match live definition %q", declaration.DefinitionDigest, want),
			ErrOwnershipInconsistent)
	}
	return nil
}

// Explain returns bounded, audit-safe structure: slice count and digest
// only, never an owner, consumer, or definition digest.
func (r OwnershipRegistry) Explain() string {
	digest := r.Digest
	if digest == "" {
		digest = r.DigestValue()
	}
	return fmt.Sprintf("slice ownership schema %d with %d slices (%s)", r.SchemaVersion, len(r.Slices), digest)
}

// NewOwnershipRegistry builds a canonical registry from declarations.
func NewOwnershipRegistry(rows ...SliceDeclaration) OwnershipRegistry {
	r := OwnershipRegistry{
		Schema:        OwnershipSchema,
		SchemaVersion: OwnershipSchemaVersion,
		Slices:        append([]SliceDeclaration(nil), rows...),
	}
	r.Digest = r.DigestValue()
	return r
}

// LoadOwnershipRegistryYAML loads an ownership fixture or checked-in
// registry. Validation and digest verification remain explicit operations.
func LoadOwnershipRegistryYAML(path string) (OwnershipRegistry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return OwnershipRegistry{}, fmt.Errorf("productslice: reading slice ownership %s: %w", path, err)
	}
	var r OwnershipRegistry
	if err := yaml.Unmarshal(data, &r); err != nil {
		return OwnershipRegistry{}, fmt.Errorf("productslice: parsing slice ownership %s: %w", path, err)
	}
	return r, nil
}
