//go:build js && wasm

package main

// chatActionRecovered takes down the failure line action wrote, once the same
// action has succeeded. It reports whether a line was taken down.
func chatActionRecovered(action string) bool {
	token := takeChatNoticeAction(action)
	if token == 0 || !chatBrowser.clearNotice(token) {
		return false
	}
	refreshChatRoute()
	return true
}
