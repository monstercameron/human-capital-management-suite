package productui

import (
	"encoding/json"
	"net/url"
	"regexp"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

const docsProjectTaskPath = "/workspace/app/project"

var docsProjectTaskID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._~-]{0,127}$`)

func docsProjectTaskIDChar(value byte) bool {
	return value >= 'A' && value <= 'Z' || value >= 'a' && value <= 'z' || value >= '0' && value <= '9' || strings.ContainsRune("._~-", rune(value))
}

// DocsProjectTaskReference is a presentation selector only. It never carries
// task data or grants access to a project task.
type DocsProjectTaskReference struct {
	ProjectID string
	TaskID    string
}

// DocsProjectTaskPreview is populated only from the server's currently
// authorized projection. Callers must leave Authorized false when resolution
// is denied, missing, or unavailable.
// An empty TaskID describes the project's board; its Title is the project
// name and Status stays empty.
type DocsProjectTaskPreview struct {
	ProjectID  string
	TaskID     string
	Title      string
	Status     string
	Authorized bool
	// Quick-look card detail, from the same authorized reads.
	ProjectName   string `json:",omitempty"`
	Key           string `json:",omitempty"`
	StatusTone    string `json:",omitempty"` // todo, doing or done
	Assignee      string `json:",omitempty"`
	AssigneePhoto string `json:",omitempty"`
	PriorityID    string `json:",omitempty"`
	DueDate       string `json:",omitempty"`
	Done          int    `json:",omitempty"`
	Total         int    `json:",omitempty"`
}

// ParseDocsProjectTaskReference accepts a typed task:<id> token or the
// canonical in-app task URL. A typed reference has no project selector; the
// authorized preview supplies its owning project before navigation.
func ParseDocsProjectTaskReference(target string) (DocsProjectTaskReference, bool) {
	target = strings.TrimSpace(target)
	if strings.HasPrefix(target, "task:") {
		id := strings.TrimPrefix(target, "task:")
		if docsProjectTaskID.MatchString(id) {
			return DocsProjectTaskReference{TaskID: id}, true
		}
		return DocsProjectTaskReference{}, false
	}
	parsed, err := url.ParseRequestURI(target)
	if err != nil || parsed.IsAbs() || parsed.Host != "" || parsed.Path != docsProjectTaskPath || parsed.Fragment != "" {
		return DocsProjectTaskReference{}, false
	}
	values, err := url.ParseQuery(parsed.RawQuery)
	if err != nil || len(values["project"]) != 1 || len(values["task"]) > 1 {
		return DocsProjectTaskReference{}, false
	}
	ref := DocsProjectTaskReference{ProjectID: values.Get("project"), TaskID: values.Get("task")}
	if !docsProjectTaskID.MatchString(ref.ProjectID) || len(values["task"]) == 1 && !docsProjectTaskID.MatchString(ref.TaskID) || !validDocsProjectTaskRouteValues(values) {
		return DocsProjectTaskReference{}, false
	}
	return ref, true
}

// docsProjectRelative reduces an absolute http(s) project address, the kind
// copied from the address bar, to its in-app path and query. Other targets
// pass through unchanged.
func docsProjectRelative(target string) (string, bool) {
	target = strings.TrimSpace(target)
	parsed, err := url.Parse(target)
	if err != nil || !parsed.IsAbs() || parsed.Scheme != "https" && parsed.Scheme != "http" {
		return target, false
	}
	if parsed.User != nil || parsed.Host == "" || parsed.Fragment != "" {
		return "", true
	}
	return parsed.EscapedPath() + "?" + parsed.RawQuery, true
}

// parseDocsProjectTaskReferenceLoose is link discovery for the resolver: it
// also accepts absolute addresses from any origin, because a lookup is by ID
// only. What is rendered as a link is gated by docsProjectSameOrigin.
func parseDocsProjectTaskReferenceLoose(target string) (DocsProjectTaskReference, bool) {
	relative, _ := docsProjectRelative(target)
	return ParseDocsProjectTaskReference(relative)
}

// docsProjectSameOrigin reports whether an absolute http(s) project address
// belongs to this workspace; relative addresses and task tokens always do.
func docsProjectSameOrigin(view View, target string) bool {
	if _, absolute := docsProjectRelative(target); !absolute {
		return true
	}
	parsed, err := url.Parse(strings.TrimSpace(target))
	origin, originErr := url.Parse(view.DocumentOrigin)
	return err == nil && originErr == nil && origin.Host != "" && strings.EqualFold(origin.Scheme, parsed.Scheme) && strings.EqualFold(origin.Host, parsed.Host)
}

// docsProjectRouteKeys are the selectors a shared project address may carry:
// project and task select the target; the rest are presentation state that
// rides along when someone copies the address they are looking at.
var docsProjectRouteKeys = map[string]bool{"project": true, "task": true, "board_view": true, "view": true, "lane": true, "filter": true, "q": true, "sort": true, "locale": true, "nav": true, "cursor": true}

func validDocsProjectTaskRouteValues(values url.Values) bool {
	for key, entries := range values {
		if !docsProjectRouteKeys[key] || len(entries) != 1 {
			return false
		}
		value := entries[0]
		if strings.ContainsAny(value, "\x00\r\n") || len(value) > 4096 {
			return false
		}
		switch key {
		case "view":
			if value != "list" && value != "board" && value != "task" {
				return false
			}
		case "lane", "board_view":
			if value != "" && !docsProjectTaskID.MatchString(value) {
				return false
			}
		}
	}
	return true
}

// DocsProjectBoardHref returns the stable canonical in-app route for a board.
func DocsProjectBoardHref(projectID string) string {
	if !docsProjectTaskID.MatchString(projectID) {
		return ""
	}
	return docsProjectTaskPath + "?" + url.Values{"project": []string{projectID}}.Encode()
}

// DocsProjectTaskHref returns the stable canonical in-app route for a task.
func DocsProjectTaskHref(ref DocsProjectTaskReference) string {
	if !docsProjectTaskID.MatchString(ref.ProjectID) || !docsProjectTaskID.MatchString(ref.TaskID) {
		return ""
	}
	values := url.Values{"project": []string{ref.ProjectID}, "task": []string{ref.TaskID}}
	return docsProjectTaskPath + "?" + values.Encode()
}

// DocsProjectTaskReferences extracts distinct Markdown link targets in source
// order. It is a selector discovery helper only; callers must resolve every
// returned target through the project service before showing task data.
func DocsProjectTaskReferences(markdown string) []DocsProjectTaskReference {
	if len(markdown) > 64*1024 {
		return nil
	}
	source := []byte(markdown)
	root := docsMarkdownParser.Parse(text.NewReader(source))
	refs := make([]DocsProjectTaskReference, 0)
	seen := make(map[DocsProjectTaskReference]struct{})
	_ = ast.Walk(root, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		var target string
		switch link := node.(type) {
		case *ast.Link:
			target = string(link.Destination)
		case *ast.AutoLink:
			target = string(link.URL(source))
		}
		if ref, ok := parseDocsProjectTaskReferenceLoose(target); ok {
			if _, exists := seen[ref]; !exists {
				seen[ref] = struct{}{}
				refs = append(refs, ref)
			}
		}
		return ast.WalkContinue, nil
	})
	return refs
}

func encodeDocsProjectTasks(previews []DocsProjectTaskPreview) string {
	if len(previews) == 0 {
		return ""
	}
	data, err := json.Marshal(previews)
	if err != nil {
		return ""
	}
	return string(data)
}

func decodeDocsProjectTasks(value string) []DocsProjectTaskPreview {
	var previews []DocsProjectTaskPreview
	if value != "" {
		_ = json.Unmarshal([]byte(value), &previews)
	}
	return previews
}

func docsProjectTaskPreview(view View, ref DocsProjectTaskReference) (DocsProjectTaskPreview, bool) {
	if view.Document == nil {
		return DocsProjectTaskPreview{}, false
	}
	var match DocsProjectTaskPreview
	found := false
	for _, preview := range view.Document.ProjectTasks {
		if preview.TaskID == ref.TaskID && (ref.ProjectID == "" || preview.ProjectID == ref.ProjectID) {
			if found && ref.ProjectID == "" {
				return DocsProjectTaskPreview{}, false
			}
			match, found = preview, true
		}
	}
	return match, found
}

func docsProjectTaskHasAuthorizedPreview(view View, ref DocsProjectTaskReference) bool {
	preview, ok := docsProjectTaskPreview(view, ref)
	return ok && docsProjectPreviewUsable(ref, preview)
}

// docsProjectPreviewUsable is the one gate every rendering passes: the
// preview must be authorized, name the referenced target, and carry a title
// (and, for a task, a status).
func docsProjectPreviewUsable(ref DocsProjectTaskReference, preview DocsProjectTaskPreview) bool {
	if !preview.Authorized || preview.TaskID != ref.TaskID || preview.ProjectID == "" || ref.ProjectID != "" && ref.ProjectID != preview.ProjectID || strings.TrimSpace(preview.Title) == "" {
		return false
	}
	return ref.TaskID == "" || strings.TrimSpace(preview.Status) != ""
}

func docsProjectTaskLinkTarget(view View, target string, navigate func(string)) (ui.Node, bool) {
	if !docsProjectSameOrigin(view, target) {
		return nil, false
	}
	ref, ok := parseDocsProjectTaskReferenceLoose(target)
	if !ok {
		return nil, false
	}
	preview, _ := docsProjectTaskPreview(view, ref)
	return docsProjectTaskLinkNode(view.Locale.Resolved, ref, preview, navigate), true
}

// docsProjectTaskLinkNode renders a safe authorized task preview. softwareLink
// uses View.Navigate for persistent in-app history and leaves the href as a
// progressive-enhancement fallback. Denied and unknown references have the
// same neutral text and expose no target identity or preview fields.
func docsProjectTaskLinkNode(locale string, ref DocsProjectTaskReference, preview DocsProjectTaskPreview, navigate func(string)) ui.Node {
	copy := docsProjectTaskCopy(locale)
	if !docsProjectPreviewUsable(ref, preview) {
		return html.Span(html.Props{Class: "docs-project-task-restricted", Role: "status"}, ui.Text(copy.unavailable))
	}
	if ref.TaskID == "" {
		href := DocsProjectBoardHref(preview.ProjectID)
		return softwareLink(navigate, html.Props{Class: "docs-project-task-link", Data: map[string]string{"docs-project-task": href}, Raw: map[string]any{"aria-label": copy.openBoard + ": " + strings.TrimSpace(preview.Title)}}, href,
			html.Span(html.Props{Class: "docs-project-task-title"}, ui.Text(strings.TrimSpace(preview.Title))))
	}
	resolved := DocsProjectTaskReference{ProjectID: preview.ProjectID, TaskID: preview.TaskID}
	href := DocsProjectTaskHref(resolved)
	if href == "" {
		return html.Span(html.Props{Class: "docs-project-task-restricted", Role: "status"}, ui.Text(copy.unavailable))
	}
	return softwareLink(navigate, html.Props{
		Class: "docs-project-task-link",
		Data:  map[string]string{"docs-project-task": href},
		Raw:   map[string]any{"aria-label": copy.open + ": " + strings.TrimSpace(preview.Title) + " (" + strings.TrimSpace(preview.Status) + ")"},
	}, href,
		html.Span(html.Props{Class: "docs-project-task-title"}, ui.Text(strings.TrimSpace(preview.Title))),
		html.Span(html.Props{Class: "docs-project-task-status"}, ui.Text(strings.TrimSpace(preview.Status))),
	)
}

type docsProjectTaskLocaleCopy struct {
	open, openBoard, unavailable, taskKind, boardKind, due, noTasks string
	progress                                                        func(done, total, open string) string
	priorities                                                      map[string]string
}

func docsProjectTaskCopy(locale string) docsProjectTaskLocaleCopy {
	switch locale {
	case "de-DE":
		return docsProjectTaskLocaleCopy{open: "Projektaufgabe öffnen", openBoard: "Projekt-Board öffnen", unavailable: "Projektaufgabe nicht verfügbar", taskKind: "Projektaufgabe", boardKind: "Projekt-Board", due: "Fällig {date}", noTasks: "Noch keine Aufgaben",
			progress: func(done, total, open string) string {
				return done + " von " + total + " erledigt · " + open + " offen"
			},
			priorities: map[string]string{"low": "Niedrig", "normal": "Normal", "high": "Hoch", "urgent": "Dringend"}}
	case "ar":
		return docsProjectTaskLocaleCopy{open: "فتح مهمة المشروع", openBoard: "فتح لوحة المشروع", unavailable: "مهمة المشروع غير متاحة", taskKind: "مهمة مشروع", boardKind: "لوحة مشروع", due: "الاستحقاق {date}", noTasks: "لا توجد مهام بعد",
			progress: func(done, total, open string) string {
				return "أُنجزت " + done + " من " + total + " · " + open + " مفتوحة"
			},
			priorities: map[string]string{"low": "منخفضة", "normal": "عادية", "high": "عالية", "urgent": "عاجلة"}}
	default:
		return docsProjectTaskLocaleCopy{open: "Open project task", openBoard: "Open project board", unavailable: "Project task unavailable", taskKind: "Project task", boardKind: "Project board", due: "Due {date}", noTasks: "No tasks yet",
			progress:   func(done, total, open string) string { return done + " of " + total + " done · " + open + " open" },
			priorities: map[string]string{"low": "Low", "normal": "Normal", "high": "High", "urgent": "Urgent"}}
	}
}

func docsProjectTaskStylesheet() string {
	return `.docs-markdown .docs-project-task-link{display:inline-flex;align-items:baseline;gap:.4em;color:var(--accent);text-decoration:underline;text-underline-offset:.15em}.docs-markdown .docs-project-task-link:focus-visible{outline:var(--hcm-focus-ring-width) solid var(--hcm-color-focus);outline-offset:2px}.docs-project-task-status,.docs-project-task-restricted{color:var(--muted);font-size:var(--hcm-font-size-small)}`
}
