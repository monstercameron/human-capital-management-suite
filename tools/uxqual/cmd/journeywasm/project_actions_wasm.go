//go:build js && wasm

package main

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"syscall/js"
	"time"

	"github.com/google/uuid"
	commonv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/common/v1"
	projectv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/project/v1"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
	projectclient "github.com/monstercameron/human-capital-management-suite/tools/uxqual/projectclient"
	"google.golang.org/protobuf/proto"
)

var projectCreateActionsReady bool
var projectCreateSubmit js.Func
var projectCreateListenerInstalled bool
var projectMoveChange js.Func
var projectMoveListenerInstalled bool
var projectBoardSettingsSubmit js.Func
var projectBoardSettingsListenerInstalled bool
var projectActionStateMu sync.Mutex
var projectPendingMoves = map[string]bool{}
var projectMoveConflicts = map[string]string{}
var projectFailedActions = map[string]string{}

// bindProjectCreateForms installs one document-level submit handler. The UI
// only enables these forms when this handler is installed; all IDs and
// revisions are re-read from the current canonical route and authorized API.
func bindProjectCreateForms(cfg journeyclient.Config, service projectv1.ProjectServiceClient, navigate func(string), revalidate func()) bool {
	projectNavigate = navigate
	if projectCreateListenerInstalled {
		return true
	}
	if service == nil || navigate == nil {
		return false
	}
	document := js.Global().Get("document")
	if !document.Truthy() {
		return false
	}
	projectCreateSubmit = js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 0 {
			return nil
		}
		event := args[0]
		form := event.Get("target")
		if !form.Truthy() || form.Get("matches").Type() != js.TypeFunction {
			return nil
		}
		if !form.Call("matches", `form[data-projectui-action="create-project"], form[data-projectui-action="create-task"]`).Bool() {
			return nil
		}
		action := form.Call("getAttribute", "data-projectui-action").String()
		route, err := projectclient.ParseState(currentPath(), currentQuery())
		if err != nil {
			return nil
		}
		if action == "create-project" && route.Route != projectclient.RouteProjects || action == "create-task" && route.Route != projectclient.RouteProject {
			return nil
		}
		if action == "create-task" {
			if projectID := form.Call("getAttribute", "data-project-id").String(); projectID == "" || projectID != route.ProjectID {
				return nil
			}
		}
		event.Call("preventDefault")
		if form.Call("hasAttribute", "aria-busy").Bool() {
			return nil
		}
		form.Call("setAttribute", "aria-busy", "true")
		form.Call("setAttribute", "data-pending", "true")
		setProjectFormDisabled(form, true)
		go func() {
			defer func() {
				if form.Get("isConnected").Bool() {
					form.Call("removeAttribute", "aria-busy")
					form.Call("removeAttribute", "data-pending")
					setProjectFormDisabled(form, false)
				}
			}()
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if action == "create-project" {
				created, createErr := service.CreateProject(chatRPCContext(ctx, cfg), &projectv1.CreateProjectRequest{
					Name: projectFormValue(form, "name"), ProjectTimezone: projectFormValue(form, "timezone"), IdempotencyKey: uuid.NewString(),
				})
				if createErr != nil || created.GetProject() == nil || created.GetProject().GetProjectId() == "" {
					setProjectFormMessage(form, "The project could not be created. Check your access and try again.", true)
					return
				}
				navigate(projectclient.CanonicalHref(projectclient.State{Route: projectclient.RouteProject, ProjectID: created.GetProject().GetProjectId()}))
				return
			}
			projectResponse, readErr := service.GetProject(chatRPCContext(ctx, cfg), &projectv1.GetProjectRequest{ProjectId: route.ProjectID})
			if readErr != nil || projectResponse.GetProject() == nil || projectResponse.GetProject().GetProjectId() != route.ProjectID {
				setProjectFormMessage(form, "The project could not be loaded. Check your access and try again.", true)
				return
			}
			configurationResponse, configErr := service.GetWorkflowConfiguration(chatRPCContext(ctx, cfg), &projectv1.GetWorkflowConfigurationRequest{ProjectId: route.ProjectID})
			if configErr != nil || configurationResponse.GetConfiguration() == nil || configurationResponse.GetConfiguration().GetProjectId() != route.ProjectID {
				setProjectFormMessage(form, "The workflow could not be loaded. Check your access and try again.", true)
				return
			}
			configuration := configurationResponse.GetConfiguration()
			initialStatus := projectFormValue(form, "initial-status-id")
			validStatus := false
			for _, candidate := range configuration.GetStatuses() {
				if candidate == nil {
					continue
				}
				if initialStatus == "" {
					initialStatus = candidate.GetStatusId()
				}
				if candidate.GetStatusId() == initialStatus {
					validStatus = true
				}
			}
			if !validStatus {
				setProjectFormMessage(form, "Choose a valid initial status before creating the task.", true)
				return
			}
			// A task is created in its type's initial status (the service
			// requires it); a different chosen status is then a normal move.
			createStatus := initialStatus
			for _, taskType := range configuration.GetTaskTypes() {
				if taskType != nil && taskType.GetTaskTypeId() == "task_default" && taskType.GetInitialStatusId() != "" {
					createStatus = taskType.GetInitialStatusId()
				}
			}
			request := &projectv1.CreateTaskRequest{
				ProjectId: route.ProjectID, ExpectedWorkflowRevision: projectResponse.GetProject().GetWorkflowRevision(),
				Title: projectFormValue(form, "title"), Description: projectFormValue(form, "description"),
				InitialStatusId: createStatus, IdempotencyKey: uuid.NewString(),
				AssigneeId: projectFormValue(form, "assignee-id"), DueDate: projectFormValue(form, "due-date"),
			}
			if value, ok := projectv1.TaskPriority_value[projectFormValue(form, "priority")]; ok {
				request.Priority = projectv1.TaskPriority(value)
			}
			created, createErr := service.CreateTask(chatRPCContext(ctx, cfg), request)
			if createErr == nil && created.GetTask() != nil && initialStatus != createStatus {
				_, _ = service.MoveTask(chatRPCContext(ctx, cfg), &projectv1.MoveTaskRequest{
					ProjectId: route.ProjectID, TaskId: created.GetTask().GetTaskId(), TargetStatusId: initialStatus,
					ExpectedTaskRevision: created.GetTask().GetRevision(), ExpectedWorkflowRevision: projectResponse.GetProject().GetWorkflowRevision(), IdempotencyKey: uuid.NewString(),
				})
			}
			if createErr != nil || created.GetTask() == nil || created.GetTask().GetProjectId() != route.ProjectID || created.GetTask().GetTaskId() == "" {
				setProjectFormMessage(form, "The task could not be created. Check your access and try again.", true)
				return
			}
			route.TaskID = created.GetTask().GetTaskId()
			route.Cursor = ""
			navigate(projectclient.CanonicalHref(route))
			if revalidate != nil {
				revalidate()
			}
		}()
		return nil
	})
	document.Call("addEventListener", "submit", projectCreateSubmit)
	projectCreateListenerInstalled = true
	return true
}

