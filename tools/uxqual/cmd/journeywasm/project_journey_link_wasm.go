//go:build js && wasm

package main

import (
	"context"
	"net/url"
	"sort"
	"strings"
	"syscall/js"
	"time"

	"github.com/google/uuid"
	commonv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/common/v1"
	projectv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/project/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/projectui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
	projectclient "github.com/monstercameron/human-capital-management-suite/tools/uxqual/projectclient"
)

// "Link to ticket…" on a journey page: pick a project, then one of its
// tickets, and the journey is linked to that ticket (AddTaskLink with the
// ticket's current revision). The journey page belongs to another surface,
// so this dialog is built here, outside its component tree, and removed when
// it closes; its words arrive as data attributes on the button.

type projectLinkerTask struct {
	projectID, taskID, key, title string
	revision                      uint64
}

var projectLinkerTasks []projectLinkerTask

func projectElement(tag, class, text string) js.Value {
	node := js.Global().Get("document").Call("createElement", tag)
	if class != "" {
		node.Set("className", class)
	}
	if text != "" {
		node.Set("textContent", text)
	}
	return node
}

func openProjectTicketLinker(cfg journeyclient.Config, service projectv1.ProjectServiceClient, button js.Value) {
	document := js.Global().Get("document")
	if old := document.Call("getElementById", "projectui-ticket-linker"); old.Truthy() {
		old.Call("remove")
	}
	words := projectClosest(button, ".projectui-journey-actions")
	text := func(name string) string { return projectAttr(words, "data-text-"+name) }
	journeyID := projectCurrentJourney()
	if journeyID == "" {
		return
	}
	dialog := projectElement("dialog", "projectui-share-dialog projectui-workflow-picker projectui-ticket-linker", "")
	dialog.Set("id", "projectui-ticket-linker")
	body := projectElement("div", "projectui-share-form projectui-picker-body", "")
	head := projectElement("div", "projectui-share-head", "")
	head.Call("appendChild", projectElement("h2", "", text("linker-title")))
	closer := projectElement("button", "projectui-modal-close", "")
	closer.Set("type", "button")
	closer.Call("setAttribute", "aria-label", text("close"))
	closer.Call("appendChild", projectElement("span", "projectui-modal-close-icon", ""))
	head.Call("appendChild", closer)
	body.Call("appendChild", head)
	body.Call("appendChild", projectElement("p", "projectui-share-item", text("title")))
	label := projectElement("label", "projectui-share-label", text("project"))
	label.Call("setAttribute", "for", "projectui-linker-project")
	body.Call("appendChild", label)
	picker := projectElement("select", "projectui-share-select", "")
	picker.Set("id", "projectui-linker-project")
	body.Call("appendChild", picker)
	searchWrap := projectElement("div", "projectui-filter-search", "")
	searchWrap.Call("appendChild", projectElement("span", "projectui-filter-search-icon", ""))
	search := projectElement("input", "", "")
	search.Set("type", "search")
	search.Set("id", "projectui-linker-search")
	search.Set("placeholder", text("search"))
	search.Call("setAttribute", "aria-label", text("search"))
	searchWrap.Call("appendChild", search)
	body.Call("appendChild", searchWrap)
	list := projectElement("ul", "projectui-picker-list projectui-linker-list", "")
	list.Set("id", "projectui-linker-list")
	body.Call("appendChild", list)
	failure := projectElement("p", "projectui-share-error", text("failed"))
	failure.Set("hidden", true)
	body.Call("appendChild", failure)
	dialog.Call("appendChild", body)
	document.Get("body").Call("appendChild", dialog)

	status := func(message string) {
		list.Set("innerHTML", "")
		list.Call("appendChild", projectElement("li", "projectui-muted projectui-picker-empty", message))
	}
	render := func() {
		query := strings.ToLower(strings.TrimSpace(search.Get("value").String()))
		list.Set("innerHTML", "")
		shown := 0
		for _, task := range projectLinkerTasks {
			if query != "" && !strings.Contains(strings.ToLower(task.key+" "+task.title), query) {
				continue
			}
			if shown >= 60 {
				break
			}
			shown++
			item := projectElement("li", "projectui-picker-item", "")
			row := projectElement("button", "projectui-picker-row", "")
			row.Set("type", "button")
			row.Call("setAttribute", "data-task-id", task.taskID)
			row.Call("appendChild", projectElement("span", "projectui-workflow-title", task.title))
			row.Call("appendChild", projectElement("span", "projectui-task-key", task.key))
			item.Call("appendChild", row)
			list.Call("appendChild", item)
		}
		if shown == 0 {
			status(text("empty"))
		}
	}
	loadTasks := func(projectID string) {
		status(text("loading"))
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			progress, err := countProjectProgress(ctx, cfg, service, projectID)
			if err != nil {
				status(text("failed"))
				return
			}
			tasks := []projectLinkerTask{}
			for _, task := range progress.tasks {
				tasks = append(tasks, projectLinkerTask{projectID: projectID, taskID: task.GetTaskId(), key: projectui.TaskKey(task.GetTaskId()), title: task.GetTitle(), revision: task.GetRevision()})
			}
			sort.SliceStable(tasks, func(i, j int) bool { return strings.ToLower(tasks[i].title) < strings.ToLower(tasks[j].title) })
			projectLinkerTasks = tasks
			render()
		}()
	}
	closer.Call("addEventListener", "click", js.FuncOf(func(js.Value, []js.Value) any {
		dialog.Call("close")
		return nil
	}))
	dialog.Call("addEventListener", "close", js.FuncOf(func(js.Value, []js.Value) any {
		dialog.Call("remove")
		return nil
	}))
	search.Call("addEventListener", "input", js.FuncOf(func(js.Value, []js.Value) any {
		render()
		return nil
	}))
	picker.Call("addEventListener", "change", js.FuncOf(func(js.Value, []js.Value) any {
		loadTasks(picker.Get("value").String())
		return nil
	}))
	list.Call("addEventListener", "click", js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 0 {
			return nil
		}
		row := projectClosest(args[0].Get("target"), "button[data-task-id]")
		if !row.Truthy() || row.Get("disabled").Bool() {
			return nil
		}
		taskID := projectAttr(row, "data-task-id")
		var chosen projectLinkerTask
		for _, task := range projectLinkerTasks {
			if task.taskID == taskID {
				chosen = task
			}
		}
		if chosen.taskID == "" {
			return nil
		}
		row.Set("disabled", true)
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			_, err := service.AddTaskLink(chatRPCContext(ctx, cfg), &projectv1.AddTaskLinkRequest{
				IdempotencyKey: uuid.NewString(), ProjectId: chosen.projectID, TaskId: chosen.taskID,
				Reference:            &projectv1.TaskLinkReference{Target: &projectv1.TaskLinkReference_Journey{Journey: &projectv1.JourneyLink{IntentId: journeyID}}},
				ExpectedTaskRevision: chosen.revision,
			})
			if err != nil {
				row.Set("disabled", false)
				failure.Set("hidden", false)
				return
			}
			projectTaskLinksForget(cfg, chosen.projectID, chosen.taskID)
			projectProgressForget(cfg, chosen.projectID)
			dialog.Call("close")
			href := projectclient.CanonicalHref(projectclient.State{Route: projectclient.RouteProject, ProjectID: chosen.projectID, TaskID: chosen.taskID, BoardViewID: "default", View: projectclient.ViewTask})
			showProjectToast(strings.ReplaceAll(text("done"), "{ticket}", chosen.key+" · "+chosen.title), href, text("open"))
		}()
		return nil
	}))
	func() {
		defer func() { _ = recover() }()
		dialog.Call("showModal")
	}()
	status(text("loading"))
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		response, err := service.ListProjects(chatRPCContext(ctx, cfg), &projectv1.ListProjectsRequest{Page: &commonv1.PageRequest{PageSize: 100}})
		if err != nil {
			status(text("failed"))
			return
		}
		projects := response.GetProjects()
		sort.SliceStable(projects, func(i, j int) bool {
			return strings.ToLower(projects[i].GetName()) < strings.ToLower(projects[j].GetName())
		})
		for _, project := range projects {
			if project == nil || project.GetProjectId() == "" {
				continue
			}
			option := projectElement("option", "", project.GetName())
			option.Set("value", project.GetProjectId())
			picker.Call("appendChild", option)
		}
		if len(projects) > 0 {
			loadTasks(picker.Get("value").String())
		} else {
			status(text("empty"))
		}
	}()
}

