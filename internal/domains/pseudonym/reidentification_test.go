package pseudonym

import (
	"strings"
	"testing"
)

func reidentPolicy() ReidentificationPolicy {
	return ReidentificationPolicy{
		MinCellSize:      10,
		MaxExportRows:    1000,
		ReviewQueue:      "privacy-review",
		QuasiIdentifiers: []string{"postal_code", "birth_year", "department"},
	}
}

func reidentRecord() OutputRecord {
	return OutputRecord{
		QueryRef:   "q1",
		FilterSig:  "department=eng&year=2026",
		RowCount:   500,
		CellSizes:  []int{120, 200, 180},
		Columns:    []string{"headcount", "attrition_rate"},
		Embedding:  false,
		JoinedWith: "",
		Exported:   false,
	}
}

// TestTodo_ANON_008 is the primary ANON-008 contract test: analytics and
// search outputs that could re-identify suppress, generalize or require
// review instead of releasing.
func TestTodo_ANON_008(t *testing.T) {
	t.Run("safe aggregate output is allowed", func(t *testing.T) {
		got, err := ScreenOutput(reidentPolicy(), reidentRecord(), nil)
		if err != nil {
			t.Fatalf("ScreenOutput: %v", err)
		}
		if got.Verdict != VerdictAllow {
			t.Fatalf("verdict = %v, want ALLOW", got.Verdict)
		}
	})

	t.Run("small cohorts suppress", func(t *testing.T) {
		rec := reidentRecord()
		rec.CellSizes = []int{120, 4, 180}
		got, err := ScreenOutput(reidentPolicy(), rec, nil)
		if err != nil {
			t.Fatal(err)
		}
		if got.Verdict != VerdictSuppress {
			t.Fatalf("verdict = %v, want SUPPRESS", got.Verdict)
		}
		tiny := reidentRecord()
		tiny.RowCount = 3
		tiny.CellSizes = []int{3}
		got, err = ScreenOutput(reidentPolicy(), tiny, nil)
		if err != nil {
			t.Fatal(err)
		}
		if got.Verdict != VerdictSuppress {
			t.Fatalf("verdict = %v, want SUPPRESS", got.Verdict)
		}
	})

	t.Run("differencing filters require review", func(t *testing.T) {
		history := []OutputRecord{reidentRecord()}
		repeat := reidentRecord()
		repeat.QueryRef = "q2"
		repeat.FilterSig = "department=eng&year=2026&status=active"
		repeat.RowCount = 499
		got, err := ScreenOutput(reidentPolicy(), repeat, history)
		if err != nil {
			t.Fatal(err)
		}
		if got.Verdict != VerdictReviewRequired {
			t.Fatalf("verdict = %v, want REVIEW_REQUIRED", got.Verdict)
		}
		if got.Queue != "privacy-review" {
			t.Fatalf("review must name its queue: %+v", got)
		}
	})

	t.Run("quasi-identifier exports generalize or suppress", func(t *testing.T) {
		rec := reidentRecord()
		rec.Columns = []string{"headcount", "postal_code", "birth_year"}
		rec.Exported = true
		got, err := ScreenOutput(reidentPolicy(), rec, nil)
		if err != nil {
			t.Fatal(err)
		}
		if got.Verdict != VerdictGeneralize && got.Verdict != VerdictSuppress {
			t.Fatalf("verdict = %v, want GENERALIZE or SUPPRESS", got.Verdict)
		}
		if len(got.DroppedColumns) == 0 {
			t.Fatalf("screen must name the unsafe columns: %+v", got)
		}
	})

	t.Run("cross-dataset joins and large embedding exports require review", func(t *testing.T) {
		joined := reidentRecord()
		joined.JoinedWith = "dataset:recruiting"
		got, err := ScreenOutput(reidentPolicy(), joined, nil)
		if err != nil {
			t.Fatal(err)
		}
		if got.Verdict != VerdictReviewRequired {
			t.Fatalf("verdict = %v, want REVIEW_REQUIRED", got.Verdict)
		}
		emb := reidentRecord()
		emb.Embedding = true
		emb.RowCount = 5000
		emb.Exported = true
		got, err = ScreenOutput(reidentPolicy(), emb, nil)
		if err != nil {
			t.Fatal(err)
		}
		if got.Verdict != VerdictReviewRequired && got.Verdict != VerdictSuppress {
			t.Fatalf("verdict = %v, want REVIEW_REQUIRED or SUPPRESS", got.Verdict)
		}
	})

	t.Run("invalid policy and records fail closed", func(t *testing.T) {
		bad := reidentPolicy()
		bad.MinCellSize = 0
		if _, err := ScreenOutput(bad, reidentRecord(), nil); err == nil {
			t.Fatal("zero cell floor must fail closed")
		}
		empty := reidentRecord()
		empty.QueryRef = ""
		if _, err := ScreenOutput(reidentPolicy(), empty, nil); err == nil {
			t.Fatal("unidentified query must fail closed")
		}
	})
}

// TestTodo_ANON_008_Security proves unsafe results never release identifying
// output and denials leak no row content.
func TestTodo_ANON_008_Security(t *testing.T) {
	rec := reidentRecord()
	rec.RowCount = 2
	rec.CellSizes = []int{2}
	rec.Columns = []string{"postal_code", "diagnosis"}
	got, err := ScreenOutput(reidentPolicy(), rec, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Verdict == VerdictAllow {
		t.Fatal("identifying output must never be allowed")
	}
	for _, reason := range got.Reasons {
		if strings.Contains(reason, "diagnosis") {
			t.Fatalf("denial must not echo sensitive columns: %q", reason)
		}
	}
}

// TestTodo_ANON_008_Conformance runs the versioned policy table: every
// cohort size maps to exactly one expected verdict.
func TestTodo_ANON_008_Conformance(t *testing.T) {
	policy := reidentPolicy()
	table := []struct {
		rows    int
		cells   []int
		verdict ScreenVerdict
	}{
		{500, []int{200, 300}, VerdictAllow},
		{50, []int{25, 25}, VerdictAllow},
		{11, []int{11}, VerdictAllow},
		{9, []int{9}, VerdictSuppress},
		{500, []int{490, 9}, VerdictSuppress},
		{1, []int{1}, VerdictSuppress},
	}
	for _, tc := range table {
		rec := reidentRecord()
		rec.RowCount = tc.rows
		rec.CellSizes = tc.cells
		got, err := ScreenOutput(policy, rec, nil)
		if err != nil {
			t.Fatalf("rows=%d: %v", tc.rows, err)
		}
		if got.Verdict != tc.verdict {
			t.Fatalf("rows=%d cells=%v: verdict=%v want %v", tc.rows, tc.cells, got.Verdict, tc.verdict)
		}
	}
}