// projectMoveTarget is where an in-flight move is heading. The loader draws
// the card there until the write settles (the optimistic move) and drops the
// override on failure, which is the rollback.
type projectMoveTarget struct {
	Status string
	Lane   string
}

// projectMoveRequest is one status and/or swimlane change, from a select,
// the ticket modal or a drop. Revisions are the ones the control rendered.
type projectMoveRequest struct {
	TaskID           string
	TaskRevision     uint64
	WorkflowRevision uint64
	// ToStatus is the target status; empty keeps the current status.
	ToStatus string
	// LaneValue is the target swimlane value; empty keeps the lane.
	LaneValue string
	LaneKind  string
	LaneField string
	// Action names the control whose focus is restored after a rollback.
	Action string
}

var projectPendingTargets = map[string]projectMoveTarget{}

func bindProjectStatusMoves(cfg journeyclient.Config, service projectv1.ProjectServiceClient, revalidate func()) bool {
	if projectMoveListenerInstalled {
		return true
	}
	if service == nil {
		return false
	}
	projectRevalidate = revalidate
	document := js.Global().Get("document")
	if !document.Truthy() {
		return false
	}
	projectMoveChange = js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 0 {
			return nil
		}
		selectNode := args[0].Get("target")
		if !selectNode.Truthy() || selectNode.Get("matches").Type() != js.TypeFunction || !selectNode.Call("matches", `select[data-projectui-action="move-status"], select[data-projectui-action="move-lane"]`).Bool() {
			return nil
		}
		request, ok := projectMoveRequestFrom(selectNode, selectNode.Get("value").String())
		if ok && startProjectMove(cfg, service, revalidate, request) {
			selectNode.Set("disabled", true)
		}
		return nil
	})
	document.Call("addEventListener", "change", projectMoveChange)
	// The board card menu offers the same moves as menuitemradio rows.
	document.Call("addEventListener", "click", js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 0 {
			return nil
		}
		row := projectClosest(args[0].Get("target"), `button[data-projectui-action="move-status"], button[data-projectui-action="move-lane"]`)
		if !row.Truthy() || row.Get("disabled").Bool() || projectAttr(row, "aria-disabled") == "true" {
			return nil
		}
		menu := projectClosest(row, "details.projectui-card-menu")
		if projectAttr(row, "aria-checked") == "true" {
			if menu.Truthy() {
				menu.Call("removeAttribute", "open")
			}
			return nil
		}
		request, ok := projectMoveRequestFrom(row, projectAttr(row, "data-value"))
		if ok && startProjectMove(cfg, service, revalidate, request) && menu.Truthy() {
			menu.Call("removeAttribute", "open")
		}
		return nil
	}))
	projectMoveListenerInstalled = true
	bindProjectBoardInteractions(cfg, service, revalidate)
	bindProjectTaskEdits(cfg, service, revalidate)
	return true
}