// projectCurrentJourney is the journey the address bar names, if any.
func projectCurrentJourney() string {
	if currentPath() != "/workspace/app/journeys" {
		return ""
	}
	values, err := url.ParseQuery(currentQuery())
	if err != nil {
		return ""
	}
	return strings.TrimSpace(values.Get("journey"))
}

// projectResolveShareHref turns the journey page's placeholder into the
// journey's address.
func projectResolveShareHref(href string) string {
	if href == "journey:current" {
		if journeyID := projectCurrentJourney(); journeyID != "" {
			return "/workspace/app/journeys?journey=" + url.QueryEscape(journeyID)
		}
		return ""
	}
	return href
}

// ensureProjectShareDialog builds the send-to-chat dialog (and its toast)
// on a page that does not render one, from the words on the page's action
// slot. It lives at the end of <body>, outside any component tree.
func ensureProjectShareDialog(words js.Value) js.Value {
	document := js.Global().Get("document")
	if dialog := document.Call("getElementById", "projectui-share-dialog"); dialog.Truthy() {
		return dialog
	}
	text := func(name string) string { return projectAttr(words, "data-text-"+name) }
	host := projectElement("div", "projectui-share-host", "")
	dialog := projectElement("dialog", "projectui-share-dialog", "")
	dialog.Set("id", "projectui-share-dialog")
	form := projectElement("form", "projectui-share-form", "")
	form.Call("setAttribute", "data-projectui-action", "share-send")
	form.Call("setAttribute", "data-sent", text("sent"))
	head := projectElement("div", "projectui-share-head", "")
	head.Call("appendChild", projectElement("h2", "", text("send-title")))
	closer := projectElement("button", "projectui-modal-close", "")
	closer.Set("type", "button")
	closer.Call("setAttribute", "data-projectui-action", "close-share")
	closer.Call("setAttribute", "aria-label", text("close"))
	closer.Call("appendChild", projectElement("span", "projectui-modal-close-icon", ""))
	head.Call("appendChild", closer)
	form.Call("appendChild", head)
	item := projectElement("p", "projectui-share-item", "")
	item.Set("id", "projectui-share-item")
	form.Call("appendChild", item)
	label := projectElement("label", "projectui-share-label", text("conversation"))
	label.Call("setAttribute", "for", "projectui-share-conversation")
	form.Call("appendChild", label)
	picker := projectElement("select", "projectui-share-select", "")
	picker.Set("id", "projectui-share-conversation")
	picker.Set("name", "conversation")
	picker.Call("setAttribute", "data-fill", "true")
	picker.Call("setAttribute", "data-channels", text("channels"))
	picker.Call("setAttribute", "data-direct", text("direct"))
	placeholder := projectElement("option", "", "…")
	placeholder.Set("value", "")
	picker.Call("appendChild", placeholder)
	form.Call("appendChild", picker)
	noteLabel := projectElement("label", "projectui-share-label", text("note"))
	noteLabel.Call("setAttribute", "for", "projectui-share-note")
	form.Call("appendChild", noteLabel)
	note := projectElement("textarea", "projectui-share-note", "")
	note.Set("id", "projectui-share-note")
	note.Set("name", "note")
	note.Set("rows", 2)
	form.Call("appendChild", note)
	for _, name := range []string{"href", "title"} {
		hidden := projectElement("input", "", "")
		hidden.Set("type", "hidden")
		hidden.Set("name", name)
		form.Call("appendChild", hidden)
	}
	failure := projectElement("p", "projectui-share-error", text("share-failed"))
	failure.Set("hidden", true)
	form.Call("appendChild", failure)
	actions := projectElement("div", "projectui-share-actions", "")
	cancel := projectElement("button", "projectui-button", text("cancel"))
	cancel.Set("type", "button")
	cancel.Call("setAttribute", "data-projectui-action", "close-share")
	send := projectElement("button", "projectui-button projectui-button-primary", text("send"))
	send.Set("type", "submit")
	actions.Call("appendChild", cancel)
	actions.Call("appendChild", send)
	form.Call("appendChild", actions)
	dialog.Call("appendChild", form)
	host.Call("appendChild", dialog)
	toast := projectElement("div", "projectui-toast", "")
	toast.Set("id", "projectui-toast")
	toast.Call("setAttribute", "role", "status")
	toast.Set("hidden", true)
	toast.Call("appendChild", projectElement("span", "projectui-toast-text", ""))
	link := projectElement("a", "projectui-toast-link", text("open-conversation"))
	link.Call("setAttribute", "href", "/workspace/app/chat")
	toast.Call("appendChild", link)
	host.Call("appendChild", toast)
	document.Get("body").Call("appendChild", host)
	return dialog
}
