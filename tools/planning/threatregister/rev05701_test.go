package threatregister

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// TestTodo_REV_057_01 proves the checked-in signed register carries the
// exact-path EDGE-07 mitigation and names the ordinary ExecuteJourney
// lost-ack/restart integration that verifies it.
func TestTodo_REV_057_01(t *testing.T) {
	r := mustLoadRegister(t)
	if got := r.Validate(); len(got) != 0 {
		t.Fatalf("Validate() = %v, want clean signed register", got)
	}
	var th *Threat
	for i := range r.Slices[0].Threats {
		if r.Slices[0].Threats[i].ID == "THR-07" {
			th = &r.Slices[0].Threats[i]
			break
		}
	}
	if th == nil {
		t.Fatal("THR-07 missing")
	}
	if len(th.Mitigations) != 1 || th.Mitigations[0] != "MIT-ATOMIC-DOMAIN-ADVANCEMENT" {
		t.Fatalf("THR-07 mitigations = %v, want exact EDGE-07 atomic advancement control", th.Mitigations)
	}
	var mitigation *Mitigation
	for i := range r.Slices[0].Mitigations {
		if r.Slices[0].Mitigations[i].ID == "MIT-ATOMIC-DOMAIN-ADVANCEMENT" {
			mitigation = &r.Slices[0].Mitigations[i]
			break
		}
	}
	if mitigation == nil || len(mitigation.ConsumingEdges) != 1 || mitigation.ConsumingEdges[0] != "EDGE-07" {
		t.Fatalf("EDGE-07 mitigation = %+v, want MIT-ATOMIC-DOMAIN-ADVANCEMENT scoped only to EDGE-07", mitigation)
	}
	wantEvidence := map[string]string{
		"TestTodo_REV_057_01_Integration": "PRIMARY",
		"TestTodo_CONN_RT_007_Fault":      "SUPPORTING",
		"TestRestoredConnectorOperationRequiresObservationOrGovernedRepairBeforeRedrive": "SUPPORTING",
		"TestTodo_CONN_RT_009_Security": "SUPPORTING",
	}
	if len(mitigation.Evidence) != len(wantEvidence) {
		t.Fatalf("mitigation evidence = %+v, want the exact primary Journey and supporting provider evidence", mitigation.Evidence)
	}
	for _, evidence := range mitigation.Evidence {
		wantRole, ok := wantEvidence[evidence.Name]
		if !ok || evidence.Role != wantRole {
			t.Errorf("unexpected mitigation evidence %+v", evidence)
			continue
		}
		if evidence.Scope == "" {
			t.Errorf("mitigation evidence %s has no boundary scope", evidence.Name)
		}
		if evidence.Role == "PRIMARY" && !strings.Contains(evidence.Scope, "EDGE-07") {
			t.Errorf("primary evidence %s must name EDGE-07 scope: %q", evidence.Name, evidence.Scope)
		}
		if evidence.Role == "SUPPORTING" && !strings.Contains(evidence.Scope, "does not prove EDGE-07") {
			t.Errorf("supporting evidence %s must disclaim EDGE-07 coverage: %q", evidence.Name, evidence.Scope)
		}
		delete(wantEvidence, evidence.Name)
	}
	for name, role := range wantEvidence {
		t.Errorf("mitigation evidence omits %s with role %s", name, role)
	}
	wantTests := map[string]bool{
		"TestTodo_REV_057_01_Integration": false,
		// These are supplemental provider-boundary tests. They do not stand
		// in for the ordinary ExecuteJourney integration above.
		"TestTodo_CONN_RT_007_Fault": false,
		"TestRestoredConnectorOperationRequiresObservationOrGovernedRepairBeforeRedrive": false,
		"TestTodo_CONN_RT_009_Security": false,
	}
	for _, test := range th.Tests {
		if _, tracked := wantTests[test.Name]; tracked {
			wantTests[test.Name] = true
		}
	}
	for name, found := range wantTests {
		if !found {
			t.Errorf("THR-07 evidence omits %s", name)
		}
	}
	if blocked, blockers := r.ReleaseDecision(time.Date(2026, 9, 24, 0, 0, 0, 0, time.UTC)); blocked {
		t.Fatalf("verified THR-07 mitigation must not block release: %s", fmt.Sprint(blockers))
	}
	if ok, err := VerifyRegisterSignature(r); err != nil || !ok {
		t.Fatalf("checked-in register signature: valid=%v err=%v", ok, err)
	}
}

func TestTodo_REV_057_01_ProviderEvidenceDoesNotSubstituteForDomainMitigation(t *testing.T) {
	r := mustLoadRegister(t)
	for i := range r.Slices[0].Threats {
		if r.Slices[0].Threats[i].ID != "THR-07" {
			continue
		}
		th := &r.Slices[0].Threats[i]
		th.Mitigations = nil
		for j := range th.Tests {
			if th.Tests[j].Name == "TestTodo_REV_057_01_Integration" {
				th.Tests = append(th.Tests[:j], th.Tests[j+1:]...)
				break
			}
		}
		if blocked, blockers := r.ReleaseDecision(time.Date(2026, 9, 24, 0, 0, 0, 0, time.UTC)); !blocked || !strings.Contains(fmt.Sprint(blockers), "THR-07") {
			t.Fatalf("provider-only evidence without the exact EDGE-07 control must block: blocked=%v blockers=%v", blocked, blockers)
		}
		return
	}
	t.Fatal("THR-07 missing")
}
