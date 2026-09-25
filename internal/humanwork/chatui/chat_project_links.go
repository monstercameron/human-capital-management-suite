package chatui

import (
	"net/url"
	"regexp"
	"sort"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// ProjectTaskReference identifies a project-owned task, or a whole project
// board when TaskID is empty, mentioned in a chat message. A task token may
// not carry its project; the authorized resolver must supply that before a
// navigable link is rendered.
type ProjectTaskReference struct {
	ProjectID string
	TaskID    string
	Start     int
	End       int
}

// ProjectTaskPreview is a server-authorized projection for one viewer.
// Unreadable and unresolved previews must not carry a title.
// A board preview has an empty TaskID and carries the project name as Title.
// Every field stays scalar: the preview cache compares values with ==.
type ProjectTaskPreview struct {
	ProjectID string
	TaskID    string
	Title     string
	Readable  bool
	State     string // loading, ready, restricted, unavailable
	// Card detail, already authorized and localized by the resolver.
	ProjectName   string
	Key           string
	Status        string
	StatusTone    string // todo, doing or done
	Assignee      string
	AssigneePhoto string
	Priority      string
	PriorityID    string
	Due           string
	DueTone       string // overdue, soon or empty
	// Board previews: task counts on the project.
	Done, Total int
}

// IsBoard reports whether the preview describes a whole project board.
func (p ProjectTaskPreview) IsBoard() bool { return p.TaskID == "" }

// ProjectTaskPreviewKey scopes a cached preview to one exact project and task.
// An empty taskID keys the project's board.
func ProjectTaskPreviewKey(projectID, taskID string) string {
	if !validProjectTaskID(projectID) || (taskID != "" && !validProjectTaskID(taskID)) {
		return ""
	}
	return projectID + "\x00" + taskID
}

var projectTaskTokenPattern = regexp.MustCompile(`task:[^\s<>"']+`)
var projectTaskURLPattern = regexp.MustCompile(`https?://[^\s<>"']+|/workspace/app/project\?[^\s<>"']+`)
var projectTaskIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._~-]{0,127}$`)

// ProjectBoardReferenceURL returns the canonical in-app URL for a project board.
func ProjectBoardReferenceURL(projectID string) string {
	if !validProjectTaskID(projectID) {
		return ""
	}
	return "/workspace/app/project?project=" + url.QueryEscape(projectID)
}

// projectReferenceKeys are the selectors a shared project address may carry.
// Presentation keys (view, locale, nav, filters) ride along when someone
// copies the address they are looking at; only project and task select what
// a preview shows.
var projectReferenceKeys = map[string]bool{"project": true, "task": true, "board_view": true, "view": true, "locale": true, "nav": true, "lane": true, "filter": true, "q": true, "sort": true, "cursor": true, "menu_q": true, "favorites": true}

// ProjectTaskReferenceURL returns the canonical in-app URL for a project task.
func ProjectTaskReferenceURL(projectID, taskID string) string {
	if !validProjectTaskID(projectID) || !validProjectTaskID(taskID) {
		return ""
	}
	return "/workspace/app/project?project=" + url.QueryEscape(projectID) + "&task=" + url.QueryEscape(taskID)
}

// ProjectTaskReferences recognizes bare task:<id> tokens and canonical
// same-origin project task URLs. Task IDs are selectors only; authorization
// remains the server's responsibility. Foreign origins and noncanonical or
// malformed URLs are ignored.
func ProjectTaskReferences(body, origin string) []ProjectTaskReference {
	if len(body) > 32*1024 {
		body = body[:32*1024]
	}
	var refs []ProjectTaskReference
	for _, span := range projectTaskTokenPattern.FindAllStringIndex(body, 16) {
		token := strings.TrimRight(body[span[0]:span[1]], ",!?;:)]}>")
		id := strings.TrimPrefix(token, "task:")
		if validProjectTaskID(id) {
			refs = append(refs, ProjectTaskReference{TaskID: id, Start: span[0], End: span[0] + len(token)})
		}
	}
	base, err := url.Parse(origin)
	if err == nil && base.IsAbs() && base.Host != "" && base.User == nil {
		for _, span := range projectTaskURLPattern.FindAllStringIndex(body, 16) {
			full := body[span[0]:span[1]]
			candidate := strings.TrimRight(full, ".,!?;:)]}>")
			parsed, parseErr := url.Parse(candidate)
			if parseErr != nil || parsed.User != nil || parsed.Fragment != "" || parsed.Path != "/workspace/app/project" {
				continue
			}
			if parsed.IsAbs() && (!strings.EqualFold(parsed.Scheme, base.Scheme) || !strings.EqualFold(parsed.Host, base.Host)) {
				continue
			}
			if !parsed.IsAbs() && (parsed.Host != "" || parsed.Scheme != "") {
				continue
			}
			values, queryErr := url.ParseQuery(parsed.RawQuery)
			if queryErr != nil || len(values["project"]) != 1 || len(values["task"]) > 1 || values.Encode() != parsed.RawQuery {
				continue
			}
			known := true
			for key, entries := range values {
				if !projectReferenceKeys[key] || len(entries) != 1 {
					known = false
					break
				}
			}
			projectID, taskID := values.Get("project"), values.Get("task")
			if !known || !validProjectTaskID(projectID) || (len(values["task"]) == 1 && !validProjectTaskID(taskID)) {
				continue
			}
			refs = append(refs, ProjectTaskReference{ProjectID: projectID, TaskID: taskID, Start: span[0], End: span[0] + len(candidate)})
		}
	}
	if len(refs) < 2 {
		return refs
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i].Start < refs[j].Start })
	merged := refs[:1]
	for _, ref := range refs[1:] {
		if ref.Start < merged[len(merged)-1].End {
			continue
		}
		merged = append(merged, ref)
	}
	return merged
}

// ProjectTaskReferenceBody replaces recognized addresses with safe labels.
// Until a ready, readable preview arrives it shows only restrictedLabel and
// emits non-link text, preventing raw IDs or untrusted titles from appearing.
func ProjectTaskReferenceBody(body, origin, restrictedLabel string, previews map[string]ProjectTaskPreview) []ui.Node {
	refs := ProjectTaskReferences(body, origin)
	if len(refs) == 0 {
		return []ui.Node{ui.Text(body)}
	}
	var nodes []ui.Node
	last := 0
	for _, ref := range refs {
		if ref.Start < last {
			continue
		}
		if ref.Start > last {
			nodes = append(nodes, ui.Text(body[last:ref.Start]))
		}
		preview, ok := previews[ProjectTaskPreviewKey(ref.ProjectID, ref.TaskID)]
		if ok && ref.ProjectID != "" && preview.State == "ready" && preview.Readable && preview.TaskID == ref.TaskID && preview.ProjectID == ref.ProjectID && preview.Title != "" {
			href := ProjectTaskReferenceURL(preview.ProjectID, preview.TaskID)
			if preview.IsBoard() {
				href = ProjectBoardReferenceURL(preview.ProjectID)
			}
			nodes = append(nodes, html.A(html.Props{Class: "chat-project-task-reference", Href: href, Data: map[string]string{"action": "open-project-task-reference", "project": preview.ProjectID, "task": preview.TaskID, "id": preview.ProjectID, "extra": preview.TaskID}, Aria: map[string]string{"label": "Open project task: " + preview.Title}}, ui.Text(preview.Title)))
		} else {
			nodes = append(nodes, html.Span(html.Props{Class: "chat-project-task-restricted", Aria: map[string]string{"label": restrictedLabel}}, ui.Text(restrictedLabel)))
		}
		last = ref.End
	}
	if last < len(body) {
		nodes = append(nodes, ui.Text(body[last:]))
	}
	return nodes
}

// projectTaskReferenceBody composes task references with the existing
// document and channel renderers for each plain Markdown text run.
func projectTaskReferenceBody(m Model, body string) []ui.Node {
	refs := ProjectTaskReferences(body, m.EmbedOrigin)
	if len(refs) == 0 {
		return docLinkReferenceBody(m, body)
	}
	var nodes []ui.Node
	last := 0
	for _, ref := range refs {
		if ref.Start < last {
			continue
		}
		if ref.Start > last {
			nodes = append(nodes, docLinkReferenceBody(m, body[last:ref.Start])...)
		}
		preview, ok := m.ProjectTaskPreviews[ProjectTaskPreviewKey(ref.ProjectID, ref.TaskID)]
		if ok && ref.ProjectID != "" && preview.State == "ready" && preview.Readable && preview.ProjectID == ref.ProjectID && preview.TaskID == ref.TaskID && strings.TrimSpace(preview.Title) != "" {
			label := strings.TrimSpace(preview.Title)
			href, aria := ProjectTaskReferenceURL(ref.ProjectID, ref.TaskID), m.tf(KeyOpenProjectTask, map[string]string{"title": label})
			if ref.TaskID == "" {
				href, aria = ProjectBoardReferenceURL(ref.ProjectID), m.tf(KeyOpenProjectBoard, map[string]string{"title": label})
			}
			nodes = append(nodes, html.A(html.Props{Class: "chat-project-task-reference", Href: href, Data: map[string]string{"action": "open-project-task-reference", "project": ref.ProjectID, "task": ref.TaskID, "id": ref.ProjectID, "extra": ref.TaskID}, Aria: map[string]string{"label": aria}}, ui.Text(label)))
		} else {
			nodes = append(nodes, html.Span(html.Props{Class: "chat-project-task-restricted", Aria: map[string]string{"label": m.t(KeyProjectTaskRestricted)}}, ui.Text(m.t(KeyProjectTaskRestricted))))
		}
		last = ref.End
	}
	if last < len(body) {
		nodes = append(nodes, docLinkReferenceBody(m, body[last:])...)
	}
	return nodes
}

// resolveProjectTaskReferencesForSnippet hides raw task references in search
// snippets, which are rendered as plain text rather than through Markdown.
func resolveProjectTaskReferencesForSnippet(m Model, body string) string {
	refs := ProjectTaskReferences(body, m.EmbedOrigin)
	if len(refs) == 0 {
		return body
	}
	var out strings.Builder
	last := 0
	for _, ref := range refs {
		if ref.Start < last {
			continue
		}
		out.WriteString(body[last:ref.Start])
		label := m.t(KeyProjectTaskRestricted)
		if preview, ok := m.ProjectTaskPreviews[ProjectTaskPreviewKey(ref.ProjectID, ref.TaskID)]; ok && ref.ProjectID != "" && preview.State == "ready" && preview.Readable && preview.ProjectID == ref.ProjectID && preview.TaskID == ref.TaskID && strings.TrimSpace(preview.Title) != "" {
			label = strings.TrimSpace(preview.Title)
		}
		out.WriteString(label)
		last = ref.End
	}
	out.WriteString(body[last:])
	return out.String()
}

func validProjectTaskID(id string) bool { return projectTaskIDPattern.MatchString(id) }
