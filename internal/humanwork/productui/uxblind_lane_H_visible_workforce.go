package productui

import "strings"

// visibleWorkforceSummary is the one presentation projection used for the
// Organization scope facts. A valid canonical graph owns the unit count,
// including authorized units without current members; without that graph the
// admitted worker projection owns the fallback count of represented units.
type visibleWorkforceCounts struct {
	Workforce int
	Units     int
}

func visibleWorkforceSummary(population []Person, graphBacked bool, graph organizationGraphProjection) visibleWorkforceCounts {
	summary := visibleWorkforceCounts{Workforce: len(population)}
	if graphBacked {
		if graph.Err == nil {
			summary.Units = len(graph.Units)
		}
		return summary
	}
	seen := make(map[string]bool, len(population))
	for _, person := range population {
		unit := strings.ToLower(strings.TrimSpace(person.Team))
		if unit != "" {
			seen[unit] = true
		}
	}
	summary.Units = len(seen)
	return summary
}
