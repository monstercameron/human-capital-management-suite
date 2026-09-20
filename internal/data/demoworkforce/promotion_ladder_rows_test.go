package demoworkforce

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/promotionladder"
)

var ladderSeedRecordedAt = time.Date(2026, time.September, 1, 13, 45, 0, 0, time.UTC)

// TestPromotionLadderEdgesProjectTheAuthoredLadder proves the storage
// projection is the authored ladder and nothing else: one edge per authored
// edge, both ends bound to the profile the architecture publishes for that
// job code, and the targets of one source numbered from one in the order the
// ladder offers them.
func TestPromotionLadderEdgesProjectTheAuthoredLadder(t *testing.T) {
	authored := PromotionPaths()
	edges := PromotionLadderEdges()
	if len(edges) != len(authored) {
		t.Fatalf("projected %d edges from %d authored ones", len(edges), len(authored))
	}
	profiles := map[string]bool{}
	architecture, err := JobArchitecture()
	if err != nil {
		t.Fatalf("JobArchitecture: %v", err)
	}
	for _, profile := range architecture.Profiles {
		profiles[profile.ProfileIDOrID()] = true
	}
	ordinals := map[string]int{}
	for i, edge := range edges {
		if err := edge.Validate(); err != nil {
			t.Fatalf("projected edge %d does not validate: %v", i, err)
		}
		source := authored[i]
		if edge.SourceJobCode != source.SourceJobCode || edge.TargetJobCode != source.TargetJobCode ||
			edge.OrgUnit != source.OrgUnit || edge.Kind != source.Kind {
			t.Fatalf("projected edge %d = %+v, want it to carry authored edge %+v", i, edge, source)
		}
		if edge.PathID != PromotionPathRef(source.OrgUnit, source.SourceJobCode, source.TargetJobCode) {
			t.Fatalf("projected edge %d publishes path ref %q", i, edge.PathID)
		}
		if !profiles[edge.SourceProfileID] || !profiles[edge.TargetProfileID] {
			t.Fatalf("projected edge %d names profiles the architecture does not publish: %s -> %s", i, edge.SourceProfileID, edge.TargetProfileID)
		}
		key := edge.OrgUnit + "|" + edge.SourceJobCode
		ordinals[key]++
		if edge.Ordinal != ordinals[key] {
			t.Fatalf("projected edge %d is ordinal %d, want %d for source %s", i, edge.Ordinal, ordinals[key], key)
		}
		if edge.Lifecycle != promotionladder.LifecyclePublished || edge.PolicyVersion != PromotionLadderVersion {
			t.Fatalf("projected edge %d publishes %q under %q", i, edge.Lifecycle, edge.PolicyVersion)
		}
	}
	multi := 0
	for _, count := range ordinals {
		if count > 1 {
			multi++
		}
	}
	if multi == 0 {
		t.Fatal("no source job publishes more than one target; the ordinal assertion above would pass vacuously")
	}
}

// TestSeedPromotionLadderLandsIsTenantScopedAndReplays proves the published
// ladder reaches migration 00317's table, that a second tenant reads none of
// it, and that a replay writes nothing.
func TestSeedPromotionLadderLandsIsTenantScopedAndReplays(t *testing.T) {
	db := pgtest.New(t)
	ctx := context.Background()
	tenantA := seedAggregateTenant(t, db)
	tenantB := seedAggregateTenant(t, db)
	edges := PromotionLadderEdges()

	var summary promotionladder.Summary
	inAggregateTx(t, db, tenantA, func(tx dbport.Tx) error {
		var seedErr error
		summary, seedErr = SeedPromotionLadder(ctx, tx, tenantA, ladderSeedRecordedAt)
		return seedErr
	})
	if summary.Inserted != len(edges) || summary.Skipped != 0 {
		t.Fatalf("seed summary = %+v, want %d inserts and no skips", summary, len(edges))
	}

	var rows int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM promotion_path_edge WHERE tenant_id = $1`, tenantA).Scan(&rows); err != nil {
		t.Fatalf("count seeded edges: %v", err)
	}
	if rows != len(edges) {
		t.Fatalf("promotion_path_edge holds %d rows, want %d", rows, len(edges))
	}
	var otherRows int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM promotion_path_edge WHERE tenant_id = $1`, tenantB).Scan(&otherRows); err != nil {
		t.Fatalf("count the other tenant's edges: %v", err)
	}
	if otherRows != 0 {
		t.Fatalf("promotion_path_edge holds %d rows for the untouched tenant, want 0", otherRows)
	}

	var replay promotionladder.Summary
	inAggregateTx(t, db, tenantA, func(tx dbport.Tx) error {
		var seedErr error
		replay, seedErr = SeedPromotionLadder(ctx, tx, tenantA, ladderSeedRecordedAt)
		return seedErr
	})
	if replay.Inserted != 0 || replay.Skipped != len(edges) {
		t.Fatalf("replay summary = %+v, want zero inserts and %d skips", replay, len(edges))
	}
	var afterReplay int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM promotion_path_edge WHERE tenant_id = $1`, tenantA).Scan(&afterReplay); err != nil {
		t.Fatalf("count edges after the replay: %v", err)
	}
	if afterReplay != len(edges) {
		t.Fatalf("the replay left %d rows, want the original %d", afterReplay, len(edges))
	}
}

// TestSeedPromotionLadderRefusesAnIncompleteCall proves the seeder refuses a
// call it could not record correctly rather than reporting a silent success.
func TestSeedPromotionLadderRefusesAnIncompleteCall(t *testing.T) {
	ctx := context.Background()
	if _, err := SeedPromotionLadder(ctx, nil, uuid.New(), ladderSeedRecordedAt); err == nil {
		t.Error("seeding without a transaction was accepted")
	}
}
