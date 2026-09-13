package pilotblueprint

import (
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/tools/planning/pilotjurisdiction"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/pilotprovider"
)

// OwnerConfirmation names the human contact and confirmation date that
// actually confirmed one workstream's customer-side ownership. It is
// deliberately empty on any tenant for whom no design partner exists yet -
// see doc.go - exactly as tools/planning/pilotjurisdiction.Reviewer and
// tools/planning/pilotprovider.VendorConfirmation are deliberately empty
// pending a real review or a real vendor.
type OwnerConfirmation struct {
	Name        string
	Contact     string
	ConfirmedAt string // RFC3339; empty means unconfirmed.
}

// confirmed reports whether every field of c is populated.
func (c OwnerConfirmation) confirmed() bool {
	return strings.TrimSpace(c.Name) != "" && strings.TrimSpace(c.Contact) != "" && strings.TrimSpace(c.ConfirmedAt) != ""
}

// CustomerFacts is the tenant-supplied input [Instantiate] reads to assess
// one design partner's pilot readiness. It is never embedded in a Blueprint
// document (see doc.go's REFACTOR discussion): the same versioned Blueprint
// template is instantiated once per tenant by pairing it with that tenant's
// own CustomerFacts, tools/planning/pilotprovider.ProviderTopology and
// tools/planning/pilotjurisdiction.JurisdictionProfile, and none of those
// tenant-specific values can rewrite the Blueprint's own workstream fields.
//
// No design partner has been selected anywhere in this repository (see
// doc.go). The zero value of CustomerFacts - every field empty - is
// therefore not a placeholder value this package invented; it is the
// accurate, current fact. Fabricating a name here would be exactly the
// "assumed customer capability reports ready" defect RED names.
type CustomerFacts struct {
	// DesignPartnerName is empty until a real design partner is under
	// contract. Every workstream's customer dependency is BLOCKED while
	// this is empty, regardless of WorkstreamOwners.
	DesignPartnerName string
	// WorkstreamOwners names, per workstream, the customer-side contact who
	// has confirmed that workstream's readiness. A missing entry, or one
	// [OwnerConfirmation.confirmed] reports false for, is a missing customer
	// input for that workstream.
	WorkstreamOwners map[WorkstreamKind]OwnerConfirmation
}

func (c CustomerFacts) ownerFor(k WorkstreamKind) (OwnerConfirmation, bool) {
	if c.WorkstreamOwners == nil {
		return OwnerConfirmation{}, false
	}
	o, ok := c.WorkstreamOwners[k]
	return o, ok
}

// ReadinessStatus is the closed vocabulary GREEN names verbatim for a
// workstream's (or the whole blueprint's) instantiated readiness.
type ReadinessStatus string

// The exact, closed set of readiness statuses, ranked worst-to-best by
// [worseStatus]: BLOCKED (missing input) outranks UNKNOWN (stale input)
// outranks READY.
const (
	ReadinessBlocked ReadinessStatus = "BLOCKED"
	ReadinessUnknown ReadinessStatus = "UNKNOWN"
	ReadinessReady   ReadinessStatus = "READY"
)

func statusRank(s ReadinessStatus) int {
	switch s {
	case ReadinessBlocked:
		return 2
	case ReadinessUnknown:
		return 1
	default:
		return 0
	}
}

func worseStatus(a, b ReadinessStatus) ReadinessStatus {
	if statusRank(b) > statusRank(a) {
		return b
	}
	return a
}

// WorkstreamReadiness is one workstream's instantiated readiness. It carries
// no field from the Blueprint's own semantic contract (no AcceptanceOracle,
// DataProcessingBoundary, Escalation or Fallback text) - only the
// workstream's identity, the computed status, and the free-text reasons that
// justify it (REFACTOR: a tenant's facts can influence Status and Reasons,
// never the contract itself).
type WorkstreamReadiness struct {
	Kind    WorkstreamKind
	Status  ReadinessStatus
	Reasons []string
}

// ReadinessReport is [Instantiate]'s result: one tenant's assessed readiness
// against a Blueprint template, discovery through hypercare.
type ReadinessReport struct {
	GeneratedAt string
	Overall     ReadinessStatus
	Workstreams []WorkstreamReadiness
}

