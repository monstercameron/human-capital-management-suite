package app

import (
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/performance"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

const (
	hiperfExecuteDigest = "sha256:execute-plan"
	hiperfVariantDigest = "sha256:high-performer-plan"
)

func hiperfInstant(t *testing.T, text string) values.Instant {
	t.Helper()
	at, err := time.Parse(time.RFC3339, text)
	if err != nil {
		t.Fatalf("parse instant %q: %v", text, err)
	}
	return values.NewInstant(at)
}

func hiperfDecimal(t *testing.T, text string, scale int32) values.Decimal {
	t.Helper()
	d, err := values.NewDecimal(text, scale, values.RoundingExactRequired)
	if err != nil {
		t.Fatalf("decimal %q: %v", text, err)
	}
	return d
}

// hiperfCalibratedRating runs a real performance calibration for one
// participant and finalizes it: manager and peer reviews at the given
// ratings, weighted 2-to-1, produce the participant's calibrated final
// rating. The result is valid by construction, so a tampered copy is a
// genuine forgery rather than a second invalid value.
func hiperfCalibratedRating(t *testing.T, managerRating, peerRating string) performance.CalibratedRating {
	t.Helper()
	cycle, err := performance.NewPerformanceCycle(
		"cycle-2026",
		performance.PopulationBindingRef{DefinitionID: "population", RevisionVersion: "7", Digest: "sha256:population"},
		performance.CalendarBindingRef{Ref: "calendar", Version: "3", Digest: "sha256:calendar"},
		performance.RatingScaleVersionRef{ID: "scale", Version: "2", Digest: "sha256:scale"},
	)
	if err != nil {
		t.Fatalf("cycle: %v", err)
	}
	graph, err := performance.FreezeParticipantReviewerGraph(cycle,
		[]performance.ParticipantRef{{ID: "participant-1"}, {ID: "participant-2"}},
		[]performance.ReviewerAssignment{
			{ParticipantID: "participant-1", ReviewerID: "manager-1", Relationship: performance.ReviewerRelationshipManager},
			{ParticipantID: "participant-1", ReviewerID: "peer-1", Relationship: performance.ReviewerRelationshipPeer},
			{ParticipantID: "participant-2", ReviewerID: "manager-2", Relationship: performance.ReviewerRelationshipManager},
		}, performance.DefaultReviewerGraphRules(), hiperfInstant(t, "2026-09-01T00:00:00Z"))
	if err != nil {
		t.Fatalf("reviewer graph: %v", err)
	}
	collection, err := performance.NewReviewCollection(graph, hiperfInstant(t, "2026-10-01T00:00:00Z"), performance.ReviewCollectionPolicy{RatingScale: cycle.RatingScale})
	if err != nil {
		t.Fatalf("collection: %v", err)
	}
	for _, item := range []struct{ reviewer, rating string }{{"manager-1", managerRating}, {"peer-1", peerRating}} {
		review := performance.Review{
			ID: item.reviewer + "-participant-1-review", Kind: performance.ReviewKindReview, State: performance.ReviewStateSubmitted,
			ReviewerID: item.reviewer, ParticipantID: "participant-1", CycleID: collection.Graph.CycleID,
			CycleRevision: collection.Graph.CycleRevision, GraphRevision: collection.Graph.GraphRevision,
			GraphDigest: collection.Graph.Digest, RatingScale: collection.Policy.RatingScale,
			Rating: item.rating, NarrativeDigest: canonicalbytes.Digest([]byte("private narrative")),
			SubmittedAt: hiperfInstant(t, "2026-09-05T12:00:00Z"), ReviewRevision: 1,
		}
		if collection, err = collection.Submit(review); err != nil {
			t.Fatalf("submit %s: %v", item.reviewer, err)
		}
	}
	rule, err := performance.NewProposedRatingRule("performance.rating", "v1", 2, 2, values.RoundingHalfEven, 2,
		map[performance.ReviewerRelationshipKind]values.Decimal{
			performance.ReviewerRelationshipManager: hiperfDecimal(t, "2.00", 2),
			performance.ReviewerRelationshipPeer:    hiperfDecimal(t, "1.00", 2),
		}, performance.OutlierHandlingNone)
	if err != nil {
		t.Fatalf("rating rule: %v", err)
	}
	proposed, err := performance.CalculateProposedRating(collection, "participant-1", rule)
	if err != nil {
		t.Fatalf("proposed: %v", err)
	}
	calibration, err := performance.NewCalibrationRule("performance.calibration", "v1", hiperfDecimal(t, "0.50", 2), nil)
	if err != nil {
		t.Fatalf("calibration rule: %v", err)
	}
	session, err := performance.NewCalibrationSession("calibration-1", graph, []performance.ProposedRating{proposed}, "facilitator-1", []string{"manager-1", "peer-1"}, calibration)
	if err != nil {
		t.Fatalf("session: %v", err)
	}
	final, err := session.FinalizeParticipant("participant-1")
	if err != nil {
		t.Fatalf("finalize: %v", err)
	}
	return final
}

// TestTodo_HIPERF_004 proves rating-driven plan resolution: under the
// execute plan a subject holding a top-band calibrated rating resolves the
// high-performer digest while every other subject resolves the execute
// digest, and prototype mode never routes to the variant.
func TestTodo_HIPERF_004(t *testing.T) {
	top := hiperfCalibratedRating(t, "5", "5")
	if top.FinalRating.String() != "5.00" {
		t.Fatalf("top fixture final = %s, want 5.00; the routing test proves nothing", top.FinalRating)
	}
	if got := ResolvePromotionPlanDigest(PromotionPlanExecute, &top, hiperfExecuteDigest, hiperfVariantDigest); got != hiperfVariantDigest {
		t.Fatalf("top-rated subject under execute resolves %q, want the variant %q", got, hiperfVariantDigest)
	}
	mid := hiperfCalibratedRating(t, "3", "3")
	if got := ResolvePromotionPlanDigest(PromotionPlanExecute, &mid, hiperfExecuteDigest, hiperfVariantDigest); got != hiperfExecuteDigest {
		t.Fatalf("mid-rated subject under execute resolves %q, want the execute %q", got, hiperfExecuteDigest)
	}
	if got := ResolvePromotionPlanDigest(PromotionPlanExecute, nil, hiperfExecuteDigest, hiperfVariantDigest); got != hiperfExecuteDigest {
		t.Fatalf("unrated subject under execute resolves %q, want the execute %q", got, hiperfExecuteDigest)
	}
	for _, plan := range []string{PromotionPlanPrototype, "", "execute-typo"} {
		if got := ResolvePromotionPlanDigest(plan, &top, hiperfExecuteDigest, hiperfVariantDigest); got != hiperfExecuteDigest {
			t.Fatalf("top-rated subject under plan %q resolves %q, want the execute %q: only plan execute routes", plan, got, hiperfExecuteDigest)
		}
	}
	if !PinsHighPerformerVariant(PromotionPlanExecute, &top) {
		t.Fatal("top-rated subject under execute does not pin the variant")
	}
	if PinsHighPerformerVariant(PromotionPlanPrototype, &top) {
		t.Fatal("prototype mode pins the variant: a global flag must never move rated workers")
	}
}

// TestTodo_HIPERF_004_Security proves the routing predicate fails closed:
// an unrated subject, a lower-rated subject and a forged top rating all
// resolve the execute digest, never the variant.
func TestTodo_HIPERF_004_Security(t *testing.T) {
	forged := hiperfCalibratedRating(t, "3", "3")
	forged.FinalRating = hiperfDecimal(t, "5.00", 2)
	if err := forged.Validate(); err == nil {
		t.Fatal("tampered rating still validates; the forgery test proves nothing")
	}
	if got := ResolvePromotionPlanDigest(PromotionPlanExecute, &forged, hiperfExecuteDigest, hiperfVariantDigest); got != hiperfExecuteDigest {
		t.Fatalf("forged top rating resolves %q, want the execute %q", got, hiperfExecuteDigest)
	}

	boundary := hiperfCalibratedRating(t, "5", "3.5")
	if boundary.FinalRating.String() != "4.50" {
		t.Fatalf("boundary fixture final = %s, want 4.50", boundary.FinalRating)
	}
	if got := ResolvePromotionPlanDigest(PromotionPlanExecute, &boundary, hiperfExecuteDigest, hiperfVariantDigest); got != hiperfVariantDigest {
		t.Fatalf("4.50-rated subject resolves %q, want the variant %q", got, hiperfVariantDigest)
	}
	below := hiperfCalibratedRating(t, "5", "3.47")
	if below.FinalRating.String() != "4.49" {
		t.Fatalf("below-band fixture final = %s, want 4.49", below.FinalRating)
	}
	if got := ResolvePromotionPlanDigest(PromotionPlanExecute, &below, hiperfExecuteDigest, hiperfVariantDigest); got != hiperfExecuteDigest {
		t.Fatalf("4.49-rated subject resolves %q, want the execute %q", got, hiperfExecuteDigest)
	}
	if PinsHighPerformerVariant(PromotionPlanExecute, &forged) {
		t.Fatal("forged rating pins the variant")
	}
	if PinsHighPerformerVariant(PromotionPlanExecute, nil) {
		t.Fatal("an unrated subject pins the variant")
	}
}
