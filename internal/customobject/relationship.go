package customobject

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// CompiledRelationship is the immutable, digest-bearing relationship
// descriptor emitted after endpoint and semantic validation.
type CompiledRelationship struct {
	Definition RelationshipDefinition `json:"definition"`
	Digest     string                 `json:"digest"`
}

// CompileRelationship validates a definition against the published custom
// object set. Endpoint names may be either Name or Name/vN.
func CompileRelationship(d RelationshipDefinition, objects []CustomObjectType) (CompiledRelationship, error) {
	if err := d.Validate(); err != nil {
		return CompiledRelationship{}, err
	}
	known := map[string]struct{}{}
	for _, object := range objects {
		if _, err := Compile(object); err != nil {
			return CompiledRelationship{}, err
		}
		known[object.Name] = struct{}{}
		known[fmt.Sprintf("%s/v%d", object.Name, object.Version)] = struct{}{}
	}
	if _, ok := known[d.Source]; !ok {
		return CompiledRelationship{}, fmt.Errorf("%w: source %q", ErrDanglingEndpoint, d.Source)
	}
	if _, ok := known[d.Target]; !ok {
		return CompiledRelationship{}, fmt.Errorf("%w: target %q", ErrDanglingEndpoint, d.Target)
	}
	b, err := json.Marshal(d)
	if err != nil {
		return CompiledRelationship{}, err
	}
	return CompiledRelationship{Definition: d, Digest: digestBytes(b)}, nil
}

func digestBytes(b []byte) string {
	// Keep relationship digests aligned with schema digests without exposing
	// the SchemaFlux implementation as part of this contract.
	return canonicalbytes.Digest(b)
}

// Cardinality limits the number of targets a source may have at one effective
// instant.  It is deliberately a closed set: custom definitions cannot add
// executable or otherwise implicit cardinality rules.
type Cardinality string

const (
	CardinalityOneToOne   Cardinality = "ONE_TO_ONE"
	CardinalityOneToMany  Cardinality = "ONE_TO_MANY"
	CardinalityManyToMany Cardinality = "MANY_TO_MANY"
)

func (c Cardinality) Valid() bool {
	return c == CardinalityOneToOne || c == CardinalityOneToMany || c == CardinalityManyToMany
}

// Scope makes tenant isolation an explicit part of the relationship contract.
type Scope struct {
	TenantScoped bool   `json:"tenant_scoped"`
	Namespace    string `json:"namespace,omitempty"`
}

// TemporalSemantics declares which temporal facts a relationship carries.
type TemporalSemantics string

const (
	TemporalEffective  TemporalSemantics = "EFFECTIVE"
	TemporalBitemporal TemporalSemantics = "BITEMPORAL"
)

func (t TemporalSemantics) Valid() bool { return t == TemporalEffective || t == TemporalBitemporal }

// LifecycleSemantics declares how an edge is ended. There is no implicit
// delete: ended edges remain historical facts.
type LifecycleSemantics string

const (
	LifecycleAppendOnly LifecycleSemantics = "APPEND_ONLY"
	LifecycleSupersede  LifecycleSemantics = "SUPERSEDE"
)

func (l LifecycleSemantics) Valid() bool { return l == LifecycleAppendOnly || l == LifecycleSupersede }

var (
	ErrInvalidRelationship = errors.New("customobject: invalid relationship")
	ErrDanglingEndpoint    = errors.New("customobject: dangling relationship endpoint")
	ErrRelationshipCycle   = errors.New("customobject: relationship cycle")
	ErrInvalidCardinality  = errors.New("customobject: invalid cardinality")
	ErrAmbiguousInterval   = errors.New("customobject: ambiguous effective interval")
	ErrCrossTenantEdge     = errors.New("customobject: cross-tenant relationship")
)

// RelationshipDefinition is a typed relationship between two custom object
// types. Source and Target are canonical custom object names (optionally with
// /vN); Namespace and Version identify the relationship itself.
type RelationshipDefinition struct {
	Name        string             `json:"name"`
	Namespace   string             `json:"namespace"`
	Version     uint32             `json:"version"`
	Source      string             `json:"source"`
	Target      string             `json:"target"`
	Inverse     string             `json:"inverse,omitempty"`
	Scope       Scope              `json:"scope"`
	Cardinality Cardinality        `json:"cardinality"`
	Temporal    TemporalSemantics  `json:"temporal"`
	Lifecycle   LifecycleSemantics `json:"lifecycle"`
	Exclusive   bool               `json:"exclusive"`
	AllowCycles bool               `json:"allow_cycles"`
}

