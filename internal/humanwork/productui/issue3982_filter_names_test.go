package productui

import (
	"regexp"
	"strings"
	"testing"
)

func TestIssue3982_TaskFilterNamesIncludeCounts(t *testing.T) {
	snapshot := AgentSnapshot{Availability: AgentsAvailable, Tasks: []AgentTask{
		{ID: "d1", State: AgentTaskCompleted}, {ID: "d2", State: AgentTaskCompleted}, {ID: "d3", State: AgentTaskCompleted},
		{ID: "r1", State: AgentTaskRunning},
	}}
	tab := func(markup, category string) string {
		m := regexp.MustCompile(`<button[^>]*data-agent-task-filter="` + category + `"[^>]*>`).FindString(markup)
		if m == "" {
			t.Fatalf("no %s filter tab in %s", category, markup)
		}
		return m
	}
	for _, tc := range []struct {
		locale, category string
		want             string
	}{
		{"en-US", "completed", `aria-label="Completed, 3 tasks"`},
		{"en-US", "active", `aria-label="Active, 1 task"`},
		{"en-US", "failed", `aria-label="Failed, 0 tasks"`},
		{"de-DE", "completed", `aria-label="Abgeschlossen, 3 Aufgaben"`},
		{"de-DE", "active", `aria-label="Aktiv, 1 Aufgabe"`},
		{"ar", "completed", `aria-label="مكتملة، ٣ مهام"`},
		{"ar", "active", `aria-label="نشطة، مهمة واحدة"`},
	} {
		markup := renderAgentUXPage5(t, tc.locale, snapshot, false)
		if got := tab(markup, tc.category); !strings.Contains(got, tc.want) {
			t.Fatalf("%s %s tab = %s, want %s", tc.locale, tc.category, got, tc.want)
		}
		if strings.Contains(markup, `aria-label="Task count"`) {
			t.Fatalf("the count still carries a name that hides its number: %s", markup)
		}
	}
}
