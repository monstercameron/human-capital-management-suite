// WF-CAP-008 jurisdiction provider: resolve the current and proposed
// addresses through the pinned reference dataset and report when the
// resolved withholding jurisdiction moves, is ambiguous, or cannot be
// resolved. Like every resolution result, consequences are descriptive
// candidates with provenance — not a legal or tax decision.
package location

import (
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/people"
)

// Jurisdiction consequence kinds.
const (
	JurisdictionChanged   = "WITHHOLDING_JURISDICTION_CHANGED"
	JurisdictionAmbiguous = "JURISDICTION_AMBIGUOUS"
	JurisdictionUnknown   = "JURISDICTION_UNKNOWN"
)

// JurisdictionImpactProvider answers the JURISDICTION domain of a change
// preview from one pinned resolution dataset.
type JurisdictionImpactProvider struct {
	Dataset ResolutionDataset
}

// Domain implements people.ImpactProvider.
func (p JurisdictionImpactProvider) Domain() people.ImpactDomain { return people.DomainJurisdiction }

// Preview implements people.ImpactProvider.
func (p JurisdictionImpactProvider) Preview(rev people.ProposedRevision) ([]people.Consequence, error) {
	if err := rev.Validate(); err != nil {
		return nil, err
	}
	current, err := ResolveAddress(ResolutionRequest{Address: postalToAddress(rev.CurrentAddress), Dataset: p.Dataset})
	if err != nil {
		return nil, err
	}
	proposed, err := ResolveAddress(ResolutionRequest{Address: postalToAddress(rev.ProposedAddress), Dataset: p.Dataset})
	if err != nil {
		return nil, err
	}
	release := "release:" + current.DatasetVersion
	// Decide on the jurisdiction candidates alone: the overall status
	// covers locality and timezone sets this provider does not read.
	if len(current.JurisdictionCandidates) == 0 || len(proposed.JurisdictionCandidates) == 0 {
		side := "current"
		if len(proposed.JurisdictionCandidates) == 0 {
			side = "proposed"
		}
		return []people.Consequence{{
			Domain: people.DomainJurisdiction, Kind: JurisdictionUnknown, Field: "address",
			Summary:      fmt.Sprintf("the %s address resolves to no jurisdiction in this dataset", side),
			EvidenceRefs: []string{release},
		}}, nil
	}
	if topJurisdictionCount(current) > 1 || topJurisdictionCount(proposed) > 1 {
		return []people.Consequence{{
			Domain: people.DomainJurisdiction, Kind: JurisdictionAmbiguous, Field: "address",
			Before:       topJurisdiction(current),
			After:        topJurisdiction(proposed),
			Summary:      "an address resolves to more than one jurisdiction with no winner",
			EvidenceRefs: []string{release},
		}}, nil
	}
	before, after := topJurisdiction(current), topJurisdiction(proposed)
	if before == after {
		return nil, nil
	}
	return []people.Consequence{{
		Domain: people.DomainJurisdiction, Kind: JurisdictionChanged, Field: "address",
		Before: before, After: after,
		Summary:      fmt.Sprintf("withholding jurisdiction moves from %s to %s", before, after),
		EvidenceRefs: []string{release},
	}}, nil
}

func postalToAddress(a people.PostalAddress) Address {
	return Address{
		Locality: a.Locality, PostalCode: a.PostalCode,
		CountryCode: a.CountryCode, SubdivisionCode: a.SubdivisionCode,
	}
}

func topJurisdiction(r LocationResolution) string {
	if len(r.JurisdictionCandidates) == 0 {
		return ""
	}
	return r.JurisdictionCandidates[0].Value
}

func topJurisdictionCount(r LocationResolution) int {
	if len(r.JurisdictionCandidates) == 0 {
		return 0
	}
	best := r.JurisdictionCandidates[0].Rank
	count := 0
	for _, c := range r.JurisdictionCandidates {
		if c.Rank == best {
			count++
		}
	}
	return count
}
