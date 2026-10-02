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
	agentv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/agent/v1"
	documentv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/document/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/agentdocref"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"google.golang.org/grpc"

	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

var agentBrowser struct {
	sync.Mutex
	binding         *agentServiceBinding
	cfg             journeyclient.Config
	tasks           []productui.AgentTask
	installed       bool
	busy            bool
	pollCancel      context.CancelFunc
	pollTaskID      string
	startPollCancel context.CancelFunc
	startPollTaskID string
	startedTaskID   string
}

const agentCallTimeout = 30 * time.Second

// configureAgentService binds the Agents page writes to the workspace gRPC
// tunnel and installs the composer's delegated click handler once.
func configureAgentService(conn grpc.ClientConnInterface, cfg journeyclient.Config) {
	configureAgentControls(cfg)
	configureAgentAnnouncements(cfg)
	configureAgentRolloutPortable(cfg)
	configureAgentAccess(cfg)
	agentBrowser.Lock()
	if agentBrowser.pollCancel != nil {
		agentBrowser.pollCancel()
	}
	if agentBrowser.startPollCancel != nil {
		agentBrowser.startPollCancel()
	}
	agentBrowser.binding = newAgentServiceBinding(cfg, conn)
	agentBrowser.cfg = cfg
	initialAgentTasks.RLock()
	agentBrowser.tasks = append([]productui.AgentTask(nil), initialAgentTasks.tasks...)
	initialAgentTasks.RUnlock()
	agentBrowser.busy = false
	agentBrowser.pollCancel, agentBrowser.pollTaskID = nil, ""
	agentBrowser.startPollCancel, agentBrowser.startPollTaskID, agentBrowser.startedTaskID = nil, "", ""
	install := !agentBrowser.installed
	agentBrowser.installed = true
	agentBrowser.Unlock()
	configurePersonaAdminDocumentService(documentv1.NewDocumentServiceClient(conn))
	installAgentRequestDocumentHandlers()
	installAgentPageObserver()
	scheduleAgentPageHydration()
	if install {
		document := js.Global().Get("document")
		document.Call("addEventListener", "click", js.FuncOf(func(_ js.Value, args []js.Value) any {
			if len(args) > 0 {
				handleAgentComposerClick(args[0])
				handleAgentAskAgainClick(args[0])
				handleAgentRetryClick(args[0])
				handleAgentFollowUpClick(args[0])
				handleAgentCopyAnswerClick(args[0])
				handleAgentTaskClick(args[0])
				handleAgentTaskListClick(args[0])
			}
			return nil
		}))
		document.Call("addEventListener", "keydown", js.FuncOf(func(_ js.Value, args []js.Value) any {
			if len(args) > 0 {
				handleAgentTaskFilterKey(args[0])
			}
			return nil
		}))
		document.Call("addEventListener", "change", js.FuncOf(func(_ js.Value, args []js.Value) any {
			if len(args) > 0 {
				handleAgentSelectionChange(args[0])
			}
			return nil
		}))
	}
}

const agentFollowUpStorageKey = "hcm-agent-follow-up-context"

const agentSelectionStorageKey = "hcm-agent-selected-persona"

const (
	agentTaskFilterStorageKey = "hcm-agent-task-filter"
	agentTaskScrollStorageKey = "hcm-agent-task-scroll"
	agentTaskFocusStorageKey  = "hcm-agent-task-focus"
)

