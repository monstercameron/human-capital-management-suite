//go:build !js || !wasm

package chatui

import "github.com/monstercameron/GoWebComponents/v5/ui"

// There is no page to read outside the browser.
func issue3929ActiveID() string { return "" }

func issue3929Composing(ui.KeyboardEvent) bool { return false }
