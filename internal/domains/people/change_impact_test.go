// TestTodo_WF_CAP_008 proves people.preview_change_impact returns typed
// jurisdiction, benefits and leave consequences for a proposed revision
// without effects.
package people

import (
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/governance/legal"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

type wfCap008Provider struct {
	domain       ImpactDomain
	consequences []Consequence
	err          error
	calls        int
	last         ProposedRevision
}

func (p *wfCap008Provider) Domain() ImpactDomain { return p.domain }

func (p *wfCap008Provider) Preview(rev ProposedRevision) ([]Consequence, error) {
	p.calls++
	p.last = rev
	if p.err != nil {
		return nil, p.err
	}
	return append([]Consequence(nil), p.consequences...), nil
}

func wfCap008Revision() ProposedRevision {
	return ProposedRevision{
		Tenant: "acme", WorkerRef: "worker-1",
		EffectiveDate:        time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
		CurrentAddress:       PostalAddress{CountryCode: "US", SubdivisionCode: "NY", PostalCode: "10001", Locality: "New York"},
		ProposedAddress:      PostalAddress{CountryCode: "US", SubdivisionCode: "CA", PostalCode: "94105", Locality: "San Francisco"},
		CurrentJurisdiction:  legal.Jurisdiction{Country: "US", State: "NY"},
		ProposedJurisdiction: legal.Jurisdiction{Country: "US", State: "CA"},
		PlanRef:              "plan-ppo",
		PopulationRef:        "population-ft",
		HoursPerWeek:         values.MustDecimal("40.00", 2, values.RoundingHalfUp),
		HoursSet:             true,
		Classification:       "FULL_TIME",
	}
}

func wfCap008Providers() []*wfCap008Provider {
	return []*wfCap008Provider{
		{domain: DomainLeave, consequences: []Consequence{
			{Domain: DomainLeave, Kind: "LEAVE_PROGRAMS_CHANGED", Field: "worksite", Before: "US-NY", After: "US-CA", Summary: "leave programs change", EvidenceRefs: []string{"program:ca-pfl"}},
		}},
		{domain: DomainBenefits, consequences: []Consequence{
			{Domain: DomainBenefits, Kind: "ELIGIBILITY_CHANGED", Field: "jurisdiction", Before: "ELIGIBLE", After: "CONDITIONAL", Summary: "benefits eligibility changes", EvidenceRefs: []string{"rule:ppo-ny"}},
			{Domain: DomainBenefits, Kind: "ELIGIBILITY_CHANGED", Field: "address", Before: "US-NY", After: "US-CA", Summary: "network changes", EvidenceRefs: []string{"rule:ppo-ny"}},
		}},
		{domain: DomainJurisdiction, consequences: []Consequence{
			{Domain: DomainJurisdiction, Kind: "WITHHOLDING_JURISDICTION_CHANGED", Field: "address", Before: "US-NY", After: "US-CA", Summary: "withholding moves", EvidenceRefs: []string{"release:2026.04"}},
		}},
	}
}

func wfCap008Sources(providers []*wfCap008Provider) []ImpactProvider {
	out := make([]ImpactProvider, 0, len(providers))
	for _, p := range providers {
		out = append(out, p)
	}
	return out
}

func TestTodo_WF_CAP_008(t *testing.T) {
	rev := wfCap008Revision()
	providers := wfCap008Providers()

	impact, err := PreviewChangeImpact(rev, wfCap008Sources(providers))
	if err != nil {
		t.Fatalf("PreviewChangeImpact: %v", err)
	}
	for _, p := range providers {
		if p.calls != 1 {
			t.Fatalf("provider %s called %d times, want once", p.domain, p.calls)
		}
	}
	if len(impact.Consequences) != 4 {
		t.Fatalf("impact has %d consequences, want 4: %+v", len(impact.Consequences), impact.Consequences)
	}
	// Deterministic order: domain rank, then kind, then field — independent
	// of provider registration order.
	want := []Consequence{
		{Domain: DomainJurisdiction, Kind: "WITHHOLDING_JURISDICTION_CHANGED", Field: "address"},
		{Domain: DomainBenefits, Kind: "ELIGIBILITY_CHANGED", Field: "address"},
		{Domain: DomainBenefits, Kind: "ELIGIBILITY_CHANGED", Field: "jurisdiction"},
		{Domain: DomainLeave, Kind: "LEAVE_PROGRAMS_CHANGED", Field: "worksite"},
	}
	for i, w := range want {
		got := impact.Consequences[i]
		if got.Domain != w.Domain || got.Kind != w.Kind || got.Field != w.Field {
			t.Fatalf("consequence %d is %s/%s/%s, want %s/%s/%s", i, got.Domain, got.Kind, got.Field, w.Domain, w.Kind, w.Field)
		}
		if got.Summary == "" || len(got.EvidenceRefs) == 0 {
			t.Fatalf("consequence %d lacks summary or evidence: %+v", i, got)
		}
	}
	if impact.RevisionDigest == "" || impact.RevisionDigest != rev.Digest() {
		t.Fatal("impact does not bind the revision it previewed")
	}
	if !impact.NotExecuted {
		t.Fatal("a preview must stay NOT_EXECUTED")
	}
	if err := impact.Verify(); err != nil {
		t.Fatalf("impact seal: %v", err)
	}
	if rev.Digest() != wfCap008Revision().Digest() {
		t.Fatal("preview mutated its input revision")
	}

	t.Run("no_effects_without_a_change", func(t *testing.T) {
		quiet := &wfCap008Provider{domain: DomainJurisdiction}
		impact, err := PreviewChangeImpact(wfCap008Revision(), []ImpactProvider{quiet})
		if err != nil {
			t.Fatalf("PreviewChangeImpact: %v", err)
		}
		if len(impact.Consequences) != 0 {
			t.Fatalf("quiet provider produced consequences: %+v", impact.Consequences)
		}
		if err := impact.Verify(); err != nil {
			t.Fatalf("empty impact seal: %v", err)
		}
	})

	t.Run("invalid_revisions_and_providers_fail_closed", func(t *testing.T) {
		bad := wfCap008Revision()
		bad.WorkerRef = ""
		if _, err := PreviewChangeImpact(bad, wfCap008Sources(providers)); !errors.Is(err, ErrInvalidRevision) {
			t.Fatalf("empty worker must fail closed, got %v", err)
		}
		bad = wfCap008Revision()
		bad.EffectiveDate = time.Time{}
		if _, err := PreviewChangeImpact(bad, wfCap008Sources(providers)); !errors.Is(err, ErrInvalidRevision) {
			t.Fatalf("missing effective date must fail closed, got %v", err)
		}
		bad = wfCap008Revision()
		bad.ProposedJurisdiction = legal.Jurisdiction{Country: "USA"}
		if _, err := PreviewChangeImpact(bad, wfCap008Sources(providers)); !errors.Is(err, ErrInvalidRevision) {
			t.Fatalf("invalid jurisdiction must fail closed, got %v", err)
		}
		dup := append(wfCap008Sources(providers), providers[0])
		if _, err := PreviewChangeImpact(wfCap008Revision(), dup); !errors.Is(err, ErrDuplicateProvider) {
			t.Fatalf("duplicate domain providers must fail closed, got %v", err)
		}
		boom := &wfCap008Provider{domain: DomainBenefits, err: errors.New("ratings unavailable")}
		if _, err := PreviewChangeImpact(wfCap008Revision(), []ImpactProvider{boom}); err == nil {
			t.Fatal("provider failure must fail the preview")
		}
		// A provider answering for another domain is refused.
		liar := &wfCap008Provider{domain: DomainLeave, consequences: []Consequence{
			{Domain: DomainBenefits, Kind: "ELIGIBILITY_CHANGED", Field: "jurisdiction", Summary: "x", EvidenceRefs: []string{"e"}},
		}}
		if _, err := PreviewChangeImpact(wfCap008Revision(), []ImpactProvider{liar}); !errors.Is(err, ErrInvalidConsequence) {
			t.Fatalf("cross-domain consequence must fail closed, got %v", err)
		}
	})
}

// TestTodo_WF_CAP_008_Golden pins the exact preview seal for one
// coast-to-coast move.
func TestTodo_WF_CAP_008_Golden(t *testing.T) {
	impact, err := PreviewChangeImpact(wfCap008Revision(), wfCap008Sources(wfCap008Providers()))
	if err != nil {
		t.Fatalf("PreviewChangeImpact: %v", err)
	}
	if impact.Digest != "sha256:716a9e07e7e5c7a04079c77985b6e32d12209390aa5509e29a4c146545bd0fdf" {
		t.Fatalf("preview seal changed: %s", impact.Digest)
	}
	if impact.RevisionDigest != wfCap008Revision().Digest() {
		t.Fatal("golden preview does not bind its revision")
	}
}