func hydrateAgentTaskPage() {
	document := js.Global().Get("document")
	if detail := document.Call("querySelector", "[data-task-view][data-task-request]"); detail.Truthy() {
		prefix := agentTaskTitlePrefix(domDataset(detail, "taskRequest"))
		current := document.Get("title").String()
		if prefix != "" && !strings.HasPrefix(current, prefix+" · ") {
			document.Set("title", prefix+" · "+current)
		}
	}
	input := document.Call("getElementById", "agents-composer-input")
	storage, _ := browserSessionStorage()
	if tasks := document.Call("querySelector", ".agents-tasks"); tasks.Truthy() && storage.Truthy() {
		counts := map[string]int{}
		for _, category := range []string{"active", "completed", "failed"} {
			counts[category] = tasks.Call("querySelectorAll", `[data-task-category="`+category+`"]`).Get("length").Int()
		}
		selected := domDataset(tasks, "selectedTaskFilter")
		if domDataset(tasks, "selectedTaskId") == "" {
			storedFilter, _ := browserStorageGet(storage, agentTaskFilterStorageKey)
			selected = preferredAgentTaskFilter(storedFilter, "", counts)
		}
		if selected != "" {
			if filter := tasks.Call("querySelector", `button[data-agent-task-filter="`+selected+`"]`); filter.Truthy() {
				selectAgentTaskFilter(tasks, filter, false)
			}
		}
		if saved, ok := browserStorageGet(storage, agentTaskScrollStorageKey); ok && saved != "" {
			js.Global().Call("scrollTo", 0, js.Global().Get("Number").Invoke(saved))
			browserStorageSet(storage, agentTaskScrollStorageKey, "")
		}
		if detail := tasks.Call("querySelector", "#agents-task-title"); detail.Truthy() && domDataset(tasks.Call("closest", ".agents-page"), "agentDetailFocused") == "" {
			detail.Call("focus")
			tasks.Call("closest", ".agents-page").Get("dataset").Set("agentDetailFocused", "true")
		} else if focusID, ok := browserStorageGet(storage, agentTaskFocusStorageKey); ok && focusID != "" {
			if row := tasks.Call("querySelector", `[data-agent-task-row-link="`+focusID+`"]`); row.Truthy() {
				row.Call("focus")
				browserStorageSet(storage, agentTaskFocusStorageKey, "")
			}
		}
	}
	if !input.Truthy() || !storage.Truthy() {
		return
	}
	selectedPersona, _ := browserStorageGet(storage, agentSelectionStorageKey)
	choices := document.Call("querySelectorAll", `input[name="agent"]`)
	for index := 0; index < choices.Get("length").Int(); index++ {
		choice := choices.Call("item", index)
		if choice.Get("value").String() == selectedPersona {
			choice.Set("checked", true)
			break
		}
	}
	context, _ := browserStorageGet(storage, agentFollowUpStorageKey)
	if context != "" {
		input.Set("value", context)
		browserStorageSet(storage, agentFollowUpStorageKey, "")
		input.Call("focus")
	}
}

func handleAgentFollowUpClick(event js.Value) {
	target := event.Get("target")
	if !target.Truthy() || target.Get("closest").Type() != js.TypeFunction {
		return
	}
	button := target.Call("closest", "button[data-agent-follow-up]")
	if !button.Truthy() {
		return
	}
	event.Call("preventDefault")
	context := agentFollowUpContext(domDataset(button, "agentFollowUpContext"))
	if storage, ok := browserSessionStorage(); ok {
		browserStorageSet(storage, agentFollowUpStorageKey, context)
	}
	js.Global().Get("location").Set("href", domDataset(button, "agentFollowUpHref"))
}

// handleAgentAskAgainClick restores a failed request in the composer rather
// than resubmitting it. This gives the person a chance to correct it and
// makes the recovery path work for terminal failures too.
func handleAgentAskAgainClick(event js.Value) {
	target := event.Get("target")
	if !target.Truthy() || target.Get("closest").Type() != js.TypeFunction {
		return
	}
	button := target.Call("closest", "button[data-agent-ask-again]")
	if !button.Truthy() {
		return
	}
	event.Call("preventDefault")
	document := js.Global().Get("document")
	input := document.Call("getElementById", "agents-composer-input")
	if !input.Truthy() {
		return
	}
	input.Set("value", domDataset(button, "agentRetryPrompt"))
	personaID := generalAgentSelection(domDataset(button, "agentRetryPersona"))
	choices := document.Call("querySelectorAll", `input[name="agent"]`)
	for index := 0; index < choices.Get("length").Int(); index++ {
		choice := choices.Call("item", index)
		choice.Set("checked", generalAgentSelection(choice.Get("value").String()) == personaID)
	}
	form := input.Call("closest", "form")
	restoreAgentRequestDocuments(form, agentTaskDocumentReferencesFromView(button.Call("closest", "[data-task-id]")))
	input.Call("scrollIntoView", map[string]any{"block": "center"})
	input.Call("focus")
}

