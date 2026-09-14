package pilotblueprint

import "testing"

// repoRoot is the path from this package's directory to the repository
// root: tools/planning/pilotblueprint is three levels below root, exactly
// like tools/planning/pilotjurisdiction, tools/planning/pilotprovider and
// tools/planning/scopeceiling.
const repoRoot = "../../.."

const blueprintPath = repoRoot + "/definitions/planning/gates/customer-001-pilot-blueprint.yaml"
const providerTopologyPath = repoRoot + "/definitions/planning/gates/select-002-provider-topology.yaml"
const jurisdictionProfilePath = repoRoot + "/definitions/planning/gates/select-001-jurisdiction-profile.yaml"

// mustLoadBlueprint loads the real, checked-in CUSTOMER-001 blueprint. It
// does not assert Validate() is clean here: blueprint_test.go's PRIMARY test
// asserts that explicitly.
func mustLoadBlueprint(t *testing.T) Blueprint {
	t.Helper()
	b, err := LoadBlueprint(blueprintPath)
	if err != nil {
		t.Fatalf("LoadBlueprint: %v", err)
	}
	return *b
}

// validOwners returns one complete owners list: exactly one owner per
// party, with the accountable role on HCM_NEXT.
func validOwners() []OwnerRole {
	return []OwnerRole{
		{Party: PartyCustomer, RACIRole: Consulted, RoleTitle: "fixture customer owner"},
		{Party: PartyHCMNext, RACIRole: Accountable, RoleTitle: "fixture HCM Next owner"},
		{Party: PartyProvider, RACIRole: Informed, RoleTitle: "fixture provider owner"},
	}
}

// validWorkstream returns a minimal, structurally complete Workstream of
// the given kind and sequence: every RED element Validate checks for is
// present. withProviderDependency adds a PROVIDER-party prerequisite so
// tests can exercise Instantiate's provider-gated branch on a subset of
// workstreams, exactly as the checked-in blueprint does.
func validWorkstream(kind WorkstreamKind, sequence int, withProviderDependency bool) Workstream {
	prereqs := []Dependency{
		{
			Description: "fixture customer prerequisite for " + string(kind),
			Party:       PartyCustomer,
			EvidenceRef: "fixture_customer_evidence_ref",
			Gate:        StopGoGate{Metric: "fixture_metric", Threshold: "fixture_threshold", Action: ActionProceed},
		},
	}
	if withProviderDependency {
		prereqs = append(prereqs, Dependency{
			Description: "fixture provider prerequisite for " + string(kind),
			Party:       PartyProvider,
			EvidenceRef: "fixture_provider_evidence_ref",
			Gate:        StopGoGate{Metric: "fixture_metric", Threshold: "fixture_threshold", Action: ActionStop},
		})
	}
	return Workstream{
		Kind:     kind,
		Sequence: sequence,
		Owners:   validOwners(),

		Prerequisites: prereqs,

		InputArtifacts:  []Artifact{{Name: "fixture input artifact", Description: "fixture description", Ref: "fixture_ref"}},
		OutputArtifacts: []Artifact{{Name: "fixture output artifact", Description: "fixture description", Ref: "fixture_ref"}},

		Timing: Timing{DueOffsetDays: 5, ExpiryOffsetDays: 30},

		AcceptanceOracle:       "fixture acceptance oracle for " + string(kind),
		DataProcessingBoundary: "fixture data-processing boundary for " + string(kind),
		Escalation:             "fixture escalation for " + string(kind),
		Fallback:               "fixture fallback for " + string(kind),
	}
}

// providerDependentKinds names the workstreams validFixture() and the
// checked-in blueprint both give a PROVIDER-party prerequisite: the stages
// that genuinely need the incumbent system before they can proceed.
var providerDependentKinds = map[WorkstreamKind]bool{
	WorkstreamDiscovery:   true,
	WorkstreamData:        true,
	WorkstreamIntegration: true,
	WorkstreamLegalReview: true,
	WorkstreamCutover:     true,
	WorkstreamHypercare:   true,
}

// validFixture returns a minimal, structurally complete Blueprint: every RED
// element is present on every workstream, discovery through hypercare, and
// Validate() reports zero violations. property_test.go and mutation_test.go
// exercise Validate itself against this fixture rather than the one real
// checked-in file; blueprint_test.go and instantiate_test.go exercise the
// real signed file directly.
func validFixture() Blueprint {
	b := Blueprint{
		SchemaVersion:          1,
		TodoID:                 "CUSTOMER-001",
		SignedDate:             "2026-09-13",
		TemplateVersion:        "1.0.0",
		ProviderTopologyRef:    "definitions/planning/gates/select-002-provider-topology.yaml",
		JurisdictionProfileRef: "definitions/planning/gates/select-001-jurisdiction-profile.yaml",
		ScopeCeilingRef:        "definitions/planning/gates/phase1-scope-ceiling.yaml",
		Signature: &Signature{
			Algorithm: "ed25519", PublicKey: "fixture-public-key", Value: "fixture-signature-value",
			KeyFixture: "tools/planning/gateevidence/testdata/dev-signing-key.yaml",
		},
	}
	for i, kind := range AllWorkstreamKinds() {
		b.Workstreams = append(b.Workstreams, validWorkstream(kind, i+1, providerDependentKinds[kind]))
	}
	return b
}

// deepCopy returns an independent copy of b so a mutation in one subtest
// cannot leak into another (slices in Go share backing arrays on a plain
// struct copy).
func deepCopy(b Blueprint) Blueprint {
	c := b
	c.Workstreams = append([]Workstream(nil), b.Workstreams...)
	for i, w := range c.Workstreams {
		w.Owners = append([]OwnerRole(nil), w.Owners...)
		w.Prerequisites = append([]Dependency(nil), w.Prerequisites...)
		w.InputArtifacts = append([]Artifact(nil), w.InputArtifacts...)
		w.OutputArtifacts = append([]Artifact(nil), w.OutputArtifacts...)
		c.Workstreams[i] = w
	}
	if b.Signature != nil {
		sig := *b.Signature
		c.Signature = &sig
	}
	return c
}
