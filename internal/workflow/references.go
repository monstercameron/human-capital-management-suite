package workflow

import (
	"errors"
	"fmt"
	"sort"
	"strconv"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/rules"
)

// ReferenceKind names the published registry a definition reference resolves
// against. Capability versions keep their own [CapabilityResolver]; every
// other immutable artifact a definition names by identity resolves through
// one [ReferenceResolver] (WF-COMP-007).
type ReferenceKind string

// The reference kinds the compiler resolves.
const (
	// RefSchema is a versioned payload schema (input, output, variables or an
	// expected signal schema).
	RefSchema ReferenceKind = "SCHEMA"
	// RefRule is a published decision table or expression a DECISION cites.
	RefRule ReferenceKind = "RULE"
	// RefResolver is a published assignee/approver resolver a node binds.
	RefResolver ReferenceKind = "RESOLVER"
	// RefTimeoutPolicy is a published timeout policy a node binds.
	RefTimeoutPolicy ReferenceKind = "TIMEOUT_POLICY"
	// RefCompensation is a published compensation a node binds.
	RefCompensation ReferenceKind = "COMPENSATION"
)

// Valid reports whether k names a declared reference kind.
func (k ReferenceKind) Valid() bool {
	switch k {
	case RefSchema, RefRule, RefResolver, RefTimeoutPolicy, RefCompensation:
		return true
	default:
		return false
	}
}

// VersionedRef is a definition's pointer to one immutable published artifact:
// an identity and an exact version, never "latest".
type VersionedRef struct {
	ID      string `json:"id"`
	Version string `json:"version"`
}

func (r VersionedRef) String() string { return r.ID + "@" + r.Version }

// Reference is one lookup the compiler asks a [ReferenceResolver] to answer.
type Reference struct {
	Kind    ReferenceKind
	ID      string
	Version string
}

func (r Reference) String() string { return string(r.Kind) + ":" + r.ID + "@" + r.Version }

// ReferenceStatus is a published artifact's lifecycle status.
type ReferenceStatus string

// The declared reference statuses. A retired target exists for history only:
// a new plan may not bind it.
const (
	ReferencePublished  ReferenceStatus = "PUBLISHED"
	ReferenceDeprecated ReferenceStatus = "DEPRECATED"
	ReferenceRetired    ReferenceStatus = "RETIRED"
)

// Valid reports whether s names a declared reference status.
func (s ReferenceStatus) Valid() bool {
	switch s {
	case ReferencePublished, ReferenceDeprecated, ReferenceRetired:
		return true
	default:
		return false
	}
}

// ResolvedReference is what a published registry says about one reference:
// its exact identity, the content digest that pins it and its status. The
// compiler copies it into the plan, so the plan digest pins the target.
type ResolvedReference struct {
	Kind    ReferenceKind   `json:"kind"`
	ID      string          `json:"id"`
	Version string          `json:"version"`
	Digest  string          `json:"digest"`
	Status  ReferenceStatus `json:"status"`
	// Descriptor is the protobuf full name a SCHEMA target declares. It is
	// empty for every other kind.
	Descriptor string `json:"descriptor,omitempty"`
}

func (r ResolvedReference) key() Reference {
	return Reference{Kind: r.Kind, ID: r.ID, Version: r.Version}
}

// ReferenceResolver answers reference lookups against published registries.
// The compiler never fetches anything; a resolver returns false for a target
// that was never published.
type ReferenceResolver interface {
	ResolveReference(ref Reference) (ResolvedReference, bool)
}

// ErrInvalidReference classifies a registry entry that cannot be published.
var ErrInvalidReference = errors.New("workflow: invalid published reference")

// ReferenceRegistry is an immutable published reference set. It is a value
// built once by its owner; there is no package-level registry.
type ReferenceRegistry struct {
	entries map[Reference]ResolvedReference
}

var _ ReferenceResolver = (*ReferenceRegistry)(nil)