func restoreAgentRequestDocuments(form js.Value, references []agentdocref.Reference) {
	if !form.Truthy() {
		return
	}
	root := form.Call("querySelector", "[data-agent-request-documents]")
	if !root.Truthy() {
		return
	}
	clearAgentRequestDocuments(form)
	chips := root.Call("querySelector", "[data-agent-document-chips]")
	_, locale := agentPageView()
	for _, reference := range references {
		if strings.TrimSpace(reference.DocumentID) == "" {
			continue
		}
		markup, err := ui.RenderToString(productui.AgentRequestDocumentChip(locale, reference.DocumentID, reference.Label, reference.SectionAnchor))
		if err != nil {
			continue
		}
		template := js.Global().Get("document").Call("createElement", "template")
		template.Set("innerHTML", markup)
		chips.Call("append", template.Get("content").Get("firstElementChild"))
	}
	updateAgentRequestDocumentCount(root)
}

func handleAgentSelectionChange(event js.Value) {
	target := event.Get("target")
	if !target.Truthy() || target.Get("name").String() != "agent" || target.Get("type").String() != "radio" || !target.Get("checked").Bool() {
		return
	}
	if storage, ok := browserSessionStorage(); ok {
		browserStorageSet(storage, agentSelectionStorageKey, target.Get("value").String())
	}
}

func handleAgentCopyAnswerClick(event js.Value) {
	target := event.Get("target")
	if !target.Truthy() || target.Get("closest").Type() != js.TypeFunction {
		return
	}
	button := target.Call("closest", "button[data-agent-copy-answer]")
	if !button.Truthy() {
		return
	}
	event.Call("preventDefault")
	statusNode := button.Call("closest", ".agents-task-next-actions").Call("querySelector", "[data-agent-copy-status]")
	fail := func() {
		if statusNode.Truthy() {
			statusNode.Set("textContent", domDataset(statusNode, "msgFailed"))
		}
	}
	clipboard := js.Global().Get("navigator").Get("clipboard")
	if !clipboard.Truthy() || clipboard.Get("writeText").Type() != js.TypeFunction {
		fail()
		return
	}
	promise := clipboard.Call("writeText", domDataset(button, "answer"))
	var onDone, onFail js.Func
	onDone = js.FuncOf(func(js.Value, []js.Value) any {
		onDone.Release()
		onFail.Release()
		if statusNode.Truthy() {
			statusNode.Set("textContent", domDataset(statusNode, "msgCopied"))
		}
		return nil
	})
	onFail = js.FuncOf(func(js.Value, []js.Value) any {
		onDone.Release()
		onFail.Release()
		fail()
		return nil
	})
	promise.Call("then", onDone, onFail)
}

func handleAgentTaskListClick(event js.Value) {
	target := event.Get("target")
	if !target.Truthy() || target.Get("closest").Type() != js.TypeFunction {
		return
	}
	if back := target.Call("closest", "[data-agent-back-tasks]"); back.Truthy() {
		if storage, ok := browserSessionStorage(); ok {
			browserStorageSet(storage, agentTaskFocusStorageKey, domDataset(back, "agentBackTaskId"))
		}
		return
	}
	if retry := target.Call("closest", "button[data-agent-page-retry]"); retry.Truthy() {
		event.Call("preventDefault")
		js.Global().Get("location").Call("reload")
		return
	}
	tasks := target.Call("closest", ".agents-tasks")
	if !tasks.Truthy() {
		if retry := target.Call("closest", "button[data-agent-tasks-retry]"); retry.Truthy() {
			event.Call("preventDefault")
			go refreshAgentTasks()
		}
		return
	}
	if link := target.Call("closest", ".agents-task-row .agents-task-link"); link.Truthy() {
		event.Call("preventDefault")
		if storage, ok := browserSessionStorage(); ok {
			browserStorageSet(storage, agentTaskFilterStorageKey, domDataset(tasks, "selectedTaskFilter"))
			browserStorageSet(storage, agentTaskScrollStorageKey, strconv.FormatFloat(js.Global().Get("scrollY").Float(), 'f', -1, 64))
		}
		taskID := domDataset(link, "agentTaskRowLink")
		location := js.Global().Get("location")
		if target, ok := agentTaskNavigationTarget(location.Get("pathname").String(), location.Get("search").String(), taskID); ok {
			js.Global().Get("history").Call("pushState", js.Null(), "", target)
			renderAgentTasks(false, false)
			revealAgentTaskDetail()
			go refreshAgentTaskByID(taskID)
		}
		return
	}
	if retry := target.Call("closest", "button[data-agent-tasks-retry]"); retry.Truthy() {
		event.Call("preventDefault")
		go refreshAgentTasks()
		return
	}
	if filter := target.Call("closest", "button[data-agent-task-filter]"); filter.Truthy() {
		event.Call("preventDefault")
		selectAgentTaskFilter(tasks, filter, false)
		return
	}
	if more := target.Call("closest", "button[data-agent-task-more]"); more.Truthy() {
		event.Call("preventDefault")
		category := domDataset(tasks, "selectedTaskFilter")
		visible := 0
		rows := tasks.Call("querySelectorAll", `[data-task-category="`+category+`"]`)
		for index := 0; index < rows.Get("length").Int(); index++ {
			if !rows.Call("item", index).Get("hidden").Bool() {
				visible++
			}
		}
		showAgentTaskRows(tasks, category, visible+productui.AgentTaskFirstPage*2)
	}
}

