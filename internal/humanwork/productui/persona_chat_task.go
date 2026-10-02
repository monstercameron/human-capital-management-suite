package productui

import (
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// RenderAgentTaskSummary is shared by the task list and a persona's thread.
// The caller supplies an authorized link; rendering cannot grant task access.
func RenderAgentTaskSummary(locale LocaleContext, task AgentTask, href string) ui.Node {
	children := []ui.Node{
		html.Strong(html.Props{}, ui.Text(task.Title)),
		html.Span(html.Props{Class: "status agents-task-status", Role: "status", Aria: map[string]string{"live": "polite", "atomic": "true"}, Data: map[string]string{"state": string(task.State)}}, ui.Text(agentTaskStateLabel(locale, task.State))),
		html.P(html.Props{Class: "muted"}, ui.Text(task.Goal)),
	}
	if steps := agentTaskStepItems(locale, task.Steps); len(steps) > 0 {
		children = append(children, html.Section(html.Props{Class: "agents-task-plan agents-task-summary-plan"},
			html.H4(html.Props{}, ui.Text(locale.Text("agents.what_agent_did"))),
			html.Ol(html.Props{}, steps...),
		))
	}
	children = append(children, html.A(html.Props{Class: "agents-task-link", Href: href}, ui.Text(locale.Text("agents.open_task"))))
	return html.Div(html.Props{Class: "agents-task-summary", Data: map[string]string{"task-id": task.ID}}, children...)
}

func agentTaskStepItems(locale LocaleContext, steps []AgentTaskStep) []ui.Node {
	items := make([]ui.Node, 0, len(steps))
	for index, step := range steps {
		name, _ := agentStepPresentation(locale, step)
		detail := strings.TrimSpace(step.Detail)
		if detail == "" {
			detail = strings.TrimSpace(step.FailureReason)
		}
		if detail == "" && strings.EqualFold(strings.TrimSpace(step.State), "failed") {
			detail = locale.Text("agents.step_failed_generic")
		}
		children := []ui.Node{
			html.Span(html.Props{Class: "agents-step-number"}, ui.Text(fmt.Sprintf("%d", index+1))),
			html.Strong(html.Props{}, ui.Text(name)),
			html.Span(html.Props{Class: "status agents-task-status", Raw: map[string]any{"data-tone": agentStepTone(step.State)}}, ui.Text(agentStepStateLabel(locale, step.State))),
		}
		if duration := agentStepDuration(locale, step.StartedAt, step.FinishedAt); duration != "" {
			children = append(children, html.Span(html.Props{Class: "agents-step-duration muted"}, ui.Text(duration)))
		}
		if detail != "" {
			children = append(children, html.P(html.Props{Class: "muted", Raw: map[string]any{"dir": "auto"}}, ui.Text(detail)))
		}
		items = append(items, html.Li(html.Props{Class: "agents-plan-step", Raw: map[string]any{
			"data-step-state": step.State, "data-step-tier": step.Tier, "data-readable-step-label": legacyAgentStepLabel(step.Name),
		}}, children...))
	}
	return items
}

func agentStepDuration(locale LocaleContext, started, finished time.Time) string {
	if started.IsZero() {
		return ""
	}
	if finished.IsZero() {
		return locale.Text("agents.step_working")
	}
	duration := finished.Sub(started)
	if duration < 0 {
		return ""
	}
	seconds := int(duration.Round(time.Second) / time.Second)
	if seconds < 60 {
		return locale.Text("agents.duration_seconds", map[string]string{"count": locale.FormatNumber(fmt.Sprint(seconds), 0)})
	}
	minutes := int(duration.Round(time.Minute) / time.Minute)
	return locale.Text("agents.duration_minutes", map[string]string{"count": locale.FormatNumber(fmt.Sprint(minutes), 0)})
}

func agentStepTone(state string) string {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "completed", "complete", "observed":
		return "completed"
	case "failed", "failure":
		return "failed"
	default:
		return "active"
	}
}
