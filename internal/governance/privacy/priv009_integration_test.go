package privacy

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/governance/privacy/inventory"
)

// jurisdictionDecided reports whether the certificate carries a state-law
// decision for jurisdiction.
func jurisdictionDecided(d NotificationDecision, jurisdiction string) (DutyDecision, bool) {
	for _, dec := range d.Decisions {
		if dec.Duty == DutyStateBreachLaw && dec.Jurisdiction == jurisdiction {
			return dec, true
		}
	}
	return DutyDecision{}, false
}

// TestTodo_PRIV_009_Integration is the INTEGRATION matrix test for PRIV-009.
//
// Applicability note (GOV-018): notification decisions are a policy kernel
// -- they reach no database, object store, transport or provider. The
// multi-package boundary this todo actually crosses is the PRIV-001
// processing registry: the incident's jurisdiction-matrix version is the
// legal rule-pack release whose BREACH_NOTIFICATION obligations a real
// validated inventory carries, and the state-law notice deadlines are read
// off those real obligation rows. This test exercises that real boundary
// with zero mocks.
func TestTodo_PRIV_009_Integration(t *testing.T) {
	t.Run("state-law deadlines resolve from the real registry's breach obligations", func(t *testing.T) {
		release, err := inventory.ValidateExecutable(breachRegistryInventory())
		if err != nil {
			t.Fatalf("ValidateExecutable: %v", err)
		}
		deadlines := breachObligationDeadlines(release.Inventory)
		if len(deadlines) == 0 {
			t.Fatal("the real registry carries no breach obligations: the boundary is stubbed")
		}

		matrix := matrixFromRegistry(t, release.Inventory)
		incident := fixtureBreachIncident(t)
		incident.MatrixVersion = matrix.Version
		incident.EvidenceID = breachEvidencePrefix + "incident:" + incident.Digest()

		decision, err := DecideNotifications(incident, matrix, mustInstant(t, fxBreachDecidedAt))
		if err != nil {
			t.Fatalf("DecideNotifications (registry-bound): %v", err)
		}
		// The jurisdiction-agnostic matrix carries the most urgent registry
		// deadline, so notice is never later than any covered jurisdiction
		// requires (over-notification fails safe).
		var min int64
		for jurisdiction, hours := range deadlines {
			if _, ok := jurisdictionDecided(decision, jurisdiction); !ok {
				t.Errorf("registry jurisdiction %q has no state-law decision behind it", jurisdiction)
			}
			if min == 0 || hours < min {
				min = hours
			}
		}
		for _, dec := range decision.Decisions {
			if dec.Duty != DutyStateBreachLaw || dec.Decision != DecisionNotify {
				continue
			}
			if dec.DeadlineHours != min {
				t.Errorf("state decision for %q deadline = %d hours, want the most urgent registry deadline %d", dec.Jurisdiction, dec.DeadlineHours, min)
			}
			if !strings.Contains(dec.Authority, "us-2026.1") {
				t.Errorf("state decision for %q authority = %q, want it to cite the rule-pack release", dec.Jurisdiction, dec.Authority)
			}
		}
	})

	t.Run("an incident versioned against another rule-pack release never decides on this registry", func(t *testing.T) {
		release, err := inventory.ValidateExecutable(breachRegistryInventory())
		if err != nil {
			t.Fatalf("ValidateExecutable: %v", err)
		}
		matrix := matrixFromRegistry(t, release.Inventory)
		incident := fixtureBreachIncident(t) // still versioned at the v3 test matrix
		incident.EvidenceID = breachEvidencePrefix + "incident:" + incident.Digest()
		if _, err := DecideNotifications(incident, matrix, mustInstant(t, fxBreachDecidedAt)); !errors.Is(err, ErrBreachBlocked) {
			t.Fatalf("DecideNotifications(cross-release matrix) = %v, want %v", err, ErrBreachBlocked)
		}
	})
}

