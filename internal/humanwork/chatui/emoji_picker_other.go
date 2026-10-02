//go:build !js || !wasm

package chatui

import "github.com/monstercameron/GoWebComponents/v5/ui"

// Native builds have no DOM: the picker's state machine, view and search run
// (and are tested) here, and the browser-only parts below do nothing.

func insertComposerEmoji(string, string, bool)             {}
func handleEmojiPickerKey(ui.KeyboardEvent) bool           { return false }
func chatEmojiClick(ui.Event, string, string, string) bool { return false }
func chatEmojiEnvironment() (touch, sheet bool)            { return false, false }
func chatEmojiPlatformDrawsFlags() bool                    { return true }
func chatEmojiStartLoad(string)                            {}
func bindChatEmoji() func()                                { return func() {} }
func chatEmojiAfterRender()                                {}
