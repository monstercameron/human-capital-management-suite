package productclient

import (
	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// projectPopulation copies the server's authorized journey summary
// (UXLIVE-027) into the product view. It converts; it never counts. A server
// that sent no summary yields nil, and the pages fall back to the admitted
// list they were given.
func projectPopulation(summary *journeyv1.JourneyPopulationSummary) *productui.JourneyPopulation {
	if summary == nil {
		return nil
	}
	out := &productui.JourneyPopulation{
		Total: int(summary.GetTotal()), Active: int(summary.GetActive()), Closed: int(summary.GetClosed()),
		NeedsAction: int(summary.GetNeedsAction()), Tracking: int(summary.GetTracking()), Exceptions: int(summary.GetExceptions()),
	}
	if stamp := summary.GetLatestUpdate(); stamp != nil && stamp.CheckValid() == nil {
		out.LatestUpdate = stamp.AsTime().UTC()
	}
	if stamp := summary.GetComputedAt(); stamp != nil && stamp.CheckValid() == nil {
		out.ComputedAt = stamp.AsTime().UTC()
	}
	for _, bucket := range summary.GetStages() {
		if bucket == nil {
			continue
		}
		out.Stages = append(out.Stages, productui.JourneyStageBucket{StatusKey: journeyStageKey(bucket.GetStage()), Count: int(bucket.GetCount())})
	}
	return out
}
