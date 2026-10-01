package attendance

import (
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/punchpolicy"
)

func TestTodo_TCLOCK_009_AttendancePolicyBridge(t *testing.T) {
	start := time.Date(2026, 1, 2, 9, 0, 0, 0, time.UTC)
	end := start.Add(8 * time.Hour)
	policy := punchpolicy.Policy{
		ID: "site-policy", Version: 3, Jurisdiction: "US-CA",
		Rounding:   punchpolicy.Rounding{IncrementMinutes: 15, Mode: punchpolicy.RoundNearest},
		AutoDeduct: punchpolicy.AutoDeduct{AfterMinutes: 360, DeductMinutes: 30, WaivedByAttestation: true},
	}
	req := PolicyEvaluationRequest{
		Attendance: Request{
			WorkerID:     "worker-1",
			Schedule:     Schedule{Ref: VersionedRef{ID: "schedule", Version: "2"}, Shifts: []Shift{{ID: "shift-1", Interval: Interval{Start: start, End: end}}}},
			Punches:      []Punch{{ID: "in", Ref: VersionedRef{ID: "punch", Version: "1"}, At: start, Direction: PunchIn}, {ID: "out", Ref: VersionedRef{ID: "punch", Version: "2"}, At: end, Direction: PunchOut}},
			Jurisdiction: Jurisdiction{Ref: VersionedRef{ID: "jurisdiction", Version: "1"}, Code: "US-CA"},
			Rules:        RuleSet{Ref: VersionedRef{ID: "rules", Version: "1"}, Name: "attendance"},
			Tolerance:    Tolerance{Ref: VersionedRef{ID: "tolerance", Version: "1"}},
			Context:      EffectiveContext{EffectiveAt: start.Add(-24 * time.Hour), KnownAt: start, Timezone: "UTC", Calendar: "gregorian"},
		},
		Intervals: []PolicyInterval{{
			ShiftID: "shift-1", Raw: punchpolicy.Interval{Start: start.Add(2 * time.Minute), End: end.Add(-2 * time.Minute)},
			Policy: policy, Shift: punchpolicy.Shift{ID: "shift-1", Published: true, Interval: punchpolicy.Interval{Start: start, End: end}},
			Attestations: []punchpolicy.Attestation{{Kind: punchpolicy.MealTakenAttestation, Taken: false}},
		}},
	}
	got, err := EvaluateWithPunchPolicies(req)
	if err != nil {
		t.Fatal(err)
	}
	if got.Attendance.Outcome != Compliant {
		t.Fatalf("attendance outcome = %s, want compliant", got.Attendance.Outcome)
	}
	if len(got.Evaluations) != 1 || got.Evaluations[0].Result.PolicyVersion != 3 {
		t.Fatalf("policy result did not pin version: %+v", got.Evaluations)
	}
	evaluated := got.Evaluations[0].Result
	if evaluated.RawStart != req.Intervals[0].Raw.Start || evaluated.RawEnd != req.Intervals[0].Raw.End {
		t.Fatal("raw interval was rewritten")
	}
	if evaluated.AutoDeductMinutes != 0 {
		t.Fatal("worker attestation should waive auto-deduction")
	}
}

func TestTodo_TCLOCK_009_AttendancePolicyBridgeRejectsDuplicateOrUnknownShift(t *testing.T) {
	base := PolicyEvaluationRequest{Attendance: Request{Schedule: Schedule{Shifts: []Shift{{ID: "known"}}}}}
	for _, tc := range []struct {
		name string
		ids  []string
		want string
	}{
		{name: "unknown", ids: []string{"missing"}, want: "unknown shift"},
		{name: "duplicate", ids: []string{"known", "known"}, want: "duplicate"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := base
			for _, id := range tc.ids {
				req.Intervals = append(req.Intervals, PolicyInterval{ShiftID: id, Shift: punchpolicy.Shift{ID: id}})
			}
			_, err := EvaluateWithPunchPolicies(req)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
		})
	}
}
