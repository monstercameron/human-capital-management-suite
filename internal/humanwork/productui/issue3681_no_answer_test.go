package productui

import (
	"strings"
	"testing"
)

// #3681: a run that stored the placeholder "No answer was produced." is not an
// answer. It is counted under Failed, says why in plain words, and offers no
// Copy answer for the placeholder.
func TestIssue3681_PlaceholderAnswerIsAFailureNotACompletedAnswer(t *testing.T) {
	placeholder := AgentTask{ID: "ph", Title: "How many vacation days carry over?", State: AgentTaskCompleted, ResultPreview: "No answer was produced.", AnswerText: "No answer was produced."}
	if got := AgentTaskGroup(placeholder); got != "failed" {
		t.Fatalf("placeholder task is listed under %q, want failed", got)
	}
	real := AgentTask{ID: "ok", Title: "Count days", State: AgentTaskCompleted, AnswerText: "Twelve days."}
	if got := AgentTaskGroup(real); got != "completed" {
		t.Fatalf("answered task is listed under %q", got)
	}
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		reason := ResolveProductLocale(locale).Text("agents.failure_no_answer")
		if reason == "" || reason == "agents.failure_no_answer" {
			t.Fatalf("%s: no plain-words reason", locale)
		}
		page := renderAgentUXPage(t, locale, AgentsAvailabilityProjection{Enabled: true, Snapshot: AgentSnapshot{Availability: AgentsAvailable, StartAvailable: true, Tasks: []AgentTask{placeholder, real}}})
		start := strings.Index(page, `data-task-id="ph"`)
		if start < 0 {
			t.Fatalf("%s: task not listed", locale)
		}
		row := page[start:]
		if end := strings.Index(row[1:], `data-task-id="`); end > 0 {
			row = row[:end+1]
		}
		if !strings.Contains(page, `data-task-category="failed" data-task-id="ph"`) || strings.Contains(row, `data-tone="completed"`) || !strings.Contains(row, reason) {
			t.Errorf("%s: placeholder row not a failure with a reason: %s", locale, row)
		}
		if strings.Contains(row, "No answer was produced.") {
			t.Errorf("%s: placeholder text shown as an answer: %s", locale, row)
		}
	}
	selected := placeholder
	page := renderAgentUXPage(t, "en-US", AgentsAvailabilityProjection{Enabled: true, Snapshot: AgentSnapshot{Availability: AgentsAvailable, StartAvailable: true, Tasks: []AgentTask{placeholder}, SelectedTask: &selected}})
	if strings.Contains(page, `data-agent-copy-answer`) || strings.Contains(page, `data-agent-answer="No answer was produced.`) {
		t.Fatal("the detail offers to copy the placeholder as an answer")
	}
	if !strings.Contains(page, ResolveProductLocale("en-US").Text("agents.failure_no_answer")) {
		t.Fatal("the detail does not say why there is no answer")
	}
	if answerBlock(ResolveProductLocale("en-US"), "   ") != nil {
		t.Fatal("a blank answer is offered for copying")
	}
}
