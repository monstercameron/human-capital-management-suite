// WF-CAP-008 benefits provider: evaluate the worker snapshot before and
// after the jurisdiction move against the versioned eligibility rules and
// report every status transition. Evaluation itself is pure, so the
// preview inherits the no-effects guarantee.
package benefits

import (
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/people"
)

// Benefits consequence kinds.
const (
	EligibilityChanged = "ELIGIBILITY_CHANGED"
)

// EligibilityImpactProvider answers the BENEFITS domain of a change
// preview from caller-supplied eligibility rules.
type EligibilityImpactProvider struct {
	Rules []EligibilityRule
}

// Domain implements people.ImpactProvider.
func (p EligibilityImpactProvider) Domain() people.ImpactDomain { return people.DomainBenefits }

// Preview implements people.ImpactProvider.
func (p EligibilityImpactProvider) Preview(rev people.ProposedRevision) ([]people.Consequence, error) {
	if err := rev.Validate(); err != nil {
		return nil, err
	}
	before, err := EvaluateEligibility(p.facts(rev, rev.CurrentJurisdiction.String()), p.Rules)
	if err != nil {
		return nil, err
	}
	after, err := EvaluateEligibility(p.facts(rev, rev.ProposedJurisdiction.String()), p.Rules)
	if err != nil {
		return nil, err
	}
	if before.Status == after.Status {
		return nil, nil
	}
	rule := before.RuleID + "@" + before.RuleVersion
	if after.RuleID != "" {
		rule = after.RuleID + "@" + after.RuleVersion
	}
	return []people.Consequence{{
		Domain: people.DomainBenefits, Kind: EligibilityChanged, Field: "jurisdiction",
		Before: string(before.Status), After: string(after.Status),
		Summary: fmt.Sprintf("benefits eligibility moves from %s to %s", before.Status, after.Status),
		EvidenceRefs: []string{
			"rule:" + rule,
			"before:" + before.Digest,
			"after:" + after.Digest,
		},
	}}, nil
}

func (p EligibilityImpactProvider) facts(rev people.ProposedRevision, jurisdiction string) WorkerFacts {
	return WorkerFacts{
		Tenant: rev.Tenant, WorkerRef: rev.WorkerRef,
		PlanRef: rev.PlanRef, PopulationRef: rev.PopulationRef,
		AsOf:           rev.EffectiveDate,
		HoursPerWeek:   rev.HoursPerWeek,
		HoursSet:       rev.HoursSet,
		Classification: rev.Classification,
		Jurisdiction:   jurisdiction,
	}
}
