package abuse

import (
	"errors"
	"testing"
	"time"
)

func abuse007Input() EvaluationInput {
	verdicts := []ReviewedVerdict{
		ReviewedTruePositive, ReviewedTruePositive, ReviewedTruePositive,
		ReviewedTruePositive, ReviewedTruePositive, ReviewedTruePositive,
		ReviewedFalsePositive, ReviewedTrueNegative, ReviewedTrueNegative,
		ReviewedUnknown,
	}
	outcomes := make([]ReviewedOutcome, 0, len(verdicts))
	for i, v := range verdicts {
		outcomes = append(outcomes, ReviewedOutcome{
			SignalID: string(rune('a'+i)) + "-signal",
			Verdict:  v,
			Reviewer: "reviewer:lead-2",
			At:       time.Date(2026, 6, 16, 9, 0, 0, 0, time.UTC),
		})
	}
	return EvaluationInput{
		DetectorID:      "detector:privileged-burst",
		DetectorVersion: "2.1.0",
		DetectorDigest:  "sha256:detector-v210",
		Outcomes:        outcomes,
		SampledTotal:    20,
		EvaluatedAt:     time.Date(2026, 6, 17, 9, 0, 0, 0, time.UTC),
	}
}

// TestTodo_ABUSE_007 is the primary ABUSE-007 contract test: evaluation
// metrics bind reviewed outcomes, sampling bias, false-positive/negative/
// unknown rates and the detector version; a degraded detector pauses or
// requires review rather than silently broadening containment.
func TestTodo_ABUSE_007(t *testing.T) {
	t.Run("reviewed outcomes bind rates sampling and version", func(t *testing.T) {
		eval, err := EvaluateDetector(abuse007Input())
		if err != nil {
			t.Fatalf("EvaluateDetector: %v", err)
		}
		if eval.DetectorID != "detector:privileged-burst" || eval.DetectorVersion != "2.1.0" {
			t.Fatalf("evaluation must bind the detector version: %+v", eval)
		}
		if eval.Reviewed != 10 {
			t.Fatalf("reviewed = %d, want 10", eval.Reviewed)
		}
		if eval.FalsePositiveRate != 0.1 {
			t.Fatalf("false-positive rate = %v, want 0.1", eval.FalsePositiveRate)
		}
		if eval.FalseNegativeRate != 0.0 {
			t.Fatalf("false-negative rate = %v, want 0.0", eval.FalseNegativeRate)
		}
		if eval.UnknownRate != 0.1 {
			t.Fatalf("unknown rate = %v, want 0.1", eval.UnknownRate)
		}
		if eval.Coverage != 0.5 {
			t.Fatalf("sampling coverage = %v, want 0.5 (10 reviewed of 20 sampled)", eval.Coverage)
		}
		if eval.Digest == "" {
			t.Fatal("evaluation must carry a digest")
		}
	})

	t.Run("missing version and unreviewed outcomes are rejected", func(t *testing.T) {
		base := abuse007Input()
		noVersion := base
		noVersion.DetectorVersion = ""
		if _, err := EvaluateDetector(noVersion); !errors.Is(err, ErrEvaluationRejected) {
			t.Fatalf("missing version must be rejected, got %v", err)
		}
		noDigest := base
		noDigest.DetectorDigest = ""
		if _, err := EvaluateDetector(noDigest); !errors.Is(err, ErrEvaluationRejected) {
			t.Fatalf("missing version digest must be rejected, got %v", err)
		}
		unreviewed := base
		unreviewed.Outcomes = append(append([]ReviewedOutcome(nil), base.Outcomes...), ReviewedOutcome{
			SignalID: "pending-signal", Reviewer: "reviewer:lead-2",
			At: time.Date(2026, 6, 16, 9, 0, 0, 0, time.UTC),
		})
		if _, err := EvaluateDetector(unreviewed); !errors.Is(err, ErrEvaluationRejected) {
			t.Fatalf("unreviewed outcome counted as reviewed must be rejected, got %v", err)
		}
		empty := base
		empty.Outcomes = nil
		if _, err := EvaluateDetector(empty); !errors.Is(err, ErrEvaluationRejected) {
			t.Fatalf("empty outcome set must be rejected, got %v", err)
		}
		oversampled := base
		oversampled.SampledTotal = 4
		if _, err := EvaluateDetector(oversampled); !errors.Is(err, ErrEvaluationRejected) {
			t.Fatalf("reviewed count above sampled total must be rejected, got %v", err)
		}
		unsampled := base
		unsampled.SampledTotal = 0
		if _, err := EvaluateDetector(unsampled); !errors.Is(err, ErrEvaluationRejected) {
			t.Fatalf("missing sampled total must be rejected, got %v", err)
		}
	})

	t.Run("degraded detector pauses or requires review", func(t *testing.T) {
		drifted := abuse007Input()
		for i := range drifted.Outcomes {
			drifted.Outcomes[i].Verdict = ReviewedFalsePositive
		}
		eval, err := EvaluateDetector(drifted)
		if err != nil {
			t.Fatalf("EvaluateDetector: %v", err)
		}
		if eval.Action != ActionRequireReview && eval.Action != ActionPaused {
			t.Fatalf("degraded detector must pause or require review, got %q", eval.Action)
		}
		missed := abuse007Input()
		for i := range missed.Outcomes {
			missed.Outcomes[i].Verdict = ReviewedFalseNegative
		}
		eval, err = EvaluateDetector(missed)
		if err != nil {
			t.Fatalf("EvaluateDetector: %v", err)
		}
		if eval.Action != ActionRequireReview && eval.Action != ActionPaused {
			t.Fatalf("high miss rate must pause or require review, got %q", eval.Action)
		}
	})

	t.Run("healthy detector maintains without broadening", func(t *testing.T) {
		eval, err := EvaluateDetector(abuse007Input())
		if err != nil {
			t.Fatalf("EvaluateDetector: %v", err)
		}
		if eval.Action != ActionMaintain {
			t.Fatalf("healthy detector maintains, got %q", eval.Action)
		}
	})
}

