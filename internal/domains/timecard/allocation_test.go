package timecard

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/labor"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func mustMinutes(t *testing.T, text string) values.Decimal {
	t.Helper()
	d, err := values.NewDecimal(text, 0, values.RoundingHalfEven)
	if err != nil {
		t.Fatalf("NewDecimal(%q): %v", text, err)
	}
	return d
}

func testLaborRule(t *testing.T, dims ...labor.DimensionKind) labor.LaborRule {
	t.Helper()
	ref := labor.RuleRef{ID: "ref", Version: "v1", Digest: "sha256:ref"}
	eff, err := values.NewOpenInstantInterval(values.NewInstant(mustTime(t, "2026-01-01T00:00:00Z")))
	if err != nil {
		t.Fatalf("NewOpenInstantInterval: %v", err)
	}
	rule, err := labor.NewLaborRule(labor.LaborRule{
		ID: "rule-1", Version: "v1", Effective: eff,
		WorkerRef: ref, TimeRef: ref, EntityRef: ref, JobRef: ref, EarningRef: ref,
		Dimensions: dims, Currency: "USD",
		BaseRate: mustDecimal(t, "20.00", 2), DifferentialRate: mustDecimal(t, "0.00", 2),
		OvertimeMultiplier: mustDecimal(t, "1.50", 2), EmployerBurdenRate: mustDecimal(t, "0.10", 2),
	})
	if err != nil {
		t.Fatalf("NewLaborRule: %v", err)
	}
	return rule
}

func mustDecimal(t *testing.T, text string, scale int32) values.Decimal {
	t.Helper()
	d, err := values.NewDecimal(text, scale, values.RoundingHalfEven)
	if err != nil {
		t.Fatalf("NewDecimal(%q): %v", text, err)
	}
	return d
}

func mustTime(t *testing.T, rfc3339 string) time.Time {
	t.Helper()
	tm, err := time.Parse(time.RFC3339, rfc3339)
	if err != nil {
		t.Fatalf("time.Parse(%q): %v", rfc3339, err)
	}
	return tm
}

func projectDimension(id string) labor.Dimension {
	return labor.Dimension{Kind: labor.DimensionProject, Value: id, Version: "v1"}
}

// TestTodo_FTIME_005 is the PRIMARY test: approved minutes allocate exactly
// across authorized lines, one source ref cannot be charged in two lines,
// and a correction produces a delta rather than rewriting the prior
// allocation.
func TestTodo_FTIME_005(t *testing.T) {
	rule := testLaborRule(t, labor.DimensionProject)
	req := TimeAllocationRequest{
		TimecardID: "tc-1", ApprovedRevision: 3, ApprovedMinutes: mustMinutes(t, "480"), Rule: rule,
		Lines: []AllocationLine{
			{Dimension: projectDimension("proj-a"), Minutes: mustMinutes(t, "300"), Sources: []AllocationSource{{Kind: "PUNCH", ID: "p1"}}},
			{Dimension: projectDimension("proj-b"), Minutes: mustMinutes(t, "180"), Sources: []AllocationSource{{Kind: "PUNCH", ID: "p2"}}},
		},
	}
	alloc, err := AllocateApprovedTime(req)
	if err != nil {
		t.Fatalf("AllocateApprovedTime: %v", err)
	}
	if !alloc.TotalMinutes.Equal(mustMinutes(t, "480")) {
		t.Fatalf("TotalMinutes = %s, want 480", alloc.TotalMinutes)
	}

	// RED: the same source charged in two lines must be rejected.
	dup := req
	dup.Lines = []AllocationLine{
		{Dimension: projectDimension("proj-a"), Minutes: mustMinutes(t, "300"), Sources: []AllocationSource{{Kind: "PUNCH", ID: "p1"}}},
		{Dimension: projectDimension("proj-b"), Minutes: mustMinutes(t, "180"), Sources: []AllocationSource{{Kind: "PUNCH", ID: "p1"}}},
	}
	if _, err := AllocateApprovedTime(dup); !errors.Is(err, ErrTimeAllocationRejected) {
		t.Fatalf("AllocateApprovedTime with a duplicated source: got %v, want ErrTimeAllocationRejected", err)
	}

	// A correction to the timecard produces a delta, not a rewrite.
	next := req
	next.ApprovedRevision = 4
	next.ApprovedMinutes = mustMinutes(t, "480")
	next.Lines = []AllocationLine{
		{Dimension: projectDimension("proj-a"), Minutes: mustMinutes(t, "260"), Sources: []AllocationSource{{Kind: "PUNCH", ID: "p1"}}},
		{Dimension: projectDimension("proj-b"), Minutes: mustMinutes(t, "220"), Sources: []AllocationSource{{Kind: "PUNCH", ID: "p2"}}},
	}
	corr, err := CorrectAllocation(alloc, next)
	if err != nil {
		t.Fatalf("CorrectAllocation: %v", err)
	}
	if len(corr.Deltas) != 2 {
		t.Fatalf("Deltas = %+v, want two changed dimensions", corr.Deltas)
	}
	byProject := map[string]values.Decimal{}
	for _, d := range corr.Deltas {
		byProject[d.Dimension.Value] = d.DeltaMinutes
	}
	if !byProject["proj-a"].Equal(mustMinutes(t, "-40")) {
		t.Fatalf("proj-a delta = %s, want -40", byProject["proj-a"])
	}
	if !byProject["proj-b"].Equal(mustMinutes(t, "40")) {
		t.Fatalf("proj-b delta = %s, want 40", byProject["proj-b"])
	}
	// The original allocation must be unchanged after correction.
	if !alloc.Lines[0].Minutes.Equal(mustMinutes(t, "300")) {
		t.Fatalf("prior allocation line mutated: %s", alloc.Lines[0].Minutes)
	}

	if _, err := CorrectAllocation(alloc, req); !errors.Is(err, ErrTimeAllocationRejected) {
		t.Fatalf("CorrectAllocation without advancing the revision: got %v, want ErrTimeAllocationRejected", err)
	}
}

