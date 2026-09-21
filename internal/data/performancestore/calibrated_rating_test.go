package performancestore_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/performancestore"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/performance"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// calibrationFixture builds a real finalized calibration through the domain:
// a cycle, a frozen participant/reviewer graph, submitted reviews, the
// proposed ratings those reviews average to, and the calibration session that
// finalizes them. participantOne scores managerOne/peerOne and participantTwo
// scores managerTwo/peerTwo, so a caller chooses each subject's band by
// choosing its reviews, never by asserting a rating.
func calibrationFixture(t *testing.T, one, two [2]string) (performance.CalibrationSession, []performance.FinalCalibratedRating) {
	t.Helper()
	scale := performance.RatingScaleVersionRef{ID: "scale", Version: "2", Digest: "sha256:scale"}
	cycle, err := performance.NewPerformanceCycle("cycle-hiperf",
		performance.PopulationBindingRef{DefinitionID: "population", RevisionVersion: "7", Digest: "sha256:population"},
		performance.CalendarBindingRef{Ref: "calendar", Version: "3", Digest: "sha256:calendar"}, scale)
	if err != nil {
		t.Fatalf("cycle: %v", err)
	}
	opened, err := cycle.Open()
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	graph, err := performance.FreezeParticipantReviewerGraph(opened,
		[]performance.ParticipantRef{{ID: "participant-1"}, {ID: "participant-2"}},
		[]performance.ReviewerAssignment{
			{ParticipantID: "participant-1", ReviewerID: "manager-1", Relationship: performance.ReviewerRelationshipManager},
			{ParticipantID: "participant-1", ReviewerID: "peer-1", Relationship: performance.ReviewerRelationshipPeer},
			{ParticipantID: "participant-2", ReviewerID: "manager-1", Relationship: performance.ReviewerRelationshipManager},
			{ParticipantID: "participant-2", ReviewerID: "peer-1", Relationship: performance.ReviewerRelationshipPeer},
		}, performance.DefaultReviewerGraphRules(), instant())
	if err != nil {
		t.Fatalf("graph: %v", err)
	}
	collection, err := performance.NewReviewCollection(graph, values.NewInstant(time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)), performance.ReviewCollectionPolicy{RatingScale: scale})
	if err != nil {
		t.Fatalf("collection: %v", err)
	}
	scores := map[string][2]string{"participant-1": one, "participant-2": two}
	for _, participant := range []string{"participant-1", "participant-2"} {
		for index, reviewer := range []string{"manager-1", "peer-1"} {
			review := performance.Review{
				ID: participant + "-" + reviewer, Kind: performance.ReviewKindReview, State: performance.ReviewStateSubmitted,
				ReviewerID: reviewer, ParticipantID: participant, CycleID: graph.CycleID,
				CycleRevision: graph.CycleRevision, GraphRevision: graph.GraphRevision, GraphDigest: graph.Digest,
				RatingScale: scale, Rating: scores[participant][index],
				NarrativeDigest: "sha256:" + strings.Repeat("a", 64), SubmittedAt: instant(), ReviewRevision: 1,
			}
			collection, err = collection.Submit(review)
			if err != nil {
				t.Fatalf("submit %s: %v", review.ID, err)
			}
		}
	}
	rule, err := performance.NewProposedRatingRule("rating", "v1", 2, 2, values.RoundingHalfEven, 2,
		map[performance.ReviewerRelationshipKind]values.Decimal{
			performance.ReviewerRelationshipManager: values.MustDecimal("2.00", 2, values.RoundingExactRequired),
			performance.ReviewerRelationshipPeer:    values.MustDecimal("1.00", 2, values.RoundingExactRequired),
		}, performance.OutlierHandlingNone)
	if err != nil {
		t.Fatalf("rating rule: %v", err)
	}
	cohort := make([]performance.ProposedRating, 0, 2)
	for _, participant := range []string{"participant-1", "participant-2"} {
		proposed, proposeErr := performance.CalculateProposedRating(collection, participant, rule)
		if proposeErr != nil {
			t.Fatalf("propose %s: %v", participant, proposeErr)
		}
		cohort = append(cohort, proposed)
	}
	calibrationRule, err := performance.NewCalibrationRule("calibration", "v1", values.MustDecimal("0.50", 2, values.RoundingExactRequired), nil)
	if err != nil {
		t.Fatalf("calibration rule: %v", err)
	}
	session, err := performance.NewCalibrationSession("session-hiperf", graph, cohort, "facilitator-1",
		[]string{"manager-1", "peer-1"}, calibrationRule)
	if err != nil {
		t.Fatalf("session: %v", err)
	}
	ratings, err := session.Finalize()
	if err != nil {
		t.Fatalf("finalize: %v", err)
	}
	return session, ratings
}

