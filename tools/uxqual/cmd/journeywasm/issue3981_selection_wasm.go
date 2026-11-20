//go:build js && wasm

package main

import (
	"sync"
	"syscall/js"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

var agentSelectionListeners sync.Once

// reconcileAgentSelection (#3981) redraws the task region when the task it shows
// is not the task the address names, after Close, Back or Forward.
func reconcileAgentSelection() {
	tasks := js.Global().Get("document").Call("querySelector", ".agents-tasks")
	if !tasks.Truthy() {
		return
	}
	search := js.Global().Get("location").Get("search").String()
	if !agentSelectionOutOfSync(domDataset(tasks, "selectedTaskId"), search) {
		return
	}
	renderAgentTasks(false, false)
	if taskID := selectedAgentTaskID(search); taskID != "" {
		go refreshAgentTaskByID(taskID)
		return
	}
	stopAgentTaskPolling()
}

// agentFocusReturn is the task whose row takes keyboard focus once Close has
// removed its detail. It is held in memory: the browser-state policy does not
// allow this record in session storage, which is why the earlier storage
// hand-off never moved focus.
var agentFocusReturn struct {
	sync.Mutex
	taskID string
}

func setAgentFocusReturn(taskID string) {
	agentFocusReturn.Lock()
	agentFocusReturn.taskID = taskID
	agentFocusReturn.Unlock()
}

// focusAgentReturnRow gives focus back to the row of the task that was closed,
// or to the first row of the list when that row is no longer there.
func focusAgentReturnRow(document js.Value) {
	agentFocusReturn.Lock()
	taskID := agentFocusReturn.taskID
	agentFocusReturn.Unlock()
	if taskID == "" {
		return
	}
	tasks := document.Call("querySelector", ".agents-tasks")
	if !tasks.Truthy() || tasks.Call("querySelector", "#agents-task-title").Truthy() {
		return
	}
	row := tasks.Call("querySelector", `[data-agent-task-row-link="`+taskID+`"]`)
	if !row.Truthy() || row.Get("hidden").Bool() {
		row = tasks.Call("querySelector", `.agents-task-link[data-agent-task-row-link]:not([hidden])`)
	}
	if row.Truthy() && !row.Call("closest", "[hidden]").Truthy() {
		row.Call("focus")
		setAgentFocusReturn("")
	}
}

// scheduleAgentSelectionReconcile looks again at a few moments after a
// navigation, because the router commits the address a little later.
func scheduleAgentSelectionReconcile() {
	for _, delay := range agentSelectionReconcileDelays {
		time.AfterFunc(time.Duration(delay)*time.Millisecond, func() {
			ui.PostAsync(reconcileAgentSelection)
		})
	}
}

// installAgentSelectionListeners reconciles on browser Back and Forward.
func installAgentSelectionListeners() {
	agentSelectionListeners.Do(func() {
		js.Global().Call("addEventListener", "popstate", js.FuncOf(func(js.Value, []js.Value) any {
			scheduleAgentSelectionReconcile()
			return nil
		}))
	})
}
