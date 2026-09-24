package performance_test

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/performance"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestTodo_REV_075_02_GraphStore(t *testing.T) {
	cycle, err := performance.NewPerformanceCycle(
		"review-cycle",
		performance.PopulationBindingRef{DefinitionID: "population", RevisionVersion: "1", Digest: "sha256:population"},
		performance.CalendarBindingRef{Ref: "calendar", Version: "1", Digest: "sha256:calendar"},
		performance.RatingScaleVersionRef{ID: "scale", Version: "1", Digest: "sha256:scale"},
	)
	if err != nil {
		t.Fatal(err)
	}
	store := performance.NewMemoryStore()
	tenant := values.TenantId("tenant-a")
	initialRow := performance.CycleRevision{CycleID: cycle.CycleID, Revision: cycle.Revision, State: cycle.State, CanonicalDigest: cycle.CanonicalDigest}
	if err := store.SaveCycle(context.Background(), tenant, initialRow); err != nil {
		t.Fatal(err)
	}
	opened, err := cycle.Open()
	if err != nil {
		t.Fatal(err)
	}
	cycleRow := performance.CycleRevision{CycleID: opened.CycleID, Revision: opened.Revision, State: opened.State, SupersedesRevision: cycle.Revision, CanonicalDigest: opened.CanonicalDigest}
	if err := store.SaveCycle(context.Background(), tenant, cycleRow); err != nil {
		t.Fatal(err)
	}
	graph, err := performance.FreezeParticipantReviewerGraph(opened,
		[]performance.ParticipantRef{{ID: "worker-1"}},
		[]performance.ReviewerAssignment{{ParticipantID: "worker-1", ReviewerID: "worker-manager", Relationship: performance.ReviewerRelationshipManager}},
		performance.DefaultReviewerGraphRules(), reviewInstant(t, "2026-09-01T00:00:00Z"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveParticipantReviewerGraph(context.Background(), tenant, graph); err != nil {
		t.Fatal(err)
	}
	inside, err := store.ListOpenParticipantReviewerGraphsForMember(context.Background(), tenant, "worker-manager")
	if err != nil || len(inside) != 1 || inside[0].Digest != graph.Digest || inside[0].Reviewers[0].ReviewerID != "worker-manager" {
		t.Fatalf("inside graph = %+v, %v", inside, err)
	}
	outside, err := store.ListOpenParticipantReviewerGraphsForMember(context.Background(), tenant, "outsider")
	if err != nil || len(outside) != 0 {
		t.Fatalf("outside graph = %+v, %v", outside, err)
	}
	otherTenant, err := store.ListOpenParticipantReviewerGraphsForMember(context.Background(), values.TenantId("tenant-b"), "worker-manager")
	if err != nil || len(otherTenant) != 0 {
		t.Fatalf("cross-tenant graph = %+v, %v", otherTenant, err)
	}
	if err := store.SaveParticipantReviewerGraph(context.Background(), tenant, graph); !errors.Is(err, performance.ErrDuplicateRevision) {
		t.Fatalf("duplicate graph revision = %v", err)
	}
}