// projectMoveRequestFrom reads the task identity and revisions a move
// control carries (a list select or a card-menu row) for the target value.
func projectMoveRequestFrom(node js.Value, value string) (projectMoveRequest, bool) {
	taskRevision, taskRevisionErr := strconv.ParseUint(projectAttr(node, "data-task-revision"), 10, 64)
	workflowRevision, workflowRevisionErr := strconv.ParseUint(projectAttr(node, "data-workflow-revision"), 10, 64)
	if taskRevisionErr != nil || workflowRevisionErr != nil || value == "" {
		return projectMoveRequest{}, false
	}
	request := projectMoveRequest{TaskID: projectAttr(node, "data-task-id"), TaskRevision: taskRevision, WorkflowRevision: workflowRevision, Action: "status"}
	if projectAttr(node, "data-projectui-action") == "move-lane" {
		request.Action, request.LaneValue = "lane", value
		request.LaneKind, request.LaneField = projectLaneConfigFor()
	} else {
		request.ToStatus = value
	}
	return request, true
}

// startProjectMove is the one write path for status and swimlane moves. It
// marks the task pending with its target (the board redraws it there at
// once), re-reads the task and workflow, refuses a stale or disallowed
// move, writes, and on any failure drops the target so the card returns to
// where the service says it is, with the conflict message on it.
func startProjectMove(cfg journeyclient.Config, service projectv1.ProjectServiceClient, revalidate func(), request projectMoveRequest) bool {
	route, err := projectclient.ParseState(currentPath(), currentQuery())
	if err != nil || route.Route != projectclient.RouteProject {
		return false
	}
	projectID, taskID := route.ProjectID, request.TaskID
	if taskID == "" || request.TaskRevision == 0 || request.WorkflowRevision == 0 || request.ToStatus == "" && request.LaneValue == "" {
		return false
	}
	key := projectActionKey(cfg.Tenant, cfg.Subject, projectID, taskID)
	projectActionStateMu.Lock()
	if projectPendingMoves[key] {
		projectActionStateMu.Unlock()
		return false
	}
	projectPendingMoves[key] = true
	projectPendingTargets[key] = projectMoveTarget{Status: request.ToStatus, Lane: request.LaneValue}
	delete(projectMoveConflicts, key)
	delete(projectFailedActions, key)
	projectActionStateMu.Unlock()
	startedHref := currentProductHref()
	if revalidate != nil {
		revalidate()
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		callCtx := chatRPCContext(ctx, cfg)
		fail := func(message string) {
			projectActionStateMu.Lock()
			delete(projectPendingMoves, key)
			delete(projectPendingTargets, key)
			projectMoveConflicts[key] = message
			projectFailedActions[key] = request.Action
			projectActionStateMu.Unlock()
			if currentProductHref() == startedHref && revalidate != nil {
				revalidate()
				focusProjectMoveConflict(taskID, 0)
				settleProjectCard(taskID, "is-returning", 0)
			}
		}
		taskResponse, readErr := service.GetTask(callCtx, &projectv1.GetTaskRequest{ProjectId: projectID, TaskId: taskID})
		if readErr != nil || taskResponse.GetTask() == nil || taskResponse.GetTask().GetProjectId() != projectID || taskResponse.GetTask().GetTaskId() != taskID {
			fail("The board changed. Review the current task status and try again.")
			return
		}
		task := taskResponse.GetTask()
		configurationResponse, configErr := service.GetWorkflowConfiguration(callCtx, &projectv1.GetWorkflowConfigurationRequest{ProjectId: projectID})
		if configErr != nil || configurationResponse.GetConfiguration() == nil || configurationResponse.GetConfiguration().GetProjectId() != projectID {
			fail("The workflow changed. Review the current task status and try again.")
			return
		}
		configuration := configurationResponse.GetConfiguration()
		if task.GetRevision() != request.TaskRevision || configuration.GetRevision() != request.WorkflowRevision {
			fail("The board changed. Review the current task status and try again.")
			return
		}
		revision := task.GetRevision()
		laneEdit, lanePatch, laneErr := projectLaneChange(request, task)
		if laneErr != "" {
			fail(laneErr)
			return
		}
		statusChange := request.ToStatus != "" && request.ToStatus != task.GetStatusId()
		if statusChange {
			allowed := false
			for _, status := range configuration.GetStatuses() {
				if status != nil && status.GetStatusId() == task.GetStatusId() {
					for _, next := range status.GetAllowedNextStatusIds() {
						if next == request.ToStatus {
							allowed = true
							break
						}
					}
				}
			}
			if !allowed {
				fail("That status is no longer an available transition.")
				return
			}
		}
		if statusChange || laneEdit != nil {
			target := request.ToStatus
			if !statusChange {
				target = task.GetStatusId()
			}
			moveResponse, moveErr := service.MoveTask(callCtx, &projectv1.MoveTaskRequest{
				ProjectId: projectID, TaskId: taskID, TargetStatusId: target, LaneFieldEdit: laneEdit,
				ExpectedTaskRevision: revision, ExpectedWorkflowRevision: request.WorkflowRevision, IdempotencyKey: uuid.NewString(),
			})
			moved := moveResponse.GetTask()
			if moveErr != nil || moved == nil || moved.GetProjectId() != projectID || moved.GetTaskId() != taskID || moved.GetStatusId() != target {
				fail("The task could not be moved. Review the current board and try again.")
				return
			}
			revision = moved.GetRevision()
		}
		if lanePatch != nil {
			lanePatch.ProjectId, lanePatch.TaskId, lanePatch.IdempotencyKey = projectID, taskID, uuid.NewString()
			lanePatch.ExpectedTaskRevision, lanePatch.ExpectedWorkflowRevision = revision, request.WorkflowRevision
			if _, patchErr := service.PatchTask(callCtx, lanePatch); patchErr != nil {
				fail("The task could not be moved to that lane. Review the current board and try again.")
				return
			}
		}
		projectActionStateMu.Lock()
		delete(projectPendingMoves, key)
		delete(projectPendingTargets, key)
		delete(projectMoveConflicts, key)
		delete(projectFailedActions, key)
		projectActionStateMu.Unlock()
		projectProgressForget(cfg, projectID)
		if currentProductHref() == startedHref && revalidate != nil {
			revalidate()
			settleProjectCard(taskID, "is-settling", 0)
		}
	}()
	return true
}

