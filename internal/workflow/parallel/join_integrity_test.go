package parallel

import (
	"errors"
	"reflect"
	"testing"
)

func TestTodo_WF_STEP_021(t *testing.T) {
	plans := []JoinPlan{
		{Strategy: JoinAll, Version: "v1"},
		{Strategy: JoinAny, Version: "v1"},
		{Strategy: JoinQuorum, Version: "v1", Quorum: 2},
		{Strategy: JoinRequiredSet, Version: "v1", RequiredID: []string{"a"}},
		{Strategy: JoinBestEffort, Version: "v1"},
	}
	for _, plan := range plans {
		t.Run(plan.Strategy, func(t *testing.T) {
			for name, results := range map[string][]BranchResult{
				"duplicate_success":     {{BranchID: "a", Outcome: OutcomeSucceeded}, {BranchID: "a", Outcome: OutcomeSucceeded}},
				"conflicting_duplicate": {{BranchID: "a", Outcome: OutcomeFailed}, {BranchID: "a", Outcome: OutcomeSucceeded}},
				"empty_identity":        {{BranchID: "", Outcome: OutcomeSucceeded}, {BranchID: "a", Outcome: OutcomeSucceeded}},
				"blank_identity":        {{BranchID: " \t", Outcome: OutcomeSucceeded}, {BranchID: "a", Outcome: OutcomeSucceeded}},
			} {
				t.Run(name, func(t *testing.T) {
					got, err := Join(plan, results)
					if !errors.Is(err, ErrJoinPlan) || !reflect.DeepEqual(got, JoinOutcome{}) {
						t.Fatalf("invalid evidence aggregated: %+v, %v", got, err)
					}
				})
			}
		})
	}
	for _, required := range [][]string{{"a", "a"}, {""}, {" \t"}} {
		got, err := Join(JoinPlan{Strategy: JoinRequiredSet, Version: "v1", RequiredID: required}, []BranchResult{{BranchID: "a", Outcome: OutcomeSucceeded}})
		if !errors.Is(err, ErrJoinPlan) || !reflect.DeepEqual(got, JoinOutcome{}) {
			t.Fatalf("invalid required set %q: %+v, %v", required, got, err)
		}
	}
	valid, err := Join(JoinPlan{Strategy: JoinQuorum, Version: "v1", Quorum: 2}, []BranchResult{
		{BranchID: "a", Outcome: OutcomeSucceeded}, {BranchID: "b", Outcome: OutcomeSucceeded},
	})
	if err != nil || valid.Verdict != JoinSucceeded || valid.Counted != 2 {
		t.Fatalf("distinct quorum: %+v, %v", valid, err)
	}
}

func TestTodo_WF_STEP_021_Mutation(t *testing.T) {
	for _, first := range []string{OutcomeUnknown, OutcomeFailed, OutcomeCancelled} {
		for _, required := range [][]string{{"a", "missing"}, {"missing", "a"}} {
			got, err := Join(JoinPlan{Strategy: JoinRequiredSet, Version: "v1", RequiredID: required}, []BranchResult{{BranchID: "a", Outcome: first}})
			if !errors.Is(err, ErrJoinMissing) || !reflect.DeepEqual(got, JoinOutcome{}) {
				t.Fatalf("%s hid a missing required branch in %v: %+v, %v", first, required, got, err)
			}
		}
	}
	for _, failed := range []string{OutcomeFailed, OutcomeCancelled} {
		results := []BranchResult{{BranchID: "a", Outcome: OutcomeUnknown}, {BranchID: "b", Outcome: failed}}
		for _, required := range [][]string{{"a", "b"}, {"b", "a"}} {
			plan := JoinPlan{Strategy: JoinRequiredSet, Version: "v1", RequiredID: required}
			for range 2 {
				got, err := Join(plan, results)
				if err != nil || got.Verdict != JoinFailed || !reflect.DeepEqual(got.Unknown, []string{"a"}) || got.Counted != 2 {
					t.Fatalf("required failure must dominate without dropping unknown evidence: %+v, %v", got, err)
				}
				results[0], results[1] = results[1], results[0]
			}
		}
	}
}
