package convergence

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/planning/closurewitness"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/designclosure"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/gateevidence/selectionbind"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/intentcoverage"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/workflowmaturity"
)

func TestFromClosureWitnessOwnsDefectsAndLeavesOrphansOwnerless(t *testing.T) {
	report := closurewitness.Report{
		Witnesses: []closurewitness.Witness{{Definition: fxAlpha, Defects: []closurewitness.Defect{
			{Code: closurewitness.DefectAbsent, Class: closurewitness.ClassHandler, Identity: "HANDLER|x|entry", Detail: "no handler"},
			{Code: closurewitness.DefectAbsent, Class: "NEW_CLASS", Identity: "NEW|x", Detail: "unknown class"},
		}}},
		Orphans: []closurewitness.Defect{{Code: closurewitness.DefectOrphan, Class: closurewitness.ClassEndpoint, Identity: "ENDPOINT|svc/M", Detail: "orphan"}},
	}
	got := FromClosureWitness(report)
	if len(got) != 3 {
		t.Fatalf("observations = %+v", got)
	}
	if got[0].Owner != fxAlpha || got[0].Contract != ContractHandler || got[0].Subject != "" {
		t.Errorf("witness defect = %+v", got[0])
	}
	if got[1].Contract != "" {
		t.Errorf("unmapped class got contract %s", got[1].Contract)
	}
	if got[2].Owner != "" || got[2].Contract != ContractEndpoint || got[2].Subject != "ENDPOINT|svc/M" {
		t.Errorf("orphan = %+v", got[2])
	}
	for _, class := range closurewitness.Classes() {
		if !closureClassContract[class].Valid() {
			t.Errorf("closure class %s has no contract", class)
		}
	}
	unknowns := ClosureWitnessUnknowns(closurewitness.Snapshot{Unsourced: []closurewitness.EdgeClass{closurewitness.ClassEvidence}})
	if len(unknowns) != 1 || unknowns[0].Code != UnknownInputUnsourced || unknowns[0].Ref != "closurewitness:EVIDENCE" {
		t.Errorf("unsourced unknowns = %+v", unknowns)
	}
}

func TestFromDesignClosureMapsFindingsAndMissingWorkBlockersOnly(t *testing.T) {
	register := designclosure.Register{Rows: []designclosure.Row{{Item: fxBeta, Blockers: []designclosure.Blocker{
		{Kind: "delivery", Ref: "no active DIRECT todos", Detail: "bound to no delivery todo"},
		{Kind: "todo", Ref: "OPEN-1", Detail: "bound todo still open"},
		{Kind: "deferral", Ref: "later", Detail: "deferred"},
		{Kind: "gap", Ref: "F-1", Detail: "open capability gap"},
	}}}}
	findings := []designclosure.Finding{{Item: fxBeta, Code: designclosure.MissingTest, Detail: "no test"}, {Item: fxBeta, Code: "SOMETHING_NEW", Detail: "?"}}
	got := FromDesignClosure(register, findings)
	contracts := map[Contract]int{}
	for _, o := range got {
		if o.Owner != fxBeta || o.Compiler != CompilerDesignClosure {
			t.Errorf("observation = %+v", o)
		}
		contracts[o.Contract]++
	}
	if len(got) != 4 || contracts[ContractTest] != 1 || contracts[ContractTodo] != 1 || contracts[ContractCapability] != 1 || contracts[""] != 1 {
		t.Fatalf("observations = %+v", got)
	}
	for code, contract := range designFindingContract {
		if !contract.Valid() {
			t.Errorf("design finding %s maps to invalid %s", code, contract)
		}
	}
}