// projectLaneChange turns a swimlane target into the write that changes it:
// an assignee or priority lane is that field (PatchTask), an enum-field lane
// is an atomic field edit on the move.
func projectLaneChange(request projectMoveRequest, task *projectv1.ProjectTask) (*projectv1.TypedFieldValue, *projectv1.PatchTaskRequest, string) {
	if request.LaneValue == "" {
		return nil, nil, ""
	}
	switch request.LaneKind {
	case "assignee":
		assignee := request.LaneValue
		if assignee == "unassigned" {
			assignee = ""
		}
		if assignee == task.GetAssigneeId() {
			return nil, nil, ""
		}
		return nil, &projectv1.PatchTaskRequest{AssigneeId: &assignee}, ""
	case "priority":
		value, ok := projectv1.TaskPriority_value[request.LaneValue]
		if !ok || value == int32(projectv1.TaskPriority_TASK_PRIORITY_UNSPECIFIED) {
			return nil, nil, "That lane is not a priority this task can take."
		}
		priority := projectv1.TaskPriority(value)
		if priority == task.GetPriority() {
			return nil, nil, ""
		}
		return nil, &projectv1.PatchTaskRequest{Priority: &priority}, ""
	case "field":
		if request.LaneField == "" || request.LaneValue == "unset" {
			return nil, nil, "That lane cannot be set from the board."
		}
		return &projectv1.TypedFieldValue{FieldId: request.LaneField, Value: &projectv1.TypedFieldValue_EnumValue{EnumValue: request.LaneValue}}, nil, ""
	default:
		return nil, nil, "This board's lanes cannot be changed by moving a task."
	}
}

