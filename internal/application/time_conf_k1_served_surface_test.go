package application_test

import (
	"context"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/application"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	timeconformance "github.com/monstercameron/human-capital-management-suite/internal/workflow/conformance/time"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/simulate"
)

// TestTodo_CONF_011_Served proves CONF-011's GREEN clause through the served
// application boundary: the golden time-punch walk, driven only by the
// served surface's constructors, ends ACCEPTED at the bridge-confirmed
// terminal with attestation, cutoff, bridge-integrity and retention
// obligations outstanding, and a duplicate punch is classified DUPLICATE
// rather than accepted silently.
func TestTodo_CONF_011_Served(t *testing.T) {
	surface := application.NewServedTimeConformanceSurface()
	if surface.ReferenceWorkflowID() != timeconformance.WorkflowID {
		t.Fatalf("served workflow id = %q, want %q", surface.ReferenceWorkflowID(), timeconformance.WorkflowID)
	}
	if ids := surface.CapabilityIDs(); len(ids) != len(timeconformance.CapabilityIDs()) {
		t.Fatalf("served capability ids = %v, want %d ids", ids, len(timeconformance.CapabilityIDs()))
	}

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
	if receipt.Terminal.NodeID != timeconformance.NodeEndAcceptedBridgeConfirmed {
		t.Fatalf("served terminal = %q, want %q (path %v)",
			receipt.Terminal.NodeID, timeconformance.NodeEndAcceptedBridgeConfirmed, receipt.NodeIDs())
	}
	if receipt.Terminal.TerminalCode != "TIME_PUNCH_ACCEPTED_BRIDGE_CONFIRMED" {
		t.Fatalf("served terminal code = %q, want TIME_PUNCH_ACCEPTED_BRIDGE_CONFIRMED", receipt.Terminal.TerminalCode)
	}
	wantObligations := []string{
		timeconformance.ObligationAttestation,
		timeconformance.ObligationCutoff,
		timeconformance.ObligationBridgeIntegrity,
		timeconformance.ObligationRecordsRetention,
	}
	if !servedTimeEqualSets(receipt.Terminal.OutstandingObligationRefs, wantObligations) {
		t.Fatalf("served outstanding obligations = %v, want %v",
			receipt.Terminal.OutstandingObligationRefs, wantObligations)
	}
	if err := receipt.Verify(); err != nil {
		t.Fatalf("served receipt does not verify: %v", err)
	}

	duplicate, err := surface.NewSetup(surface.DuplicatePunchEnvironment())
	if err != nil {
		t.Fatalf("served duplicate NewSetup: %v", err)
	}
	duplicateReceipt, err := simulate.Run(context.Background(), duplicate.Plan, duplicate.Inputs, duplicate.Options)
	if err != nil {
		t.Fatalf("served duplicate walk: %v", err)
	}
	if duplicateReceipt.Terminal.NodeID != timeconformance.NodeEndDuplicatePunch {
		t.Fatalf("served duplicate terminal = %q, want %q",
			duplicateReceipt.Terminal.NodeID, timeconformance.NodeEndDuplicatePunch)
	}
}

func TestServedTimeConformanceAppSurface(t *testing.T) {
	var app application.App
	if got := app.TimeConformance(); got.NewSetup == nil || got.GoldenEnvironment == nil {
		t.Fatal("composed application omitted Time conformance surface")
	}
	var nilApp *application.App
	if got := nilApp.TimeConformance(); got.NewSetup != nil {
		t.Fatal("nil application exposed Time conformance capabilities")
	}
}

func servedTimeEqualSets(got, want []string) bool {
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
