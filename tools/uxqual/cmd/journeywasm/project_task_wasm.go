//go:build js && wasm

package main

import (
	"context"
	"strconv"
	"strings"
	"syscall/js"
	"time"

	"github.com/google/uuid"
	projectv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/project/v1"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
	projectclient "github.com/monstercameron/human-capital-management-suite/tools/uxqual/projectclient"
)

// Task field edits from the ticket modal and the task page. Each control
// carries the task identity and the revisions it rendered; a write expects
// exactly those revisions, so a stale page gets a conflict instead of
// overwriting someone else's change. Per-field state ("saving", "saved",
// "error") is kept here and projected by the loader, so feedback survives
// the re-render that follows every write.

var projectTaskEditsInstalled bool

// projectFieldStates maps projectActionKey(task) -> field -> state.
var projectFieldStates = map[string]map[string]string{}
var projectFieldErrors = map[string]map[string]string{}

func setProjectFieldState(key, field, state, message string) {
	projectActionStateMu.Lock()
	defer projectActionStateMu.Unlock()
	if projectFieldStates[key] == nil {
		projectFieldStates[key] = map[string]string{}
		projectFieldErrors[key] = map[string]string{}
	}
	projectFieldStates[key][field] = state
	projectFieldErrors[key][field] = message
}

// projectFieldStateFor copies one task's field states for the loader.
func projectFieldStateFor(tenantID, subjectID, projectID, taskID string) (map[string]string, map[string]string) {
	key := projectActionKey(tenantID, subjectID, projectID, taskID)
	projectActionStateMu.Lock()
	defer projectActionStateMu.Unlock()
	states, errors := map[string]string{}, map[string]string{}
	for field, state := range projectFieldStates[key] {
		states[field] = state
	}
	for field, message := range projectFieldErrors[key] {
		errors[field] = message
	}
	return states, errors
}

type projectTaskControl struct {
	projectID, taskID          string
	taskRevision, flowRevision uint64
}

func readProjectTaskControl(node js.Value) (projectTaskControl, bool) {
	control := projectTaskControl{projectID: projectAttr(node, "data-project-id"), taskID: projectAttr(node, "data-task-id")}
	var err1, err2 error
	control.taskRevision, err1 = strconv.ParseUint(projectAttr(node, "data-task-revision"), 10, 64)
	control.flowRevision, err2 = strconv.ParseUint(projectAttr(node, "data-workflow-revision"), 10, 64)
	route, err := projectclient.ParseState(currentPath(), currentQuery())
	if err != nil || route.Route != projectclient.RouteProject || control.projectID != route.ProjectID || control.taskID == "" || err1 != nil || err2 != nil {
		return control, false
	}
	return control, true
}