func (d RelationshipDefinition) Validate() error {
	if strings.TrimSpace(d.Name) == "" || strings.TrimSpace(d.Source) == "" || strings.TrimSpace(d.Target) == "" {
		return fmt.Errorf("%w: name and endpoints are required", ErrInvalidRelationship)
	}
	if d.Version == 0 {
		return fmt.Errorf("%w: version is required", ErrInvalidRelationship)
	}
	if !d.Cardinality.Valid() {
		return fmt.Errorf("%w: %q", ErrInvalidCardinality, d.Cardinality)
	}
	if d.Temporal == "" {
		return fmt.Errorf("%w: temporal semantics are required", ErrInvalidRelationship)
	}
	if !d.Temporal.Valid() {
		return fmt.Errorf("%w: temporal semantics %q", ErrInvalidRelationship, d.Temporal)
	}
	if d.Lifecycle == "" {
		return fmt.Errorf("%w: lifecycle semantics are required", ErrInvalidRelationship)
	}
	if !d.Lifecycle.Valid() {
		return fmt.Errorf("%w: lifecycle semantics %q", ErrInvalidRelationship, d.Lifecycle)
	}
	if d.Inverse == d.Name && d.Inverse != "" {
		return fmt.Errorf("%w: inverse cannot be itself", ErrInvalidRelationship)
	}
	return nil
}

// RelationshipFact is an effective-dated edge. Effective intervals are
// canonical half-open kernel intervals; Recorded and Known are required when
// bitemporal semantics are selected.
type RelationshipFact struct {
	Relationship string                   `json:"relationship"`
	Source       string                   `json:"source"`
	Target       string                   `json:"target"`
	SourceTenant string                   `json:"source_tenant"`
	TargetTenant string                   `json:"target_tenant"`
	Effective    values.EffectiveInterval `json:"effective"`
	Recorded     values.RecordedAt        `json:"recorded"`
	Known        values.KnownAt           `json:"known"`
}

func (f RelationshipFact) Validate(d RelationshipDefinition) error {
	if f.Relationship != d.Name || f.Source == "" || f.Target == "" {
		return fmt.Errorf("%w: fact does not identify this relationship and both endpoints", ErrInvalidRelationship)
	}
	if err := f.Effective.Validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrAmbiguousInterval, err)
	}
	if d.Scope.TenantScoped && (f.SourceTenant == "" || f.TargetTenant == "" || f.SourceTenant != f.TargetTenant) {
		return ErrCrossTenantEdge
	}
	if d.Temporal == TemporalBitemporal && (!f.Recorded.Instant().IsSet() || !f.Known.Instant().IsSet()) {
		return fmt.Errorf("%w: bitemporal fact requires recorded and known-at", ErrInvalidRelationship)
	}
	return nil
}

// ValidateFacts validates endpoint references, interval overlap, tenancy and
// cycle constraints for one custom relationship definition.
func ValidateFacts(d RelationshipDefinition, facts []RelationshipFact) error {
	if err := d.Validate(); err != nil {
		return err
	}
	var kind values.IntervalKind
	for i, f := range facts {
		if err := f.Validate(d); err != nil {
			return err
		}
		if i == 0 {
			kind = f.Effective.Kind()
		} else if f.Effective.Kind() != kind {
			return fmt.Errorf("%w: facts use mixed interval kinds", ErrAmbiguousInterval)
		}
	}
	if d.Exclusive || d.Cardinality == CardinalityOneToOne {
		for i := range facts {
			for j := i + 1; j < len(facts); j++ {
				if facts[i].Source == facts[j].Source {
					overlap, err := facts[i].Effective.Overlaps(facts[j].Effective)
					if err != nil {
						return fmt.Errorf("%w: %v", ErrAmbiguousInterval, err)
					}
					if overlap && facts[i].Target != facts[j].Target {
						return fmt.Errorf("%w: overlapping targets for %s", ErrInvalidRelationship, facts[i].Source)
					}
				}
			}
		}
	}
	if !d.AllowCycles {
		adj := map[string][]string{}
		for _, f := range facts {
			adj[f.Source] = append(adj[f.Source], f.Target)
		}
		if cycle := relationshipCycle(adj); len(cycle) > 0 {
			return fmt.Errorf("%w: %v", ErrRelationshipCycle, cycle)
		}
	}
	return nil
}

func relationshipCycle(adj map[string][]string) []string {
	color := map[string]uint8{}
	var path []string
	var visit func(string) []string
	visit = func(n string) []string {
		color[n] = 1
		path = append(path, n)
		for _, next := range adj[n] {
			if color[next] == 1 {
				return append(append([]string(nil), path...), next)
			}
			if color[next] == 0 {
				if c := visit(next); c != nil {
					return c
				}
			}
		}
		path = path[:len(path)-1]
		color[n] = 2
		return nil
	}
	nodes := make([]string, 0, len(adj))
	for n := range adj {
		nodes = append(nodes, n)
	}
	sort.Strings(nodes)
	for _, n := range nodes {
		if color[n] == 0 {
			if c := visit(n); c != nil {
				return c
			}
		}
	}
	return nil
}
