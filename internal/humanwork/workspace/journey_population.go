package workspace

import (
	"sort"
	"time"
)

// JourneyPopulation is the authorization-filtered journey summary projection
// (UXLIVE-027): the one set of totals, status buckets and freshness every
// summary surface reads. It is computed once, on the server, over exactly the
// journeys [JourneyEngine.ListJourneys] returned to the viewer, so a page
// that shows a count and a page that lists the journeys can never disagree
// about the population behind it.
type JourneyPopulation struct {
	// Total is every visible journey; Total == Active + Closed.
	Total  int
	Active int
	Closed int
	// NeedsAction counts open journeys whose next step the viewer holds.
	NeedsAction int
	// Tracking counts open journeys the viewer initiated, whoever holds the
	// next step: the population My Work's "tracked" filter lists.
	Tracking int
	// Exceptions counts journeys stopped on an exception: BLOCKED, FAILED or
	// REPAIR_REQUIRED.
	Exceptions int
	// Stages is one bucket per stage present, ordered by stage token.
	Stages []JourneyStageCount
	// LatestUpdate is the most recent durable change across the set; zero
	// when the set is empty.
	LatestUpdate time.Time
	// ComputedAt is when the summary was computed.
	ComputedAt time.Time
}

// JourneyStageCount is one status bucket.
type JourneyStageCount struct {
	Stage JourneyStage
	Count int
}

// JourneyStageIsException reports whether a stage is an exception a person
// must look at: the proposal is blocked, the run failed, or it needs repair.
func JourneyStageIsException(stage JourneyStage) bool {
	switch stage {
	case JourneyStageBlocked, JourneyStageFailed, JourneyStageRepairRequired:
		return true
	}
	return false
}

// SummarizeJourneys computes the population summary over one authorized
// journey list. Closure and responsibility are the viewer projection the
// engine already resolved for each journey (PROMOUX-012), never re-derived.
func SummarizeJourneys(journeys []JourneySummary, computedAt time.Time) JourneyPopulation {
	population := JourneyPopulation{Total: len(journeys), ComputedAt: computedAt.UTC()}
	buckets := map[JourneyStage]int{}
	for _, journey := range journeys {
		buckets[journey.Stage]++
		if journey.UpdatedAt.After(population.LatestUpdate) {
			population.LatestUpdate = journey.UpdatedAt.UTC()
		}
		if JourneyStageIsException(journey.Stage) {
			population.Exceptions++
		}
		if journey.Viewer.Closed {
			population.Closed++
			continue
		}
		population.Active++
		if journey.Viewer.Responsibility == JourneyResponsibilityActionRequired {
			population.NeedsAction++
		}
		for _, relationship := range journey.Viewer.Relationships {
			if relationship == JourneyViewerInitiator {
				population.Tracking++
				break
			}
		}
	}
	population.Stages = make([]JourneyStageCount, 0, len(buckets))
	for stage, count := range buckets {
		population.Stages = append(population.Stages, JourneyStageCount{Stage: stage, Count: count})
	}
	sort.Slice(population.Stages, func(i, j int) bool { return population.Stages[i].Stage < population.Stages[j].Stage })
	return population
}
