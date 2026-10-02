//go:build !js || !wasm

package chatui

import "github.com/monstercameron/GoWebComponents/v5/ui"

func rememberChatLayerOpener(ui.MouseEvent)     {}
func restoreChatLayerFocus(...string)           {}
func syncChatAnchoredLayers()                   {}
func focusChatAgentsSection()                   {}
func bindChatActiveRows(localStore) func()      { return nil }
func chatLayerContainsEvent(ui.MouseEvent) bool { return false }
func focusChatLayerField(string)                {}

func chatTopLayerKind() string { return "" }
