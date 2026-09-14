package pilotblueprint

import "testing"

// TestTodo_CUSTOMER_001_Security is CUSTOMER-001's SECURITY test, the same
// shape as tools/planning/pilotjurisdiction's TestTodo_SELECT_001_Security
// and tools/planning/pilotprovider's TestTodo_SELECT_002_Security: it proves
// the real, checked-in, signed blueprint verifies, then proves that
// tampering any single signed field - including, specifically, quietly
// dropping an escalation/fallback or quietly reassigning an owner -
// invalidates the signature.
func TestTodo_CUSTOMER_001_Security(t *testing.T) {
	original := mustLoadBlueprint(t)

	ok, err := VerifyBlueprintSignature(original)
	if err != nil {
		t.Fatalf("VerifyBlueprintSignature(original): %v", err)
	}
	if !ok {
		t.Fatal("the checked-in customer-001-pilot-blueprint.yaml must verify against its own signature")
	}

	tamperCases := []struct {
		name   string
		tamper func(*Blueprint)
	}{
		{"a workstream is quietly reordered", func(b *Blueprint) {
			b.Workstreams[0], b.Workstreams[1] = b.Workstreams[1], b.Workstreams[0]
		}},
		{"an owner role title is quietly replaced", func(b *Blueprint) {
			b.Workstreams[0].Owners[0].RoleTitle = "Someone Else"
		}},
		{"a prerequisite's gate action is quietly softened", func(b *Blueprint) {
			for i := range b.Workstreams {
				if len(b.Workstreams[i].Prerequisites) > 0 {
					b.Workstreams[i].Prerequisites[0].Gate.Action = ActionProceed
					break
				}
			}
		}},
		{"an escalation path is quietly erased", func(b *Blueprint) { b.Workstreams[0].Escalation = "" }},
		{"a fallback is quietly erased", func(b *Blueprint) { b.Workstreams[0].Fallback = "" }},
		{"the provider topology reference is quietly repointed", func(b *Blueprint) {
			b.ProviderTopologyRef = "definitions/planning/gates/some-other-topology.yaml"
		}},
		{"the template version is quietly bumped without re-signing", func(b *Blueprint) {
			b.TemplateVersion = "2.0.0"
		}},
	}

	for _, tc := range tamperCases {
		t.Run(tc.name, func(t *testing.T) {
			tampered := deepCopy(original)
			tc.tamper(&tampered)

			ok, err := VerifyBlueprintSignature(tampered)
			if err != nil {
				t.Fatalf("VerifyBlueprintSignature(tampered): %v", err)
			}
			if ok {
				t.Fatalf("tampered blueprint (%s) must not verify against the original signature", tc.name)
			}
		})
	}
}
