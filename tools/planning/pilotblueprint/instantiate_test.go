package pilotblueprint

import (
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/tools/planning/pilotjurisdiction"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/pilotprovider"
)

func mustLoadRealProvider(t *testing.T) pilotprovider.ProviderTopology {
	t.Helper()
	tp, err := pilotprovider.LoadTopology(providerTopologyPath)
	if err != nil {
		t.Fatalf("pilotprovider.LoadTopology: %v", err)
	}
	return *tp
}

func mustLoadRealJurisdiction(t *testing.T) pilotjurisdiction.JurisdictionProfile {
	t.Helper()
	p, err := pilotjurisdiction.LoadProfile(jurisdictionProfilePath)
	if err != nil {
		t.Fatalf("pilotjurisdiction.LoadProfile: %v", err)
	}
	return *p
}

// TestInstantiateAgainstRealRepositoryStateReportsBlockedNamingMissingCustomerAndProviderInputs
// is the load-bearing test the task names explicitly: instantiating the
// real, checked-in blueprint against today's real repository state - no
// design partner (the zero value of CustomerFacts), the real
// PLACEHOLDER_UNVERIFIED select-002-provider-topology.yaml, and the real
// select-001-jurisdiction-profile.yaml with its honestly empty
// reviewer.name - must report BLOCKED for every workstream and name the
// exact missing customer and provider inputs, never READY.
func TestInstantiateAgainstRealRepositoryStateReportsBlockedNamingMissingCustomerAndProviderInputs(t *testing.T) {
	bp := mustLoadBlueprint(t)
	if v := bp.Validate(); len(v) != 0 {
		t.Fatalf("checked-in blueprint must validate clean before instantiation, got: %v", v)
	}
	provider := mustLoadRealProvider(t)
	jurisdiction := mustLoadRealJurisdiction(t)

	// The accurate current fact: no design partner exists anywhere in this
	// repository (see doc.go). Constructing anything else here would be the
	// exact "assumed customer capability reports ready" defect RED names.
	noCustomer := CustomerFacts{}

	now := time.Date(2026, 9, 13, 0, 0, 0, 0, time.UTC)
	report := Instantiate(bp, noCustomer, provider, jurisdiction, now)

	if report.Overall != ReadinessBlocked {
		t.Fatalf("Overall = %s, want %s - instantiating against today's real facts must never report ready", report.Overall, ReadinessBlocked)
	}
	if len(report.Workstreams) != len(AllWorkstreamKinds()) {
		t.Fatalf("report has %d workstreams, want %d", len(report.Workstreams), len(AllWorkstreamKinds()))
	}

	byKind := map[WorkstreamKind]WorkstreamReadiness{}
	for _, w := range report.Workstreams {
		byKind[w.Kind] = w
	}

	// Every single workstream must be BLOCKED or UNKNOWN, never READY, and
	// every one must name the missing customer input by substance, not just
	// carry a bare status.
	for _, w := range report.Workstreams {
		if w.Status == ReadinessReady {
			t.Errorf("workstream %s reports READY against today's real facts - no design partner exists", w.Kind)
		}
		if len(w.Reasons) == 0 {
			t.Errorf("workstream %s reports %s with no named reasons", w.Kind, w.Status)
		}
		foundCustomerReason := false
		for _, r := range w.Reasons {
			if strings.Contains(r, "customer input missing") {
				foundCustomerReason = true
			}
		}
		if !foundCustomerReason {
			t.Errorf("workstream %s does not name a missing customer input: %v", w.Kind, w.Reasons)
		}
	}

	// A provider-dependent workstream must additionally name the exact
	// provider gate violations - not merely say "provider not ready".
	discovery, ok := byKind[WorkstreamDiscovery]
	if !ok {
		t.Fatal("report carries no DISCOVERY workstream")
	}
	if discovery.Status != ReadinessBlocked {
		t.Errorf("DISCOVERY status = %s, want %s", discovery.Status, ReadinessBlocked)
	}
	wantProviderFields := []string{"selection_status", "provider.vendor_id", "vendor_confirmation."}
	for _, want := range wantProviderFields {
		found := false
		for _, r := range discovery.Reasons {
			if strings.Contains(r, want) {
				found = true
			}
		}
		if !found {
			t.Errorf("DISCOVERY reasons do not name provider gate field %q: %v", want, discovery.Reasons)
		}
	}

	// The legal-review workstream must additionally name the unassigned
	// jurisdiction reviewer.
	legal, ok := byKind[WorkstreamLegalReview]
	if !ok {
		t.Fatal("report carries no LEGAL_REVIEW workstream")
	}
	foundReviewerReason := false
	for _, r := range legal.Reasons {
		if strings.Contains(r, "no named reviewer") {
			foundReviewerReason = true
		}
	}
	if !foundReviewerReason {
		t.Errorf("LEGAL_REVIEW reasons do not name the unassigned jurisdiction reviewer: %v", legal.Reasons)
	}

	// A workstream with no provider dependency (CONFIGURATION) must be
	// blocked purely on the customer input, not decorated with irrelevant
	// provider reasons.
	configuration, ok := byKind[WorkstreamConfiguration]
	if !ok {
		t.Fatal("report carries no CONFIGURATION workstream")
	}
	for _, r := range configuration.Reasons {
		if strings.Contains(r, "provider input missing") {
			t.Errorf("CONFIGURATION (no provider dependency) unexpectedly carries a provider reason: %v", configuration.Reasons)
		}
	}
}