// NewReferenceRegistry publishes entries, refusing a blank identity, an
// undigested entry, an undeclared kind or status, and a duplicate identity.
func NewReferenceRegistry(entries ...ResolvedReference) (*ReferenceRegistry, error) {
	r := &ReferenceRegistry{entries: make(map[Reference]ResolvedReference, len(entries))}
	for _, e := range entries {
		switch {
		case !e.Kind.Valid():
			return nil, fmt.Errorf("%w: kind %q", ErrInvalidReference, e.Kind)
		case e.ID == "" || e.Version == "":
			return nil, fmt.Errorf("%w: %s names no exact (id, version)", ErrInvalidReference, e.key())
		case e.Digest == "":
			return nil, fmt.Errorf("%w: %s carries no digest", ErrInvalidReference, e.key())
		case !e.Status.Valid():
			return nil, fmt.Errorf("%w: %s status %q", ErrInvalidReference, e.key(), e.Status)
		case e.Kind == RefSchema && e.Descriptor == "":
			return nil, fmt.Errorf("%w: schema %s declares no descriptor", ErrInvalidReference, e.key())
		}
		if _, dup := r.entries[e.key()]; dup {
			return nil, fmt.Errorf("%w: %s is published twice", ErrInvalidReference, e.key())
		}
		r.entries[e.key()] = e
	}
	return r, nil
}

// ResolveReference implements [ReferenceResolver].
func (r *ReferenceRegistry) ResolveReference(ref Reference) (ResolvedReference, bool) {
	if r == nil {
		return ResolvedReference{}, false
	}
	e, ok := r.entries[ref]
	return e, ok
}

// RuleTableReference publishes a validated [rules.Table] as a RULE target,
// pinned by the table's own canonical digest.
func RuleTableReference(t rules.Table, status ReferenceStatus) (ResolvedReference, error) {
	digest, err := t.Digest()
	if err != nil {
		return ResolvedReference{}, fmt.Errorf("%w: %v", ErrInvalidReference, err)
	}
	return ResolvedReference{Kind: RefRule, ID: t.ID, Version: t.Version, Digest: digest, Status: status}, nil
}

// SchemaReference is the lookup a [SchemaRef] resolves as.
func SchemaReference(s SchemaRef) Reference {
	return Reference{Kind: RefSchema, ID: s.SchemaID, Version: strconv.FormatUint(uint64(s.Version), 10)}
}

// Reference site field names, used as diagnostic fields and plan keys.
const (
	refFieldInputSchema     = "input_schema"
	refFieldOutputSchema    = "output_schema"
	refFieldVariablesSchema = "variables_schema"
	refFieldSignalSchema    = "signal.expected_schema_ref"
	refFieldRule            = "decision.rule_ref"
	refFieldResolver        = "resolver_ref"
	refFieldTimeoutPolicy   = "timeout_policy"
	refFieldCompensation    = "compensation_ref"
)

// referenceSite is one place a definition names a reference.
type referenceSite struct {
	nodeID     string
	field      string
	ref        Reference
	descriptor string
	// requiresResolver marks a reference kind that did not exist before
	// WF-COMP-007. Compiling it without a resolver is refused rather than
	// silently accepted; the older kinds keep their pre-resolver behaviour.
	requiresResolver bool
}

// resolvedReferences is the output of the single reference pass.
type resolvedReferences struct {
	sites map[string]map[string]ResolvedReference
	all   []ResolvedReference
}

func (r resolvedReferences) at(nodeID, field string) *ResolvedReference {
	got, ok := r.sites[nodeID][field]
	if !ok {
		return nil
	}
	return &got
}