func handleAgentTaskFilterKey(event js.Value) {
	target := event.Get("target")
	if !target.Truthy() || target.Get("closest").Type() != js.TypeFunction {
		return
	}
	filter := target.Call("closest", "button[data-agent-task-filter]")
	if !filter.Truthy() {
		return
	}
	key := event.Get("key").String()
	if key != "ArrowLeft" && key != "ArrowRight" && key != "Home" && key != "End" {
		return
	}
	tasks := filter.Call("closest", ".agents-tasks")
	if !tasks.Truthy() {
		return
	}
	buttons := tasks.Call("querySelectorAll", "button[data-agent-task-filter]")
	length := buttons.Get("length").Int()
	if length == 0 {
		return
	}
	current := 0
	for index := 0; index < length; index++ {
		if buttons.Call("item", index).Equal(filter) {
			current = index
			break
		}
	}
	next, ok := nextAgentTaskFilterIndex(current, length, key)
	if !ok {
		return
	}
	event.Call("preventDefault")
	selectAgentTaskFilter(tasks, buttons.Call("item", next), true)
}

func selectAgentTaskFilter(tasks, selected js.Value, focus bool) {
	category := domDataset(selected, "agentTaskFilter")
	tasks.Call("setAttribute", "data-selected-task-filter", category)
	if storage, ok := browserSessionStorage(); ok {
		browserStorageSet(storage, agentTaskFilterStorageKey, category)
	}
	buttons := tasks.Call("querySelectorAll", "button[data-agent-task-filter]")
	for index := 0; index < buttons.Get("length").Int(); index++ {
		button := buttons.Call("item", index)
		active := button.Equal(selected)
		button.Call("setAttribute", "aria-selected", map[bool]string{true: "true", false: "false"}[active])
		button.Call("setAttribute", "tabindex", map[bool]string{true: "0", false: "-1"}[active])
	}
	showAgentTaskRows(tasks, category, productui.AgentTaskFirstPage)
	empties := tasks.Call("querySelectorAll", "[data-agent-task-empty]")
	for index := 0; index < empties.Get("length").Int(); index++ {
		empty := empties.Call("item", index)
		empty.Set("hidden", domDataset(empty, "agentTaskEmpty") != category || tasks.Call("querySelectorAll", `[data-task-category="`+category+`"]`).Get("length").Int() > 0)
	}
	if focus {
		selected.Call("focus")
	}
}

func showAgentTaskRows(tasks js.Value, category string, limit int) {
	rows := tasks.Call("querySelectorAll", "[data-task-category]")
	matching := 0
	for index := 0; index < rows.Get("length").Int(); index++ {
		row := rows.Call("item", index)
		match := domDataset(row, "taskCategory") == category
		if match {
			matching++
		}
		row.Set("hidden", !match || matching > limit)
	}
	more := tasks.Call("querySelector", "button[data-agent-task-more]")
	if more.Truthy() {
		more.Set("hidden", matching <= limit)
	}
}

func currentAgentBinding() *agentServiceBinding {
	agentBrowser.Lock()
	defer agentBrowser.Unlock()
	return agentBrowser.binding
}

