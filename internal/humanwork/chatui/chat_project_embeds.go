package chatui

import (
	"strconv"
	"strings"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// projectPreviewEmbeds renders one quick-look card per distinct project task
// or board address in a message, in the order they appear. A card shows only
// fields from a ready, readable preview; until the authorized read lands it
// is a skeleton of the ready card's own rows, and a refused read is a neutral
// locked card that never names the task.
func projectPreviewEmbeds(m Model, body string) []ui.Node {
	refs := ProjectTaskReferences(body, m.EmbedOrigin)
	if len(refs) == 0 {
		return nil
	}
	seen := map[string]bool{}
	out := make([]ui.Node, 0, len(refs))
	for _, ref := range refs {
		key := ProjectTaskPreviewKey(ref.ProjectID, ref.TaskID)
		if ref.ProjectID == "" || key == "" || seen[key] {
			continue
		}
		seen[key] = true
		preview, ok := m.ProjectTaskPreviews[key]
		if !ok || preview.ProjectID != ref.ProjectID || preview.TaskID != ref.TaskID {
			preview = ProjectTaskPreview{ProjectID: ref.ProjectID, TaskID: ref.TaskID, State: "loading"}
		}
		out = append(out, html.WithKey(projectPreviewCard(m, preview), "project-embed:"+key))
	}
	return out
}

func projectPreviewCard(m Model, preview ProjectTaskPreview) ui.Node {
	board := preview.IsBoard()
	labelKey := KeyProjectTaskEmbed
	if board {
		labelKey = KeyProjectBoardEmbed
	}
	head := []ui.Node{html.Span(html.Props{Class: "chat-embed-label chat-project-embed-kind"}, projectEmbedGlyph(board), ui.Text(m.t(labelKey)))}
	switch {
	case preview.State == "ready" && preview.Readable && strings.TrimSpace(preview.Title) != "":
	case preview.State == "ready" || preview.State == "restricted" || preview.State == "unavailable":
		return html.Div(html.Props{Class: "chat-embed chat-project-embed is-locked", Data: map[string]string{"embed-state": preview.State}},
			append(head, html.Span(html.Props{Class: "chat-doc-embed-locked", Text: m.t(KeyProjectTaskRestricted)}))...)
	default:
		return html.Div(html.Props{Class: "chat-embed chat-project-embed is-loading", Data: map[string]string{"embed-state": "loading"}, Aria: map[string]string{"busy": "true"}},
			append(head,
				html.Span(html.Props{Class: "chat-doc-skeleton chat-doc-skeleton-title", Aria: map[string]string{"hidden": "true"}}),
				html.Span(html.Props{Class: "chat-doc-skeleton chat-doc-skeleton-byline", Aria: map[string]string{"hidden": "true"}}),
				html.Span(html.Props{Class: "sr-only", Text: m.t(KeyProjectEmbedLoading)}))...)
	}

	title := strings.TrimSpace(preview.Title)
	children := head
	if board {
		children = append(children, html.Strong(html.Props{Class: "chat-embed-source chat-project-embed-title", Dir: "auto", Text: title}))
		children = append(children, projectBoardSummary(m, preview))
		return html.A(html.Props{Class: "chat-embed chat-embed-link chat-project-embed is-board", Href: ProjectBoardReferenceURL(preview.ProjectID), Data: map[string]string{"action": "open-project-task-reference", "project": preview.ProjectID, "task": "", "id": preview.ProjectID, "extra": ""}, Aria: map[string]string{"label": m.tf(KeyOpenProjectBoard, map[string]string{"title": title})}}, children...)
	}

	titleRow := []ui.Node{}
	if preview.Key != "" {
		titleRow = append(titleRow, html.Span(html.Props{Class: "chat-project-embed-key", Text: preview.Key}))
	}
	titleRow = append(titleRow, html.Strong(html.Props{Class: "chat-embed-source chat-project-embed-title", Dir: "auto", Text: title}))
	children = append(children, html.Span(html.Props{Class: "chat-project-embed-titlerow"}, titleRow...))
	if preview.ProjectName != "" {
		children = append(children, html.Span(html.Props{Class: "chat-embed-byline", Dir: "auto", Text: preview.ProjectName}))
	}
	meta := []ui.Node{}
	if preview.Status != "" {
		meta = append(meta, html.Span(html.Props{Class: "chat-project-embed-status", Data: map[string]string{"tone": preview.StatusTone}}, html.Span(html.Props{Class: "chat-project-embed-dot", Aria: map[string]string{"hidden": "true"}}), ui.Text(preview.Status)))
	}
	if level := strings.ToLower(strings.TrimPrefix(preview.PriorityID, "TASK_PRIORITY_")); projectPriorityKeys[level] != "" {
		meta = append(meta, html.Span(html.Props{Class: "chat-project-embed-priority", Data: map[string]string{"level": level}}, projectPriorityBars(), ui.Text(m.t(projectPriorityKeys[level]))))
	}
	if due, err := time.Parse("2006-01-02", preview.Due); err == nil {
		meta = append(meta, html.Span(html.Props{Class: "chat-project-embed-due", Data: map[string]string{"tone": projectDueTone(due, preview.StatusTone == "done", time.Now())}, Text: m.tf(KeyProjectDue, map[string]string{"date": formatShortDate(m.Locale, due)})}))
	}
	if preview.Assignee != "" {
		meta = append(meta, html.Span(html.Props{Class: "chat-project-embed-assignee"}, personAvatarWithPhoto(preview.Assignee, "chat-project-embed-avatar", preview.AssigneePhoto), ui.Text(preview.Assignee)))
	}
	if len(meta) > 0 {
		children = append(children, html.Span(html.Props{Class: "chat-project-embed-meta"}, meta...))
	}
	return html.A(html.Props{Class: "chat-embed chat-embed-link chat-project-embed", Href: ProjectTaskReferenceURL(preview.ProjectID, preview.TaskID), Data: map[string]string{"action": "open-project-task-reference", "project": preview.ProjectID, "task": preview.TaskID, "id": preview.ProjectID, "extra": preview.TaskID}, Aria: map[string]string{"label": m.tf(KeyOpenProjectTask, map[string]string{"title": title})}}, children...)
}

// projectBoardSummary is a board card's progress: a done/total bar and the
// counts in words, or a quiet line when the board has no tasks yet.
func projectBoardSummary(m Model, preview ProjectTaskPreview) ui.Node {
	if preview.Total <= 0 {
		return html.Span(html.Props{Class: "chat-embed-byline", Text: m.t(KeyProjectBoardEmpty)})
	}
	done := min(max(preview.Done, 0), preview.Total)
	percent := done * 100 / preview.Total
	return html.Span(html.Props{Class: "chat-project-embed-progress"},
		html.Span(html.Props{Class: "chat-project-embed-bar", Aria: map[string]string{"hidden": "true"}},
			html.Span(html.Props{Class: "chat-project-embed-fill", Style: map[string]string{"inline-size": strconv.Itoa(percent) + "%"}})),
		html.Span(html.Props{Class: "chat-project-embed-counts", Text: m.tf(KeyProjectBoardProgress, map[string]string{"done": m.n(done), "total": m.n(preview.Total), "open": m.n(preview.Total - done)})}))
}

var projectPriorityKeys = map[string]string{"low": KeyProjectPriorityLow, "normal": KeyProjectPriorityNormal, "high": KeyProjectPriorityHigh, "urgent": KeyProjectPriorityUrgent}

// projectDueTone flags an open task's due date: overdue once the day has
// passed, soon within three days. Done tasks are never overdue.
func projectDueTone(due time.Time, done bool, now time.Time) string {
	if done {
		return ""
	}
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	day := time.Date(due.Year(), due.Month(), due.Day(), 0, 0, 0, 0, time.UTC)
	switch {
	case day.Before(today):
		return "overdue"
	case day.Sub(today) <= 3*24*time.Hour:
		return "soon"
	}
	return ""
}

// projectReferenceNavigate opens a shared project task or board in-app on a
// plain click; modified clicks keep the browser's own new-tab behaviour.
func projectReferenceNavigate(m Model, projectID, taskID string, plain bool) bool {
	if !plain || m.Callbacks.Navigate == nil {
		return false
	}
	href := ProjectBoardReferenceURL(projectID)
	if taskID != "" {
		href = ProjectTaskReferenceURL(projectID, taskID)
	}
	if href == "" {
		return false
	}
	m.Callbacks.Navigate(href)
	return true
}

func projectEmbedGlyph(board bool) ui.Node {
	path := "M9 11l3 3 8-8M4 12v7a1 1 0 0 0 1 1h14"
	if board {
		path = "M4 5h4v14H4zM10 5h4v9h-4zM16 5h4v6h-4z"
	}
	return html.Tag("svg", html.Props{Class: "chat-project-embed-glyph", Raw: map[string]any{"viewBox": "0 0 24 24", "fill": "none", "stroke": "currentColor", "stroke-width": "2", "stroke-linecap": "round", "stroke-linejoin": "round", "aria-hidden": "true", "focusable": "false"}}, html.Tag("path", html.Props{Raw: map[string]any{"d": path}}))
}

func projectPriorityBars() ui.Node {
	return html.Span(html.Props{Class: "chat-project-embed-bars", Aria: map[string]string{"hidden": "true"}}, html.Span(html.Props{}), html.Span(html.Props{}), html.Span(html.Props{}))
}

// projectEmbedStyles shapes the project quick-look cards on the shared
// .chat-embed frame: a kind row, a key and title row, the project byline, a
// wrapping meta row, and a progress bar for boards.
const projectEmbedStyles = `.chat-project-embed{gap:5px;text-decoration:none}` +
	`.chat-project-embed.is-board{border-inline-start-color:var(--hcm-color-info,var(--accent))}` +
	`.chat-project-embed-kind{display:inline-flex;align-items:center;gap:5px}` +
	`.chat-project-embed-glyph{width:13px;height:13px;flex:none}` +
	`.chat-project-embed-titlerow{display:flex;align-items:baseline;gap:8px;min-width:0}` +
	`.chat-project-embed-key{flex:none;font-size:.75rem;font-weight:600;color:var(--muted);font-variant-numeric:tabular-nums}` +
	`.chat-project-embed-title{min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;unicode-bidi:plaintext}` +
	`.chat-project-embed-meta{display:flex;flex-wrap:wrap;align-items:center;gap:6px 12px;margin-top:2px;font-size:.8125rem;color:var(--muted)}` +
	`.chat-project-embed-status{display:inline-flex;align-items:center;gap:5px;padding:1px 8px;border-radius:999px;background:color-mix(in srgb,var(--ink) 7%,transparent);color:var(--ink);font-weight:600;font-size:.75rem}` +
	`.chat-project-embed-dot{width:7px;height:7px;border-radius:50%;background:var(--muted)}` +
	`.chat-project-embed-status[data-tone="doing"] .chat-project-embed-dot{background:var(--hcm-color-info,#2563eb)}` +
	`.chat-project-embed-status[data-tone="done"] .chat-project-embed-dot{background:var(--hcm-color-success,#15803d)}` +
	`.chat-project-embed-priority{display:inline-flex;align-items:center;gap:5px}` +
	`.chat-project-embed-bars{display:inline-flex;align-items:flex-end;gap:1px;height:10px}` +
	`.chat-project-embed-bars>span{width:3px;border-radius:1px;background:color-mix(in srgb,var(--ink) 22%,transparent)}` +
	`.chat-project-embed-bars>span:nth-child(1){height:4px}.chat-project-embed-bars>span:nth-child(2){height:7px}.chat-project-embed-bars>span:nth-child(3){height:10px}` +
	`.chat-project-embed-priority[data-level="normal"] .chat-project-embed-bars>span:nth-child(-n+2),.chat-project-embed-priority[data-level="low"] .chat-project-embed-bars>span:nth-child(1){background:var(--muted)}` +
	`.chat-project-embed-priority[data-level="high"] .chat-project-embed-bars>span{background:var(--hcm-color-warning,#b45309)}` +
	`.chat-project-embed-priority[data-level="urgent"]{color:var(--hcm-color-danger,#b42318);font-weight:600}.chat-project-embed-priority[data-level="urgent"] .chat-project-embed-bars>span{background:var(--hcm-color-danger,#b42318)}` +
	`.chat-project-embed-due[data-tone="overdue"]{color:var(--hcm-color-danger,#b42318);font-weight:600}` +
	`.chat-project-embed-due[data-tone="soon"]{color:var(--hcm-color-warning,#b45309);font-weight:600}` +
	`.chat-project-embed-assignee{display:inline-flex;align-items:center;gap:5px;color:var(--ink)}` +
	`.chat-project-embed-avatar{position:relative;display:inline-grid;place-items:center;width:18px;height:18px;border-radius:50%;overflow:hidden;background:var(--soft);font-size:.5625rem;font-weight:700;color:var(--ink)}` +
	`.chat-project-embed-avatar .chat-avatar-photo{position:absolute;inset:0;width:100%;height:100%;object-fit:cover}` +
	`.chat-project-embed-progress{display:flex;align-items:center;gap:10px;margin-top:2px}` +
	`.chat-project-embed-bar{flex:1;max-width:200px;height:6px;border-radius:999px;background:color-mix(in srgb,var(--ink) 10%,transparent);overflow:hidden}` +
	`.chat-project-embed-fill{display:block;height:100%;border-radius:inherit;background:var(--hcm-color-success,#15803d)}` +
	`.chat-project-embed-counts{font-size:.8125rem;color:var(--muted);font-variant-numeric:tabular-nums}` +
	`.chat-project-embed:hover .chat-project-embed-title{text-decoration:underline}`
