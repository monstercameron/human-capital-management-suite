package operator

import (
	"slices"
	"strings"
	"testing"
)

// TestEveryKindHasExactlyOneFamilyAndPolicy proves the authority vocabulary is
// total and closed: every kind the gateway admits resolves to one family and
// one policy, the three families this todo adds are distinct kinds with
// distinct capability strings, and a kind outside the vocabulary has neither.
func TestEveryKindHasExactlyOneFamilyAndPolicy(t *testing.T) {
	seen := map[Kind]bool{}
	byFamily := map[Family][]Kind{}
	for _, k := range Kinds() {
		if seen[k] {
			t.Fatalf("%s appears twice in Kinds()", k)
		}
		seen[k] = true
		p, ok := PolicyFor(k)
		if !ok || !strings.HasPrefix(p.IntentType, "hcmnext.operations.") {
			t.Fatalf("%s policy = %+v, %v", k, p, ok)
		}
		f := k.Family()
		if !slices.Contains(Families(), f) {
			t.Fatalf("%s resolves to family %q, which is not in Families()", k, f)
		}
		byFamily[f] = append(byFamily[f], k)
	}
	for _, k := range authorityKinds() {
		if !seen[k] {
			t.Errorf("%s is declared but Kinds() does not list it", k)
		}
	}
	if _, ok := PolicyFor("SQL_CONSOLE"); ok {
		t.Error("an undeclared kind resolved to a policy")
	}
	if got := Kind("SQL_CONSOLE").Family(); got != FamilyOperations {
		t.Errorf("an undeclared kind's family = %q, want the %s fallback", got, FamilyOperations)
	}
	if _, ok := authorityPolicyFor(KindDatabaseRepair); ok {
		t.Error("authorityPolicyFor answered for a kind it does not own")
	}

	// The repair-authority families separate repair from approval; the
	// instance and override families do not.
	for k, want := range map[Kind]bool{
		KindWorkflowRepair:           true,
		KindWorkflowMigrate:          true,
		KindDatabaseRepair:           true,
		KindProjectionRebuild:        true,
		KindWorkflowNodeIntervention: true,
		KindWorkflowOverrideDecision: false,
		KindWorkflowPause:            false,
		KindDiagnosticRead:           false,
	} {
		if got := k.SeparatesRepairFromApproval(); got != want {
			t.Errorf("%s separates repair from approval = %v, want %v", k, got, want)
		}
	}

	// Each workflow family holds at least one kind, and the instance controls
	// are not repair authority.
	for _, f := range []Family{FamilyInstances, FamilyRepair, FamilyOverride, FamilyMigrate, FamilyOperations} {
		if len(byFamily[f]) == 0 {
			t.Errorf("family %s holds no kind", f)
		}
	}
	if slices.Contains(byFamily[FamilyRepair], KindWorkflowPause) {
		t.Error("pausing an instance is classed as repair authority")
	}
}

// TestFamilyForCapabilityMapsOnlyGovernedIDs proves the capability-id mapping
// the gateway suspension reads: a published id under a governed family maps to
// it, a neighbouring id that merely shares a prefix does not, and anything
// else is ungoverned.
func TestFamilyForCapabilityMapsOnlyGovernedIDs(t *testing.T) {
	for id, want := range map[string]Family{
		"workflow.repair":                 FamilyRepair,
		"workflow.repair.retry_node":      FamilyRepair,
		"workflow.override.decision":      FamilyOverride,
		"workflow.migrate.instance":       FamilyMigrate,
		"workflow.instances.pause":        FamilyInstances,
		"workflow.instances.force_cancel": FamilyInstances,
	} {
		got, ok := FamilyForCapability(id)
		if !ok || got != want {
			t.Errorf("FamilyForCapability(%q) = %q, %v; want %q", id, got, ok, want)
		}
	}
	for _, id := range []string{
		"", "   ", "workflow", "workflow.repairs.retry", "workflow.repairing",
		"people.employee.read", "operations.general", "operations.general.failover",
	} {
		if got, ok := FamilyForCapability(id); ok {
			t.Errorf("FamilyForCapability(%q) = %q, true; want ungoverned", id, got)
		}
	}
}
