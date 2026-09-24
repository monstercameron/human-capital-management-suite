//go:build !(js && wasm)

package chatui

import "github.com/monstercameron/GoWebComponents/v5/ui"

func rememberChatDialogTrigger(ui.Event, string) {}
func focusChatDialog()                           {}
func trapChatDialogFocus(ui.Event) bool          { return false }
func restoreChatDialogFocus()                    {}
