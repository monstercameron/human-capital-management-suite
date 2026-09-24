package workflow

import (
	"fmt"
	"sort"
)

const declaredSchemaDigestProfile = "hcmnext.workflow.DeclaredSchema/v1"

// DeclaredSchemaResolver builds a versioned schema-reference set from the
// exact schema declarations on one immutable workflow definition. This keeps
// compiler callers that also resolve executable rule/transform payloads from
// losing the schema identities that the definition already declares.
//
// The resulting digest binds schema id, version, and protobuf descriptor.
// It is a schema identity digest; it does not claim to carry a protobuf
// descriptor set or validate message fields.
func DeclaredSchemaResolver(def Definition) (*ReferenceRegistry, error) {
	entries := make(map[Reference]ResolvedReference)
	for _, site := range referenceSites(&def) {
		if site.ref.Kind != RefSchema {
			continue
		}
		entry := ResolvedReference{
			Kind: RefSchema, ID: site.ref.ID, Version: site.ref.Version,
			Digest: "sha256:" + canonicalDigest(declaredSchemaDigestProfile, struct {
				ID         string `json:"id"`
				Version    string `json:"version"`
				Descriptor string `json:"descriptor"`
			}{site.ref.ID, site.ref.Version, site.descriptor}),
			Status: ReferencePublished, Descriptor: site.descriptor,
		}
		if prior, exists := entries[site.ref]; exists {
			if prior.Descriptor != entry.Descriptor {
				return nil, fmt.Errorf("workflow: schema identity %s is declared with conflicting protobuf descriptors", site.ref)
			}
			continue
		}
		entries[site.ref] = entry
	}
	refs := make([]ResolvedReference, 0, len(entries))
	for _, entry := range entries {
		refs = append(refs, entry)
	}
	sort.Slice(refs, func(i, j int) bool {
		if refs[i].ID != refs[j].ID {
			return refs[i].ID < refs[j].ID
		}
		return refs[i].Version < refs[j].Version
	})
	return NewReferenceRegistry(refs...)
}

// ComposeReferenceResolvers checks resolvers in order and returns the first
// exact match. Callers should put durable registries before derived, local
// schema declarations so an authoritative registry remains the source of
// truth whenever it publishes the same identity.
func ComposeReferenceResolvers(resolvers ...ReferenceResolver) ReferenceResolver {
	filtered := make([]ReferenceResolver, 0, len(resolvers))
	for _, resolver := range resolvers {
		if resolver != nil {
			filtered = append(filtered, resolver)
		}
	}
	return composedReferenceResolver{resolvers: filtered}
}

type composedReferenceResolver struct {
	resolvers []ReferenceResolver
}

func (r composedReferenceResolver) ResolveReference(ref Reference) (ResolvedReference, bool) {
	for _, resolver := range r.resolvers {
		if resolved, ok := resolver.ResolveReference(ref); ok {
			return resolved, true
		}
	}
	return ResolvedReference{}, false
}
