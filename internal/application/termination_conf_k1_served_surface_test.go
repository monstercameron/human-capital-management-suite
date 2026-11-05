package application_test

import (
	"context"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/application"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	terminationconformance "github.com/monstercameron/human-capital-management-suite/internal/workflow/conformance/termination"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/simulate"
)

// TestTodo_CONF_005_Served proves CONF-005's GREEN clause through the served
// application boundary: the golden termination walk, driven only by the
// served surface's constructors, ends at the pending-obligations terminal
// with all six obligations outstanding (P0 access revocation tracked as its
// own obligation), and a legal hold blocks instead of proceeding.
func TestTodo_CONF_005_Served(t *testing.T) {
	surface := application.NewServedTerminationConformanceSurface()

	setup, err := surface.NewSetup(surface.GoldenEnvironment())
	if err != nil {
		t.Fatalf("served NewSetup: %v", err)
	}
	receipt, err := simulate.Run(context.Background(), setup.Plan, setup.Inputs, setup.Options)
	if err != nil {
		t.Fatalf("served golden walk: %v", err)
	}
	if receipt.Mode != workflow.ModeSimulate {
		t.Fatalf("served mode = %s, want %s", receipt.Mode, workflow.ModeSimulate)
	}
	if receipt.Terminal.NodeID != terminationconformance.NodeEndPendingObligations {
		t.Fatalf("served terminal = %q, want %q (path %v)",
			receipt.Terminal.NodeID, terminationconformance.NodeEndPendingObligations, receipt.NodeIDs())
	}
	wantObligations := []string{
		terminationconformance.ObligationFinalPay,
		terminationconformance.ObligationAccessRevocation,
		terminationconformance.ObligationNoticeEvidence,
		terminationconformance.ObligationRecordsRetention,
		terminationconformance.ObligationEquipment,
		terminationconformance.ObligationBenefits,
	}
	if !servedTerminationEqualSets(receipt.Terminal.OutstandingObligationRefs, wantObligations) {
		t.Fatalf("served outstanding obligations = %v, want %v",
			receipt.Terminal.OutstandingObligationRefs, wantObligations)
	}
	if err := receipt.Verify(); err != nil {
		t.Fatalf("served receipt does not verify: %v", err)
	}

	blocked, err := surface.NewSetup(surface.LegalHoldEnvironment())
	if err != nil {
		t.Fatalf("served legal-hold NewSetup: %v", err)
	}
	blockedReceipt, err := simulate.Run(context.Background(), blocked.Plan, blocked.Inputs, blocked.Options)
	if err != nil {
		t.Fatalf("served legal-hold walk: %v", err)
	}
	if blockedReceipt.Terminal.NodeID != terminationconformance.NodeEndLegalHoldBlocked {
		t.Fatalf("served legal-hold terminal = %q, want %q",
			blockedReceipt.Terminal.NodeID, terminationconformance.NodeEndLegalHoldBlocked)
	}
}

func TestServedTerminationConformanceAppSurface(t *testing.T) {
	var app application.App
	if got := app.TerminationConformance(); got.NewSetup == nil || got.GoldenEnvironment == nil {
		t.Fatal("composed application omitted Termination conformance surface")
	}
	var nilApp *application.App
	if got := nilApp.TerminationConformance(); got.NewSetup != nil {
		t.Fatal("nil application exposed Termination conformance capabilities")
	}
}

func servedTerminationEqualSets(got, want []string) bool {
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