func bindProjectTaskEdits(cfg journeyclient.Config, service projectv1.ProjectServiceClient, revalidate func()) {
	if projectTaskEditsInstalled {
		return
	}
	document := js.Global().Get("document")
	if !document.Truthy() {
		return
	}
	projectTaskEditsInstalled = true
	document.Call("addEventListener", "change", js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 0 {
			return nil
		}
		node := args[0].Get("target")
		action := projectAttr(node, "data-projectui-action")
		if action != "set-assignee" && action != "set-priority" && action != "set-due" && action != "set-start" && action != "set-points" {
			return nil
		}
		control, ok := readProjectTaskControl(node)
		if !ok {
			return nil
		}
		value := node.Get("value").String()
		patch := &projectv1.PatchTaskRequest{}
		field := ""
		switch action {
		case "set-assignee":
			field, patch.AssigneeId = "assignee", &value
		case "set-priority":
			priority, found := projectv1.TaskPriority_value[value]
			if !found || priority == 0 {
				return nil
			}
			typed := projectv1.TaskPriority(priority)
			field, patch.Priority = "priority", &typed
		case "set-due":
			field, patch.DueDate = "due", &value
		case "set-start":
			field, patch.StartDate = "start", &value
		case "set-points":
			points, err := strconv.Atoi(strings.TrimSpace(value))
			if strings.TrimSpace(value) == "" {
				points, err = 0, nil
			}
			if err != nil || points < 0 || points > 1000 {
				setProjectFieldState(projectActionKey(cfg.Tenant, cfg.Subject, control.projectID, control.taskID), "points", "error", "")
				if revalidate != nil {
					revalidate()
				}
				return nil
			}
			typed := uint32(points)
			field, patch.StoryPoints = "points", &typed
		}
		runProjectPatch(cfg, service, revalidate, control, field, patch)
		return nil
	}))
	document.Call("addEventListener", "submit", js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 0 {
			return nil
		}
		event := args[0]
		form := event.Get("target")
		action := projectAttr(form, "data-projectui-action")
		switch action {
		case "edit-title", "edit-description", "add-comment", "edit-comment", "delete-comment":
		default:
			return nil
		}
		control, ok := readProjectTaskControl(form)
		if !ok {
			return nil
		}
		event.Call("preventDefault")
		switch action {
		case "edit-title":
			title := strings.Join(strings.Fields(projectFormValue(form, "title")), " ")
			if title == "" {
				return nil
			}
			runProjectPatch(cfg, service, revalidate, control, "title", &projectv1.PatchTaskRequest{Title: &title})
		case "edit-description":
			field := form.Call("querySelector", `[name="description"]`)
			description := ""
			if field.Truthy() {
				description = strings.TrimRight(field.Get("value").String(), " \n\t")
			}
			runProjectPatch(cfg, service, revalidate, control, "description", &projectv1.PatchTaskRequest{Description: &description})
		default:
			runProjectComment(cfg, service, revalidate, control, form, action)
		}
		return nil
	}))
	// The title editor is a textarea so long titles wrap; Enter still saves
	// it, as in a single-line field.
	// Labels: Enter or a comma in the label field adds what was typed; the
	// x on a chip removes it. Each change writes the whole list.
	document.Call("addEventListener", "keydown", js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 0 {
			return nil
		}
		event := args[0]
		key := event.Get("key").String()
		input := event.Get("target")
		if (key != "Enter" && key != ",") || !projectClosest(input, ".projectui-label-editor").Truthy() || projectAttr(input, "name") != "label" {
			return nil
		}
		event.Call("preventDefault")
		addProjectLabel(cfg, service, revalidate, input)
		return nil
	}))
	document.Call("addEventListener", "change", js.FuncOf(func(_ js.Value, args []js.Value) any {
		// Picking a suggestion from the list adds it at once.
		if len(args) == 0 {
			return nil
		}
		input := args[0].Get("target")
		if projectAttr(input, "name") == "label" && projectClosest(input, ".projectui-label-editor").Truthy() {
			addProjectLabel(cfg, service, revalidate, input)
		}
		return nil
	}))
	document.Call("addEventListener", "click", js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 0 {
			return nil
		}
		button := projectClosest(args[0].Get("target"), `[data-projectui-action="remove-label"]`)
		editor := projectClosest(button, ".projectui-label-editor")
		if !button.Truthy() || !editor.Truthy() {
			return nil
		}
		control, ok := readProjectTaskControl(editor)
		if !ok {
			return nil
		}
		remove := strings.ToLower(projectAttr(button, "data-label"))
		labels := []string{}
		for _, label := range projectLabelList(editor) {
			if strings.ToLower(label) != remove {
				labels = append(labels, label)
			}
		}
		runProjectPatch(cfg, service, revalidate, control, "labels", &projectv1.PatchTaskRequest{Labels: &projectv1.TaskLabels{Values: labels}})
		return nil
	}))
	// Escape in a changed title puts the saved text back instead of closing
	// the modal; in an unchanged one it closes the modal as usual.
	document.Call("addEventListener", "keydown", js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 0 || args[0].Get("key").String() != "Escape" {
			return nil
		}
		target := args[0].Get("target")
		if projectAttr(target, "name") != "title" || !projectClosest(target, `form[data-projectui-action="edit-title"]`).Truthy() {
			return nil
		}
		saved := projectAttr(target, "data-saved")
		if target.Get("value").String() == saved {
			return nil
		}
		args[0].Call("preventDefault")
		args[0].Call("stopPropagation")
		target.Set("value", saved)
		target.Call("blur")
		return nil
	}), true)
	document.Call("addEventListener", "keydown", js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 0 || args[0].Get("key").String() != "Enter" || args[0].Get("shiftKey").Bool() {
			return nil
		}
		target := args[0].Get("target")
		form := projectClosest(target, `form[data-projectui-action="edit-title"]`)
		if !form.Truthy() || projectAttr(target, "name") != "title" {
			return nil
		}
		args[0].Call("preventDefault")
		form.Call("requestSubmit")
		return nil
	}))
	// Cancel on an inline editor restores the saved text; the reset button
	// does that natively, and dropping focus hides the actions again.
	document.Call("addEventListener", "reset", js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 0 {
			return nil
		}
		form := args[0].Get("target")
		if projectAttr(form, "data-projectui-action") == "edit-title" || projectAttr(form, "data-projectui-action") == "edit-description" {
			if active := js.Global().Get("document").Get("activeElement"); active.Truthy() && form.Call("contains", active).Bool() {
				active.Call("blur")
			}
		}
		return nil
	}))
}

