package intent

import (
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/intent/lifecycle"
)

// RegistryProfile names how the registry is published. Under BOOTSTRAP the
// registry is a compiled-in Go table: the build is the publication, and the
// binary's digest is the snapshot fingerprint. MANAGED registries can accept
// successor definitions through [Registry.PublishManaged], which runs a
// compatibility gate before returning a new immutable registry. Signed
// control-bundle activation remains outside this package.
type RegistryProfile string

// Registry profiles.
const (
	ProfileBootstrap RegistryProfile = "BOOTSTRAP"
	ProfileManaged   RegistryProfile = "MANAGED"
)

// Catalog is the set of external references a definition may name. It exists so
// that an unknown schema or capability is a compilation failure rather than a
// runtime surprise: under the BOOTSTRAP profile these lists are compiled in
// beside the definitions themselves.
type Catalog struct {
	Schemas      []SchemaRef
	Capabilities []string
}

// Registry publishes intent definitions. It is immutable once constructed:
// BOOTSTRAP additions are source changes, while MANAGED additions go through
// [Registry.PublishManaged] and return a new registry value.
type Registry struct {
	profile     RegistryProfile
	byRef       map[Ref]Definition
	byName      map[string][]Ref
	policies    map[string]NegativeStatePolicy
	order       []Ref
	knownSchema map[string]bool
	knownCap    map[string]bool
	// catalog is the publication context definitions were compiled
	// against. PublishManaged reuses it so a managed publish enforces
	// exactly the same schema, capability, policy and lifecycle rules as
	// the original compilation.
	catalog Catalog
}

// NewRegistry compiles definitions and policies into an immutable registry.
//
// It rejects, in this order: an invalid definition, material reuse of an
// (intent_type_id, version) pair, an unknown input or result schema, an unknown
// required capability, an unresolved negative-state policy reference, an
// applicable negative state the policy does not decide, and a definition whose
// declared lifecycle transitions widen the shared kernel profiles.
func NewRegistry(profile RegistryProfile, defs []Definition, policies []NegativeStatePolicy, catalog Catalog) (*Registry, error) {
	if profile != ProfileBootstrap && profile != ProfileManaged {
		return nil, newError("NewRegistry", "profile", ErrInvalidDefinition,
			"%q is not a registry profile", string(profile))
	}
	r := &Registry{
		profile:     profile,
		byRef:       make(map[Ref]Definition, len(defs)),
		byName:      map[string][]Ref{},
		policies:    make(map[string]NegativeStatePolicy, len(policies)),
		knownSchema: map[string]bool{},
		knownCap:    map[string]bool{},
		catalog:     catalog,
	}
	for _, s := range catalog.Schemas {
		if err := s.Validate(); err != nil {
			return nil, err
		}
		r.knownSchema[s.String()] = true
	}
	for _, c := range catalog.Capabilities {
		r.knownCap[c] = true
	}
	for _, p := range policies {
		if err := p.Validate(); err != nil {
			return nil, err
		}
		if _, dup := r.policies[p.Ref()]; dup {
			return nil, newError("NewRegistry", "negative_state_policy", ErrInvalidNegativeStatePolicy,
				"policy %s is registered twice", p.Ref())
		}
		r.policies[p.Ref()] = p
	}

	kernelProfiles := lifecycle.KernelProfiles()
	for _, d := range defs {
		if err := d.Validate(); err != nil {
			return nil, err
		}
		if _, dup := r.byRef[d.Ref]; dup {
			return nil, newError("NewRegistry", "definition_ref", ErrDuplicateDefinition,
				"%s is registered twice; a published version is immutable", d.Ref)
		}
		if !r.knownSchema[d.InputSchema.String()] {
			return nil, newError("NewRegistry", "input_schema_ref", ErrInvalidDefinition,
				"%s names unknown schema %s", d.Ref, d.InputSchema)
		}
		if !r.knownSchema[d.ResultSchema.String()] {
			return nil, newError("NewRegistry", "result_schema_ref", ErrInvalidDefinition,
				"%s names unknown schema %s", d.Ref, d.ResultSchema)
		}
		for _, c := range d.RequiredCapabilities {
			if !r.knownCap[c] {
				return nil, newError("NewRegistry", "required_capability_refs", ErrInvalidDefinition,
					"%s names unknown capability %q", d.Ref, c)
			}
		}
		if err := r.checkNegativeStates(d); err != nil {
			return nil, err
		}
		for dim, rules := range d.AllowedTransitions {
			base, ok := kernelProfiles[dim]
			if !ok {
				return nil, newError("NewRegistry", "allowed_transitions", ErrInvalidDefinition,
					"%s narrows unknown dimension %s", d.Ref, dim)
			}
			narrowed := lifecycle.Profile{
				ID:          base.ID + "#" + d.Ref.String(),
				Dimension:   dim,
				States:      base.States,
				Initial:     base.Initial,
				Terminal:    base.Terminal,
				Transitions: rules,
			}
			if err := narrowed.Narrows(base); err != nil {
				return nil, err
			}
		}
		r.byRef[d.Ref] = d
		r.order = append(r.order, d.Ref)
		r.byName[normalizeName(d.DisplayName)] = append(r.byName[normalizeName(d.DisplayName)], d.Ref)
	}
	sort.Slice(r.order, func(i, j int) bool {
		if r.order[i].TypeID != r.order[j].TypeID {
			return r.order[i].TypeID < r.order[j].TypeID
		}
		return r.order[i].Version < r.order[j].Version
	})
	return r, nil
}