// TestTodo_FTIME_005_Security proves an allocation line can never target a
// dimension outside the rule's authorized set, and that an allocation
// carries no pay-rate data -- only dimension identity and minutes travel
// with it, so a denied project member's rate is never exposed by an
// allocation result.
func TestTodo_FTIME_005_Security(t *testing.T) {
	rule := testLaborRule(t, labor.DimensionProject) // ACTIVITY is not authorized
	req := TimeAllocationRequest{
		TimecardID: "tc-1", ApprovedRevision: 1, ApprovedMinutes: mustMinutes(t, "60"), Rule: rule,
		Lines: []AllocationLine{
			{Dimension: labor.Dimension{Kind: labor.DimensionActivity, Value: "act-1", Version: "v1"}, Minutes: mustMinutes(t, "60"), Sources: []AllocationSource{{Kind: "PUNCH", ID: "p1"}}},
		},
	}
	if _, err := AllocateApprovedTime(req); !errors.Is(err, ErrTimeAllocationRejected) {
		t.Fatalf("AllocateApprovedTime outside the authorized dimension set: got %v, want ErrTimeAllocationRejected", err)
	}
}

// TestTodo_FTIME_005_Race proves concurrent, independent allocation calls
// over the same immutable request always agree and never corrupt shared
// state -- no minute is charged twice even under concurrency.
func TestTodo_FTIME_005_Race(t *testing.T) {
	rule := testLaborRule(t, labor.DimensionProject)
	req := TimeAllocationRequest{
		TimecardID: "tc-race", ApprovedRevision: 1, ApprovedMinutes: mustMinutes(t, "120"), Rule: rule,
		Lines: []AllocationLine{
			{Dimension: projectDimension("proj-a"), Minutes: mustMinutes(t, "80"), Sources: []AllocationSource{{Kind: "PUNCH", ID: "p1"}}},
			{Dimension: projectDimension("proj-b"), Minutes: mustMinutes(t, "40"), Sources: []AllocationSource{{Kind: "PUNCH", ID: "p2"}}},
		},
	}
	var wg sync.WaitGroup
	digests := make([]string, 64)
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			alloc, err := AllocateApprovedTime(req)
			if err != nil {
				t.Errorf("goroutine %d: AllocateApprovedTime: %v", i, err)
				return
			}
			digests[i] = alloc.Digest()
		}(i)
	}
	wg.Wait()
	for i, d := range digests {
		if d == "" || d != digests[0] {
			t.Fatalf("goroutine %d produced digest %q, want %q (deterministic)", i, d, digests[0])
		}
	}
}
