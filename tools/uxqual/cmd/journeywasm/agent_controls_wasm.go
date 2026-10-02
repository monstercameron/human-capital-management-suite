//go:build js && wasm

package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"syscall/js"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

var agentControlsBrowser struct {
	sync.Mutex
	config         journeyclient.Config
	mount          js.Value
	observer       js.Value
	click, observe js.Func
	bound, busy    bool
	locale         string
	snapshot       productui.AgentControlsSnapshot
	historyFilter  productui.AgentRunHistoryFilter
}

func configureAgentControls(cfg journeyclient.Config) {
	agentControlsBrowser.Lock()
	changed := agentControlsBrowser.config.Bearer != cfg.Bearer
	previous := agentControlsBrowser.mount
	if changed {
		agentControlsBrowser.mount = js.Undefined()
		agentControlsBrowser.snapshot = productui.AgentControlsSnapshot{}
		agentControlsBrowser.busy = false
	}
	agentControlsBrowser.config = cfg
	bind := !agentControlsBrowser.bound
	agentControlsBrowser.bound = true
	agentControlsBrowser.Unlock()
	if changed && previous.Truthy() {
		renderAgentControlsMount(previous, productui.AgentControlsSnapshot{}, "loading")
	}
	if bind {
		agentControlsBrowser.click = js.FuncOf(func(_ js.Value, args []js.Value) any {
			if len(args) > 0 {
				handleAgentOwnerClick(args[0])
			}
			return nil
		})
		js.Global().Get("document").Call("addEventListener", "click", agentControlsBrowser.click)
		change := js.FuncOf(func(_ js.Value, args []js.Value) any {
			if len(args) > 0 {
				handleAgentHistoryChange(args[0])
			}
			return nil
		})
		js.Global().Get("document").Call("addEventListener", "change", change)
		agentControlsBrowser.observe = js.FuncOf(func(_ js.Value, _ []js.Value) any { findAgentControlsMount(); return nil })
		agentControlsBrowser.observer = js.Global().Get("MutationObserver").New(agentControlsBrowser.observe)
		agentControlsBrowser.observer.Call("observe", js.Global().Get("document").Get("body"), map[string]any{"childList": true, "subtree": true, "attributes": true, "attributeFilter": []any{"data-locale"}})
	}
	findAgentControlsMount()
}

func findAgentControlsMount() {
	if !agentOperationsRoute(js.Global().Get("location").Get("pathname").String()) {
		agentControlsBrowser.Lock()
		agentControlsBrowser.mount = js.Undefined()
		agentControlsBrowser.Unlock()
		return
	}
	mount := js.Global().Get("document").Call("getElementById", "agent-controls")
	if !mount.Truthy() {
		return
	}
	agentControlsBrowser.Lock()
	locale := domAttribute(mount, "data-locale")
	if agentControlsBrowser.mount.Truthy() && agentControlsBrowser.mount.Equal(mount) && agentControlsBrowser.locale == locale {
		agentControlsBrowser.Unlock()
		return
	}
	agentControlsBrowser.mount = mount
	agentControlsBrowser.locale = locale
	agentControlsBrowser.snapshot = productui.AgentControlsSnapshot{}
	agentControlsBrowser.Unlock()
	go runAgentOwnerRequest(mount, "", nil, "done")
}

func renderAgentControlsMount(mount js.Value, snapshot productui.AgentControlsSnapshot, message string) {
	if !mount.Truthy() || !mount.Get("isConnected").Bool() {
		return
	}
	locale := productui.ResolveProductLocale(domAttribute(mount, "data-locale")).WithTimeZone(agentViewerTimeZone())
	markup, err := ui.RenderToString(productui.RenderAgentControls(locale, snapshot, message))
	if err == nil {
		mount.Set("innerHTML", markup)
		renderAgentHistoryMount(mount, snapshot)
	}
}

