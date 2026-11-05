// ProgramImpactProvider answers WF-CAP-008's LEAVE domain: the programs
// covering the worker before and after the jurisdiction move.
package leave

import (
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/evidence"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/people"
	"github.com/monstercameron/human-capital-management-suite/internal/governance/legal"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func wfCap008Programs(t *testing.T) []LeaveProgramRevision {
	t.Helper()
	start := values.NewInstant(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	end := values.NewInstant(time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC))
	iv, err := values.NewInstantInterval(start, end)
	if err != nil {
		t.Fatalf("interval: %v", err)
	}
	program := func(id string, jurisdiction legal.Jurisdiction, revision uint64) LeaveProgramRevision {
		return LeaveProgramRevision{ProgramID: id, Revision: revision, Name: id, Kind: StatutoryProgram, Sponsor: "policy-source",
			Authority:    evidence.SourceAuthority{Kind: evidence.AuthorityLocal, System: "policy-registry", PolicyRef: "policy:leave"},
			Jurisdiction: jurisdiction, Scope: []string{"worker"}, EligibilityRuleRefs: []string{"eligibility:" + id},
			Effective: iv, Protected: true, Paid: true, PayTreatment: "wage-replacement",
			EvidenceRequirements: []string{"certification"}, NoticeRequirements: []string{"worker-notice"},
			Interaction:   InteractionMetadata{ConcurrencyGroup: id, Priority: 1, Stacking: false, OffsetAllowed: true, MostProtective: true, ConflictPolicy: "MOST_PROTECTIVE"},
			BalanceSource: "leave-ledger", ReturnObligations: []string{"readiness-review"}, UnknownPolicy: UnknownReview}
	}
	return []LeaveProgramRevision{
		program("ny-pfl", legal.Jurisdiction{Country: "US", State: "NY"}, 3),
		program("ca-pfl", legal.Jurisdiction{Country: "US", State: "CA"}, 5),
		program("us-fmla", legal.Jurisdiction{Country: "US"}, 9),
	}
}

func wfCap008LeaveRevision() people.ProposedRevision {
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

func TestProgramImpactProvider(t *testing.T) {
	provider := ProgramImpactProvider{Programs: wfCap008Programs(t)}
	if provider.Domain() != people.DomainLeave {
		t.Fatalf("provider answers %s, want LEAVE", provider.Domain())
	}

	moved, err := provider.Preview(wfCap008LeaveRevision())
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	if len(moved) != 1 {
		t.Fatalf("move produced %d consequences, want 1: %+v", len(moved), moved)
	}
	c := moved[0]
	if c.Domain != people.DomainLeave || c.Kind != LeaveProgramsChanged {
		t.Fatalf("consequence is mistyped: %+v", c)
	}
	if c.Before != "ny-pfl,us-fmla" || c.After != "ca-pfl,us-fmla" {
		t.Fatalf("consequence misses the program swap: %+v", c)
	}
	if len(c.EvidenceRefs) != 2 || c.EvidenceRefs[0] != "program:ca-pfl@5" || c.EvidenceRefs[1] != "program:ny-pfl@3" {
		t.Fatalf("consequence evidence is wrong: %+v", c.EvidenceRefs)
	}

	rev := wfCap008LeaveRevision()
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
	broken := ProgramImpactProvider{Programs: []LeaveProgramRevision{{}}}
	if _, err := broken.Preview(wfCap008LeaveRevision()); err == nil {
		t.Fatal("invalid programs must fail closed")
	}
}
