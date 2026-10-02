//go:build !(js && wasm)

package chatui

// chatPlatform is empty outside the browser: the key is written Ctrl+K.
func chatPlatform() string { return "" }

func bindChatSearchShortcut(Model) func() { return nil }

func bindChatSearchRecent(Model) func() { return nil }
