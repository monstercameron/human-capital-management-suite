package productui

import (
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func agentTaskRelativeTime(locale LocaleContext, at, now time.Time) string {
	elapsed := now.Sub(at)
	if elapsed < time.Minute {
		return locale.Text("agents.time_now")
	}
	count, key := int(elapsed/time.Minute), "time_minutes"
	if elapsed >= time.Hour {
		count, key = int(elapsed/time.Hour), "time_hours"
	}
	if elapsed >= 24*time.Hour {
		count, key = int(elapsed/(24*time.Hour)), "time_days"
	}
	return agentUXR7Text(locale, key, "{count}", locale.FormatNumber(strconv.Itoa(count), 0))
}

func agentTaskTime(locale LocaleContext, task AgentTask, now time.Time) ui.Node {
	at := task.UpdatedAt
	if at.IsZero() {
		at = task.CreatedAt
	}
	if at.IsZero() {
		return nil
	}
	exact := agentTaskDateTimeLabel(locale, at, now)
	return html.Time(html.Props{Class: "agents-task-time", Title: exact, Raw: map[string]any{
		"datetime": at.UTC().Format(time.RFC3339), "tabindex": "0", "aria-label": exact,
	}}, ui.Text(agentTaskRelativeTime(locale, at, now)))
}

func agentTaskRowTime(locale LocaleContext, task AgentTask, now time.Time) ui.Node {
	if when := agentTaskTime(locale, task, now); when != nil {
		return when
	}
	return html.Span(html.Props{Class: "agents-task-time"}, ui.Text(locale.Text("agents.time_now")))
}

func agentTaskRowTimes(locale LocaleContext, task AgentTask, now time.Time) ui.Node {
	children := []ui.Node{agentTaskRowTime(locale, task, now)}
	return html.Span(html.Props{Class: "agents-task-times"}, children...)
}

func agentTaskTimeline(locale LocaleContext, task AgentTask) ui.Node {
	items := make([]ui.Node, 0, 2)
	if !task.CreatedAt.IsZero() {
		items = append(items, html.Span(html.Props{}, ui.Text(locale.Text("agents.started", map[string]string{"time": agentTaskDateTimeLabel(locale, task.CreatedAt, time.Now())}))))
	}
	if !task.UpdatedAt.IsZero() {
		key := "agents.updated"
		if agentTaskCategory(task.State) != "active" {
			key = "agents.finished"
		}
		items = append(items, html.Span(html.Props{}, ui.Text(locale.Text(key, map[string]string{"time": agentTaskDateTimeLabel(locale, task.UpdatedAt, time.Now())}))))
	}
	if len(items) == 0 {
		return nil
	}
	return html.P(html.Props{Class: "agents-task-timeline muted"}, items...)
}

func localizedAgentFailureReason(locale LocaleContext, reason string) string {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return ""
	}
	lower := strings.ToLower(reason)
	switch {
	case strings.Contains(lower, "too long"), strings.Contains(lower, "timed out"), strings.Contains(lower, "timeout"):
		return locale.Text("agents.failure_timeout")
	case strings.Contains(lower, "not allowed"), strings.Contains(lower, "permission"), strings.Contains(lower, "cannot read"), strings.Contains(lower, "could not read"):
		return locale.Text("agents.failure_document_access")
	case strings.Contains(lower, "unavailable"), strings.Contains(lower, "provider"), strings.Contains(lower, "service"), strings.Contains(lower, "connection"):
		return locale.Text("agents.failure_service")
	default:
		return locale.Text("agents.failure_unknown")
	}
}

func agentTaskDateTimeLabel(locale LocaleContext, at, now time.Time) string {
	zone, err := time.LoadLocation(locale.normalized().TimeZone)
	if err != nil {
		zone = time.UTC
	}
	local := at.In(zone)
	timeLabel := docsLocaleDigits(locale.Resolved, local.Format("15:04"))
	if locale.Resolved == "" || locale.Resolved == DefaultProductLocale {
		timeLabel = local.Format("3:04 PM")
	}
	return ChatDocDateLabel(locale, at, now) + ", " + timeLabel
}

