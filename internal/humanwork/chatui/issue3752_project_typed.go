package chatui

import (
	"net/url"
	"sort"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// ChatReferenceProjectTask is the kind of a typed reference that "Send to
// chat" attaches to a post (issue 3752). Its ID is "<project>" for a board or
// "<project>/<task>" for a task. The server authorized the sender when the
// post was written and authorizes each reader when it is read, so a reference
// the reader may not open arrives with no ID at all.
const ChatReferenceProjectTask = "PROJECT_TASK"

// ProjectTaskChatReference splits a typed reference into its project and task.
func ProjectTaskChatReference(ref ChatReference) (projectID, taskID string, ok bool) {
	if ref.Kind != ChatReferenceProjectTask {
		return "", "", false
	}
	parts := strings.Split(ref.ID, "/")
	if len(parts) > 2 {
		return "", "", false
	}
	projectID = parts[0]
	if len(parts) == 2 {
		taskID = parts[1]
	}
	if !validProjectTaskID(projectID) || (len(parts) == 2 && !validProjectTaskID(taskID)) {
		return "", "", false
	}
	return projectID, taskID, true
}

// ProjectTaskReferencesFor is ProjectTaskReferences for a message that may
// carry typed references. The typed reference, not the text of the body, says
// which board or task the message points at:
//   - an address in the body that names the same board or task unfurls on any
//     origin, since the typed reference vouches for it;
//   - a typed reference whose address is not in the body (the author edited it
//     away) is returned with Start and End of -1: a card, no inline text;
//   - an address on the viewer's own origin that points at the project page
//     but cannot be understood is returned Unresolved, so it can say so in
//     words instead of staying a raw, truncated URL.
func ProjectTaskReferencesFor(body, origin string, typed []ChatReference) []ProjectTaskReference {
	refs := ProjectTaskReferences(body, origin)
	if len(body) > 32*1024 {
		body = body[:32*1024]
	}
	want := map[string]bool{}
	var order []string
	for _, ref := range typed {
		if projectID, taskID, ok := ProjectTaskChatReference(ref); ok {
			key := projectID + "\x00" + taskID
			if !want[key] {
				want[key] = true
				order = append(order, key)
			}
		}
	}
	base, baseErr := url.Parse(origin)
	overlaps := func(start, end int) bool {
		for _, ref := range refs {
			if ref.Start >= 0 && start < ref.End && ref.Start < end {
				return true
			}
		}
		return false
	}
	for _, span := range projectTaskURLPattern.FindAllStringIndex(body, 16) {
		candidate := strings.TrimRight(body[span[0]:span[1]], ".,!?;:)]}>")
		end := span[0] + len(candidate)
		if overlaps(span[0], end) {
			continue
		}
		parsed, err := url.Parse(candidate)
		if err != nil || parsed.User != nil || parsed.Path != "/workspace/app/project" {
			continue
		}
		sameOrigin := !parsed.IsAbs() || (baseErr == nil && base.Host != "" && strings.EqualFold(parsed.Scheme, base.Scheme) && strings.EqualFold(parsed.Host, base.Host))
		if !parsed.IsAbs() && (parsed.Host != "" || parsed.Scheme != "") {
			continue
		}
		values, queryErr := url.ParseQuery(parsed.RawQuery)
		projectID, taskID := values.Get("project"), values.Get("task")
		understood := queryErr == nil && len(values["project"]) == 1 && len(values["task"]) <= 1 && validProjectTaskID(projectID) && (len(values["task"]) == 0 || validProjectTaskID(taskID))
		if understood && want[projectID+"\x00"+taskID] && parsed.Fragment == "" {
			refs = append(refs, ProjectTaskReference{ProjectID: projectID, TaskID: taskID, Start: span[0], End: end})
			continue
		}
		if sameOrigin && baseErr == nil && base.Host != "" {
			refs = append(refs, ProjectTaskReference{Start: span[0], End: end, Unresolved: true})
		}
	}
	sort.SliceStable(refs, func(i, j int) bool { return refs[i].Start < refs[j].Start })
	placed := map[string]bool{}
	for _, ref := range refs {
		if !ref.Unresolved {
			placed[ref.ProjectID+"\x00"+ref.TaskID] = true
		}
	}
	for _, key := range order {
		if placed[key] {
			continue
		}
		parts := strings.SplitN(key, "\x00", 2)
		refs = append(refs, ProjectTaskReference{ProjectID: parts[0], TaskID: parts[1], Start: -1, End: -1})
	}
	return refs
}

// projectUnresolvedLabel is the words on the chip that stands in for a project
// address that could not be understood.
func projectUnresolvedLabel(m Model) string {
	return chatbug039Text("", projectUnresolvedCopy[chatbug039LocaleIndex(m.Locale)], projectUnresolvedCopy[0])
}

var projectUnresolvedCopy = [3]string{
	"This project link can’t be opened",
	"Dieser Projektlink kann nicht geöffnet werden",
	"لا يمكن فتح رابط المشروع هذا",
}

func projectUnresolvedChip(m Model) ui.Node {
	label := projectUnresolvedLabel(m)
	return html.Span(html.Props{Class: "chat-project-task-restricted chat-project-task-unresolved", Data: map[string]string{"state": "unresolved"}, Aria: map[string]string{"label": label}}, ui.Text(label))
}
