//go:build js && wasm

package main

import (
	"sync"
	"syscall/js"
)

var agentDraftListeners sync.Once

func agentDraftIdentity() (tenant, subject string) {
	agentBrowser.Lock()
	defer agentBrowser.Unlock()
	return agentBrowser.cfg.Tenant, agentBrowser.cfg.Subject
}

// rememberAgentComposerDraft records what the composer holds now (#3975).
func rememberAgentComposerDraft(input js.Value) {
	tenant, subject := agentDraftIdentity()
	agentDraftStore.Remember(tenant, subject, input.Get("value").String())
}

// restoreAgentComposerDraft puts an unsent question back into an empty composer
// after the page was redrawn, announcing it politely. It never replaces text.
func restoreAgentComposerDraft(input js.Value) {
	if !input.Truthy() {
		return
	}
	tenant, subject := agentDraftIdentity()
	text, ok := agentDraftStore.Restore(tenant, subject, input.Get("value").String())
	if !ok {
		return
	}
	input.Set("value", text)
	if status := js.Global().Get("document").Call("getElementById", "agents-composer-status"); status.Truthy() {
		status.Set("textContent", domDataset(status, "msgDraftRestored"))
	}
}

// installAgentDraftListeners remembers every edit of the composer. Programmatic
// changes (a follow-up, a retry) record themselves.
func installAgentDraftListeners() {
	agentDraftListeners.Do(func() {
		js.Global().Get("document").Call("addEventListener", "input", js.FuncOf(func(_ js.Value, args []js.Value) any {
			if len(args) == 0 {
				return nil
			}
			if target := args[0].Get("target"); target.Truthy() && target.Get("id").String() == "agents-composer-input" {
				rememberAgentComposerDraft(target)
			}
			return nil
		}))
	})
}
