//go:build js && wasm

package main

import (
	"context"
	"strings"

	projectv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/project/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/projectui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/projectclient"
)

// resolveChatProjectPreview reads one shared project address as the current
// viewer. A task link becomes a task card (status, priority, due date and
// assignee from the task, its project's name and workflow); a board link
// becomes a board card with task counts. Any refused or failed read yields
// the neutral restricted answer, so nothing unauthorized reaches the card.
func resolveChatProjectPreview(ctx context.Context, active journeyclient.Config, client projectv1.ProjectServiceClient, ref chatui.ProjectTaskReference) chatui.ProjectTaskPreview {
	value := restrictedChatProjectTaskAnswer(ref)
	callCtx := chatRPCContext(ctx, active)
	project, err := client.GetProject(callCtx, &projectv1.GetProjectRequest{ProjectId: ref.ProjectID})
	if err != nil || project.GetProject().GetProjectId() != ref.ProjectID {
		return value
	}
	projectName := strings.TrimSpace(project.GetProject().GetName())
	if ref.TaskID == "" {
		if projectName == "" {
			return value
		}
		preview := chatui.ProjectTaskPreview{ProjectID: ref.ProjectID, Title: projectName, ProjectName: projectName, Readable: true, State: "ready"}
		if counts, err := countProjectProgress(ctx, active, client, ref.ProjectID); err == nil {
			preview.Done, preview.Total = counts.done, counts.total
		}
		return preview
	}
	response, err := client.GetTask(callCtx, &projectv1.GetTaskRequest{ProjectId: ref.ProjectID, TaskId: ref.TaskID})
	task := response.GetTask()
	if err != nil || task == nil || task.GetProjectId() != ref.ProjectID || task.GetTaskId() != ref.TaskID || strings.TrimSpace(task.GetTitle()) == "" {
		return value
	}
	preview := chatui.ProjectTaskPreview{
		ProjectID: ref.ProjectID, TaskID: ref.TaskID, Title: strings.TrimSpace(task.GetTitle()), Readable: true, State: "ready",
		ProjectName: projectName, Key: projectui.TaskKey(task.GetTaskId()), PriorityID: task.GetPriority().String(), Due: task.GetDueDate(),
	}
	if configuration, err := client.GetWorkflowConfiguration(callCtx, &projectv1.GetWorkflowConfigurationRequest{ProjectId: ref.ProjectID}); err == nil {
		for _, status := range configuration.GetConfiguration().GetStatuses() {
			if status.GetStatusId() != task.GetStatusId() {
				continue
			}
			preview.Status = strings.TrimSpace(status.GetName())
			switch status.GetCategory() {
			case projectv1.ProjectStatusCategory_PROJECT_STATUS_CATEGORY_DONE, projectv1.ProjectStatusCategory_PROJECT_STATUS_CATEGORY_CANCELLED:
				preview.StatusTone = "done"
			case projectv1.ProjectStatusCategory_PROJECT_STATUS_CATEGORY_ACTIVE, projectv1.ProjectStatusCategory_PROJECT_STATUS_CATEGORY_BLOCKED:
				preview.StatusTone = "doing"
			default:
				preview.StatusTone = "todo"
			}
		}
	}
	if assignee := task.GetAssigneeId(); assignee != "" {
		names, photos := projectDirectory(ctx, active)
		preview.Assignee, preview.AssigneePhoto = projectclient.PersonName(assignee, names), photos[assignee]
	}
	return preview
}
