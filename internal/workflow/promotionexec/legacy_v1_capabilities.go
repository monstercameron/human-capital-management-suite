package promotionexec

import "github.com/monstercameron/human-capital-management-suite/internal/capability"

// frozenV1CapabilitySnapshot preserves the manifest bytes used by already
// published Promotion v1.0.0 plans. New plans resolve through
// capabilityRegistry; this compatibility snapshot is used only by the frozen
// v1 compiler and cannot execute capabilities.
type frozenV1CapabilitySnapshot struct{}

func (frozenV1CapabilitySnapshot) Lookup(key capability.Key) (capability.Record, bool) {
	var owner string
	var effect capability.EffectClass
	var scope string
	switch key.ID {
	case capSnapshotWorker:
		owner, effect, scope = "people", capability.EffectReadOnly, "scope:people.read"
	case capSimulate, capEvaluateBand:
		owner, effect, scope = "rewards", capability.EffectReadOnly, "scope:rewards.read"
	case capRevalidate:
		owner, effect, scope = "governance", capability.EffectReadOnly, "scope:governance.read"
	case capExecute:
		owner, effect, scope = "people", capability.EffectInternalMutation, "scope:people.write"
	case capObservePayroll:
		owner, effect, scope = "payroll", capability.EffectReadOnly, "scope:observation.read"
	case capObserveAccess:
		owner, effect, scope = "access", capability.EffectReadOnly, "scope:observation.read"
	case capObserveRecon:
		owner, effect, scope = "reconciliation", capability.EffectReadOnly, "scope:observation.read"
	case capReleaseHold:
		owner, effect, scope = "rewards", capability.EffectInternalMutation, "scope:rewards.write"
	default:
		return capability.Record{}, false
	}
	if key.Version != 1 {
		return capability.Record{}, false
	}
	id := key.ID
	return capability.Record{Definition: capability.Definition{
		ID: id, Version: 1, OwnerDomain: owner,
		RequestSchema:  capability.SchemaRef{SchemaID: id + ".request/v1", Version: 1, ProtobufFullName: "hcmnext.capabilities.v1.CapabilityDefinition"},
		ResponseSchema: capability.SchemaRef{SchemaID: id + ".response/v1", Version: 1, ProtobufFullName: "hcmnext.capabilities.v1.CapabilityDefinition"},
		ErrorSchema:    capability.SchemaRef{SchemaID: id + ".error/v1", Version: 1, ProtobufFullName: "hcmnext.capabilities.v1.CapabilityDefinition"},
		EffectClass:    effect, IdempotencyPolicyRef: "idempotency.promotion." + owner + ".v1", AuthZScopeRef: scope,
		LegalBasisRef: "legal.promotion.execution/v1", EntitlementRef: "entitlement.promotion.execution/v1",
		SLOClassRef: "slo.promotion.execution/v1", TestRef: "conformance:" + id + "/v1",
	}, Status: capability.StatusActive, Digest: "sha256:promotionexec-" + owner}, true
}