func runProjectPatch(cfg journeyclient.Config, service projectv1.ProjectServiceClient, revalidate func(), control projectTaskControl, field string, patch *projectv1.PatchTaskRequest) {
	key := projectActionKey(cfg.Tenant, cfg.Subject, control.projectID, control.taskID)
	setProjectFieldState(key, field, "saving", "")
	if revalidate != nil {
		revalidate()
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		patch.ProjectId, patch.TaskId, patch.IdempotencyKey = control.projectID, control.taskID, uuid.NewString()
		patch.ExpectedTaskRevision, patch.ExpectedWorkflowRevision = control.taskRevision, control.flowRevision
		response, err := service.PatchTask(chatRPCContext(ctx, cfg), patch)
		if err != nil || response.GetTask() == nil || response.GetTask().GetTaskId() != control.taskID {
			// A refused write whose value is already what the task holds (a
			// repeated change event, or the same edit made elsewhere) is not
			// a conflict worth reporting.
			if current, readErr := service.GetTask(chatRPCContext(ctx, cfg), &projectv1.GetTaskRequest{ProjectId: control.projectID, TaskId: control.taskID}); readErr == nil && projectPatchApplied(patch, current.GetTask()) {
				setProjectFieldState(key, field, "saved", "")
			} else {
				setProjectFieldState(key, field, "error", projectWriteError(err))
			}
		} else {
			setProjectFieldState(key, field, "saved", "")
		}
		projectProgressForget(cfg, control.projectID)
		if revalidate != nil {
			revalidate()
		}
	}()
}

