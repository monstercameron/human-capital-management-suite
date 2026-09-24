package performance_test

import (
	"errors"
	"sync"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/performance"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func ratingCaseFixture(t *testing.T) performance.RatingCase {
	t.Helper()
	graph, proposed := calibrationGraph(t)
	session, err := performance.NewCalibrationSession("calibration-contest", graph, []performance.ProposedRating{proposed}, "facilitator-1", []string{"manager-1", "peer-1"}, calibrationRule(t))
	if err != nil {
		t.Fatalf("session: %v", err)
	}
	adjustment, err := performance.NewCalibrationAdjustment("participant-1", proposed.Rating, proposedDecimal(t, "3.50", 2), performance.CalibrationReasonEvidence, "peer-1")
	if err != nil {
		t.Fatalf("adjustment: %v", err)
	}
	session, err = session.AddAdjustment(adjustment)
	if err != nil {
		t.Fatalf("adjust session: %v", err)
	}
	calibrated, err := session.FinalizeParticipant("participant-1")
	if err != nil {
		t.Fatalf("calibrated: %v", err)
	}
	window, err := performance.NewRatingContestWindow(reviewInstant(t, "2026-09-10T00:00:00Z"), reviewInstant(t, "2026-10-01T00:00:00Z"))
	if err != nil {
		t.Fatalf("window: %v", err)
	}
	caseValue, err := performance.NewRatingCase("rating-case-1", calibrated, window, reviewInstant(t, "2026-10-15T00:00:00Z"), "manager-1")
	if err != nil {
		t.Fatalf("rating case: %v", err)
	}
	return caseValue
}

func ratingEvidence(id string) []performance.EvidenceRef {
	return []performance.EvidenceRef{{ID: id, Version: "1", Digest: "sha256:" + id}}
}

func TestTodo_PERFORMANCE_006(t *testing.T) {
	base := ratingCaseFixture(t)
	contest, err := performance.NewRatingContest("contest-1", "participant-1", performance.ContestReasonEvidence, canonicalbytes.Digest([]byte("participant narrative")), ratingEvidence("contest-evidence"), reviewInstant(t, "2026-09-20T12:00:00Z"))
	if err != nil {
		t.Fatalf("contest: %v", err)
	}
	contested, err := base.RaiseContest(contest)
	if err != nil {
		t.Fatalf("raise contest: %v", err)
	}
	if !contested.Contested || len(contested.Events) != 1 || contested.Events[0].Kind != performance.RatingEventContestRaised || contested.Events[0].Digest == "" {
		t.Fatalf("contest state/events = %+v", contested)
	}
	if base.Contested || len(base.Events) != 0 || base.CanonicalDigest == contested.CanonicalDigest {
		t.Fatal("raising contest mutated the prior value")
	}
	if _, err := contested.Finalize(reviewInstant(t, "2026-10-01T00:00:00Z")); !errors.Is(err, performance.ErrFinalizationBeforeCutoff) {
		t.Fatalf("early finalization error = %v", err)
	}
	correction, err := performance.NewRatingCorrection("placeholder", "independent-reviewer", performance.CorrectionDecisionCorrect, proposedDecimal(t, "3.75", 2), performance.CorrectionReasonEvidence, ratingEvidence("correction-evidence"), reviewInstant(t, "2026-10-05T12:00:00Z"))
	if err != nil {
		t.Fatalf("correction: %v", err)
	}
	corrected, err := contested.DecideCorrection(correction)
	if err != nil {
		t.Fatalf("decide correction: %v", err)
	}
	if !corrected.CorrectionDecided || !corrected.Corrected || len(corrected.Events) != 2 || corrected.Events[1].Kind != performance.RatingEventCorrectionDecided {
		t.Fatalf("correction state/events = %+v", corrected)
	}
	final, err := corrected.Finalize(reviewInstant(t, "2026-10-15T00:00:00Z"))
	if err != nil {
		t.Fatalf("finalize: %v", err)
	}
	if final.FinalRating.String() != "3.75" || !final.Contested || !final.Corrected || len(final.Events) != 3 || final.Events[2].Kind != performance.RatingEventFinalized {
		t.Fatalf("final rating = %+v", final)
	}
	if final.CanonicalDigest == "" || final.ProposedRating.CanonicalDigest == "" || final.CalibratedRating.CanonicalDigest == "" || final.Contest.Digest == "" || final.Correction.Digest == "" {
		t.Fatalf("final history is incomplete: %+v", final)
	}
	explanation, err := final.Explain()
	if err != nil || explanation.ContestDigest != final.Contest.Digest || explanation.CorrectionDigest != final.Correction.Digest || len(explanation.EventDigests) != 3 {
		t.Fatalf("final explanation = %+v, err=%v", explanation, err)
	}
	if _, err := final.RaiseContest(contest); !errors.Is(err, performance.ErrContestAfterFinalization) {
		t.Fatalf("post-finalization contest error = %v", err)
	}

	uphold, err := performance.NewRatingCorrection("placeholder", "independent-reviewer", performance.CorrectionDecisionUphold, values.Decimal{}, performance.CorrectionReasonPolicy, ratingEvidence("uphold-evidence"), reviewInstant(t, "2026-10-05T12:00:00Z"))
	if err != nil {
		t.Fatalf("uphold correction: %v", err)
	}
	upheldCase, err := contested.DecideCorrection(uphold)
	if err != nil {
		t.Fatalf("decide uphold: %v", err)
	}
	upheld, err := upheldCase.Finalize(reviewInstant(t, "2026-10-15T00:00:00Z"))
	if err != nil || upheld.Corrected || !upheld.CorrectionDecided || upheld.FinalRating.String() != "3.50" {
		t.Fatalf("upheld final = %+v, err=%v", upheld, err)
	}
}

