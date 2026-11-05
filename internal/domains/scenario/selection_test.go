package scenario

import (
	"encoding/json"
	"math/big"
	"os"
	"path/filepath"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func selectionDecimal(t *testing.T, text string) values.Decimal {
	t.Helper()
	d, err := values.NewDecimal(text, 4, values.RoundingExactRequired)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func selectionUnit(t *testing.T) []SelectionMember {
	t.Helper()
	return []SelectionMember{
		{ID: "m1", Age: 45, Cohort: "IC3", Groups: map[string]string{"sex": "M"}, Scores: map[string]values.Decimal{"performance": selectionDecimal(t, "9"), "tenure": selectionDecimal(t, "5")}, Pay: selectionDecimal(t, "120000")},
		{ID: "m2", Age: 38, Cohort: "IC3", Groups: map[string]string{"sex": "F"}, Scores: map[string]values.Decimal{"performance": selectionDecimal(t, "8"), "tenure": selectionDecimal(t, "4")}, Pay: selectionDecimal(t, "110000")},
		{ID: "m3", Age: 52, Cohort: "IC3", Groups: map[string]string{"sex": "F"}, Scores: map[string]values.Decimal{"performance": selectionDecimal(t, "7"), "tenure": selectionDecimal(t, "9")}, Pay: selectionDecimal(t, "108000")},
		{ID: "m4", Age: 29, Cohort: "IC3", Groups: map[string]string{"sex": "M"}, Scores: map[string]values.Decimal{"performance": selectionDecimal(t, "6"), "tenure": selectionDecimal(t, "1")}, Pay: selectionDecimal(t, "95000")},
		{ID: "m5", Age: 61, Cohort: "IC3", Groups: map[string]string{"sex": "F"}, Scores: map[string]values.Decimal{"performance": selectionDecimal(t, "5"), "tenure": selectionDecimal(t, "12")}, Pay: selectionDecimal(t, "105000")},
	}
}

func selectionCriteria(t *testing.T) []SelectionCriterion {
	t.Helper()
	return []SelectionCriterion{
		{Name: "performance", Weight: selectionDecimal(t, "0.8")},
		{Name: "tenure", Weight: selectionDecimal(t, "0.2")},
	}
}

// TestTodo_WF_CAP_009 is the PRIMARY test: a decisional unit ranks by
// declared criteria, adverse impact reports four-fifths and age-band
// results, and pay equity reports gaps by cohort -- all privileged,
// explainable evidence.
func TestTodo_WF_CAP_009(t *testing.T) {
	unit := selectionUnit(t)
	criteria := selectionCriteria(t)

	ranked, err := RankDecisionalUnit(unit, criteria, 2)
	if err != nil {
		t.Fatalf("RankDecisionalUnit: %v", err)
	}
	if len(ranked) != 5 {
		t.Fatalf("ranked = %d members, want 5", len(ranked))
	}
	// performance*0.8 + tenure*0.2: m1=8.2, m2=7.2, m3=7.4, m4=5.0, m5=6.4.
	wantOrder := []string{"m1", "m3", "m2", "m5", "m4"}
	for i, want := range wantOrder {
		if ranked[i].Member.ID != want || ranked[i].Rank != i+1 {
			t.Fatalf("rank %d = %s, want %s", i+1, ranked[i].Member.ID, want)
		}
		if ranked[i].Selected != (i < 2) {
			t.Fatalf("rank %d selected = %v, want %v", i+1, ranked[i].Selected, i < 2)
		}
	}
	if len(ranked[0].Breakdown) != 2 {
		t.Fatalf("rank breakdown = %+v, want one term per criterion", ranked[0].Breakdown)
	}

	impact, err := AnalyzeAdverseImpact(ranked, []string{"sex", "age_band"})
	if err != nil {
		t.Fatalf("AnalyzeAdverseImpact: %v", err)
	}
	// Selected: m1 (M, 40+), m3 (F, 40+). Rates: M 1/2, F 1/3 -> F ratio
	// (1/3)/(1/2) = 2/3 < 4/5, flagged. Age: 40+ 2/3, under-40 0/2 ->
	// under-40 ratio 0, flagged.
	byGroup := map[string]ImpactRatio{}
	for _, ratio := range impact.Ratios {
		byGroup[ratio.Dimension+"/"+ratio.Group] = ratio
	}
	female := byGroup["sex/F"]
	if female.Flagged != true || female.RatioToMax.Cmp(big.NewRat(2, 3)) != 0 {
		t.Fatalf("sex/F = %+v, want flagged at exactly 2/3", female)
	}
	male := byGroup["sex/M"]
	if male.Flagged || male.RatioToMax.Cmp(big.NewRat(1, 1)) != 0 {
		t.Fatalf("sex/M = %+v, want unflagged at 1", male)
	}
	under40 := byGroup["age_band/under-40"]
	if under40.Flagged != true || under40.RatioToMax.Sign() != 0 {
		t.Fatalf("age_band/under-40 = %+v, want flagged at 0", under40)
	}

	gaps, err := AnalyzePayEquity(ranked, "sex")
	if err != nil {
		t.Fatalf("AnalyzePayEquity: %v", err)
	}
	if len(gaps) != 1 || gaps[0].Cohort != "IC3" {
		t.Fatalf("gaps = %+v, want one IC3 cohort row", gaps)
	}
	// Cohort means: M (120000+95000)/2 = 107500, F 323000/3.
	// Gap = min/max = 107500 / (323000/3) = 322500/323000 = 645/646.
	if gaps[0].GapRat.Cmp(big.NewRat(645, 646)) != 0 {
		t.Fatalf("IC3 gap ratio = %s, want exactly 645/646", gaps[0].GapRat)
	}
	if gaps[0].LowerGroup != "M" || gaps[0].UpperGroup != "F" {
		t.Fatalf("IC3 gap groups = %s/%s, want M/F", gaps[0].LowerGroup, gaps[0].UpperGroup)
	}

	report, err := ComposeSelectionAnalysis(unit, criteria, 2, []string{"sex", "age_band"}, "sex")
	if err != nil {
		t.Fatalf("ComposeSelectionAnalysis: %v", err)
	}
	if !report.Privileged || report.Handling == "" {
		t.Fatalf("report = %+v, want privileged explainable evidence", report.Privileged)
	}
}

// TestTodo_WF_CAP_009_Golden pins the worked selection analysis.
func TestTodo_WF_CAP_009_Golden(t *testing.T) {
	report, err := ComposeSelectionAnalysis(selectionUnit(t), selectionCriteria(t), 2, []string{"sex", "age_band"}, "sex")
	if err != nil {
		t.Fatal(err)
	}
	got, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	path := filepath.Join("testdata", "wf_cap009_selection.golden")
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("selection analysis differs from golden %s\n--- got ---\n%s\n--- want ---\n%s", path, got, want)
	}
}

// TestTodo_WF_CAP_009_Property proves the analysis is deterministic and
// exact: ranking is stable under input permutation, the four-fifths flag
// matches exact rational arithmetic, and identical pay yields no gap.
func TestTodo_WF_CAP_009_Property(t *testing.T) {
	unit := selectionUnit(t)
	criteria := selectionCriteria(t)

	byID := map[string]SelectionMember{}
	for _, m := range unit {
		byID[m.ID] = m
	}
	permuted := []SelectionMember{byID["m4"], byID["m1"], byID["m5"], byID["m2"], byID["m3"]}
	first, err := RankDecisionalUnit(unit, criteria, 3)
	if err != nil {
		t.Fatal(err)
	}
	second, err := RankDecisionalUnit(permuted, criteria, 3)
	if err != nil {
		t.Fatal(err)
	}
	for i := range first {
		if first[i].Member.ID != second[i].Member.ID || !first[i].Score.Equal(second[i].Score) {
			t.Fatalf("ranking moved under permutation at %d: %s vs %s", i, first[i].Member.ID, second[i].Member.ID)
		}
	}

	impact, err := AnalyzeAdverseImpact(first, []string{"sex"})
	if err != nil {
		t.Fatal(err)
	}
	fourFifths := big.NewRat(4, 5)
	for _, ratio := range impact.Ratios {
		// rate_group / rate_max >= 4/5 unflagged, computed exactly:
		// selected_g * total_max * 5 ? selected_max * total_g * 4.
		var max ImpactRatio
		for _, other := range impact.Ratios {
			if other.Dimension == ratio.Dimension && (max.Group == "" || other.Rate.Cmp(max.Rate) > 0) {
				max = other
			}
		}
		left := new(big.Int).Mul(big.NewInt(int64(ratio.Selected*max.Total*5)), big.NewInt(1))
		right := new(big.Int).Mul(big.NewInt(int64(max.Selected*ratio.Total*4)), big.NewInt(1))
		wantFlagged := left.Cmp(right) < 0
		if ratio.Flagged != wantFlagged {
			t.Fatalf("group %s flagged = %v, exact arithmetic says %v", ratio.Group, ratio.Flagged, wantFlagged)
		}
		if !ratio.Flagged && ratio.RatioToMax.Cmp(fourFifths) < 0 {
			t.Fatalf("group %s unflagged below 4/5: %s", ratio.Group, ratio.RatioToMax)
		}
	}

	equal := selectionUnit(t)
	for i := range equal {
		equal[i].Pay = selectionDecimal(t, "100000")
	}
	equalRanked, err := RankDecisionalUnit(equal, criteria, 2)
	if err != nil {
		t.Fatal(err)
	}
	equalGaps, err := AnalyzePayEquity(equalRanked, "sex")
	if err != nil {
		t.Fatal(err)
	}
	for _, gap := range equalGaps {
		if gap.GapRat.Cmp(big.NewRat(1, 1)) != 0 {
			t.Fatalf("identical pay gap = %s, want exactly 1", gap.GapRat)
		}
	}

}
