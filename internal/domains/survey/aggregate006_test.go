package survey

import (
	"errors"
	"testing"
	"time"
)

var survey006At = time.Date(2026, 3, 6, 9, 0, 0, 0, time.UTC)

func survey006Input() AggregateInput {
	return AggregateInput{
		Tenant: "acme", CampaignRef: "campaign-q1", RuleVersion: "survey-rules/v3",
		ScoreMin: 1, ScoreMax: 5,
		Scores:      []int64{5, 4, 5, 3, 4, 5},
		Nonresponse: 1, MinimumCohort: 5, DecidedAt: survey006At,
	}
}

// TestTodo_SURVEY_006 is the PRIMARY contract: scoring, weights,
// nonresponse and rounding are versioned and exact, results stay
// cohort-safe, and representativeness is never fabricated.
func TestTodo_SURVEY_006(t *testing.T) {
	got, err := ComputeAggregate(survey006Input())
	if err != nil {
		t.Fatalf("ComputeAggregate: %v", err)
	}
	if got.Respondents != 6 || got.Mean.String() != "4.33" {
		t.Fatalf("mean of [5 4 5 3 4 5] must be 4.33: %+v", got)
	}
	if got.Distribution[5] != 3 || got.Distribution[3] != 1 {
		t.Fatalf("distribution must count exactly: %+v", got.Distribution)
	}
	if got.NonresponseRateBps != 1428 {
		t.Fatalf("nonresponse 1/7 must be 1428bps, got %d", got.NonresponseRateBps)
	}
	if got.Representativeness != RepresentativeLimited {
		t.Fatalf("partial nonresponse must limit representativeness: %+v", got)
	}
	if got.Digest == "" {
		t.Fatalf("result must seal a digest")
	}

	t.Run("full response at double minimum earns representative", func(t *testing.T) {
		in := survey006Input()
		in.Scores = []int64{4, 4, 4, 4, 4, 4, 4, 4, 4, 4}
		in.Nonresponse = 0
		got, err := ComputeAggregate(in)
		if err != nil {
			t.Fatal(err)
		}
		if got.Representativeness != RepresentativeCohort {
			t.Fatalf("census cohort must be representative: %+v", got)
		}
		if !got.Uncertainty.IsZero() {
			t.Fatalf("zero nonresponse must carry zero uncertainty: %+v", got)
		}
	})

	t.Run("heavy nonresponse stays unknown", func(t *testing.T) {
		in := survey006Input()
		in.Nonresponse = 30
		got, err := ComputeAggregate(in)
		if err != nil {
			t.Fatal(err)
		}
		if got.Representativeness != RepresentativeUnknown {
			t.Fatalf("heavy nonresponse must stay UNKNOWN: %+v", got)
		}
	})

	t.Run("small cohorts never release", func(t *testing.T) {
		in := survey006Input()
		in.Scores = []int64{5, 4, 3}
		if _, err := ComputeAggregate(in); !errors.Is(err, ErrAggregateRejected) {
			t.Fatalf("below-minimum cohort must be SURVEY_006_REJECTED, got %v", err)
		}
	})

	t.Run("weights reshape the mean exactly", func(t *testing.T) {
		in := survey006Input()
		in.Scores = []int64{5, 5, 5, 5, 1}
		in.Weights = []int64{1, 1, 1, 1, 4}
		got, err := ComputeAggregate(in)
		if err != nil {
			t.Fatal(err)
		}
		if got.Mean.String() != "3.00" {
			t.Fatalf("weighted mean must be 3.00, got %s", got.Mean.String())
		}
	})
}

func TestTodo_SURVEY_006_Property(t *testing.T) {
	a, err := ComputeAggregate(survey006Input())
	if err != nil {
		t.Fatal(err)
	}
	b, err := ComputeAggregate(survey006Input())
	if err != nil {
		t.Fatal(err)
	}
	if a.Digest != b.Digest {
		t.Fatalf("identical cohorts must aggregate identically")
	}
	// Score order is not semantic.
	shuffled := survey006Input()
	shuffled.Scores = []int64{3, 4, 5, 5, 4, 5}
	c, err := ComputeAggregate(shuffled)
	if err != nil {
		t.Fatal(err)
	}
	if !c.Mean.Equal(a.Mean) || c.Digest != a.Digest {
		t.Fatalf("score order must not move the result")
	}
	// Distribution always sums to respondents.
	total := 0
	for _, n := range a.Distribution {
		total += n
	}
	if total != a.Respondents {
		t.Fatalf("distribution must sum to respondents")
	}
	// Malformed inputs are refused, never defaulted.
	for name, mutate := range map[string]func(*AggregateInput){
		"range":   func(in *AggregateInput) { in.ScoreMax = in.ScoreMin },
		"weights": func(in *AggregateInput) { in.Weights = []int64{1, 1} },
		"outside": func(in *AggregateInput) { in.Scores[0] = 9 },
		"rule":    func(in *AggregateInput) { in.RuleVersion = "" },
	} {
		in := survey006Input()
		mutate(&in)
		if _, err := ComputeAggregate(in); !errors.Is(err, ErrAggregateRejected) {
			t.Fatalf("malformed %s must be SURVEY_006_REJECTED", name)
		}
	}
}
