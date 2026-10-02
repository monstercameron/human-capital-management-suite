//go:build js && wasm

package main

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"syscall/js"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	documentv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/document/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/agentdocref"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var agentRequestDocuments struct {
	sync.Mutex
	timer *time.Timer
	epoch uint64
}

var agentRequestDocumentListeners sync.Once

var (
	agentPageObserver         js.Value
	agentPageObserverCallback js.Func
	agentPageObserved         js.Value
)

func installAgentPageObserver() {
	if agentPageObserver.Truthy() {
		return
	}
	document := js.Global().Get("document")
	body := document.Get("body")
	observerConstructor := js.Global().Get("MutationObserver")
	if !body.Truthy() || observerConstructor.Type() != js.TypeFunction {
		return
	}
	agentPageObserverCallback = js.FuncOf(func(js.Value, []js.Value) any {
		page := document.Call("querySelector", ".agents-page")
		if !page.Truthy() {
			stopAgentTaskPolling()
			agentPageObserved = js.Undefined()
			return nil
		}
		if agentPageObserved.Truthy() && page.Equal(agentPageObserved) {
			return nil
		}
		agentPageObserved = page
		scheduleAgentPageHydration()
		return nil
	})
	agentPageObserver = observerConstructor.New(agentPageObserverCallback)
	agentPageObserver.Call("observe", body, map[string]any{"childList": true, "subtree": true})
}

func scheduleAgentPageHydration() {
	time.AfterFunc(100*time.Millisecond, func() {
		go func() {
			ui.PostAsync(hydrateAgentTaskPage)
			go probeAgentDocumentHub()
			refreshAgentTasks()
		}()
	})
}

func agentPageView() (productui.View, productui.LocaleContext) {
	agentBrowser.Lock()
	cfg := agentBrowser.cfg
	agentBrowser.Unlock()
	locale := productui.ResolveProductLocale(cfg.Locale).WithTimeZone(agentViewerTimeZone())
	view := productui.NewView(productui.PageAgents, cfg.Tenant, cfg.Subject, "")
	view.Locale = locale
	return view, locale
}

func renderAgentTasks(loading, failed bool) {
	document := js.Global().Get("document")
	mount := document.Call("querySelector", ".agents-tasks")
	if !mount.Truthy() {
		return
	}
	agentBrowser.Lock()
	tasks := append([]productui.AgentTask(nil), agentBrowser.tasks...)
	agents := agentBrowser.cfg.Agents
	agentBrowser.Unlock()
	snapshot := agentTasksRegionSnapshot(agents, tasks, loading, failed)
	if taskID := selectedAgentTaskID(js.Global().Get("location").Get("search").String()); taskID != "" {
		for index := range tasks {
			if tasks[index].ID == taskID {
				selected := tasks[index]
				snapshot.SelectedTask = &selected
				break
			}
		}
	}
	view, locale := agentPageView()
	markup, err := ui.RenderToString(productui.RenderAgentTasksRegion(view, locale, snapshot))
	if err == nil {
		mount.Set("outerHTML", markup)
		page := document.Call("querySelector", ".agents-page")
		if page.Truthy() {
			if snapshot.SelectedTask != nil {
				page.Get("dataset").Set("hasTaskDetail", "true")
			} else {
				page.Call("removeAttribute", "data-has-task-detail")
			}
		}
		hydrateAgentTaskPage()
	}
}