func TestTodo_PERFORMANCE_006_Race(t *testing.T) {
	base := ratingCaseFixture(t)
	contest, err := performance.NewRatingContest("contest-race", "participant-1", performance.ContestReasonProcess, canonicalbytes.Digest([]byte("race narrative")), ratingEvidence("race-evidence"), reviewInstant(t, "2026-09-20T12:00:00Z"))
	if err != nil {
		t.Fatal(err)
	}
	contested, err := base.RaiseContest(contest)
	if err != nil {
		t.Fatal(err)
	}
	const readers = 32
	var wait sync.WaitGroup
	errs := make(chan error, readers)
	for i := 0; i < readers; i++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			if _, err := contested.Explain(); err != nil {
				errs <- err
			}
		}()
	}
	wait.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("concurrent explain: %v", err)
	}
	if len(base.Events) != 0 || len(contested.Events) != 1 {
		t.Fatal("concurrent-read fixture was not immutable")
	}
}

func TestTodo_PERFORMANCE_006_Mutation(t *testing.T) {
	base := ratingCaseFixture(t)
	badNarrative, err := performance.NewRatingContest("contest-bad", "participant-1", performance.ContestReasonEvidence, "raw participant narrative", ratingEvidence("bad-evidence"), reviewInstant(t, "2026-09-20T12:00:00Z"))
	if !errors.Is(err, performance.ErrInvalidContest) || badNarrative.Digest != "" {
		t.Fatalf("raw narrative accepted: contest=%+v err=%v", badNarrative, err)
	}
	contest, err := performance.NewRatingContest("contest-sec", "participant-1", performance.ContestReasonEvidence, canonicalbytes.Digest([]byte("secure narrative")), ratingEvidence("secure-evidence"), reviewInstant(t, "2026-09-20T12:00:00Z"))
	if err != nil {
		t.Fatal(err)
	}
	contested, err := base.RaiseContest(contest)
	if err != nil {
		t.Fatal(err)
	}
	managerCorrection, err := performance.NewRatingCorrection("placeholder", "manager-1", performance.CorrectionDecisionUphold, values.Decimal{}, performance.CorrectionReasonPolicy, ratingEvidence("manager-evidence"), reviewInstant(t, "2026-10-05T12:00:00Z"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := contested.DecideCorrection(managerCorrection); !errors.Is(err, performance.ErrCorrectionSeparation) {
		t.Fatalf("manager correction error = %v", err)
	}
	adjusterCorrection, err := performance.NewRatingCorrection("placeholder", "peer-1", performance.CorrectionDecisionUphold, values.Decimal{}, performance.CorrectionReasonPolicy, ratingEvidence("adjuster-evidence"), reviewInstant(t, "2026-10-05T12:00:00Z"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := contested.DecideCorrection(adjusterCorrection); !errors.Is(err, performance.ErrCorrectionSeparation) {
		t.Fatalf("adjuster correction error = %v", err)
	}
	refer, err := performance.NewRatingCorrection("placeholder", "independent-reviewer", performance.CorrectionDecisionRefer, values.Decimal{}, performance.CorrectionReasonProcess, ratingEvidence("refer-evidence"), reviewInstant(t, "2026-10-05T12:00:00Z"))
	if err != nil {
		t.Fatal(err)
	}
	referred, err := contested.DecideCorrection(refer)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := referred.Finalize(reviewInstant(t, "2026-10-15T00:00:00Z")); !errors.Is(err, performance.ErrContestOpen) {
		t.Fatalf("refer finalization error = %v", err)
	}
	mutated := referred
	mutated.Events[0].Digest = "sha256:tampered"
	if _, err := mutated.Explain(); err == nil {
		t.Fatal("tampered transition event was accepted")
	}
}
