package performance_test

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/performance"
)

// topProposed calibrates a 5.00 proposed rating through the real review
// chain: two 5-point reviews weighted 2-to-1.
func topProposed(t *testing.T) (performance.FrozenParticipantReviewerGraph, performance.ProposedRating) {
	t.Helper()
	cycle, graph := reviewGraph(t)
	collection, err := performance.NewReviewCollection(graph, reviewInstant(t, "2026-10-01T00:00:00Z"), performance.ReviewCollectionPolicy{RatingScale: cycle.RatingScale})
	if err != nil {
		t.Fatalf("collection: %v", err)
	}
	for _, item := range []struct{ reviewer, rating string }{{"manager-1", "5"}, {"peer-1", "5"}} {
		review := reviewFor(t, collection, item.reviewer, "participant-1", performance.ReviewKindReview, 1, 0, "2026-09-05T12:00:00Z")
		review.Rating = item.rating
		review.ID = item.reviewer + "-top-rating"
		var collectErr error
		collection, collectErr = collection.Submit(review)
		if collectErr != nil {
			t.Fatalf("submit %s: %v", item.reviewer, collectErr)
		}
	}
	proposed, err := performance.CalculateProposedRating(collection, "participant-1", proposedRule(t, 2, performance.OutlierHandlingNone))
	if err != nil {
		t.Fatalf("proposed: %v", err)
	}
	return graph, proposed
}

func finalizeOne(t *testing.T, graph performance.FrozenParticipantReviewerGraph, proposed performance.ProposedRating) performance.CalibratedRating {
	t.Helper()
	session, err := performance.NewCalibrationSession("calibration-1", graph, []performance.ProposedRating{proposed}, "facilitator-1", []string{"manager-1", "peer-1"}, calibrationRule(t))
	if err != nil {
		t.Fatalf("session: %v", err)
	}
	final, err := session.FinalizeParticipant("participant-1")
	if err != nil {
		t.Fatalf("finalize: %v", err)
	}
	return final
}

// TestHighPerformerTopBand proves the top-band predicate: a valid 5.00
// calibration is a high performer, a valid 3.33 is not, the 4.50 boundary
// is scale-independent, and a tampered rating is not.
func TestHighPerformerTopBand(t *testing.T) {
	graph, proposed := topProposed(t)
	top := finalizeOne(t, graph, proposed)
	if top.FinalRating.String() != "5.00" {
		t.Fatalf("top fixture final = %s, want 5.00", top.FinalRating)
	}
	if !performance.IsHighPerformer(top) {
		t.Fatal("valid 5.00 calibration is not a high performer")
	}

	midGraph, midProposed := calibrationGraph(t)
	mid := finalizeOne(t, midGraph, midProposed)
	if performance.IsHighPerformer(mid) {
		t.Fatalf("valid %s calibration is a high performer", mid.FinalRating)
	}

	if !performance.IsTopBandRating(proposedDecimal(t, "4.5", 1)) {
		t.Fatal("4.5 at scale 1 is not top-band: the bar compares values, not text")
	}
	if performance.IsTopBandRating(proposedDecimal(t, "4.49", 2)) {
		t.Fatal("4.49 is top-band")
	}
	forged := top
	forged.FinalRating = proposedDecimal(t, "4.00", 2)
	if err := forged.Validate(); err == nil {
		t.Fatal("tampered rating still validates; the forgery case proves nothing")
	}
	if performance.IsHighPerformer(forged) {
		t.Fatal("tampered calibration is a high performer")
	}
}
