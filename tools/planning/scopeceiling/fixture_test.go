package scopeceiling

// validFixture returns a minimal, structurally complete
// ScopeCeilingManifest: exactly one item per category, all four required
// selection slots present and empty, and no signature. It is unsigned on
// purpose - property_test.go and mutation_test.go exercise Validate, not
// signature verification, and TestPhaseOneManifestContainsOnlyAuthorizedSourceBoundExecutableScope
// and TestTodo_PHASE_001_Security already exercise the real signed
// checked-in manifest.
func validFixture() ScopeCeilingManifest {
	return ScopeCeilingManifest{
		SchemaVersion:       1,
		TodoID:              "PHASE-001",
		SignedDate:          "2026-09-13",
		FreshnessWindowDays: 30,
		Intents: []IntentItem{
			{
				ID: "hcmnext.people.explain_worker_state/v1", OwnerDomain: "PEOPLE",
				KernelFamily: "ANALYTICAL_REQUEST", SideEffect: "READ_ONLY",
				Gate: GateP1A, Disposition: Include, Rationale: "fixture intent",
			},
		},
		Capabilities: []CapabilityItem{
			{
				ID: "hcmnext.people.explain_worker_state", OwnerDomain: "people",
				EffectClass: ReadOnly, Gate: GateP1A, Disposition: Include, Rationale: "fixture capability",
			},
		},
		Workflows: []WorkflowItem{
			{
				ID: "promotion.preflight-simulate-observe/v1", VerticalSlice: "promotion",
				Gate: GateP1A, Disposition: Include, Rationale: "fixture workflow",
			},
		},
		UserFlows: []UserFlowItem{
			{ID: "UF-007", Archetype: "UF-A2/A4/A6", Gate: GateP1A, Disposition: Include, Rationale: "fixture user flow"},
		},
		Endpoints: []EndpointItem{
			{
				EndpointID: "hcmnext.intents.v1.IntentService/GetIntent", ServiceFullName: "hcmnext.intents.v1.IntentService",
				MethodName: "GetIntent", Generated: true, Gate: GateP1A,
				EndpointDisposition: GenericIntentLifecycleOnly, Disposition: Include, Rationale: "fixture endpoint",
			},
		},
		Models: []ModelItem{
			{ID: "people-employment-assignment-domain", SpecRef: "specs/people-employment-assignment-domain.md", Gate: GateP1A, Disposition: Include, Rationale: "fixture model"},
		},
		Effects: []EffectItem{
			{Class: ReadOnly, Gate: GateP1A, Disposition: Include, Rationale: "fixture effect"},
		},
		SelectionSlots: []SelectionSlot{
			{Name: "provider", FillingTodoID: "SELECT-002", Filled: false},
			{Name: "jurisdiction", FillingTodoID: "SELECT-001", Filled: false},
			{Name: "topology", FillingTodoID: "SELECT-002", Filled: false},
			{Name: "slo", FillingTodoID: "SELECT-002", Filled: false},
		},
		DeferredDomains: []DeferredDomain{
			{Name: "remaining_catalog_vocabulary", SourceRef: "specs/business-intent-catalog.md", Rationale: "fixture deferred domain"},
		},
		Signature: &Signature{
			Algorithm: "ed25519", PublicKey: "fixture-public-key", Value: "fixture-signature-value",
			KeyFixture: "tools/planning/gateevidence/testdata/dev-signing-key.yaml",
		},
	}
}

// deepCopy returns an independent copy of m so a mutation in one subtest
// cannot leak into another (slices in Go share backing arrays on a plain
// struct copy).
func deepCopy(m ScopeCeilingManifest) ScopeCeilingManifest {
	c := m
	c.Intents = append([]IntentItem(nil), m.Intents...)
	c.Capabilities = append([]CapabilityItem(nil), m.Capabilities...)
	c.Workflows = append([]WorkflowItem(nil), m.Workflows...)
	c.UserFlows = append([]UserFlowItem(nil), m.UserFlows...)
	c.Endpoints = append([]EndpointItem(nil), m.Endpoints...)
	c.Models = append([]ModelItem(nil), m.Models...)
	c.Effects = append([]EffectItem(nil), m.Effects...)
	c.SelectionSlots = append([]SelectionSlot(nil), m.SelectionSlots...)
	c.DeferredDomains = append([]DeferredDomain(nil), m.DeferredDomains...)
	if m.Signature != nil {
		sig := *m.Signature
		c.Signature = &sig
	}
	return c
}
