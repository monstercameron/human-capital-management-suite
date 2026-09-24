//go:build js && wasm

package chatui

import (
	"syscall/js"
)

// browserDraftPersistence deliberately does not persist draft text. The
// session's chat model retains drafts across in-app navigation, and its
// sidebar projection restores them after a reload. Browser storage is reserved
// for bounded, non-sensitive presentation hints by the WEB-031 boundary.
type browserDraftPersistence struct{}

func newDraftPersistence() draftPersistence                    { return browserDraftPersistence{} }
func (browserDraftPersistence) load(string) map[string]string  { return nil }
func (browserDraftPersistence) save(string, map[string]string) {}
func (browserDraftPersistence) clear()                         {}

// installDraftLogoutListener clears the tab copy before the shell follows its
// normal logout link. The browserDrafts state-error path handles server-side
// revocation while the page remains open.
func installDraftLogoutListener(clearDrafts func()) func() {
	doc := js.Global().Get("document")
	if !doc.Truthy() {
		return func() {}
	}
	listener := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 0 {
			return nil
		}
		target := args[0].Get("target")
		if !target.Truthy() || target.Get("closest").Type() != js.TypeFunction {
			return nil
		}
		if target.Call("closest", "a.jn-logout,a[href$='/logout']").Truthy() {
			clearDrafts()
		}
		return nil
	})
	doc.Call("addEventListener", "click", listener, true)
	return func() {
		doc.Call("removeEventListener", "click", listener, true)
		listener.Release()
	}
}
