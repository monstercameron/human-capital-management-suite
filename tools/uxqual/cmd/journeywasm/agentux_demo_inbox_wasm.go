//go:build js && wasm

package main

import (
	"context"
	"net/http"
	"sync"
	"syscall/js"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/agentdemo"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// The mount is supplied by the owner page. State lasts only for this client
// session; no browser storage, timers or focus events drive a support run.
var agentuxDemoInboxBrowser struct {
	sync.Mutex
	config          journeyclient.Config
	bound, busy     bool
	input           agentdemo.Email
	submit, observe js.Func
	observer        js.Value
}

func configureAgentUXDemoInbox(cfg journeyclient.Config) {
	agentuxDemoInboxBrowser.Lock()
	agentuxDemoInboxBrowser.config = cfg
	bind := !agentuxDemoInboxBrowser.bound
	agentuxDemoInboxBrowser.bound = true
	agentuxDemoInboxBrowser.Unlock()
	if bind {
		document := js.Global().Get("document")
		agentuxDemoInboxBrowser.submit = js.FuncOf(func(_ js.Value, args []js.Value) any {
			if len(args) > 0 {
				agentuxDemoInboxSubmit(args[0])
			}
			return nil
		})
		document.Call("addEventListener", "submit", agentuxDemoInboxBrowser.submit)
		agentuxDemoInboxBrowser.observe = js.FuncOf(func(js.Value, []js.Value) any { agentuxDemoInboxFindMount(); return nil })
		agentuxDemoInboxBrowser.observer = js.Global().Get("MutationObserver").New(agentuxDemoInboxBrowser.observe)
		agentuxDemoInboxBrowser.observer.Call("observe", document.Get("body"), map[string]any{"childList": true, "subtree": true})
	}
	agentuxDemoInboxFindMount()
}

func agentuxDemoInboxFindMount() {
	mount := js.Global().Get("document").Call("getElementById", "agentux-demo-support-inbox")
	if !mount.Truthy() || mount.Call("querySelector", "[data-support-demo-form]").Truthy() {
		return
	}
	markup, err := ui.RenderToString(agentuxDemoInboxControl(productui.ResolveProductLocale(domAttribute(mount, "data-locale"))))
	if err == nil {
		mount.Set("innerHTML", markup)
	}
}

func agentuxDemoInboxSubmit(event js.Value) {
	form := event.Get("target").Call("closest", "[data-support-demo-form]")
	if !form.Truthy() {
		return
	}
	event.Call("preventDefault")
	mount := form.Call("closest", ".support-demo-inbox")
	if !mount.Truthy() {
		return
	}
	if !form.Call("reportValidity").Bool() {
		return
	}
	value := func(field string) string {
		return form.Call("querySelector", "[data-support-demo-field="+field+"]").Get("value").String()
	}
	email := agentdemo.Email{From: value("from"), Subject: value("subject"), Body: value("body")}
	agentuxDemoInboxBrowser.Lock()
	if agentuxDemoInboxBrowser.busy {
		agentuxDemoInboxBrowser.Unlock()
		return
	}
	prior := agentuxDemoInboxBrowser.input
	if prior.IdempotencyKey != "" && prior.From == email.From && prior.Subject == email.Subject && prior.Body == email.Body {
		email.IdempotencyKey = prior.IdempotencyKey
	} else {
		email.IdempotencyKey = js.Global().Get("crypto").Call("randomUUID").String()
	}
	agentuxDemoInboxBrowser.input = email
	agentuxDemoInboxBrowser.busy = true
	cfg := agentuxDemoInboxBrowser.config
	agentuxDemoInboxBrowser.Unlock()
	button := form.Call("querySelector", "button[type=submit]")
	button.Set("disabled", true)
	form.Call("setAttribute", "aria-busy", "true")
	status := mount.Call("querySelector", "[data-support-demo-status]")
	locale := domAttribute(mount, "data-locale")
	status.Set("textContent", agentuxDemoInboxText(locale, "sending"))
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_, err := agentuxDemoInboxRequest(ctx, http.DefaultClient, personaChatHTTPConfig(cfg), email)
		ui.PostAsync(func() {
			agentuxDemoInboxBrowser.Lock()
			agentuxDemoInboxBrowser.busy = false
			// Keep the immutable key while the visible input is unchanged,
			// including when a successful response is submitted again.
			agentuxDemoInboxBrowser.Unlock()
			if !form.Get("isConnected").Bool() {
				return
			}
			button.Set("disabled", false)
			form.Call("removeAttribute", "aria-busy")
			key := "queued"
			if err != nil {
				key = agentuxDemoInboxErrorKey(err)
			}
			status.Set("textContent", agentuxDemoInboxText(locale, key))
			if err != nil {
				for _, field := range []string{"from", "subject", "body"} {
					form.Call("querySelector", "[data-support-demo-field="+field+"]").Call("setAttribute", "aria-invalid", "true")
				}
				status.Set("role", "alert")
			} else {
				for _, field := range []string{"from", "subject", "body"} {
					form.Call("querySelector", "[data-support-demo-field="+field+"]").Call("removeAttribute", "aria-invalid")
				}
				status.Set("role", "status")
			}
		})
	}()
}