// TestTodo_REV_096_01 proves the adapter's conversion: a real finalized
// calibration round-trips through the stored adjustments document into a
// performance.FinalCalibratedRating that passes Validate() and drives the
// high-performer predicate, and a document somebody edited is refused instead
// of routing anybody.
func TestTodo_REV_096_01(t *testing.T) {
	session, ratings := calibrationFixture(t, [2]string{"5", "5"}, [2]string{"3", "3"})
	document, err := performancestore.EncodeCalibratedRatings(ratings)
	if err != nil {
		t.Fatalf("EncodeCalibratedRatings: %v", err)
	}

	top, err := performancestore.DecodeCalibratedRating(document, "participant-1")
	if err != nil {
		t.Fatalf("decode the top-band subject: %v", err)
	}
	if err := top.Validate(); err != nil {
		t.Fatalf("decoded rating does not validate: %v", err)
	}
	if top.FinalRating.String() != "5.00" || top.SessionDigest != session.CanonicalDigest {
		t.Fatalf("decoded rating = %s, session digest %q", top.FinalRating, top.SessionDigest)
	}
	if len(top.ProposedRating.Contributions) != 2 || top.ProposedRatingDigest != top.ProposedRating.CanonicalDigest {
		t.Fatalf("decoded rating lost its proposed rating: %+v", top.ProposedRating)
	}
	if !app.PinsHighPerformerVariant(app.PromotionPlanExecute, &top) {
		t.Fatal("a valid 5.00 calibrated rating does not pin the high-performer variant")
	}

	middle, err := performancestore.DecodeCalibratedRating(document, "participant-2")
	if err != nil {
		t.Fatalf("decode the mid-band subject: %v", err)
	}
	if app.PinsHighPerformerVariant(app.PromotionPlanExecute, &middle) {
		t.Fatalf("a %s calibrated rating pins the high-performer variant", middle.FinalRating)
	}

	if _, err := performancestore.DecodeCalibratedRating(document, "participant-404"); err == nil {
		t.Fatal("a subject the session never rated decoded successfully")
	}

	// A tampered final rating must not survive: the digest chain is what the
	// predicate trusts, and Validate is what checks it.
	var raw map[string]any
	if err := json.Unmarshal(document, &raw); err != nil {
		t.Fatal(err)
	}
	entries, _ := raw["ratings"].([]any)
	entry, _ := entries[0].(map[string]any)
	rating, _ := entry["rating"].(map[string]any)
	rating["FinalRating"] = "2.00/ROUNDING_HALF_EVEN"
	forged, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := performancestore.DecodeCalibratedRating(forged, "participant-1"); err == nil {
		t.Fatal("a tampered calibrated rating decoded successfully")
	}

	// An invalid rating is refused at write time rather than stored.
	broken := ratings[0]
	broken.CanonicalDigest = ""
	if _, err := performancestore.EncodeCalibratedRatings([]performance.FinalCalibratedRating{broken}); err == nil {
		t.Fatal("an unvalidated calibrated rating was encoded")
	}
	if _, err := performancestore.DecodeCalibratedRating([]byte(`{"schema":"other","ratings":[]}`), "participant-1"); err == nil {
		t.Fatal("a foreign adjustments document was accepted")
	}
}

