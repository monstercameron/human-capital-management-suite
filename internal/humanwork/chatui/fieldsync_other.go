//go:build !(js && wasm)

package chatui

// installFieldSync is a browser concern (see fieldsync_js.go); the
// server-rendered tree carries the application value as an attribute the
// browser build picks up on hydration.
func installFieldSync()      {}
func clearChatScrollMemory() {}

// ScrollToNewest is a browser concern; the server-rendered tree has no
// scroll position.
func ScrollToNewest()      {}
func BeginScrollToNewest() {}
