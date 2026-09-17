// SURVEY-005 RED: minimum-cohort and re-identification defenses for
// aggregate release. These tests name the production contract in
// disclosure.go before it exists.
package survey

import (
	"errors"
	"strings"
	"testing"
)

func survey005Request() ReleaseRequest {
	return ReleaseRequest{
		CohortCount:   10,
		MinimumCohort: 5,
		CellCounts:    []int{5, 5},
		DecidedAt:     testInstant(),
	}
}

// TestTodo_SURVEY_005 is the PRIMARY acceptance case: small cohorts are
// suppressed, small cells and rare attributes generalize, differencing and
// repeated filters cannot isolate a respondent, and free text needs review.
func TestTodo_SURVEY_005(t *testing.T) {
	t.Run("clean release", func(t *testing.T) {
		d, err := EvaluateRelease(survey005Request())
		if err != nil {
			t.Fatalf("EvaluateRelease error = %v", err)
		}
		if d.Outcome != ReleaseReleased {
			t.Fatalf("Outcome = %s, want RELEASED (reasons %v)", d.Outcome, d.Reasons)
		}
		if !d.Releasable() {
			t.Fatal("Releasable = false for a clean release")
		}
		if len(d.Reasons) != 0 {
			t.Fatalf("Reasons = %v, want none", d.Reasons)
		}
		if d.CohortCount != 10 || d.MinimumCohort != 5 {
			t.Fatalf("decision state = %+v, want cohort 10 minimum 5", d)
		}
		if d.Digest == "" {
			t.Fatal("Digest is empty for a valid decision")
		}
	})

	t.Run("small cohort suppressed", func(t *testing.T) {
		req := survey005Request()
		req.CohortCount = 4
		req.CellCounts = []int{2, 2}
		d, err := EvaluateRelease(req)
		if err != nil {
			t.Fatalf("EvaluateRelease error = %v", err)
		}
		if d.Outcome != ReleaseSuppressed {
			t.Fatalf("Outcome = %s, want SUPPRESSED", d.Outcome)
		}
		if d.Releasable() {
			t.Fatal("Releasable = true for a suppressed release")
		}
		if !hasReason(d.Reasons, ReleaseReasonSmallCohort) {
			t.Fatalf("Reasons = %v, want SMALL_COHORT", d.Reasons)
		}
	})

	t.Run("small cell generalized", func(t *testing.T) {
		req := survey005Request()
		req.CellCounts = []int{9, 1}
		d, err := EvaluateRelease(req)
		if err != nil {
			t.Fatalf("EvaluateRelease error = %v", err)
		}
		if d.Outcome != ReleaseGeneralized {
			t.Fatalf("Outcome = %s, want GENERALIZED", d.Outcome)
		}
		if !hasReason(d.Reasons, ReleaseReasonSmallCell) {
			t.Fatalf("Reasons = %v, want SMALL_CELL", d.Reasons)
		}
	})

	t.Run("zero cell generalized", func(t *testing.T) {
		req := survey005Request()
		req.CellCounts = []int{10, 0}
		d, err := EvaluateRelease(req)
		if err != nil {
			t.Fatalf("EvaluateRelease error = %v", err)
		}
		if d.Outcome != ReleaseGeneralized || !hasReason(d.Reasons, ReleaseReasonSmallCell) {
			t.Fatalf("Outcome = %s reasons %v, want GENERALIZED for exact-zero cell", d.Outcome, d.Reasons)
		}
	})

	t.Run("differencing suppressed", func(t *testing.T) {
		req := survey005Request()
		req.PriorReleases = []PriorRelease{{CohortCount: 11, OverlapCount: 10}}
		d, err := EvaluateRelease(req)
		if err != nil {
			t.Fatalf("EvaluateRelease error = %v", err)
		}
		if d.Outcome != ReleaseSuppressed {
			t.Fatalf("Outcome = %s, want SUPPRESSED", d.Outcome)
		}
		if !hasReason(d.Reasons, ReleaseReasonDifferencing) {
			t.Fatalf("Reasons = %v, want DIFFERENCING", d.Reasons)
		}
	})

	t.Run("repeated identical filter needs review", func(t *testing.T) {
		req := survey005Request()
		req.PriorReleases = []PriorRelease{
			{CohortCount: 10, OverlapCount: 10},
			{CohortCount: 10, OverlapCount: 10},
		}
		d, err := EvaluateRelease(req)
		if err != nil {
			t.Fatalf("EvaluateRelease error = %v", err)
		}
		if d.Outcome != ReleaseNeedsReview {
			t.Fatalf("Outcome = %s, want NEEDS_REVIEW", d.Outcome)
		}
		if !hasReason(d.Reasons, ReleaseReasonRepeatedQuery) {
			t.Fatalf("Reasons = %v, want REPEATED_QUERY", d.Reasons)
		}
	})

	t.Run("free text needs review", func(t *testing.T) {
		req := survey005Request()
		req.HasFreeText = true
		d, err := EvaluateRelease(req)
		if err != nil {
			t.Fatalf("EvaluateRelease error = %v", err)
		}
		if d.Outcome != ReleaseNeedsReview || !hasReason(d.Reasons, ReleaseReasonFreeText) {
			t.Fatalf("Outcome = %s reasons %v, want NEEDS_REVIEW with FREE_TEXT", d.Outcome, d.Reasons)
		}
	})

	t.Run("rare attribute generalized", func(t *testing.T) {
		req := survey005Request()
		req.HasRareAttribute = true
		d, err := EvaluateRelease(req)
		if err != nil {
			t.Fatalf("EvaluateRelease error = %v", err)
		}
		if d.Outcome != ReleaseGeneralized || !hasReason(d.Reasons, ReleaseReasonRareAttribute) {
			t.Fatalf("Outcome = %s reasons %v, want GENERALIZED with RARE_ATTRIBUTE", d.Outcome, d.Reasons)
		}
	})

	t.Run("suppress dominates review and generalize", func(t *testing.T) {
		req := survey005Request()
		req.CohortCount = 3
		req.CellCounts = []int{3}
		req.HasFreeText = true
		req.HasRareAttribute = true
		d, err := EvaluateRelease(req)
		if err != nil {
			t.Fatalf("EvaluateRelease error = %v", err)
		}
		if d.Outcome != ReleaseSuppressed {
			t.Fatalf("Outcome = %s, want SUPPRESSED", d.Outcome)
		}
		for _, want := range []ReleaseReason{ReleaseReasonSmallCohort, ReleaseReasonSmallCell, ReleaseReasonFreeText, ReleaseReasonRareAttribute} {
			if !hasReason(d.Reasons, want) {
				t.Fatalf("Reasons = %v, want %s", d.Reasons, want)
			}
		}
	})

	t.Run("invalid requests fail closed", func(t *testing.T) {
		cases := map[string]func(ReleaseRequest) ReleaseRequest{
			"threshold below floor": func(r ReleaseRequest) ReleaseRequest { r.MinimumCohort = 4; return r },
			"negative cohort":       func(r ReleaseRequest) ReleaseRequest { r.CohortCount = -1; return r },
			"negative cell":         func(r ReleaseRequest) ReleaseRequest { r.CellCounts = []int{6, -1}; return r },
			"overlap beyond cohort": func(r ReleaseRequest) ReleaseRequest {
				r.PriorReleases = []PriorRelease{{CohortCount: 4, OverlapCount: 11}}
				return r
			},
			"negative overlap": func(r ReleaseRequest) ReleaseRequest {
				r.PriorReleases = []PriorRelease{{CohortCount: 4, OverlapCount: -1}}
				return r
			},
		}
		for name, mutate := range cases {
			if _, err := EvaluateRelease(mutate(survey005Request())); !errors.Is(err, ErrInvalidReleaseRequest) {
				t.Fatalf("%s: error = %v, want ErrInvalidReleaseRequest", name, err)
			}
		}
		if _, err := EvaluateRelease(ReleaseRequest{}); !errors.Is(err, ErrInvalidReleaseRequest) {
			t.Fatalf("zero request: error = %v, want ErrInvalidReleaseRequest", err)
		}
	})

	t.Run("threshold below floor cites anonymity floor", func(t *testing.T) {
		req := survey005Request()
		req.MinimumCohort = 2
		_, err := EvaluateRelease(req)
		if !errors.Is(err, ErrAnonymityThresholdTooLow) {
			t.Fatalf("error = %v, want ErrAnonymityThresholdTooLow", err)
		}
	})

	t.Run("explanation discloses no payload content", func(t *testing.T) {
		req := survey005Request()
		req.HasFreeText = true
		d, err := EvaluateRelease(req)
		if err != nil {
			t.Fatalf("EvaluateRelease error = %v", err)
		}
		text := d.Explain()
		if text == "" {
			t.Fatal("Explain is empty")
		}
		if !strings.Contains(text, "NEEDS_REVIEW") {
			t.Fatalf("Explain = %q, want outcome named", text)
		}
		for _, leaked := range []string{"@", "member-", "psn:", "subject", "answer=", "Value"} {
			if strings.Contains(text, leaked) {
				t.Fatalf("Explain = %q leaks payload marker %q", text, leaked)
			}
		}
	})

	t.Run("identical inputs decide identically", func(t *testing.T) {
		first, err := EvaluateRelease(survey005Request())
		if err != nil {
			t.Fatalf("first EvaluateRelease error = %v", err)
		}
		second, err := EvaluateRelease(survey005Request())
		if err != nil {
			t.Fatalf("second EvaluateRelease error = %v", err)
		}
		if first.Digest != second.Digest || first.Outcome != second.Outcome || len(first.Reasons) != len(second.Reasons) {
			t.Fatalf("nondeterministic decisions: %+v vs %+v", first, second)
		}
	})
}

