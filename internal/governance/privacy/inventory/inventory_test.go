package inventory

import (
	"strings"
	"testing"
	"time"
)

func validInventory() Inventory {
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	a := ProcessingActivity{ID: "payroll", Version: "2026-01", Status: StatusApproved, Controller: "acme", Processor: "human-capital-management-suite", Purpose: "payroll", DataSubjects: []string{"worker"}, DataCategories: []string{"salary"}, Systems: []string{"hr", "payroll-provider"}, Recipients: []string{"payroll-provider"}, Regions: []string{"US", "EU"}, LawfulBasis: "legal-obligation", Retention: "payroll-7y", SecurityControls: []string{"encryption"}, DPIARef: "dpia/payroll/1", Obligations: []ObligationDeadline{{Kind: MonitoringConsent, Jurisdiction: "EU", RulePackRelease: "eu-2026.1", DeadlineHours: 24, EffectiveFrom: from}, {Kind: BreachNotification, Jurisdiction: "EU", RulePackRelease: "eu-2026.1", DeadlineHours: 72, EffectiveFrom: from}}}
	f := ProcessingDataFlow{ID: "payroll-to-provider", ActivityID: a.ID, Version: a.Version, SourceSystem: "hr", DestinationSystem: "payroll-provider", Recipient: "payroll-provider", SourceRegion: "EU", TransferPolicyVersion: "privacy-transfer-rules-2026.09.1", Controller: a.Controller, Processor: a.Processor, Purpose: a.Purpose, DataCategories: []string{"salary"}, Operations: []string{"transmit"}, TransferRegions: []string{"EU"}, ContractRefs: []string{"dpa/1"}, Safeguards: []string{"eu.adequacy.decision.v1"}, SecurityControls: []string{"encryption"}, RetentionRef: a.Retention, EffectiveFrom: from}
	o := DataFlowOccurrence{ID: "receipt-1", FlowID: f.ID, Recipient: "payroll-provider", Region: "EU", DataCategories: []string{"salary"}, ReceivedAt: from.Add(time.Hour)}
	return Inventory{Activities: []ProcessingActivity{a}, Flows: []ProcessingDataFlow{f}, Occurrences: []DataFlowOccurrence{o}}
}

func TestInventoryValidatesVersionedFlowAndReceipt(t *testing.T) {
	i := validInventory()
	if err := i.Validate(); err != nil {
		t.Fatalf("valid inventory rejected: %v", err)
	}
	d1, err := i.Digest()
	if err != nil {
		t.Fatal(err)
	}
	d2, err := i.Digest()
	if err != nil || d1 != d2 {
		t.Fatalf("digest is not stable: %q %q %v", d1, d2, err)
	}
}

func TestInventoryRejectsMissingGovernanceAndOutOfScopeReceipt(t *testing.T) {
	i := validInventory()
	i.Activities[0].Obligations = []ObligationDeadline{{Kind: BreachNotification, Jurisdiction: "EU", RulePackRelease: "eu-2026.1", DeadlineHours: 72, EffectiveFrom: time.Now()}}
	if err := i.Validate(); err == nil || !strings.Contains(err.Error(), "monitoring") {
		t.Fatalf("expected monitoring obligation error, got %v", err)
	}
	i = validInventory()
	i.Activities[0].Obligations[0].EffectiveFrom = time.Time{}
	if err := i.Validate(); err == nil {
		t.Fatal("expected invalid obligation interval")
	}
	i = validInventory()
	i.Occurrences[0].Region = "APAC"
	if err := i.Validate(); err == nil || !strings.Contains(err.Error(), "outside flow scope") {
		t.Fatalf("expected receipt scope error, got %v", err)
	}
}

func TestInventoryRejectsFlowVersionAndCategoryEscapes(t *testing.T) {
	i := validInventory()
	i.Flows[0].Version = "2025-01"
	if err := i.Validate(); err == nil || !strings.Contains(err.Error(), "no versioned activity") {
		t.Fatalf("expected version binding error, got %v", err)
	}
	i = validInventory()
	i.Flows[0].DataCategories = []string{"bank-account"}
	if err := i.Validate(); err == nil || !strings.Contains(err.Error(), "exceeds activity scope") {
		t.Fatalf("expected category scope error, got %v", err)
	}
}
