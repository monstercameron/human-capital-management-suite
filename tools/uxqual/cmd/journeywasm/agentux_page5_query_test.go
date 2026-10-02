package main

import "testing"

func TestAgentUXPage5_SelectedTaskQuery(t *testing.T) {
	for _, tc := range []struct {
		query string
		want  string
	}{
		{"", ""}, {"?locale=de-DE", ""}, {"?task=task-7", "task-7"}, {"?task=%20task-8%20", "task-8"}, {"?task=%zz", ""},
	} {
		if got := selectedAgentTaskID(tc.query); got != tc.want {
			t.Fatalf("selectedAgentTaskID(%q) = %q, want %q", tc.query, got, tc.want)
		}
	}
}

func TestAgentUXPage5_StoredTaskFilterNeedsRows(t *testing.T) {
	counts := map[string]int{"active": 0, "completed": 7, "failed": 4}
	if got := preferredAgentTaskFilter("active", "", counts); got != "completed" {
		t.Fatalf("empty stored tab won: %q", got)
	}
	if got := preferredAgentTaskFilter("failed", "", counts); got != "failed" {
		t.Fatalf("populated stored tab ignored: %q", got)
	}
	if got := preferredAgentTaskFilter("failed", "completed", counts); got != "completed" {
		t.Fatalf("selected task tab did not win: %q", got)
	}
}
