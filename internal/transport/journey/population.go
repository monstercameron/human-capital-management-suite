package journey

import (
	"sort"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
)

// toPopulation carries the engine-side journey population summary
// (UXLIVE-027) onto the wire. The buckets are ordered by the wire stage
// enum so every client reads them in one stable order.
func toPopulation(p workspace.JourneyPopulation) *journeyv1.JourneyPopulationSummary {
	out := &journeyv1.JourneyPopulationSummary{
		Total: int32(p.Total), Active: int32(p.Active), Closed: int32(p.Closed),
		NeedsAction: int32(p.NeedsAction), Tracking: int32(p.Tracking), Exceptions: int32(p.Exceptions),
		ComputedAt: toTimestamp(p.ComputedAt),
	}
	if !p.LatestUpdate.IsZero() {
		out.LatestUpdate = toTimestamp(p.LatestUpdate)
	}
	for _, bucket := range p.Stages {
		out.Stages = append(out.Stages, &journeyv1.JourneyStageCount{Stage: stageToProto(bucket.Stage), Count: int32(bucket.Count)})
	}
	sort.SliceStable(out.Stages, func(i, j int) bool { return out.Stages[i].GetStage() < out.Stages[j].GetStage() })
	return out
}