func bindProjectBoardSettings(cfg journeyclient.Config, service projectv1.ProjectServiceClient, revalidate func()) bool {
	if projectBoardSettingsListenerInstalled {
		return true
	}
	if service == nil {
		return false
	}
	document := js.Global().Get("document")
	if !document.Truthy() {
		return false
	}
	projectBoardSettingsSubmit = js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 0 {
			return nil
		}
		event := args[0]
		form := event.Get("target")
		if !form.Truthy() || form.Get("matches").Type() != js.TypeFunction || !form.Call("matches", `form[data-projectui-action="save-board-view"]`).Bool() {
			return nil
		}
		route, err := projectclient.ParseState(currentPath(), currentQuery())
		if err != nil || route.Route != projectclient.RouteProject {
			return nil
		}
		projectID := form.Call("getAttribute", "data-project-id").String()
		viewID := form.Call("getAttribute", "data-view-id").String()
		expectedRevision, parseErr := strconv.ParseUint(form.Call("getAttribute", "data-view-revision").String(), 10, 64)
		if projectID == "" || projectID != route.ProjectID || viewID == "" || viewID != route.BoardViewID || parseErr != nil || expectedRevision == 0 {
			return nil
		}
		event.Call("preventDefault")
		if form.Call("hasAttribute", "aria-busy").Bool() {
			return nil
		}
		unmapped := projectAttr(form, "data-msg-unmapped")
		if unmapped == "" {
			unmapped = "Every status needs exactly one column."
		}
		failed := projectAttr(form, "data-msg-failed")
		if failed == "" {
			failed = "Board settings couldn't be saved. Reload the board and try again."
		}
		if !projectStatusMappingComplete(form) {
			setProjectFormMessage(form, unmapped, true)
			return nil
		}
		form.Call("setAttribute", "aria-busy", "true")
		form.Call("setAttribute", "data-pending", "true")
		setProjectFormDisabled(form, true)
		go func() {
			defer func() {
				if form.Get("isConnected").Bool() {
					form.Call("removeAttribute", "aria-busy")
					form.Call("removeAttribute", "data-pending")
					setProjectFormDisabled(form, false)
				}
			}()
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			callCtx := chatRPCContext(ctx, cfg)
			viewsResponse, viewsErr := service.ListBoardViews(callCtx, &projectv1.ListBoardViewsRequest{ProjectId: projectID, Page: &commonv1.PageRequest{PageSize: 100}})
			if viewsErr != nil {
				setProjectFormMessage(form, failed, true)
				return
			}
			var authorizedView *projectv1.BoardView
			for _, candidate := range viewsResponse.GetViews() {
				if candidate != nil && candidate.GetProjectId() == projectID && candidate.GetViewId() == viewID && candidate.GetRevision() == expectedRevision {
					authorizedView = candidate
					break
				}
			}
			if authorizedView == nil {
				setProjectFormMessage(form, failed, true)
				return
			}
			configurationResponse, configErr := service.GetWorkflowConfiguration(callCtx, &projectv1.GetWorkflowConfigurationRequest{ProjectId: projectID})
			if configErr != nil || configurationResponse.GetConfiguration() == nil || configurationResponse.GetConfiguration().GetProjectId() != projectID {
				setProjectFormMessage(form, failed, true)
				return
			}
			view := proto.Clone(authorizedView).(*projectv1.BoardView)
			view.Name = projectFormValue(form, "name")
			grouping, ok := projectGroupingFromName(projectFormValue(form, "swimlane-grouping"))
			if !ok {
				setProjectFormMessage(form, failed, true)
				return
			}
			view.SwimlaneGrouping = grouping
			view.SwimlaneFieldId = projectFormValue(form, "swimlane-field-id")
			configuration := configurationResponse.GetConfiguration()
			if grouping == projectv1.BoardSwimlaneGrouping_BOARD_SWIMLANE_GROUPING_ENUM_FIELD {
				fieldValid := false
				for _, field := range configuration.GetFields() {
					if field != nil && field.GetFieldId() == view.GetSwimlaneFieldId() && field.GetType() == projectv1.ProjectFieldType_PROJECT_FIELD_TYPE_ENUM {
						fieldValid = true
						break
					}
				}
				if !fieldValid {
					setProjectFormMessage(form, failed, true)
					return
				}
			} else {
				view.SwimlaneFieldId = ""
			}
			columns := make([]*projectv1.BoardColumn, 0, len(authorizedView.GetColumns()))
			seenStatuses := map[string]bool{}
			validStatuses := map[string]bool{}
			for _, status := range configuration.GetStatuses() {
				if status != nil && status.GetStatusId() != "" {
					validStatuses[status.GetStatusId()] = true
				}
			}
			for index, original := range authorizedView.GetColumns() {
				if original == nil {
					continue
				}
				name := projectFormValue(form, "column-name-"+strconv.Itoa(index))
				rawStatuses := projectCheckedValues(form, "column-status-"+strconv.Itoa(index))
				statusIDs := []string{}
				for _, raw := range strings.Split(rawStatuses, ",") {
					statusID := strings.TrimSpace(raw)
					if statusID == "" {
						continue
					}
					if !validStatuses[statusID] || seenStatuses[statusID] {
						setProjectFormMessage(form, unmapped, true)
						return
					}
					seenStatuses[statusID] = true
					statusIDs = append(statusIDs, statusID)
				}
				columns = append(columns, &projectv1.BoardColumn{ColumnId: original.GetColumnId(), Name: name, StatusIds: statusIDs, Position: int32(index)})
			}
			if len(columns) == 0 || len(seenStatuses) != len(validStatuses) {
				setProjectFormMessage(form, unmapped, true)
				return
			}
			view.Columns = columns
			saved, saveErr := service.SaveBoardView(callCtx, &projectv1.SaveBoardViewRequest{
				IdempotencyKey: uuid.NewString(), ProjectId: projectID, ViewId: viewID,
				ExpectedViewRevision: expectedRevision, View: view,
			})
			if saveErr != nil || saved.GetView() == nil || saved.GetView().GetProjectId() != projectID || saved.GetView().GetViewId() != viewID {
				setProjectFormMessage(form, failed, true)
				return
			}
			if currentProductHref() == projectclient.CanonicalHref(route) && revalidate != nil {
				revalidate()
			}
		}()
		return nil
	})
	document.Call("addEventListener", "submit", projectBoardSettingsSubmit)
	// A status belongs to one column: ticking it in one column clears it
	// from the others, and any change clears a stale mapping error.
	document.Call("addEventListener", "change", js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 0 {
			return nil
		}
		box := args[0].Get("target")
		form := projectClosest(box, `form[data-projectui-action="save-board-view"]`)
		if !form.Truthy() || projectAttr(box, "type") != "checkbox" || !strings.HasPrefix(projectAttr(box, "name"), "column-status-") {
			return nil
		}
		if box.Get("checked").Bool() {
			boxes := form.Call("querySelectorAll", `input[type="checkbox"][name^="column-status-"]`)
			for index := 0; index < boxes.Get("length").Int(); index++ {
				other := boxes.Index(index)
				if !other.Equal(box) && other.Get("value").String() == box.Get("value").String() {
					other.Set("checked", false)
				}
			}
		}
		if result := form.Call("querySelector", `[data-projectui-result][role="alert"]`); result.Truthy() && projectStatusMappingComplete(form) {
			result.Call("remove")
		}
		return nil
	}))
	projectBoardSettingsListenerInstalled = true
	return true
}

