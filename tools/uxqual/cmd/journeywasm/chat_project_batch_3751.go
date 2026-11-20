package main

import (
	"strings"

	projectv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/project/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/projectui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/projectclient"
)

// chatProjectPreviewRequest is the one batched read for every claimed task or
// board link (issue 3751). The server answers each ref as the viewer, so the
// browser no longer makes three or four calls per link.
func chatProjectPreviewRequest(refs []chatui.ProjectTaskReference) *projectv1.GetProjectPreviewsRequest {
	request := &projectv1.GetProjectPreviewsRequest{Refs: make([]*projectv1.ProjectPreviewRef, 0, len(refs))}
	for _, ref := range refs {
		request.Refs = append(request.Refs, &projectv1.ProjectPreviewRef{ProjectId: ref.ProjectID, TaskId: ref.TaskID})
	}
	return request
}

// chatProjectBatchAnswers turns a batched response into one cached answer per
// requested ref. A failed call is a retryable failure for every ref unless it
// is a refusal or a gone record (chatProjectReadFailureState); a ref the
// server did not answer is a retryable failure too, never a claim that the
// reader lacks access. directory resolves assignee names and photos and is
// only asked when a ready task has an assignee.
func chatProjectBatchAnswers(refs []chatui.ProjectTaskReference, response *projectv1.GetProjectPreviewsResponse, callErr error, viewer string, directory func() (map[string]string, map[string]string)) map[string]chatui.ProjectTaskPreview {
	answers := make(map[string]chatui.ProjectTaskPreview, len(refs))
	byKey := make(map[string]*projectv1.ProjectPreview, len(response.GetPreviews()))
	for _, item := range response.GetPreviews() {
		if item != nil {
			byKey[chatui.ProjectTaskPreviewKey(item.GetProjectId(), item.GetTaskId())] = item
		}
	}
	for _, ref := range refs {
		key := chatui.ProjectTaskPreviewKey(ref.ProjectID, ref.TaskID)
		switch item := byKey[key]; {
		case callErr != nil:
			answers[key] = chatProjectTaskAnswer(ref, chatProjectReadFailureState(callErr))
		case item == nil:
			answers[key] = chatProjectTaskAnswer(ref, "failed")
		default:
			answers[key] = chatProjectBatchAnswer(ref, item, viewer, directory)
		}
	}
	return answers
}

func chatProjectBatchAnswer(ref chatui.ProjectTaskReference, item *projectv1.ProjectPreview, viewer string, directory func() (map[string]string, map[string]string)) chatui.ProjectTaskPreview {
	switch item.GetState() {
	case projectv1.ProjectPreviewState_PROJECT_PREVIEW_STATE_READY:
	case projectv1.ProjectPreviewState_PROJECT_PREVIEW_STATE_MISSING:
		return chatProjectTaskAnswer(ref, "missing")
	case projectv1.ProjectPreviewState_PROJECT_PREVIEW_STATE_ARCHIVED:
		// Issue 3750: an archived item the viewer may read keeps its title.
		answer := chatProjectTaskAnswer(ref, "archived")
		if title := strings.TrimSpace(item.GetTitle()); title != "" {
			answer.Title, answer.Readable = title, true
		}
		return answer
	case projectv1.ProjectPreviewState_PROJECT_PREVIEW_STATE_FAILED:
		return chatProjectTaskAnswer(ref, "failed")
	default:
		return restrictedChatProjectTaskAnswer(ref)
	}
	projectName := strings.TrimSpace(item.GetProjectName())
	if ref.TaskID == "" {
		if projectName == "" {
			return restrictedChatProjectTaskAnswer(ref)
		}
		preview := chatui.ProjectTaskPreview{ProjectID: ref.ProjectID, Title: projectName, ProjectName: projectName, Readable: true, State: "ready"}
		if item.GetProgressKnown() {
			preview.Done, preview.Total = int(item.GetDoneTasks()), int(item.GetTotalTasks())
		}
		return preview
	}
	title := strings.TrimSpace(item.GetTitle())
	if title == "" {
		return restrictedChatProjectTaskAnswer(ref)
	}
	preview := chatui.ProjectTaskPreview{
		ProjectID: ref.ProjectID, TaskID: ref.TaskID, Title: title, Readable: true, State: "ready",
		ProjectName: projectName, Key: projectui.TaskKey(ref.TaskID), PriorityID: item.GetPriority().String(), Due: item.GetDueDate(),
	}
	if name := strings.TrimSpace(item.GetStatusName()); name != "" {
		preview.Status = name
		switch item.GetStatusCategory() {
		case projectv1.ProjectStatusCategory_PROJECT_STATUS_CATEGORY_DONE, projectv1.ProjectStatusCategory_PROJECT_STATUS_CATEGORY_CANCELLED:
			preview.StatusTone = "done"
		case projectv1.ProjectStatusCategory_PROJECT_STATUS_CATEGORY_ACTIVE, projectv1.ProjectStatusCategory_PROJECT_STATUS_CATEGORY_BLOCKED:
			preview.StatusTone = "doing"
		default:
			preview.StatusTone = "todo"
		}
	}
	if assignee := item.GetAssigneeId(); assignee != "" {
		var names, photos map[string]string
		if directory != nil {
			names, photos = directory()
		}
		preview.Assignee, preview.AssigneePhoto = projectclient.PersonName(assignee, names), photos[assignee]
		// The claim rests on the stable assignee id, never on the display name (#3994).
		preview.WaitsOnViewer = chatui.TaskWaitsOnSubject(preview.StatusTone, assignee, strings.TrimSpace(viewer))
	}
	return preview
}