func runAgentOwnerRequest(mount js.Value, action string, input any, message string) {
	agentControlsBrowser.Lock()
	cfg := agentControlsBrowser.config
	agentControlsBrowser.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	reply, err := agentControlsRequest(ctx, http.DefaultClient, personaChatHTTPConfig(cfg), action, input)
	ui.PostAsync(func() {
		personas := cfg.PersonaAdminSnapshot
		personaAdminBrowser.Lock()
		if personaAdminBrowser.tenant == cfg.Tenant && personaAdminBrowser.subject == cfg.Subject && personaAdminBrowser.bearer == cfg.Bearer && personaAdminBrowser.snapshot != nil {
			current := *personaAdminBrowser.snapshot
			personas = &current
		}
		personaAdminBrowser.Unlock()
		agentControlsBrowser.Lock()
		if !agentControlsBrowser.mount.Equal(mount) || agentControlsBrowser.config.Bearer != cfg.Bearer {
			agentControlsBrowser.Unlock()
			return
		}
		agentControlsBrowser.busy = false
		if err == nil {
			agentControlsBrowser.snapshot = reply.Snapshot
			if personas != nil {
				agentControlsBrowser.snapshot.Agents = personas.Personas
				agentControlsBrowser.snapshot.AllowedCommands = personas.AllowedCommands
			}
		} else if errors.Is(err, errAgentControlsDenied) {
			message = "access_denied"
			agentControlsBrowser.snapshot = productui.AgentControlsSnapshot{}
		} else if errors.Is(err, context.DeadlineExceeded) {
			message = "timed_out"
			agentControlsBrowser.snapshot = productui.AgentControlsSnapshot{}
		} else if errors.Is(err, errAgentControlsConflict) {
			message = "conflict"
		} else if errors.Is(err, errAgentControlsInvalid) {
			message = "error"
		} else {
			message = "error"
			agentControlsBrowser.snapshot = productui.AgentControlsSnapshot{}
		}
		snapshot := agentControlsBrowser.snapshot
		agentControlsBrowser.Unlock()
		values := map[string]string{}
		fields := mount.Call("querySelectorAll", "input[id],select[id]")
		for i := 0; i < fields.Length(); i++ {
			field := fields.Index(i)
			values[field.Get("id").String()] = field.Get("value").String()
		}
		renderAgentControlsMount(mount, snapshot, message)
		for id, value := range values {
			field := js.Global().Get("document").Call("getElementById", id)
			if field.Truthy() {
				field.Set("value", value)
			}
		}
		if action != "" {
			status := js.Global().Get("document").Call("getElementById", "agent-controls-status")
			if status.Truthy() {
				status.Call("focus")
			}
		}
		if err == nil && len(reply.Export) > 0 {
			downloadAgentOwnerExport(reply.Export)
		}
	})
}

func handleAgentOwnerClick(event js.Value) {
	target := event.Get("target")
	if !target.Truthy() || target.Get("closest").Type() != js.TypeFunction {
		return
	}
	if pager := target.Call("closest", "[data-agent-history-page]"); pager.Truthy() {
		event.Call("preventDefault")
		page, _ := strconv.Atoi(domDataset(pager, "agentHistoryPage"))
		agentControlsBrowser.Lock()
		agentControlsBrowser.historyFilter.Page = page
		mount, snapshot := agentControlsBrowser.mount, agentControlsBrowser.snapshot
		agentControlsBrowser.Unlock()
		renderAgentHistoryMount(mount, snapshot)
		return
	}
	button := target.Call("closest", "button[data-owner-action],button[data-owner-draft],button[data-owner-refresh]")
	if !button.Truthy() || button.Get("disabled").Bool() {
		return
	}
	event.Call("preventDefault")
	dataset := button.Get("dataset")
	if confirmation := dataset.Get("ownerConfirm"); confirmation.Type() == js.TypeString && confirmation.String() != "" && !js.Global().Call("confirm", confirmation.String()).Bool() {
		return
	}
	agentControlsBrowser.Lock()
	if agentControlsBrowser.busy {
		agentControlsBrowser.Unlock()
		return
	}
	agentControlsBrowser.busy = true
	mount := agentControlsBrowser.mount
	agentControlsBrowser.Unlock()
	button.Set("disabled", true)
	button.Call("setAttribute", "aria-busy", "true")
	if busyLabel := dataset.Get("busyLabel"); busyLabel.Type() == js.TypeString {
		button.Set("textContent", busyLabel.String())
	}
	if dataset.Get("ownerRefresh").Type() == js.TypeString {
		go runAgentOwnerRequest(mount, "", nil, "done")
		return
	}
	var input any
	action := "control"
	if draft := dataset.Get("ownerDraft"); draft.Type() == js.TypeString {
		action = draft.String()
		value := func(key string) string {
			return js.Global().Get("document").Call("getElementById", "agent-schedule-"+key).Get("value").String()
		}
		revision, err := strconv.ParseUint(value("revision"), 10, 64)
		if value("revision") == "" {
			revision, err = 0, nil
		}
		if err != nil {
			agentControlsBrowser.Lock()
			agentControlsBrowser.busy = false
			agentControlsBrowser.Unlock()
			button.Set("disabled", false)
			reportAgentControlsInputError("agent-schedule-revision")
			return
		}
		budget := map[string]uint64{}
		for _, field := range []struct{ key, name string }{{"max_cost", "MaxCostMicros"}, {"max_input", "MaxInputTokens"}, {"max_output", "MaxOutputTokens"}} {
			amount, parseErr := strconv.ParseUint(value(field.key), 10, 64)
			if parseErr != nil {
				agentControlsBrowser.Lock()
				agentControlsBrowser.busy = false
				agentControlsBrowser.Unlock()
				button.Set("disabled", false)
				reportAgentControlsInputError("agent-schedule-" + field.key)
				return
			}
			budget[field.name] = amount
			js.Global().Get("document").Call("getElementById", "agent-schedule-"+field.key).Call("removeAttribute", "aria-invalid")
		}
		budgetJSON, _ := json.Marshal(budget)
		input = productui.AgentScheduleDraft{ID: value("id"), ExpectedRevision: revision, Version: value("version"), Installation: value("installation"), Recurrence: value("recurrence"), Zone: value("zone"), Calendar: value("calendar"), Destination: value("destination"), Budget: string(budgetJSON), Misfire: value("misfire"), Overlap: value("overlap"), DST: value("dst")}
	} else {
		revision, err := strconv.ParseUint(dataset.Get("ownerRevision").String(), 10, 64)
		if err != nil {
			agentControlsBrowser.Lock()
			agentControlsBrowser.busy = false
			agentControlsBrowser.Unlock()
			button.Set("disabled", false)
			return
		}
		input = productui.AgentControlsCommand{Kind: dataset.Get("ownerKind").String(), ID: dataset.Get("ownerId").String(), ExpectedRevision: revision, Action: dataset.Get("ownerAction").String(), IdempotencyKey: js.Global().Get("crypto").Call("randomUUID").String(), Reason: js.Global().Get("document").Call("getElementById", "agent-controls-reason").Get("value").String(), IncidentID: js.Global().Get("document").Call("getElementById", "agent-controls-incident").Get("value").String()}
		if command, ok := input.(productui.AgentControlsCommand); ok && command.Kind == "schedule" && command.Action == "skip" {
			occurrence := js.Global().Get("document").Call("getElementById", "agent-occurrence-"+command.ID)
			if occurrence.Truthy() {
				command.Occurrence = occurrence.Get("value").String()
			}
			input = command
		}
	}
	go runAgentOwnerRequest(mount, action, input, "action_done")
}