// Instantiate assesses tenant readiness against bp without ever mutating
// bp (REFACTOR; proved by refactor_test.go's digest-invariance check). For
// every workstream in bp.Workstreams, in order, it checks:
//
//   - the customer dependency: BLOCKED if customer.DesignPartnerName is
//     empty or no confirmed owner is recorded for that workstream; UNKNOWN
//     if the owner is confirmed but the confirmation is older than the
//     workstream's Timing.ExpiryOffsetDays as of now;
//   - the provider dependency, for any workstream naming one: BLOCKED
//     unless provider.SatisfiesRealProviderSelectionGate() reports true, by
//     value, exactly as tools/planning/pilotprovider's own real-selection
//     gate is checked - a topology cannot satisfy this by flipping
//     SelectionStatus alone, and neither can Instantiate be fooled by it;
//   - the legal-review workstream's jurisdiction dependency: BLOCKED while
//     jurisdiction.Reviewer.Name is empty, exactly what
//     tools/planning/pilotjurisdiction's own checked-in profile reports
//     today.
//
// The overall report status is the worst (BLOCKED > UNKNOWN > READY) status
// across every workstream. GREEN requires this function return
// UNKNOWN|BLOCKED for missing or stale customer/provider inputs; it can
// never report READY except when every workstream's own checks above pass
// against real, confirmed, unexpired values - there is no separate flag
// this function trusts instead of recomputing them.
func Instantiate(
	bp Blueprint,
	customer CustomerFacts,
	provider pilotprovider.ProviderTopology,
	jurisdiction pilotjurisdiction.JurisdictionProfile,
	now time.Time,
) ReadinessReport {
	report := ReadinessReport{GeneratedAt: now.UTC().Format(time.RFC3339), Overall: ReadinessReady}

	providerSatisfies, providerViolations := provider.SatisfiesRealProviderSelectionGate()

	for _, w := range bp.Workstreams {
		status := ReadinessReady
		var reasons []string

		if strings.TrimSpace(customer.DesignPartnerName) == "" {
			status = worseStatus(status, ReadinessBlocked)
			reasons = append(reasons, "customer input missing: no design partner has been selected anywhere in this repository (WEDGE-001 selected a problem, not a partner)")
		} else if owner, ok := customer.ownerFor(w.Kind); !ok || !owner.confirmed() {
			status = worseStatus(status, ReadinessBlocked)
			reasons = append(reasons, fmt.Sprintf("customer input missing: no named, contactable, dated customer owner confirmed for workstream %s", w.Kind))
		} else {
			confirmedAt, err := time.Parse(time.RFC3339, owner.ConfirmedAt)
			if err != nil {
				status = worseStatus(status, ReadinessBlocked)
				reasons = append(reasons, fmt.Sprintf("customer input malformed: owner confirmed_at %q for workstream %s is not RFC3339", owner.ConfirmedAt, w.Kind))
			} else if now.Sub(confirmedAt) > time.Duration(w.Timing.ExpiryOffsetDays)*24*time.Hour {
				status = worseStatus(status, ReadinessUnknown)
				reasons = append(reasons, fmt.Sprintf("customer input stale: owner confirmation for workstream %s is older than its %d-day expiry window", w.Kind, w.Timing.ExpiryOffsetDays))
			}
		}

		if w.hasPartyDependency(PartyProvider) && !providerSatisfies {
			status = worseStatus(status, ReadinessBlocked)
			for _, v := range providerViolations {
				reasons = append(reasons, fmt.Sprintf("provider input missing: %s", v.String()))
			}
		}

		if w.Kind == WorkstreamLegalReview && strings.TrimSpace(jurisdiction.Reviewer.Name) == "" {
			status = worseStatus(status, ReadinessBlocked)
			reasons = append(reasons, "provider/legal input missing: the pinned SELECT-001 jurisdiction profile has no named reviewer (reviewer.name is missing)")
		}

		report.Workstreams = append(report.Workstreams, WorkstreamReadiness{
			Kind: w.Kind, Status: status, Reasons: reasons,
		})
		report.Overall = worseStatus(report.Overall, status)
	}
	if len(bp.Workstreams) == 0 {
		report.Overall = ReadinessBlocked
	}

	return report
}
