//go:build js && wasm

package main

import (
	"context"
	"sync"
	"syscall/js"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	agentv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/agent/v1"
	"google.golang.org/grpc"

	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

var agentBrowser struct {
	sync.Mutex
	binding   *agentServiceBinding
	installed bool
	busy      bool
}

const agentCallTimeout = 30 * time.Second

// configureAgentService binds the Agents page writes to the workspace gRPC
// tunnel and installs the composer's delegated click handler once.
func configureAgentService(conn grpc.ClientConnInterface, cfg journeyclient.Config) {
	configureAgentControls(cfg)
	configureAgentRolloutPortable(cfg)
	agentBrowser.Lock()
	agentBrowser.binding = newAgentServiceBinding(cfg, conn)
	agentBrowser.busy = false
	install := !agentBrowser.installed
	agentBrowser.installed = true
	agentBrowser.Unlock()
	if install {
		js.Global().Get("document").Call("addEventListener", "click", js.FuncOf(func(_ js.Value, args []js.Value) any {
			if len(args) > 0 {
				handleAgentComposerClick(args[0])
				handleAgentTaskClick(args[0])
			}
			return nil
		}))
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
			status.Set("textContent", status.Get("dataset").Get("msg"+capitalizeASCII(key)).String())
		}
	}
	text := input.Get("value").String()
	if _, ok := newStartAgentTaskRequest(text); !ok {
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
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), agentCallTimeout)
		defer cancel()
		mode := agentv1.AgentStartMode_AGENT_START_MODE_LONG_TASK
		if button.Get("dataset").Get("agentAction").String() == "quick-answer" {
			mode = agentv1.AgentStartMode_AGENT_START_MODE_QUICK_ANSWER
		}
		taskID, err := binding.StartTaskMode(ctx, text, mode)
		agentBrowser.Lock()
		agentBrowser.busy = false
		agentBrowser.Unlock()
		ui.PostAsync(func() {
			if err != nil {
				button.Set("disabled", false)
				setStatus(agentStartMessageKey(err))
				return
			}
			setStatus("done")
			location := js.Global().Get("location")
			query := js.Global().Get("URLSearchParams").New(location.Get("search").String())
			query.Call("set", "task", taskID)
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
	taskID := button.Get("dataset").Get("taskId").String()
	versionText := button.Get("dataset").Get("taskVersion").String()
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
	switch button.Get("dataset").Get("taskAction").String() {
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
			status.Set("textContent", status.Get("dataset").Get("msg"+capitalizeASCII(key)).String())
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
