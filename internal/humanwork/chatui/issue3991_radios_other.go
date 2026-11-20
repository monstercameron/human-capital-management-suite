//go:build !js || !wasm

package chatui

import "github.com/monstercameron/GoWebComponents/v5/ui"

// There is no page to read outside the browser.
func chatcmd003RadioTarget(ui.KeyboardEvent) (string, string, []string, bool) {
	return "", "", nil, false
}
func chatcmd003RadioFocus(string) {}
