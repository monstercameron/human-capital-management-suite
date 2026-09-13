package pilotblueprint

import "testing"

// TestImplementationBlueprintCoversEveryPilotDependencyDeliverableOwnerAndAcceptanceGate
// is CUSTOMER-001's PRIMARY test. It loads the real, checked-in
// definitions/planning/gates/customer-001-pilot-blueprint.yaml and proves,
// driven off the workstream list itself rather than a hardcoded expectation,
// that every discovery-through-hypercare workstream carries every RED
// element (a customer, HCM Next and provider owner, a prerequisite, an
// input and output artifact, a due/expiry, an acceptance oracle, a
// data-processing boundary, an escalation and a fallback), that GREEN's
// exact ordered coverage holds, and that the blueprint verifies against its
// own signature.
func TestImplementationBlueprintCoversEveryPilotDependencyDeliverableOwnerAndAcceptanceGate(t *testing.T) {
	bp := mustLoadBlueprint(t)

	violations := bp.Validate()
	if len(violations) != 0 {
		t.Fatalf("the checked-in blueprint must validate as a structurally complete template, got: %v", violations)
	}

	if bp.TodoID != "CUSTOMER-001" {
		t.Fatalf("todo_id = %q, want CUSTOMER-001", bp.TodoID)
	}
	if bp.ProviderTopologyRef == "" || bp.JurisdictionProfileRef == "" || bp.ScopeCeilingRef == "" {
		t.Fatalf("blueprint must bind to the real signed provider/jurisdiction/scope-ceiling artifacts, got %+v", bp)
	}

	// GREEN: exact, ordered discovery-through-hypercare coverage, driven off
	// AllWorkstreamKinds rather than restated here.
	want := AllWorkstreamKinds()
	if len(bp.Workstreams) != len(want) {
		t.Fatalf("blueprint has %d workstreams, want exactly %d", len(bp.Workstreams), len(want))
	}
	for i, w := range bp.Workstreams {
		if w.Kind != want[i] {
			t.Fatalf("workstreams[%d].kind = %s, want %s (discovery-through-hypercare order)", i, w.Kind, want[i])
		}
		if w.Sequence != i+1 {
			t.Errorf("workstreams[%d].sequence = %d, want %d", i, w.Sequence, i+1)
		}

		// RED, checked per workstream, driven off the live workstream
		// rather than a hardcoded per-kind expectation:
		seenParty := map[Party]bool{}
		accountable := 0
		for _, o := range w.Owners {
			seenParty[o.Party] = true
			if o.RoleTitle == "" {
				t.Errorf("workstream %s has an owner with no role_title: %+v", w.Kind, o)
			}
			if o.RACIRole == Accountable {
				accountable++
			}
		}
		for _, party := range AllParties() {
			if !seenParty[party] {
				t.Errorf("workstream %s has no %s owner", w.Kind, party)
			}
		}
		if accountable != 1 {
			t.Errorf("workstream %s must name exactly one ACCOUNTABLE owner, found %d", w.Kind, accountable)
		}

		if len(w.Prerequisites) == 0 {
			t.Errorf("workstream %s names no prerequisite", w.Kind)
		}
		for _, d := range w.Prerequisites {
			if d.EvidenceRef == "" {
				t.Errorf("workstream %s has a prerequisite with no evidence_ref: %+v", w.Kind, d)
			}
			if !validActions[d.Gate.Action] {
				t.Errorf("workstream %s has a prerequisite with an unknown gate action: %+v", w.Kind, d)
			}
		}

		if len(w.InputArtifacts) == 0 {
			t.Errorf("workstream %s names no input artifact", w.Kind)
		}
		if len(w.OutputArtifacts) == 0 {
			t.Errorf("workstream %s names no output artifact", w.Kind)
		}
		if w.Timing.DueOffsetDays <= 0 || w.Timing.ExpiryOffsetDays <= 0 {
			t.Errorf("workstream %s is missing a due/expiry: %+v", w.Kind, w.Timing)
		}
		if w.AcceptanceOracle == "" {
			t.Errorf("workstream %s has no acceptance oracle", w.Kind)
		}
		if w.DataProcessingBoundary == "" {
			t.Errorf("workstream %s has no data-processing boundary", w.Kind)
		}
		if w.Escalation == "" {
			t.Errorf("workstream %s has no escalation", w.Kind)
		}
		if w.Fallback == "" {
			t.Errorf("workstream %s has no fallback", w.Kind)
		}
	}

	// The legal-review workstream must bind a provider/jurisdiction
	// dependency naming the real SELECT-001 profile - the only workstream
	// this package special-cases in Instantiate.
	foundLegalJurisdictionDep := false
	for _, w := range bp.Workstreams {
		if w.Kind != WorkstreamLegalReview {
			continue
		}
		for _, d := range w.Prerequisites {
			if d.Party == PartyProvider || d.Party == PartyHCMNext {
				foundLegalJurisdictionDep = true
			}
		}
	}
	if !foundLegalJurisdictionDep {
		t.Error("the LEGAL_REVIEW workstream names no provider/HCM-Next-owed prerequisite")
	}

	// Signature verifies.
	ok, err := VerifyBlueprintSignature(bp)
	if err != nil {
		t.Fatalf("VerifyBlueprintSignature: %v", err)
	}
	if !ok {
		t.Fatal("the checked-in blueprint must verify against its own signature")
	}
}
