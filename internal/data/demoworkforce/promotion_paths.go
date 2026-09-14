package demoworkforce

import "sort"

// gradeRank orders every grade this company's staffing catalog uses, lowest
// first. It exists once, here, so the ladder below and any caller that needs
// to compare two grades read the same ranking rather than each inventing
// its own.
var gradeRank = map[string]int{
	"P2": 1, "P3": 2, "P4": 3, "P5": 4,
	"M2": 5, "M3": 6, "M4": 7,
	"E6": 8, "E7": 9,
}

// PromotionPathEdge is one deterministic career-ladder edge published among
// this demo company's own roles.
//
// An edge exists from a role to the lowest-ranked other role in the SAME
// organization unit that outranks it (ties broken by job code so the result
// is stable). A role that already holds the top grade published in its unit
// gets no edge at all: that is a structural absence of a next role, not an
// administrative refusal, and the two must stay distinguishable to whatever
// reads this catalog.
type PromotionPathEdge struct {
	OrgUnit                                  string
	SourceJobCode, SourceGrade               string
	TargetJobCode, TargetGrade, TargetTitle  string
	MinimumBaseIncrease, MaximumBaseIncrease string
}

// These illustrative HarborCare ladder bounds are authored as exact decimal
// fractions, not inferred from an employee's current pay or a target role's
// example salary. Compensation preflight still applies its separate checks.
const (
	demoMinimumBaseIncrease = "0.0500"
	demoMaximumBaseIncrease = "0.5000"
)

// PromotionPaths computes the demo company's career ladder from its own
// staffing catalog. It is pure and deterministic: no clock, no I/O, no
// randomness, so two calls in the same build always agree.
func PromotionPaths() []PromotionPathEdge {
	edges := make([]PromotionPathEdge, 0, len(staffing))
	for _, group := range staffing {
		seen := map[string]bool{}
		for _, role := range group.Roles {
			if seen[role.Code] {
				continue
			}
			seen[role.Code] = true
			sourceRank, ok := gradeRank[role.Grade]
			if !ok {
				continue
			}
			var best *Role
			for i := range group.Roles {
				candidate := group.Roles[i]
				if candidate.Code == role.Code {
					continue
				}
				candidateRank, ok := gradeRank[candidate.Grade]
				if !ok || candidateRank <= sourceRank {
					continue
				}
				if best == nil {
					copyOf := candidate
					best = &copyOf
					continue
				}
				bestRank := gradeRank[best.Grade]
				if candidateRank < bestRank || (candidateRank == bestRank && candidate.Code < best.Code) {
					copyOf := candidate
					best = &copyOf
				}
			}
			if best == nil {
				continue
			}
			edges = append(edges, PromotionPathEdge{
				OrgUnit:       group.Code,
				SourceJobCode: role.Code, SourceGrade: role.Grade,
				TargetJobCode: best.Code, TargetGrade: best.Grade, TargetTitle: best.Title,
				MinimumBaseIncrease: demoMinimumBaseIncrease, MaximumBaseIncrease: demoMaximumBaseIncrease,
			})
		}
	}
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].OrgUnit != edges[j].OrgUnit {
			return edges[i].OrgUnit < edges[j].OrgUnit
		}
		return edges[i].SourceJobCode < edges[j].SourceJobCode
	})
	return edges
}

// PayZones returns the distinct pay zones the demo company's locations use,
// sorted. It is exported so a caller publishing placements for the ladder
// above (which is zone-agnostic) can cross every target job/grade with every
// zone actually in use, without hard-coding the location table a second time.
func PayZones() []string {
	seen := make(map[string]bool, len(locations))
	zones := make([]string, 0, len(locations))
	for _, location := range locations {
		if !seen[location.Zone] {
			seen[location.Zone] = true
			zones = append(zones, location.Zone)
		}
	}
	sort.Strings(zones)
	return zones
}
