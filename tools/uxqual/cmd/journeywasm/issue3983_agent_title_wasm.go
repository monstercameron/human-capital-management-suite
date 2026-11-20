//go:build js && wasm

package main

import "syscall/js"

// syncAgentTaskTabTitle sets the tab title from the Agents page's own title and
// the task now open, if any, so a task's question replaces the previous one and
// is removed when no task is open.
func syncAgentTaskTabTitle(document js.Value) {
	root := document.Get("documentElement")
	read := func(name string) string {
		if value := root.Call("getAttribute", name); value.Type() == js.TypeString {
			return value.String()
		}
		return ""
	}
	prefix := ""
	if detail := document.Call("querySelector", "[data-task-view][data-task-request]"); detail.Truthy() {
		prefix = agentTaskTitlePrefix(domDataset(detail, "taskRequest"))
	}
	current := document.Get("title").String()
	next, base := agentTaskTabTitle(current, read("data-hcm-agent-title-last"), read("data-hcm-agent-title-base"), prefix)
	if current != next {
		document.Set("title", next)
	}
	root.Call("setAttribute", "data-hcm-agent-title-base", base)
	root.Call("setAttribute", "data-hcm-agent-title-last", next)
}
