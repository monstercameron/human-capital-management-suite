package application_test

import (
	"context"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/application"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	talentconformance "github.com/monstercameron/human-capital-management-suite/internal/workflow/conformance/talent"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/simulate"
)

// TestTodo_CONF_012_Served proves CONF-012's GREEN clause through the served
// application boundary: the golden talent-calibration walk, driven only by
// the served surface's constructors, ends at the pending-approvals terminal
// with provenance-retention, calibration-record and evidence-retention
// obligations outstanding, and a stale population is reported stale rather
// than calibrated silently.
func TestTodo_CONF_012_Served(t *testing.T) {
	surface := application.NewServedTalentConformanceSurface()
	if surface.ReferenceWorkflowID() != talentconformance.WorkflowID {
		t.Fatalf("served workflow id = %q, want %q", surface.ReferenceWorkflowID(), talentconformance.WorkflowID)
	}
	if ids := surface.CapabilityIDs(); len(ids) != len(talentconformance.CapabilityIDs()) {
		t.Fatalf("served capability ids = %v, want %d ids", ids, len(talentconformance.CapabilityIDs()))
	}

	setup, err := surface.NewSetupWithParams(surface.GoldenEnvironment(), talentconformance.Params{})
	if err != nil {
		t.Fatalf("served NewSetupWithParams: %v", err)
	}
	receipt, err := simulate.Run(context.Background(), setup.Plan, setup.Inputs, setup.Options)
	if err != nil {
		t.Fatalf("served golden walk: %v", err)
	}
	if receipt.Mode != workflow.ModeSimulate {
		t.Fatalf("served mode = %s, want %s", receipt.Mode, workflow.ModeSimulate)
	}
	if receipt.Terminal.NodeID != talentconformance.NodeEndPendingApprovals {
		t.Fatalf("served terminal = %q, want %q (path %v)",
			receipt.Terminal.NodeID, talentconformance.NodeEndPendingApprovals, receipt.NodeIDs())
	}
	if receipt.Terminal.TerminalCode != "TALENT_CALIBRATION_PENDING_APPROVALS" {
		t.Fatalf("served terminal code = %q, want TALENT_CALIBRATION_PENDING_APPROVALS", receipt.Terminal.TerminalCode)
	}
	wantObligations := []string{
		talentconformance.ObligationProvenanceRetention,
		talentconformance.ObligationCalibrationRecord,
		talentconformance.ObligationEvidenceRetention,
	}
	if !servedTalentEqualSets(receipt.Terminal.OutstandingObligationRefs, wantObligations) {
		t.Fatalf("served outstanding obligations = %v, want %v",
			receipt.Terminal.OutstandingObligationRefs, wantObligations)
	}
	if err := receipt.Verify(); err != nil {
		t.Fatalf("served receipt does not verify: %v", err)
	}

	stale, err := surface.NewSetup(surface.StalePopulationEnvironment())
	if err != nil {
		t.Fatalf("served stale NewSetup: %v", err)
	}
	staleReceipt, err := simulate.Run(context.Background(), stale.Plan, stale.Inputs, stale.Options)
	if err != nil {
		t.Fatalf("served stale walk: %v", err)
	}
	if staleReceipt.Terminal.NodeID != talentconformance.NodeEndStalePopulation {
		t.Fatalf("served stale terminal = %q, want %q",
			staleReceipt.Terminal.NodeID, talentconformance.NodeEndStalePopulation)
	}
}

func TestServedTalentConformanceAppSurface(t *testing.T) {
	var app application.App
	if got := app.TalentConformance(); got.NewSetup == nil || got.GoldenEnvironment == nil {
		t.Fatal("composed application omitted Talent conformance surface")
	}
	var nilApp *application.App
	if got := nilApp.TalentConformance(); got.NewSetup != nil {
		t.Fatal("nil application exposed Talent conformance capabilities")
	}
}

func servedTalentEqualSets(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	seen := make(map[string]int, len(got))
	for _, item := range got {
		seen[item]++
	}
	for _, item := range want {
		seen[item]--
		if seen[item] < 0 {
			return false
		}
	}
	return true
}
