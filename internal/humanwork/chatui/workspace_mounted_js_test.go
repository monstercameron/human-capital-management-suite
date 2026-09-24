//go:build js && wasm

package chatui

import (
	"syscall/js"
	"testing"
)

// The document-wide field-sync observer skips mutation batches when no chat
// workspace is mounted (a Docs page after a chat visit) and works as before
// while one is.
func TestChatWorkspaceMountedGatesFieldSync(t *testing.T) {
	var found js.Value = js.Null()
	query := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) > 0 && args[0].String() == ".chat-workspace" {
			return found
		}
		return js.Null()
	})
	defer query.Release()
	doc := js.Global().Get("Object").New()
	doc.Set("querySelector", query)
	if chatWorkspaceMounted(doc) {
		t.Fatal("no workspace in the document, but the observer would still walk every mutation")
	}
	found = js.Global().Get("Object").New()
	if !chatWorkspaceMounted(doc) {
		t.Fatal("a mounted workspace must keep field sync running")
	}
	if chatWorkspaceMounted(js.Undefined()) {
		t.Fatal("a missing document has no workspace")
	}
}
