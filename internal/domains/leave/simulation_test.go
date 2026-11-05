package leave

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func simulationInputs(t *testing.T) (LeaveInputSnapshot, EligibilityResolution, LeaveEntitlementPlan) {
	t.Helper()
	snapshot, err := BuildSnapshot(snapshotEntries(t))
	if err != nil {
		panic(err)
	}
	resolution, err := ResolveEligibility(eligibilityQueries(), eligibilityFacts())
	if err != nil {
		panic(err)
	}
	plan, err := ComposePlan(planRequest())
	if err != nil {
		panic(err)
	}
	return snapshot, resolution, plan
}

func TestTodo_LEAVE_006(t *testing.T) {
	snapshot, resolution, plan := simulationInputs(t)
	simulation, err := Simulate(snapshot, resolution, plan)
	if err != nil {
		t.Fatalf("Simulate: %v", err)
	}
	if !simulation.NotExecuted || simulation.Digest == "" {
		t.Fatalf("simulation=%+v", simulation)
	}
	if err := simulation.Verify(); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	// The simulation binds every material component.
	if simulation.SnapshotDigest != snapshot.Digest || simulation.ResolutionDigest != resolution.Digest || simulation.PlanDigest != plan.Digest {
		t.Fatal("simulation dropped a component binding")
	}
	// Successor revision freezes everything under one digest with a masked view.
	proposal, err := FreezeProposal(simulation, plan, 1)
	if err != nil {
		t.Fatalf("FreezeProposal: %v", err)
	}
	if err := proposal.Verify(); err != nil {
		t.Fatalf("proposal Verify: %v", err)
	}
	if len(proposal.View.SegmentKinds) != 5 || proposal.View.ScheduledHours != 40 || proposal.View.PlannedDebits != 32 {
		t.Fatalf("view=%+v", proposal.View)
	}
	for _, ref := range proposal.View.MaskedRefs {
		if !strings.HasSuffix(ref, ":***") {
			t.Fatalf("unmasked ref in ordinary artifact: %q", ref)
		}
	}
	// RED: unsealed components and hollow revisions never freeze.
	broken := simulation
	broken.Digest = "sha256:forged"
	if _, err := FreezeProposal(broken, plan, 1); err == nil {
		t.Fatal("forged simulation froze")
	}
	if _, err := FreezeProposal(simulation, plan, 0); err == nil {
		t.Fatal("zero revision froze")
	}
	if _, err := Simulate(LeaveInputSnapshot{}, resolution, plan); err == nil {
		t.Fatal("unsealed snapshot simulated")
	}
}

func TestTodo_LEAVE_006_Property(t *testing.T) {
	snapshot, resolution, plan := simulationInputs(t)
	first, err := Simulate(snapshot, resolution, plan)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Simulate(snapshot, resolution, plan)
	if err != nil || first.Digest != second.Digest {
		t.Fatal("simulation is not deterministic")
	}
	// NOT_EXECUTED is invariant: no simulation ever reports execution.
	if !first.NotExecuted || !second.NotExecuted {
		t.Fatal("simulation reports execution")
	}
	// Successor revisions chain by number with distinct seals.
	next, err := FreezeProposal(first, plan, 2)
	if err != nil {
		t.Fatal(err)
	}
	current, err := FreezeProposal(first, plan, 1)
	if err != nil {
		t.Fatal(err)
	}
	if next.Digest == current.Digest {
		t.Fatal("successor revision reused the seal")
	}
}

func TestTodo_LEAVE_006_Golden(t *testing.T) {
	snapshot, resolution, plan := simulationInputs(t)
	simulation, err := Simulate(snapshot, resolution, plan)
	if err != nil {
		t.Fatal(err)
	}
	proposal, err := FreezeProposal(simulation, plan, 1)
	if err != nil {
		t.Fatal(err)
	}
	lines := []string{
		"simulation=" + simulation.Digest,
		"executed=false",
		"proposal=" + proposal.Digest,
		"masked=" + strings.Join(proposal.View.MaskedRefs, ","),
	}
	got := strings.Join(lines, "\n") + "\n"
	path := filepath.Join("testdata", "leave006_simulation.golden")
	if os.Getenv("HCMNEXT_UPDATE_GOLDEN") != "" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden: %v (set HCMNEXT_UPDATE_GOLDEN=1)", err)
	}
	if string(want) != got {
		t.Fatalf("golden mismatch\n--- want ---\n%s\n--- got ---\n%s", want, got)
	}
}

func TestTodo_LEAVE_006_Conformance(t *testing.T) {
	snapshot, resolution, plan := simulationInputs(t)
	simulation, err := Simulate(snapshot, resolution, plan)
	if err != nil {
		t.Fatal(err)
	}
	// Obligations union eligibility and plan obligations.
	if len(simulation.Obligations) < 2 {
		t.Fatalf("obligations=%v", simulation.Obligations)
	}
	// Evidence refs cover every snapshot input.
	if len(simulation.EvidenceRefs) != len(requiredLeaveInputs) {
		t.Fatalf("refs=%d, want %d", len(simulation.EvidenceRefs), len(requiredLeaveInputs))
	}
}

func TestTodo_LEAVE_006_Mutation(t *testing.T) {
	snapshot, resolution, plan := simulationInputs(t)
	base, err := Simulate(snapshot, resolution, plan)
	if err != nil {
		t.Fatal(err)
	}
	// Component change re-identifies the simulation.
	changed := snapshotEntries(t)
	changed[0].Revision = "rev-8"
	altered, err := BuildSnapshot(changed)
	if err != nil {
		t.Fatal(err)
	}
	rebuilt, err := Simulate(altered, resolution, plan)
	if err != nil {
		t.Fatal(err)
	}
	if rebuilt.Digest == base.Digest {
		t.Fatal("component mutation kept the simulation digest")
	}
	// Forged proposals never verify.
	proposal, err := FreezeProposal(base, plan, 1)
	if err != nil {
		t.Fatal(err)
	}
	proposal.Revision = 2
	if err := proposal.Verify(); err == nil {
		t.Fatal("forged proposal verified")
	}
}

func TestTodo_LEAVE_006_Security(t *testing.T) {
	snapshot, resolution, plan := simulationInputs(t)
	simulation, err := Simulate(snapshot, resolution, plan)
	if err != nil {
		t.Fatal(err)
	}
	proposal, err := FreezeProposal(simulation, plan, 1)
	if err != nil {
		t.Fatal(err)
	}
	// No raw reference survives into the ordinary artifact.
	raw := map[string]bool{}
	for _, ref := range simulation.EvidenceRefs {
		raw[ref] = true
	}
	for _, masked := range proposal.View.MaskedRefs {
		if raw[masked] {
			t.Fatalf("raw ref leaked into safe view: %q", masked)
		}
	}
	// Masking keeps the compartment and drops the detail.
	if MaskRef("restricted:legal-sealed") != "restricted:***" {
		t.Fatal("masking lost the compartment")
	}
	if MaskRef("bare-bytes") != "untyped:***" {
		t.Fatal("untyped masking is wrong")
	}
	// Tampered execution flags never verify.
	executed := simulation
	executed.NotExecuted = false
	if err := executed.Verify(); err == nil {
		t.Fatal("executed simulation verified")
	}
}
