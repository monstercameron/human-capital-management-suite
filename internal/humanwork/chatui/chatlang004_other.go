//go:build !js || !wasm

package chatui

// The native build has no page to press the gear on and no network client.
func chatlangOpenSettings() {}

func chatlangCorrect(_, _ string, _ uint64, _ string, done func(error)) { done(nil) }