func reportAgentControlsInputError(id string) {
	document := js.Global().Get("document")
	field := document.Call("getElementById", id)
	status := document.Call("getElementById", "agent-controls-status")
	if status.Truthy() {
		status.Set("textContent", domDataset(status, "msgInvalid"))
	}
	if field.Truthy() {
		field.Call("setAttribute", "aria-invalid", "true")
		field.Call("focus")
	}
}

func downloadAgentOwnerExport(content []byte) {
	blob := js.Global().Get("Blob").New([]any{string(content)}, map[string]any{"type": "application/json"})
	url := js.Global().Get("URL").Call("createObjectURL", blob)
	anchor := js.Global().Get("document").Call("createElement", "a")
	anchor.Set("href", url)
	anchor.Set("download", "agent-records.json")
	anchor.Call("click")
	js.Global().Get("URL").Call("revokeObjectURL", url)
}

func handleAgentHistoryChange(event js.Value) {
	field := event.Get("target")
	if !field.Truthy() || field.Get("dataset").Get("agentHistoryFilter").Type() != js.TypeString {
		return
	}
	agentControlsBrowser.Lock()
	filter := agentControlsBrowser.historyFilter
	switch domDataset(field, "agentHistoryFilter") {
	case "agent":
		filter.Agent = field.Get("value").String()
	case "version":
		filter.Version = field.Get("value").String()
	case "outcome":
		filter.Outcome = field.Get("value").String()
	}
	filter.Page = 1
	agentControlsBrowser.historyFilter = filter
	mount, snapshot := agentControlsBrowser.mount, agentControlsBrowser.snapshot
	agentControlsBrowser.Unlock()
	renderAgentHistoryMount(mount, snapshot)
}
func renderAgentHistoryMount(mount js.Value, snapshot productui.AgentControlsSnapshot) {
	history := mount.Call("querySelector", "#agent-run-history")
	if !history.Truthy() {
		return
	}
	agentControlsBrowser.Lock()
	filter := agentControlsBrowser.historyFilter
	agentControlsBrowser.Unlock()
	finished := []productui.AgentControlRun{}
	for _, run := range snapshot.Runs {
		switch strings.ToUpper(run.State) {
		case "COMPLETED", "FAILED", "CANCELLED", "CANCELED", "EXPIRED", "STOPPED_RESPONDING":
			finished = append(finished, run)
		}
	}
	locale := productui.ResolveProductLocale(domAttribute(mount, "data-locale")).WithTimeZone(agentViewerTimeZone())
	markup, err := ui.RenderToString(productui.RenderAgentRunHistory(locale, finished, filter))
	if err == nil {
		history.Set("outerHTML", markup)
	}
}
