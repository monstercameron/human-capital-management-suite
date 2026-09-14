package threatregister

import "testing"

// TestTodo_THREAT_001_Security is THREAT-001's SECURITY test, the same
// shape as tools/planning/pilotprovider's TestTodo_SELECT_002_Security: it
// proves the real, checked-in, signed register verifies, then proves that
// tampering any single signed field - including quietly downgrading a
// CRITICAL threat's severity, quietly filling in an owner nobody actually
// designated, or quietly relaxing a mitigation's declared coverage -
// invalidates the signature.
func TestTodo_THREAT_001_Security(t *testing.T) {
	original := mustLoadRegister(t)

	ok, err := VerifyRegisterSignature(original)
	if err != nil {
		t.Fatalf("VerifyRegisterSignature(original): %v", err)
	}
	if !ok {
		t.Fatal("the checked-in threat-001-register.yaml must verify against its own signature")
	}

	tamperCases := []struct {
		name   string
		tamper func(*Register)
	}{
		{"a CRITICAL threat's severity is quietly downgraded", func(r *Register) {
			r.Slices[0].Threats[1].Severity = SeverityMedium // THR-02, CONFUSED_DEPUTY
		}},
		{"the unaccepted residual risk quietly gains an accepting owner", func(r *Register) {
			r.Slices[0].ResidualRisks[0].AcceptedBy = "Someone Nobody Designated"
		}},
		{"the unaccepted residual risk's expiry is quietly extended", func(r *Register) {
			r.Slices[0].ResidualRisks[0].ExpiryDate = "2099-01-01"
		}},
		{"a mitigation's consuming edges are quietly narrowed", func(r *Register) {
			m := &r.Slices[0].Mitigations[0]
			if len(m.ConsumingEdges) > 1 {
				m.ConsumingEdges = m.ConsumingEdges[:1]
			}
		}},
		{"a threat's attack class is quietly reclassified to a less severe-sounding one", func(r *Register) {
			r.Slices[0].Threats[2].AttackClass = string(AttackMetadataLeakage) // was TENANT_CROSSOVER
		}},
		{"a threat's owner is quietly reassigned", func(r *Register) {
			r.Slices[0].Threats[0].Owner = "SOMEONE_ELSE"
		}},
		{"an edge is quietly removed", func(r *Register) {
			r.Slices[0].Edges = r.Slices[0].Edges[1:]
		}},
	}

	for _, tc := range tamperCases {
		t.Run(tc.name, func(t *testing.T) {
			tampered := deepCopy(original)
			tc.tamper(&tampered)

			ok, err := VerifyRegisterSignature(tampered)
			if err != nil {
				t.Fatalf("VerifyRegisterSignature(tampered): %v", err)
			}
			if ok {
				t.Fatalf("tampered register (%s) must not verify against the original signature", tc.name)
			}
		})
	}
}
