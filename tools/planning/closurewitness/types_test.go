package closurewitness

import "testing"

func TestClassesAreTheTwelveChainClassesInOrder(t *testing.T) {
	want := []EdgeClass{"SOURCE", "PHASE_GATE", "SLICE", "MODEL", "ENGINE", "CAPABILITY", "HANDLER", "ENDPOINT", "SCENARIO", "TEST", "TODO", "EVIDENCE"}
	got := Classes()
	if len(got) != len(want) {
		t.Fatalf("Classes() has %d entries, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] || !got[i].Valid() || classRank(got[i]) != i {
			t.Errorf("class %d = %s (valid=%v rank=%d), want %s", i, got[i], got[i].Valid(), classRank(got[i]), want[i])
		}
	}
	if EdgeClass("WORKFLOW").Valid() {
		t.Error("an undeclared class reported valid")
	}
}

func TestDefectCodesMapToTheStateTheyForce(t *testing.T) {
	cases := map[string]EdgeState{
		DefectAbsent:            StateAbsent,
		DefectReverseAbsent:     StateAbsent,
		DefectDuplicate:         StateDuplicate,
		DefectAggregateOnly:     StateAggregateOnly,
		DefectUnsourced:         StateUnsourced,
		DefectStale:             StateStale,
		DefectOrphan:            StateStale,
		DefectMaturityOverclaim: StateStale,
	}
	for code, want := range cases {
		if got := defectState(code); got != want {
			t.Errorf("defectState(%s) = %s, want %s", code, got, want)
		}
	}
	order := []EdgeState{StateAbsent, StateUnsourced, StateDuplicate, StateStale, StateAggregateOnly, StateJustified, StateBound}
	for i := 1; i < len(order); i++ {
		if stateRank(order[i-1]) >= stateRank(order[i]) {
			t.Errorf("%s must rank worse than %s", order[i-1], order[i])
		}
	}
}