// TestTodo_REV_096_01_Integration proves the whole production path against a
// real database: the durable performance rows, the composed reader bound the
// way serve binds it, a subject whose most recently finalized cycle is
// top-band, and the promotion plan digest that subject resolves.
func TestTodo_REV_096_01_Integration(t *testing.T) {
	db := newDB(t)
	tenant := addTenant(t, db)
	ctx := context.Background()
	conn := appConn(t, db)
	store := performancestore.New(conn)

	session, ratings := calibrationFixture(t, [2]string{"5", "5"}, [2]string{"3", "3"})
	document, err := performancestore.EncodeCalibratedRatings(ratings)
	if err != nil {
		t.Fatal(err)
	}
	graphDocument, err := performancestore.EncodeCalibrationGraph(session.Graph.CycleID, session.Graph.GraphRevision, session.Graph.Digest, "care-operations")
	if err != nil {
		t.Fatal(err)
	}
	// A closed cycle: the reader deliberately refuses a rating recorded
	// against a cycle that is not finalized.
	if err := store.SaveCycle(ctx, tenant, performance.CycleRevision{
		CycleID: session.CycleID, Revision: 1, State: performance.PerformanceCycleClosed, CanonicalDigest: digest('a'),
	}); err != nil {
		t.Fatalf("SaveCycle: %v", err)
	}
	if err := store.SaveCalibrationSession(ctx, tenant, performance.CalibrationSessionRecord{
		SessionID: session.SessionID, CycleID: session.CycleID, CycleRevision: 1,
		Graph: graphDocument, Adjustments: document, CanonicalDigest: session.CanonicalDigest,
	}); err != nil {
		t.Fatalf("SaveCalibrationSession: %v", err)
	}
	for _, rating := range ratings {
		caseID := "case-" + rating.ParticipantID
		if err := store.SaveRatingCase(ctx, tenant, performance.RatingCaseRecord{
			CaseID: caseID, ParticipantID: rating.ParticipantID, CanonicalDigest: rating.CanonicalDigest,
		}); err != nil {
			t.Fatalf("SaveRatingCase %s: %v", caseID, err)
		}
		if err := store.FinalizeRatingCase(ctx, tenant, caseID, rating.CanonicalDigest); err != nil {
			t.Fatalf("FinalizeRatingCase %s: %v", caseID, err)
		}
		if err := store.SaveFinalRating(ctx, tenant, caseID, performance.FinalRatingRecord{
			RatingRef: uuid.NewSHA1(uuid.NameSpaceOID, []byte(caseID)).String(), ParticipantID: rating.ParticipantID,
			CycleID: session.CycleID, FinalRating: rating.FinalRating, FinalizedAt: instant(),
			CanonicalDigest: rating.CanonicalDigest,
		}); err != nil {
			t.Fatalf("SaveFinalRating %s: %v", caseID, err)
		}
	}

	scoped := uuid.UUID(tenant)
	reader := performancestore.NewCalibratedRatings(conn, func(values.TenantId) uuid.UUID { return scoped })
	// The reader satisfies the interface the intent service routes through.
	var lookup app.CalibratedRatingLookup = reader

	top, err := lookup.LookupCalibratedRating(ctx, values.TenantId("harborcare-demo"), "participant-1")
	if err != nil {
		t.Fatalf("LookupCalibratedRating: %v", err)
	}
	if err := top.Validate(); err != nil {
		t.Fatalf("the served rating does not validate: %v", err)
	}
	if !performance.IsHighPerformer(top) {
		t.Fatalf("the served rating %s is not a high performer", top.FinalRating)
	}
	const (
		executeDigest = "sha256:execute-plan"
		variantDigest = "sha256:high-performer-variant"
	)
	if got := app.ResolvePromotionPlanDigest(app.PromotionPlanExecute, &top, executeDigest, variantDigest); got != variantDigest {
		t.Fatalf("the top-band subject resolved %q, want the variant digest", got)
	}

	middle, err := lookup.LookupCalibratedRating(ctx, values.TenantId("harborcare-demo"), "participant-2")
	if err != nil {
		t.Fatalf("LookupCalibratedRating for the mid-band subject: %v", err)
	}
	if got := app.ResolvePromotionPlanDigest(app.PromotionPlanExecute, &middle, executeDigest, variantDigest); got != executeDigest {
		t.Fatalf("a %s subject resolved %q, want the execute digest", middle.FinalRating, got)
	}

	if _, err := lookup.LookupCalibratedRating(ctx, values.TenantId("harborcare-demo"), "participant-404"); err == nil {
		t.Fatal("an unrated subject resolved a calibrated rating")
	}
	if _, err := lookup.LookupCalibratedRating(ctx, values.TenantId("harborcare-demo"), " "); err == nil {
		t.Fatal("an empty subject reference resolved a calibrated rating")
	}
	other := performancestore.NewCalibratedRatings(conn, func(values.TenantId) uuid.UUID { return uuid.New() })
	if _, err := other.LookupCalibratedRating(ctx, values.TenantId("other-tenant"), "participant-1"); err == nil {
		t.Fatal("another tenant read the subject's calibrated rating")
	}
	unbound := performancestore.NewCalibratedRatings(nil, nil)
	if _, err := unbound.LookupCalibratedRating(ctx, values.TenantId("harborcare-demo"), "participant-1"); err == nil {
		t.Fatal("an unbound reader answered a lookup")
	}
	if _, err := performancestore.EncodeCalibrationGraph("", 0, "", ""); err == nil {
		t.Fatal("an incomplete calibration graph reference was encoded")
	}
}
