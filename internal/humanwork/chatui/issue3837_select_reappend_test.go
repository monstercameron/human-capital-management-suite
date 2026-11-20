package chatui

import "testing"

// TestIssue3837_SelectReappendKeepsTheChoiceOnlyWhenOptionsAreMoved pins when the
// option chosen before an append is chosen again after it. The browser half
// (issue3837_select_reappend_js_test.go) runs the same decision against a select
// that behaves as a browser's does.
func TestIssue3837_SelectReappendKeepsTheChoiceOnlyWhenOptionsAreMoved(t *testing.T) {
	cases := []struct {
		name               string
		multiple           bool
		chosen             int
		moves, bringsOther bool
		want               bool
	}{
		{name: "the select's own options are appended again", chosen: 0, moves: true, want: true},
		{name: "a later option was chosen by the person", chosen: 3, moves: true, want: true},
		{name: "first mount moves nothing", chosen: -1, moves: false, want: false},
		{name: "new options join an untouched select", chosen: 0, moves: false, want: false},
		{name: "nothing was chosen", chosen: -1, moves: true, want: false},
		{name: "several choices are left to the browser", multiple: true, chosen: 0, moves: true, want: false},
		{name: "a new option arrives chosen and wins", chosen: 0, moves: true, bringsOther: true, want: false},
	}
	for _, c := range cases {
		if got := selectReappendKeepsChoice(c.multiple, c.chosen, c.moves, c.bringsOther); got != c.want {
			t.Errorf("%s: keep choice = %v, want %v", c.name, got, c.want)
		}
	}
}
