//go:build js && wasm

package main

import (
	"syscall/js"

	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// openChatPersonFragment opens the details a document's person chip links
// to ("/workspace/app/chat#person=<subject>"): the same pane, the same
// directory read and the same authorization as clicking a name in chat.
func openChatPersonFragment(cfg journeyclient.Config) {
	location := js.Global().Get("location")
	if !location.Truthy() {
		return
	}
	hash := location.Get("hash").String()
	id, ok := chatPersonFragmentSubject(hash)
	if !ok {
		chatPersonFragments.claim("")
		return
	}
	active := chatBrowser.config(cfg)
	if !chatPersonFragments.claim(active.Tenant + "\x00" + active.Subject + "\x00" + hash) {
		return
	}
	openChatPersonRecorded(active, id, chatHistory.replace)
}
