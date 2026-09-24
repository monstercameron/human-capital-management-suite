package promotionexec

import (
	"context"
	"errors"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
)

// ErrManifestOnlyCapability is returned if code accidentally invokes one of
// the compiler's manifest-only bindings. Served compositions bind these
// exact definitions to their domain handlers.
var ErrManifestOnlyCapability = errors.New("promotionexec: capability manifest has no execution binding")

type promotionCapabilitySpec struct {
	id, owner string
	effect    capability.EffectClass
	scope     string
}

// capabilityRegistry creates the published capabilities used by Promotion
// compilation. The three P1A read records come directly from the bootstrap
// registry; this prevents a second definition from silently assigning those
// keys a different digest. The four additional graph reads are the seven
// Promotion read records required by WF-EXT-005.
func capabilityRegistry() (*capability.Registry, error) {
	registry, err := capability.NewBootstrapRegistry()
	if err != nil {
		return nil, err
	}
	for _, spec := range []promotionCapabilitySpec{
		{capRevalidate, "governance", capability.EffectReadOnly, "scope:governance.read"},
		{capObservePayroll, "payroll", capability.EffectReadOnly, "scope:observation.read"},
		{capObserveAccess, "access", capability.EffectReadOnly, "scope:observation.read"},
		{capObserveRecon, "reconciliation", capability.EffectReadOnly, "scope:observation.read"},
		{capExecute, "people", capability.EffectInternalMutation, "scope:people.write"},
		{capReleaseHold, "rewards", capability.EffectInternalMutation, "scope:rewards.write"},
	} {
		def := promotionCapabilityDefinition(spec)
		if spec.id == capExecute {
			// The bootstrap v1 record is read-only. Keep its published meaning
			// and publish the mutation under a new exact version.
			def.Version = 2
			def.RequestSchema.Version = 2
			def.ResponseSchema.Version = 2
			def.ErrorSchema.Version = 2
			def.RequestSchema.SchemaID = spec.id + ".request/v2"
			def.ResponseSchema.SchemaID = spec.id + ".response/v2"
			def.ErrorSchema.SchemaID = spec.id + ".error/v2"
		}
		if err := registry.Register(def, func(context.Context, any) (any, error) {
			return nil, ErrManifestOnlyCapability
		}); err != nil {
			return nil, err
		}
	}
	return registry, nil
}

// ManifestRegistry returns the authoritative capability registry used to
// compile the current Promotion graph. Variant graphs build on the same
// immutable Promotion manifests and add their own registrations before
// compilation.
func ManifestRegistry() (*capability.Registry, error) {
	return capabilityRegistry()
}

func promotionCapabilityDefinition(spec promotionCapabilitySpec) capability.Definition {
	id := spec.id
	definition := capability.Definition{
		ID: id, Version: 1, OwnerDomain: spec.owner,
		RequestSchema:  capability.SchemaRef{SchemaID: id + ".request/v1", Version: 1, ProtobufFullName: "hcmnext.capabilities.v1.CapabilityDefinition"},
		ResponseSchema: capability.SchemaRef{SchemaID: id + ".response/v1", Version: 1, ProtobufFullName: "hcmnext.capabilities.v1.CapabilityDefinition"},
		ErrorSchema:    capability.SchemaRef{SchemaID: id + ".error/v1", Version: 1, ProtobufFullName: "hcmnext.capabilities.v1.CapabilityDefinition"},
		EffectClass:    spec.effect, RiskClass: "LOW",
		IdempotencyPolicyRef: "idempotency.promotion." + spec.owner + ".v1",
		AuthZScopeRef:        spec.scope, LegalBasisRef: "legal.promotion.execution/v1",
		EntitlementRef: "entitlement.promotion.execution/v1", SLOClassRef: "slo.promotion.execution/v1",
		TestRef: "conformance:" + id + "/v1",
	}
	if spec.effect.IsWrite() {
		definition.WriteData = capability.DataDomainFieldSet{DataDomains: []string{spec.owner}}
	} else {
		definition.ReadData = capability.DataDomainFieldSet{DataDomains: []string{spec.owner}}
	}
	if spec.id == capRevalidate || spec.id == capObservePayroll || spec.id == capObserveAccess || spec.id == capObserveRecon {
		definition.RiskClass = Definition().RiskClass
	}
	return definition
}

// GovernedReadDefinitions returns the four additional graph-read records in
// the compiler's registry, with their immutable metadata intact.
func GovernedReadDefinitions() []capability.Definition {
	defs := make([]capability.Definition, 0, 4)
	for _, spec := range []promotionCapabilitySpec{
		{capRevalidate, "governance", capability.EffectReadOnly, "scope:governance.read"},
		{capObservePayroll, "payroll", capability.EffectReadOnly, "scope:observation.read"},
		{capObserveAccess, "access", capability.EffectReadOnly, "scope:observation.read"},
		{capObserveRecon, "reconciliation", capability.EffectReadOnly, "scope:observation.read"},
	} {
		defs = append(defs, promotionCapabilityDefinition(spec))
	}
	return defs
}
