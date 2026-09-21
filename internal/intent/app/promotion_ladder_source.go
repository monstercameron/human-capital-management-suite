package app

// The published career ladder, read from the tenant's own rows.
//
// The demo company's ladder was a compiled-in Go map: the options a client
// was offered, the gate that admitted a proposal and the catalog that opened
// vacancies all read the same process-local table, which no tenant owned and
// no operator could inspect. migration 00317 and
// internal/data/promotionladder made the ladder a tenant's own rows; this
// file is the served read of them.
//
// The authored ladder stays as the fallback, and deliberately so: a cell with
// no execution database, and a tenant whose ladder has not been seeded yet,
// keep exactly the catalog they had. What changes is that when rows exist,
// the rows are what the served catalog publishes.

import (
	"context"
	"fmt"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/demoworkforce"
	"github.com/monstercameron/human-capital-management-suite/internal/data/promotionladder"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// promotionLadderSource answers the published ladder a tenant's served
// catalog offers.
type promotionLadderSource struct {
	reader promotionladder.Reader
	// now is the business instant the effective-dated read is taken at. It
	// is the cell's own clock, so a pinned clock reads the ladder that was
	// published then rather than the one published now.
	now func() time.Time
}

// edges returns the tenant's published ladder.
//
// A source that reads no rows falls back to the authored ladder rather than
// publishing an empty catalog: "this deployment has not seeded its ladder" and
// "this worker has no next step" are different answers, and a client that
// cannot tell them apart shows a structural gap as an eligibility verdict.
func (s *promotionLadderSource) edges(ctx context.Context, tenant values.TenantId) ([]demoworkforce.PromotionPathEdge, error) {
	if s == nil {
		return demoworkforce.PromotionPaths(), nil
	}
	at := time.Now().UTC()
	if s.now != nil {
		at = s.now().UTC()
	}
	stored, err := s.reader.Edges(ctx, tenant, at)
	if err != nil {
		return nil, fmt.Errorf("app: journey: read the published promotion ladder: %w", err)
	}
	if len(stored) == 0 {
		return demoworkforce.PromotionPaths(), nil
	}
	edges := make([]demoworkforce.PromotionPathEdge, 0, len(stored))
	for _, edge := range stored {
		edges = append(edges, demoworkforce.PromotionPathEdge{
			OrgUnit: edge.OrgUnit, Kind: edge.Kind,
			SourceJobCode: edge.SourceJobCode, SourceGrade: edge.SourceGrade,
			TargetJobCode: edge.TargetJobCode, TargetGrade: edge.TargetGrade, TargetTitle: edge.TargetTitle,
			MinimumBaseIncrease: edge.MinimumBaseIncrease, MaximumBaseIncrease: edge.MaximumBaseIncrease,
		})
	}
	return edges, nil
}

// ladderEdges is the journey engine's own read of the published ladder. An
// engine composed with no ladder source publishes the authored one.
func (e *journeyEngine) ladderEdges(ctx context.Context, tenant values.TenantId) ([]demoworkforce.PromotionPathEdge, error) {
	return e.ladder.edges(ctx, tenant)
}
