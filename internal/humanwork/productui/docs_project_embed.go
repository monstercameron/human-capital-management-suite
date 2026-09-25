package productui

import (
	"strconv"
	"strings"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/yuin/goldmark/ast"
)

// docsProjectEmbedParagraph turns a paragraph that holds nothing but one
// project task or board address into a quick-look card, the way a pasted
// link unfurls in chat. Links inside running text stay inline chips, and a
// target without an authorized preview keeps the neutral inline rendering.
func docsProjectEmbedParagraph(view View, paragraph *ast.Paragraph, source []byte) (ui.Node, bool) {
	var target, text string
	for child := paragraph.FirstChild(); child != nil; child = child.NextSibling() {
		switch node := child.(type) {
		case *ast.Link:
			if target != "" {
				return nil, false
			}
			target = string(node.Destination)
		case *ast.AutoLink:
			if target != "" {
				return nil, false
			}
			target = string(node.URL(source))
		case *ast.Text:
			// The parser does not linkify, so an address pasted on its own
			// line arrives as plain text; that text is the target.
			text += string(node.Segment.Value(source))
		default:
			return nil, false
		}
	}
	if text = strings.TrimSpace(text); text != "" {
		if target != "" || strings.ContainsAny(text, " \t\n") {
			return nil, false
		}
		target = text
	}
	ref, ok := parseDocsProjectTaskReferenceLoose(target)
	if !ok || ref.ProjectID == "" || !docsProjectSameOrigin(view, target) {
		return nil, false
	}
	preview, found := docsProjectTaskPreview(view, ref)
	if !found || !docsProjectPreviewUsable(ref, preview) {
		return nil, false
	}
	return docsProjectEmbedCard(view, ref, preview), true
}

func docsProjectEmbedCard(view View, ref DocsProjectTaskReference, preview DocsProjectTaskPreview) ui.Node {
	locale := view.Locale.Resolved
	copy := docsProjectTaskCopy(locale)
	title := strings.TrimSpace(preview.Title)
	kind := copy.taskKind
	href := DocsProjectTaskHref(DocsProjectTaskReference{ProjectID: preview.ProjectID, TaskID: preview.TaskID})
	label := copy.open + ": " + title
	if ref.TaskID == "" {
		kind, href, label = copy.boardKind, DocsProjectBoardHref(preview.ProjectID), copy.openBoard+": "+title
	}
	children := []ui.Node{html.Span(html.Props{Class: "docs-project-embed-kind"}, ui.Text(kind))}
	titleRow := []ui.Node{}
	if preview.Key != "" {
		titleRow = append(titleRow, html.Span(html.Props{Class: "docs-project-embed-key"}, ui.Text(preview.Key)))
	}
	titleRow = append(titleRow, html.Span(html.Props{Class: "docs-project-embed-title", Dir: "auto"}, ui.Text(title)))
	children = append(children, html.Span(html.Props{Class: "docs-project-embed-titlerow"}, titleRow...))

	if ref.TaskID == "" {
		children = append(children, docsProjectBoardProgress(locale, copy, preview))
	} else {
		if name := strings.TrimSpace(preview.ProjectName); name != "" {
			children = append(children, html.Span(html.Props{Class: "docs-project-embed-byline", Dir: "auto"}, ui.Text(name)))
		}
		meta := []ui.Node{html.Span(html.Props{Class: "docs-project-embed-status", Data: map[string]string{"tone": preview.StatusTone}}, html.Span(html.Props{Class: "docs-project-embed-dot", Aria: map[string]string{"hidden": "true"}}), ui.Text(strings.TrimSpace(preview.Status)))}
		if level := strings.ToLower(strings.TrimPrefix(preview.PriorityID, "TASK_PRIORITY_")); copy.priorities[level] != "" {
			meta = append(meta, html.Span(html.Props{Class: "docs-project-embed-priority", Data: map[string]string{"level": level}}, ui.Text(copy.priorities[level])))
		}
		if due, err := time.Parse("2006-01-02", preview.DueDate); err == nil {
			meta = append(meta, html.Span(html.Props{Class: "docs-project-embed-due", Data: map[string]string{"tone": docsProjectDueTone(due, preview.StatusTone == "done", time.Now())}}, ui.Text(strings.ReplaceAll(copy.due, "{date}", docsShortDateLabel(locale, due)))))
		}
		if assignee := strings.TrimSpace(preview.Assignee); assignee != "" {
			meta = append(meta, html.Span(html.Props{Class: "docs-project-embed-assignee"}, personAvatar(assignee, "", preview.AssigneePhoto, "tiny"), ui.Text(assignee)))
		}
		children = append(children, html.Span(html.Props{Class: "docs-project-embed-meta"}, meta...))
	}
	class := "docs-project-embed"
	if ref.TaskID == "" {
		class += " is-board"
	}
	return softwareLink(view.Navigate, html.Props{Class: class, Data: map[string]string{"docs-project-task": href}, Raw: map[string]any{"aria-label": label}}, href, children...)
}