func TestFromIntentCoverageResolvesFeatureOwnersAndLeavesStrangersOwnerless(t *testing.T) {
	snap := intentcoverage.Snapshot{
		Intents: []intentcoverage.Intent{{ID: fxAlpha}},
		Gaps:    []intentcoverage.CapabilityGap{{FeatureID: "FEAT-1", IntentID: fxAlpha}},
	}
	report := intentcoverage.Report{Orphans: []intentcoverage.Orphan{
		{Kind: intentcoverage.KindIntentTest, ID: fxAlpha, Detail: "no oracle"},
		{Kind: intentcoverage.KindGapDeferment, ID: "FEAT-1", Detail: "deferred without target"},
		{Kind: intentcoverage.KindTodoDirectDangling, ID: "NEXT-9", Detail: "dangling"},
	}}
	got := FromIntentCoverage(report, snap)
	if len(got) != 3 || got[0].Owner != fxAlpha || got[0].Contract != ContractTest ||
		got[1].Owner != fxAlpha || got[1].Contract != ContractCapability ||
		got[2].Owner != "" || got[2].Subject != "todo_direct_dangling:NEXT-9" || got[2].Contract != ContractTodo {
		t.Fatalf("observations = %+v", got)
	}
	for kind, contract := range intentOrphanContract {
		if !contract.Valid() {
			t.Errorf("orphan kind %s maps to invalid %s", kind, contract)
		}
	}
}

func TestFromWorkflowMaturityMapsBlockersOntoDefinitions(t *testing.T) {
	report := workflowmaturity.Report{Results: []workflowmaturity.DefinitionResult{{Definition: fxBeta, Blockers: []workflowmaturity.Blocker{
		{Code: workflowmaturity.MissingAdversarialScenario, Detail: "no matrix"},
		{Code: "FUTURE_CODE", Detail: "?"},
	}}}}
	got := FromWorkflowMaturity(report)
	if len(got) != 2 || got[0].Owner != fxBeta || got[0].Contract != ContractScenario || got[1].Contract != "" {
		t.Fatalf("observations = %+v", got)
	}
	for code, contract := range maturityBlockerContract {
		if !contract.Valid() {
			t.Errorf("maturity code %s maps to invalid %s", code, contract)
		}
	}
}

func TestFromSelectionBindDeduplicatesSlotsOntoFillersAndSurfacesThem(t *testing.T) {
	report := selectionbind.Report{
		ManifestReasons: []string{"signature does not verify"},
		Bindings: []selectionbind.BindingResult{
			{TodoID: fxSelectX, Reasons: []string{"placeholder vendor"}},
			{TodoID: fxSelectY, Ready: true},
		},
		Slots: []selectionbind.SlotResult{
			{Slot: "provider", FillingTodoIDs: []string{fxSelectX}, Reasons: []string{"SELECT-X has not passed its own gate"}},
			{Slot: "slo", Reasons: []string{"no filler declared"}},
			{Slot: "jurisdiction", Filled: true},
		},
	}
	obs, unknowns := FromSelectionBind(report)
	if len(obs) != 4 {
		t.Fatalf("observations = %+v", obs)
	}
	if obs[0].Owner != SelectionManifestOwner || obs[1].Owner != fxSelectX || obs[2].Owner != fxSelectX || obs[3].Owner != "" || obs[3].Subject != "slot:slo" {
		t.Errorf("observations = %+v", obs)
	}
	for _, o := range obs {
		if o.Contract != ContractSelectionGate {
			t.Errorf("selection observation contract %s", o.Contract)
		}
	}
	if len(unknowns) != 2 || unknowns[0].Ref != "slot:provider" || unknowns[1].Ref != "slot:slo" {
		t.Errorf("unknowns = %+v", unknowns)
	}
	r := Converge(Snapshot{Selected: []SelectedOwner{{Owner: fxSelectX, Phase: "P0"}}, Observations: obs, Unknowns: unknowns})
	if g := gapByKey(t, r, fxSelectX, ContractSelectionGate, ""); len(g.Observations) != 2 {
		t.Errorf("binding and slot reasons did not merge: %+v", g)
	}
}
