//go:build !js || !wasm

package chatui

import "github.com/monstercameron/GoWebComponents/v5/ui"

func toggleEmojiPicker(string)                   {}
func insertComposerEmoji(string, string)         {}
func closeEmojiPickers(bool)                     {}
func handleEmojiPickerKey(ui.KeyboardEvent) bool { return false }
