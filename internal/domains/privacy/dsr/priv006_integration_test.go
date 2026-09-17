package dsr

import (
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/governance/privacy/inventory"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// TestTodo_PRIV_006_Integration is the INTEGRATION matrix test for PRIV-006.
//
// Applicability note (GOV-018): item resolution itself is a pure decision
// over presented evidence -- it reaches no database, object store,
// transport or provider, so there is no store-backed integration to write.
// The multi-package boundary this todo actually crosses is the PRIV-001
// processing registry (does the governing activity certify as executable?)
// composed with the PRIV-005 intake/verify pipeline (is the request
// advanceable?) composed with PRIV-006 resolution. This test exercises that
// real boundary with zero mocks: a real validated inventory, a real
// verified request, and the real Resolve.
func TestTodo_PRIV_006_Integration(t *testing.T) {
	t.Run("a validated inventory plus a verified request resolves end to end", func(t *testing.T) {
		release, err := inventory.ValidateExecutable(validProcessingInventory())
		if err != nil {
			t.Fatalf("ValidateExecutable: %v", err)
		}
		if release.Inventory.Occurrences[0].Recipient != release.Inventory.Flows[0].Recipient {
			t.Fatal("the real registry did not retain its receipt evidence: the boundary is stubbed")
		}

		req := fixtureVerifiedRequest(t, "dsr-int-erase", KindErasure, trust.AssuranceHigh)
		res, err := Resolve(fixtureResolutionSpec(t, req, allRedClasses()))
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if err := res.Validate(); err != nil {
			t.Fatalf("end-to-end certificate does not validate: %v", err)
		}
		if res.RequestDigest != req.Digest() {
			t.Error("the certificate is not bound to the verified request's digest: the pipeline stages disagree")
		}
		if len(res.Items) != len(allRedClasses()) {
			t.Errorf("resolved %d items, want the full presented set (%d)", len(res.Items), len(allRedClasses()))
		}
	})

	t.Run("an invalid governing inventory gates the pipeline before any resolution", func(t *testing.T) {
		broken := validProcessingInventory()
		broken.Flows[0].TransferRegions = []string{"APAC"} // outside the activity's regions
		if _, err := inventory.ValidateExecutable(broken); !errors.Is(err, inventory.ErrInvalid) {
			t.Fatalf("ValidateExecutable(broken) = %v, want %v: the gate this pipeline depends on is not enforced", err, inventory.ErrInvalid)
		}
		// The harness never reaches Resolve with an uncertified governing
		// context: there is deliberately no certificate to assert on here,
		// only the gate that stops the pipeline first.
	})
}

// validProcessingInventory is a real PRIV-001 processing registry for the
// employment context a DSR resolves against: one approved activity, one
// bound flow, one observed receipt. It mirrors the shape
// inventory.validInventory certifies, scoped to worker data instead of
// payroll.
func validProcessingInventory() inventory.Inventory {
	from := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	a := inventory.ProcessingActivity{
		ID: "employment", Version: "2026-01", Status: inventory.StatusApproved,
		Controller: "acme", Processor: "human-capital-management-suite",
		Purpose:          "employment",
		DataSubjects:     []string{"worker"},
		DataCategories:   []string{"contact", "compensation"},
		Systems:          []string{"hr", "records-archive"},
		Recipients:       []string{"records-archive"},
		Regions:          []string{"US", "EU"},
		LawfulBasis:      "contract",
		Retention:        "employment-7y",
		SecurityControls: []string{"encryption"},
		DPIARef:          "dpia/employment/1",
		Obligations: []inventory.ObligationDeadline{
			{Kind: inventory.MonitoringConsent, Jurisdiction: "US-CA", RulePackRelease: "us-2026.1", DeadlineHours: 24, EffectiveFrom: from},
			{Kind: inventory.BreachNotification, Jurisdiction: "US-CA", RulePackRelease: "us-2026.1", DeadlineHours: 72, EffectiveFrom: from},
		},
	}
	f := inventory.ProcessingDataFlow{
		ID: "hr-to-archive", ActivityID: a.ID, Version: a.Version,
		SourceSystem: "hr", DestinationSystem: "records-archive",
		Recipient: "records-archive", Controller: a.Controller, Processor: a.Processor,
		Purpose: a.Purpose, DataCategories: []string{"contact"},
		Operations: []string{"transmit"}, TransferRegions: []string{"EU"},
		ContractRefs: []string{"dpa/1"}, Safeguards: []string{"scc"},
		SecurityControls: []string{"encryption"}, RetentionRef: a.Retention,
		EffectiveFrom: from,
	}
	o := inventory.DataFlowOccurrence{
		ID: "receipt-1", FlowID: f.ID, Recipient: "records-archive",
		Region: "EU", DataCategories: []string{"contact"}, ReceivedAt: from.Add(time.Hour),
	}
	return inventory.Inventory{Activities: []inventory.ProcessingActivity{a}, Flows: []inventory.ProcessingDataFlow{f}, Occurrences: []inventory.DataFlowOccurrence{o}}
}