// referenceSites enumerates every reference the definition declares, for every
// step type, in declaration order. It is the only place that knows where
// references live, so adding a reference field means adding one line here.
func referenceSites(def *Definition) []referenceSite {
	var out []referenceSite
	schema := func(nodeID, field string, s SchemaRef) {
		if s.Valid() {
			out = append(out, referenceSite{nodeID: nodeID, field: field, ref: SchemaReference(s), descriptor: s.ProtobufFullName})
		}
	}
	versioned := func(nodeID, field string, kind ReferenceKind, v *VersionedRef) {
		if v != nil {
			out = append(out, referenceSite{
				nodeID: nodeID, field: field, requiresResolver: true,
				ref: Reference{Kind: kind, ID: v.ID, Version: v.Version},
			})
		}
	}
	schema("", refFieldInputSchema, def.InputSchema)
	schema("", refFieldOutputSchema, def.OutputSchema)
	schema("", refFieldVariablesSchema, def.VariablesSchema)
	for i := range def.Nodes {
		n := &def.Nodes[i]
		schema(n.ID, refFieldInputSchema, n.InputSchema)
		schema(n.ID, refFieldOutputSchema, n.OutputSchema)
		if n.Signal != nil {
			schema(n.ID, refFieldSignalSchema, n.Signal.ExpectedSchemaRef)
		}
		if n.Decision != nil && (n.Decision.RuleRef != "" || n.Decision.RuleVersion != "") {
			out = append(out, referenceSite{
				nodeID: n.ID, field: refFieldRule,
				ref:              Reference{Kind: RefRule, ID: n.Decision.RuleRef, Version: n.Decision.RuleVersion},
				requiresResolver: n.Decision.RuleVersion != "",
			})
		}
		versioned(n.ID, refFieldResolver, RefResolver, n.ResolverRef)
		versioned(n.ID, refFieldTimeoutPolicy, RefTimeoutPolicy, n.TimeoutPolicy)
		versioned(n.ID, refFieldCompensation, RefCompensation, n.CompensationRef)
	}
	return out
}

// resolveReferences is the one reference-resolution pass shared by every step
// type. With no resolver it keeps the pre-WF-COMP-007 behaviour for schema
// and unversioned rule references and refuses every newer reference kind with
// [CodeReferenceResolverRequired]. With a resolver, every reference must name
// an exact published, non-retired target, and the resolved target is carried
// into the plan.
func resolveReferences(def *Definition, opts Options, c *collector) resolvedReferences {
	out := resolvedReferences{sites: map[string]map[string]ResolvedReference{}}
	seen := map[Reference]bool{}
	for _, s := range referenceSites(def) {
		loc := Location{NodeID: s.nodeID, Field: s.field, Ref: s.ref.String()}
		if s.nodeID == "" {
			loc.Ref = def.WorkflowID + " " + s.ref.String()
		}
		if opts.References == nil {
			if s.requiresResolver {
				c.add(CodeReferenceResolverRequired, loc,
					"%s declares a %s reference but no reference resolver was supplied to the compiler", s.field, s.ref.Kind)
			}
			continue
		}
		if s.ref.ID == "" || s.ref.Version == "" {
			c.add(CodeUnresolvedRef, loc, "%s names no exact (id, version)", s.field)
			continue
		}
		got, ok := opts.References.ResolveReference(s.ref)
		if !ok {
			c.add(CodeUnresolvedRef, loc, "%s target is not published", s.field)
			continue
		}
		if got.key() != s.ref || got.Digest == "" || !got.Status.Valid() {
			c.add(CodeUnresolvedRef, loc, "resolver answered %s with a different or undigested target %s", s.field, got.key())
			continue
		}
		if got.Status == ReferenceRetired {
			c.add(CodeRetiredReference, loc, "%s target is retired and cannot be bound by a new plan", s.field)
			continue
		}
		if s.ref.Kind == RefSchema && got.Descriptor != s.descriptor {
			c.add(CodeTypeMismatch, loc, "%s declares descriptor %q but the published schema is %q",
				s.field, s.descriptor, got.Descriptor)
			continue
		}
		if out.sites[s.nodeID] == nil {
			out.sites[s.nodeID] = map[string]ResolvedReference{}
		}
		out.sites[s.nodeID][s.field] = got
		if !seen[got.key()] {
			seen[got.key()] = true
			out.all = append(out.all, got)
		}
	}
	sort.Slice(out.all, func(i, j int) bool {
		a, b := out.all[i], out.all[j]
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		if a.ID != b.ID {
			return a.ID < b.ID
		}
		return a.Version < b.Version
	})
	return out
}