func hasReason(reasons []ReleaseReason, want ReleaseReason) bool {
	for _, r := range reasons {
		if r == want {
			return true
		}
	}
	return false
}

// TestTodo_SURVEY_005_Property checks disclosure-control algebra: shrinking a
// cohort never relaxes the outcome, and decisions stay deterministic with
// sorted, de-duplicated reasons.
func TestTodo_SURVEY_005_Property(t *testing.T) {
	severity := map[ReleaseOutcome]int{
		ReleaseReleased:    0,
		ReleaseGeneralized: 1,
		ReleaseNeedsReview: 2,
		ReleaseSuppressed:  3,
	}

	t.Run("shrinking cohort never relaxes outcome", func(t *testing.T) {
		base := survey005Request()
		base.CellCounts = nil
		prev := -1
		for cohort := 12; cohort >= 0; cohort-- {
			req := base
			req.CohortCount = cohort
			d, err := EvaluateRelease(req)
			if err != nil {
				t.Fatalf("cohort %d: error = %v", cohort, err)
			}
			got := severity[d.Outcome]
			if prev != -1 && got < prev {
				t.Fatalf("cohort %d relaxed to %s after stricter cohort", cohort, d.Outcome)
			}
			prev = got
		}
	})

	t.Run("reasons sorted and de-duplicated", func(t *testing.T) {
		req := survey005Request()
		req.CohortCount = 3
		req.CellCounts = []int{1, 1, 1}
		req.HasFreeText = true
		req.HasRareAttribute = true
		d, err := EvaluateRelease(req)
		if err != nil {
			t.Fatalf("EvaluateRelease error = %v", err)
		}
		seen := make(map[ReleaseReason]bool, len(d.Reasons))
		for i, r := range d.Reasons {
			if seen[r] {
				t.Fatalf("Reasons = %v, want de-duplicated", d.Reasons)
			}
			seen[r] = true
			if i > 0 && d.Reasons[i-1] >= r {
				t.Fatalf("Reasons = %v, want sorted", d.Reasons)
			}
		}
	})

	t.Run("released means every cell and cohort meet the floor", func(t *testing.T) {
		for _, cells := range [][]int{nil, {5}, {5, 6, 7}, {100}} {
			req := survey005Request()
			req.CohortCount = 11
			req.CellCounts = cells
			d, err := EvaluateRelease(req)
			if err != nil {
				t.Fatalf("cells %v: error = %v", cells, err)
			}
			if !d.Releasable() || d.Outcome != ReleaseReleased {
				t.Fatalf("cells %v: outcome = %s, want RELEASED", cells, d.Outcome)
			}
		}
	})
}

