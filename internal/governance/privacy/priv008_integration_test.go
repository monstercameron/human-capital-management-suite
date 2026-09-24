package privacy

import (
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/governance/privacy/inventory"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/dataclass"
)

// TestTodo_PRIV_008_Integration is the INTEGRATION matrix test for PRIV-008.
//
// Applicability note (GOV-018): the FTI boundary is a policy kernel -- it
// reaches no database, object store, transport or provider. The
// multi-package boundary it actually crosses is the PRIV-001 processing
// registry (is the governing activity executable?) composed with the live
// government-data policy table (which key scope does FTI require today?)
// composed with real session assurance. This test exercises that real
// boundary with zero mocks.
func TestTodo_PRIV_008_Integration(t *testing.T) {
	t.Run("a validated registry activity classifies onto the live FTI key scope", func(t *testing.T) {
		release, err := inventory.ValidateExecutable(ftiRegistryInventory())
		if err != nil {
			t.Fatalf("ValidateExecutable: %v", err)
		}
		var activity inventory.ProcessingActivity
		for _, a := range release.Inventory.Activities {
			if a.ID == "tax-return-processing" {
				activity = a
			}
		}
		if activity.ID == "" {
			t.Fatal("the real registry lost the FTI activity: the boundary is stubbed")
		}

		classification, err := ClassifyFTI(activity)
		if err != nil {
			t.Fatalf("ClassifyFTI (registry activity): %v", err)
		}
		live, err := dataclass.ClassPolicyFor(dataclass.FTI)
		if err != nil {
			t.Fatalf("ClassPolicyFor(FTI): %v", err)
		}
		if classification.KeyScope != string(live.Key) {
			t.Errorf("classification key scope = %q, want the live FTI requirement %q", classification.KeyScope, live.Key)
		}

		grant, err := AuthorizeFTIAccess(FTIAccessSpec{
			Classification: classification, Principal: "tax-admin-7",
			Assurance: trust.AssuranceHigh, Remote: true,
			KeyScope: classification.KeyScope, Purpose: "tax_return_processing",
		})
		if err != nil {
			t.Fatalf("AuthorizeFTIAccess (registry-bound): %v", err)
		}
		if grant.ActivityDigest != classification.ActivityDigest {
			t.Error("the grant is not bound to the classified activity's digest")
		}
	})

	t.Run("a valid registry activity without FTI data cannot mint FTI grants", func(t *testing.T) {
		release, err := inventory.ValidateExecutable(ftiRegistryInventory())
		if err != nil {
			t.Fatalf("ValidateExecutable: %v", err)
		}
		var ordinary inventory.ProcessingActivity
		for _, a := range release.Inventory.Activities {
			if a.ID == "employment" {
				ordinary = a
			}
		}
		if _, err := ClassifyFTI(ordinary); !errors.Is(err, ErrFTIBlocked) {
			t.Fatalf("ClassifyFTI(ordinary registry activity) = %v, want %v", err, ErrFTIBlocked)
		}
	})
}

// ftiRegistryInventory is a real PRIV-001 registry holding both the FTI
// tax activity and an ordinary employment activity, each with a bound flow
// and receipt. Both activities must certify as executable for the boundary
// tests above to mean anything.
func ftiRegistryInventory() inventory.Inventory {
	from := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	obligations := []inventory.ObligationDeadline{
		{Kind: inventory.MonitoringConsent, Jurisdiction: "US", RulePackRelease: "us-2026.1", DeadlineHours: 24, EffectiveFrom: from},
		{Kind: inventory.BreachNotification, Jurisdiction: "US", RulePackRelease: "us-2026.1", DeadlineHours: 72, EffectiveFrom: from},
	}
	ftiActivity := inventory.ProcessingActivity{
		ID: "tax-return-processing", Version: "2026-01", Status: inventory.StatusApproved,
		Controller: "acme", Processor: "tax-engine",
		Purpose:          "tax_return_processing",
		DataSubjects:     []string{"worker"},
		DataCategories:   []string{"FTI", "contact"},
		Systems:          []string{"hr", "irs-mef"},
		Recipients:       []string{"irs-mef", "state-revenue-agency"},
		Regions:          []string{"US"},
		LawfulBasis:      "legal-obligation",
		Retention:        "tax-7y",
		SecurityControls: []string{"pub1075-encryption", "audit-logging"},
		DPIARef:          "dpia/tax/1",
		Obligations:      obligations,
	}
	ordinary := inventory.ProcessingActivity{
		ID: "employment", Version: "2026-01", Status: inventory.StatusApproved,
		Controller: "acme", Processor: "human-capital-management-suite",
		Purpose:          "employment",
		DataSubjects:     []string{"worker"},
		DataCategories:   []string{"contact"},
		Systems:          []string{"hr", "records-archive"},
		Recipients:       []string{"records-archive"},
		Regions:          []string{"US"},
		LawfulBasis:      "contract",
		Retention:        "employment-7y",
		SecurityControls: []string{"encryption"},
		DPIARef:          "dpia/employment/1",
		Obligations:      obligations,
	}
	ftiFlow := inventory.ProcessingDataFlow{
		ID: "hr-to-mef", ActivityID: ftiActivity.ID, Version: ftiActivity.Version,
		SourceSystem: "hr", DestinationSystem: "irs-mef",
		Recipient: "irs-mef", Controller: ftiActivity.Controller, Processor: ftiActivity.Processor,
		SourceRegion: "US", TransferPolicyVersion: "privacy-transfer-rules-2026.09.1",
		Purpose: ftiActivity.Purpose, DataCategories: []string{"FTI"},
		Operations: []string{"transmit"}, TransferRegions: []string{"US"},
		ContractRefs: []string{"dpa/tax-1"}, Safeguards: []string{"pub1075"},
		SecurityControls: []string{"pub1075-encryption"}, RetentionRef: ftiActivity.Retention,
		EffectiveFrom: from,
	}
	ordinaryFlow := inventory.ProcessingDataFlow{
		ID: "hr-to-archive", ActivityID: ordinary.ID, Version: ordinary.Version,
		SourceSystem: "hr", DestinationSystem: "records-archive",
		Recipient: "records-archive", Controller: ordinary.Controller, Processor: ordinary.Processor,
		SourceRegion: "US", TransferPolicyVersion: "privacy-transfer-rules-2026.09.1",
		Purpose: ordinary.Purpose, DataCategories: []string{"contact"},
		Operations: []string{"transmit"}, TransferRegions: []string{"US"},
		ContractRefs: []string{"dpa/1"}, Safeguards: []string{"scc"},
		SecurityControls: []string{"encryption"}, RetentionRef: ordinary.Retention,
		EffectiveFrom: from,
	}
	occurrences := []inventory.DataFlowOccurrence{
		{ID: "receipt-fti-1", FlowID: ftiFlow.ID, Recipient: "irs-mef", Region: "US", DataCategories: []string{"FTI"}, ReceivedAt: from.Add(time.Hour)},
		{ID: "receipt-hr-1", FlowID: ordinaryFlow.ID, Recipient: "records-archive", Region: "US", DataCategories: []string{"contact"}, ReceivedAt: from.Add(2 * time.Hour)},
	}
	return inventory.Inventory{
		Activities:  []inventory.ProcessingActivity{ftiActivity, ordinary},
		Flows:       []inventory.ProcessingDataFlow{ftiFlow, ordinaryFlow},
		Occurrences: occurrences,
	}
}