func (r *Registry) checkNegativeStates(d Definition) error {
	if len(d.ApplicableNegativeStates) == 0 {
		return nil
	}
	policy, ok := r.policies[d.NegativeStatePolicyRef]
	if !ok {
		return newError("NewRegistry", "negative_state_policy_ref", ErrMissingNegativeStatePolicy,
			"%s references unknown policy %q", d.Ref, d.NegativeStatePolicyRef)
	}
	for _, state := range d.ApplicableNegativeStates {
		if !state.Valid() {
			return newError("NewRegistry", "applicable_negative_states", ErrInvalidNegativeStatePolicy,
				"%s declares an unspecified applicable negative state", d.Ref)
		}
		if _, err := policy.Decide(state); err != nil {
			return newError("NewRegistry", "applicable_negative_states", ErrMissingNegativeStatePolicy,
				"%s declares %s applicable but %s does not decide it",
				d.Ref, state, policy.Ref())
		}
	}
	return nil
}

// Profile returns the registry publication profile.
func (r *Registry) Profile() RegistryProfile { return r.profile }

// Len returns the number of published definitions. It is the only denominator
// any coverage claim may use.
func (r *Registry) Len() int { return len(r.byRef) }

// Refs returns every published definition reference, sorted.
func (r *Registry) Refs() []Ref { return append([]Ref(nil), r.order...) }

// Definitions returns every published definition, sorted by reference. Each
// one is a deep copy (see [Definition.clone]): a caller may write to the
// returned definitions and their slices without reaching the registry, and two
// callers may do so concurrently without racing each other.
func (r *Registry) Definitions() []Definition {
	out := make([]Definition, 0, len(r.order))
	for _, ref := range r.order {
		out = append(out, r.byRef[ref].clone())
	}
	return out
}

// Resolve returns the definition published at an exact reference.
//
// Resolution is by (intent_type_id, version) only. A free-form display name, an
// unqualified id and a wrong version all fail here rather than falling back to
// a "closest" definition.
func (r *Registry) Resolve(ref Ref) (Definition, error) {
	if err := ref.Validate(); err != nil {
		return Definition{}, err
	}
	d, ok := r.byRef[ref]
	if !ok {
		return Definition{}, newError("Resolve", "definition_ref", ErrUnknownDefinition,
			"%s is not published", ref)
	}
	if !d.Maturity.InCatalog() {
		return Definition{}, newError("Resolve", "maturity", ErrNotInvocable,
			"%s is at %s, below DRAFT_CONTRACT", ref, d.Maturity)
	}
	return d.clone(), nil
}

// ResolveText parses a canonical "intent_type_id/vN" reference and resolves it.
func (r *Registry) ResolveText(s string) (Definition, error) {
	ref, err := ParseRef(s)
	if err != nil {
		return Definition{}, err
	}
	return r.Resolve(ref)
}

// ResolveForInstantiation resolves a reference and additionally rejects a
// definition that may not be instantiated. A RETIRED definition still resolves
// through [Registry.Resolve] so that historical instances remain readable; it
// can never be invoked.
func (r *Registry) ResolveForInstantiation(ref Ref) (Definition, error) {
	d, err := r.Resolve(ref)
	if err != nil {
		return Definition{}, err
	}
	if !d.Maturity.Invocable() {
		return Definition{}, newError("ResolveForInstantiation", "maturity", ErrNotInvocable,
			"%s is %s; invocation is prohibited and historical resolution is preserved",
			ref, d.Maturity)
	}
	return d, nil
}

// LookupDisplayName resolves a display label to the references that currently
// carry it. It is presentation only: the runtime always uses the reference, so
// renaming a label never changes what resolves.
func (r *Registry) LookupDisplayName(name string) []Ref {
	refs := r.byName[normalizeName(name)]
	return append([]Ref(nil), refs...)
}

// Policy returns a published negative-state policy by "id/vN".
func (r *Registry) Policy(ref string) (NegativeStatePolicy, error) {
	p, ok := r.policies[ref]
	if !ok {
		return NegativeStatePolicy{}, newError("Policy", "negative_state_policy_ref",
			ErrMissingNegativeStatePolicy, "%q is not published", ref)
	}
	return p, nil
}

// PolicyFor returns the negative-state policy a definition references.
func (r *Registry) PolicyFor(ref Ref) (NegativeStatePolicy, error) {
	d, err := r.Resolve(ref)
	if err != nil {
		return NegativeStatePolicy{}, err
	}
	return r.Policy(d.NegativeStatePolicyRef)
}

// ByRelease returns the definitions scheduled for one release, sorted.
func (r *Registry) ByRelease(rel Release) []Definition {
	var out []Definition
	for _, ref := range r.order {
		if d := r.byRef[ref]; d.Release == rel {
			out = append(out, d.clone())
		}
	}
	return out
}

// ByFamily returns the definitions of one kernel family, sorted.
func (r *Registry) ByFamily(f Family) []Definition {
	var out []Definition
	for _, ref := range r.order {
		if d := r.byRef[ref]; d.Family == f {
			out = append(out, d.clone())
		}
	}
	return out
}

func normalizeName(s string) string { return strings.ToLower(strings.TrimSpace(s)) }