// saveAgentSetting sends the administrator's choice over the tunnel and
// reloads the document on success, so the server recomposes the agents
// projection (navigation, page and setting) from the stored value.
func saveAgentSetting(_ journeyclient.Config, enabled bool, done func(error)) {
	binding := currentAgentBinding()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), agentCallTimeout)
		defer cancel()
		err := binding.SetAgentsEnabled(ctx, enabled)
		ui.PostAsync(func() {
			done(err)
			if err == nil {
				js.Global().Get("location").Call("reload")
			}
		})
	}()
}

// handleAgentComposerClick starts a task using the button's explicit server
// policy. Quick answers auto-confirm the bounded read-only plan; long tasks
// remain awaiting plan confirmation.
func handleAgentComposerClick(event js.Value) {
	target := event.Get("target")
	if !target.Truthy() || target.Get("closest").Type() != js.TypeFunction {
		return
	}
	button := target.Call("closest", "button[data-agent-action]")
	if !button.Truthy() || button.Get("disabled").Bool() {
		return
	}
	document := js.Global().Get("document")
	input := document.Call("getElementById", "agents-composer-input")
	status := document.Call("getElementById", "agents-composer-status")
	if !input.Truthy() {
		return
	}
	setStatus := func(key string) {
		if status.Truthy() {
			status.Set("textContent", domDataset(status, "msg"+capitalizeASCII(key)))
		}
	}
	text := input.Get("value").String()
	references := agentRequestDocumentReferences(input.Call("closest", "form"))
	personaID := ""
	if selected := input.Call("closest", "form").Call("querySelector", `input[name="agent"]:checked`); selected.Truthy() {
		personaID = generalAgentSelection(selected.Get("value").String())
		if storage, ok := browserSessionStorage(); ok {
			browserStorageSet(storage, agentSelectionStorageKey, personaID)
		}
	}
	mode := agentv1.AgentStartMode_AGENT_START_MODE_LONG_TASK
	if domDataset(button, "agentAction") == "quick-answer" {
		mode = agentv1.AgentStartMode_AGENT_START_MODE_QUICK_ANSWER
	}
	if _, ok := newStartAgentTaskModeRequestWithSelection(text, mode, references, personaID); !ok {
		setStatus("empty")
		return
	}
	agentBrowser.Lock()
	if agentBrowser.busy {
		agentBrowser.Unlock()
		return
	}
	agentBrowser.busy = true
	binding := agentBrowser.binding
	agentBrowser.Unlock()
	button.Set("disabled", true)
	setStatus("working")
	optimisticID := "optimistic-agent-task"
	optimistic := productui.AgentTask{ID: optimisticID, Title: text, Goal: text, State: productui.AgentTaskRunning, CreatedAt: time.Now(), UpdatedAt: time.Now(), ResultPreview: domDataset(status, "msgWorking")}
	for _, reference := range references {
		optimistic.Documents = append(optimistic.Documents, productui.AgentTaskDocumentReference{DocumentID: reference.DocumentID, Label: reference.Label, SectionAnchor: reference.SectionAnchor})
	}
	agentBrowser.Lock()
	agentBrowser.tasks = append([]productui.AgentTask{optimistic}, agentBrowser.tasks...)
	agentBrowser.Unlock()
	renderAgentTasks(false, false)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), agentCallTimeout)
		defer cancel()
		task, err := binding.StartTaskModeWithSelection(ctx, text, mode, references, personaID)
		agentBrowser.Lock()
		agentBrowser.busy = false
		filtered := agentBrowser.tasks[:0]
		for _, existing := range agentBrowser.tasks {
			if existing.ID != optimisticID {
				filtered = append(filtered, existing)
			}
		}
		agentBrowser.tasks = filtered
		if err == nil {
			agentBrowser.tasks = reconcileAgentTasks(agentBrowser.tasks, append([]productui.AgentTask{task}, agentBrowser.tasks...))
			agentBrowser.startedTaskID = task.ID
		}
		agentBrowser.Unlock()
		ui.PostAsync(func() {
			if err != nil {
				button.Set("disabled", false)
				setAgentStartFailure(status, err)
				renderAgentTasks(false, false)
				return
			}
			setStatus("done")
			input.Set("value", "")
			clearAgentRequestDocuments(input.Call("closest", "form"))
			button.Set("disabled", false)
			renderAgentTasks(false, false)
			ensureStartedAgentTaskPolling(task)
		})
	}()
}