// ensureStartedAgentTaskPolling keeps the caller's newly started task fresh
// even when its detail is not open. Earlier code polled only running steps in
// an open detail, leaving the composer in its loading state indefinitely.
func ensureStartedAgentTaskPolling(task productui.AgentTask) {
	if agentTaskFinal(task) {
		applyStartedAgentTask(task)
		return
	}
	agentBrowser.Lock()
	if agentBrowser.startPollCancel != nil {
		agentBrowser.startPollCancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	agentBrowser.startPollCancel, agentBrowser.startPollTaskID = cancel, task.ID
	binding := agentBrowser.binding
	agentBrowser.Unlock()
	if binding == nil {
		return
	}
	go pollAgentTask(ctx, 3*time.Second, task.ID, func(ctx context.Context, taskID string) (productui.AgentTask, error) {
		callCtx, callCancel := context.WithTimeout(ctx, agentCallTimeout)
		defer callCancel()
		return binding.GetTask(callCtx, taskID)
	}, func(projected productui.AgentTask) {
		ui.PostAsync(func() { applyStartedAgentTask(projected) })
	})
}

func applyStartedAgentTask(projection productui.AgentTask) {
	agentBrowser.Lock()
	merged := projection
	for index, previous := range agentBrowser.tasks {
		if previous.ID == projection.ID {
			merged = mergeAgentTask(previous, projection)
			agentBrowser.tasks[index] = merged
			break
		}
	}
	started := agentBrowser.startedTaskID == merged.ID
	terminal := started && agentTaskFinal(merged)
	if terminal {
		if agentBrowser.startPollCancel != nil && agentBrowser.startPollTaskID == merged.ID {
			agentBrowser.startPollCancel()
			agentBrowser.startPollCancel, agentBrowser.startPollTaskID = nil, ""
		}
		agentBrowser.startedTaskID = ""
	}
	agentBrowser.Unlock()
	renderAgentTasks(false, false)
	if terminal {
		announceStartedAgentTask(merged)
	}
}

func announceStartedAgentTask(task productui.AgentTask) {
	document := js.Global().Get("document")
	if status := document.Call("getElementById", "agents-composer-status"); status.Truthy() {
		status.Set("textContent", "")
	}
	tasks := document.Call("querySelector", ".agents-tasks")
	if !tasks.Truthy() {
		return
	}
	announcement := document.Call("getElementById", "agents-tasks-announcement")
	if announcement.Truthy() {
		agent := strings.TrimSpace(task.AnsweringAgentDisplayName)
		if strings.EqualFold(strings.TrimSpace(task.AnsweringAgentID), "general-agent") {
			_, locale := agentPageView()
			agent = locale.Text("agents.general_agent")
		}
		if agent == "" {
			agent = domDataset(announcement, "msgAgent")
		}
		key := "msgTask" + capitalizeASCII(agentTaskTerminalAnnouncement(task))
		message := strings.ReplaceAll(domDataset(announcement, key), "__AGENT__", agent)
		announcement.Set("textContent", message)
	}
	category := "failed"
	if task.State == productui.AgentTaskCompleted {
		category = "completed"
	}
	if filter := tasks.Call("querySelector", `button[data-agent-task-filter="`+category+`"]`); filter.Truthy() {
		selectAgentTaskFilter(tasks, filter, false)
	}
	links := tasks.Call("querySelectorAll", ".agents-task-link[data-agent-task-row-link]")
	for index := 0; index < links.Get("length").Int(); index++ {
		link := links.Call("item", index)
		if domDataset(link, "agentTaskRowLink") != task.ID {
			continue
		}
		link.Get("classList").Call("add", "is-new")
		link.Call("scrollIntoView", map[string]any{"block": "nearest"})
		link.Call("focus")
		time.AfterFunc(4*time.Second, func() {
			ui.PostAsync(func() { link.Get("classList").Call("remove", "is-new") })
		})
		break
	}
}

func refreshAgentTasks() {
	binding := currentAgentBinding()
	if binding == nil {
		ui.PostAsync(func() { renderAgentTasks(false, true) })
		return
	}
	ui.PostAsync(func() { renderAgentTasks(true, false) })
	ctx, cancel := context.WithTimeout(context.Background(), agentCallTimeout)
	tasks, err := binding.ListTasks(ctx)
	cancel()
	if err == nil {
		initialAgentTasks.RLock()
		firstPaint := append([]productui.AgentTask(nil), initialAgentTasks.tasks...)
		initialAgentTasks.RUnlock()
		agentBrowser.Lock()
		previous := agentBrowser.tasks
		if len(previous) == 0 && len(firstPaint) > 0 {
			previous = firstPaint
		}
		agentBrowser.tasks = reconcileAgentTasks(previous, tasks)
		agentBrowser.Unlock()
	}
	ui.PostAsync(func() {
		renderAgentTasks(false, err != nil)
		if err == nil {
			go refreshSelectedAgentTask()
		}
	})
}

func refreshSelectedAgentTask() {
	location := js.Global().Get("location")
	query := js.Global().Get("URLSearchParams").New(location.Get("search").String())
	taskID := selectedAgentTaskID(location.Get("search").String())
	if taskID == "" {
		stopAgentTaskPolling()
		return
	}
	agentBrowser.Lock()
	allowed := selectedAgentTaskCanRefresh(taskID, agentBrowser.tasks)
	agentBrowser.Unlock()
	if !allowed {
		stopAgentTaskPolling()
		query.Call("delete", "task")
		target := location.Get("pathname").String()
		if remaining := query.Call("toString").String(); remaining != "" {
			target += "?" + remaining
		}
		location.Set("href", target+"#agents-tasks")
		return
	}
	refreshAgentTaskByID(taskID)
}

func refreshAgentTaskByID(taskID string) {
	binding := currentAgentBinding()
	if binding == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), agentCallTimeout)
	projection, err := binding.GetTask(ctx, taskID)
	cancel()
	if err != nil {
		return
	}
	agentBrowser.Lock()
	merged := projection
	for index, previous := range agentBrowser.tasks {
		if previous.ID == taskID {
			merged = mergeAgentTask(previous, projection)
			agentBrowser.tasks[index] = merged
			break
		}
	}
	if !agentTaskNeedsPolling(merged) && agentBrowser.pollTaskID == projection.ID {
		if agentBrowser.pollCancel != nil {
			agentBrowser.pollCancel()
		}
		agentBrowser.pollCancel, agentBrowser.pollTaskID = nil, ""
	}
	retry := agentBrowser.cfg.Agents != nil && agentBrowser.cfg.Agents.StartAvailable
	agentBrowser.Unlock()
	ui.PostAsync(func() {
		mount := js.Global().Get("document").Call("querySelector", ".agents-task-view")
		if !mount.Truthy() {
			return
		}
		view, locale := agentPageView()
		markup, renderErr := ui.RenderToString(productui.RenderAgentTaskDetail(view, locale, merged, retry))
		if renderErr == nil {
			mount.Set("outerHTML", markup)
			ensureAgentTaskPolling(merged)
		}
	})
}

