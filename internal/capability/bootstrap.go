package capability

import "context"

// bootstrapEcho is the placeholder handler bound to every BOOTSTRAP-profile
// capability. It performs no domain logic: the intent and transport layers
// (built separately) supply the real handler by registering their own
// Registry with real bindings. What this package proves is the registry and
// gateway plumbing - resolution, immutability, effect-class enforcement and
// evidence - not the business behavior behind each capability, which is
// covered elsewhere.
func bootstrapEcho(id string) Handler {
	return func(_ context.Context, payload any) (any, error) {
		return map[string]any{"capability": id, "echo": payload}, nil
	}
}

// bootstrapSchema fabricates a valid, distinct schema reference for a
// bootstrap capability slot. Real typed request/response/error schemas are
// published by the intent layer (INTENT-001) once it exists; BOOTSTRAP only
// needs schema identity to be present and stable.
func bootstrapSchema(id, slot string) SchemaRef {
	return SchemaRef{
		SchemaID:         id + "." + slot + "/v1",
		Version:          1,
		ProtobufFullName: "hcmnext.capabilities.v1.CapabilityDefinition",
	}
}

// bootstrapDefinition fills in the governance references every BOOTSTRAP P1A
// capability shares: zero-effect, observation-only, low risk, read-safe
// idempotency (planning/next-steps.md P1A: "must persist zero worker,
// employment, ... mutations").
func bootstrapDefinition(id, ownerDomain string, readDomains []string) Definition {
	return Definition{
		ID:                   id,
		Version:              1,
		OwnerDomain:          ownerDomain,
		RequestSchema:        bootstrapSchema(id, "request"),
		ResponseSchema:       bootstrapSchema(id, "response"),
		ErrorSchema:          bootstrapSchema(id, "error"),
		EffectClass:          EffectReadOnly,
		ReadData:             DataDomainFieldSet{DataDomains: readDomains},
		WriteData:            DataDomainFieldSet{},
		RiskClass:            "LOW",
		IdempotencyPolicyRef: "idempotency.read-safe.v1",
		AgentEligible:        false,
		AuthZScopeRef:        "scope:" + ownerDomain + ".read",
		LegalBasisRef:        "legal.p1a.observation-only.v1",
		EntitlementRef:       "entitlement.pilot.p1a.v1",
		SLOClassRef:          "slo.interactive.p95-2s.v1",
		TestRef:              "conformance:" + id + "/v1",
	}
}

// bootstrapDefinitions is the compiled-in P1A table: the eight executable
// P1A intent contracts (planning/next-steps.md "P1A - paid observation,
// preflight and simulation"), the separately callable effective-date
// debugger, and the registry's resolve/explain capabilities, so the registry
// discovers itself through the same path a UI, agent or connector uses
// (CAP-001 REFACTOR).
func bootstrapDefinitions() []Definition {
	return []Definition{
		bootstrapDefinition("hcmnext.people.explain_worker_state", "people", []string{"worker", "employment"}),
		bootstrapDefinition("hcmnext.people.promote_worker", "people", []string{"worker", "employment", "position"}),
		bootstrapDefinition("hcmnext.rewards.simulate_compensation", "rewards", []string{"compensation", "budget"}),
		bootstrapDefinition("hcmnext.rewards.evaluate_pay_band_position", "rewards", []string{"compensation"}),
		bootstrapDefinition("hcmnext.intelligence.explain_transaction", "intelligence", []string{"transaction_ledger"}),
		bootstrapDefinition("hcmnext.operations.detect_drift", "operations", []string{"projection", "ledger"}),
		bootstrapDefinition("hcmnext.operations.create_repair_plan", "operations", []string{"projection", "ledger"}),
		bootstrapDefinition("hcmnext.operations.simulate_repair", "operations", []string{"projection", "ledger"}),
		bootstrapDefinition("hcmnext.dataops.explain_field_history", "dataops", []string{"field_history"}),
		bootstrapDefinition("hcmnext.registry.resolve_capability", "registry", []string{"capability_registry"}),
		bootstrapDefinition("hcmnext.registry.explain_capability", "registry", []string{"capability_registry"}),
	}
}

// NewBootstrapRegistry returns the compiled-in BOOTSTRAP registry: the P1A
// capability table, each version Active and bound to a handler. The build is
// the publication (capability-registry-and-lifecycle.md "Bootstrap
// Profile"): there is no separate propose/validate/review/publish/activate
// step, and Register below can only fail on a programming error in this
// file, never on external input.
func NewBootstrapRegistry() (*Registry, error) {
	r := NewRegistry()
	for _, def := range bootstrapDefinitions() {
		if err := r.Register(def, bootstrapEcho(def.ID)); err != nil {
			return nil, err
		}
	}
	return r, nil
}