func runProjectComment(cfg journeyclient.Config, service projectv1.ProjectServiceClient, revalidate func(), control projectTaskControl, form js.Value, action string) {
	key := projectActionKey(cfg.Tenant, cfg.Subject, control.projectID, control.taskID)
	body := projectFormValue(form, "body")
	commentID := projectAttr(form, "data-comment-id")
	revision, _ := strconv.ParseUint(projectAttr(form, "data-comment-revision"), 10, 64)
	if action != "delete-comment" && body == "" {
		return
	}
	if action != "add-comment" && (commentID == "" || revision == 0) {
		return
	}
	setProjectFieldState(key, "comment", "saving", "")
	setProjectFormDisabled(form, true)
	if revalidate != nil {
		revalidate()
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		callCtx := chatRPCContext(ctx, cfg)
		var err error
		switch action {
		case "add-comment":
			_, err = service.AddTaskComment(callCtx, &projectv1.AddTaskCommentRequest{ProjectId: control.projectID, TaskId: control.taskID, IdempotencyKey: uuid.NewString(), BodyText: body})
		case "edit-comment":
			_, err = service.EditTaskComment(callCtx, &projectv1.EditTaskCommentRequest{ProjectId: control.projectID, TaskId: control.taskID, CommentId: commentID, ExpectedRevision: revision, IdempotencyKey: uuid.NewString(), BodyText: body})
		case "delete-comment":
			_, err = service.DeleteTaskComment(callCtx, &projectv1.DeleteTaskCommentRequest{ProjectId: control.projectID, TaskId: control.taskID, CommentId: commentID, ExpectedRevision: revision, IdempotencyKey: uuid.NewString()})
		}
		if form.Get("isConnected").Bool() {
			setProjectFormDisabled(form, false)
		}
		if err != nil {
			setProjectFieldState(key, "comment", "error", projectWriteError(err))
		} else {
			setProjectFieldState(key, "comment", "", "")
			if action == "add-comment" && form.Get("isConnected").Bool() {
				form.Call("reset")
			}
		}
		if revalidate != nil {
			revalidate()
		}
	}()
}

// projectWriteError is deliberately empty: the page shows its own
// localized "not saved" sentence for any failed write, so nothing about the
// service error leaks into the page and every language reads the same.
func projectWriteError(error) string { return "" }

// projectPatchApplied reports whether every field the patch sets already
// has that value on task.
func projectPatchApplied(patch *projectv1.PatchTaskRequest, task *projectv1.ProjectTask) bool {
	if task == nil {
		return false
	}
	if patch.Title != nil && *patch.Title != task.GetTitle() {
		return false
	}
	if patch.Description != nil && *patch.Description != task.GetDescription() {
		return false
	}
	if patch.AssigneeId != nil && *patch.AssigneeId != task.GetAssigneeId() {
		return false
	}
	if patch.DueDate != nil && *patch.DueDate != task.GetDueDate() {
		return false
	}
	if patch.Priority != nil && *patch.Priority != task.GetPriority() {
		return false
	}
	return true
}

// projectLabelList reads the labels an editor rendered.
func projectLabelList(editor js.Value) []string {
	labels := []string{}
	for _, label := range strings.Split(projectAttr(editor, "data-labels"), "\n") {
		if label = strings.TrimSpace(label); label != "" {
			labels = append(labels, label)
		}
	}
	return labels
}

// addProjectLabel adds the typed label (or several, comma-separated) to the
// task. Labels are trimmed, at most 40 characters, at most 20 per task and
// unique regardless of case, as the service requires.
func addProjectLabel(cfg journeyclient.Config, service projectv1.ProjectServiceClient, revalidate func(), input js.Value) {
	editor := projectClosest(input, ".projectui-label-editor")
	control, ok := readProjectTaskControl(editor)
	if !ok {
		return
	}
	typed := strings.TrimSpace(input.Get("value").String())
	if typed == "" {
		return
	}
	labels := projectLabelList(editor)
	seen := map[string]bool{}
	for _, label := range labels {
		seen[strings.ToLower(label)] = true
	}
	added := false
	for _, part := range strings.Split(typed, ",") {
		label := strings.Join(strings.Fields(part), " ")
		if label == "" || len([]rune(label)) > 40 || seen[strings.ToLower(label)] || len(labels) >= 20 {
			continue
		}
		seen[strings.ToLower(label)] = true
		labels = append(labels, label)
		added = true
	}
	input.Set("value", "")
	if added {
		runProjectPatch(cfg, service, revalidate, control, "labels", &projectv1.PatchTaskRequest{Labels: &projectv1.TaskLabels{Values: labels}})
	}
}
