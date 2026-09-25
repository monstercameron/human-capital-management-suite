package demoworkforce

// The published career ladder, as promotion_path_edge rows.
//
// [PromotionPaths] authors the ladder; this file projects it onto the
// storage shape internal/data/promotionladder records, binding each end to
// the job profile the architecture publishes for that job code so the two
// catalogs cannot drift apart.
//
// The path reference is the one the served catalog has always published
// ("demoworkforce:<unit>:<source>-><target>"), so an edge recorded here and
// an edge published from memory name the same thing.

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/promotionladder"
)

// PromotionPathRevision is the revision every edge of this authored ladder
// publishes under. It matches the revision the served catalog has always
// stamped on a demo edge.
const PromotionPathRevision = "1"

// PromotionPathRef is the published reference of one ladder edge.
func PromotionPathRef(orgUnit, sourceJobCode, targetJobCode string) string {
	return "demoworkforce:" + orgUnit + ":" + sourceJobCode + "->" + targetJobCode
}

// PromotionLadderEdges projects the authored ladder onto the published
// storage shape, in the same order [PromotionPaths] returns it, numbering
// each source's targets from one so the nearest step stays first when the
// ladder is read back.
func PromotionLadderEdges() []promotionladder.Edge { return HarborCarePack.PromotionLadderEdges() }

// PromotionLadderEdges projects this company's ladder onto storage rows.
func (p *Pack) PromotionLadderEdges() []promotionladder.Edge {
	authored := p.PromotionPaths()
	edges := make([]promotionladder.Edge, 0, len(authored))
	ordinals := make(map[string]int, len(authored))
	for _, edge := range authored {
		source := edge.OrgUnit + "|" + edge.SourceJobCode
		ordinals[source]++
		edges = append(edges, promotionladder.Edge{
			PathID:   PromotionPathRef(edge.OrgUnit, edge.SourceJobCode, edge.TargetJobCode),
			Revision: PromotionPathRevision,
			OrgUnit:  edge.OrgUnit, Kind: edge.Kind, Ordinal: ordinals[source],
			SourceProfileID: p.JobProfileID(edge.SourceJobCode),
			SourceJobCode:   edge.SourceJobCode, SourceGrade: edge.SourceGrade,
			TargetProfileID: p.JobProfileID(edge.TargetJobCode),
			TargetJobCode:   edge.TargetJobCode, TargetGrade: edge.TargetGrade, TargetTitle: edge.TargetTitle,
			MinimumBaseIncrease: edge.MinimumBaseIncrease, MaximumBaseIncrease: edge.MaximumBaseIncrease,
			Lifecycle: promotionladder.LifecyclePublished, PolicyVersion: p.ladderVersion,
		})
	}
	return edges
}

// SeedPromotionLadder records the authored ladder inside tx, which the caller
// owns and has already scoped to tenant. It is replay-safe: see
// [promotionladder.Seed].
func SeedPromotionLadder(ctx context.Context, tx dbport.Tx, tenant uuid.UUID, recordedAt time.Time) (promotionladder.Summary, error) {
	return HarborCarePack.SeedPromotionLadder(ctx, tx, tenant, recordedAt)
}

// SeedPromotionLadder records this company's ladder inside tx.
func (p *Pack) SeedPromotionLadder(ctx context.Context, tx dbport.Tx, tenant uuid.UUID, recordedAt time.Time) (promotionladder.Summary, error) {
	if tx == nil || tenant == uuid.Nil {
		return promotionladder.Summary{}, fmt.Errorf("demoworkforce: seed the promotion ladder: a transaction and tenant are required")
	}
	recorded := recordedAt.UTC()
	if recorded.IsZero() {
		return promotionladder.Summary{}, fmt.Errorf("demoworkforce: seed the promotion ladder: recorded_at is required")
	}
	return promotionladder.Seed(ctx, tx, tenant, p.PromotionLadderEdges(),
		CatalogEffectiveFrom, JobArchitectureKnownFrom, recorded)
}
