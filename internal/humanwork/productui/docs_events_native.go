//go:build !(js && wasm)

package productui

import "github.com/monstercameron/GoWebComponents/v5/ui"

// docsEventAction has no DOM to read outside the browser; server-rendered
// Docs links and forms work without it.
func docsEventAction(ui.Event) (action, id string, plain bool) { return "", "", false }

func setDocsSearchValue(string) {}

func setDocsFieldValue(string, string) {}
func docsSubmitForm(string)            {}
func docsModifierHeld(ui.Event) bool   { return false }

func docsListenEscape(func()) func() { return func() {} }