// FuzzTodo_SURVEY_005 feeds hostile counts and flags at the disclosure gate:
// it must never panic, never release an under-floor cohort, and every
// rejection must carry the typed invalid-request error.
func FuzzTodo_SURVEY_005(f *testing.F) {
	f.Add(10, 5, 5, 5, 10, 8, false, false)
	f.Add(3, 5, 1, 0, 0, 0, true, true)
	f.Add(0, 5, 0, 0, 5, 5, false, false)
	f.Add(-1, 4, -2, 7, 20, -3, true, false)
	f.Fuzz(func(t *testing.T, cohort, minimum, cellA, cellB, priorCohort, overlap int, freeText, rare bool) {
		req := ReleaseRequest{
			CohortCount:      cohort,
			MinimumCohort:    minimum,
			CellCounts:       []int{cellA, cellB},
			HasFreeText:      freeText,
			HasRareAttribute: rare,
			PriorReleases:    []PriorRelease{{CohortCount: priorCohort, OverlapCount: overlap}},
			DecidedAt:        testInstant(),
		}
		d, err := EvaluateRelease(req)
		if err != nil {
			if !errors.Is(err, ErrInvalidReleaseRequest) {
				t.Fatalf("untyped error %v for %+v", err, req)
			}
			return
		}
		if d.Digest == "" {
			t.Fatal("valid decision has empty digest")
		}
		if d.Outcome == ReleaseReleased {
			if d.CohortCount < d.MinimumCohort || d.MinimumCohort < 5 {
				t.Fatalf("released under-floor cohort: %+v", d)
			}
			if len(d.Reasons) != 0 {
				t.Fatalf("released with reasons %v", d.Reasons)
			}
		}
		if d.Outcome != ReleaseReleased && len(d.Reasons) == 0 {
			t.Fatalf("blocking outcome %s carries no reason", d.Outcome)
		}
	})
}