// breachRegistryInventory is a real PRIV-001 registry whose activities
// carry jurisdiction-scoped BREACH_NOTIFICATION obligations under rule-pack
// release us-2026.1: the release doubles as the jurisdiction-matrix
// version incidents are decided against.
func breachRegistryInventory() inventory.Inventory {
	from := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	obligations := func(jurisdiction string, hours int) []inventory.ObligationDeadline {
		return []inventory.ObligationDeadline{
			{Kind: inventory.MonitoringConsent, Jurisdiction: jurisdiction, RulePackRelease: "us-2026.1", DeadlineHours: 24, EffectiveFrom: from},
			{Kind: inventory.BreachNotification, Jurisdiction: jurisdiction, RulePackRelease: "us-2026.1", DeadlineHours: hours, EffectiveFrom: from},
		}
	}
	payments := inventory.ProcessingActivity{
		ID: "payment-processing", Version: "2026-01", Status: inventory.StatusApproved,
		Controller: "acme", Processor: "payments-provider",
		Purpose:          "payment_processing",
		DataSubjects:     []string{"worker"},
		DataCategories:   []string{"payment", "contact"},
		Systems:          []string{"hr", "payments-provider"},
		Recipients:       []string{"payments-provider"},
		Regions:          []string{"US"},
		LawfulBasis:      "contract",
		Retention:        "payments-7y",
		SecurityControls: []string{"encryption"},
		DPIARef:          "dpia/payments/1",
		Obligations:      obligations("US-CA", 72),
	}
	bank := inventory.ProcessingActivity{
		ID: "payroll-payout", Version: "2026-01", Status: inventory.StatusApproved,
		Controller: "acme", Processor: "payroll-provider",
		Purpose:          "payroll_payout",
		DataSubjects:     []string{"worker"},
		DataCategories:   []string{"bank", "contact"},
		Systems:          []string{"hr", "payroll-provider"},
		Recipients:       []string{"payroll-provider"},
		Regions:          []string{"US"},
		LawfulBasis:      "contract",
		Retention:        "payroll-7y",
		SecurityControls: []string{"encryption"},
		DPIARef:          "dpia/payroll/1",
		Obligations:      obligations("US-NY", 96),
	}
	flows := []inventory.ProcessingDataFlow{
		{
			ID: "hr-to-payments", ActivityID: payments.ID, Version: payments.Version,
			SourceSystem: "hr", DestinationSystem: "payments-provider",
			Recipient: "payments-provider", Controller: payments.Controller, Processor: payments.Processor,
			SourceRegion: "US", TransferPolicyVersion: "privacy-transfer-rules-2026.09.1",
			Purpose: payments.Purpose, DataCategories: []string{"payment"},
			Operations: []string{"transmit"}, TransferRegions: []string{"US"},
			ContractRefs: []string{"dpa/pay-1"}, Safeguards: []string{"scc"},
			SecurityControls: []string{"encryption"}, RetentionRef: payments.Retention,
			EffectiveFrom: from,
		},
		{
			ID: "hr-to-payroll", ActivityID: bank.ID, Version: bank.Version,
			SourceSystem: "hr", DestinationSystem: "payroll-provider",
			Recipient: "payroll-provider", Controller: bank.Controller, Processor: bank.Processor,
			SourceRegion: "US", TransferPolicyVersion: "privacy-transfer-rules-2026.09.1",
			Purpose: bank.Purpose, DataCategories: []string{"bank"},
			Operations: []string{"transmit"}, TransferRegions: []string{"US"},
			ContractRefs: []string{"dpa/pay-2"}, Safeguards: []string{"scc"},
			SecurityControls: []string{"encryption"}, RetentionRef: bank.Retention,
			EffectiveFrom: from,
		},
	}
	occurrences := []inventory.DataFlowOccurrence{
		{ID: "receipt-pay-1", FlowID: flows[0].ID, Recipient: "payments-provider", Region: "US", DataCategories: []string{"payment"}, ReceivedAt: from.Add(time.Hour)},
		{ID: "receipt-bank-1", FlowID: flows[1].ID, Recipient: "payroll-provider", Region: "US", DataCategories: []string{"bank"}, ReceivedAt: from.Add(2 * time.Hour)},
	}
	return inventory.Inventory{
		Activities:  []inventory.ProcessingActivity{payments, bank},
		Flows:       flows,
		Occurrences: occurrences,
	}
}

// breachObligationDeadlines reads the registry's BREACH_NOTIFICATION
// deadlines by jurisdiction: the source of truth state-law decisions cite.
func breachObligationDeadlines(inv inventory.Inventory) map[string]int64 {
	out := map[string]int64{}
	for _, a := range inv.Activities {
		for _, o := range a.Obligations {
			if o.Kind == inventory.BreachNotification {
				out[o.Jurisdiction] = int64(o.DeadlineHours)
			}
		}
	}
	return out
}

// matrixFromRegistry builds the complete duty table with state-law
// deadlines and authorities taken from the registry's real breach
// obligations (authority cites the rule-pack release), and the remaining
// duties from their standing statutory grounds. The matrix version IS the
// rule-pack release, so an incident decided here is reproducible only
// against this registry generation.
func matrixFromRegistry(t *testing.T, inv inventory.Inventory) NotificationMatrix {
	t.Helper()
	deadlines := breachObligationDeadlines(inv)
	if len(deadlines) == 0 {
		t.Fatal("registry carries no breach obligations to build a matrix from")
	}
	rule := func(duty Duty, class BreachDataClass, threshold, deadline int64, authority string) MatrixRule {
		return MatrixRule{Duty: duty, Class: class, MinConsumers: threshold, DeadlineHours: deadline, Authority: authority}
	}
	var rules []MatrixRule
	for _, class := range AllBreachDataClasses() {
		rules = append(rules,
			rule(DutyFederal, class, 1, 72, "FEDERAL-BREACH-STATUTE"),
			rule(DutyStateBreachLaw, class, 1, 72, "STATE-BREACH-STATUTE"),
			rule(DutyGLBA, class, 1, 72, "15-U.S.C.-6801-GLBA"),
			rule(DutyTenantContract, class, 1, 24, "MSA-2026-SCHED-PRIVACY"),
		)
	}
	// Rules are jurisdiction-agnostic but the registry deadlines are not
	// (CA and NY differ above), so every state rule carries the most
	// urgent (minimum) registry deadline: notice is never later than any
	// covered jurisdiction requires.
	min := int64(0)
	for _, d := range deadlines {
		if min == 0 || d < min {
			min = d
		}
	}
	for i, r := range rules {
		if r.Duty == DutyStateBreachLaw {
			rules[i].DeadlineHours = min
			rules[i].Authority = "STATE-BREACH-STATUTE/us-2026.1"
		}
	}
	matrix, err := NewNotificationMatrix("us-2026.1", rules)
	if err != nil {
		t.Fatalf("NewNotificationMatrix (registry): %v", err)
	}
	return matrix
}
