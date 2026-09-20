package app

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/journeyinvalidation"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
)

type recordingPublisher struct {
	records []journeyinvalidation.Committed
}

func (r *recordingPublisher) Publish(c journeyinvalidation.Committed) bool {
	r.records = append(r.records, c)
	return true
}

// A failed operation, a caller with no principal, or an engine composed
// without the pieces the post-commit allocation needs must publish nothing
// -- and must never panic on the business write's own return path.
func TestPublishCommittedPublishesNothingWithoutACommittedWrite(t *testing.T) {
	publisher := &recordingPublisher{}
	engine := &journeyEngine{invalidations: publisher}
	engine.publishCommitted(context.Background(), errors.New("refused"), "intent-1")
	engine.publishCommitted(context.Background(), nil, "intent-1") // no principal
	engine.publishTenantCommitted(context.Background(), "acme", "intent-1")
	var nilEngine *journeyEngine
	nilEngine.publishCommitted(context.Background(), nil, "intent-1")
	nilEngine.publishTenantCommitted(context.Background(), "acme", "intent-1")
	(&journeyEngine{}).publishTenantCommitted(context.Background(), "acme", "intent-1")
	if len(publisher.records) != 0 {
		t.Fatalf("published %+v without a committed write and an allocation path", publisher.records)
	}
}

func TestInterventionChangedOnlyForRecordedOutcomes(t *testing.T) {
	for outcome, want := range map[workspace.JourneyInterventionOutcome]bool{
		workspace.InterventionApplied:          true,
		workspace.InterventionPendingSafePoint: true,
		workspace.InterventionRepairRequired:   true,
		workspace.InterventionIndeterminate:    true,
		workspace.InterventionDenied:           false,
		workspace.InterventionTooLate:          false,
	} {
		if got := interventionChanged(outcome); got != want {
			t.Errorf("interventionChanged(%s) = %t, want %t", outcome, got, want)
		}
	}
}

func TestTransitionCounterIsAWireProjection(t *testing.T) {
	want, err := promotion.RegionJourneys.Projection()
	if err != nil || transitionCounterProjection != want {
		t.Fatalf("counter projection = %q, want %q (%v)", transitionCounterProjection, want, err)
	}
}