// TestTodo_SURVEY_005_Security proves the disclosure gate denies without
// leaking: errors and explanations carry counts and fixed codes only, never
// respondent content, member references, or answer values.
func TestTodo_SURVEY_005_Security(t *testing.T) {
	markers := []string{"alice@example.com", "member-123", "psn:", "subject-ref", "Strongly agree", "Salary: 250k"}

	t.Run("explanations carry no respondent content", func(t *testing.T) {
		req := survey005Request()
		req.CohortCount = 2
		req.HasFreeText = true
		d, err := EvaluateRelease(req)
		if err != nil {
			t.Fatalf("EvaluateRelease error = %v", err)
		}
		text := d.Explain()
		for _, m := range markers {
			if strings.Contains(text, m) {
				t.Fatalf("Explain = %q contains %q", text, m)
			}
		}
		if strings.ContainsAny(text, "@") {
			t.Fatalf("Explain = %q contains identity-shaped text", text)
		}
	})

	t.Run("rejections carry no respondent content", func(t *testing.T) {
		req := survey005Request()
		req.CohortCount = -5
		_, err := EvaluateRelease(req)
		if !errors.Is(err, ErrInvalidReleaseRequest) {
			t.Fatalf("error = %v, want ErrInvalidReleaseRequest", err)
		}
		for _, m := range markers {
			if strings.Contains(err.Error(), m) {
				t.Fatalf("error = %q contains %q", err, m)
			}
		}
	})

	t.Run("request and decision types hold no content fields", func(t *testing.T) {
		req := survey005Request()
		d, err := EvaluateRelease(req)
		if err != nil {
			t.Fatalf("EvaluateRelease error = %v", err)
		}
		got := d.Explain() + string(d.Outcome)
		for _, r := range d.Reasons {
			got += string(r)
		}
		_ = req
		for _, m := range markers {
			if strings.Contains(got, m) {
				t.Fatalf("decision text contains %q", m)
			}
		}
	})
}

