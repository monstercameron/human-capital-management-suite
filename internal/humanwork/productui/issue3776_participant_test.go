package productui

import (
	"strings"
	"testing"
	"time"
)

// The Ask view introduces the agent as a participant: a mark, a name, what it
// can use and its recent answers; the colour rules of #3776 are pinned by the
// ChatDesign_3776 tests beside this one.
func TestIssue3776_AskViewShowsTheAgentAsAParticipant(t *testing.T) {
	now := time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)
	tasks := []AgentTask{
		{ID: "old", Title: "Oldest question", State: AgentTaskCompleted, AnswerText: "Old answer.", ResultPreview: "Old answer.", UpdatedAt: now.Add(-3 * time.Hour)},
		{ID: "none", Title: "No answer here", State: AgentTaskCompleted, UpdatedAt: now},
		{ID: "bad", Title: "A failed one", State: AgentTaskFailed, UpdatedAt: now},
		{ID: "new", Title: "Parental leave policy", State: AgentTaskCompleted, AnswerText: "Twelve weeks.", ResultPreview: "Twelve weeks.", UpdatedAt: now.Add(-time.Hour)},
	}
	snapshot := AgentSnapshot{Availability: AgentsAvailable, StartAvailable: true, DocumentHubAvailable: true, Tasks: tasks,
		Agents: []AgentSummary{{ID: "hr", Name: "HR Helper", Description: "Answers HR questions", Skills: []string{"Policy search", "Leave balance"}}}}
	want := map[string][]string{
		"en-US": {"What it can use", "Documents you can read", "Policy search", "Recent answers"},
		"de-DE": {"Was er nutzen kann", "Dokumente, die Sie lesen dürfen", "Policy search", "Letzte Antworten"},
		"ar":    {"ما يمكنه استخدامه", "المستندات التي يمكنك قراءتها", "Policy search", "آخر الإجابات"},
	}
	for locale, expected := range want {
		page := renderAgentUXPage(t, locale, AgentsAvailabilityProjection{Enabled: true, Snapshot: snapshot})
		start := strings.Index(page, `data-agent-participant="true"`)
		if start < 0 {
			t.Fatalf("%s: the Ask view has no participant card", locale)
		}
		region := map[string]string{"en-US": "About the agent you are asking", "de-DE": "Über den Agenten, den Sie fragen", "ar": "عن الوكيل الذي تسأله"}[locale]
		if !strings.Contains(page[start-80:start], `aria-label="`+region+`"`) {
			t.Errorf("%s: the card has no accessible name %q", locale, region)
		}
		card := page[start:]
		card = card[:strings.Index(card, "</section>")]
		for _, text := range expected {
			if !strings.Contains(card, text) {
				t.Errorf("%s: card lacks %q: %s", locale, text, card)
			}
		}
		if !strings.Contains(card, "<svg") || !strings.Contains(card, "Parental leave policy") || !strings.Contains(card, "Twelve weeks.") {
			t.Errorf("%s: card lacks the avatar or the recent answer: %s", locale, card)
		}
		if strings.Contains(card, "No answer here") || strings.Contains(card, "A failed one") {
			t.Errorf("%s: a task with no answer is listed as an answer: %s", locale, card)
		}
		if strings.Index(card, "Parental leave policy") > strings.Index(card, "Oldest question") {
			t.Errorf("%s: answers are not newest first", locale)
		}
		if !strings.Contains(card, "task=new#agents-task-title") && !strings.Contains(card, "task=new") {
			t.Errorf("%s: an answer does not open its task: %s", locale, card)
		}
		if strings.Index(page, "data-agent-participant") > strings.Index(page, `class="agents-composer"`) {
			t.Errorf("%s: the card comes after the form", locale)
		}
	}
}

func TestIssue3776_ParticipantIsTruthfulWhenThereIsNothingToShow(t *testing.T) {
	page := renderAgentUXPage(t, "en-US", AgentsAvailabilityProjection{Enabled: true, Snapshot: AgentSnapshot{Availability: AgentsAvailable, StartAvailable: true}})
	start := strings.Index(page, `data-agent-participant="true"`)
	if start < 0 {
		t.Fatal("no participant card")
	}
	card := page[start:]
	card = card[:strings.Index(card, "</section>")]
	for _, text := range []string{"No answers yet", "Nothing beyond its general knowledge"} {
		if !strings.Contains(card, text) {
			t.Errorf("empty card lacks %q: %s", text, card)
		}
	}
	if strings.Contains(card, "Documents you can read") {
		t.Error("claims documents the hub does not offer")
	}
}