func projectGroupingFromName(value string) (projectv1.BoardSwimlaneGrouping, bool) {
	switch value {
	case "BOARD_SWIMLANE_GROUPING_NONE":
		return projectv1.BoardSwimlaneGrouping_BOARD_SWIMLANE_GROUPING_NONE, true
	case "BOARD_SWIMLANE_GROUPING_ASSIGNEE":
		return projectv1.BoardSwimlaneGrouping_BOARD_SWIMLANE_GROUPING_ASSIGNEE, true
	case "BOARD_SWIMLANE_GROUPING_PRIORITY":
		return projectv1.BoardSwimlaneGrouping_BOARD_SWIMLANE_GROUPING_PRIORITY, true
	case "BOARD_SWIMLANE_GROUPING_ENUM_FIELD":
		return projectv1.BoardSwimlaneGrouping_BOARD_SWIMLANE_GROUPING_ENUM_FIELD, true
	default:
		return projectv1.BoardSwimlaneGrouping_BOARD_SWIMLANE_GROUPING_UNSPECIFIED, false
	}
}

func projectActionKey(tenantID, subjectID, projectID, taskID string) string {
	return tenantID + "\x00" + subjectID + "\x00" + projectID + "\x00" + taskID
}

func projectActionProjectionState(tenantID, subjectID, projectID string) (map[string]bool, map[string]string, map[string]string) {
	projectActionStateMu.Lock()
	defer projectActionStateMu.Unlock()
	pending := map[string]bool{}
	conflicts := map[string]string{}
	failed := map[string]string{}
	prefix := tenantID + "\x00" + subjectID + "\x00" + projectID + "\x00"
	for key, value := range projectPendingMoves {
		if strings.HasPrefix(key, prefix) {
			pending[strings.TrimPrefix(key, prefix)] = value
		}
	}
	for key, value := range projectMoveConflicts {
		if strings.HasPrefix(key, prefix) {
			conflicts[strings.TrimPrefix(key, prefix)] = value
		}
	}
	for key, value := range projectFailedActions {
		if strings.HasPrefix(key, prefix) {
			failed[strings.TrimPrefix(key, prefix)] = value
		}
	}
	return pending, conflicts, failed
}