func agentDocumentHref(reference AgentTaskDocumentReference) string {
	href := "/workspace/app/docs?document=" + url.QueryEscape(strings.TrimSpace(reference.DocumentID))
	if anchor := strings.TrimSpace(reference.SectionAnchor); anchor != "" {
		href += "#" + url.PathEscape(anchor)
	}
	return href
}

func agentTaskDocumentLinks(view View, locale LocaleContext, task AgentTask, compact bool) ui.Node {
	references := task.Documents
	labelKey := "attached"
	switch task.DocumentUsageState {
	case AgentDocumentUsageUsed:
		references = task.UsedDocuments
		labelKey = "sources"
	case AgentDocumentUsageNone:
		className := "agents-task-documents"
		heading := html.H2
		if compact {
			className += " is-compact"
			heading = html.H3
			return html.Section(html.Props{Class: className, Role: "status"}, html.Span(html.Props{Class: "agents-documents-read-label"}, ui.Text(locale.Text("agents.used_documents"))), html.P(html.Props{Class: "muted"}, ui.Text(locale.Text("agents.no_documents_used"))))
		}
		return html.Section(html.Props{Class: className, Role: "status"}, heading(html.Props{}, ui.Text(locale.Text("agents.used_documents"))), html.P(html.Props{Class: "muted"}, ui.Text(locale.Text("agents.no_documents_used"))))
	}
	links := make([]ui.Node, 0, len(references))
	for _, reference := range references {
		if strings.TrimSpace(reference.DocumentID) == "" {
			continue
		}
		label := strings.TrimSpace(reference.Label)
		if label == "" {
			label = locale.Text("agents.document")
		}
		linkChildren := []ui.Node{productIcon("document", "agents-document-icon"), softwareLink(view.Navigate, html.Props{}, agentDocumentHref(reference), ui.Text(label))}
		links = append(links, html.Li(html.Props{Class: "agents-document-reference", Raw: map[string]any{
			"data-agent-task-document": "true", "data-document-id": reference.DocumentID, "data-document-label": label, "data-section-anchor": reference.SectionAnchor,
		}}, linkChildren...))
	}
	if len(links) == 0 {
		return nil
	}
	className := "agents-task-documents"
	heading := html.H2
	if compact {
		className += " is-compact"
		heading = html.H3
	}
	if compact {
		return html.Section(html.Props{Class: className}, html.Span(html.Props{Class: "agents-documents-read-label"}, ui.Text(agentUXR7Text(locale, labelKey))), html.Ul(html.Props{}, links...))
	}
	return html.Section(html.Props{Class: className}, heading(html.Props{}, ui.Text(agentUXR7Text(locale, labelKey))), html.Ul(html.Props{}, links...))
}

func agentTaskOmissions(locale LocaleContext, omissions []AgentTaskDocumentOmission) ui.Node {
	items := make([]ui.Node, 0, len(omissions))
	generic := 0
	for _, omission := range omissions {
		label := strings.TrimSpace(omission.Label)
		if label == "" {
			generic++
			continue
		}
		items = append(items, html.Li(html.Props{}, ui.Text(locale.Text("agents.document_not_used", map[string]string{"label": label}))))
	}
	if generic > 0 {
		items = append(items, html.Li(html.Props{}, ui.Text(locale.Plural("agents.documents_not_used_count", int64(generic)))))
	}
	if len(items) == 0 {
		return nil
	}
	return html.Section(html.Props{Class: "agents-document-omissions", Role: "status"}, html.Ul(html.Props{}, items...))
}

// RenderAgentTasksRegion gives the browser RPC reconciler the same component
// used for server first paint.
func RenderAgentTasksRegion(view View, locale LocaleContext, snapshot AgentSnapshot) ui.Node {
	return tagAgentTasks(view, locale, snapshot)
}

// RenderAgentTaskDetail gives GetAgentTask reconciliation the same detail
// component used for server first paint.
func RenderAgentTaskDetail(view View, locale LocaleContext, task AgentTask, retryAvailable bool) ui.Node {
	return tagAgentTaskViewWithRetry(view, locale, task, retryAvailable)
}
