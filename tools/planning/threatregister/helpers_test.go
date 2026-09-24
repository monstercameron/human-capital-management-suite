package threatregister

import "testing"

// repoRoot is the path from this package's directory to the repository
// root: tools/planning/threatregister is three levels below root, exactly
// like tools/planning/pilotprovider and tools/planning/pilotjurisdiction.
const repoRoot = "../../.."

const registerPath = repoRoot + "/definitions/planning/gates/threat-001-register.yaml"

// mustLoadRegister loads the real, checked-in THREAT-001 register.
func mustLoadRegister(t *testing.T) Register {
	t.Helper()
	r, err := LoadRegister(registerPath)
	if err != nil {
		t.Fatalf("LoadRegister: %v", err)
	}
	return *r
}

// validFixture returns a minimal, structurally complete Register: every RED
// element is present, every attack class and test kind is covered exactly
// once, every slice graph edge is mapped, the one shared mitigation retains
// both consuming edges, and Validate() reports zero violations.
// property_test.go and mutation_test.go exercise Validate itself against
// this fixture rather than the one real checked-in file (whose one
// documented gap is intentional, not a defect to be mutated away from).
func validFixture() Register {
	edges := []SliceEdge{
		{ID: "E1", Actor: "A1", EntryPoint: "EP1", TrustBoundary: "TB1", Asset: "AS1"},
		{ID: "E2", Actor: "A1", EntryPoint: "EP1", TrustBoundary: "TB1", Asset: "AS1"},
		{ID: "E3", Actor: "A1", EntryPoint: "EP1", TrustBoundary: "TB1", Asset: "AS1"},
		{ID: "E4", Actor: "A1", EntryPoint: "EP1", TrustBoundary: "TB1", Asset: "AS1"},
		{ID: "E5", Actor: "A1", EntryPoint: "EP1", TrustBoundary: "TB1", Asset: "AS1"},
		{ID: "E6", Actor: "A1", EntryPoint: "EP1", TrustBoundary: "TB1", Asset: "AS1"},
		{ID: "E7", Actor: "A1", EntryPoint: "EP1", TrustBoundary: "TB1", Asset: "AS1"},
		{ID: "E8", Actor: "A1", EntryPoint: "EP1", TrustBoundary: "TB1", Asset: "AS1"},
		{ID: "E9", Actor: "A1", EntryPoint: "EP1", TrustBoundary: "TB1", Asset: "AS1"},
	}

	threats := make([]Threat, 0, len(AllAttackClasses()))
	for i, class := range AllAttackClasses() {
		threats = append(threats, Threat{
			ID:             fixtureThreatID(i),
			Asset:          "AS1",
			TrustBoundary:  "TB1",
			AttackClass:    string(class),
			Scenario:       "fixture scenario for " + string(class),
			Severity:       SeverityHigh,
			ConsumingEdges: []string{edges[i].ID},
			Detection:      "fixture detection",
			Recovery:       "fixture recovery",
			Owner:          "fixture-owner",
			Mitigations:    []string{"MIT-SHARED"},
			Tests: []ThreatTest{
				{Kind: TestKindNegative, Name: "fixture negative test " + string(class)},
				{Kind: TestKindSecurity, Name: "fixture security test " + string(class)},
				{Kind: TestKindFault, Name: "fixture fault test " + string(class)},
			},
		})
	}

	// MIT-SHARED is named by every threat above, so its consuming_edges must
	// retain the union of all nine edges.
	sharedEdges := make([]string, len(edges))
	for i, e := range edges {
		sharedEdges[i] = e.ID
	}

	return Register{
		SchemaVersion: 1,
		TodoID:        "THREAT-001",
		SignedDate:    "2026-09-13",
		Slices: []Slice{
			{
				SliceID: "fixture-slice",
				Actors:  []Actor{{ID: "A1", Kind: "manager", Description: "fixture actor"}},
				Assets:  []Asset{{ID: "AS1", DataClass: DataSensitive, Description: "fixture asset"}},
				TrustBoundaries: []TrustBoundary{
					{ID: "TB1", From: "fixture-from", To: "fixture-to", Description: "fixture boundary"},
				},
				EntryPoints: []EntryPoint{{ID: "EP1", Channel: "fixture-channel", Description: "fixture entry point"}},
				Edges:       edges,
				Threats:     threats,
				Mitigations: []Mitigation{
					{ID: "MIT-SHARED", Description: "fixture shared mitigation", ConsumingEdges: sharedEdges, Evidence: []MitigationEvidence{{Role: "PRIMARY", Name: "fixture evidence", Scope: "fixture edge"}}},
				},
				ResidualRisks: nil,
			},
		},
		Signature: &Signature{
			Algorithm: "ed25519", PublicKey: "fixture-public-key", Value: "fixture-signature-value",
			KeyFixture: "tools/planning/gateevidence/testdata/dev-signing-key.yaml",
		},
	}
}

func fixtureThreatID(i int) string {
	const letters = "0123456789"
	return "THR-FIXTURE-" + string(letters[i%10])
}

// deepCopy returns an independent copy of r so a mutation in one subtest
// cannot leak into another (slices in Go share backing arrays on a plain
// struct copy).
func deepCopy(r Register) Register {
	c := r
	c.Slices = append([]Slice(nil), r.Slices...)
	for i, s := range c.Slices {
		s.Actors = append([]Actor(nil), s.Actors...)
		s.Assets = append([]Asset(nil), s.Assets...)
		s.TrustBoundaries = append([]TrustBoundary(nil), s.TrustBoundaries...)
		s.EntryPoints = append([]EntryPoint(nil), s.EntryPoints...)
		s.Edges = append([]SliceEdge(nil), s.Edges...)
		s.Threats = make([]Threat, len(r.Slices[i].Threats))
		for j, th := range r.Slices[i].Threats {
			th.ConsumingEdges = append([]string(nil), th.ConsumingEdges...)
			th.Mitigations = append([]string(nil), th.Mitigations...)
			th.Tests = append([]ThreatTest(nil), th.Tests...)
			s.Threats[j] = th
		}
		s.Mitigations = make([]Mitigation, len(r.Slices[i].Mitigations))
		for j, m := range r.Slices[i].Mitigations {
			m.ConsumingEdges = append([]string(nil), m.ConsumingEdges...)
			m.Evidence = append([]MitigationEvidence(nil), m.Evidence...)
			s.Mitigations[j] = m
		}
		s.ResidualRisks = append([]ResidualRisk(nil), s.ResidualRisks...)
		c.Slices[i] = s
	}
	if r.Signature != nil {
		sig := *r.Signature
		c.Signature = &sig
	}
	return c
}