func docsProjectBoardProgress(locale string, copy docsProjectTaskLocaleCopy, preview DocsProjectTaskPreview) ui.Node {
	if preview.Total <= 0 {
		return html.Span(html.Props{Class: "docs-project-embed-byline"}, ui.Text(copy.noTasks))
	}
	done := min(max(preview.Done, 0), preview.Total)
	digits := func(n int) string { return docsLocaleDigits(locale, strconv.Itoa(n)) }
	return html.Span(html.Props{Class: "docs-project-embed-progress"},
		html.Span(html.Props{Class: "docs-project-embed-bar", Aria: map[string]string{"hidden": "true"}},
			html.Span(html.Props{Class: "docs-project-embed-fill", Style: map[string]string{"inline-size": strconv.Itoa(done*100/preview.Total) + "%"}})),
		html.Span(html.Props{Class: "docs-project-embed-counts"}, ui.Text(copy.progress(digits(done), digits(preview.Total), digits(preview.Total-done)))))
}

// docsProjectDueTone flags an open task's due date: overdue once the day
// has passed, soon within three days. Done tasks are never overdue.
func docsProjectDueTone(due time.Time, done bool, now time.Time) string {
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

func docsProjectEmbedStylesheet() string {
	return `.docs-markdown .docs-project-embed{display:grid;gap:.3rem;inline-size:min(100%,36rem);margin-block:.75rem;padding:.75rem .9rem;border:1px solid var(--line);border-inline-start:3px solid var(--accent);border-radius:var(--hcm-radius-control);background:var(--canvas);color:var(--ink);text-decoration:none}` +
		`.docs-markdown .docs-project-embed.is-board{border-inline-start-color:var(--hcm-color-info,var(--accent))}` +
		`.docs-markdown .docs-project-embed:hover{border-color:color-mix(in srgb,var(--accent) 45%,var(--line))}.docs-markdown .docs-project-embed:hover .docs-project-embed-title{text-decoration:underline}` +
		`.docs-markdown .docs-project-embed:focus-visible{outline:var(--hcm-focus-ring-width) solid var(--hcm-color-focus);outline-offset:2px}` +
		`.docs-project-embed-kind{color:var(--muted);font-size:var(--hcm-font-size-small);font-weight:600;letter-spacing:.01em}` +
		`.docs-project-embed-titlerow{display:flex;align-items:baseline;gap:.5rem;min-inline-size:0}` +
		`.docs-project-embed-key{flex:none;color:var(--muted);font-size:var(--hcm-font-size-small);font-weight:600;font-variant-numeric:tabular-nums}` +
		`.docs-project-embed-title{min-inline-size:0;font-weight:650;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;unicode-bidi:plaintext}` +
		`.docs-project-embed-byline,.docs-project-embed-counts{color:var(--muted);font-size:var(--hcm-font-size-small)}` +
		`.docs-project-embed-meta{display:flex;flex-wrap:wrap;align-items:center;gap:.35rem .9rem;color:var(--muted);font-size:var(--hcm-font-size-small)}` +
		`.docs-project-embed-status{display:inline-flex;align-items:center;gap:.35rem;padding:.05rem .55rem;border-radius:999px;background:color-mix(in srgb,var(--ink) 7%,transparent);color:var(--ink);font-weight:600}` +
		`.docs-project-embed-dot{inline-size:.45rem;block-size:.45rem;border-radius:50%;background:var(--muted)}` +
		`.docs-project-embed-status[data-tone="doing"] .docs-project-embed-dot{background:var(--hcm-color-info,#2563eb)}.docs-project-embed-status[data-tone="done"] .docs-project-embed-dot{background:var(--hcm-color-success,#15803d)}` +
		`.docs-project-embed-priority[data-level="high"]{color:var(--hcm-color-warning,#b45309);font-weight:600}.docs-project-embed-priority[data-level="urgent"]{color:var(--hcm-color-danger,#b42318);font-weight:600}` +
		`.docs-project-embed-due[data-tone="overdue"]{color:var(--hcm-color-danger,#b42318);font-weight:600}.docs-project-embed-due[data-tone="soon"]{color:var(--hcm-color-warning,#b45309);font-weight:600}` +
		`.docs-project-embed-assignee{display:inline-flex;align-items:center;gap:.35rem;color:var(--ink)}` +
		`.docs-project-embed-assignee .avatar{inline-size:1.25rem;block-size:1.25rem;min-inline-size:1.25rem;font-size:.5625rem}` +
		`.docs-project-embed-progress{display:flex;align-items:center;gap:.65rem}` +
		`.docs-project-embed-bar{flex:1;max-inline-size:14rem;block-size:.4rem;border-radius:999px;background:color-mix(in srgb,var(--ink) 10%,transparent);overflow:hidden}` +
		`.docs-project-embed-fill{display:block;block-size:100%;border-radius:inherit;background:var(--hcm-color-success,#15803d)}`
}
