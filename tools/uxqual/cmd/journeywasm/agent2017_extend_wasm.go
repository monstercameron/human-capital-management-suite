//go:build js && wasm

package main

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"syscall/js"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// extendAgentTaskBudget adds the policy's allowance to a task paused at its
// budget ceiling. The request id is new for every click, and the server fences
// the call to the task's current budget revision, so a second click on the same
// page cannot add the allowance twice.
func extendAgentTaskBudget(button js.Value, taskID string) {
	cfg := agentAccessConfig()
	button.Set("disabled", true)
	status := js.Global().Get("document").Call("getElementById", "agents-task-control-status")
	setStatus := func(key string) {
		if status.Truthy() {
			status.Set("textContent", domDataset(status, "msg"+capitalizeASCII(key)))
		}
	}
	setStatus("working")
	requestID := "extend-" + strconv.FormatInt(time.Now().UnixNano(), 36) + "-" + strconv.FormatInt(int64(js.Global().Get("Math").Call("random").Float()*1e9), 36)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), agentAccessCallTimeout)
		defer cancel()
		err := agentAccessCall(ctx, http.DefaultClient, cfg, http.MethodPost, agentTaskAPI, agentTaskBudgetRequest{TaskID: taskID, RequestID: requestID}, nil)
		ui.PostAsync(func() {
			if err == nil {
				setStatus("done")
				location := js.Global().Get("location")
				query := js.Global().Get("URLSearchParams").New(location.Get("search").String())
				query.Call("set", "task", taskID)
				location.Set("href", location.Get("pathname").String()+"?"+query.Call("toString").String()+"#agents-task-title")
				return
			}
			button.Set("disabled", false)
			if errors.Is(err, errAgentAccessDenied) {
				setStatus("denied")
				return
			}
			setStatus("failed")
		})
	}()
}
