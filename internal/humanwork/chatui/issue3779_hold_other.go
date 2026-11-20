//go:build !js || !wasm

package chatui

import "github.com/monstercameron/GoWebComponents/v5/ui"

// Without a browser there is no server to ask: native rendering tests draw the
// page from a model and never run its effects.
func chatHoldCall(string, any, func(ChatHoldData, int, error)) func() { return nil }
func chatHoldReadForm(ui.Event) map[string]string                     { return map[string]string{} }
