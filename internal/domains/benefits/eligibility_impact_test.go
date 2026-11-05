// EligibilityImpactProvider answers WF-CAP-008's BENEFITS domain: the
// eligibility status before and after the jurisdiction move.
package benefits

import (
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/people"
	"github.com/monstercameron/human-capital-management-suite/internal/governance/legal"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func wfCap008EligibilityRules() []EligibilityRule {
	return []EligibilityRule{
		{
			ID: "ppo-elig", Version: "2026.1", Tenant: "acme",
			MinHoursPerWeek:        values.MustDecimal("30.00", 2, values.RoundingHalfUp),
			AllowedClassifications: []string{"FULL_TIME"},
			Jurisdictions:          []string{"US-NY"},
			EffectiveFrom:          time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			EffectiveTo:            time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC),
		},
	}
}

func wfCap008EligibilityRevision() people.ProposedRevision {
	return people.ProposedRevision{
		Tenant: "acme", WorkerRef: "worker-1",
		EffectiveDate:        time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
		CurrentAddress:       people.PostalAddress{CountryCode: "US", SubdivisionCode: "US-NY", PostalCode: "10001", Locality: "New York"},
		ProposedAddress:      people.PostalAddress{CountryCode: "US", SubdivisionCode: "US-CA", PostalCode: "94105", Locality: "San Francisco"},
		CurrentJurisdiction:  legal.Jurisdiction{Country: "US", State: "NY"},
		ProposedJurisdiction: legal.Jurisdiction{Country: "US", State: "CA"},
		PlanRef:              "plan-ppo",
		PopulationRef:        "population-ft",
		HoursPerWeek:         values.MustDecimal("40.00", 2, values.RoundingHalfUp),
		HoursSet:             true,
		Classification:       "FULL_TIME",
	}
}

func TestEligibilityImpactProvider(t *testing.T) {
	provider := EligibilityImpactProvider{Rules: wfCap008EligibilityRules()}
	if provider.Domain() != people.DomainBenefits {
		t.Fatalf("provider answers %s, want BENEFITS", provider.Domain())
	}

	moved, err := provider.Preview(wfCap008EligibilityRevision())
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	if len(moved) != 1 {
		t.Fatalf("move produced %d consequences, want 1: %+v", len(moved), moved)
	}
	c := moved[0]
	if c.Domain != people.DomainBenefits || c.Kind != EligibilityChanged {
		t.Fatalf("consequence is mistyped: %+v", c)
	}
	if c.Before != "ELIGIBLE" || c.After != "INELIGIBLE" {
		t.Fatalf("consequence misses the eligibility loss: %+v", c)
	}
	if c.Summary == "" || len(c.EvidenceRefs) == 0 {
		t.Fatalf("consequence lacks summary or evidence: %+v", c)
	}

	rev := wfCap008EligibilityRevision()
	rev.ProposedJurisdiction = rev.CurrentJurisdiction
	stayed, err := provider.Preview(rev)
	if err != nil {
		t.Fatalf("Preview same jurisdiction: %v", err)
	}
	if len(stayed) != 0 {
		t.Fatalf("unmoved jurisdiction produced consequences: %+v", stayed)
	}

	if _, err := provider.Preview(people.ProposedRevision{}); err == nil {
		t.Fatal("empty revision must fail closed")
	}
	empty := EligibilityImpactProvider{}
	if _, err := empty.Preview(wfCap008EligibilityRevision()); err == nil {
		t.Fatal("missing rules must fail closed")
	}
}
