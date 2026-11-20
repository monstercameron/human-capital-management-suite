//go:build !(js && wasm)

package chatui

import "github.com/monstercameron/GoWebComponents/v5/ui"

// The sheet is drawn by the browser client only.
func issue900005OpenShortcuts(ui.Event, string) {}
