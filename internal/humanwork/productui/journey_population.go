package productui

import "time"

// JourneyPopulation is the client copy of the server's authorization-filtered
// journey summary (UXLIVE-027). The server computes it once over exactly the
// journeys it returned in the same response; summary pages read their totals
// here and never recount or re-filter View.Work to produce a number.
type JourneyPopulation struct {
	Total       int
	Active      int
	Closed      int
	NeedsAction int
	Tracking    int
	Exceptions  int
	// Stages is one bucket per stage present, keyed by the stage status key
	// the rest of the product localizes (journey.stage_*).
	Stages       []JourneyStageBucket
	LatestUpdate time.Time
	ComputedAt   time.Time
}

// JourneyStageBucket is one server-counted status bucket.
type JourneyStageBucket struct {
	StatusKey string
	Count     int
}

// journeyTotals is the one place a summary surface reads its journey totals.
// It is the server summary whenever one was read. The only exception is a
// per-record verdict map that addresses work: then presentation may hide
// journeys the server counted, and the totals follow the admitted
// population so a count can never include a record the viewer cannot open.
func journeyTotals(view View) (population JourneyPopulation, fromServer bool) {
	if view.JourneyPopulation != nil && !workVerdictsPresent(view) {
		return *view.JourneyPopulation, true
	}
	admitted := admittedWork(view)
	population.Total = len(admitted)
	population.Active, population.Closed = journeyCounts(admitted)
	for _, item := range admitted {
		if WorkNeedsViewerAction(item) {
			population.NeedsAction++
		}
		if !item.Terminal && WorkViewerInitiated(item) {
			population.Tracking++
		}
		if workIsException(item) {
			population.Exceptions++
		}
	}
	return population, false
}

// workIsException mirrors the server's exception buckets (BLOCKED, FAILED,
// REPAIR_REQUIRED) for listing the exception rows the summary counted.
func workIsException(item WorkItem) bool {
	switch item.StatusKey {
	case "journey.stage_blocked", "journey.stage_failed", "journey.stage_repair_required":
		return true
	}
	return false
}