func stopAgentTaskPolling() {
	agentBrowser.Lock()
	if agentBrowser.pollCancel != nil {
		agentBrowser.pollCancel()
	}
	agentBrowser.pollCancel, agentBrowser.pollTaskID = nil, ""
	agentBrowser.Unlock()
}

func ensureAgentTaskPolling(task productui.AgentTask) {
	agentBrowser.Lock()
	if !agentTaskNeedsPolling(task) {
		if agentBrowser.pollCancel != nil {
			agentBrowser.pollCancel()
		}
		agentBrowser.pollCancel, agentBrowser.pollTaskID = nil, ""
		agentBrowser.Unlock()
		return
	}
	if agentBrowser.pollCancel != nil && agentBrowser.pollTaskID == task.ID {
		agentBrowser.Unlock()
		return
	}
	if agentBrowser.pollCancel != nil {
		agentBrowser.pollCancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	agentBrowser.pollCancel, agentBrowser.pollTaskID = cancel, task.ID
	binding := agentBrowser.binding
	agentBrowser.Unlock()
	go pollAgentTask(ctx, 3*time.Second, task.ID, func(ctx context.Context, taskID string) (productui.AgentTask, error) {
		callCtx, callCancel := context.WithTimeout(ctx, agentCallTimeout)
		defer callCancel()
		return binding.GetTask(callCtx, taskID)
	}, func(projected productui.AgentTask) {
		ui.PostAsync(func() { applyPolledAgentTask(projected) })
	})
}

func applyPolledAgentTask(projection productui.AgentTask) {
	agentBrowser.Lock()
	merged := projection
	for index, previous := range agentBrowser.tasks {
		if previous.ID == projection.ID {
			merged = mergeAgentTask(previous, projection)
			agentBrowser.tasks[index] = merged
			break
		}
	}
	retry := agentBrowser.cfg.Agents != nil && agentBrowser.cfg.Agents.StartAvailable
	agentBrowser.Unlock()
	mount := js.Global().Get("document").Call("querySelector", ".agents-task-view")
	if !mount.Truthy() {
		stopAgentTaskPolling()
		return
	}
	view, locale := agentPageView()
	markup, err := ui.RenderToString(productui.RenderAgentTaskDetail(view, locale, merged, retry))
	if err == nil {
		mount.Set("outerHTML", markup)
	}
}

func setAgentStartFailure(node js.Value, err error) {
	if !node.Truthy() {
		return
	}
	code := status.Code(err)
	if code == codes.PermissionDenied || code == codes.FailedPrecondition || code == codes.Unavailable {
		if message := strings.TrimSpace(status.Convert(err).Message()); message != "" {
			node.Set("textContent", message)
			return
		}
	}
	key := capitalizeASCII(agentStartMessageKey(err))
	node.Set("textContent", domDataset(node, "msg"+key))
}

func probeAgentDocumentHub() {
	personaDocumentPicker.Lock()
	client := personaDocumentPicker.client
	personaDocumentPicker.Unlock()
	if client == nil {
		return
	}
	agentBrowser.Lock()
	cfg := agentBrowser.cfg
	agentBrowser.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	_, err := client.GetDocumentLibrary(chatRPCContext(ctx, cfg), &documentv1.GetDocumentLibraryRequest{})
	cancel()
	if err == nil {
		ui.PostAsync(mountAgentRequestDocumentPicker)
	}
}

func mountAgentRequestDocumentPicker() {
	mount := js.Global().Get("document").Call("querySelector", "[data-agent-request-document-mount]")
	if !mount.Truthy() || mount.Call("querySelector", "[data-agent-request-documents]").Truthy() {
		return
	}
	_, locale := agentPageView()
	markup, err := ui.RenderToString(productui.AgentRequestDocumentPicker(locale))
	if err == nil {
		mount.Set("innerHTML", markup)
	}
}

func installAgentRequestDocumentHandlers() {
	agentRequestDocumentListeners.Do(func() {
		document := js.Global().Get("document")
		document.Call("addEventListener", "click", js.FuncOf(func(_ js.Value, args []js.Value) any {
			if len(args) > 0 {
				handleAgentRequestDocumentClick(args[0])
			}
			return nil
		}))
		document.Call("addEventListener", "input", js.FuncOf(func(_ js.Value, args []js.Value) any {
			if len(args) == 0 {
				return nil
			}
			target := args[0].Get("target")
			if target.Get("id").String() == "agents-composer-input" && strings.HasSuffix(target.Get("value").String(), "#") {
				openAgentRequestDocumentPicker(target.Call("closest", "form"))
			}
			if target.Get("dataset").Get("agentDocumentSearch").Type() == js.TypeString {
				scheduleAgentRequestDocumentSearch(target)
			}
			return nil
		}))
		document.Call("addEventListener", "keydown", js.FuncOf(func(_ js.Value, args []js.Value) any {
			if len(args) == 0 {
				return nil
			}
			event, target := args[0], args[0].Get("target")
			if target.Get("dataset").Get("agentDocumentSearch").Type() == js.TypeString && agentRequestDocumentKey(target, event.Get("key").String()) {
				event.Call("preventDefault")
			}
			return nil
		}))
	})
}

func handleAgentRequestDocumentClick(event js.Value) {
	target := event.Get("target")
	if !target.Truthy() || target.Get("closest").Type() != js.TypeFunction {
		return
	}
	if button := target.Call("closest", "[data-agent-document-open]"); button.Truthy() {
		event.Call("preventDefault")
		form := button.Call("closest", "form")
		root := form.Call("querySelector", "[data-agent-request-documents]")
		setAgentRequestDocumentPickerOpen(root, domAttribute(button, "aria-expanded") != "true")
		return
	}
	if button := target.Call("closest", "[data-agent-document-remove]"); button.Truthy() {
		event.Call("preventDefault")
		root := button.Call("closest", "[data-agent-request-documents]")
		button.Call("closest", "[data-agent-request-reference]").Call("remove")
		updateAgentRequestDocumentCount(root)
		return
	}
	if option := target.Call("closest", "[data-agent-document-option]"); option.Truthy() {
		event.Call("preventDefault")
		addAgentRequestDocument(option.Call("closest", "[data-agent-request-documents]"), option)
	}
}

func openAgentRequestDocumentPicker(form js.Value) {
	if !form.Truthy() {
		return
	}
	root := form.Call("querySelector", "[data-agent-request-documents]")
	if !root.Truthy() {
		return
	}
	setAgentRequestDocumentPickerOpen(root, true)
}

func setAgentRequestDocumentPickerOpen(root js.Value, open bool) {
	if !root.Truthy() {
		return
	}
	root.Call("querySelector", "[data-agent-document-panel]").Set("hidden", !open)
	button := root.Call("querySelector", "[data-agent-document-open]")
	button.Call("setAttribute", "aria-expanded", strconv.FormatBool(open))
	label := domDataset(root, "msgOpen")
	if open {
		label = domDataset(root, "msgDone")
	}
	button.Set("textContent", label)
	if open {
		root.Call("querySelector", "[data-agent-document-search]").Call("focus")
	}
}

func scheduleAgentRequestDocumentSearch(input js.Value) {
	root := input.Call("closest", "[data-agent-request-documents]")
	setAgentRequestDocumentStatus(root, domDataset(root, "msgLoading"))
	agentRequestDocuments.Lock()
	agentRequestDocuments.epoch++
	epoch := agentRequestDocuments.epoch
	if agentRequestDocuments.timer != nil {
		agentRequestDocuments.timer.Stop()
	}
	query := strings.TrimSpace(input.Get("value").String())
	agentRequestDocuments.timer = time.AfterFunc(250*time.Millisecond, func() { go searchAgentRequestDocuments(epoch, query) })
	agentRequestDocuments.Unlock()
}

func searchAgentRequestDocuments(epoch uint64, query string) {
	personaDocumentPicker.Lock()
	client := personaDocumentPicker.client
	personaDocumentPicker.Unlock()
	agentBrowser.Lock()
	cfg := agentBrowser.cfg
	agentBrowser.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	locale := productui.ResolveProductLocale(cfg.Locale).WithTimeZone(agentViewerTimeZone())
	items, err := searchAgentRequestDocumentSuggestions(chatRPCContext(ctx, cfg), client, query, cfg.Subject, locale, time.Now())
	cancel()
	ui.PostAsync(func() {
		agentRequestDocuments.Lock()
		current := agentRequestDocuments.epoch
		agentRequestDocuments.Unlock()
		if epoch != current {
			return
		}
		root := js.Global().Get("document").Call("querySelector", "[data-agent-request-documents]")
		if !root.Truthy() {
			return
		}
		if err != nil {
			renderAgentRequestDocumentResults(root, nil)
			setAgentRequestDocumentStatus(root, domDataset(root, "msgFailed"))
			return
		}
		renderAgentRequestDocumentResults(root, items)
		if len(items) == 0 {
			setAgentRequestDocumentStatus(root, domDataset(root, "msgEmpty"))
		} else {
			setAgentRequestDocumentStatus(root, "")
		}
	})
}

func renderAgentRequestDocumentResults(root js.Value, items []productui.AgentRequestDocumentSuggestion) {
	document := js.Global().Get("document")
	list := root.Call("querySelector", "[data-agent-document-results]")
	list.Set("textContent", "")
	_, locale := agentPageView()
	for index, item := range items {
		markup, err := ui.RenderToString(productui.AgentRequestDocumentResult(locale, item, index))
		if err != nil {
			continue
		}
		template := document.Call("createElement", "template")
		template.Set("innerHTML", markup)
		list.Call("append", template.Get("content").Get("firstElementChild"))
	}
	list.Set("hidden", len(items) == 0)
	input := root.Call("querySelector", "[data-agent-document-search]")
	input.Call("setAttribute", "aria-expanded", strconv.FormatBool(len(items) > 0))
	root.Get("dataset").Set("activeIndex", "-1")
}

func agentRequestDocumentKey(input js.Value, key string) bool {
	root := input.Call("closest", "[data-agent-request-documents]")
	options := root.Call("querySelectorAll", "[data-agent-document-option]")
	count := options.Get("length").Int()
	if key == "Escape" {
		setAgentRequestDocumentPickerOpen(root, false)
		return true
	}
	if count == 0 {
		return false
	}
	active, _ := strconv.Atoi(domDataset(root, "activeIndex"))
	switch key {
	case "ArrowDown", "ArrowUp":
		delta := 1
		if key == "ArrowUp" {
			delta = -1
		}
		active = (active + delta + count) % count
		root.Get("dataset").Set("activeIndex", strconv.Itoa(active))
		input.Call("setAttribute", "aria-activedescendant", options.Call("item", active).Get("id").String())
		return true
	case "Enter":
		if active >= 0 && active < count {
			addAgentRequestDocument(root, options.Call("item", active))
			return true
		}
	}
	return false
}

func addAgentRequestDocument(root, option js.Value) {
	limit, _ := strconv.Atoi(domDataset(root, "agentdocLimit"))
	rows := root.Call("querySelectorAll", "[data-agent-request-reference]")
	if rows.Get("length").Int() >= limit {
		setAgentRequestDocumentStatus(root, domDataset(root, "msgLimit"))
		return
	}
	documentID := domDataset(option, "agentDocumentOption")
	for index := 0; index < rows.Get("length").Int(); index++ {
		if domDataset(rows.Call("item", index), "documentId") == documentID {
			return
		}
	}
	title := domDataset(option, "agentDocumentTitle")
	anchor := domDataset(option, "agentDocumentAnchor")
	_, locale := agentPageView()
	markup, err := ui.RenderToString(productui.AgentRequestDocumentChip(locale, documentID, title, anchor))
	if err != nil {
		return
	}
	template := js.Global().Get("document").Call("createElement", "template")
	template.Set("innerHTML", markup)
	root.Call("querySelector", "[data-agent-document-chips]").Call("append", template.Get("content").Get("firstElementChild"))
	root.Call("querySelector", "[data-agent-document-search]").Set("value", "")
	renderAgentRequestDocumentResults(root, nil)
	updateAgentRequestDocumentCount(root)
	setAgentRequestDocumentPickerOpen(root, false)
}

func updateAgentRequestDocumentCount(root js.Value) {
	count := root.Call("querySelectorAll", "[data-agent-request-reference]").Get("length").Int()
	countNode := root.Call("querySelector", "[data-agent-document-count]")
	template := domDataset(root, "msgCount")
	countNode.Set("textContent", strings.ReplaceAll(template, "__COUNT__", localizedAgentPageNumber(count)))
	countNode.Set("hidden", count == 0)
	if count >= agentdocref.MaxRequestReferences {
		setAgentRequestDocumentStatus(root, domDataset(root, "msgLimit"))
	}
	atLimit := count >= agentdocref.MaxRequestReferences
	root.Call("querySelector", "[data-agent-document-open]").Set("disabled", atLimit)
	root.Call("querySelector", "[data-agent-document-search]").Set("disabled", atLimit)
}

func localizedAgentPageNumber(value int) string {
	agentBrowser.Lock()
	locale := productui.ResolveProductLocale(agentBrowser.cfg.Locale)
	agentBrowser.Unlock()
	return locale.FormatNumber(strconv.Itoa(value), 0)
}

func setAgentRequestDocumentStatus(root js.Value, message string) {
	if node := root.Call("querySelector", "[data-agent-document-status]"); node.Truthy() {
		node.Set("textContent", message)
	}
}

func agentRequestDocumentReferences(form js.Value) []agentdocref.Reference {
	if !form.Truthy() {
		return nil
	}
	rows := form.Call("querySelectorAll", "[data-agent-request-reference]")
	result := make([]agentdocref.Reference, 0, rows.Get("length").Int())
	for index := 0; index < rows.Get("length").Int() && index < agentdocref.MaxRequestReferences; index++ {
		row := rows.Call("item", index)
		result = append(result, agentdocref.Reference{DocumentID: domDataset(row, "documentId"), Label: domDataset(row, "documentLabel"), SectionAnchor: domDataset(row, "sectionAnchor"), VersionMode: agentdocref.ModeLatestPublished})
	}
	return result
}

func clearAgentRequestDocuments(form js.Value) {
	if !form.Truthy() {
		return
	}
	root := form.Call("querySelector", "[data-agent-request-documents]")
	if !root.Truthy() {
		return
	}
	root.Call("querySelector", "[data-agent-document-chips]").Set("textContent", "")
	root.Call("querySelector", "[data-agent-document-search]").Set("value", "")
	renderAgentRequestDocumentResults(root, nil)
	setAgentRequestDocumentStatus(root, "")
	setAgentRequestDocumentPickerOpen(root, false)
	updateAgentRequestDocumentCount(root)
}

func agentTaskDocumentReferencesFromView(view js.Value) []agentdocref.Reference {
	if !view.Truthy() {
		return nil
	}
	rows := view.Call("querySelectorAll", "[data-agent-task-document]")
	result := make([]agentdocref.Reference, 0, rows.Get("length").Int())
	for index := 0; index < rows.Get("length").Int() && index < agentdocref.MaxRequestReferences; index++ {
		row := rows.Call("item", index)
		result = append(result, agentdocref.Reference{DocumentID: domDataset(row, "documentId"), Label: domDataset(row, "documentLabel"), SectionAnchor: domDataset(row, "sectionAnchor"), VersionMode: agentdocref.ModeLatestPublished})
	}
	return result
}
