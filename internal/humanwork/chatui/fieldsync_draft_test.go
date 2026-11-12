package chatui

import "testing"

// Issue #3629: a valid unsaved milestone title or due date must survive an
// unrelated composer re-render (a sweep) after focus has left the box.
func TestFieldDraftKept_Issue3629(t *testing.T) {
	cases := []struct {
		name                     string
		sweep, typed, hasApplied bool
		applied, want            string
		keep                     bool
	}{
		{name: "typed draft survives sweep when saved value unchanged", sweep: true, typed: true, hasApplied: true, applied: "QA audit only — milestone self test", want: "QA audit only — milestone self test", keep: true},
		{name: "typed empty due date survives sweep", sweep: true, typed: true, hasApplied: true, applied: "", want: "", keep: true},
		{name: "application value change replaces draft", sweep: true, typed: true, hasApplied: true, applied: "old", want: "new", keep: false},
		{name: "attribute record path always applies", sweep: false, typed: true, hasApplied: true, applied: "saved", want: "saved", keep: false},
		{name: "untyped box is not a draft", sweep: true, typed: false, hasApplied: true, applied: "saved", want: "saved", keep: false},
		{name: "box never given a value has no draft to keep", sweep: true, typed: true, hasApplied: false, applied: "", want: "", keep: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := fieldDraftKept(tc.sweep, tc.typed, tc.hasApplied, tc.applied, tc.want); got != tc.keep {
				t.Fatalf("fieldDraftKept(sweep=%v, typed=%v, hasApplied=%v, applied=%q, want=%q) = %v, want %v",
					tc.sweep, tc.typed, tc.hasApplied, tc.applied, tc.want, got, tc.keep)
			}
		})
	}
}
