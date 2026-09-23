//go:build !(js && wasm)

package chatui

import "github.com/monstercameron/GoWebComponents/v5/ui"

func virtualScrollGeometry(ui.Event) (float64, float64) { return 0, 0 }
func focusedVirtualMessageID() string                   { return "" }
func captureVirtualFocus() virtualFocus                 { return virtualFocus{} }
func imageViewerPostID() string                         { return "" }
func jumpPendingForRoom(string) bool                    { return false }
func virtualAwayIntent(ui.Event) bool                   { return false }
func virtualBottomDisarmed(string) bool                 { return false }
func syncVirtualTimeline(Model, []Message, virtualLayout, *virtualCache, virtualPosition, virtualFocus, func(virtualPosition), func()) func() {
	return nil
}
