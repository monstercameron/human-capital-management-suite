package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// RenderAgentTaskSummary is shared by the task list and a persona's thread.
// The caller supplies an authorized link; rendering cannot grant task access.
func RenderAgentTaskSummary(locale LocaleContext, task AgentTask, href string) ui.Node {
	return html.Div(html.Props{Class: "agents-task-summary", Data: map[string]string{"task-id": task.ID}},
		html.Strong(html.Props{}, ui.Text(task.Title)),
		html.Span(html.Props{Class: "status agents-task-status", Role: "status", Aria: map[string]string{"live": "polite", "atomic": "true"}, Data: map[string]string{"state": string(task.State)}}, ui.Text(agentTaskStateLabel(locale, task.State))),
		html.P(html.Props{Class: "muted"}, ui.Text(task.Goal)),
		html.A(html.Props{Class: "agents-task-link", Href: href}, ui.Text(locale.Text("agents.open_task"))),
	)
}
