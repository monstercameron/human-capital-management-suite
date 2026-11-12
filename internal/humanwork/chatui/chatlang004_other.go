//go:build !js || !wasm

package chatui

import "github.com/monstercameron/GoWebComponents/v5/ui"

// The native build has no page to press the gear on and no network client.
func chatlangOpenSettings() {}

func chatlangCorrect(_, _ string, _ uint64, _ string, done func(error)) { done(nil) }

func focusChatLanguageEditor(string) {}

func focusMessageAfterEdit(string) {}

func chatlangEventInEditor(ui.KeyboardEvent) bool { return false }
