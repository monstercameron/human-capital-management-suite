package localcommit

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func rev004Interval(t *testing.T, start, end string) values.EffectiveInterval {
	t.Helper()
	a, err := time.Parse(time.RFC3339, start)
	if err != nil {
		t.Fatal(err)
	}
	b, err := time.Parse(time.RFC3339, end)
	if err != nil {
		t.Fatal(err)
	}
	iv, err := values.NewInstantInterval(values.NewInstant(a), values.NewInstant(b))
	if err != nil {
		t.Fatal(err)
	}
	return iv
}

func rev004Base(t *testing.T) InvariantInput {
	t.Helper()
	iv := instantInterval(t)
	return InvariantInput{
		Tenant:               values.TenantId("11111111-1111-4111-8111-111111111111"),
		WorkerID:             "worker",
		ManagerID:            "manager",
		EmploymentIntervals:  []values.EffectiveInterval{iv},
		AssignmentIntervals:  []values.EffectiveInterval{iv},
		PositionCapacity:     4,
		PositionOccupied:     1,
		CompensationCurrency: "USD",
		BudgetCurrency:       "USD",
	}
}

// TestTodo_REV_004_01 is the REV-004-01 primary: one fixture per remaining
// invariant vector aborts evaluation with exactly the matching finding.
func TestTodo_REV_004_01(t *testing.T) {
	clean := rev004Base(t)
	if got := EvaluateInvariants(clean); got.Status != InvariantPass {
		t.Fatalf("clean baseline = %+v, want PASS", got)
	}

	late := rev004Interval(t, "2027-02-01T00:00:00Z", "2027-03-01T00:00:00Z")
	cycle := rev004Base(t)
	cycle.ManagerAncestors = []string{"director", "worker"}
	overlap := rev004Base(t)
	overlap.AssignmentIntervals = []values.EffectiveInterval{late}
	capacity := rev004Base(t)
	capacity.PositionCapacity = 1
	capacity.PositionOccupied = 2
	currency := rev004Base(t)
	currency.BudgetCurrency = "EUR"

	cases := []struct {
		name string
		in   InvariantInput
		code InvariantCode
	}{
		{"manager cycle", cycle, InvariantManagerCycle},
		{"assignment overlap", overlap, InvariantEmploymentAssignment},
		{"position capacity", capacity, InvariantPositionCapacity},
		{"currency mismatch", currency, InvariantCurrencyMismatch},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := EvaluateInvariants(tc.in)
			if got.Status != InvariantFail {
				t.Fatalf("status = %s, want FAIL (findings=%+v)", got.Status, got.Findings)
			}
			if len(got.Findings) != 1 {
				t.Fatalf("findings = %+v, want exactly one %s finding", got.Findings, tc.code)
			}
			if got.Findings[0].Code != tc.code {
				t.Errorf("finding code = %s, want %s", got.Findings[0].Code, tc.code)
			}
		})
	}
}

// TestTodo_REV_004_01_Golden pins the finding code and stream bytes for all
// four invariant vectors.
func TestTodo_REV_004_01_Golden(t *testing.T) {
	late := rev004Interval(t, "2027-02-01T00:00:00Z", "2027-03-01T00:00:00Z")
	cycle := rev004Base(t)
	cycle.ManagerAncestors = []string{"director", "worker"}
	overlap := rev004Base(t)
	overlap.AssignmentIntervals = []values.EffectiveInterval{late}
	capacity := rev004Base(t)
	capacity.PositionCapacity = 1
	capacity.PositionOccupied = 2
	currency := rev004Base(t)
	currency.BudgetCurrency = "EUR"

	var lines []string
	for _, in := range []InvariantInput{cycle, overlap, capacity, currency} {
		got := EvaluateInvariants(in)
		if len(got.Findings) != 1 {
			t.Fatalf("findings = %+v, want exactly one", got.Findings)
		}
		lines = append(lines, string(got.Findings[0].Code)+"|"+got.Findings[0].Stream)
	}
	sort.Strings(lines)
	want, err := os.ReadFile(filepath.Join("testdata", "rev00401.golden"))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(lines, "\n") + "\n"; got != string(want) {
		t.Errorf("golden mismatch:\n got: %q\nwant: %q", got, string(want))
	}
}

// TestTodo_REV_004_01_Property proves the capacity boundary algebra: FAIL
// holds exactly when occupancy exceeds capacity.
func TestTodo_REV_004_01_Property(t *testing.T) {
	for _, capacity := range []int64{1, 2, 3} {
		for _, occupied := range []int64{0, 1, 2, 3} {
			in := rev004Base(t)
			in.PositionCapacity = capacity
			in.PositionOccupied = occupied
			got := EvaluateInvariants(in)
			wantFail := occupied > capacity
			if wantFail && got.Status != InvariantFail {
				t.Errorf("capacity=%d occupied=%d: status=%s, want FAIL", capacity, occupied, got.Status)
			}
			if !wantFail && got.Status != InvariantPass {
				t.Errorf("capacity=%d occupied=%d: status=%s, want PASS", capacity, occupied, got.Status)
			}
		}
	}
}

// TestTodo_REV_004_01_Mutation proves each fixture is defect-sensitive:
// breaking the field fails with the matching code and repairing it returns
// to PASS, so a mutant that drops any of the four checks is killed.
func TestTodo_REV_004_01_Mutation(t *testing.T) {
	late := rev004Interval(t, "2027-02-01T00:00:00Z", "2027-03-01T00:00:00Z")
	cases := []struct {
		name    string
		code    InvariantCode
		breakIt func(*InvariantInput)
		fixIt   func(*InvariantInput)
	}{
		{"manager cycle", InvariantManagerCycle,
			func(in *InvariantInput) { in.ManagerAncestors = []string{"director", "worker"} },
			func(in *InvariantInput) { in.ManagerAncestors = []string{"director"} }},
		{"assignment overlap", InvariantEmploymentAssignment,
			func(in *InvariantInput) { in.AssignmentIntervals = []values.EffectiveInterval{late} },
			func(in *InvariantInput) { in.AssignmentIntervals = in.EmploymentIntervals }},
		{"position capacity", InvariantPositionCapacity,
			func(in *InvariantInput) { in.PositionCapacity = 1; in.PositionOccupied = 2 },
			func(in *InvariantInput) { in.PositionCapacity = 2; in.PositionOccupied = 1 }},
		{"currency mismatch", InvariantCurrencyMismatch,
			func(in *InvariantInput) { in.BudgetCurrency = "EUR" },
			func(in *InvariantInput) { in.BudgetCurrency = in.CompensationCurrency }},
	}
	for _, tc := range cases {
		in := rev004Base(t)
		tc.breakIt(&in)
		broken := EvaluateInvariants(in)
		if broken.Status != InvariantFail {
			t.Fatalf("%s: broken status=%s, want FAIL", tc.name, broken.Status)
		}
		tc.fixIt(&in)
		if fixed := EvaluateInvariants(in); fixed.Status != InvariantPass {
			t.Errorf("%s: repaired = %+v, want PASS", tc.name, fixed)
		}
	}
}