// TestInstantiateReturnsUnknownForStaleCustomerConfirmation proves GREEN's
// UNKNOWN branch is real and distinct from BLOCKED: a customer owner who
// once confirmed readiness but whose confirmation has aged past the
// workstream's own expiry window is reported UNKNOWN (evidence exists but
// is stale), not BLOCKED (evidence never existed).
func TestInstantiateReturnsUnknownForStaleCustomerConfirmation(t *testing.T) {
	bp := validFixture()
	provider := realSelectionFixtureProvider()
	jurisdiction := reviewedFixtureJurisdiction()

	staleConfirmedAt := "2020-01-01T00:00:00Z" // long before any fixture's expiry window
	customer := CustomerFacts{
		DesignPartnerName: "Fixture Design Partner Inc.",
		WorkstreamOwners:  map[WorkstreamKind]OwnerConfirmation{},
	}
	for _, k := range AllWorkstreamKinds() {
		customer.WorkstreamOwners[k] = OwnerConfirmation{Name: "Fixture Owner", Contact: "fixture@example.test", ConfirmedAt: staleConfirmedAt}
	}

	now := time.Date(2026, 9, 13, 0, 0, 0, 0, time.UTC)
	report := Instantiate(bp, customer, provider, jurisdiction, now)

	if report.Overall != ReadinessUnknown {
		t.Fatalf("Overall = %s, want %s (stale, not missing)", report.Overall, ReadinessUnknown)
	}
	for _, w := range report.Workstreams {
		if w.Status != ReadinessUnknown {
			t.Errorf("workstream %s status = %s, want %s", w.Kind, w.Status, ReadinessUnknown)
		}
		found := false
		for _, r := range w.Reasons {
			if strings.Contains(r, "customer input stale") {
				found = true
			}
		}
		if !found {
			t.Errorf("workstream %s does not name a stale customer input: %v", w.Kind, w.Reasons)
		}
	}
}

