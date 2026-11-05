// JurisdictionImpactProvider answers WF-CAP-008's JURISDICTION domain:
// the resolved withholding jurisdiction before and after the move.
package location

import (
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/people"
	"github.com/monstercameron/human-capital-management-suite/internal/governance/legal"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func wfCap008Dataset(t *testing.T) ResolutionDataset {
	t.Helper()
	table, err := NewJurisdictionTable(JurisdictionTable{TableID: "jurisdiction", Version: "2026.1", Rules: []JurisdictionRule{
		{CountryCode: "US", SubdivisionCode: "US-NY", Locality: "New York", FederalTax: "US-FED-TAX", StateTax: "US-NY-TAX", LocalTax: "US-NYC-TAX", FederalLabor: "US-FED-LABOR", StateLabor: "US-NY-LABOR", LocalLabor: "US-NYC-LABOR"},
		{CountryCode: "US", SubdivisionCode: "US-CA", Locality: "San Francisco", FederalTax: "US-FED-TAX", StateTax: "US-CA-TAX", LocalTax: "US-SF-TAX", FederalLabor: "US-FED-LABOR", StateLabor: "US-CA-LABOR", LocalLabor: "US-SF-LABOR"},
	}})
	if err != nil {
		t.Fatalf("jurisdiction table: %v", err)
	}
	return ResolutionDataset{DatasetID: "location-reference", Version: "2026.1", TZDBVersion: "2026a", JurisdictionTable: table}
}

func wfCap008Revision() people.ProposedRevision {
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

func TestJurisdictionImpactProvider(t *testing.T) {
	provider := JurisdictionImpactProvider{Dataset: wfCap008Dataset(t)}
	if provider.Domain() != people.DomainJurisdiction {
		t.Fatalf("provider answers %s, want JURISDICTION", provider.Domain())
	}

	moved, err := provider.Preview(wfCap008Revision())
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	if len(moved) != 1 {
		t.Fatalf("move produced %d consequences, want 1: %+v", len(moved), moved)
	}
	c := moved[0]
	if c.Domain != people.DomainJurisdiction || c.Kind != JurisdictionChanged || c.Field != "address" {
		t.Fatalf("consequence is mistyped: %+v", c)
	}
	if c.Before == "" || c.After == "" || c.Before == c.After {
		t.Fatalf("consequence names no move: %+v", c)
	}
	if c.Summary == "" || len(c.EvidenceRefs) == 0 {
		t.Fatalf("consequence lacks summary or evidence: %+v", c)
	}

	rev := wfCap008Revision()
	rev.ProposedAddress = rev.CurrentAddress
	stayed, err := provider.Preview(rev)
	if err != nil {
		t.Fatalf("Preview same address: %v", err)
	}
	if len(stayed) != 0 {
		t.Fatalf("unmoved address produced consequences: %+v", stayed)
	}

	rev = wfCap008Revision()
	rev.ProposedAddress = people.PostalAddress{CountryCode: "CA", SubdivisionCode: "CA-ON", PostalCode: "M5H", Locality: "Toronto"}
	unknown, err := provider.Preview(rev)
	if err != nil {
		t.Fatalf("Preview unknown: %v", err)
	}
	if len(unknown) != 1 || unknown[0].Kind != JurisdictionUnknown {
		t.Fatalf("unresolvable address must report UNKNOWN: %+v", unknown)
	}

	tied, err := NewJurisdictionTable(JurisdictionTable{TableID: "jurisdiction", Version: "2026.1", Rules: []JurisdictionRule{
		{CountryCode: "US", SubdivisionCode: "US-NY", FederalTax: "US-FED-TAX", StateTax: "US-NY-TAX", LocalTax: "US-NYC-TAX", FederalLabor: "US-FED-LABOR", StateLabor: "US-NY-LABOR", LocalLabor: "US-NYC-LABOR"},
		{CountryCode: "US", Locality: "New York", FederalTax: "US-FED-TAX", StateTax: "US-NY-TAX", LocalTax: "US-NYC-TAX", FederalLabor: "US-FED-LABOR", StateLabor: "US-NY-LABOR", LocalLabor: "US-NYC-LABOR"},
		{CountryCode: "US", SubdivisionCode: "US-CA", Locality: "San Francisco", FederalTax: "US-FED-TAX", StateTax: "US-CA-TAX", LocalTax: "US-SF-TAX", FederalLabor: "US-FED-LABOR", StateLabor: "US-CA-LABOR", LocalLabor: "US-SF-LABOR"},
	}})
	if err != nil {
		t.Fatalf("tied table: %v", err)
	}
	tiedProvider := JurisdictionImpactProvider{Dataset: ResolutionDataset{DatasetID: "location-reference", Version: "2026.1", TZDBVersion: "2026a", JurisdictionTable: tied}}
	ambiguous, err := tiedProvider.Preview(wfCap008Revision())
	if err != nil {
		t.Fatalf("Preview tied: %v", err)
	}
	if len(ambiguous) != 1 || ambiguous[0].Kind != JurisdictionAmbiguous {
		t.Fatalf("tied candidates must report AMBIGUOUS: %+v", ambiguous)
	}

	if _, err := provider.Preview(people.ProposedRevision{}); err == nil {
		t.Fatal("empty revision must fail closed")
	}
	broken := JurisdictionImpactProvider{}
	if _, err := broken.Preview(wfCap008Revision()); err == nil {
		t.Fatal("missing dataset must fail closed")
	}
}