// TestTodo_ABUSE_007_Security proves version binding: an evaluation cannot
// be replayed under another detector, version or digest, and a degraded
// evaluation can never authorize broader containment.
func TestTodo_ABUSE_007_Security(t *testing.T) {
	eval, err := EvaluateDetector(abuse007Input())
	if err != nil {
		t.Fatalf("EvaluateDetector: %v", err)
	}
	if err := VerifyEvaluationBinding(eval, "detector:privileged-burst", "2.1.0", "sha256:detector-v210"); err != nil {
		t.Fatalf("own binding must verify: %v", err)
	}
	for name, args := range map[string][3]string{
		"foreign detector": {"detector:other", "2.1.0", "sha256:detector-v210"},
		"foreign version":  {"detector:privileged-burst", "9.9.9", "sha256:detector-v210"},
		"foreign digest":   {"detector:privileged-burst", "2.1.0", "sha256:forged"},
	} {
		if err := VerifyEvaluationBinding(eval, args[0], args[1], args[2]); !errors.Is(err, ErrEvaluationRejected) {
			t.Fatalf("%s binding must be rejected, got %v", name, err)
		}
	}
	tampered := eval
	tampered.FalsePositives++
	if err := VerifyEvaluationBinding(tampered, "detector:privileged-burst", "2.1.0", "sha256:detector-v210"); err == nil {
		t.Fatal("tampered evaluation digest must break binding verification")
	}
	drifted := abuse007Input()
	for i := range drifted.Outcomes {
		drifted.Outcomes[i].Verdict = ReviewedFalsePositive
	}
	bad, err := EvaluateDetector(drifted)
	if err != nil {
		t.Fatalf("EvaluateDetector: %v", err)
	}
	if bad.Action == ActionMaintain {
		t.Fatal("degraded evaluation must not report maintain")
	}
	if !bad.Action.Valid() {
		t.Fatalf("action %q is outside the closed vocabulary", bad.Action)
	}
}
