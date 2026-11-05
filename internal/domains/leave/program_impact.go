// WF-CAP-008 leave provider: match the worker's current and proposed
// jurisdiction against caller-supplied leave programs and report gained
// and lost program coverage. Only programs effective on the revision's
// effective date count; a country-wide program covers every state in its
// country, a state program only its own state.
package leave

import (
	"fmt"
	"sort"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/people"
	"github.com/monstercameron/human-capital-management-suite/internal/governance/legal"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// Leave consequence kinds.
const (
	LeaveProgramsChanged = "LEAVE_PROGRAMS_CHANGED"
)

// ProgramImpactProvider answers the LEAVE domain of a change preview
// from caller-supplied program revisions.
type ProgramImpactProvider struct {
	Programs []LeaveProgramRevision
}

// Domain implements people.ImpactProvider.
func (p ProgramImpactProvider) Domain() people.ImpactDomain { return people.DomainLeave }

// Preview implements people.ImpactProvider.
func (p ProgramImpactProvider) Preview(rev people.ProposedRevision) ([]people.Consequence, error) {
	if err := rev.Validate(); err != nil {
		return nil, err
	}
	for _, program := range p.Programs {
		if err := program.Validate(); err != nil {
			return nil, err
		}
	}
	at, err := values.NewInstantFromUnix(rev.EffectiveDate.Unix(), 0)
	if err != nil {
		return nil, err
	}
	before := coveredPrograms(p.Programs, rev.CurrentJurisdiction, at)
	after := coveredPrograms(p.Programs, rev.ProposedJurisdiction, at)
	var gained, lost []string
	for _, id := range after {
		if !containsID(before, id) {
			gained = append(gained, id)
		}
	}
	for _, id := range before {
		if !containsID(after, id) {
			lost = append(lost, id)
		}
	}
	if len(gained) == 0 && len(lost) == 0 {
		return nil, nil
	}
	return []people.Consequence{{
		Domain: people.DomainLeave, Kind: LeaveProgramsChanged, Field: "worksite",
		Before: joinIDs(before), After: joinIDs(after),
		Summary:      fmt.Sprintf("leave programs gained [%s] and lost [%s]", joinIDs(gained), joinIDs(lost)),
		EvidenceRefs: programRefs(p.Programs, gained, lost),
	}}, nil
}

func coveredPrograms(programs []LeaveProgramRevision, jurisdiction legal.Jurisdiction, at values.Instant) []string {
	var out []string
	for _, program := range programs {
		ok, err := program.Effective.ContainsInstant(at)
		if err != nil || !ok {
			continue
		}
		if programCovers(program.Jurisdiction, jurisdiction) {
			out = append(out, program.ProgramID)
		}
	}
	sort.Strings(out)
	return out
}

func programCovers(program, worker legal.Jurisdiction) bool {
	if program.Country != worker.Country {
		return false
	}
	return program.State == "" || program.State == worker.State
}

func containsID(ids []string, id string) bool {
	for _, have := range ids {
		if have == id {
			return true
		}
	}
	return false
}

func joinIDs(ids []string) string {
	out := ""
	for i, id := range ids {
		if i > 0 {
			out += ","
		}
		out += id
	}
	return out
}

func programRefs(programs []LeaveProgramRevision, gained, lost []string) []string {
	byID := make(map[string]LeaveProgramRevision, len(programs))
	for _, program := range programs {
		byID[program.ProgramID] = program
	}
	var refs []string
	for _, id := range append(append([]string(nil), gained...), lost...) {
		refs = append(refs, fmt.Sprintf("program:%s@%d", id, byID[id].Revision))
	}
	sort.Strings(refs)
	return refs
}
