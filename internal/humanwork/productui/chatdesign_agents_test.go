package productui

import (
	"regexp"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/agenticon"
)

// #3776: the General agent's mark is the neutral informational pair in every
// locale, as a choice and on the tasks it answered; never the danger or the
// warning colour. Its glyph and shape are the ones it has always worn.
func TestChatDesign_3776_GeneralAgentMarkIsNeverTheDangerColour(t *testing.T) {
	seeded := agenticon.Fallback(agentUX074GeneralSeed)
	mark := generalAgentIcon()
	if !mark.Valid() || mark.Glyph != seeded.Glyph || mark.Shape != seeded.Shape {
		t.Fatalf("the General agent's mark is not its own glyph and shape: %+v, seeded %+v", mark, seeded)
	}
	if mark.Foreground != "--hcm-color-info" || mark.Background != "--hcm-color-info-surface" {
		t.Fatalf("the General agent's mark is drawn in %s on %s", mark.Foreground, mark.Background)
	}
	icon := regexp.MustCompile(`<svg[^>]*class="agent-icon"[^>]*>.*?</svg>`)
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		task := AgentTask{ID: "g", Title: "Welcome a teammate", State: AgentTaskCompleted, AnswerText: "Say hello."}
		page := renderAgentUXPage(t, locale, AgentsAvailabilityProjection{Enabled: true, Snapshot: AgentSnapshot{Availability: AgentsAvailable, StartAvailable: true, Tasks: []AgentTask{task}}})
		for surface, marker := range map[string]string{"choice": `id="agents-choice-general"`, "task": `data-task-id="g"`} {
			start := strings.Index(page, marker)
			if start < 0 {
				t.Fatalf("%s: the page has no General agent %s", locale, surface)
			}
			drawn := icon.FindString(page[start:])
			if drawn == "" {
				t.Fatalf("%s: the General agent %s has no mark", locale, surface)
			}
			if !strings.Contains(drawn, `stroke="var(--hcm-color-info)"`) || !strings.Contains(drawn, `fill="var(--hcm-color-info-surface)"`) {
				t.Errorf("%s: the General agent %s is not drawn in the neutral pair: %s", locale, surface, drawn)
			}
			if strings.Contains(drawn, "danger") || strings.Contains(drawn, "warning") {
				t.Errorf("%s: the General agent %s is drawn in an alarm colour: %s", locale, surface, drawn)
			}
		}
	}
}

// The colour and mark of a task follow its outcome. A task that finished with
// nothing to show stays under Completed but is not drawn as a success, and its
// row says so in words in every locale.
func TestChatDesign_3776_TaskStatusColourFollowsTheOutcome(t *testing.T) {
	for _, tc := range []struct {
		name string
		task AgentTask
		tone string
	}{
		{"answered", AgentTask{State: AgentTaskCompleted, AnswerText: "Twelve days."}, "completed"},
		{"answered from the preview", AgentTask{State: AgentTaskCompleted, ResultPreview: "Twelve days."}, "completed"},
		{"no answer", AgentTask{State: AgentTaskCompleted}, "no-answer"},
		{"blank answer", AgentTask{State: AgentTaskCompleted, AnswerText: "  "}, "no-answer"},
		{"failed", AgentTask{State: AgentTaskFailed}, "failed"},
		{"running", AgentTask{State: AgentTaskRunning}, "active"},
	} {
		if got := agentTaskTone(tc.task); got != tc.tone {
			t.Errorf("%s: tone %q, want %q", tc.name, got, tc.tone)
		}
	}
	// The tab a task is listed under does not move.
	if AgentTaskGroup(AgentTask{State: AgentTaskCompleted}) != "completed" {
		t.Fatal("a task with no answer left the Completed tab")
	}
	if agentTaskStatusGlyph("no-answer") == agentTaskStatusGlyph("completed") {
		t.Fatal("a task with no answer wears the success mark")
	}
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		sentence := ResolveProductLocale(locale).Text("agents.task_no_answer")
		if sentence == "" || sentence == "agents.task_no_answer" {
			t.Fatalf("%s: no sentence for a task without an answer", locale)
		}
		tasks := []AgentTask{
			{ID: "empty", Title: "Summarise the handbook", State: AgentTaskCompleted},
			{ID: "full", Title: "Count vacation days", State: AgentTaskCompleted, AnswerText: "Twelve days."},
		}
		page := renderAgentUXPage(t, locale, AgentsAvailabilityProjection{Enabled: true, Snapshot: AgentSnapshot{Availability: AgentsAvailable, StartAvailable: true, Tasks: tasks}})
		row := func(id string) string {
			start := strings.Index(page, `data-task-id="`+id+`"`)
			if start < 0 {
				t.Fatalf("%s: task %s is not listed", locale, id)
			}
			rest := page[start:]
			if end := strings.Index(rest[1:], `data-task-id="`); end > 0 {
				rest = rest[:end+1]
			}
			return rest
		}
		if empty := row("empty"); !strings.Contains(empty, `class="agents-task-state-icon" data-tone="no-answer"`) || strings.Contains(empty, `data-tone="completed"`) || !strings.Contains(empty, sentence) {
			t.Errorf("%s: a task with no answer is drawn as a success or does not say so: %s", locale, empty)
		}
		if full := row("full"); !strings.Contains(full, `class="agents-task-state-icon" data-tone="completed"`) || strings.Contains(full, sentence) {
			t.Errorf("%s: an answered task lost its success mark: %s", locale, full)
		}
	}
	css := Stylesheet()
	for _, want := range []string{
		`.agents-task-state-icon[data-tone=no-answer]{border:1px solid var(--muted);color:var(--muted);}`,
		`.agents-task-status[data-tone="no-answer"]{border:1px solid var(--muted);color:var(--muted);}`,
	} {
		if !strings.Contains(css, want) {
			t.Errorf("stylesheet is missing %q", want)
		}
	}
	if regexp.MustCompile(`data-tone="?no-answer"?\]\{[^}]*(success|danger)`).MatchString(css) {
		t.Error("the no-answer tone borrows the success or danger colour")
	}
}