func focusProjectMoveConflict(taskID string, attempt int) {
	if attempt >= 12 {
		return
	}
	var callback js.Func
	callback = js.FuncOf(func(js.Value, []js.Value) any {
		defer callback.Release()
		conflicts := js.Global().Get("document").Call("querySelectorAll", ".projectui-conflict")
		for index := 0; index < conflicts.Get("length").Int(); index++ {
			conflict := conflicts.Index(index)
			card := conflict.Call("closest", `[data-task-id="`+taskID+`"]`)
			if card.Truthy() {
				conflict.Call("focus", map[string]any{"preventScroll": true})
				return nil
			}
		}
		focusProjectMoveConflict(taskID, attempt+1)
		return nil
	})
	js.Global().Call("requestAnimationFrame", callback)
}

// projectStatusMappingComplete reports whether every status offered in the
// settings form is ticked in exactly one column.
func projectStatusMappingComplete(form js.Value) bool {
	boxes := form.Call("querySelectorAll", `input[type="checkbox"][name^="column-status-"]`)
	counts := map[string]int{}
	for index := 0; index < boxes.Get("length").Int(); index++ {
		box := boxes.Index(index)
		value := box.Get("value").String()
		if _, seen := counts[value]; !seen {
			counts[value] = 0
		}
		if box.Get("checked").Bool() {
			counts[value]++
		}
	}
	for _, count := range counts {
		if count != 1 {
			return false
		}
	}
	return true
}

