package main

import "testing"

func TestIssue3981_RegionFollowsTheAddressOnCloseBackAndForward(t *testing.T) {
	cases := []struct {
		name     string
		rendered string
		query    string
		want     bool
	}{
		{"close leaves the detail drawn", "task-1", "?locale=en-US", true},
		{"close with nothing drawn", "", "?locale=en-US", false},
		{"back to the first task while the second is drawn", "task-2", "?task=task-1", true},
		{"forward to the second task while the first is drawn", "task-1", "?task=task-2", true},
		{"in step", "task-1", "?task=task-1&locale=de-DE", false},
		{"opening a task from a task-free page", "", "?task=task-9", true},
		{"a null placeholder is no task", "task-1", "?task=null", true},
		{"padding around the drawn id is ignored", " task-1 ", "?task=task-1", false},
	}
	for _, c := range cases {
		if got := agentSelectionOutOfSync(c.rendered, c.query); got != c.want {
			t.Errorf("%s: agentSelectionOutOfSync(%q, %q) = %v, want %v", c.name, c.rendered, c.query, got, c.want)
		}
	}
	if len(agentSelectionReconcileDelays) < 2 || agentSelectionReconcileDelays[0] != 0 {
		t.Fatalf("the first look must be immediate and a later one must follow: %v", agentSelectionReconcileDelays)
	}
}
