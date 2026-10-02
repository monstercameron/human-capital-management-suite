//go:build !js || !wasm

package chatui

import "github.com/monstercameron/GoWebComponents/v5/ui"

// There is no page to read outside the browser.
func chatcmd002FieldValues(string) map[string]string        { return nil }
func chatcmd002ClearField(string, string)                   {}
func chatcmd002KeyAction(ui.KeyboardEvent) (string, string) { return "", "" }