func projectFormValue(form js.Value, name string) string {
	field := form.Call("querySelector", `[name="`+name+`"]`)
	if !field.Truthy() {
		return ""
	}
	return strings.TrimSpace(field.Get("value").String())
}

func setProjectFormDisabled(form js.Value, disabled bool) {
	fields := form.Call("querySelectorAll", "input, textarea, select, button")
	for index := 0; index < fields.Get("length").Int(); index++ {
		fields.Index(index).Set("disabled", disabled)
	}
}

func setProjectFormMessage(form js.Value, message string, alert bool) {
	selector := `[data-projectui-result]`
	result := form.Call("querySelector", selector)
	if !result.Truthy() {
		result = js.Global().Get("document").Call("createElement", "p")
		result.Call("setAttribute", "data-projectui-result", "true")
		result.Call("setAttribute", "aria-live", "polite")
		// Keep the message above a sticky action bar so it stays in view.
		if actions := form.Call("querySelector", ".project-page-form-actions"); actions.Truthy() && actions.Get("parentNode").Equal(form) {
			form.Call("insertBefore", result, actions)
		} else {
			form.Call("appendChild", result)
		}
	}
	role := "status"
	if alert {
		role = "alert"
	}
	result.Call("setAttribute", "role", role)
	result.Set("textContent", message)
}

// projectCheckedValues joins the checked checkbox values of one name with
// commas, the shape the column mapping parser reads. A text input of that
// name (an older page) is read as before.
func projectCheckedValues(form js.Value, name string) string {
	boxes := form.Call("querySelectorAll", `input[type="checkbox"][name="`+name+`"]`)
	if boxes.Get("length").Int() == 0 {
		return projectFormValue(form, name)
	}
	values := []string{}
	for index := 0; index < boxes.Get("length").Int(); index++ {
		if box := boxes.Index(index); box.Get("checked").Bool() {
			values = append(values, box.Get("value").String())
		}
	}
	return strings.Join(values, ",")
}