func handleAgentRetryClick(event js.Value) {
	target := event.Get("target")
	if !target.Truthy() || target.Get("closest").Type() != js.TypeFunction {
		return
	}
	button := target.Call("closest", "button[data-agent-retry]")
	if !button.Truthy() || button.Get("disabled").Bool() || button.Get("dataset").Get("agentAskAgain").Truthy() {
		return
	}
	prompt := domDataset(button, "agentRetryPrompt")
	personaID := generalAgentSelection(domDataset(button, "agentRetryPersona"))
	if _, ok := newStartAgentTaskRequest(prompt); !ok {
		return
	}
	references := agentTaskDocumentReferencesFromView(button.Call("closest", "[data-task-view]"))
	binding := currentAgentBinding()
	if binding == nil {
		return
	}
	agentBrowser.Lock()
	if agentBrowser.busy {
		agentBrowser.Unlock()
		return
	}
	agentBrowser.busy = true
	agentBrowser.Unlock()
	button.Set("disabled", true)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), agentCallTimeout)
		defer cancel()
		task, err := binding.StartTaskModeWithSelection(ctx, prompt, agentv1.AgentStartMode_AGENT_START_MODE_LONG_TASK, references, personaID)
		agentBrowser.Lock()
		agentBrowser.busy = false
		agentBrowser.Unlock()
		ui.PostAsync(func() {
			if err != nil {
				button.Set("disabled", false)
				return
			}
			location := js.Global().Get("location")
			query := js.Global().Get("URLSearchParams").New(location.Get("search").String())
			query.Call("set", "task", task.ID)
			location.Set("href", location.Get("pathname").String()+"?"+query.Call("toString").String()+"#agents-task-title")
		})
	}()
}

func handleAgentTaskClick(event js.Value) {
	target := event.Get("target")
	if !target.Truthy() || target.Get("closest").Type() != js.TypeFunction {
		return
	}
	button := target.Call("closest", "button[data-task-action]")
	if !button.Truthy() {
		return
	}
	taskID := domDataset(button, "taskId")
	versionText := domDataset(button, "taskVersion")
	var version uint64
	for _, r := range versionText {
		if r < '0' || r > '9' {
			return
		}
		version = version*10 + uint64(r-'0')
	}
	if version == 0 {
		return
	}
	var action agentv1.AgentTaskAction
	switch domDataset(button, "taskAction") {
	case "extend-budget":
		// AGENT2-017: the proto action enum has no extend value, so this one goes
		// over the JSON route.
		extendAgentTaskBudget(button, taskID)
		return
	case "confirm-plan":
		action = agentv1.AgentTaskAction_AGENT_TASK_ACTION_CONFIRM_PLAN
	case "pause":
		action = agentv1.AgentTaskAction_AGENT_TASK_ACTION_PAUSE
	case "resume":
		action = agentv1.AgentTaskAction_AGENT_TASK_ACTION_RESUME
	case "cancel":
		action = agentv1.AgentTaskAction_AGENT_TASK_ACTION_CANCEL
	default:
		return
	}
	binding := currentAgentBinding()
	if binding == nil {
		return
	}
	button.Set("disabled", true)
	status := js.Global().Get("document").Call("getElementById", "agents-task-control-status")
	setStatus := func(key string) {
		if status.Truthy() {
			status.Set("textContent", domDataset(status, "msg"+capitalizeASCII(key)))
		}
	}
	setStatus("working")
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), agentCallTimeout)
		defer cancel()
		_, err := binding.ControlTask(ctx, taskID, version, action)
		ui.PostAsync(func() {
			if err == nil {
				setStatus("done")
				location := js.Global().Get("location")
				query := js.Global().Get("URLSearchParams").New(location.Get("search").String())
				query.Call("set", "task", taskID)
				location.Set("href", location.Get("pathname").String()+"?"+query.Call("toString").String()+"#agents-task-title")
			} else {
				button.Set("disabled", false)
				setStatus(agentControlMessageKey(err))
			}
		})
	}()
}

func capitalizeASCII(word string) string {
	if word == "" || word[0] < 'a' || word[0] > 'z' {
		return word
	}
	return string(word[0]-'a'+'A') + word[1:]
}
