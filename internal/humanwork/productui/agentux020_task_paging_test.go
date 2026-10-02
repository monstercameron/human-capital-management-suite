package productui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// TestTodo_AGENTUX_020_ShowOlder: a group with more than ten tasks lists the
// newest ten and puts the rest behind "Show older", in each language, with the
// counts on the tabs still counting every task.
func TestTodo_AGENTUX_020_ShowOlder(t *testing.T) {
	for _, tc := range []struct{ locale, want string }{{"en-US", "Show older"}, {"de-DE", "Ältere anzeigen"}, {"ar", "عرض الأقدم"}} {
		locale := ResolveProductLocale(tc.locale)
		view := ApplyLocale(NewView(PageAgents, "tenant", "owner", ""), locale)
		snapshot := AgentSnapshot{Availability: AgentsAvailable}
		for index := 0; index < 12; index++ {
			snapshot.Tasks = append(snapshot.Tasks, AgentTask{ID: fmt.Sprintf("task-%02d", index), Title: fmt.Sprintf("Question %02d", index), State: AgentTaskCompleted, AnswerText: "Answer", UpdatedAt: time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)})
		}
		markup, err := ui.RenderToString(RenderAgentTasksRegion(view, locale, snapshot))
		if err != nil {
			t.Fatal(err)
		}
		hidden := 0
		for _, row := range strings.Split(markup, `data-task-category="completed"`)[1:] {
			if strings.Contains(row[:strings.Index(row, ">")], "hidden") || strings.Contains(strings.Split(row, ">")[0], "hidden") {
				hidden++
			}
		}
		if hidden != 2 {
			t.Fatalf("%s: %d rows are hidden behind the button, want 2: %s", tc.locale, hidden, markup)
		}
		at := strings.Index(markup, `data-agent-task-more`)
		button := markup[strings.LastIndex(markup[:at], "<button"):]
		button = button[:strings.Index(button, "</button>")]
		if !strings.Contains(button, tc.want) || strings.Contains(button, " hidden") {
			t.Fatalf("%s: the older-tasks button is missing or hidden: %s", tc.locale, button)
		}
	}
}