// TestTodo_SURVEY_005_Mutation kills the semantic mutants that matter for a
// disclosure gate: off-by-one thresholds, a dropped floor, and removed
// differencing, repeat-query, free-text, cell, and rare-attribute checks.
func TestTodo_SURVEY_005_Mutation(t *testing.T) {
	t.Run("boundary cohort releases only at the floor", func(t *testing.T) {
		req := survey005Request()
		req.CohortCount = 5
		req.CellCounts = []int{5}
		d, err := EvaluateRelease(req)
		if err != nil {
			t.Fatalf("EvaluateRelease error = %v", err)
		}
		if d.Outcome != ReleaseReleased {
			t.Fatalf("floor cohort: outcome = %s, want RELEASED", d.Outcome)
		}
		req.CohortCount = 4
		req.CellCounts = []int{4}
		d, err = EvaluateRelease(req)
		if err != nil {
			t.Fatalf("EvaluateRelease error = %v", err)
		}
		if d.Outcome != ReleaseSuppressed || !hasReason(d.Reasons, ReleaseReasonSmallCohort) {
			t.Fatalf("below floor: outcome = %s reasons %v", d.Outcome, d.Reasons)
		}
	})

	t.Run("floor cannot be lowered", func(t *testing.T) {
		req := survey005Request()
		req.MinimumCohort = 1
		if _, err := EvaluateRelease(req); !errors.Is(err, ErrAnonymityThresholdTooLow) {
			t.Fatalf("error = %v, want ErrAnonymityThresholdTooLow", err)
		}
	})

	t.Run("boundary cell generalizes only below the floor", func(t *testing.T) {
		req := survey005Request()
		req.CohortCount = 10
		req.CellCounts = []int{5, 5}
		if d, err := EvaluateRelease(req); err != nil || d.Outcome != ReleaseReleased {
			t.Fatalf("floor cells: outcome = %+v err = %v", d, err)
		}
		req.CellCounts = []int{6, 4}
		d, err := EvaluateRelease(req)
		if err != nil {
			t.Fatalf("EvaluateRelease error = %v", err)
		}
		if d.Outcome != ReleaseGeneralized || !hasReason(d.Reasons, ReleaseReasonSmallCell) {
			t.Fatalf("below-floor cell: outcome = %s reasons %v", d.Outcome, d.Reasons)
		}
	})

	t.Run("single-respondent difference is caught", func(t *testing.T) {
		req := survey005Request()
		req.CohortCount = 10
		req.CellCounts = []int{5, 5}
		req.PriorReleases = []PriorRelease{{CohortCount: 9, OverlapCount: 9}}
		d, err := EvaluateRelease(req)
		if err != nil {
			t.Fatalf("EvaluateRelease error = %v", err)
		}
		if d.Outcome != ReleaseSuppressed || !hasReason(d.Reasons, ReleaseReasonDifferencing) {
			t.Fatalf("differencing: outcome = %s reasons %v", d.Outcome, d.Reasons)
		}
	})

	t.Run("subset overlap without isolable remainder releases", func(t *testing.T) {
		req := survey005Request()
		req.CohortCount = 10
		req.CellCounts = []int{5, 5}
		req.PriorReleases = []PriorRelease{{CohortCount: 10, OverlapCount: 5}}
		d, err := EvaluateRelease(req)
		if err != nil {
			t.Fatalf("EvaluateRelease error = %v", err)
		}
		if d.Outcome != ReleaseReleased {
			t.Fatalf("safe overlap: outcome = %s reasons %v", d.Outcome, d.Reasons)
		}
	})

	t.Run("one repeat tolerates, two repeats review", func(t *testing.T) {
		req := survey005Request()
		req.PriorReleases = []PriorRelease{{CohortCount: 10, OverlapCount: 10}}
		if d, err := EvaluateRelease(req); err != nil || d.Outcome != ReleaseReleased {
			t.Fatalf("single repeat: outcome = %+v err = %v", d, err)
		}
		req.PriorReleases = append(req.PriorReleases, PriorRelease{CohortCount: 10, OverlapCount: 10})
		d, err := EvaluateRelease(req)
		if err != nil {
			t.Fatalf("EvaluateRelease error = %v", err)
		}
		if d.Outcome != ReleaseNeedsReview || !hasReason(d.Reasons, ReleaseReasonRepeatedQuery) {
			t.Fatalf("double repeat: outcome = %s reasons %v", d.Outcome, d.Reasons)
		}
	})

	t.Run("each flag alone changes the outcome", func(t *testing.T) {
		for _, tc := range []struct {
			name    string
			mutate  func(ReleaseRequest) ReleaseRequest
			outcome ReleaseOutcome
			reason  ReleaseReason
		}{
			{"free text", func(r ReleaseRequest) ReleaseRequest { r.HasFreeText = true; return r }, ReleaseNeedsReview, ReleaseReasonFreeText},
			{"rare attribute", func(r ReleaseRequest) ReleaseRequest { r.HasRareAttribute = true; return r }, ReleaseGeneralized, ReleaseReasonRareAttribute},
		} {
			d, err := EvaluateRelease(tc.mutate(survey005Request()))
			if err != nil {
				t.Fatalf("%s: error = %v", tc.name, err)
			}
			if d.Outcome != tc.outcome || !hasReason(d.Reasons, tc.reason) {
				t.Fatalf("%s: outcome = %s reasons %v", tc.name, d.Outcome, d.Reasons)
			}
		}
	})
}
