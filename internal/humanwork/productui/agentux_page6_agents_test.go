package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestAgentUXPage6_TaskRowsStretchWithTheirTabs(t *testing.T) {
	css := agentsStylesheet() + agentUXR7Stylesheet()
	for selector, declarations := range map[string][]string{
		".agents-main":      {"grid-template-columns:minmax(0,1fr)", "width:100%"},
		".agents-task-list": {"grid-template-columns:minmax(0,1fr)", "justify-items:stretch", "width:100%"},
		".agents-task-row":  {"justify-self:stretch", "width:100%"},
		".agents-task-link": {"justify-self:stretch", "width:100%"},
	} {
		got := declarationsFor(css, selector)
		for _, want := range declarations {
			if !strings.Contains(got, want) {
				t.Errorf("%s missing %q: %s", selector, want, got)
			}
		}
	}
}

func TestAgentUXPage6_TaskTerminalAnnouncement(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	view := ApplyLocale(NewView(PageAgents, "tenant", "viewer", ""), locale)
	markup, err := ui.RenderToString(RenderAgentTasksRegion(view, locale, AgentSnapshot{}))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`id="agents-tasks-announcement"`, `role="status"`, `aria-live="polite"`,
		`data-msg-task-answered="__AGENT__ answered. The answer is under Tasks."`,
		`data-msg-task-failed="__AGENT__ could not finish the task. It is under Tasks."`,
	} {
		if !strings.Contains(markup, want) {
			t.Fatalf("terminal announcement missing %q: %s", want, markup)
		}
	}
}

func TestAgentUXPage6_TaskDetailRemainsInline(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	view := ApplyLocale(NewView(PageAgents, "tenant", "viewer", ""), locale)
	task := AgentTask{ID: "task-7", Title: "What is 1 and 1?", AnswerText: "1 and 1 equals 2.", State: AgentTaskCompleted}
	markup, err := ui.RenderToString(RenderAgentTasksRegion(view, locale, AgentSnapshot{Tasks: []AgentTask{task}, SelectedTask: &task}))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`class="agents-task-row is-expanded"`, `data-selected="true"`, `aria-expanded="true"`, `id="agents-task-detail-task-7"`, "1 and 1 equals 2."} {
		if !strings.Contains(markup, want) {
			t.Fatalf("inline task detail missing %q: %s", want, markup)
		}
	}
}