// TestInstantiateReturnsReadyWhenEveryInputIsCurrentlyConfirmed proves the
// schema can support a genuinely ready pilot instantiation - the checked-in
// blueprint's permanent BLOCKED result is a fact about today's real inputs,
// not a hardcoded false Instantiate always returns.
func TestInstantiateReturnsReadyWhenEveryInputIsCurrentlyConfirmed(t *testing.T) {
	bp := validFixture()
	provider := realSelectionFixtureProvider()
	jurisdiction := reviewedFixtureJurisdiction()

	now := time.Date(2026, 9, 13, 0, 0, 0, 0, time.UTC)
	customer := CustomerFacts{
		DesignPartnerName: "Fixture Design Partner Inc.",
		WorkstreamOwners:  map[WorkstreamKind]OwnerConfirmation{},
	}
	for _, k := range AllWorkstreamKinds() {
		customer.WorkstreamOwners[k] = OwnerConfirmation{
			Name: "Fixture Owner", Contact: "fixture@example.test", ConfirmedAt: now.Add(-24 * time.Hour).Format(time.RFC3339),
		}
	}

	report := Instantiate(bp, customer, provider, jurisdiction, now)
	if report.Overall != ReadinessReady {
		t.Fatalf("Overall = %s, want %s, reasons: %+v", report.Overall, ReadinessReady, report.Workstreams)
	}
	for _, w := range report.Workstreams {
		if w.Status != ReadinessReady {
			t.Errorf("workstream %s status = %s, want %s (reasons: %v)", w.Kind, w.Status, ReadinessReady, w.Reasons)
		}
		if len(w.Reasons) != 0 {
			t.Errorf("workstream %s is READY but still carries reasons: %v", w.Kind, w.Reasons)
		}
	}
}

// TestInstantiateNamingAPartnerAloneIsNotEnough proves READY requires full
// value-level confirmation, not merely naming a design partner - the same
// property tools/planning/pilotprovider proves for
// SatisfiesRealProviderSelectionGate: a single flag/name is not itself
// evidence.
func TestInstantiateNamingAPartnerAloneIsNotEnough(t *testing.T) {
	bp := validFixture()
	provider := realSelectionFixtureProvider()
	jurisdiction := reviewedFixtureJurisdiction()
	now := time.Date(2026, 9, 13, 0, 0, 0, 0, time.UTC)

	customer := CustomerFacts{DesignPartnerName: "Fixture Design Partner Inc."} // no per-workstream owners confirmed

	report := Instantiate(bp, customer, provider, jurisdiction, now)
	if report.Overall == ReadinessReady {
		t.Fatal("naming a design partner alone, with no per-workstream owner confirmation, must not report READY")
	}
	for _, w := range report.Workstreams {
		if w.Status == ReadinessReady {
			t.Errorf("workstream %s reports READY with no confirmed owner", w.Kind)
		}
	}
}

// realSelectionFixtureProvider returns a ProviderTopology that satisfies
// SatisfiesRealProviderSelectionGate, isolating Instantiate's customer-side
// logic from the real checked-in placeholder topology's provider-side
// failure.
func realSelectionFixtureProvider() pilotprovider.ProviderTopology {
	tp := pilotprovider.ProviderTopology{
		SelectionStatus: pilotprovider.StatusVendorConfirmed,
		Provider:        pilotprovider.Provider{VendorID: "acme-hcm-production"},
		VendorConfirmation: pilotprovider.VendorConfirmation{
			ConfirmedByContact:  "Jane Procurement",
			ContractDocumentRef: "contracts/acme-hcm-msa-2027.pdf",
			SignedEffectiveDate: "2027-01-01",
		},
	}
	if ok, v := tp.SatisfiesRealProviderSelectionGate(); !ok {
		panic("realSelectionFixtureProvider must satisfy the real selection gate: " + v[0].String())
	}
	return tp
}

// reviewedFixtureJurisdiction returns a JurisdictionProfile with a named
// reviewer, isolating Instantiate's customer/provider-side logic from the
// real checked-in profile's honestly empty reviewer.
func reviewedFixtureJurisdiction() pilotjurisdiction.JurisdictionProfile {
	return pilotjurisdiction.JurisdictionProfile{
		Reviewer: pilotjurisdiction.Reviewer{
			Name: "Fixture Reviewer, Esq.", Qualification: "fixture qualification", AssignedDate: "2026-09-13",
		},
	}
}
